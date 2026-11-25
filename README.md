# PocketArchive

Self-hosted, multi-tenant-ready email archiving and compliance platform for
Microsoft 365. PocketArchive receives Exchange Online journal reports (or PST /
mbox imports), encrypts every raw message and attachment, indexes searchable
metadata, and enforces legal hold and retention at both the API and PostgreSQL
layers.

Built to deploy as a single Docker stack on a Linux VM:

```
GitHub Actions ──▶ GHCR ──▶ Portainer stack ──▶ Docker / Docker Compose
```

## Components

```
pocketarchive/
├── web/                       TanStack Start (React + Tailwind) UI and BFF
├── engine/                    Go API, SMTP journal receiver, workers
│   ├── cmd/engine/            serve | migrate | bootstrap | reindex
│   ├── internal/api/          internal REST API
│   ├── internal/smtp/         STARTTLS journal receiver (go-smtp)
│   ├── internal/ingest/       journal / mbox / PST parsing
│   ├── internal/crypto/       envelope encryption (AES-256-GCM)
│   ├── internal/store/        PostgreSQL repositories + RLS context
│   ├── internal/search/       Meilisearch client
│   ├── internal/worker/       import, journal and index workers
│   └── migrations/            embedded SQL schema + RLS + triggers
├── deploy/portainer/          production Compose stack + .env.example
├── deploy/dev/                local Postgres / Meilisearch / Valkey
├── deploy/postgres/           non-superuser role bootstrap SQL
└── .github/workflows/ci-cd.yml
```

| Service | Purpose |
| --- | --- |
| `web` | Authenticated UI + BFF. Holds the engine JWT in an HttpOnly cookie. |
| `engine` | REST API, SMTP journal listener, ingestion/index workers, crypto. |
| `postgres` | Immutable metadata, RLS, audit log, job state. |
| `meilisearch` | Tenant-filtered full-text search. |
| `valkey` | Login rate limiting (fails open if unavailable). |

## Security model

- **Tenant isolation** — every tenant-scoped table has `tenant_id`, `FORCE ROW
  LEVEL SECURITY`, and a policy driven by transaction-local
  `app.current_tenant_id`. The engine connects as `archive_app`, a
  non-superuser role with `NOBYPASSRLS`. Startup refuses to migrate under a
  superuser/BYPASSRLS role because RLS would be silently bypassed.
- **Envelope encryption** — `ARCHIVE_MASTER_KEK` wraps a random KEK per tenant;
  each blob gets a random 32-byte DEK wrapped with the tenant KEK. Blob and key
  AES-256-GCM AAD is `tenant_id + ":" + sha256`, so ciphertext cannot be moved
  between tenants or renamed.
- **Deduplication** — SHA-256 is calculated on raw `.eml` bytes and each
  attachment before encryption. Blobs are stored once per tenant at
  `tenants/{tenant_id}/blobs/{sha256[:2]}/{sha256}`.
- **Integrity** — messages carry `raw_sha256`, immutable `retain_until` and
  `legal_hold`. PostgreSQL triggers reject deletion while held or retained, block
  metadata mutation, and prevent shortening retention. The API applies the same
  checks before attempting a delete.
- **Audit** — `audit_logs` is append-only (a trigger rejects UPDATE/DELETE) and
  records logins, ingestion, search queries, message views, attachment
  downloads, exports, legal-hold changes, imports, sources and retention changes.
- **Browser auth** — the engine issues an HS256 JWT plus revocable session row.
  The web BFF stores the JWT in a `HttpOnly`, `SameSite=Lax` cookie; the token is
  never exposed to browser JavaScript. The BFF proxy strips cookies and adds
  `Authorization: Bearer`.

> The raw message body is stored in PostgreSQL for preview and search. Database
> and volume encryption remain an infrastructure responsibility. Blob objects
> (raw `.eml`, journal envelopes, attachments) are always encrypted.

## Quick start (development)

Requirements: Docker, Go 1.26+, Node 22+.

```bash
# 1. Local dependencies
make dev-up

# 2. Secrets
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

# 3. Engine (migrates, seeds the tenant/admin, starts API + SMTP + workers)
cd engine && go run ./cmd/engine serve

# 4. Web (second terminal)
cd web && npm install
ENGINE_INTERNAL_URL='http://localhost:8080' npm run dev
```

Open <http://localhost:3000> and sign in with the bootstrap administrator.

`make dev-down` stops the dependency stack. Data lives in Docker volumes.

### Engine commands

```bash
engine serve                     # API + SMTP + workers (default)
engine serve --enable-api=false  # e.g. separate SMTP/worker roles
engine migrate
engine bootstrap --email admin@example.com --password 'long-password'
engine reindex                   # rebuild the Meilisearch index
engine version
```

## Production deployment (Portainer + GHCR)

1. Push this repository to `main`. GitHub Actions builds and pushes:
   - `ghcr.io/<owner>/pocketarchive-engine:latest`
   - `ghcr.io/<owner>/pocketarchive-engine:<sha>`
   - `ghcr.io/<owner>/pocketarchive-web:latest`
   - `ghcr.io/<owner>/pocketarchive-web:<sha>`
2. Make both GHCR packages **public** (or add a registry credential to
   Portainer) under *Package settings → Change visibility*.
3. Generate secrets:

   ```bash
   openssl rand -base64 32   # ARCHIVE_MASTER_KEK (must decode to exactly 32 bytes)
   openssl rand -base64 32   # SESSION_SECRET (32+ characters)
   openssl rand -base64 32   # MEILI_MASTER_KEY
   openssl rand -hex 24      # POSTGRES_PASSWORD (hex avoids URL-encoding in DATABASE_URL)
   ```

4. In Portainer, create a stack **from Git** with:
   - Repository: your PocketArchive repo
   - Compose path: `deploy/portainer/docker-compose.yml`
   - Environment: paste the contents of `deploy/portainer/.env.example` with
     real values.

   The stack mounts `deploy/postgres/init-app-role.sh` to create the
   non-superuser `archive_app` role and `archive` database. Deploying from Git
   keeps that relative path available. If you paste the Compose file manually,
   also provide the init script and adjust the volume path.

5. Add `PORTAINER_WEBHOOK_URL` as a GitHub Actions secret to redeploy
   automatically after each successful image push.

Only port `3000` (web) and `2525` (SMTP journal) are published. The engine API
stays on the internal Docker network. Terminate TLS in front of port 3000
(Caddy, Traefik, nginx, or a cloud load balancer): production cookies are marked
`Secure`, so the UI must be served over HTTPS. To accept journal mail on port
25, add `"25:2525"` to the engine ports; most cloud providers require a support
request to unblock inbound port 25.

### Office 365 journaling

1. Create a shared mailbox or mail contact for the journal target
   (`journal@mail.yourcompany.com` by default).
2. In Microsoft Purview / Exchange admin center, create a journal rule that
   sends all internal and external messages to that address.
3. Set `SMTP_ALLOWED_DOMAINS=yourcompany.com` (or an IP allowlist / SMTP AUTH).
4. Mount a publicly trusted certificate for `SMTP_HOSTNAME`:
   `SMTP_TLS_CERT_FILE=/certs/fullchain.pem`, `SMTP_TLS_KEY_FILE=/certs/privkey.pem`.
   The engine runs as UID 10001, so the mounted key must be readable by that
   user (for example `chmod 640` plus a matching group, or a Docker secret).
   Without mounted certificates the engine generates a self-signed dev cert.
5. Send a test message and confirm it appears in **Archive search** with source
   `journal`. Recent journal metrics are shown on **Sources**.

The receiver writes the MIME payload to the spool, inserts a queue row, and only
then returns `250 OK`. If spooling or enqueueing fails it returns `451` so the
sender retries. Office 365 journal envelopes are preserved as their own encrypted
blob while the inner `message/rfc822` original is archived and indexed.

### Backups

Back up, ideally to separate storage:

- PostgreSQL (metadata, tenant wrapped KEKs, audit log, job state)
- the `archive_blobs` volume (encrypted blobs)
- `ARCHIVE_MASTER_KEK` in a secrets manager / offline vault

Restoring blobs without the master KEK is impossible by design. The spool and
Meilisearch index are disposable; search can be rebuilt with `engine reindex`.

## Authentication API

The engine exposes `/api/v1` to the web BFF:

- `POST /auth/login`, `GET /auth/me`, `POST /auth/logout`
- `GET /tenant`, `PUT /tenant/settings`
- `GET /setup`, `GET /stats`, `GET|POST|PATCH|DELETE /sources`
- `POST /imports`, `PUT /imports/{id}/chunks/{n}`, `POST /imports/{id}/complete`
- `GET /imports`, `GET /imports/{id}`, `GET /imports/{id}/errors`
- `GET /search` (query, sender, recipient, dates, attachment flag, source, legal hold)
- `GET /messages/{id}`, `DELETE /messages/{id}`
- `GET /messages/{id}/export`, `GET /messages/{id}/attachments/{attachmentId}`
- `POST /messages/{id}/legal-hold`
- `GET /audit-logs`
- `GET /healthz`, `GET /readyz`

All protected endpoints require `Authorization: Bearer <JWT>` and resolve the
tenant from the token; tenant IDs are never accepted from clients.

## Verification

```bash
cd engine && go build ./... && go vet ./... && go test ./...
cd web && npm ci && npm run typecheck && npm run build
docker compose -f deploy/portainer/docker-compose.yml --env-file deploy/portainer/.env.example config
docker compose -f deploy/dev/docker-compose.dev.yml config
```

CI runs the Go and web checks on every PR and publishes multi-stage images only
from `main`. The engine image compiles `readpst` from pinned libpst 0.6.76
(because Alpine has no libpst package) and runs as UID 10001.

## Current limitations / roadmap

- Single visible tenant in the UI; tenant IDs, RLS, KEKs and search filters are
  already multi-tenant throughout.
- No Microsoft Graph mailbox sync, MFA/SSO, S3 blob backend, antivirus scanning,
  automated backup scheduler, or blob garbage collection.
- Retention applies to newly ingested messages; existing `retain_until` values
  are immutable and can only be extended.
- Legal holds and retention are enforced, but a full records-management policy
  engine (labels, disposition review) is future work.
- Only `linux/amd64` images are published today; the Go binary is portable, but
  the libpst build would need an arm64 build stage for arm64 images.
