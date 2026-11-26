# Architecture

## Goals

PocketArchive is a self-hosted compliance archive for Microsoft 365 email. The
initial deployment is single-tenant, but the data model, encryption and search
paths are tenant-scoped so additional tenants can be added without a schema
rewrite.

Primary goals:

- Capture journal mail reliably without losing messages on process failure.
- Keep raw messages and attachments encrypted at rest with per-tenant keys.
- Provide fast tenant-scoped search and a usable compliance UI.
- Make retention and legal hold enforceable outside the UI.
- Keep an append-only audit trail.
- Deploy as one Git-driven Docker stack.

## Runtime topology

```text
                    ┌──────────────────────────────┐
  Microsoft 365 ───▶│ engine :2525 SMTP + STARTTLS │
  (journaling)      └──────────────┬───────────────┘
                                   │ spool file + queue row
                                   ▼
                    ┌──────────────────────────────┐
                    │ journal_queue / import_jobs  │
                    └──────────────┬───────────────┘
                                   │ workers
                                   ▼
              ┌────────────────────────────────────────┐
              │ parser → encryption → PostgreSQL        │
              │        → SHA-256 blob deduplication     │
              │        → search indexing                │
              └───────────────┬────────────────────────┘
                              │
              ┌───────────────┴───────────────┐
              ▼                               ▼
      PostgreSQL 16                     Meilisearch
      RLS, audit, metadata             tenant-filtered FTS
              ▲
              │ HTTP API :8080 (internal only)
              │
      TanStack Start BFF :3000 ──▶ browser (HttpOnly cookie)
```

| Component | Responsibility |
| --- | --- |
| `engine serve` | Runs the HTTP API, SMTP receiver and worker pool in one process. Flags can disable components. |
| `engine db-init` | One-shot provisioning of `archive_app` and the `archive` database. |
| PostgreSQL | Source of truth for tenants, users, sessions, message metadata, blobs, job state and audit. |
| Meilisearch | Full-text index; PostgreSQL remains authoritative. |
| Valkey | Login rate limiting only. Failures fail open. |
| `web` | TanStack Start SSR UI and BFF proxy; holds the engine JWT. |

## Startup lifecycle

Production Compose orders startup explicitly:

```text
postgres
  └─ TCP healthcheck passes
       └─ db-init (one-shot)
            ├─ connect as PostgreSQL superuser
            ├─ create/repair archive_app (NOSUPERUSER NOBYPASSRLS)
            ├─ create/ensure archive database owned by archive_app
            ├─ reset archive_app password to ARCHIVE_DB_PASSWORD
            └─ exit 0
                 └─ engine serve
                      ├─ connect as archive_app using PG* env
                      ├─ apply embedded migrations
                      ├─ ensure tenant, tenant KEK and default journal source
                      ├─ create bootstrap admin if no users exist
                      ├─ fail if no administrator exists
                      ├─ ensure Meilisearch index/settings (retry)
                      ├─ start API, SMTP and workers
                      └─ become healthy
                           └─ web
```

The engine never receives the PostgreSQL superuser password. `db-init` is
idempotent, so it also repairs a drifted `archive_app` password on redeploy.
See [ADR 0001](adr/0001-database-provisioning.md).

## Ingestion paths

### Office 365 journal mail

1. Remote SMTP client connects to the engine on port `2525`.
2. STARTTLS is negotiated if configured. A self-signed certificate is generated
   when no certificate paths are mounted.
3. The receiver applies sender-domain, CIDR and optional SMTP AUTH policy.
4. The raw MIME payload is written to the spool and synced to disk.
5. A `journal_queue` row is inserted.
6. Only then does the engine return `250 OK`.
7. A worker claims the row and parses the journal envelope. The inner
   `message/rfc822` original is archived; the outer journal envelope is
   preserved as a separate encrypted blob.
8. On success the spool file is removed. Failures are retried with backoff up to
   three attempts and then quarantined.

### PST / mbox imports

1. The BFF forwards an authenticated import request to the engine.
2. The engine creates an `import_jobs` row in `awaiting_upload` state.
3. The browser uploads sequential 8 MB chunks. Each chunk is appended to a spool
   file and the received byte/chunk counters are updated.
4. `complete` moves the job to `queued`.
5. A worker claims the job, counts items, parses them and archives each message.
6. Progress (`processed`, `duplicates`, `errors`) is updated while running.
7. Item-level parse failures are recorded in `import_job_errors`; the job can
   still complete with a non-zero error count. Fatal errors mark it `failed`.

PST extraction invokes `readpst` from libpst inside the engine container and
processes the resulting `.eml` files one at a time in a temporary workspace.

## Processing pipeline

For each message:

1. Parse with enmime.
2. Extract headers, addresses, date, plain text and HTML.
3. Sanitize HTML with bluemonday.
4. Compute SHA-256 of the raw message and each attachment.
5. Encrypt each new blob with a fresh DEK, wrapped by the tenant KEK.
6. Insert message metadata and attachment rows into PostgreSQL in one transaction.
7. Write an ingestion audit entry.
8. Leave `search_indexed_at` empty; an index worker eventually sends the search
   document to Meilisearch.

Blobs are content-addressed per tenant:

```text
<BLOB_STORAGE_PATH>/tenants/{tenant_id}/blobs/{sha256[:2]}/{sha256}
```

A duplicate blob is encrypted and stored once, then referenced by multiple
message/attachment rows.

## Data model

All tenant-scoped tables have `tenant_id`, row-level security and a tenant
policy. `tenants` is the tenant registry; `schema_migrations` is infrastructure.

| Table | Purpose |
| --- | --- |
| `tenants` | Tenant registry, wrapped tenant KEK, retention default. |
| `users` | Admin/auditor accounts, bcrypt password hashes. |
| `sessions` | Revocable sessions; token hashes only. |
| `ingestion_sources` | Journal/PST/mbox/eml source configuration shown in the UI. |
| `import_jobs` | Upload state and processed/duplicate/error progress. |
| `import_job_errors` | Quarantined item-level import errors. |
| `journal_queue` | Durable SMTP spool queue, attempts and retry schedule. |
| `blobs` | SHA-256, plaintext size, wrapped DEK and file nonce. |
| `messages` | Immutable headers/body, hashes, retention, legal hold, source. |
| `message_attachments` | Attachment metadata and blob references. |
| `audit_logs` | Append-only security and compliance event log. |

Key indexes include tenant + sent date, tenant + raw hash, sender, GIN on
recipients, and a partial index for messages awaiting search indexing.

## Tenant isolation

Every repository operation runs through a transaction that executes:

```sql
SELECT set_config('app.current_tenant_id', $1, true);
```

RLS policies compare `tenant_id` to that transaction-local setting. Tables are
`ENABLE`d and `FORCE`d, so even the table owner is subject to RLS. The migration
refuses to run under a superuser or `BYPASSRLS` role because those roles bypass
row-level security.

The API resolves the tenant from the authenticated JWT. No endpoint accepts a
tenant ID from the client.

Workers cannot poll all tenants in one query because RLS would hide rows. They
enumerate the tenant registry, then set tenant context separately per tenant
before claiming queue rows.

## Search

Meilisearch stores one document per message. Every document contains
`tenant_id`, and every query ANDs a tenant filter with user filters.

Searchable fields: subject, body text, sender, recipient, attachment names.
Filterable fields: tenant, sender, recipients, attachment flag, source type,
legal hold, sent timestamp. Sort: sent timestamp descending.

PostgreSQL remains authoritative. If the index is unavailable:

- writes/ingestion continue; messages remain unindexed,
- the index worker retries,
- API search returns `502 search backend unavailable`,
- `engine reindex` rebuilds the index from PostgreSQL.

## Failure and retry behavior

| Failure | Behavior |
| --- | --- |
| SMTP spool write or queue insert fails | Return `451`; sender retries. |
| Journal parse/message insert fails | Retry up to 3 attempts with backoff, then quarantine. |
| Import item fails | Record item error, continue job, increment `error_items`. |
| Import is fatally malformed | Job marked `failed`; spool remains for inspection. |
| Meilisearch down | Ingestion continues; indexer retries; search returns 502. |
| Valkey down | Login rate limiter fails open. |
| Database unavailable at startup | Engine exits; Compose restarts it. |
| No administrator and no bootstrap credentials | Engine exits with a clear error. |
| `db-init` fails | Engine is not started; inspect `db-init` logs. |

## Scaling and availability

The current MVP is designed for **one engine instance** with shared
`archive_blobs` and `archive_spool` volumes. Database queue claims use
`SKIP LOCKED`, so limited horizontal worker scaling is technically possible, but
HA, leader election, multi-node blob storage and SMTP failover are not supported.

Postgres, Meilisearch and Valkey are single-node services in the stack. Backups
and restore are covered in [operations.md](operations.md).

## Code map

| Path | Contents |
| --- | --- |
| `engine/cmd/engine` | CLI modes and process lifecycle. |
| `engine/internal/api` | HTTP routing, auth middleware, handlers, import/search/legal-hold APIs. |
| `engine/internal/smtp` | go-smtp backend, TLS, sender policy, spooling. |
| `engine/internal/ingest` | MIME parsing, journal report unpacking, mbox reader, PST extraction, sanitizer. |
| `engine/internal/crypto` | Master key, tenant KEK wrapping, blob DEK wrapping and AES-GCM. |
| `engine/internal/archive` | Encrypted blob service and deduplication. |
| `engine/internal/store` | PostgreSQL queries and tenant transaction context. |
| `engine/internal/search` | Meilisearch HTTP client and document mapping. |
| `engine/internal/worker` | Journal, import and index loops. |
| `engine/internal/dbinit` | Idempotent PostgreSQL role/database provisioning. |
| `engine/migrations` | Embedded schema, RLS, immutability/audit triggers. |
| `web/src/routes` | Login, archive search, imports, sources, compliance, BFF proxy. |
| `deploy/portainer` | Production stack and environment template. |
| `deploy/dev` | Local dependency stack. |
