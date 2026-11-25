package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

const testSecret = "0123456789abcdef0123456789abcdef"

func TestPasswordHash(t *testing.T) {
	hash, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword(hash, "correct horse battery") {
		t.Fatal("valid password rejected")
	}
	if VerifyPassword(hash, "wrong") {
		t.Fatal("invalid password accepted")
	}
	if _, err := HashPassword("short"); err == nil {
		t.Fatal("expected minimum-length error")
	}
}

func TestJWTIssueParse(t *testing.T) {
	m, err := NewManager(testSecret, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	userID, tenantID, sessionID := uuid.New(), uuid.New(), uuid.New()
	token, expires, err := m.Issue(userID, tenantID, sessionID, "admin@example.com", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if !expires.After(time.Now()) {
		t.Fatal("token already expired")
	}
	claims, err := m.Parse(token)
	if err != nil {
		t.Fatal(err)
	}
	if claims.Subject != userID.String() || claims.TenantID != tenantID.String() || claims.SessionID != sessionID.String() {
		t.Fatalf("unexpected claims: %+v", claims)
	}
	if !strings.Contains(token, ".") {
		t.Fatal("not a JWT")
	}
}

func TestJWTRejectsWrongSecret(t *testing.T) {
	m1, _ := NewManager(testSecret, time.Hour)
	m2, _ := NewManager("fedcba9876543210fedcba9876543210", time.Hour)
	token, _, err := m1.Issue(uuid.New(), uuid.New(), uuid.New(), "a@b.c", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m2.Parse(token); err == nil {
		t.Fatal("token accepted with wrong secret")
	}
}

func TestSessionTokenHash(t *testing.T) {
	m, _ := NewManager(testSecret, time.Hour)
	token, err := NewSessionToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(token) < 40 {
		t.Fatalf("session token too short: %d", len(token))
	}
	if string(m.HashSessionToken(token)) == string(m.HashSessionToken(token+"x")) {
		t.Fatal("token hash collision")
	}
}
