package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *Store) CreateImportJob(ctx context.Context, job ImportJob) (ImportJob, error) {
	if job.ID == uuid.Nil {
		job.ID = uuid.New()
	}
	err := s.WithTenant(ctx, job.TenantID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			INSERT INTO import_jobs
				(id, tenant_id, kind, status, original_filename, spool_path, size_bytes, created_by)
			VALUES ($1, $2, $3, 'awaiting_upload', $4, $5, $6, $7)
			RETURNING created_at, updated_at`,
			job.ID, job.TenantID, job.Kind, job.OriginalFilename, job.SpoolPath, job.SizeBytes, job.CreatedBy).
			Scan(&job.CreatedAt, &job.UpdatedAt)
	})
	if err != nil {
		return ImportJob{}, fmt.Errorf("create import job: %w", err)
	}
	job.Status = "awaiting_upload"
	return job, nil
}

func (s *Store) GetImportJob(ctx context.Context, tenantID, id uuid.UUID) (ImportJob, error) {
	var j ImportJob
	err := s.WithTenant(ctx, tenantID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT id, tenant_id, kind, status, original_filename, spool_path, size_bytes,
			       bytes_received, chunks_received, total_items, processed_items, duplicate_items,
			       error_items, error_message, created_by, created_at, started_at, completed_at, updated_at
			FROM import_jobs WHERE id = $1`, id).
			Scan(&j.ID, &j.TenantID, &j.Kind, &j.Status, &j.OriginalFilename, &j.SpoolPath,
				&j.SizeBytes, &j.BytesReceived, &j.ChunksReceived, &j.TotalItems, &j.ProcessedItems,
				&j.DuplicateItems, &j.ErrorItems, &j.ErrorMessage, &j.CreatedBy, &j.CreatedAt,
				&j.StartedAt, &j.CompletedAt, &j.UpdatedAt)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ImportJob{}, ErrNotFound
	}
	return j, err
}

func (s *Store) ListImportJobs(ctx context.Context, tenantID uuid.UUID, limit int) ([]ImportJob, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	var out []ImportJob
	err := s.WithTenant(ctx, tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id, tenant_id, kind, status, original_filename, spool_path, size_bytes,
			       bytes_received, chunks_received, total_items, processed_items, duplicate_items,
			       error_items, error_message, created_by, created_at, started_at, completed_at, updated_at
			FROM import_jobs ORDER BY created_at DESC LIMIT $1`, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var j ImportJob
			if err := rows.Scan(&j.ID, &j.TenantID, &j.Kind, &j.Status, &j.OriginalFilename,
				&j.SpoolPath, &j.SizeBytes, &j.BytesReceived, &j.ChunksReceived, &j.TotalItems,
				&j.ProcessedItems, &j.DuplicateItems, &j.ErrorItems, &j.ErrorMessage, &j.CreatedBy,
				&j.CreatedAt, &j.StartedAt, &j.CompletedAt, &j.UpdatedAt); err != nil {
				return err
			}
			out = append(out, j)
		}
		return rows.Err()
	})
	return out, err
}

// AppendImportChunk enforces sequential chunk indexes and appends bytes to the
// upload spool file while holding a row lock.
func (s *Store) AppendImportChunk(ctx context.Context, tenantID, id uuid.UUID, index int, data []byte, maxBytes int64) (ImportJob, error) {
	var j ImportJob
	err := s.WithTenant(ctx, tenantID, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			SELECT id, tenant_id, kind, status, original_filename, spool_path, size_bytes,
			       bytes_received, chunks_received, total_items, processed_items, duplicate_items,
			       error_items, error_message, created_by, created_at, started_at, completed_at, updated_at
			FROM import_jobs WHERE id = $1 FOR UPDATE`, id).
			Scan(&j.ID, &j.TenantID, &j.Kind, &j.Status, &j.OriginalFilename, &j.SpoolPath,
				&j.SizeBytes, &j.BytesReceived, &j.ChunksReceived, &j.TotalItems, &j.ProcessedItems,
				&j.DuplicateItems, &j.ErrorItems, &j.ErrorMessage, &j.CreatedBy, &j.CreatedAt,
				&j.StartedAt, &j.CompletedAt, &j.UpdatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if j.Status != "awaiting_upload" {
			return fmt.Errorf("%w: import is not accepting chunks", ErrConflict)
		}
		if index != j.ChunksReceived {
			return fmt.Errorf("%w: expected chunk %d", ErrConflict, j.ChunksReceived)
		}
		newSize := j.BytesReceived + int64(len(data))
		if maxBytes > 0 && newSize > maxBytes {
			return fmt.Errorf("%w: upload exceeds maximum size", ErrConflict)
		}
		if j.SizeBytes > 0 && newSize > j.SizeBytes {
			return fmt.Errorf("%w: upload exceeds declared size", ErrConflict)
		}
		f, err := os.OpenFile(j.SpoolPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
		if err != nil {
			return fmt.Errorf("open spool file: %w", err)
		}
		if _, err := f.Write(data); err != nil {
			f.Close()
			return fmt.Errorf("append chunk: %w", err)
		}
		if err := f.Close(); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE import_jobs
			SET bytes_received = $2, chunks_received = chunks_received + 1, updated_at = now()
			WHERE id = $1`, id, newSize); err != nil {
			return err
		}
		j.BytesReceived = newSize
		j.ChunksReceived++
		return nil
	})
	return j, err
}

func (s *Store) CompleteImport(ctx context.Context, tenantID, id uuid.UUID) (ImportJob, error) {
	var j ImportJob
	err := s.WithTenant(ctx, tenantID, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			UPDATE import_jobs
			SET status = 'queued',
			    size_bytes = CASE WHEN size_bytes = 0 THEN bytes_received ELSE size_bytes END,
			    updated_at = now()
			WHERE id = $1 AND status = 'awaiting_upload' AND bytes_received > 0
			RETURNING id, tenant_id, kind, status, original_filename, spool_path, size_bytes,
			          bytes_received, chunks_received, total_items, processed_items, duplicate_items,
			          error_items, error_message, created_by, created_at, started_at, completed_at, updated_at`, id).
			Scan(&j.ID, &j.TenantID, &j.Kind, &j.Status, &j.OriginalFilename, &j.SpoolPath,
				&j.SizeBytes, &j.BytesReceived, &j.ChunksReceived, &j.TotalItems, &j.ProcessedItems,
				&j.DuplicateItems, &j.ErrorItems, &j.ErrorMessage, &j.CreatedBy, &j.CreatedAt,
				&j.StartedAt, &j.CompletedAt, &j.UpdatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: import has no uploaded data or is already complete", ErrConflict)
		}
		return err
	})
	return j, err
}

func (s *Store) ClaimImportJob(ctx context.Context, tenantID uuid.UUID) (ImportJob, bool, error) {
	var j ImportJob
	err := s.WithTenant(ctx, tenantID, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			UPDATE import_jobs
			SET status = 'running', started_at = COALESCE(started_at, now()), updated_at = now()
			WHERE id = (
				SELECT id FROM import_jobs
				WHERE status = 'queued'
				ORDER BY created_at
				LIMIT 1
				FOR UPDATE SKIP LOCKED
			)
			RETURNING id, tenant_id, kind, status, original_filename, spool_path, size_bytes,
			          bytes_received, chunks_received, total_items, processed_items, duplicate_items,
			          error_items, error_message, created_by, created_at, started_at, completed_at, updated_at`).
			Scan(&j.ID, &j.TenantID, &j.Kind, &j.Status, &j.OriginalFilename, &j.SpoolPath,
				&j.SizeBytes, &j.BytesReceived, &j.ChunksReceived, &j.TotalItems, &j.ProcessedItems,
				&j.DuplicateItems, &j.ErrorItems, &j.ErrorMessage, &j.CreatedBy, &j.CreatedAt,
				&j.StartedAt, &j.CompletedAt, &j.UpdatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	})
	if errors.Is(err, ErrNotFound) {
		return ImportJob{}, false, nil
	}
	if err != nil {
		return ImportJob{}, false, err
	}
	return j, true, nil
}

func (s *Store) UpdateImportProgress(ctx context.Context, tenantID, id uuid.UUID, total, processed, duplicates, errs int) error {
	return s.WithTenant(ctx, tenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			UPDATE import_jobs
			SET total_items = $2, processed_items = $3, duplicate_items = $4, error_items = $5, updated_at = now()
			WHERE id = $1`, id, total, processed, duplicates, errs)
		return err
	})
}

func (s *Store) FinishImportJob(ctx context.Context, tenantID, id uuid.UUID, status, errMessage string) error {
	if status != "completed" && status != "failed" {
		return errors.New("invalid terminal import status")
	}
	return s.WithTenant(ctx, tenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			UPDATE import_jobs
			SET status = $2, error_message = NULLIF($3, ''), completed_at = now(), updated_at = now()
			WHERE id = $1`, id, status, errMessage)
		return err
	})
}

func (s *Store) InsertImportError(ctx context.Context, tenantID, jobID uuid.UUID, itemRef, message string) error {
	return s.WithTenant(ctx, tenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO import_job_errors (tenant_id, job_id, item_ref, error)
			VALUES ($1, $2, $3, $4)`, tenantID, jobID, itemRef, message)
		return err
	})
}

func (s *Store) ListImportErrors(ctx context.Context, tenantID, jobID uuid.UUID, limit int) ([]ImportJobError, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var out []ImportJobError
	err := s.WithTenant(ctx, tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id, tenant_id, job_id, item_ref, error, created_at
			FROM import_job_errors WHERE job_id = $1 ORDER BY id DESC LIMIT $2`, jobID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e ImportJobError
			if err := rows.Scan(&e.ID, &e.TenantID, &e.JobID, &e.ItemRef, &e.Error, &e.CreatedAt); err != nil {
				return err
			}
			out = append(out, e)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Store) EnqueueJournal(ctx context.Context, tenantID uuid.UUID, spoolPath, envelopeFrom, remoteAddr string) error {
	return s.WithTenant(ctx, tenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO journal_queue (tenant_id, spool_path, envelope_from, remote_addr)
			VALUES ($1, $2, $3, $4)`, tenantID, spoolPath, envelopeFrom, remoteAddr)
		return err
	})
}

func (s *Store) ClaimJournal(ctx context.Context, tenantID uuid.UUID) (JournalQueueItem, bool, error) {
	var item JournalQueueItem
	err := s.WithTenant(ctx, tenantID, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			UPDATE journal_queue
			SET status = 'processing', attempts = attempts + 1, updated_at = now()
			WHERE id = (
				SELECT id FROM journal_queue
				WHERE status = 'queued' AND next_attempt_at <= now()
				ORDER BY id
				LIMIT 1
				FOR UPDATE SKIP LOCKED
			)
			RETURNING id, tenant_id, spool_path, COALESCE(envelope_from, ''), COALESCE(remote_addr, ''), attempts`).
			Scan(&item.ID, &item.TenantID, &item.SpoolPath, &item.EnvelopeFrom, &item.RemoteAddr, &item.Attempts)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	})
	if errors.Is(err, ErrNotFound) {
		return JournalQueueItem{}, false, nil
	}
	if err != nil {
		return JournalQueueItem{}, false, err
	}
	return item, true, nil
}

func (s *Store) MarkJournalDone(ctx context.Context, tenantID uuid.UUID, id int64) error {
	return s.WithTenant(ctx, tenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE journal_queue SET status = 'done', last_error = NULL, updated_at = now() WHERE id = $1`, id)
		return err
	})
}

func (s *Store) RetryJournal(ctx context.Context, tenantID uuid.UUID, id int64, message string, nextAt time.Time) error {
	return s.WithTenant(ctx, tenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			UPDATE journal_queue
			SET status = 'queued', last_error = $2, next_attempt_at = $3, updated_at = now()
			WHERE id = $1`, id, message, nextAt)
		return err
	})
}

func (s *Store) FailJournal(ctx context.Context, tenantID uuid.UUID, id int64, message string) error {
	return s.WithTenant(ctx, tenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE journal_queue SET status = 'failed', last_error = $2, updated_at = now() WHERE id = $1`, id, message)
		return err
	})
}
