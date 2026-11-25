// Package crypto implements the envelope-encryption scheme used for archived
// blobs. The master key never leaves the engine process and tenant data keys
// are wrapped with AES-256-GCM.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

const (
	KeySize   = 32
	nonceSize = 12
)

var (
	ErrInvalidMasterKey = errors.New("master key must be 32 bytes encoded as base64")
	ErrDecrypt          = errors.New("ciphertext authentication failed")
)

// MasterKey is the Key Encryption Key (KEK) loaded from ARCHIVE_MASTER_KEK.
type MasterKey struct {
	key []byte
}

// ParseMasterKey decodes a base64-encoded 32-byte master key. Standard, raw
// standard and URL-safe base64 are accepted to make key generation painless.
func ParseMasterKey(encoded string) (*MasterKey, error) {
	if encoded == "" {
		return nil, ErrInvalidMasterKey
	}
	encoders := []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	}
	var lastErr error
	for _, enc := range encoders {
		raw, err := enc.DecodeString(encoded)
		if err != nil {
			lastErr = err
			continue
		}
		if len(raw) != KeySize {
			return nil, ErrInvalidMasterKey
		}
		key := make([]byte, KeySize)
		copy(key, raw)
		return &MasterKey{key: key}, nil
	}
	return nil, fmt.Errorf("%w: %v", ErrInvalidMasterKey, lastErr)
}

// NewDataKey generates a random 32-byte key for a tenant or a blob.
func NewDataKey() ([]byte, error) {
	key := make([]byte, KeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generate data key: %w", err)
	}
	return key, nil
}

func tenantAAD(tenantID uuid.UUID) []byte {
	return []byte("tenant:" + tenantID.String())
}

// WrapKEK encrypts a tenant KEK with the master key. The tenant ID is bound as
// additional authenticated data.
func (m *MasterKey) WrapKEK(tenantID uuid.UUID, kek []byte) ([]byte, error) {
	if len(kek) != KeySize {
		return nil, fmt.Errorf("tenant KEK must be %d bytes", KeySize)
	}
	return seal(m.key, kek, tenantAAD(tenantID))
}

// UnwrapKEK decrypts a tenant KEK stored in the tenants table.
func (m *MasterKey) UnwrapKEK(tenantID uuid.UUID, wrapped []byte) ([]byte, error) {
	return open(m.key, wrapped, tenantAAD(tenantID))
}

// BlobCiphertext is the result of encrypting one blob. WrappedDEK and Nonce are
// persisted in PostgreSQL; Ciphertext is written to the blob store.
type BlobCiphertext struct {
	WrappedDEK []byte
	Nonce      []byte
	Ciphertext []byte
}

func blobAAD(tenantID uuid.UUID, sha256 string) []byte {
	return []byte(tenantID.String() + ":" + sha256)
}

// EncryptBlob generates a fresh blob DEK and encrypts plaintext with AES-256-GCM.
// The blob SHA-256 and tenant ID are bound as AAD so a blob cannot be moved
// between tenants or renamed without detection.
func EncryptBlob(tenantID uuid.UUID, sha256 string, tenantKEK, plaintext []byte) (*BlobCiphertext, error) {
	if len(tenantKEK) != KeySize {
		return nil, fmt.Errorf("tenant KEK must be %d bytes", KeySize)
	}
	dek, err := NewDataKey()
	if err != nil {
		return nil, err
	}
	wrapped, err := seal(tenantKEK, dek, blobAAD(tenantID, sha256))
	if err != nil {
		return nil, fmt.Errorf("wrap blob key: %w", err)
	}
	nonce, ciphertext, err := sealWithNonce(dek, plaintext, blobAAD(tenantID, sha256))
	if err != nil {
		return nil, fmt.Errorf("encrypt blob: %w", err)
	}
	return &BlobCiphertext{WrappedDEK: wrapped, Nonce: nonce, Ciphertext: ciphertext}, nil
}

// DecryptBlob recovers a blob using the tenant KEK and wrapped per-blob DEK.
func DecryptBlob(tenantID uuid.UUID, sha256 string, tenantKEK, wrappedDEK, nonce, ciphertext []byte) ([]byte, error) {
	if len(tenantKEK) != KeySize {
		return nil, fmt.Errorf("tenant KEK must be %d bytes", KeySize)
	}
	dek, err := open(tenantKEK, wrappedDEK, blobAAD(tenantID, sha256))
	if err != nil {
		return nil, fmt.Errorf("unwrap blob key: %w", err)
	}
	combined := make([]byte, 0, len(nonce)+len(ciphertext))
	combined = append(combined, nonce...)
	combined = append(combined, ciphertext...)
	return open(dek, combined, blobAAD(tenantID, sha256))
}

func seal(key, plaintext, aad []byte) ([]byte, error) {
	nonce, sealed, err := sealWithNonce(key, plaintext, aad)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(nonce)+len(sealed))
	out = append(out, nonce...)
	out = append(out, sealed...)
	return out, nil
}

func sealWithNonce(key, plaintext, aad []byte) ([]byte, []byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, err
	}
	return nonce, gcm.Seal(nil, nonce, plaintext, aad), nil
}

func open(key, sealed, aad []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(sealed) < gcm.NonceSize() {
		return nil, ErrDecrypt
	}
	nonce, ciphertext := sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, ErrDecrypt
	}
	return plaintext, nil
}
