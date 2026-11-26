# Troubleshooting

Start with:

```bash
docker ps -a --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}'
```

Then inspect the relevant service:

```bash
docker logs --tail=200 <db-init>
docker logs --tail=200 <postgres>
docker logs --tail=200 <engine>
docker logs --tail=200 <meilisearch>
docker logs --tail=200 <web>
```

## The engine logs an old configuration error

If the engine reports:

```text
missing or invalid DATABASE_URL, ARCHIVE_MASTER_KEK, SESSION_SECRET
```

you are running an **old engine image** and/or an old stack definition.

The current stack:

- does not use `DATABASE_URL`; it passes `PGHOST`, `PGDATABASE`, `PGUSER`,
  `PGPASSWORD`, `PGSSLMODE`,
- has a `db-init` service,
- requires `ARCHIVE_DB_PASSWORD`.

The new configuration error wording is:

```text
DATABASE_URL or PGHOST+PGDATABASE+PGUSER
```

Fixes:

1. Pull the current image:
   ```bash
   docker pull ghcr.io/bangsmackpow/pocketarchive-engine:latest
   ```
2. Confirm the image has `db-init`:
   ```bash
   docker run --rm --pull always \
     ghcr.io/bangsmackpow/pocketarchive-engine:latest db-init
   ```
   With no environment, the current image prints:
   ```text
   ARCHIVE_DB_PASSWORD is required for db-init
   ```
   An old image prints usage/unknown command.
3. In Portainer, update the stack from Git and enable re-pull. A restart alone
   does not pull a new image.
4. Pin the commit SHA if `latest` remains stale:
   ```env
   ENGINE_IMAGE=ghcr.io/bangsmackpow/pocketarchive-engine:1fddf3135ba71d868e027f4e7f917ea411c6b722
   WEB_IMAGE=ghcr.io/bangsmackpow/pocketarchive-web:1fddf3135ba71d868e027f4e7f917ea411c6b722
   ```
   Use the latest successful CI SHA from your fork/repository.
5. Remove orphaned old containers/stacks:
   ```bash
   docker ps -a | grep -i pocketarchive
   ```

## `password authentication failed for user "archive_app"` (28P01)

The application password in `PGPASSWORD` / `ARCHIVE_DB_PASSWORD` does not match
the role stored in PostgreSQL.

1. Check `db-init`:
   ```bash
   docker logs --tail=100 <db-init>
   ```
2. Redeploy the stack so `db-init` runs. It resets the role password.
3. Confirm the engine env names:
   ```bash
   docker inspect <engine> \
     --format '{{range .Config.Env}}{{println .}}{{end}}' | cut -d= -f1 | sort
   ```
   Expect `PGHOST`, `PGDATABASE`, `PGUSER`, `PGPASSWORD`, `PGSSLMODE`, not
   `DATABASE_URL`.

Manual fallback, if needed:

```bash
docker exec -it <postgres> psql -U postgres -d postgres
```

```text
\password archive_app
```

Enter `ARCHIVE_DB_PASSWORD`, `\q`, and restart the engine.

## `db-init` cannot authenticate as `postgres`

The PostgreSQL volume was initialized with a different `POSTGRES_PASSWORD`.
PostgreSQL does not update the superuser password when the environment changes.

Options:

- Reset the superuser password through the local socket:
  ```bash
  docker exec -it <postgres> psql -U postgres -d postgres
  ```
  ```text
  \password postgres
  ```
  Enter the new `POSTGRES_PASSWORD`, then `\q`, and redeploy.
- Or delete `postgres_data` for a clean install (destroys all data).

See [operations.md](operations.md) and [deployment.md](deployment.md) for
rotation details.

## `db-init` exits successfully but the engine does not start

Check:

- Engine logs for missing `ARCHIVE_MASTER_KEK`, `SESSION_SECRET`, or other
  required variables.
- `docker inspect <engine>` environment for empty values.
- Whether the engine image is current (see the old-image section above).
- Whether the `archive` database exists and `archive_app` owns it.

The engine starts only after `db-init` exits 0 and Postgres is healthy.

## `no administrator exists`

The engine intentionally exits instead of serving an unusable UI.

Set both:

```env
BOOTSTRAP_ADMIN_EMAIL=admin@example.com
BOOTSTRAP_ADMIN_PASSWORD=<at least 12 characters>
```

Then redeploy. If users already exist, the bootstrap values are not required
and are not used to reset an existing password.

To create an admin manually against an existing database:

```bash
docker exec <engine> /usr/local/bin/engine bootstrap \
  --email admin@example.com --password 'long-password'
```

## Web shows 502 / "engine unreachable"

The BFF cannot reach `http://engine:8080`.

- Check the engine container is running and healthy.
- Check it is on the same Docker network.
- Check `ENGINE_INTERNAL_URL` is `http://engine:8080` in the web service.
- Check engine logs for startup/migration/config errors.

## Login succeeds but the browser is redirected back to login

- Confirm the UI is served over HTTPS. Production cookies are `Secure`; plain
  HTTP will not persist the cookie.
- For plain-HTTP development, set `SESSION_COOKIE_SECURE: "false"` on `web`.
- Confirm reverse-proxy `Host`/forwarded headers do not rewrite the origin in a
  way that breaks the BFF.
- Check the browser console for failed `/api/proxy` requests.

## Search returns 502

Meilisearch is unavailable or the index is not ready.

```bash
docker logs --tail=100 <meilisearch>
docker exec <engine> sh -c 'wget -qO- http://meilisearch:7700/health'
```

Search comes back as soon as Meilisearch is healthy. Existing messages remain in
PostgreSQL; once search is available, the indexer catches up. Rebuild manually:

```bash
docker exec <engine> /usr/local/bin/engine reindex
```

## Search is empty after an import

Indexing is asynchronous. Wait a few seconds and refresh.

If it remains empty:

- Check the import job has `processed_items > 0`.
- Check engine logs for `index messages` errors.
- Check Meilisearch health and disk.
- Verify the search query/filters (sender/recipient filters are exact
  addresses).
- Run `engine reindex`.

## Stale search results after resetting the database

If `postgres_data` was recreated but `meili_data` was kept, old documents remain.
Remove the Meilisearch volume (or delete the index) and reindex, or perform a
full clean reinstall:

```bash
docker volume ls | grep meili_data
```

## SMTP connection refused

- Confirm the engine is running and listening on `2525` in the container.
- Confirm the published port mapping:
  ```bash
  docker port <engine>
  ```
- Confirm the host firewall/cloud security group allows the port.
- Confirm no Postfix or other MTA is binding the host port.
- If you mapped `25:2525`, verify nothing else owns host port 25.

## SMTP returns `550 sender is not authorized`

The sender domain/IP is not allowed.

- Set `SMTP_ALLOWED_DOMAINS` to the journal sender's domain.
- Or add the source CIDR to `SMTP_ALLOWED_IPS`.
- Or configure SMTP AUTH.
- If AUTH is enabled and no trusted CIDR matches, unauthenticated clients are
  rejected.

The receiver logs the policy warning at startup if no policy is configured.

## SMTP returns `451 temporary archive failure`

The engine could not spool or enqueue the message.

- Check free disk space for `archive_spool`.
- Check container UID `10001` write permissions on the volume.
- Check PostgreSQL availability and engine logs.
- The sender will retry; do not acknowledge the mail manually.

## SMTP returns `552 message exceeds maximum size`

Increase `SMTP_MAX_MESSAGE_BYTES` and redeploy, or configure the sender to split
large messages. The default is 100 MiB.

## Office 365 cannot establish TLS / certificate errors

- The default self-signed certificate is not trusted by Microsoft 365.
- Mount a publicly trusted certificate for `SMTP_HOSTNAME`.
- Ensure the private key is readable by UID `10001`.
- Verify the certificate hostname matches the connector target.
- Confirm port 25/587/2525 is permitted by the firewall.

## PST import fails with `readpst is not installed`

Use the official engine image; it compiles and installs `readpst`. If running the
engine from source outside the container, install libpst/pst-utils on the host.

Other PST failures:

- `readpst` timeout: increase `PST_TIMEOUT`.
- No `.eml` output: the PST may be corrupt/password-protected.
- Temp disk full: PST extraction requires temporary spool space.

## Import stuck in `queued` or `running`

- Confirm the engine is healthy and workers are enabled
  (`ENABLE_WORKER=true`).
- Check engine logs for claim/parse errors.
- Check `import_jobs.error_message` and `import_job_errors`.
- Check `archive_spool` permissions/space.
- Restart the engine if a worker appears wedged. There is no retry API in the
  MVP; a failed source must be re-uploaded.

## Volume permission errors

The engine runs as UID/GID `10001`. Named Docker volumes initialized from the
image inherit ownership for `/data/blobs` and `/data/spool`. If you replaced
them or mounted host directories, fix ownership:

```bash
docker run --rm -v <stack>_archive_blobs:/data alpine:3.20 \
  chown -R 10001:10001 /data
```

Do the same for the spool volume.

## Disk full

Check:

```bash
docker system df
docker exec <engine> df -h /data/blobs /data/spool
```

Large contributors:

- encrypted blobs (expected archive growth),
- PST upload spool files during import,
- PostgreSQL WAL/data,
- Meilisearch index,
- quarantined journal/import files.

Do not delete rows from `blobs` manually; references and ciphertext would break.
There is no blob garbage collector in the MVP.

## Portainer stack keeps using old compose or images

- Confirm the stack Git reference is `main` and the path is
  `deploy/portainer/docker-compose.yml`.
- Use **Update the stack** / **Pull and redeploy**, not just Restart.
- Enable re-pull image.
- Check for multiple old stacks/containers with similar names.
- Pin image tags to the CI commit SHA.

## Full clean reset

When all data is disposable:

```bash
docker volume ls --format '{{.Name}}' \
  | grep -E 'postgres_data|meili_data|valkey_data|archive_blobs|archive_spool|postfix'
```

Delete the Portainer stack, remove the volumes, remove old Postfix containers
and volumes, then redeploy. See [operations.md](operations.md).

## Collecting diagnostics

Useful non-secret information to collect:

```bash
docker ps -a --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}'
docker image inspect ghcr.io/<owner>/pocketarchive-engine:latest \
  --format '{{.Created}} {{.Id}}'
docker logs --tail=200 <db-init>
docker logs --tail=200 <engine>
docker inspect <engine> --format '{{range .Config.Env}}{{println .}}{{end}}' \
  | cut -d= -f1 | sort
```

Do not post database passwords, `ARCHIVE_MASTER_KEK`, `SESSION_SECRET`, SMTP
credentials, or raw archived email contents publicly.
