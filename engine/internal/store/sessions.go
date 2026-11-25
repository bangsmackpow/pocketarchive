package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *Store) CreateSession(ctx context.Context, sess Session) error {
	return s.WithTenant(ctx, sess.TenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO sessions (id, tenant_id, user_id, token_hash, expires_at, ip, user_agent)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			sess.ID, sess.TenantID, sess.UserID, sess.TokenHash, sess.ExpiresAt, sess.IP, sess.UserAgent)
		return err
	})
}

func (s *Store) SessionActive(ctx context.Context, tenantID, sessionID, userID uuid.UUID, now time.Time) (bool, error) {
	var ok bool
	err := s.WithTenant(ctx, tenantID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM sessions
				WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL AND expires_at > $3
			)`, sessionID, userID, now).Scan(&ok)
	})
	return ok, err
}

func (s *Store) TouchSession(ctx context.Context, tenantID, sessionID uuid.UUID) error {
	return s.WithTenant(ctx, tenantID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE sessions SET last_seen_at = now() WHERE id = $1`, sessionID)
		return err
	})
}

func (s *Store) RevokeSession(ctx context.Context, tenantID, sessionID uuid.UUID) error {
	err := s.WithTenant(ctx, tenantID, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE sessions SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`, sessionID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}
