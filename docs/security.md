# Security

## Reporting a vulnerability

Please report suspected vulnerabilities privately through GitHub Security
Advisories for this repository rather than opening a public issue. Include:

- affected component and version/commit,
- reproduction steps,
- impact assessment,
- any suggested fix.

There is no formal SLA for this MVP.

## Supported versions

Only the current `main` branch and the images built from it are supported.
Historical commits are provided for rollback convenience, not security support.

## Trust boundaries

```text
Internet / Microsoft 365
        │ SMTP (TLS)
        ▼
┌─────────────┐      ┌──────────────┐
│   engine    │─────▶│  PostgreSQL  │
│  (untrusted │      │  (trusted    │
│    mail)    │      │   metadata)  │
└──────┬──────┘      └──────────────┘
       │
       │ HTTP (internal network)
       ▼
┌─────────────┐
│     web     │◀──── browser session (HttpOnly cookie)
└─────────────┘
```

- SMTP payloads are attacker-controlled input. They are parsed, sanitized and
  size-limited, but should be treated as hostile.
- The engine API is intended to be reachable only by the web BFF on the internal
  Docker network.
- PostgreSQL is the source of truth for authorization, retention and audit.
- The browser never receives the engine JWT.

## Authentication and sessions

- Passwords are hashed with bcrypt (cost 12).
- Login returns a signed HS256 JWT containing user ID, tenant ID, session ID,
  role and expiry.
- A random session token is stored hashed in `sessions`; logout revokes the
  session row.
- The web BFF stores the JWT in a `HttpOnly`, `SameSite=Lax`, `Secure` cookie in
  production. JavaScript cannot read it.
- The BFF proxy strips incoming cookies and adds `Authorization: Bearer`.
- Login attempts are rate-limited in Valkey. If Valkey is unavailable, the
  limiter fails open.
- `SESSION_SECRET` signs JWTs and peppers session-token hashes. Changing it
  invalidates all sessions.

There is no MFA, SSO, password reset flow, account lockout or audit of failed
login for an unknown tenant slug. These are roadmap items.

## Authorization

Roles:

- `admin`: full access, imports, sources, retention, legal hold, delete.
- `auditor`: read-only archive/search/compliance views.

The API enforces roles server-side. Hiding UI controls is not the security
boundary.

Tenant identity is always derived from the JWT; no endpoint accepts a
client-supplied tenant ID.

## Tenant isolation and RLS

Tenant-scoped tables have:

- `tenant_id NOT NULL`,
- `ENABLE ROW LEVEL SECURITY`,
- `FORCE ROW LEVEL SECURITY`,
- a policy comparing `tenant_id` with
  `current_setting('app.current_tenant_id', true)`.

Every repository transaction calls:

```sql
SELECT set_config('app.current_tenant_id', $1, true);
```

The setting is transaction-local and resets at commit/rollback.

The engine connects as `archive_app`, a non-superuser role with
`NOBYPASSRLS`. The migration refuses to run as a superuser or `BYPASSRLS` role,
because those roles would silently bypass RLS.

The `tenants` registry is intentionally not tenant-scoped; it stores tenant
names, slugs, retention settings and wrapped tenant KEKs. Wrapped KEKs are
useless without the master KEK.

Workers enumerate tenants and set context per tenant before claiming queue rows.

## Envelope encryption

Key hierarchy:

```text
ARCHIVE_MASTER_KEK (32 bytes, environment/secrets)
        │ AES-256-GCM, AAD tenant:<id>
        ▼
per-tenant KEK (32 random bytes, wrapped in tenants.wrapped_kek)
        │ AES-256-GCM, AAD tenant_id:sha256
        ▼
per-blob DEK (32 random bytes, wrapped in blobs.wrapped_dek)
        │ AES-256-GCM, AAD tenant_id:sha256
        ▼
encrypted blob ciphertext in archive_blobs
```

- The master KEK is loaded from `ARCHIVE_MASTER_KEK` and validated as exactly
  32 bytes after base64 decoding.
- Each blob gets a fresh DEK. Wrapped DEK and file nonce are stored in
  PostgreSQL; ciphertext is stored on disk.
- AAD binds ciphertext to tenant and SHA-256. Moving or renaming ciphertext
  fails authentication.
- SHA-256 is computed over plaintext before encryption.
- Blob paths are tenant-scoped:
  `tenants/{tenant_id}/blobs/{sha256[:2]}/{sha256}`.
- Duplicate plaintext within a tenant reuses the existing ciphertext and wrapped
  DEK.

There is **no master-key rotation or re-wrap tool** in the MVP. Do not change
`ARCHIVE_MASTER_KEK` after data exists. Back it up separately from the database.

## Data at rest

Encrypted:

- raw `.eml` messages,
- journal envelopes,
- attachments.

Not encrypted by the application:

- message headers, plain-text and sanitized HTML bodies in PostgreSQL,
- search documents in Meilisearch,
- audit log details, subjects, email addresses.

Use full-disk/volume encryption for the VM and backups. Meilisearch and
PostgreSQL volumes are not application-encrypted.

## Integrity, retention and legal hold

- `messages` carries `raw_sha256`, `retain_until` and `legal_hold`.
- PostgreSQL triggers:
  - reject deletion while `legal_hold` is true,
  - reject deletion before `retain_until`,
  - block mutation of archived metadata,
  - prevent shortening `retain_until`.
- The API applies the same delete checks before issuing SQL.
- Legal-hold changes require admin and a reason when applying a hold.

These controls assume an honest application and a non-superuser database role.
A database superuser can disable triggers or bypass RLS; protect the superuser
credential.

## Audit log

`audit_logs` is append-only through triggers that reject `UPDATE` and `DELETE`.
It records:

- login success/failure,
- logout,
- message ingestion,
- import creation/completion/failure,
- search queries and filters,
- message views,
- attachment downloads,
- `.eml` export,
- legal-hold changes,
- source and retention changes,
- message deletion.

Audit rows include tenant, actor (when known), target, IP, user agent and JSON
detail. They are not encrypted by the application. Treat database backups and
log exports as sensitive.

## Browser and email rendering security

- Email HTML is sanitized server-side with bluemonday; scripts, event handlers
  and dangerous URLs are removed.
- The UI renders HTML inside an iframe with `sandbox=""`.
- The iframe document includes a restrictive CSP:
  - no scripts,
  - no remote images (privacy/tracking pixels blocked),
  - inline styles allowed.
- Attachment downloads use `Content-Disposition: attachment` and `nosniff`.
- The web app sets `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`
  and `Referrer-Policy: no-referrer`.
- Mutating BFF requests reject mismatched `Origin` headers; the session cookie
  is `SameSite=Lax`.
- The BFF proxy never exposes the engine bearer token to the browser.

## SMTP security

- STARTTLS is advertised when a certificate is configured; otherwise a
  self-signed dev certificate is generated.
- Authentication is PLAIN over TLS only.
- Sender policy can combine:
  - allowed sender domains,
  - trusted CIDRs (`SMTP_ALLOWED_IPS`),
  - SMTP AUTH.
- If no policy is configured, all mail is rejected.
- Message size is limited (`SMTP_MAX_MESSAGE_BYTES`), and the receiver caps
  recipients.
- Raw MIME is spooled before `250 OK`.
- Journal envelopes are preserved for provenance.

Sender-domain allowlisting alone does **not** authenticate the sender. For
production, use a trusted TLS certificate, restrict source IPs where possible,
and/or SMTP AUTH.

## Deployment hardening checklist

- [ ] Serve the UI only behind HTTPS; keep production cookies `Secure`.
- [ ] Do not publish the engine API, PostgreSQL, Meilisearch or Valkey.
- [ ] Make GHCR packages private or use a scoped registry credential.
- [ ] Use long random secrets; store them in a secret manager.
- [ ] Keep `POSTGRES_PASSWORD` and `ARCHIVE_DB_PASSWORD` distinct.
- [ ] Restrict SSH and Docker socket access.
- [ ] Enable host/volume encryption and encrypt backups.
- [ ] Set `SMTP_ALLOWED_DOMAINS`, optionally `SMTP_ALLOWED_IPS`/AUTH.
- [ ] Mount a publicly trusted SMTP certificate.
- [ ] Back up PostgreSQL, blobs and the master KEK separately.
- [ ] Monitor failed logins, queue failures and disk usage.
- [ ] Remove bootstrap admin credentials from the stack after first login.
- [ ] Keep Meilisearch/PostgreSQL patched; redeploy updated images regularly.
- [ ] Do not add Postfix or another MTA unless it is explicitly designed,
  secured and documented.

## Known limitations and risks

- No MFA/SSO, password reset, or account recovery.
- Login rate limiting is per IP+email and fails open if Valkey is down.
- No antivirus or attachment sandboxing.
- Single-tenant UI; multi-tenant administration is not exposed.
- Message bodies and search data are unencrypted at the application layer.
- No master-key rotation or tenant deletion workflow.
- No HA/failover; a single engine instance is assumed.
- SMTP sender-domain allowlists can be spoofed without IP/AUTH enforcement.
- Imported PST parsing relies on `readpst`; malformed files are quarantined but
  parser vulnerabilities are outside this application's control.
- Audit immutability is not protection against a malicious database superuser.
