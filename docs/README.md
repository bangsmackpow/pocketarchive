# PocketArchive documentation

Start with the [README](../README.md) for the project overview and quick start.

| Document | Contents |
| --- | --- |
| [architecture.md](architecture.md) | Components, data flows, schema, startup lifecycle, failure behavior, scaling |
| [deployment.md](deployment.md) | VM prerequisites, Portainer Git stack, secrets, environment reference, TLS, upgrades |
| [operations.md](operations.md) | Health, logs, volumes, backup/restore, rotation, reindex, reset, disaster recovery |
| [security.md](security.md) | Trust boundaries, RLS, encryption, auth, audit, hardening, known gaps |
| [api.md](api.md) | Internal REST API, endpoint reference, request/response examples |
| [smtp-journaling.md](smtp-journaling.md) | SMTP receiver, Office 365 journaling, TLS, policy, retries, quarantine |
| [troubleshooting.md](troubleshooting.md) | Startup, database, image, SMTP, search, import and UI problems |
| [development.md](development.md) | Local workflow, tests, migrations, adding API/UI features |
| [adr/0001-database-provisioning.md](adr/0001-database-provisioning.md) | Why database provisioning is a `db-init` job |

Repository-level policies:

- [CONTRIBUTING.md](../CONTRIBUTING.md)
- [SECURITY.md](../SECURITY.md)
