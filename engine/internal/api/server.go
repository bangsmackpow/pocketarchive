// Package api implements the engine's internal REST API.
package api

import (
	"context"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/bangsmackpow/pocketarchive/engine/internal/archive"
	"github.com/bangsmackpow/pocketarchive/engine/internal/auth"
	"github.com/bangsmackpow/pocketarchive/engine/internal/config"
	"github.com/bangsmackpow/pocketarchive/engine/internal/ratelimit"
	"github.com/bangsmackpow/pocketarchive/engine/internal/search"
	"github.com/bangsmackpow/pocketarchive/engine/internal/store"
)

type Server struct {
	Config  config.Config
	Store   *store.Store
	Search  *search.Client
	Archive *archive.Service
	Auth    *auth.Manager
	Limiter *ratelimit.Limiter
	Log     *slog.Logger
}

func New(cfg config.Config, st *store.Store, sc *search.Client, ar *archive.Service, am *auth.Manager, limiter *ratelimit.Limiter, log *slog.Logger) *Server {
	return &Server{Config: cfg, Store: st, Search: sc, Archive: ar, Auth: am, Limiter: limiter, Log: log}
}

func (s *Server) Handler() http.Handler {
	specific := http.NewServeMux()
	specific.HandleFunc("GET /healthz", s.handleHealth)
	specific.HandleFunc("GET /readyz", s.handleReady)
	specific.HandleFunc("POST /api/v1/auth/login", s.handleLogin)

	protected := http.NewServeMux()
	protected.HandleFunc("GET /api/v1/auth/me", s.handleMe)
	protected.HandleFunc("POST /api/v1/auth/logout", s.handleLogout)
	protected.HandleFunc("GET /api/v1/tenant", s.handleTenant)
	protected.HandleFunc("PUT /api/v1/tenant/settings", s.handleUpdateSettings)
	protected.HandleFunc("GET /api/v1/setup", s.handleSetup)
	protected.HandleFunc("GET /api/v1/stats", s.handleStats)
	protected.HandleFunc("GET /api/v1/sources", s.handleListSources)
	protected.HandleFunc("POST /api/v1/sources", s.handleCreateSource)
	protected.HandleFunc("PATCH /api/v1/sources/{id}", s.handleUpdateSource)
	protected.HandleFunc("DELETE /api/v1/sources/{id}", s.handleDeleteSource)
	protected.HandleFunc("POST /api/v1/imports", s.handleCreateImport)
	protected.HandleFunc("GET /api/v1/imports", s.handleListImports)
	protected.HandleFunc("GET /api/v1/imports/{id}", s.handleGetImport)
	protected.HandleFunc("PUT /api/v1/imports/{id}/chunks/{index}", s.handleUploadChunk)
	protected.HandleFunc("POST /api/v1/imports/{id}/complete", s.handleCompleteImport)
	protected.HandleFunc("GET /api/v1/imports/{id}/errors", s.handleImportErrors)
	protected.HandleFunc("GET /api/v1/search", s.handleSearch)
	protected.HandleFunc("GET /api/v1/messages/{id}", s.handleGetMessage)
	protected.HandleFunc("DELETE /api/v1/messages/{id}", s.handleDeleteMessage)
	protected.HandleFunc("GET /api/v1/messages/{id}/export", s.handleExportMessage)
	protected.HandleFunc("GET /api/v1/messages/{id}/attachments/{attachmentID}", s.handleDownloadAttachment)
	protected.HandleFunc("POST /api/v1/messages/{id}/legal-hold", s.handleLegalHold)
	protected.HandleFunc("GET /api/v1/audit-logs", s.handleAuditLogs)

	specific.Handle("/api/v1/", s.requireAuth(protected))
	return s.recoverer(s.logRequests(s.securityHeaders(specific)))
}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			writeError(w, http.StatusUnauthorized, "unauthorized", "missing bearer token")
			return
		}
		claims, err := s.Auth.Parse(strings.TrimSpace(strings.TrimPrefix(header, "Bearer ")))
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "invalid or expired token")
			return
		}
		tenantID, _ := uuid.Parse(claims.TenantID)
		sessionID, _ := uuid.Parse(claims.SessionID)
		userID, _ := uuid.Parse(claims.Subject)
		active, err := s.Store.SessionActive(r.Context(), tenantID, sessionID, userID, time.Now())
		if err != nil {
			s.Log.Error("check session", "error", err)
			writeError(w, http.StatusInternalServerError, "internal", "session check failed")
			return
		}
		if !active {
			writeError(w, http.StatusUnauthorized, "unauthorized", "session revoked or expired")
			return
		}
		p := &principal{
			UserID:    userID,
			TenantID:  tenantID,
			SessionID: sessionID,
			Email:     claims.Email,
			Role:      claims.Role,
		}
		next.ServeHTTP(w, r.WithContext(withPrincipal(r.Context(), p)))
	})
}

type principal struct {
	UserID    uuid.UUID
	TenantID  uuid.UUID
	SessionID uuid.UUID
	Email     string
	Role      string
}

type principalKey struct{}

func withPrincipal(ctx context.Context, p *principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

func principalFrom(ctx context.Context) *principal {
	p, _ := ctx.Value(principalKey{}).(*principal)
	return p
}

func (s *Server) adminOnly(w http.ResponseWriter, r *http.Request) *principal {
	p := principalFrom(r.Context())
	if p == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return nil
	}
	if p.Role != "admin" {
		writeError(w, http.StatusForbidden, "forbidden", "administrator role required")
		return nil
	}
	return p
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(sw, r)
		if s.Log != nil {
			s.Log.Info("http request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", sw.status,
				"duration_ms", time.Since(start).Milliseconds(),
				"remote", clientIP(r))
		}
	})
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				if s.Log != nil {
					s.Log.Error("panic serving request", "panic", rec, "stack", string(debug.Stack()))
				}
				writeError(w, http.StatusInternalServerError, "internal", "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func clientIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		first := strings.TrimSpace(strings.Split(forwarded, ",")[0])
		if first != "" {
			return first
		}
	}
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i > 0 {
		host = host[:i]
	}
	return host
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "service": "pocketarchive-engine"})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	checks := map[string]string{"database": "ok", "search": "ok"}
	status := http.StatusOK
	if err := s.Store.Ping(ctx); err != nil {
		checks["database"] = err.Error()
		status = http.StatusServiceUnavailable
	}
	if s.Search != nil {
		if err := s.Search.Health(ctx); err != nil {
			checks["search"] = err.Error()
			status = http.StatusServiceUnavailable
		}
	}
	if s.Limiter != nil {
		if err := s.Limiter.Ping(ctx); err != nil {
			checks["valkey"] = "degraded: " + err.Error()
		} else {
			checks["valkey"] = "ok"
		}
	}
	writeJSON(w, status, map[string]any{"status": map[bool]string{true: "ok", false: "degraded"}[status == http.StatusOK], "checks": checks})
}
