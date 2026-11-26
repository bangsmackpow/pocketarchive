# Deployment

## Target environment

- Linux VM with Docker Engine and Portainer.
- A public hostname for the UI and SMTP endpoint.
- TLS termination for the web UI (Caddy, Traefik, nginx, or a cloud load balancer).
- Inbound SMTP access on `2525`, or `25`/`587` if you map those ports.
- Outbound internet access for image pulls.

The stack runs as a single-node Compose project:

| Service | Image | Exposed |
| --- | --- | --- |
| `web` | `ghcr.io/<owner>/pocketarchive-web` | host `3000` |
| `engine` | `ghcr.io/<owner>/pocketarchive-engine` | host `2525`; API internal `8080` |
| `db-init` | engine image, one-shot | none |
| `postgres` | `postgres:16-alpine` | internal |
| `meilisearch` | `getmeili/meilisearch:v1.15` | internal |
| `valkey` | `valkey/valkey:8-alpine` | internal |

No Postfix service is used. The Go engine is the SMTP journal receiver.

## Images and CI

Pushes to `main` run `.github/workflows/ci-cd.yml`:

1. `go build`, `go vet`, `go test` in `engine/`.
2. `npm ci`, typecheck and production build in `web/`.
3. On `main`, build and push:
   - `ghcr.io/<owner>/pocketarchive-engine:latest`
   - `ghcr.io/<owner>/pocketarchive-engine:<commit-sha>`
   - `ghcr.io/<owner>/pocketarchive-web:latest`
   - `ghcr.io/<owner>/pocketarchive-web:<commit-sha>`
4. Optionally POST to `secrets.PORTAINER_WEBHOOK_URL`.

Images are currently built for `linux/amd64` only.

Make the GHCR packages public, or configure a registry credential in Portainer:

- Package settings → Change visibility → Public

For production rollback, pin `ENGINE_IMAGE` / `WEB_IMAGE` to a known commit SHA
rather than relying on `latest`.

## Generate secrets

```bash
openssl rand -base64 32   # ARCHIVE_MASTER_KEK — must decode to exactly 32 bytes
openssl rand -base64 32   # SESSION_SECRET — at least 32 characters
openssl rand -base64 32   # MEILI_MASTER_KEY
openssl rand -hex 24      # POSTGRES_PASSWORD — PostgreSQL superuser, postgres + db-init only
openssl rand -hex 24      # ARCHIVE_DB_PASSWORD — engine application role
```

Use hex for the database passwords. They are passed through `PG*` variables
rather than a URL, but hex avoids shell/YAML quoting surprises.

Keep an offline copy of `ARCHIVE_MASTER_KEK`. Without it, encrypted blobs cannot
be recovered. Changing it after data exists without re-wrapping tenant KEKs will
make existing blobs unreadable.

## Create the Portainer stack

1. In Portainer: **Stacks → Add stack → Git repository**.
2. Repository: `https://github.com/bangsmackpow/pocketarchive` (or your fork).
3. Reference: `refs/heads/main`.
4. Compose path: `deploy/portainer/docker-compose.yml`.
5. Paste `deploy/portainer/.env.example` into the environment editor and replace
   all placeholders.
6. Deploy.

The stack has no required host-path bind mounts. Optional TLS certificate
mounts are commented out in the Compose file.

### Environment reference

Required by the Compose file:

| Variable | Purpose |
| --- | --- |
| `POSTGRES_PASSWORD` | PostgreSQL superuser password. Available to `postgres` and `db-init`; not passed to the engine. |
| `ARCHIVE_DB_PASSWORD` | Password for the `archive_app` role; used by `db-init` and engine. |
| `ARCHIVE_MASTER_KEK` | Base64 32-byte envelope-encryption master key. |
| `SESSION_SECRET` | JWT signing key and session-token pepper, 32+ characters. |
| `MEILI_MASTER_KEY` | Meilisearch master key. |

SMTP and first-run settings:

| Variable | Default | Purpose |
| --- | --- | --- |
| `SMTP_ALLOWED_DOMAINS` | empty | Comma-separated sender domains. With no IP/AUTH policy, mail is rejected. |
| `SMTP_ALLOWED_IPS` | empty | Comma-separated CIDRs trusted as journal sources. |
| `SMTP_AUTH_USERNAME` / `SMTP_AUTH_PASSWORD` | empty | Enables SMTP AUTH (PLAIN over TLS). |
| `SMTP_HOSTNAME` | `mail.archive.local` | EHLO hostname and self-signed certificate CN. |
| `SMTP_TLS_CERT_FILE` / `SMTP_TLS_KEY_FILE` | empty | Mounted certificate paths inside the container. |
| `SMTP_MAX_MESSAGE_BYTES` | `104857600` | Maximum accepted message size. |
| `BOOTSTRAP_ADMIN_EMAIL` | empty | First-run administrator email. |
| `BOOTSTRAP_ADMIN_PASSWORD` | empty | First-run administrator password, minimum 12 characters. |
| `DEFAULT_TENANT_SLUG` | `default` | Default tenant slug. |
| `DEFAULT_TENANT_NAME` | `Default Archive` | Display name. |
| `DEFAULT_RETENTION_DAYS` | `2557` | Retention stamped on newly ingested mail. |

Optional tuning:

| Variable | Default | Purpose |
| --- | --- | --- |
| `ARCHIVE_DB_USER` | `archive_app` | Application role override. |
| `ARCHIVE_DB_NAME` | `archive` | Application database override. |
| `MEILI_INDEX` | `messages` | Meilisearch index UID. |
| `SESSION_TTL` | `12h` | JWT/session lifetime. |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error`. |
| `WORKER_CONCURRENCY` | `2` | Journal and import workers per loop. |
| `MAX_UPLOAD_BYTES` | `5368709120` | Import upload limit (5 GiB). |
| `ENGINE_IMAGE` / `WEB_IMAGE` | GHCR `latest` | Pin image tags or use a fork. |

The engine also supports `MAX_CHUNK_BYTES`, `PST_TIMEOUT`, `ENABLE_API`,
`ENABLE_SMTP` and `ENABLE_WORKER` when run outside the supplied stack. See
`engine/internal/config/config.go`.

## First deployment verification

Watch service startup in Portainer logs or with Docker:

```bash
docker ps -a --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}'
```

Expected order:

1. `postgres` becomes healthy.
2. `db-init` exits `0`:
   ```text
   application database initialized role=archive_app database=archive role_created=true database_created=true
   ```
3. `engine` logs:
   ```text
   default tenant ready
   initial administrator created
   HTTP API listening
   SMTP journal receiver listening
   workers started
   ```
4. `web` becomes healthy.

Check the database role and database:

```bash
docker exec -it <postgres-container> psql -U postgres -d postgres -c '\du archive_app'
docker exec -it <postgres-container> psql -U postgres -d postgres -c '\l archive'
```

Open `https://<your-host>/` and sign in with the bootstrap administrator.

After the first successful login, remove `BOOTSTRAP_ADMIN_PASSWORD` from the
stack environment. The engine will continue using the existing user.

## TLS and reverse proxy

The web container serves plain HTTP on `3000`. Terminate TLS externally.

Example Caddy configuration:

```caddyfile
archive.example.com {
    reverse_proxy 127.0.0.1:3000
}
```

Example nginx location:

```nginx
location / {
    proxy_pass http://127.0.0.1:3000;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto https;
}
```

Production cookies are `Secure`, so the UI must be accessed over HTTPS. For a
temporary plain-HTTP test, set `SESSION_COOKIE_SECURE: "false"` in the `web`
service.

Web security headers are set by the app (`X-Frame-Options: DENY`,
`X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer`, private
no-store cache).

## SMTP and Office 365

Full instructions: [smtp-journaling.md](smtp-journaling.md).

Short version:

1. Publish `2525`, or add `"25:2525"` / `"587:2525"` to the engine port list.
2. Set `SMTP_ALLOWED_DOMAINS` to your Microsoft 365 domain.
3. Mount a trusted certificate for `SMTP_HOSTNAME`; the container UID is `10001`.
4. Configure an Exchange journal rule or outbound connector to the endpoint.
5. Send a test message and verify it appears in Archive search.

Do not introduce Postfix. If port 25 is already bound by another MTA on the VM,
move that MTA or put the archive receiver on a dedicated IP/port.

## Upgrades

1. Back up PostgreSQL, `archive_blobs`, and the master KEK.
2. Let CI publish the new images.
3. In Portainer, update the stack from Git and enable re-pull.
4. New engine images run embedded migrations at startup.
5. `db-init` runs again and is idempotent.
6. Verify `/healthz`, a search, attachment download and a test journal message.

Migrations are forward-only. Roll back application images only if you have a
compatible database backup; schema changes may not be reversible.

## Clean reinstall

Because `db-init` is idempotent, a password mismatch alone usually does not
require deleting data. For a full clean start:

```bash
docker volume ls --format '{{.Name}}' \
  | grep -E 'postgres_data|meili_data|valkey_data|archive_blobs|archive_spool'
```

Delete the Portainer stack, remove all matching volumes, then redeploy. This
destroys metadata, tenant KEKs, audit logs, blobs and search data. See
[troubleshooting.md](troubleshooting.md).

## Resource sizing

A small single-tenant deployment can start on 2 vCPU / 4 GiB RAM. Disk usage is
dominated by encrypted blobs and the PostgreSQL database; PST imports can
temporarily require additional spool space. Monitor:

- `archive_blobs` and `archive_spool` volume usage,
- PostgreSQL size and WAL growth,
- Meilisearch data size,
- import queue depth and failed job counts.

Backups and rotation procedures are in [operations.md](operations.md).
