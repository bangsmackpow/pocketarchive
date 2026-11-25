-- PocketArchive initial schema.
--
-- Security invariants:
--   * Every tenant-scoped table carries tenant_id and is protected by FORCE RLS.
--   * The application must connect as a non-superuser role without BYPASSRLS.
--   * audit_logs is append-only.
--   * messages cannot be deleted while under legal hold or before retain_until.

DO $$
DECLARE
  is_super boolean;
  bypasses boolean;
BEGIN
  SELECT rolsuper, rolbypassrls INTO is_super, bypasses
  FROM pg_roles WHERE rolname = current_user;

  IF COALESCE(is_super, false) OR COALESCE(bypasses, false) THEN
    RAISE EXCEPTION
      'DATABASE_URL must use a non-superuser role without BYPASSRLS; row-level security would otherwise be bypassed';
  END IF;
END
$$;

CREATE TABLE tenants (
  id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  slug            text NOT NULL UNIQUE,
  name            text NOT NULL,
  wrapped_kek     bytea NOT NULL,
  kek_version     integer NOT NULL DEFAULT 1,
  retention_days  integer NOT NULL DEFAULT 2557 CHECK (retention_days BETWEEN 1 AND 36500),
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE users (
  id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id      uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  email          text NOT NULL,
  password_hash  text NOT NULL,
  role           text NOT NULL DEFAULT 'admin' CHECK (role IN ('admin', 'auditor')),
  is_active      boolean NOT NULL DEFAULT true,
  created_at     timestamptz NOT NULL DEFAULT now(),
  last_login_at  timestamptz
);
CREATE UNIQUE INDEX users_tenant_email_uq ON users (tenant_id, lower(email));

CREATE TABLE sessions (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id     uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  user_id       uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token_hash    bytea NOT NULL UNIQUE,
  expires_at    timestamptz NOT NULL,
  revoked_at    timestamptz,
  ip            text,
  user_agent    text,
  created_at    timestamptz NOT NULL DEFAULT now(),
  last_seen_at  timestamptz
);
CREATE INDEX sessions_tenant_user_idx ON sessions (tenant_id, user_id);

CREATE TABLE ingestion_sources (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id   uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  name        text NOT NULL,
  kind        text NOT NULL CHECK (kind IN ('journal', 'pst', 'mbox', 'eml')),
  config      jsonb NOT NULL DEFAULT '{}'::jsonb,
  enabled     boolean NOT NULL DEFAULT true,
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ingestion_sources_tenant_idx ON ingestion_sources (tenant_id, created_at DESC);

CREATE TABLE import_jobs (
  id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id          uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  kind               text NOT NULL CHECK (kind IN ('pst', 'mbox')),
  status             text NOT NULL DEFAULT 'awaiting_upload'
                       CHECK (status IN ('awaiting_upload', 'queued', 'running', 'completed', 'failed', 'cancelled')),
  original_filename  text NOT NULL,
  spool_path         text NOT NULL,
  size_bytes         bigint NOT NULL DEFAULT 0,
  bytes_received     bigint NOT NULL DEFAULT 0,
  chunks_received    integer NOT NULL DEFAULT 0,
  total_items        integer NOT NULL DEFAULT 0,
  processed_items    integer NOT NULL DEFAULT 0,
  duplicate_items    integer NOT NULL DEFAULT 0,
  error_items        integer NOT NULL DEFAULT 0,
  error_message      text,
  created_by         uuid REFERENCES users(id) ON DELETE SET NULL,
  created_at         timestamptz NOT NULL DEFAULT now(),
  started_at         timestamptz,
  completed_at       timestamptz,
  updated_at         timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX import_jobs_tenant_status_idx ON import_jobs (tenant_id, status, created_at DESC);

CREATE TABLE import_job_errors (
  id          bigserial PRIMARY KEY,
  tenant_id   uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  job_id      uuid NOT NULL REFERENCES import_jobs(id) ON DELETE CASCADE,
  item_ref    text,
  error       text NOT NULL,
  created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX import_job_errors_job_idx ON import_job_errors (job_id, id);

CREATE TABLE journal_queue (
  id              bigserial PRIMARY KEY,
  tenant_id       uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  spool_path      text NOT NULL,
  envelope_from   text,
  remote_addr     text,
  status          text NOT NULL DEFAULT 'queued'
                    CHECK (status IN ('queued', 'processing', 'done', 'failed')),
  attempts        integer NOT NULL DEFAULT 0,
  next_attempt_at timestamptz NOT NULL DEFAULT now(),
  last_error      text,
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX journal_queue_claim_idx ON journal_queue (tenant_id, status, next_attempt_at);

CREATE TABLE blobs (
  tenant_id     uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  sha256        text NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
  size_bytes    bigint NOT NULL,
  wrapped_dek   bytea NOT NULL,
  file_nonce    bytea NOT NULL,
  created_at    timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, sha256)
);

CREATE TABLE messages (
  id                       uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id                uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  import_job_id            uuid REFERENCES import_jobs(id) ON DELETE SET NULL,
  source_id                uuid REFERENCES ingestion_sources(id) ON DELETE SET NULL,
  source_type              text NOT NULL CHECK (source_type IN ('journal', 'pst', 'mbox', 'eml')),
  message_id               text,
  subject                  text NOT NULL DEFAULT '',
  from_addr                text NOT NULL DEFAULT '',
  from_name                text NOT NULL DEFAULT '',
  to_addrs                 text[] NOT NULL DEFAULT '{}',
  cc_addrs                 text[] NOT NULL DEFAULT '{}',
  bcc_addrs                text[] NOT NULL DEFAULT '{}',
  sent_at                  timestamptz NOT NULL DEFAULT now(),
  received_at              timestamptz NOT NULL DEFAULT now(),
  size_bytes               bigint NOT NULL,
  raw_sha256               text NOT NULL CHECK (raw_sha256 ~ '^[0-9a-f]{64}$'),
  raw_blob_sha256          text NOT NULL CHECK (raw_blob_sha256 ~ '^[0-9a-f]{64}$'),
  journal_envelope_sha256  text CHECK (journal_envelope_sha256 IS NULL OR journal_envelope_sha256 ~ '^[0-9a-f]{64}$'),
  headers                  jsonb NOT NULL DEFAULT '{}'::jsonb,
  body_text                text NOT NULL DEFAULT '',
  body_html                text NOT NULL DEFAULT '',
  has_attachments          boolean NOT NULL DEFAULT false,
  attachment_count         integer NOT NULL DEFAULT 0,
  attachment_filenames     text[] NOT NULL DEFAULT '{}',
  retain_until             timestamptz NOT NULL,
  legal_hold               boolean NOT NULL DEFAULT false,
  legal_hold_reason        text,
  legal_hold_by            uuid,
  legal_hold_at            timestamptz,
  search_indexed_at        timestamptz,
  created_at               timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX messages_tenant_sent_idx ON messages (tenant_id, sent_at DESC);
CREATE INDEX messages_tenant_received_idx ON messages (tenant_id, received_at DESC);
CREATE INDEX messages_tenant_raw_sha_idx ON messages (tenant_id, raw_sha256);
CREATE INDEX messages_tenant_legal_hold_idx ON messages (tenant_id, legal_hold) WHERE legal_hold;
CREATE INDEX messages_tenant_unindexed_idx ON messages (tenant_id, created_at) WHERE search_indexed_at IS NULL;
CREATE INDEX messages_to_addrs_idx ON messages USING gin (to_addrs);
CREATE INDEX messages_from_addr_idx ON messages (tenant_id, from_addr);

CREATE TABLE message_attachments (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id     uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  message_id    uuid NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  filename      text NOT NULL,
  content_type  text NOT NULL DEFAULT 'application/octet-stream',
  content_id    text,
  size_bytes    bigint NOT NULL,
  sha256        text NOT NULL CHECK (sha256 ~ '^[0-9a-f]{64}$'),
  created_at    timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY (tenant_id, sha256) REFERENCES blobs (tenant_id, sha256)
);
CREATE INDEX message_attachments_message_idx ON message_attachments (message_id);

CREATE TABLE audit_logs (
  id            bigserial PRIMARY KEY,
  tenant_id     uuid NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  actor_user_id uuid,
  actor_email   text,
  action        text NOT NULL,
  target_type   text,
  target_id     text,
  ip            text,
  user_agent    text,
  detail        jsonb NOT NULL DEFAULT '{}'::jsonb,
  created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX audit_logs_tenant_created_idx ON audit_logs (tenant_id, created_at DESC);
CREATE INDEX audit_logs_tenant_action_idx ON audit_logs (tenant_id, action, created_at DESC);

-- Tenant isolation. The setting is transaction-local and is always applied by
-- the Go repository layer before touching tenant-scoped data.
DO $$
DECLARE
  tbl text;
  tables text[] := ARRAY[
    'users', 'sessions', 'ingestion_sources', 'import_jobs', 'import_job_errors',
    'journal_queue', 'blobs', 'messages', 'message_attachments', 'audit_logs'
  ];
BEGIN
  FOREACH tbl IN ARRAY tables LOOP
    EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', tbl);
    EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', tbl);
    EXECUTE format(
      'CREATE POLICY tenant_isolation ON %I USING (tenant_id = NULLIF(current_setting(''app.current_tenant_id'', true), '''')::uuid) WITH CHECK (tenant_id = NULLIF(current_setting(''app.current_tenant_id'', true), '''')::uuid)',
      tbl
    );
  END LOOP;
END
$$;

-- Audit records are append-only, including for the table owner.
CREATE FUNCTION audit_logs_immutable() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'audit_logs is append-only';
END
$$;

CREATE TRIGGER audit_logs_no_update
  BEFORE UPDATE ON audit_logs
  FOR EACH ROW EXECUTE FUNCTION audit_logs_immutable();

CREATE TRIGGER audit_logs_no_delete
  BEFORE DELETE ON audit_logs
  FOR EACH ROW EXECUTE FUNCTION audit_logs_immutable();

-- Message integrity: metadata is immutable, retention can only be extended, and
-- deletion is forbidden while a message is under legal hold or still retained.
CREATE FUNCTION messages_integrity_guard() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'DELETE' THEN
    IF OLD.legal_hold THEN
      RAISE EXCEPTION 'message % is under legal hold and cannot be deleted', OLD.id
        USING ERRCODE = 'check_violation';
    END IF;
    IF now() < OLD.retain_until THEN
      RAISE EXCEPTION 'message % is retained until %', OLD.id, OLD.retain_until
        USING ERRCODE = 'check_violation';
    END IF;
    RETURN OLD;
  END IF;

  IF NEW.tenant_id IS DISTINCT FROM OLD.tenant_id
     OR NEW.raw_sha256 IS DISTINCT FROM OLD.raw_sha256
     OR NEW.raw_blob_sha256 IS DISTINCT FROM OLD.raw_blob_sha256
     OR NEW.journal_envelope_sha256 IS DISTINCT FROM OLD.journal_envelope_sha256
     OR NEW.message_id IS DISTINCT FROM OLD.message_id
     OR NEW.subject IS DISTINCT FROM OLD.subject
     OR NEW.from_addr IS DISTINCT FROM OLD.from_addr
     OR NEW.from_name IS DISTINCT FROM OLD.from_name
     OR NEW.to_addrs IS DISTINCT FROM OLD.to_addrs
     OR NEW.cc_addrs IS DISTINCT FROM OLD.cc_addrs
     OR NEW.bcc_addrs IS DISTINCT FROM OLD.bcc_addrs
     OR NEW.sent_at IS DISTINCT FROM OLD.sent_at
     OR NEW.received_at IS DISTINCT FROM OLD.received_at
     OR NEW.size_bytes IS DISTINCT FROM OLD.size_bytes
     OR NEW.headers IS DISTINCT FROM OLD.headers
     OR NEW.body_text IS DISTINCT FROM OLD.body_text
     OR NEW.body_html IS DISTINCT FROM OLD.body_html
     OR NEW.has_attachments IS DISTINCT FROM OLD.has_attachments
     OR NEW.attachment_count IS DISTINCT FROM OLD.attachment_count
     OR NEW.attachment_filenames IS DISTINCT FROM OLD.attachment_filenames
     OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
    RAISE EXCEPTION 'archived message metadata is immutable'
      USING ERRCODE = 'check_violation';
  END IF;

  IF NEW.retain_until < OLD.retain_until THEN
    RAISE EXCEPTION 'retention period cannot be reduced'
      USING ERRCODE = 'check_violation';
  END IF;

  RETURN NEW;
END
$$;

CREATE TRIGGER messages_no_delete
  BEFORE DELETE ON messages
  FOR EACH ROW EXECUTE FUNCTION messages_integrity_guard();

CREATE TRIGGER messages_no_metadata_update
  BEFORE UPDATE ON messages
  FOR EACH ROW EXECUTE FUNCTION messages_integrity_guard();
