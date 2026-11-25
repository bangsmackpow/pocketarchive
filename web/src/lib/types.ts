export type Role = 'admin' | 'auditor'

export interface User {
  id: string
  email: string
  role: Role
}

export interface Tenant {
  id: string
  slug: string
  name: string
  retention_days: number
}

export interface Session {
  user: User
  tenant: Tenant
}

export interface SearchHit {
  id: string
  subject: string
  from_addr: string
  from_name: string
  to_addrs: string[]
  cc_addrs: string[]
  sent_at: string
  received_at: string
  has_attachments: boolean
  attachment_count: number
  attachment_filenames: string[]
  source_type: 'journal' | 'pst' | 'mbox' | 'eml'
  legal_hold: boolean
  size_bytes: number
  message_id: string
}

export interface SearchResponse {
  hits: SearchHit[]
  total: number
  page: number
  limit: number
  took_ms: number
}

export interface Attachment {
  id: string
  message_id: string
  filename: string
  content_type: string
  content_id?: string
  size_bytes: number
  sha256: string
  created_at: string
}

export interface MessageDetail {
  id: string
  tenant_id: string
  import_job_id?: string
  source_id?: string
  source_type: string
  message_id: string
  subject: string
  from_addr: string
  from_name: string
  to_addrs: string[]
  cc_addrs: string[]
  bcc_addrs: string[]
  sent_at: string
  received_at: string
  size_bytes: number
  raw_sha256: string
  raw_blob_sha256: string
  journal_envelope_sha256?: string
  headers: Record<string, string[]>
  body_text: string
  body_html: string
  has_attachments: boolean
  attachment_count: number
  retain_until: string
  legal_hold: boolean
  legal_hold_reason?: string
  legal_hold_at?: string
  created_at: string
  attachments: Attachment[]
}

export interface ImportJob {
  id: string
  tenant_id: string
  kind: 'pst' | 'mbox'
  status:
    | 'awaiting_upload'
    | 'queued'
    | 'running'
    | 'completed'
    | 'failed'
    | 'cancelled'
  original_filename: string
  size_bytes: number
  bytes_received: number
  chunks_received: number
  total_items: number
  processed_items: number
  duplicate_items: number
  error_items: number
  error_message?: string
  created_at: string
  started_at?: string
  completed_at?: string
  updated_at: string
}

export interface ImportJobError {
  id: number
  job_id: string
  item_ref?: string
  error: string
  created_at: string
}

export interface IngestionSource {
  id: string
  name: string
  kind: 'journal' | 'pst' | 'mbox' | 'eml'
  config: Record<string, unknown>
  enabled: boolean
  created_at: string
  updated_at: string
}

export interface AuditLog {
  id: number
  actor_user_id?: string
  actor_email?: string
  action: string
  target_type?: string
  target_id?: string
  ip?: string
  user_agent?: string
  detail: Record<string, unknown>
  created_at: string
}

export interface Stats {
  total_messages: number
  messages_24h: number
  journal_messages_24h: number
  legal_hold_count: number
  attachments: number
  storage_bytes: number
  queue_backlog: number
  queue_failed: number
  last_journal_at?: string
}

export interface SetupInfo {
  smtp: {
    hostname: string
    port: number
    allowed_domains: string[]
    allowed_ips: string[]
    auth_enabled: boolean
    tls_mode: string
    max_message_bytes: number
  }
  tenant: { slug: string; name: string }
  journaling: { provider: string; address: string; transport: string }
}
