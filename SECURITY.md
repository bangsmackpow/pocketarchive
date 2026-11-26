# Security policy

## Reporting a vulnerability

Report suspected vulnerabilities privately through GitHub Security Advisories for
`bangsmackpow/pocketarchive`. Do not open a public issue for an exploitable
problem.

Include:

- affected component and commit/version,
- reproduction steps or proof of concept,
- impact and any known mitigations,
- whether the issue affects deployed data, credentials or availability.

There is no formal SLA; this is an MVP project.

## Supported versions

Only the current `main` branch and images built from it are supported.

## High-level model

- PostgreSQL is the source of truth and enforces tenant row-level security.
- The engine connects as a non-superuser `archive_app` role with `NOBYPASSRLS`.
- The `postgres` and one-shot `db-init` services use the PostgreSQL superuser password; the long-running engine does not.
- Blobs are encrypted with per-tenant, per-blob AES-256-GCM keys derived through
  an envelope hierarchy rooted in `ARCHIVE_MASTER_KEK`.
- Audit logs are append-only through PostgreSQL triggers.
- The browser stores the engine JWT only in an HttpOnly, SameSite cookie.
- Email HTML is sanitized and rendered in a sandboxed iframe with a strict CSP.

Read [docs/security.md](docs/security.md) for the full threat model, known
limitations and deployment hardening checklist.

## Secrets that must never be committed or shared

- `ARCHIVE_MASTER_KEK`
- `SESSION_SECRET`
- `POSTGRES_PASSWORD`
- `ARCHIVE_DB_PASSWORD`
- `MEILI_MASTER_KEY`
- SMTP AUTH credentials and TLS private keys
- database dumps, blob archives, raw email, audit exports

## Deployment expectations

- Serve the UI over HTTPS; production cookies are `Secure`.
- Do not publish the engine API, PostgreSQL, Meilisearch or Valkey.
- Use long random secrets and store them in a secret manager.
- Back up PostgreSQL, blobs and the master KEK separately.
- Keep host/volume encryption enabled for PostgreSQL and Meilisearch data.
- Configure an SMTP sender policy and trusted certificate.
- Keep images patched and monitor failed logins, queue failures and disk usage.

## Out of scope

The following are known limitations of the MVP, documented in
[docs/security.md](docs/security.md):

- no MFA/SSO,
- no antivirus/attachment sandbox,
- no application-level encryption of message bodies/search data,
- no master-key rotation,
- no HA/failover,
- no protection against a malicious database superuser.
