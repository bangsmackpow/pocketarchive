# Internal API

The Go engine exposes a JSON API under `/api/v1`. It is designed to be reached
by the TanStack Start BFF over the internal Docker network, not directly from
the browser. The BFF adds `Authorization: Bearer <JWT>` from the HttpOnly
session cookie.

All API responses are `Cache-Control: no-store`. The engine binds `:8080` inside
the container; Compose does not publish it.

## Authentication

### `POST /api/v1/auth/login`

```json
{
  "email": "admin@example.com",
  "password": "correct horse battery",
  "tenant_slug": "default"
}
```

`tenant_slug` is optional and defaults to `DEFAULT_TENANT_SLUG`.

Success `200`:

```json
{
  "token": "<jwt>",
  "expires_at": "2026-11-26T16:00:00Z",
  "user": { "id": "<uuid>", "email": "admin@example.com", "role": "admin" },
  "tenant": {
    "id": "<uuid>",
    "slug": "default",
    "name": "Default Archive",
    "retention_days": 2557
  }
}
```

Failures:

- `400` missing email/password
- `401 invalid_credentials`
- `429 rate_limited`

### `GET /api/v1/auth/me`

Returns `{ "user": ..., "tenant": ... }` for the current token.

### `POST /api/v1/auth/logout`

Revokes the current session and audits the logout.

## Authentication requirements

Every route below except login and health requires:

```http
Authorization: Bearer <jwt>
```

The JWT is HS256 and includes `tid` (tenant), `sid` (session), `role`, `sub`
(user), and `exp`. The middleware additionally verifies that the session row is
active and not revoked.

Roles:

- `admin`: full access.
- `auditor`: read-only endpoints; admin-only mutations return `403 forbidden`.

## Error format

```json
{
  "error": {
    "code": "not_found",
    "message": "resource not found"
  }
}
```

Common codes: `unauthorized`, `forbidden`, `invalid_request`, `invalid_json`,
`invalid_filter`, `invalid_date`, `not_found`, `conflict`, `legal_hold`,
`retention`, `rate_limited`, `search_failed`, `search_unavailable`,
`decrypt_failed`, `engine_unreachable`, `internal`.

## Tenant and setup

### `GET /api/v1/tenant`

Returns the current tenant and retention setting.

### `PUT /api/v1/tenant/settings`

Admin only.

```json
{ "retention_days": 3650 }
```

`retention_days` must be between 1 and 36500. Applies to newly ingested
messages; existing messages keep their immutable `retain_until`.

### `GET /api/v1/setup`

Returns SMTP display information:

```json
{
  "smtp": {
    "hostname": "mail.example.com",
    "port": 2525,
    "allowed_domains": ["example.com"],
    "allowed_ips": [],
    "auth_enabled": false,
    "tls_mode": "mounted-certificate",
    "max_message_bytes": 104857600
  },
  "tenant": { "slug": "default", "name": "Default Archive" },
  "journaling": {
    "provider": "microsoft365",
    "address": "journal@mail.example.com",
    "transport": "SMTP with STARTTLS"
  }
}
```

### `GET /api/v1/stats`

Returns:

```json
{
  "total_messages": 1200,
  "messages_24h": 42,
  "journal_messages_24h": 40,
  "legal_hold_count": 3,
  "attachments": 87,
  "storage_bytes": 123456789,
  "queue_backlog": 0,
  "queue_failed": 0,
  "last_journal_at": "2026-11-26T12:00:00Z"
}
```

## Ingestion sources

### `GET /api/v1/sources`

```json
{ "sources": [ { "id": "...", "name": "Office 365 Journal", "kind": "journal", "config": {}, "enabled": true } ] }
```

### `POST /api/v1/sources` — admin

```json
{
  "name": "Office 365 Journal",
  "kind": "journal",
  "enabled": true,
  "config": { "provider": "microsoft365" }
}
```

`kind` is `journal`, `pst`, `mbox` or `eml`.

### `PATCH /api/v1/sources/{id}` — admin

```json
{ "name": "Office 365 Journal", "enabled": true, "config": {} }
```

### `DELETE /api/v1/sources/{id}` — admin

The default journal source is seeded on first boot if none exists.

## Imports

Import creation/upload/completion is admin-only.

### `POST /api/v1/imports`

```json
{ "filename": "archive.pst", "size_bytes": 1073741824 }
```

Only `.pst` and `.mbox` are accepted. `201` returns an import job in
`awaiting_upload` state. Chunks are assigned sequential indexes starting at 0.

### `PUT /api/v1/imports/{id}/chunks/{index}`

Raw request body, `Content-Type: application/octet-stream`. Maximum chunk size
is `MAX_CHUNK_BYTES` (default 32 MiB); the UI uses 8 MiB chunks.

Success:

```json
{ "id": "...", "bytes_received": 8388608, "chunks_received": 1 }
```

`409` if indexes are out of order or the declared/max size is exceeded.

### `POST /api/v1/imports/{id}/complete`

Queues the job for a worker. Requires at least one non-empty chunk.

### `GET /api/v1/imports?limit=50`

```json
{ "jobs": [ { "id": "...", "status": "running", "total_items": 100, "processed_items": 40, "duplicate_items": 1, "error_items": 0 } ] }
```

Job statuses: `awaiting_upload`, `queued`, `running`, `completed`, `failed`,
`cancelled`.

### `GET /api/v1/imports/{id}`

Returns one job, including timestamps and error message.

### `GET /api/v1/imports/{id}/errors?limit=100`

```json
{ "errors": [ { "id": 1, "item_ref": "message-12", "error": "parse failure", "created_at": "..." } ] }
```

## Search

### `GET /api/v1/search`

Query parameters:

| Param | Meaning |
| --- | --- |
| `q` | Full-text query: subject, body, sender, recipient, attachment names. |
| `from` | Exact sender address (case-insensitive). |
| `to` | Exact recipient address (array membership). |
| `date_from` | Inclusive start: `YYYY-MM-DD` or RFC3339. |
| `date_to` | Inclusive end; date-only means end of that UTC day. |
| `has_attachment` | `true` / `false`. |
| `source` | Comma-separated `journal,pst,mbox,eml`. |
| `legal_hold` | `true` / `false`. |
| `page` | 1-based page; default 1. |
| `limit` | Page size; default 25, maximum 100. |

Response:

```json
{
  "hits": [
    {
      "id": "<uuid>",
      "subject": "Quarterly report",
      "from_addr": "alice@example.com",
      "from_name": "Alice",
      "to_addrs": ["bob@example.com"],
      "cc_addrs": [],
      "sent_at": "2024-01-01T10:00:00Z",
      "received_at": "2026-11-26T12:00:00Z",
      "has_attachments": true,
      "attachment_count": 1,
      "attachment_filenames": ["report.pdf"],
      "source_type": "journal",
      "legal_hold": false,
      "size_bytes": 12345,
      "message_id": "<msg@example.com>"
    }
  ],
  "total": 1,
  "page": 1,
  "limit": 25,
  "took_ms": 4,
  "fallback": false
}
```

Search is tenant-filtered by the JWT. If Meilisearch is unavailable, the API
returns `502 search_failed`.

## Messages

### `GET /api/v1/messages/{id}`

Returns the full metadata, sanitized HTML, plain text, headers and attachments:

```json
{
  "id": "...",
  "tenant_id": "...",
  "source_type": "journal",
  "message_id": "<msg@example.com>",
  "subject": "Quarterly report",
  "from_addr": "alice@example.com",
  "from_name": "Alice",
  "to_addrs": ["bob@example.com"],
  "cc_addrs": [],
  "bcc_addrs": [],
  "sent_at": "...",
  "received_at": "...",
  "size_bytes": 12345,
  "raw_sha256": "...",
  "raw_blob_sha256": "...",
  "journal_envelope_sha256": "...",
  "headers": { "Subject": ["Quarterly report"] },
  "body_text": "...",
  "body_html": "<p>...</p>",
  "has_attachments": true,
  "attachment_count": 1,
  "retain_until": "2033-11-26T12:00:00Z",
  "legal_hold": false,
  "created_at": "...",
  "attachments": [
    {
      "id": "...",
      "message_id": "...",
      "filename": "report.pdf",
      "content_type": "application/pdf",
      "size_bytes": 100,
      "sha256": "...",
      "created_at": "..."
    }
  ]
}
```

Viewing a message is audited.

### `GET /api/v1/messages/{id}/export`

Returns the decrypted original `.eml` with `Content-Disposition: attachment`.
Audited as `message.export`.

### `GET /api/v1/messages/{id}/attachments/{attachmentID}`

Returns the decrypted attachment with its content type and an attachment
disposition. Audited as `attachment.download`.

### `DELETE /api/v1/messages/{id}` — admin

Returns `409 legal_hold` if under hold, `409 retention` if before
`retain_until`, otherwise deletes the message and audits the action. Blobs are
not garbage-collected.

### `POST /api/v1/messages/{id}/legal-hold` — admin

```json
{ "hold": true, "reason": "Litigation matter 2026-014" }
```

A reason is required when `hold` is true. Response:

```json
{ "ok": true, "legal_hold": true }
```

Audited as `message.legal_hold.enabled` or
`message.legal_hold.released`. Search is updated asynchronously; if Meilisearch
fails, the message is marked for reindexing.

## Audit log

### `GET /api/v1/audit-logs`

Query parameters:

| Param | Meaning |
| --- | --- |
| `action` | Case-insensitive substring match, e.g. `legal_hold`. |
| `actor` | Case-insensitive substring match on actor email. |
| `date_from`, `date_to` | `YYYY-MM-DD` or RFC3339. |
| `limit` | Default 50, maximum 200. |
| `offset` | Default 0. |

Response:

```json
{
  "logs": [
    {
      "id": 123,
      "actor_user_id": "...",
      "actor_email": "admin@example.com",
      "action": "message.view",
      "target_type": "message",
      "target_id": "...",
      "ip": "203.0.113.10",
      "user_agent": "...",
      "detail": { "subject": "Quarterly report" },
      "created_at": "..."
    }
  ],
  "limit": 50,
  "offset": 0
}
```

## Health

### `GET /healthz`

Liveness:

```json
{ "status": "ok", "service": "pocketarchive-engine" }
```

### `GET /readyz`

Checks PostgreSQL and Meilisearch; reports Valkey as degraded without failing:

```json
{
  "status": "ok",
  "checks": { "database": "ok", "search": "ok", "valkey": "ok" }
}
```

Returns `503` if the database or search check fails.

## BFF proxy

The browser calls:

```text
/api/proxy/<path>  →  http://engine:8080/api/v1/<path>
```

The BFF reads the `pa_session` cookie, adds `Authorization: Bearer`, forwards
method/body/query, strips hop-by-hop headers and returns the engine response.
The engine token never reaches client JavaScript.
