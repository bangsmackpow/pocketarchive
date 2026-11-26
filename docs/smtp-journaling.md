# SMTP journaling

## The engine is the SMTP server

PocketArchive does **not** use Postfix, Postgrey, Dovecot, or any external MTA.
The Go engine listens for SMTP directly and performs:

- STARTTLS,
- sender-domain/CIDR/SMTP AUTH policy,
- size limits,
- spool-before-ack,
- durable queue insertion,
- journal report unpacking,
- retries and quarantine.

If a Postfix container exists in your Portainer environment, it is not part of
this architecture. Remove it (and its volumes) before deploying PocketArchive.

## Listener behavior

| Setting | Default |
| --- | --- |
| Listen address | `SMTP_ADDR=:2525` inside the container |
| Published port | `2525:2525` in the supplied Compose stack |
| Hostname / EHLO | `SMTP_HOSTNAME` |
| Max message size | `SMTP_MAX_MESSAGE_BYTES` (100 MiB) |
| Max recipients | 100 |
| TLS | STARTTLS when a certificate is configured; otherwise self-signed dev cert |
| Required policy | At least one of sender domain, trusted CIDR, or SMTP AUTH |

The engine binds to `:2525` inside the container. To receive mail on the
standard SMTP port, add a mapping in `deploy/portainer/docker-compose.yml`:

```yaml
ports:
  - "2525:2525"
  - "25:2525"
  # - "587:2525"
```

Ensure nothing else (including Postfix or a host MTA) is bound to that host
port. Most cloud providers allow inbound port 25, but block outbound port 25 by
default.

## Acknowledgement semantics

For each accepted message:

1. Write the raw MIME payload to the spool under
   `/data/spool/journal/{tenant_id}/`.
2. Sync and close the file.
3. Insert a `journal_queue` row.
4. Return `250 OK`.

If spooling or queue insertion fails, the engine returns a temporary failure
(`451`) and removes the partial file so the sender retries.

Size violations return `552`; unauthorized senders return `550`.

## Sender policy

Policy is evaluated on `MAIL FROM`, remote IP and SMTP authentication.

- `SMTP_AUTH_USERNAME` / `SMTP_AUTH_PASSWORD`: enables SMTP AUTH with PLAIN over
  TLS. Authenticated clients are accepted without domain/IP restrictions.
- If AUTH is configured and a client is unauthenticated, it is accepted only if
  its source IP matches an entry in `SMTP_ALLOWED_IPS`.
- If AUTH is not configured:
  - if `SMTP_ALLOWED_DOMAINS` is set, the sender domain must match;
  - if `SMTP_ALLOWED_IPS` is set, the connection IP must match a CIDR;
  - both restrictions apply when both are set.
- If no domain list, CIDR list or AUTH credential is configured, all mail is
  rejected.

`SMTP_ALLOWED_DOMAINS` is a comma-separated list, e.g.:

```env
SMTP_ALLOWED_DOMAINS=yourcompany.com,subsidiary.com
SMTP_ALLOWED_IPS=203.0.113.0/24,198.51.100.10/32
```

Sender-domain allowlisting alone does not authenticate the sender. Use a
trusted certificate, restrict source IPs where Microsoft publishes stable
ranges for your tenant, or use SMTP AUTH.

## TLS

Production:

- Mount a publicly trusted certificate and key readable by container UID
  `10001`.
- Set:
  ```env
  SMTP_HOSTNAME=mail.yourcompany.com
  SMTP_TLS_CERT_FILE=/certs/fullchain.pem
  SMTP_TLS_KEY_FILE=/certs/privkey.pem
  ```
- Mount the files into the `engine` service (see commented examples in the
  Compose file).
- Expose port `25`/`587`/`2525` as appropriate and allow it through the firewall.

TLS 1.2+ is required. SMTP AUTH is rejected without TLS.

Development:

- With no certificate paths set, the engine generates a self-signed certificate
  for `SMTP_HOSTNAME`, `localhost`, `127.0.0.1` and `::1`.
- Office 365 will not trust this certificate. Mount a real one for production.

## Microsoft 365 journaling setup

1. Create a shared mailbox or mail contact for the journal target, for example
   `journal@mail.yourcompany.com`.
2. In Microsoft Purview / Exchange admin center, create a journal rule:
   - journal internal and external messages as required,
   - send journal reports to the journal target.
3. Configure an outbound connector or DNS path so Exchange can reach the VM on
   port `25` (or whichever port you publish). If your tenant policy only permits
   the smart-host connector, configure the connector with the archive hostname
   and port.
4. Set `SMTP_ALLOWED_DOMAINS` to your sender domain(s), for example
   `yourcompany.com`. Optionally add Microsoft 365 source CIDRs to
   `SMTP_ALLOWED_IPS`.
5. Mount a trusted certificate for `SMTP_HOSTNAME`.
6. Send an inbound and an outbound test message.
7. In the UI, **Sources** should show a recent journal message; **Archive
   search** should show the message with source `journal`.

Office 365 journal reports are commonly `multipart/report` envelopes containing
the original message as `message/rfc822`. The engine:

- preserves the complete outer journal envelope as an encrypted blob,
- archives the inner original message as the searchable message,
- falls back to the SMTP envelope recipient when the inner message has no `To`,
- records envelope metadata in the audit detail.

If a report has no inner `message/rfc822` part, the outer report itself is
archived rather than discarded.

## Queue, retries and quarantine

Workers claim `journal_queue` rows with `SKIP LOCKED`.

- Success: spool file removed, queue row marked `done`.
- Parse/store failure: retried after a backoff of approximately
  `attempts²` minutes.
- After 3 attempts: queue row marked `failed`; the spool file and a
  `.error.txt` note are moved to:
  ```text
  /data/spool/quarantine/{tenant_id}/
  ```
- Every archived message is also recorded in the audit log.

Inspect quarantine:

```bash
docker exec <engine> sh -c 'find /data/spool/quarantine -maxdepth 3 -type f | head'
docker logs --tail=200 <engine>
```

## Testing manually

With a trusted or ignored certificate, a simple SMTP test can use `swaks`:

```bash
swaks --server mail.yourcompany.com --port 2525 \
  --from postmaster@yourcompany.com \
  --to journal@mail.yourcompany.com \
  --header "Subject: PocketArchive SMTP test" \
  --body "hello archive" \
  --tls
```

For a self-signed development certificate, either disable verification in your
client or use `openssl s_client`/a Python `smtplib` script with
`ssl._create_unverified_context()`.

For a journal-format test, send a `multipart/report` message with a
`message/rfc822` part containing the original message. The test suite includes
a fixture for this format.

## Spool and disk behavior

- Spool files are written with mode `0640`.
- The spool volume must be writable by UID `10001`.
- A queued message occupies spool until processed or quarantined.
- Large journal bursts can fill `/data/spool`; monitor disk and backlog.
- If the spool is lost, queued metadata points at missing files and those queue
  rows fail/quarantine.

## Why not Postfix?

Postfix is a full MTA. Adding it in front of the engine creates:

- a second queue and retry model,
- a second TLS certificate configuration,
- another set of ports and volumes,
- another failure point between Office 365 and the archive,
- more ways for mail to be delayed or lost.

The Go receiver already spools before acknowledging, persists queue state in
PostgreSQL, retries, quarantines and preserves the journal envelope. For the
supported architecture, expose the engine directly.

If an external relay is unavoidable (for example, port 25 is owned by another
production MTA), treat it as a separate, explicitly secured system and document
its TLS, queue and monitoring configuration. It is not part of the PocketArchive
stack.
