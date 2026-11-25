package crypto

import (
	"bytes"
	"encoding/base64"
	"testing"

	"github.com/google/uuid"
)

func testKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, KeySize)
	for i := range key {
		key[i] = byte(i + 1)
	}
	return key
}

func TestParseMasterKey(t *testing.T) {
	key := testKey(t)
	encoded := base64.StdEncoding.EncodeToString(key)
	m, err := ParseMasterKey(encoded)
	if err != nil {
		t.Fatalf("ParseMasterKey: %v", err)
	}
	if !bytes.Equal(m.key, key) {
		t.Fatal("decoded key mismatch")
	}
	if _, err := ParseMasterKey(base64.StdEncoding.EncodeToString([]byte("short"))); err == nil {
		t.Fatal("expected short key error")
	}
	if _, err := ParseMasterKey("not-base64!"); err == nil {
		t.Fatal("expected base64 error")
	}
}

func TestWrapUnwrapTenantKEK(t *testing.T) {
	master, err := ParseMasterKey(base64.StdEncoding.EncodeToString(testKey(t)))
	if err != nil {
		t.Fatal(err)
	}
	tenantID := uuid.New()
	kek := testKey(t)
	wrapped, err := master.WrapKEK(tenantID, kek)
	if err != nil {
		t.Fatal(err)
	}
	got, err := master.UnwrapKEK(tenantID, wrapped)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, kek) {
		t.Fatal("unwrapped KEK mismatch")
	}
	if _, err := master.UnwrapKEK(uuid.New(), wrapped); err == nil {
		t.Fatal("expected tenant AAD mismatch")
	}
}

func TestEncryptDecryptBlob(t *testing.T) {
	tenantID := uuid.New()
	sha := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	kek := testKey(t)
	plaintext := []byte("Subject: hello\r\n\r\nbody with secret data")

	enc, err := EncryptBlob(tenantID, sha, kek, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(enc.Ciphertext, plaintext) {
		t.Fatal("ciphertext contains plaintext")
	}
	got, err := DecryptBlob(tenantID, sha, kek, enc.WrappedDEK, enc.Nonce, enc.Ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatal("roundtrip mismatch")
	}
	if _, err := DecryptBlob(uuid.New(), sha, kek, enc.WrappedDEK, enc.Nonce, enc.Ciphertext); err == nil {
		t.Fatal("expected tenant AAD failure")
	}
	otherSHA := "1123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if _, err := DecryptBlob(tenantID, otherSHA, kek, enc.WrappedDEK, enc.Nonce, enc.Ciphertext); err == nil {
		t.Fatal("expected sha AAD failure")
	}
	// Tampering must fail authentication.
	tampered := append([]byte(nil), enc.Ciphertext...)
	tampered[0] ^= 0xff
	if _, err := DecryptBlob(tenantID, sha, kek, enc.WrappedDEK, enc.Nonce, tampered); err == nil {
		t.Fatal("expected tamper detection")
	}
}
