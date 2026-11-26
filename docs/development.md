# Development

## Prerequisites

- Docker with Compose v2
- Go 1.26+
- Node.js 22+ and npm
- `openssl`

## First-time setup

```bash
git clone https://github.com/bangsmackpow/pocketarchive.git
cd pocketarchive
make dev-up
```

`make dev-up` starts:

- PostgreSQL 16 on `localhost:5432` (`postgres` / `devpassword`)
- Meilisearch on `localhost:7700` with master key `devmasterkey`
- Valkey on `localhost:6379`

Create the local application role and database:

```bash
make db-init
```

The target invokes `engine db-init` with the local admin connection and defaults
`archive_app` / `archive` / `devpassword`. It is idempotent and can be re-run to
reset the application password.

Configure the engine for a shell session:

```bash
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
```

The production stack uses `PG*` environment variables instead of
`DATABASE_URL`; `DATABASE_URL` remains supported for local convenience.

## Run the services

Terminal 1 — engine:

```bash
cd engine
go run ./cmd/engine serve
```

This:

- applies migrations,
- ensures the default tenant, tenant KEK and journal source,
- creates the bootstrap admin if no users exist,
- fails fast if there is no administrator,
- starts the API on `:8080`, SMTP on `:2525` and workers.

Terminal 2 — web:

```bash
cd web
npm install
ENGINE_INTERNAL_URL='http://localhost:8080' npm run dev
```

Open <http://localhost:3000>.

## Make targets

```bash
make help
make dev-up
make db-init
make dev-down
make dev-reset
make engine-build
make engine-test
make web-install
make web-dev
make web-build
make compose-check
make test
```

`make dev-reset` deletes the local dependency volumes. `make test` runs Go
tests/vet and a production web build.

## Engine commands

```bash
cd engine
go run ./cmd/engine serve
go run ./cmd/engine serve --enable-api=false
go run ./cmd/engine db-init
go run ./cmd/engine migrate
go run ./cmd/engine bootstrap --email admin@example.com --password 'long-password'
go run ./cmd/engine reindex
go run ./cmd/engine version
```

`db-init` reads:

- `ADMIN_DATABASE_URL`, or standard `PGHOST`/`PGDATABASE`/`PGUSER`/`PGPASSWORD`
  for the superuser connection,
- `ARCHIVE_DB_USER`, `ARCHIVE_DB_NAME`, `ARCHIVE_DB_PASSWORD` for the role to
  provision.

`serve`, `migrate`, `bootstrap` and `reindex` use:

- `DATABASE_URL`, or
- standard `PGHOST`/`PGDATABASE`/`PGUSER`/`PGPASSWORD`.

## Tests and checks

```bash
cd engine
gofmt -w .
go build ./...
go vet ./...
go test ./...
```

```bash
cd web
npm run typecheck
npm run build
```

The Go tests cover crypto/AAD behavior, JWT/password handling, mbox/journal
parsing, HTML sanitization, SMTP policy and config validation. They do not
require a database. Manual integration testing uses the local dependency stack.

Compose validation:

```bash
docker compose -f deploy/portainer/docker-compose.yml \
  --env-file deploy/portainer/.env.example config
docker compose -f deploy/dev/docker-compose.dev.yml config
```

## Database migrations

Migrations live in `engine/migrations/` and are embedded into the binary.

- Add a new lexically ordered file, e.g. `0002_....sql`.
- Never edit an already-applied migration; add a new one.
- Migrations run in a transaction and are recorded in `schema_migrations`.
- There are no down migrations.
- Test against a fresh database (`make dev-reset`, `make dev-up`, `make db-init`,
  `go run ./cmd/engine serve`).
- RLS/tenant policies must be added for every new tenant-scoped table.
- Preserve the non-superuser/BYPASSRLS startup guard.

Useful SQL checks:

```bash
docker exec -it <postgres> psql -U postgres -d archive
```

```sql
SELECT * FROM schema_migrations;
SELECT tablename, rowsecurity, forcerowsecurity FROM pg_tables WHERE schemaname='public';
```

## Adding an API endpoint

1. Add the route in `engine/internal/api/server.go`.
2. Implement the handler in the appropriate `handlers_*.go` file.
3. Use `principalFrom(ctx)` for tenant/user identity. Never accept a tenant ID
   from the request.
4. Use `store.WithTenant` for every tenant-scoped query.
5. Add an audit entry for security/compliance-sensitive actions.
6. Enforce admin-only mutations with `adminOnly`.
7. Validate request bodies and size limits.
8. Add tests where practical and update `docs/api.md`.

## Adding a frontend page or feature

- File-based routes live in `web/src/routes/`.
- Authenticated pages are children of `_app.tsx`; the layout guards sessions via
  the server function `getSessionFn`.
- Browser API calls go through `/api/proxy/...` using `apiFetch` in
  `web/src/lib/api.ts`; the BFF attaches the HttpOnly engine token.
- Do not read or store the engine JWT in client state.
- Use TanStack Query for server data and invalidate affected query keys after
  mutations.
- Shared UI primitives are in `web/src/components/ui.tsx`.
- Run `npm run typecheck` and `npm run build`.

## Deployment changes

- Production services and env live in `deploy/portainer/docker-compose.yml`.
- `db-init` must run before the engine; keep the
  `service_completed_successfully` dependency.
- The engine must not receive PostgreSQL superuser credentials.
- Do not reintroduce relative host bind mounts for initialization.
- Keep the engine as the SMTP listener; do not add Postfix.
- Update `deploy/portainer/.env.example` and `docs/deployment.md` when adding
  required settings.

## CI

`.github/workflows/ci-cd.yml`:

- on PRs and `main`: Go build/vet/test, web typecheck/build;
- on `main`: build and push amd64 images to GHCR with `latest` and the commit SHA;
- optionally POST to `PORTAINER_WEBHOOK_URL`.

A docs-only push still triggers image builds; that is expected.

## Coding conventions

- Go: standard library conventions, `gofmt`, `go vet`, small packages under
  `engine/internal`.
- Errors: wrap with context; do not log secrets.
- SQL: always scope tenant tables inside `WithTenant`; add indexes for query
  paths.
- Frontend: strict TypeScript, functional React components, no secrets in the
  client bundle.
- Comments should explain invariants and security assumptions, not restate code.

## Resetting local state

```bash
make dev-down
make dev-reset
make dev-up
make db-init
```

Then restart the engine and web. This deletes all local archive data, Meilisearch
data and rate-limit counters.
