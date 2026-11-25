package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *Store) InsertAudit(ctx context.Context, e AuditEntry) error {
	return s.WithTenant(ctx, e.TenantID, func(tx pgx.Tx) error {
		return InsertAuditTx(ctx, tx, e)
	})
}

// InsertAuditTx appends an audit entry inside an existing tenant transaction.
func InsertAuditTx(ctx context.Context, tx pgx.Tx, e AuditEntry) error {
	detail := e.Detail
	if detail == nil {
		detail = map[string]any{}
	}
	raw, err := json.Marshal(detail)
	if err != nil {
		return fmt.Errorf("marshal audit detail: %w", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO audit_logs
			(tenant_id, actor_user_id, actor_email, action, target_type, target_id, ip, user_agent, detail)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		e.TenantID, e.ActorUserID, e.ActorEmail, e.Action, e.TargetType, e.TargetID, e.IP, e.UserAgent, raw)
	return err
}

type AuditFilter struct {
	Action string
	Actor  string
	From   *time.Time
	To     *time.Time
	Limit  int
	Offset int
}

func (s *Store) ListAudit(ctx context.Context, tenantID uuid.UUID, f AuditFilter) ([]AuditLog, error) {
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	if f.Offset < 0 {
		f.Offset = 0
	}
	var out []AuditLog
	err := s.WithTenant(ctx, tenantID, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id, tenant_id, actor_user_id, actor_email, action, target_type, target_id,
			       ip, user_agent, detail, created_at
			FROM audit_logs
			WHERE ($1 = '' OR action ILIKE '%' || $1 || '%')
			  AND ($2 = '' OR actor_email ILIKE '%' || $2 || '%')
			  AND ($3::timestamptz IS NULL OR created_at >= $3)
			  AND ($4::timestamptz IS NULL OR created_at < $4)
			ORDER BY id DESC
			LIMIT $5 OFFSET $6`,
			f.Action, f.Actor, f.From, f.To, f.Limit, f.Offset)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var a AuditLog
			var detail []byte
			if err := rows.Scan(&a.ID, &a.TenantID, &a.ActorUserID, &a.ActorEmail, &a.Action,
				&a.TargetType, &a.TargetID, &a.IP, &a.UserAgent, &detail, &a.CreatedAt); err != nil {
				return err
			}
			_ = json.Unmarshal(detail, &a.Detail)
			out = append(out, a)
		}
		return rows.Err()
	})
	return out, err
}
