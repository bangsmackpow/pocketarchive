package store

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *Store) GetBlobMeta(ctx context.Context, tenantID uuid.UUID, sha256 string) (BlobMeta, error) {
	var b BlobMeta
	err := s.WithTenant(ctx, tenantID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT tenant_id, sha256, size_bytes, wrapped_dek, file_nonce, created_at
			FROM blobs WHERE sha256 = $1`, sha256).
			Scan(&b.TenantID, &b.SHA256, &b.SizeBytes, &b.WrappedDEK, &b.FileNonce, &b.CreatedAt)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return BlobMeta{}, ErrNotFound
	}
	return b, err
}
