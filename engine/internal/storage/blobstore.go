package storage

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"github.com/google/uuid"
)

var shaPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// BlobStore persists encrypted blob ciphertext under
// tenants/{tenant_id}/blobs/{sha256[:2]}/{sha256}.
type BlobStore struct {
	root string
}

func NewBlobStore(root string) (*BlobStore, error) {
	if root == "" {
		return nil, errors.New("blob storage path is empty")
	}
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, fmt.Errorf("create blob storage: %w", err)
	}
	return &BlobStore{root: root}, nil
}

// Path validates identifiers before building a filesystem path.
func (s *BlobStore) Path(tenantID uuid.UUID, sha256 string) (string, error) {
	if tenantID == uuid.Nil {
		return "", errors.New("tenant id is required")
	}
	if !shaPattern.MatchString(sha256) {
		return "", errors.New("invalid blob sha256")
	}
	return filepath.Join(s.root, "tenants", tenantID.String(), "blobs", sha256[:2], sha256), nil
}

func (s *BlobStore) Exists(tenantID uuid.UUID, sha256 string) (bool, error) {
	p, err := s.Path(tenantID, sha256)
	if err != nil {
		return false, err
	}
	st, err := os.Stat(p)
	if err == nil {
		return st.Mode().IsRegular(), nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

// Write stores ciphertext atomically. Existing blobs are never overwritten.
func (s *BlobStore) Write(tenantID uuid.UUID, sha256 string, ciphertext []byte) error {
	return s.write(tenantID, sha256, ciphertext, false)
}

// Replace atomically writes ciphertext even if a stale file exists.
func (s *BlobStore) Replace(tenantID uuid.UUID, sha256 string, ciphertext []byte) error {
	return s.write(tenantID, sha256, ciphertext, true)
}

func (s *BlobStore) write(tenantID uuid.UUID, sha256 string, ciphertext []byte, replace bool) error {
	p, err := s.Path(tenantID, sha256)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		return fmt.Errorf("create blob directory: %w", err)
	}
	if !replace {
		if _, err := os.Stat(p); err == nil {
			return nil
		}
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".blob-*")
	if err != nil {
		return fmt.Errorf("create temporary blob: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := tmp.Chmod(0o640); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(ciphertext); err != nil {
		tmp.Close()
		return fmt.Errorf("write blob: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, p); err != nil {
		return fmt.Errorf("commit blob: %w", err)
	}
	return nil
}

func (s *BlobStore) Open(tenantID uuid.UUID, sha256 string) (io.ReadCloser, error) {
	p, err := s.Path(tenantID, sha256)
	if err != nil {
		return nil, err
	}
	return os.Open(p)
}

func (s *BlobStore) ReadAll(tenantID uuid.UUID, sha256 string) ([]byte, error) {
	f, err := s.Open(tenantID, sha256)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}
