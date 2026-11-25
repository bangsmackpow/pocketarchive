package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const messageColumns = `id, tenant_id, import_job_id, source_id, source_type, message_id, subject,
	from_addr, from_name, to_addrs, cc_addrs, bcc_addrs, sent_at, received_at, size_bytes,
	raw_sha256, raw_blob_sha256, journal_envelope_sha256, headers, body_text, body_html,
	has_attachments, attachment_count, attachment_filenames, retain_until, legal_hold,
	legal_hold_reason, legal_hold_by, legal_hold_at, search_indexed_at, created_at`

func scanMessage(row pgx.Row) (Message, error) {
	var m Message
	var headers []byte
	err := row.Scan(&m.ID, &m.TenantID, &m.ImportJobID, &m.SourceID, &m.SourceType, &m.MessageID,
		&m.Subject, &m.FromAddr, &m.FromName, &m.ToAddrs, &m.CcAddrs, &m.BccAddrs, &m.SentAt,
		&m.ReceivedAt, &m.SizeBytes, &m.RawSHA256, &m.RawBlobSHA256, &m.JournalEnvelopeSHA256,
		&headers, &m.BodyText, &m.BodyHTML, &m.HasAttachments, &m.AttachmentCount,
		&m.AttachmentFilenames, &m.RetainUntil, &m.LegalHold, &m.LegalHoldReason, &m.LegalHoldBy,
		&m.LegalHoldAt, &m.SearchIndexedAt, &m.CreatedAt)
	if err != nil {
		return Message{}, err
	}
	_ = json.Unmarshal(headers, &m.Headers)
	if m.Headers == nil {
		m.Headers = map[string][]string{}
	}
	return m, nil
}

// InsertMessageTx inserts immutable message metadata using the caller's tenant
// transaction. Blobs must already exist.
func InsertMessageTx(ctx context.Context, tx pgx.Tx, m *Message) error {
	headers, err := json.Marshal(m.Headers)
	if err != nil {
		return fmt.Errorf("marshal headers: %w", err)
	}
	if m.AttachmentFilenames == nil {
		m.AttachmentFilenames = []string{}
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO messages (
			tenant_id, import_job_id, source_id, source_type, message_id, subject, from_addr,
			from_name, to_addrs, cc_addrs, bcc_addrs, sent_at, received_at, size_bytes,
			raw_sha256, raw_blob_sha256, journal_envelope_sha256, headers, body_text, body_html,
			has_attachments, attachment_count, attachment_filenames, retain_until, legal_hold
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14,
			$15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25
		) RETURNING id, created_at`,
		m.TenantID, m.ImportJobID, m.SourceID, m.SourceType, m.MessageID, m.Subject, m.FromAddr,
		m.FromName, m.ToAddrs, m.CcAddrs, m.BccAddrs, m.SentAt, m.ReceivedAt, m.SizeBytes,
		m.RawSHA256, m.RawBlobSHA256, m.JournalEnvelopeSHA256, headers, m.BodyText, m.BodyHTML,
		m.HasAttachments, m.AttachmentCount, m.AttachmentFilenames, m.RetainUntil, m.LegalHold).
		Scan(&m.ID, &m.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert message: %w", err)
	}
	return nil
}

func InsertAttachmentTx(ctx context.Context, tx pgx.Tx, a *Attachment) error {
	return tx.QueryRow(ctx, `
		INSERT INTO message_attachments (tenant_id, message_id, filename, content_type, content_id, size_bytes, sha256)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6, $7)
		RETURNING id, created_at`,
		a.TenantID, a.MessageID, a.Filename, a.ContentType, a.ContentID, a.SizeBytes, a.SHA256).
		Scan(&a.ID, &a.CreatedAt)
}

func (s *Store) GetMessage(ctx context.Context, tenantID, id uuid.UUID) (Message, error) {
	var m Message
	err := s.WithTenant(ctx, tenantID, func(tx pgx.Tx) error {
		var err error
		m, err = scanMessage(tx.QueryRow(ctx, `SELECT `+messageColumns+` FROM messages WHERE id = $1`, id))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `
			SELECT id, tenant_id, message_id, filename, content_type, COALESCE(content_id, ''),
			       size_bytes, sha256, created_at
			FROM message_attachments WHERE message_id = $1 ORDER BY filename`, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var a Attachment
			if err := rows.Scan(&a.ID, &a.TenantID, &a.MessageID, &a.Filename, &a.ContentType,
				&a.ContentID, &a.SizeBytes, &a.SHA256, &a.CreatedAt); err != nil {
				return err
			}
			m.Attachments = append(m.Attachments, a)
		}
		return rows.Err()
	})
	return m, err
}

func (s *Store) GetAttachment(ctx context.Context, tenantID, messageID, attachmentID uuid.UUID) (Attachment, error) {
	var a Attachment
	err := s.WithTenant(ctx, tenantID, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			SELECT id, tenant_id, message_id, filename, content_type, COALESCE(content_id, ''),
			       size_bytes, sha256, created_at
			FROM message_attachments WHERE id = $1 AND message_id = $2`, attachmentID, messageID).
			Scan(&a.ID, &a.TenantID, &a.MessageID, &a.Filename, &a.ContentType, &a.ContentID,
				&a.SizeBytes, &a.SHA256, &a.CreatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	})
	return a, err
}

func (s *Store) MessageSHAExists(ctx context.Context, tenantID uuid.UUID, sha256 string) (bool, error) {
	var exists bool
	err := s.WithTenant(ctx, tenantID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM messages WHERE raw_sha256 = $1)`, sha256).Scan(&exists)
	})
	return exists, err
}

func (s *Store) SetLegalHold(ctx context.Context, tenantID, id, actor uuid.UUID, hold bool, reason string) (bool, error) {
	var found bool
	err := s.WithTenant(ctx, tenantID, func(tx pgx.Tx) error {
		var reasonPtr *string
		if reason != "" {
			reasonPtr = &reason
		}
		tag, err := tx.Exec(ctx, `
			UPDATE messages
			SET legal_hold = $2::boolean,
			    legal_hold_reason = CASE WHEN $2::boolean THEN $3::text ELSE NULL END,
			    legal_hold_by = CASE WHEN $2::boolean THEN $4::uuid ELSE NULL END,
			    legal_hold_at = CASE WHEN $2::boolean THEN now() ELSE NULL END
			WHERE id = $1`, id, hold, reasonPtr, actor)
		if err != nil {
			return err
		}
		found = tag.RowsAffected() > 0
		if !found {
			return ErrNotFound
		}
		return nil
	})
	return found, err
}

func (s *Store) DeleteMessage(ctx context.Context, tenantID, id uuid.UUID) error {
	err := s.WithTenant(ctx, tenantID, func(tx pgx.Tx) error {
		var hold bool
		var retainUntil time.Time
		err := tx.QueryRow(ctx, `SELECT legal_hold, retain_until FROM messages WHERE id = $1 FOR UPDATE`, id).
			Scan(&hold, &retainUntil)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if hold {
			return ErrLegalHold
		}
		if time.Now().Before(retainUntil) {
			return ErrRetention
		}
		_, err = tx.Exec(ctx, `DELETE FROM messages WHERE id = $1`, id)
		return err
	})
	return err
}

func (s *Store) SetSearchIndexed(ctx context.Context, tenantID uuid.UUID, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	raw := make([]string, len(ids))
	for i, id := range ids {
		raw[i] = id.String()
	}
	return s.WithTenant(ctx, tenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE messages SET search_indexed_at = now() WHERE id = ANY($1::uuid[])`, raw)
		return err
	})
}

func (s *Store) ClearSearchIndexed(ctx context.Context, tenantID uuid.UUID) error {
	return s.WithTenant(ctx, tenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE messages SET search_indexed_at = NULL`)
		return err
	})
}

func (s *Store) MarkSearchStale(ctx context.Context, tenantID, id uuid.UUID) error {
	return s.WithTenant(ctx, tenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE messages SET search_indexed_at = NULL WHERE id = $1`, id)
		return err
	})
}

func (s *Store) ListUnindexed(ctx context.Context, tenantID uuid.UUID, limit int) ([]Message, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var out []Message
	err := s.WithTenant(ctx, tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT `+messageColumns+` FROM messages
			WHERE search_indexed_at IS NULL
			ORDER BY created_at LIMIT $1`, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			m, err := scanMessage(rows)
			if err != nil {
				return err
			}
			out = append(out, m)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Store) ListMessagesBatch(ctx context.Context, tenantID uuid.UUID, limit, offset int) ([]Message, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	var out []Message
	err := s.WithTenant(ctx, tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT `+messageColumns+` FROM messages
			ORDER BY created_at, id LIMIT $1 OFFSET $2`, limit, offset)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			m, err := scanMessage(rows)
			if err != nil {
				return err
			}
			out = append(out, m)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Store) Stats(ctx context.Context, tenantID uuid.UUID) (Stats, error) {
	var st Stats
	err := s.WithTenant(ctx, tenantID, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			SELECT
				count(*),
				count(*) FILTER (WHERE received_at >= now() - interval '24 hours'),
				count(*) FILTER (WHERE received_at >= now() - interval '24 hours' AND source_type = 'journal'),
				count(*) FILTER (WHERE legal_hold),
				COALESCE(sum(attachment_count), 0)
			FROM messages`).
			Scan(&st.TotalMessages, &st.Messages24h, &st.JournalMessages24h, &st.LegalHoldCount, &st.Attachments)
		if err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT COALESCE(sum(size_bytes), 0) FROM blobs`).Scan(&st.StorageBytes); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `
			SELECT count(*) FILTER (WHERE status IN ('queued', 'processing')),
			       count(*) FILTER (WHERE status = 'failed')
			FROM journal_queue`).Scan(&st.QueueBacklog, &st.QueueFailed); err != nil {
			return err
		}
		var last pgtype.Timestamptz
		if err := tx.QueryRow(ctx, `SELECT max(received_at) FROM messages WHERE source_type = 'journal'`).Scan(&last); err != nil {
			return err
		}
		if last.Valid {
			t := last.Time
			st.LastJournalAt = &t
		}
		return nil
	})
	return st, err
}
