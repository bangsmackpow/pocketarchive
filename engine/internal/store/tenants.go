package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *Store) GetTenantBySlug(ctx context.Context, slug string) (Tenant, error) {
	var t Tenant
	err := s.pool.QueryRow(ctx, `
		SELECT id, slug, name, wrapped_kek, kek_version, retention_days, created_at, updated_at
		FROM tenants WHERE slug = $1`, slug).
		Scan(&t.ID, &t.Slug, &t.Name, &t.WrappedKEK, &t.KEKVersion, &t.RetentionDays, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Tenant{}, ErrNotFound
	}
	if err != nil {
		return Tenant{}, fmt.Errorf("get tenant by slug: %w", err)
	}
	return t, nil
}

func (s *Store) GetTenantByID(ctx context.Context, id uuid.UUID) (Tenant, error) {
	var t Tenant
	err := s.pool.QueryRow(ctx, `
		SELECT id, slug, name, wrapped_kek, kek_version, retention_days, created_at, updated_at
		FROM tenants WHERE id = $1`, id).
		Scan(&t.ID, &t.Slug, &t.Name, &t.WrappedKEK, &t.KEKVersion, &t.RetentionDays, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Tenant{}, ErrNotFound
	}
	if err != nil {
		return Tenant{}, fmt.Errorf("get tenant: %w", err)
	}
	return t, nil
}

// ListTenants is intentionally unscoped: workers must enumerate tenants before
// setting RLS context, and login resolves a tenant by slug.
func (s *Store) ListTenants(ctx context.Context) ([]Tenant, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, slug, name, wrapped_kek, kek_version, retention_days, created_at, updated_at
		FROM tenants ORDER BY created_at`)
	if err != nil {
		return nil, fmt.Errorf("list tenants: %w", err)
	}
	defer rows.Close()
	var out []Tenant
	for rows.Next() {
		var t Tenant
		if err := rows.Scan(&t.ID, &t.Slug, &t.Name, &t.WrappedKEK, &t.KEKVersion, &t.RetentionDays, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) CreateTenant(ctx context.Context, slug, name string, retentionDays int, wrappedKEK []byte) (Tenant, error) {
	return s.CreateTenantWithID(ctx, uuid.New(), slug, name, retentionDays, wrappedKEK)
}

func (s *Store) CreateTenantWithID(ctx context.Context, id uuid.UUID, slug, name string, retentionDays int, wrappedKEK []byte) (Tenant, error) {
	var t Tenant
	err := s.pool.QueryRow(ctx, `
		INSERT INTO tenants (id, slug, name, retention_days, wrapped_kek)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, slug, name, wrapped_kek, kek_version, retention_days, created_at, updated_at`,
		id, slug, name, retentionDays, wrappedKEK).
		Scan(&t.ID, &t.Slug, &t.Name, &t.WrappedKEK, &t.KEKVersion, &t.RetentionDays, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return Tenant{}, fmt.Errorf("create tenant: %w", err)
	}
	return t, nil
}

func (s *Store) SetTenantKEK(ctx context.Context, id uuid.UUID, wrappedKEK []byte) error {
	_, err := s.pool.Exec(ctx, `UPDATE tenants SET wrapped_kek = $2, updated_at = now() WHERE id = $1`, id, wrappedKEK)
	if err != nil {
		return fmt.Errorf("update tenant kek: %w", err)
	}
	return nil
}

func (s *Store) UpdateRetention(ctx context.Context, id uuid.UUID, days int) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE tenants SET retention_days = $2, updated_at = now() WHERE id = $1`, id, days)
	if err != nil {
		return fmt.Errorf("update retention: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
