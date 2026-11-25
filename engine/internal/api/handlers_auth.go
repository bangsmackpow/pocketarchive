package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/bangsmackpow/pocketarchive/engine/internal/auth"
	"github.com/bangsmackpow/pocketarchive/engine/internal/store"
)

type tenantView struct {
	ID            uuid.UUID `json:"id"`
	Slug          string    `json:"slug"`
	Name          string    `json:"name"`
	RetentionDays int       `json:"retention_days"`
}

func viewTenant(t store.Tenant) tenantView {
	return tenantView{ID: t.ID, Slug: t.Slug, Name: t.Name, RetentionDays: t.RetentionDays}
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email      string `json:"email"`
		Password   string `json:"password"`
		TenantSlug string `json:"tenant_slug"`
	}
	if err := decodeJSON(w, r, &req, 64<<10); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid request body")
		return
	}
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	slug := strings.TrimSpace(req.TenantSlug)
	if slug == "" {
		slug = s.Config.DefaultTenantSlug
	}
	ip := clientIP(r)
	if req.Email == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "email and password are required")
		return
	}
	if s.Limiter != nil && !s.Limiter.Allow(r.Context(), "login:"+ip+":"+req.Email) {
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many login attempts; try again later")
		return
	}

	tenant, err := s.Store.GetTenantBySlug(r.Context(), slug)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "invalid email or password")
		return
	}
	if err != nil {
		s.Log.Error("resolve tenant for login", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "login failed")
		return
	}

	user, err := s.Store.GetUserByEmail(r.Context(), tenant.ID, req.Email)
	if err != nil || !user.IsActive || !auth.VerifyPassword(user.PasswordHash, req.Password) {
		_ = s.Store.InsertAudit(r.Context(), store.AuditEntry{
			TenantID:   tenant.ID,
			ActorEmail: req.Email,
			Action:     "auth.login.failed",
			TargetType: "user",
			TargetID:   req.Email,
			IP:         ip,
			UserAgent:  r.UserAgent(),
		})
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "invalid email or password")
		return
	}

	sessionToken, err := auth.NewSessionToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not create session")
		return
	}
	sessionID := uuid.New()
	expires := time.Now().Add(s.Auth.TTL())
	if err := s.Store.CreateSession(r.Context(), store.Session{
		ID:        sessionID,
		TenantID:  tenant.ID,
		UserID:    user.ID,
		TokenHash: s.Auth.HashSessionToken(sessionToken),
		ExpiresAt: expires,
		IP:        ip,
		UserAgent: r.UserAgent(),
	}); err != nil {
		s.Log.Error("create session", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not create session")
		return
	}
	token, expiresAt, err := s.Auth.Issue(user.ID, tenant.ID, sessionID, user.Email, user.Role)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not issue token")
		return
	}
	_ = s.Store.UpdateLastLogin(r.Context(), tenant.ID, user.ID)
	_ = s.Store.InsertAudit(r.Context(), store.AuditEntry{
		TenantID:    tenant.ID,
		ActorUserID: &user.ID,
		ActorEmail:  user.Email,
		Action:      "auth.login.success",
		TargetType:  "user",
		TargetID:    user.ID.String(),
		IP:          ip,
		UserAgent:   r.UserAgent(),
	})
	if s.Limiter != nil {
		s.Limiter.Reset(r.Context(), "login:"+ip+":"+req.Email)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"token":      token,
		"expires_at": expiresAt,
		"user": map[string]any{
			"id":    user.ID,
			"email": user.Email,
			"role":  user.Role,
		},
		"tenant": viewTenant(tenant),
	})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	tenant, err := s.Store.GetTenantByID(r.Context(), p.TenantID)
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user": map[string]any{
			"id":    p.UserID,
			"email": p.Email,
			"role":  p.Role,
		},
		"tenant": viewTenant(tenant),
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	if err := s.Store.RevokeSession(r.Context(), p.TenantID, p.SessionID); err != nil {
		s.Log.Error("revoke session", "error", err)
	}
	s.audit(r.Context(), p, "auth.logout", "session", p.SessionID.String(), nil)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleTenant(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	tenant, err := s.Store.GetTenantByID(r.Context(), p.TenantID)
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, viewTenant(tenant))
}

func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	p := s.adminOnly(w, r)
	if p == nil {
		return
	}
	var req struct {
		RetentionDays int `json:"retention_days"`
	}
	if err := decodeJSON(w, r, &req, 16<<10); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid request body")
		return
	}
	if req.RetentionDays < 1 || req.RetentionDays > 36500 {
		writeError(w, http.StatusBadRequest, "invalid_request", "retention_days must be between 1 and 36500")
		return
	}
	if err := s.Store.UpdateRetention(r.Context(), p.TenantID, req.RetentionDays); err != nil {
		storeError(w, err)
		return
	}
	s.audit(r.Context(), p, "retention.updated", "tenant", p.TenantID.String(), map[string]any{"retention_days": req.RetentionDays})
	tenant, err := s.Store.GetTenantByID(r.Context(), p.TenantID)
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, viewTenant(tenant))
}

func (s *Server) audit(ctx context.Context, p *principal, action, targetType, targetID string, detail map[string]any) {
	if p == nil {
		return
	}
	entry := store.AuditEntry{
		TenantID:    p.TenantID,
		ActorUserID: &p.UserID,
		ActorEmail:  p.Email,
		Action:      action,
		TargetType:  targetType,
		TargetID:    targetID,
		Detail:      detail,
	}
	if err := s.Store.InsertAudit(ctx, entry); err != nil && s.Log != nil {
		s.Log.Error("write audit log", "action", action, "error", err)
	}
}

func (s *Server) auditRequest(r *http.Request, p *principal, action, targetType, targetID string, detail map[string]any) {
	if p == nil {
		return
	}
	entry := store.AuditEntry{
		TenantID:    p.TenantID,
		ActorUserID: &p.UserID,
		ActorEmail:  p.Email,
		Action:      action,
		TargetType:  targetType,
		TargetID:    targetID,
		IP:          clientIP(r),
		UserAgent:   r.UserAgent(),
		Detail:      detail,
	}
	if err := s.Store.InsertAudit(r.Context(), entry); err != nil && s.Log != nil {
		s.Log.Error("write audit log", "action", action, "error", err)
	}
}
