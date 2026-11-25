// Package archive ties blob encryption to PostgreSQL metadata and disk storage.
package archive

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/bangsmackpow/pocketarchive/engine/internal/crypto"
	"github.com/bangsmackpow/pocketarchive/engine/internal/storage"
	"github.com/bangsmackpow/pocketarchive/engine/internal/store"
)

type Service struct {
	Store  *store.Store
	Blobs  *storage.BlobStore
	Master *crypto.MasterKey
}

// Ensure encrypts plaintext into a content-addressed blob, reusing an existing
// ciphertext when the same tenant already stored identical bytes.
func (s *Service) Ensure(ctx context.Context, tenantID uuid.UUID, plaintext []byte) (sha256Hex string, duplicate bool, err error) {
	sum := sha256.Sum256(plaintext)
	sha := hex.EncodeToString(sum[:])

	err = s.Store.WithTenant(ctx, tenantID, func(tx pgx.Tx) error {
		// Serialize writers for this tenant/blob across engine instances.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, tenantID.String()+":"+sha); err != nil {
			return err
		}

		var wrappedDEK, nonce []byte
		var size int64
		dbErr := tx.QueryRow(ctx,
			`SELECT wrapped_dek, file_nonce, size_bytes FROM blobs WHERE sha256 = $1`, sha).Scan(&wrappedDEK, &nonce, &size)
		hasMeta := dbErr == nil
		if dbErr != nil && !errors.Is(dbErr, pgx.ErrNoRows) {
			return dbErr
		}

		onDisk, statErr := s.Blobs.Exists(tenantID, sha)
		if statErr != nil {
			return statErr
		}
		if hasMeta && onDisk {
			duplicate = true
			return nil
		}

		var wrappedKEK []byte
		if err := tx.QueryRow(ctx, `SELECT wrapped_kek FROM tenants WHERE id = $1`, tenantID).Scan(&wrappedKEK); err != nil {
			return fmt.Errorf("load tenant kek: %w", err)
		}
		kek, err := s.Master.UnwrapKEK(tenantID, wrappedKEK)
		if err != nil {
			return fmt.Errorf("unwrap tenant kek: %w", err)
		}

		enc, err := crypto.EncryptBlob(tenantID, sha, kek, plaintext)
		if err != nil {
			return err
		}
		if err := s.Blobs.Replace(tenantID, sha, enc.Ciphertext); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO blobs (tenant_id, sha256, size_bytes, wrapped_dek, file_nonce)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (tenant_id, sha256) DO UPDATE
			SET size_bytes = EXCLUDED.size_bytes,
			    wrapped_dek = EXCLUDED.wrapped_dek,
			    file_nonce = EXCLUDED.file_nonce`,
			tenantID, sha, int64(len(plaintext)), enc.WrappedDEK, enc.Nonce)
		return err
	})
	if err != nil {
		return "", false, err
	}
	return sha, duplicate, nil
}

// Read decrypts a blob into memory. Callers must enforce size limits.
func (s *Service) Read(ctx context.Context, tenantID uuid.UUID, sha string) ([]byte, error) {
	meta, err := s.Store.GetBlobMeta(ctx, tenantID, sha)
	if err != nil {
		return nil, err
	}
	var wrappedKEK []byte
	err = s.Store.WithTenant(ctx, tenantID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT wrapped_kek FROM tenants WHERE id = $1`, tenantID).Scan(&wrappedKEK)
	})
	if err != nil {
		return nil, fmt.Errorf("load tenant kek: %w", err)
	}
	kek, err := s.Master.UnwrapKEK(tenantID, wrappedKEK)
	if err != nil {
		return nil, fmt.Errorf("unwrap tenant kek: %w", err)
	}
	ciphertext, err := s.Blobs.ReadAll(tenantID, sha)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("blob %s is missing from storage", sha)
		}
		return nil, err
	}
	return crypto.DecryptBlob(tenantID, sha, kek, meta.WrappedDEK, meta.FileNonce, ciphertext)
}
