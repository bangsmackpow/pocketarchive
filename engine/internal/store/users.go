package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *Store) CountUsers(ctx context.Context, tenantID uuid.UUID) (int, error) {
	var n int
	err := s.WithTenant(ctx, tenantID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&n)
	})
	return n, err
}

func (s *Store) CreateUser(ctx context.Context, u User) (User, error) {
	err := s.WithTenant(ctx, u.TenantID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			INSERT INTO users (tenant_id, email, password_hash, role)
			VALUES ($1, $2, $3, $4)
			RETURNING id, created_at`,
			u.TenantID, u.Email, u.PasswordHash, u.Role).
			Scan(&u.ID, &u.CreatedAt)
	})
	if err != nil {
		return User{}, fmt.Errorf("create user: %w", err)
	}
	u.IsActive = true
	return u, nil
}

func (s *Store) GetUserByEmail(ctx context.Context, tenantID uuid.UUID, email string) (User, error) {
	var u User
	err := s.WithTenant(ctx, tenantID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT id, tenant_id, email, password_hash, role, is_active, created_at, last_login_at
			FROM users WHERE lower(email) = lower($1)`, email).
			Scan(&u.ID, &u.TenantID, &u.Email, &u.PasswordHash, &u.Role, &u.IsActive, &u.CreatedAt, &u.LastLoginAt)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("get user: %w", err)
	}
	return u, nil
}

func (s *Store) GetUserByID(ctx context.Context, tenantID, userID uuid.UUID) (User, error) {
	var u User
	err := s.WithTenant(ctx, tenantID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT id, tenant_id, email, password_hash, role, is_active, created_at, last_login_at
			FROM users WHERE id = $1`, userID).
			Scan(&u.ID, &u.TenantID, &u.Email, &u.PasswordHash, &u.Role, &u.IsActive, &u.CreatedAt, &u.LastLoginAt)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return u, err
}

func (s *Store) UpdateLastLogin(ctx context.Context, tenantID, userID uuid.UUID) error {
	return s.WithTenant(ctx, tenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE users SET last_login_at = now() WHERE id = $1`, userID)
		return err
	})
}
