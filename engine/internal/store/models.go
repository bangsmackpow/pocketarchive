package store

import (
	"time"

	"github.com/google/uuid"
)

type Tenant struct {
	ID            uuid.UUID `json:"id"`
	Slug          string    `json:"slug"`
	Name          string    `json:"name"`
	WrappedKEK    []byte    `json:"-"`
	KEKVersion    int       `json:"-"`
	RetentionDays int       `json:"retention_days"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type User struct {
	ID           uuid.UUID  `json:"id"`
	TenantID     uuid.UUID  `json:"tenant_id"`
	Email        string     `json:"email"`
	PasswordHash string     `json:"-"`
	Role         string     `json:"role"`
	IsActive     bool       `json:"is_active"`
	CreatedAt    time.Time  `json:"created_at"`
	LastLoginAt  *time.Time `json:"last_login_at,omitempty"`
}

type Session struct {
	ID         uuid.UUID
	TenantID   uuid.UUID
	UserID     uuid.UUID
	TokenHash  []byte
	ExpiresAt  time.Time
	RevokedAt  *time.Time
	IP         string
	UserAgent  string
	LastSeenAt *time.Time
}

type IngestionSource struct {
	ID        uuid.UUID      `json:"id"`
	TenantID  uuid.UUID      `json:"tenant_id"`
	Name      string         `json:"name"`
	Kind      string         `json:"kind"`
	Config    map[string]any `json:"config"`
	Enabled   bool           `json:"enabled"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

type ImportJob struct {
	ID               uuid.UUID  `json:"id"`
	TenantID         uuid.UUID  `json:"tenant_id"`
	Kind             string     `json:"kind"`
	Status           string     `json:"status"`
	OriginalFilename string     `json:"original_filename"`
	SpoolPath        string     `json:"-"`
	SizeBytes        int64      `json:"size_bytes"`
	BytesReceived    int64      `json:"bytes_received"`
	ChunksReceived   int        `json:"chunks_received"`
	TotalItems       int        `json:"total_items"`
	ProcessedItems   int        `json:"processed_items"`
	DuplicateItems   int        `json:"duplicate_items"`
	ErrorItems       int        `json:"error_items"`
	ErrorMessage     *string    `json:"error_message,omitempty"`
	CreatedBy        *uuid.UUID `json:"created_by,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	StartedAt        *time.Time `json:"started_at,omitempty"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type ImportJobError struct {
	ID        int64     `json:"id"`
	TenantID  uuid.UUID `json:"-"`
	JobID     uuid.UUID `json:"job_id"`
	ItemRef   *string   `json:"item_ref,omitempty"`
	Error     string    `json:"error"`
	CreatedAt time.Time `json:"created_at"`
}

type JournalQueueItem struct {
	ID           int64
	TenantID     uuid.UUID
	SpoolPath    string
	EnvelopeFrom string
	RemoteAddr   string
	Attempts     int
}

type BlobMeta struct {
	TenantID   uuid.UUID
	SHA256     string
	SizeBytes  int64
	WrappedDEK []byte
	FileNonce  []byte
	CreatedAt  time.Time
}

type Attachment struct {
	ID          uuid.UUID `json:"id"`
	TenantID    uuid.UUID `json:"-"`
	MessageID   uuid.UUID `json:"message_id"`
	Filename    string    `json:"filename"`
	ContentType string    `json:"content_type"`
	ContentID   string    `json:"content_id,omitempty"`
	SizeBytes   int64     `json:"size_bytes"`
	SHA256      string    `json:"sha256"`
	CreatedAt   time.Time `json:"created_at"`
}

type Message struct {
	ID                    uuid.UUID           `json:"id"`
	TenantID              uuid.UUID           `json:"tenant_id"`
	ImportJobID           *uuid.UUID          `json:"import_job_id,omitempty"`
	SourceID              *uuid.UUID          `json:"source_id,omitempty"`
	SourceType            string              `json:"source_type"`
	MessageID             string              `json:"message_id"`
	Subject               string              `json:"subject"`
	FromAddr              string              `json:"from_addr"`
	FromName              string              `json:"from_name"`
	ToAddrs               []string            `json:"to_addrs"`
	CcAddrs               []string            `json:"cc_addrs"`
	BccAddrs              []string            `json:"bcc_addrs"`
	SentAt                time.Time           `json:"sent_at"`
	ReceivedAt            time.Time           `json:"received_at"`
	SizeBytes             int64               `json:"size_bytes"`
	RawSHA256             string              `json:"raw_sha256"`
	RawBlobSHA256         string              `json:"raw_blob_sha256"`
	JournalEnvelopeSHA256 *string             `json:"journal_envelope_sha256,omitempty"`
	Headers               map[string][]string `json:"headers"`
	BodyText              string              `json:"body_text"`
	BodyHTML              string              `json:"body_html"`
	HasAttachments        bool                `json:"has_attachments"`
	AttachmentCount       int                 `json:"attachment_count"`
	AttachmentFilenames   []string            `json:"-"`
	RetainUntil           time.Time           `json:"retain_until"`
	LegalHold             bool                `json:"legal_hold"`
	LegalHoldReason       *string             `json:"legal_hold_reason,omitempty"`
	LegalHoldBy           *uuid.UUID          `json:"legal_hold_by,omitempty"`
	LegalHoldAt           *time.Time          `json:"legal_hold_at,omitempty"`
	SearchIndexedAt       *time.Time          `json:"-"`
	CreatedAt             time.Time           `json:"created_at"`
	Attachments           []Attachment        `json:"attachments,omitempty"`
}

type AuditLog struct {
	ID          int64          `json:"id"`
	TenantID    uuid.UUID      `json:"-"`
	ActorUserID *uuid.UUID     `json:"actor_user_id,omitempty"`
	ActorEmail  string         `json:"actor_email,omitempty"`
	Action      string         `json:"action"`
	TargetType  string         `json:"target_type,omitempty"`
	TargetID    string         `json:"target_id,omitempty"`
	IP          string         `json:"ip,omitempty"`
	UserAgent   string         `json:"user_agent,omitempty"`
	Detail      map[string]any `json:"detail"`
	CreatedAt   time.Time      `json:"created_at"`
}

type AuditEntry struct {
	TenantID    uuid.UUID
	ActorUserID *uuid.UUID
	ActorEmail  string
	Action      string
	TargetType  string
	TargetID    string
	IP          string
	UserAgent   string
	Detail      map[string]any
}

type Stats struct {
	TotalMessages      int64      `json:"total_messages"`
	Messages24h        int64      `json:"messages_24h"`
	JournalMessages24h int64      `json:"journal_messages_24h"`
	LegalHoldCount     int64      `json:"legal_hold_count"`
	Attachments        int64      `json:"attachments"`
	StorageBytes       int64      `json:"storage_bytes"`
	QueueBacklog       int64      `json:"queue_backlog"`
	QueueFailed        int64      `json:"queue_failed"`
	LastJournalAt      *time.Time `json:"last_journal_at,omitempty"`
}

type SearchDocument struct {
	ID                  string   `json:"id"`
	TenantID            string   `json:"tenant_id"`
	Subject             string   `json:"subject"`
	BodyText            string   `json:"body_text"`
	FromAddr            string   `json:"from_addr"`
	FromName            string   `json:"from_name"`
	ToAddrs             []string `json:"to_addrs"`
	CcAddrs             []string `json:"cc_addrs"`
	SentAt              int64    `json:"sent_at"`
	ReceivedAt          int64    `json:"received_at"`
	HasAttachments      bool     `json:"has_attachments"`
	AttachmentCount     int      `json:"attachment_count"`
	AttachmentFilenames []string `json:"attachment_filenames"`
	SourceType          string   `json:"source_type"`
	LegalHold           bool     `json:"legal_hold"`
	SizeBytes           int64    `json:"size_bytes"`
	MessageID           string   `json:"message_id"`
	RetainUntil         int64    `json:"retain_until"`
}

type UnindexedMessage struct {
	Message
	AttachmentFilenames []string
}
