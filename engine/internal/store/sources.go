package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func scanSource(row pgx.Row) (IngestionSource, error) {
	var src IngestionSource
	var cfg []byte
	err := row.Scan(&src.ID, &src.TenantID, &src.Name, &src.Kind, &cfg, &src.Enabled, &src.CreatedAt, &src.UpdatedAt)
	if err != nil {
		return IngestionSource{}, err
	}
	if len(cfg) > 0 {
		_ = json.Unmarshal(cfg, &src.Config)
	}
	if src.Config == nil {
		src.Config = map[string]any{}
	}
	return src, nil
}

func (s *Store) ListSources(ctx context.Context, tenantID uuid.UUID) ([]IngestionSource, error) {
	var out []IngestionSource
	err := s.WithTenant(ctx, tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id, tenant_id, name, kind, config, enabled, created_at, updated_at
			FROM ingestion_sources ORDER BY created_at`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			src, err := scanSource(rows)
			if err != nil {
				return err
			}
			out = append(out, src)
		}
		return rows.Err()
	})
	return out, err
}

func (s *Store) CreateSource(ctx context.Context, src IngestionSource) (IngestionSource, error) {
	cfg, err := json.Marshal(src.Config)
	if err != nil {
		return IngestionSource{}, err
	}
	err = s.WithTenant(ctx, src.TenantID, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx, `
			INSERT INTO ingestion_sources (tenant_id, name, kind, config, enabled)
			VALUES ($1, $2, $3, $4, $5)
			RETURNING id, tenant_id, name, kind, config, enabled, created_at, updated_at`,
			src.TenantID, src.Name, src.Kind, cfg, src.Enabled)
		var scanErr error
		src, scanErr = scanSource(row)
		return scanErr
	})
	return src, err
}

func (s *Store) UpdateSource(ctx context.Context, tenantID, id uuid.UUID, name string, enabled bool, config map[string]any) (IngestionSource, error) {
	cfg, err := json.Marshal(config)
	if err != nil {
		return IngestionSource{}, err
	}
	var src IngestionSource
	err = s.WithTenant(ctx, tenantID, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx, `
			UPDATE ingestion_sources
			SET name = $2, enabled = $3, config = $4, updated_at = now()
			WHERE id = $1
			RETURNING id, tenant_id, name, kind, config, enabled, created_at, updated_at`,
			id, name, enabled, cfg)
		var scanErr error
		src, scanErr = scanSource(row)
		if errors.Is(scanErr, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return scanErr
	})
	return src, err
}

func (s *Store) DeleteSource(ctx context.Context, tenantID, id uuid.UUID) error {
	err := s.WithTenant(ctx, tenantID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM ingestion_sources WHERE id = $1`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
	if errors.Is(err, ErrNotFound) {
		return ErrNotFound
	}
	return err
}

func (s *Store) EnsureDefaultSource(ctx context.Context, tenantID uuid.UUID, name string) error {
	return s.WithTenant(ctx, tenantID, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ingestion_sources WHERE kind = 'journal')`).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return nil
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO ingestion_sources (tenant_id, name, kind, config, enabled)
			VALUES ($1, $2, 'journal', '{"provider":"microsoft365"}'::jsonb, true)`, tenantID, name)
		if err != nil {
			return fmt.Errorf("create default journal source: %w", err)
		}
		return nil
	})
}
