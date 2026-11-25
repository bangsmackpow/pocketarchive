package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/bangsmackpow/pocketarchive/engine/internal/store"
)

func (s *Server) handleListSources(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	sources, err := s.Store.ListSources(r.Context(), p.TenantID)
	if err != nil {
		s.Log.Error("list sources", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not load sources")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sources": sources})
}

func (s *Server) handleCreateSource(w http.ResponseWriter, r *http.Request) {
	p := s.adminOnly(w, r)
	if p == nil {
		return
	}
	var req struct {
		Name    string         `json:"name"`
		Kind    string         `json:"kind"`
		Enabled bool           `json:"enabled"`
		Config  map[string]any `json:"config"`
	}
	if err := decodeJSON(w, r, &req, 64<<10); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid request body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}
	switch req.Kind {
	case "journal", "pst", "mbox", "eml":
	default:
		writeError(w, http.StatusBadRequest, "invalid_kind", "kind must be journal, pst, mbox or eml")
		return
	}
	src, err := s.Store.CreateSource(r.Context(), store.IngestionSource{
		TenantID: p.TenantID,
		Name:     req.Name,
		Kind:     req.Kind,
		Config:   req.Config,
		Enabled:  req.Enabled,
	})
	if err != nil {
		s.Log.Error("create source", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not create source")
		return
	}
	s.auditRequest(r, p, "source.created", "source", src.ID.String(), map[string]any{"name": src.Name, "kind": src.Kind})
	writeJSON(w, http.StatusCreated, src)
}

func (s *Server) handleUpdateSource(w http.ResponseWriter, r *http.Request) {
	p := s.adminOnly(w, r)
	if p == nil {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "invalid source id")
		return
	}
	var req struct {
		Name    string         `json:"name"`
		Enabled bool           `json:"enabled"`
		Config  map[string]any `json:"config"`
	}
	if err := decodeJSON(w, r, &req, 64<<10); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid request body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "name is required")
		return
	}
	src, err := s.Store.UpdateSource(r.Context(), p.TenantID, id, req.Name, req.Enabled, req.Config)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "source not found")
			return
		}
		s.Log.Error("update source", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not update source")
		return
	}
	s.auditRequest(r, p, "source.updated", "source", src.ID.String(), map[string]any{"name": src.Name, "enabled": src.Enabled})
	writeJSON(w, http.StatusOK, src)
}

func (s *Server) handleDeleteSource(w http.ResponseWriter, r *http.Request) {
	p := s.adminOnly(w, r)
	if p == nil {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "invalid source id")
		return
	}
	if err := s.Store.DeleteSource(r.Context(), p.TenantID, id); err != nil {
		storeError(w, err)
		return
	}
	s.auditRequest(r, p, "source.deleted", "source", id.String(), nil)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	stats, err := s.Store.Stats(r.Context(), p.TenantID)
	if err != nil {
		s.Log.Error("load stats", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not load stats")
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	tenant, err := s.Store.GetTenantByID(r.Context(), p.TenantID)
	if err != nil {
		storeError(w, err)
		return
	}
	tlsMode := "automatic-self-signed"
	if s.Config.SMTPTLSCertFile != "" && s.Config.SMTPTLSKeyFile != "" {
		tlsMode = "mounted-certificate"
	}
	domains := s.Config.SMTPAllowedDomains
	if domains == nil {
		domains = []string{}
	}
	ips := s.Config.SMTPAllowedCIDRs
	if ips == nil {
		ips = []string{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"smtp": map[string]any{
			"hostname":          s.Config.SMTPHostname,
			"port":              2525,
			"allowed_domains":   domains,
			"allowed_ips":       ips,
			"auth_enabled":      s.Config.SMTPAuthUsername != "",
			"tls_mode":          tlsMode,
			"max_message_bytes": s.Config.SMTPMaxBytes,
		},
		"tenant": map[string]any{
			"slug": tenant.Slug,
			"name": tenant.Name,
		},
		"journaling": map[string]any{
			"provider":  "microsoft365",
			"address":   "journal@" + s.Config.SMTPHostname,
			"transport": "SMTP with STARTTLS",
		},
	})
}
