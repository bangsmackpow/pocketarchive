# ADR 0001: Database provisioning via an idempotent `db-init` command

- Status: Accepted
- Date: 2026-11-26

## Context

The first production stack provisioned the application database with a SQL file
mounted into PostgreSQL:

```yaml
../postgres/init-app-role.sh:/docker-entrypoint-initdb.d/00-init-app-role.sh
```

It created `archive_app` and `archive`, and the engine connected with a URL built
by string interpolation:

```yaml
DATABASE_URL: postgres://archive_app:${POSTGRES_PASSWORD}@postgres:5432/archive
```

This caused several operational problems:

1. `/docker-entrypoint-initdb.d` only runs when the PostgreSQL data directory is
   empty. Reusing `postgres_data` or changing `POSTGRES_PASSWORD` after first
   boot left a stale role password. Redeploys did not repair it.
2. Relative host bind mounts are fragile in Portainer deployments and make the
   stack dependent on repository layout outside the Compose file.
3. Raw passwords in URLs break on characters such as `/`, `@` and `%`.
4. The engine and the PostgreSQL superuser shared one password.
5. The engine could start before the init script/role was ready, producing crash
   loops and confusing 28P01 errors.
6. The superuser password was available to the long-running engine process even
   though the engine only needed `archive_app`.

## Decision

1. Add an idempotent `engine db-init` command. It:
   - connects as the PostgreSQL superuser using `ADMIN_DATABASE_URL` or standard
     `PG*` variables,
   - creates the application role and database if missing,
   - always resets the application role password,
   - enforces `NOSUPERUSER NOBYPASSRLS`,
   - ensures the database is owned by the application role.

2. Run `db-init` as a one-shot Compose service before the engine. The engine
   depends on `service_completed_successfully`.

3. Remove the PostgreSQL init-script bind mount from production.

4. Pass database settings to the engine through `PGHOST`, `PGPORT`,
   `PGDATABASE`, `PGUSER`, `PGPASSWORD`, `PGSSLMODE`. Keep `DATABASE_URL` only as
   a local/development option.

5. Use two distinct database secrets:
   - `POSTGRES_PASSWORD` for the superuser, available only to `db-init`;
   - `ARCHIVE_DB_PASSWORD` for `archive_app`, available to `db-init` and the
     engine.

6. Use a TCP healthcheck for PostgreSQL (`pg_isready -h 127.0.0.1`) so it does
   not report healthy during the temporary Unix-socket initialization server.

7. Fail engine startup when no administrator exists and no bootstrap credentials
   are provided.

## Alternatives considered

### Keep the Postgres init script and rely on clean volumes

Rejected. It works only for a perfect first boot and makes every password or
volume issue a manual recovery.

### Have the engine run as a superuser and provision itself

Rejected. A long-running service with `BYPASSRLS`/superuser access defeats the
RLS model and increases blast radius.

### Use a custom PostgreSQL image with the SQL baked in

Rejected for the MVP. It adds another image to build and publish, and the
password still cannot be safely embedded. The application already has a Go
binary suitable for operational commands.

### Build the DSN with URL-encoded passwords

Partially acceptable, but `PG*` variables are the standard PostgreSQL client
mechanism and avoid encoding bugs entirely.

## Consequences

Positive:

- First boot is deterministic: Postgres healthy → db-init → engine → web.
- `ARCHIVE_DB_PASSWORD` rotation is self-healing.
- The engine never holds the superuser password.
- No host-path init-script dependency.
- Password characters no longer matter.
- Startup errors are explicit and actionable.
- The Go binary owns operational modes, matching the original architecture.

Negative:

- One extra short-lived container per stack.
- `POSTGRES_PASSWORD` still cannot be changed on an existing volume without a
  manual superuser reset.
- Compose requires support for `service_completed_successfully` (modern Docker
  Compose v2; current Portainer releases support it).
- The `db-init` image must be available before the engine starts, but both use
  the same image.

## Migration

For a deployment using the old stack:

1. Pull the current engine image containing `db-init`.
2. Update the Portainer stack from Git.
3. Add `ARCHIVE_DB_PASSWORD` to the stack environment.
4. Redeploy. `db-init` repairs the role even on an existing volume, provided
   `POSTGRES_PASSWORD` still matches the superuser.
5. If the superuser password is stale, reset it through the Postgres container’s
   local socket or delete `postgres_data` when data is disposable.
6. Remove any old Postfix service; the Go engine is the SMTP receiver.
