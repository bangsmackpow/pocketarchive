// Package auth provides password hashing, session token handling and JWTs.
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

const (
	issuer     = "pocketarchive"
	bcryptCost = 12
)

func HashPassword(password string) (string, error) {
	if len(password) < 12 {
		return "", errors.New("password must be at least 12 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

func VerifyPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// Manager issues and validates HS256 access tokens.
type Manager struct {
	secret []byte
	ttl    time.Duration
}

func NewManager(secret string, ttl time.Duration) (*Manager, error) {
	if len(secret) < 32 {
		return nil, errors.New("SESSION_SECRET must be at least 32 characters")
	}
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}
	return &Manager{secret: []byte(secret), ttl: ttl}, nil
}

type Claims struct {
	TenantID  string `json:"tid"`
	SessionID string `json:"sid"`
	Email     string `json:"email"`
	Role      string `json:"role"`
	jwt.RegisteredClaims
}

func (m *Manager) TTL() time.Duration { return m.ttl }

func (m *Manager) Issue(userID, tenantID, sessionID uuid.UUID, email, role string) (string, time.Time, error) {
	now := time.Now()
	expires := now.Add(m.ttl)
	claims := Claims{
		TenantID:  tenantID.String(),
		SessionID: sessionID.String(),
		Email:     email,
		Role:      role,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   userID.String(),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now.Add(-30 * time.Second)),
			ExpiresAt: jwt.NewNumericDate(expires),
			ID:        uuid.NewString(),
		},
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign token: %w", err)
	}
	return signed, expires, nil
}

func (m *Manager) Parse(token string) (*Claims, error) {
	claims := &Claims{}
	parsed, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		if t.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, fmt.Errorf("unexpected signing method %q", t.Method.Alg())
		}
		return m.secret, nil
	}, jwt.WithIssuer(issuer), jwt.WithExpirationRequired())
	if err != nil {
		return nil, err
	}
	if !parsed.Valid {
		return nil, errors.New("invalid token")
	}
	if _, err := uuid.Parse(claims.TenantID); err != nil {
		return nil, errors.New("invalid tenant claim")
	}
	if _, err := uuid.Parse(claims.SessionID); err != nil {
		return nil, errors.New("invalid session claim")
	}
	if _, err := uuid.Parse(claims.Subject); err != nil {
		return nil, errors.New("invalid subject claim")
	}
	return claims, nil
}

// NewSessionToken returns an opaque random token stored in the user's cookie.
func NewSessionToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// HashSessionToken peppers the opaque token with SESSION_SECRET before storage.
func (m *Manager) HashSessionToken(token string) []byte {
	mac := hmac.New(sha256.New, m.secret)
	mac.Write([]byte(token))
	return mac.Sum(nil)
}
