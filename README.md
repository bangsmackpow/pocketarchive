# PocketArchive

Self-hosted, multi-tenant-ready email archiving and compliance platform for
Microsoft 365. PocketArchive receives Exchange Online journal reports (or PST /
mbox imports), encrypts raw messages and attachments, indexes searchable
metadata, and enforces retention and legal hold at both the API and PostgreSQL
layers.

The project is an MVP intended to run as a single Docker stack on a Linux VM:

```text
GitHub Actions ──▶ GHCR ──▶ Portainer stack ──▶ Docker / Docker Compose
```

## Architecture at a glance

```text
Microsoft 365 journaling
        │ SMTP + STARTTLS
        ▼
┌──────────────────────────── pocketarchive-engine ────────────────────────────┐
│ Go SMTP receiver ──▶ spool ──▶ journal queue                                 │
│                                                                             │
│ Import uploads ──▶ chunked spool ──▶ import jobs                            │
│                                                                             │
│ Workers ──▶ journal/mbox/PST parser ──▶ envelope encryption ──▶ PostgreSQL   │
│                                      └──▶ SHA-256 blob deduplication         │
│                                      └──▶ Meilisearch indexing              │
│                                                                             │
│ HTTP API ◀── TanStack Start BFF ◀── browser session cookie                  │
└─────────────────────────────────────────────────────────────────────────────┘
        │                    │                         │
        ▼                    ▼                         ▼
   PostgreSQL 16       Meilisearch               Valkey 8
   metadata, RLS,      tenant-filtered           login rate
   audit, retention    full-text search          limiting
```

| Service | Purpose |
| --- | --- |
| `web` | TanStack Start UI + BFF. Stores the engine JWT in an HttpOnly cookie. |
| `engine` | REST API, Go SMTP journal receiver, ingestion/index workers, crypto. |
| `db-init` | One-shot idempotent provisioning of `archive_app` and `archive`. |
| `postgres` | Metadata, row-level security, audit log, retention, job state. |
| `meilisearch` | Tenant-filtered full-text search. |
| `valkey` | Login rate limiting; failures fail open. |

**No Postfix is used.** The Go engine is the SMTP journal listener. Adding
Postfix would duplicate TLS, spooling, queueing and sender policy.

## Repository layout

```text
pocketarchive/
├── web/                       TanStack Start (React + Tailwind) frontend + BFF
├── engine/                    Go backend and workers
│   ├── cmd/engine/            serve | db-init | migrate | bootstrap | reindex
│   ├── internal/api/          internal REST API
│   ├── internal/smtp/         STARTTLS journal receiver (go-smtp)
│   ├── internal/ingest/       journal / mbox / PST parsing and HTML sanitization
│   ├── internal/crypto/       envelope encryption (AES-256-GCM)
│   ├── internal/dbinit/       idempotent PostgreSQL provisioning
│   ├── internal/store/        PostgreSQL repositories and tenant context
│   ├── internal/search/       Meilisearch client
│   ├── internal/worker/       journal, import and search-index workers
│   └── migrations/            embedded SQL schema, RLS and triggers
├── deploy/portainer/          production Compose stack + .env.example
├── deploy/dev/                local PostgreSQL / Meilisearch / Valkey
├── docs/                      architecture, deployment, operations, API and security
├── .github/workflows/ci-cd.yml
├── Makefile
└── README.md
```

## Security highlights

- Tenant-scoped tables have `tenant_id`, `FORCE ROW LEVEL SECURITY` and policies
  driven by transaction-local `app.current_tenant_id`.
- The engine connects as `archive_app` (`NOSUPERUSER NOBYPASSRLS`). Only the
  short-lived `db-init` container receives the PostgreSQL superuser password.
- Blobs use envelope encryption: a master KEK wraps per-tenant KEKs, which wrap
  per-blob DEKs. AES-256-GCM AAD binds ciphertext to tenant and SHA-256.
- Identical raw messages and attachments are deduplicated per tenant.
- Message metadata is immutable. PostgreSQL triggers reject deletion during
  legal hold or retention, and prevent retention from being shortened.
- `audit_logs` is append-only. Logins, ingestion, searches, views, downloads,
  exports, legal-hold changes and configuration changes are recorded.
- The browser only sees an HttpOnly, SameSite cookie; the engine JWT is never
  exposed to JavaScript.

See [docs/security.md](docs/security.md) for the threat model and limitations.

## Quick start (development)

Requirements: Docker, Go 1.26+, Node 22+.

```bash
# 1. Local PostgreSQL, Meilisearch and Valkey
make dev-up

# 2. Development configuration
export POSTGRES_PASSWORD='devpassword'
export ARCHIVE_DB_PASSWORD='devpassword'
export ARCHIVE_MASTER_KEK="$(openssl rand -base64 32)"
export SESSION_SECRET="$(openssl rand -base64 32)"
export DATABASE_URL='postgres://archive_app:devpassword@localhost:5432/archive?sslmode=disable'
export BLOB_STORAGE_PATH="$PWD/.data/blobs"
export SPOOL_PATH="$PWD/.data/spool"
export MEILI_URL='http://localhost:7700'
export MEILI_MASTER_KEY='devmasterkey'
export VALKEY_URL='redis://localhost:6379'
export BOOTSTRAP_ADMIN_EMAIL='admin@example.com'
export BOOTSTRAP_ADMIN_PASSWORD='ChangeMe-Now-123'
export SMTP_ALLOWED_DOMAINS='example.com'
export SESSION_COOKIE_SECURE='false'

# 3. Create the local archive_app role and archive database (idempotent)
make db-init

# 4. Run the engine: migrations, tenant/admin seed, API + SMTP + workers
cd engine && go run ./cmd/engine serve

# 5. Run the web app in a second terminal
cd web && npm install
ENGINE_INTERNAL_URL='http://localhost:8080' npm run dev
```

Open <http://localhost:3000> and sign in with the bootstrap administrator.

Useful commands:

```bash
make dev-down       # stop local dependencies
make dev-reset      # stop and delete local development volumes
make db-init        # repair/reapply the local archive_app password
make test           # Go tests + web production build
```

Full instructions: [docs/development.md](docs/development.md).

## Production deployment

1. Push to `main`; CI builds and publishes:
   - `ghcr.io/<owner>/pocketarchive-engine:latest` and `:<sha>`
   - `ghcr.io/<owner>/pocketarchive-web:latest` and `:<sha>`
2. In Portainer, create a **Git-based stack** using
   `deploy/portainer/docker-compose.yml`.
3. Set the environment from `deploy/portainer/.env.example`:
   ```bash
   openssl rand -base64 32   # ARCHIVE_MASTER_KEK
   openssl rand -base64 32   # SESSION_SECRET
   openssl rand -base64 32   # MEILI_MASTER_KEY
   openssl rand -hex 24      # POSTGRES_PASSWORD (superuser; postgres + db-init only)
   openssl rand -hex 24      # ARCHIVE_DB_PASSWORD (engine)
   ```
4. Terminate TLS in front of port `3000`. Production cookies are `Secure`.
5. Point Office 365 journaling at the engine on port `2525` (or add port 25),
   with a trusted SMTP certificate and `SMTP_ALLOWED_DOMAINS` set.

Startup order is deterministic:

```text
postgres (TCP healthy) → db-init (exit 0) → engine migrations/bootstrap → web
```

The complete deployment guide is [docs/deployment.md](docs/deployment.md).
Office 365 journaling is covered in
[docs/smtp-journaling.md](docs/smtp-journaling.md).

## Documentation

| Document | Contents |
| --- | --- |
| [docs/architecture.md](docs/architecture.md) | Components, data flows, schema, startup lifecycle, scaling, failure behavior |
| [docs/deployment.md](docs/deployment.md) | VM prerequisites, Portainer/Git stack, environment reference, first boot, TLS, upgrades |
| [docs/operations.md](docs/operations.md) | Health checks, logs, backups/restore, key and password rotation, reindex, reset, disaster recovery |
| [docs/security.md](docs/security.md) | Trust boundaries, RLS, encryption, secrets, auth, audit, hardening, known gaps |
| [docs/api.md](docs/api.md) | Internal REST API, auth, endpoint reference, upload/search/legal-hold examples |
| [docs/smtp-journaling.md](docs/smtp-journaling.md) | Go SMTP receiver behavior, Office 365 setup, TLS, allowlists, testing, quarantine |
| [docs/troubleshooting.md](docs/troubleshooting.md) | Startup, database, image, SMTP, search, import and UI issues |
| [docs/development.md](docs/development.md) | Local workflow, tests, migrations, adding API/UI features |
| [docs/adr/0001-database-provisioning.md](docs/adr/0001-database-provisioning.md) | Why database provisioning is a `db-init` job, not a Postgres init script |
| [CONTRIBUTING.md](CONTRIBUTING.md) | Contribution workflow and repository conventions |
| [SECURITY.md](SECURITY.md) | Vulnerability reporting and deployment security checklist |

## Verification

```bash
cd engine && go build ./... && go vet ./... && go test ./...
cd web && npm ci && npm run typecheck && npm run build
docker compose -f deploy/portainer/docker-compose.yml \
  --env-file deploy/portainer/.env.example config
docker compose -f deploy/dev/docker-compose.dev.yml config
```

CI runs the Go and web checks on every PR. On `main` it builds and pushes the
images, then optionally triggers a Portainer webhook. Published images are
currently `linux/amd64`; the engine image compiles `readpst` from pinned libpst
0.6.76 and runs as UID 10001.

## Current limitations

- Single-tenant UI. Tenant IDs, RLS, KEKs, storage paths and search filters are
  multi-tenant throughout, but there is no tenant switcher or tenant-admin API.
- No Microsoft Graph mailbox sync, MFA/SSO, S3 blob backend, antivirus scanning,
  automated backup scheduler, or blob garbage collection.
- Message bodies are stored in PostgreSQL for preview and search. Database and
  volume encryption remain infrastructure responsibilities.
- Retention applies to newly ingested messages. Existing `retain_until` values
  are immutable and can only be extended.
- The deployment is designed for a single engine instance sharing the blob and
  spool volumes; horizontal scaling and HA are not yet supported.
- Only `linux/amd64` images are published.
