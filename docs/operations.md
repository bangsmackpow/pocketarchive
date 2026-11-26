# Operations runbook

This guide assumes the Portainer stack from `deploy/portainer/docker-compose.yml`
and container names similar to `<stack>-engine-1`, `<stack>-postgres-1`, etc.
Use the names shown in Portainer or `docker ps`.

## Health checks

| Check | Command |
| --- | --- |
| Engine liveness | `curl -fsS http://127.0.0.1:8080/healthz` (inside container/network) |
| Engine readiness | `curl -fsS http://127.0.0.1:8080/readyz` |
| Web health | `curl -fsS http://127.0.0.1:3000/api/health` |
| Postgres | `docker exec <postgres> pg_isready -h 127.0.0.1 -U postgres -d postgres` |
| Meilisearch | `curl -fsS http://<meili>:7700/health` |

`readyz` checks PostgreSQL and Meilisearch; Valkey is reported as degraded but
does not fail readiness. The Docker healthcheck for the engine uses `/healthz`
because database migrations and bootstrap already ran at startup.

## Logs

```bash
docker logs --tail=200 -f <engine>
docker logs --tail=200 -f <db-init>
docker logs --tail=200 -f <postgres>
docker logs --tail=200 -f <meilisearch>
docker logs --tail=200 -f <web>
```

Useful engine log events:

- `default tenant ready`
- `initial administrator created`
- `HTTP API listening`
- `SMTP journal receiver listening`
- `archived message`
- `import complete` / `import failed`
- `journal message quarantined`
- `database authentication failed ...`

## Volumes

| Volume | Contents | Rebuildable |
| --- | --- | --- |
| `postgres_data` | Source-of-truth metadata, keys, audit | No |
| `archive_blobs` | Encrypted raw messages, envelopes, attachments | No |
| `archive_spool` | In-flight SMTP and import upload files, quarantine | Partially; queue rows reference paths |
| `meili_data` | Search index | Yes, reindex from PostgreSQL |
| `valkey_data` | Rate-limit counters | Yes |

## Backup

Back up all three of:

1. PostgreSQL database dump
2. `archive_blobs` volume
3. `ARCHIVE_MASTER_KEK` (and other deployment secrets) from a secure secret store

`archive_spool` may also be backed up if in-flight journal/import data must be
preserved. Drain queues first if possible.

Recommended consistent procedure:

1. Stop the `engine` and `web` containers in Portainer (or scale them to 0).
2. Dump PostgreSQL:

   ```bash
   docker exec <postgres> pg_dump -U postgres -Fc archive > archive-$(date +%F).dump
   ```

3. Archive blobs:

   ```bash
   docker run --rm \
     -v <stack>_archive_blobs:/data:ro \
     -v "$PWD":/backup \
     alpine:3.20 tar -C /data -czf /backup/archive-blobs-$(date +%F).tgz .
   ```

4. Store the dump, blob archive and secrets in separate secure storage.
5. Restart the stack.

A `pg_dump` taken while the engine is running is transactionally consistent, but
stopping the engine guarantees that no concurrent blob write spans the database
and volume snapshots.

Do not back up Meilisearch as the only search copy. PostgreSQL is authoritative;
`meili_data` can be recreated.

## Restore

1. Deploy the stack so `db-init` creates the `archive_app` role and empty
   `archive` database.
2. Stop `engine` and `web`.
3. Restore PostgreSQL:

   ```bash
   docker exec -i <postgres> pg_restore -U postgres -d archive --clean --if-exists < archive.dump
   ```

4. Restore the blob volume:

   ```bash
   docker run --rm \
     -v <stack>_archive_blobs:/data \
     -v "$PWD":/backup \
     alpine:3.20 sh -c 'rm -rf /data/* && tar -C /data -xzf /backup/archive-blobs-YYYY-MM-DD.tgz'
   ```

5. Ensure `ARCHIVE_MASTER_KEK` is exactly the key used when the data was
   archived. A different key makes existing blobs undecryptable.
6. Start the engine and web containers.
7. Rebuild search:

   ```bash
   docker exec <engine> /usr/local/bin/engine reindex
   ```

8. Verify login, a message preview, an attachment download, a `.eml` export and
   audit logs.

Restoring only PostgreSQL without blobs leaves metadata pointing at missing
ciphertext. Restoring only blobs without PostgreSQL loses the wrapped per-blob
DEKs.

## Upgrade

1. Take a database + blob backup.
2. Let CI publish new images from `main`.
3. In Portainer, update the stack from Git with re-pull enabled.
4. `db-init` runs and is idempotent.
5. The engine applies embedded migrations at startup.
6. Check `/healthz`, `/readyz`, search, attachment download, export, and send a
   test journal message.

Migrations are forward-only. If you need to roll back application images, keep a
pre-upgrade database backup; the new schema may not be compatible with old code.

To pin a release, set:

```env
ENGINE_IMAGE=ghcr.io/<owner>/pocketarchive-engine:<sha>
WEB_IMAGE=ghcr.io/<owner>/pocketarchive-web:<sha>
```

## Password and secret rotation

### `ARCHIVE_DB_PASSWORD` (engine database role)

1. Generate a new hex password.
2. Update the stack environment.
3. Redeploy.

`db-init` resets `archive_app` to the new value before the engine starts. No
manual SQL is required.

### `POSTGRES_PASSWORD` (PostgreSQL superuser)

PostgreSQL does not update its superuser password when the environment changes
on an existing volume. To change it:

1. Update the stack environment.
2. Open a shell in the Postgres container:

   ```bash
   docker exec -it <postgres> psql -U postgres -d postgres
   ```

3. Run `\password postgres`, enter the new value twice, then `\q`.
4. Redeploy so `db-init` and the rest of the stack see the new value.

If the volume has no data to preserve, deleting `postgres_data` and redeploying
is simpler.

### `SESSION_SECRET`

Change the value and redeploy. All existing sessions are invalidated. Users must
log in again.

### `MEILI_MASTER_KEY`

Change it in the stack environment and redeploy Meilisearch and the engine
together. Existing indexed data remains; the engine must use the same key.

### `ARCHIVE_MASTER_KEK`

**Do not change this on an existing archive.** There is no key-rotation or
re-wrap tool in the MVP. The master KEK decrypts tenant KEKs stored in
PostgreSQL. Losing or changing it renders all stored blobs unreadable.

## Search operations

`engine reindex` iterates all tenants and messages and overwrites their search
documents. It does **not** delete unrelated stale document IDs. To rebuild from
scratch:

1. Delete the Meilisearch index (or remove `meili_data`) while the engine is
   stopped.
2. Start Meilisearch and the engine; the engine recreates index settings.
3. Run `engine reindex`.

```bash
docker exec <engine> /usr/local/bin/engine reindex
```

## Quarantine and stuck jobs

Quarantined journal files live under:

```text
/data/spool/quarantine/{tenant_id}/{uuid}.eml
```

Each has a sibling `.error.txt`. Inspect with:

```bash
docker exec <engine> sh -c 'find /data/spool/quarantine -maxdepth 3 -type f | head'
```

Import errors are visible in the UI and stored in `import_job_errors`. A failed
import keeps its uploaded spool file; there is no retry API in the MVP. Fix the
source file and create a new import.

If jobs remain queued:

1. Confirm the engine is running and healthy.
2. Check engine logs for claim/parse errors.
3. Check `journal_queue` / `import_jobs` status and `last_error` columns.
4. Confirm the `archive_spool` volume is writable by UID 10001.

## Full reset

On a fresh development or test deployment:

```bash
docker volume ls --format '{{.Name}}' \
  | grep -E 'postgres_data|meili_data|valkey_data|archive_blobs|archive_spool'
```

Delete the Portainer stack, remove those volumes and any Postfix volumes left by
an old stack, then redeploy. This destroys all archive data.

## Monitoring recommendations

Alert on:

- Engine `/healthz` or `/readyz` failing.
- Web `/api/health` failing.
- `journal_queue` backlog or failed count growing.
- `import_jobs` stuck in `running`/`queued`.
- PostgreSQL disk usage, connection saturation and WAL growth.
- `archive_blobs`/`archive_spool` volume usage.
- Meilisearch disk usage and task failures.
- Repeated `password authentication failed` or SMTP auth failures.

The `GET /api/v1/stats` endpoint (via the authenticated UI/BFF) exposes total
messages, 24-hour counts, legal-hold count, attachment count, blob storage size,
queue backlog and queue failures.

## Disaster recovery assumptions

- Losing `postgres_data` without a backup loses message metadata, audit logs and
  wrapped tenant KEKs.
- Losing `archive_blobs` without a backup loses original messages/attachments.
- Losing `ARCHIVE_MASTER_KEK` makes blobs unrecoverable even with a PostgreSQL
  backup.
- Meilisearch and Valkey can be rebuilt.
- A single-node stack has no automatic failover; recovery is restore into a new
  stack.
