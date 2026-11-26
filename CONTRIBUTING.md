# Contributing

## Before you start

- Read the [architecture](docs/architecture.md) and [security](docs/security.md)
  documents.
- Check existing issues/PRs to avoid duplicate work.
- Do not commit secrets, `.env` files, dumps, archived email, or private keys.

## Local workflow

```bash
make dev-up
make db-init
cd engine && go run ./cmd/engine serve
# second terminal
cd web && npm run dev
```

See [docs/development.md](docs/development.md) for environment variables and
local reset instructions.

## Checks

Run before opening a PR:

```bash
cd engine
gofmt -w .
go build ./...
go vet ./...
go test ./...

cd ../web
npm run typecheck
npm run build
```

Validate the deployment files:

```bash
docker compose -f deploy/portainer/docker-compose.yml \
  --env-file deploy/portainer/.env.example config
docker compose -f deploy/dev/docker-compose.dev.yml config
```

CI runs the same Go/web checks on every pull request.

## Code guidelines

- Go: keep packages under `engine/internal` focused; wrap errors with context;
  never log passwords, tokens, message bodies or KEKs.
- SQL: every tenant-scoped query must run inside `store.WithTenant`; add RLS
  policies to every new tenant-scoped table; add indexes for new query patterns.
- Security: preserve the non-superuser/no-BYPASSRLS startup guard, the
  `db-init` least-privilege boundary, and append-only audit behavior.
- API: derive tenant identity from the JWT, never from request input. Enforce
  admin-only mutations server-side and audit sensitive actions.
- Frontend: strict TypeScript, no secrets in client code, all authenticated API
  calls through the BFF proxy.
- Email HTML: keep sanitization server-side; keep the preview iframe sandboxed.

## Migrations

- Add a new lexically ordered file under `engine/migrations/`; do not edit an
  applied migration.
- Migrations are embedded and forward-only.
- Test from an empty database (`make dev-reset`, `make dev-up`, `make db-init`).
- Include RLS enable/force and policies for new tenant tables.
- Update the schema description in `docs/architecture.md` if tables change.

## Pull requests

- Keep changes focused and explain the operational/security impact.
- Update documentation when behavior, configuration or deployment changes.
- Include reproduction steps and verification output for bug fixes.
- Do not reintroduce Postfix or host-path initialization scripts into the
  production stack.

## Commit messages

Use a concise imperative subject, optionally with a body describing context,
tradeoffs and verification. Example:

```text
fix: make db-init idempotent and enforce least privilege

Moves application role provisioning out of the Postgres entrypoint.
Verified against a clean PostgreSQL volume and an existing volume.
```
