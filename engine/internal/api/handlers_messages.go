package api

import (
	"fmt"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/bangsmackpow/pocketarchive/engine/internal/search"
	"github.com/bangsmackpow/pocketarchive/engine/internal/store"
)

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	if s.Search == nil {
		writeError(w, http.StatusServiceUnavailable, "search_unavailable", "search is not configured")
		return
	}
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 25
	}
	dateFrom, dateTo, err := parseDateRange(q.Get("date_from"), q.Get("date_to"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_date", "dates must be YYYY-MM-DD or RFC3339")
		return
	}
	var hasAttachment *bool
	if v := q.Get("has_attachment"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_filter", "has_attachment must be true or false")
			return
		}
		hasAttachment = &b
	}
	var legalHold *bool
	if v := q.Get("legal_hold"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_filter", "legal_hold must be true or false")
			return
		}
		legalHold = &b
	}
	var sources []string
	if v := q.Get("source"); v != "" {
		for _, part := range strings.Split(v, ",") {
			if part = strings.TrimSpace(part); part != "" {
				sources = append(sources, part)
			}
		}
	}

	req := search.SearchRequest{
		TenantID:      p.TenantID,
		Query:         strings.TrimSpace(q.Get("q")),
		From:          strings.TrimSpace(q.Get("from")),
		To:            strings.TrimSpace(q.Get("to")),
		DateFrom:      dateFrom,
		DateTo:        dateTo,
		HasAttachment: hasAttachment,
		SourceTypes:   sources,
		LegalHold:     legalHold,
		Page:          page,
		Limit:         limit,
	}
	res, err := s.Search.Search(r.Context(), req)
	if err != nil {
		if s.Log != nil {
			s.Log.Error("search failed", "error", err)
		}
		writeError(w, http.StatusBadGateway, "search_failed", "search backend unavailable")
		return
	}
	s.auditRequest(r, p, "search.query", "search", "", map[string]any{
		"q":              req.Query,
		"from":           req.From,
		"to":             req.To,
		"date_from":      q.Get("date_from"),
		"date_to":        q.Get("date_to"),
		"has_attachment": q.Get("has_attachment"),
		"source":         q.Get("source"),
		"legal_hold":     q.Get("legal_hold"),
		"results":        res.Total,
		"page":           page,
	})
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleGetMessage(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "invalid message id")
		return
	}
	msg, err := s.Store.GetMessage(r.Context(), p.TenantID, id)
	if err != nil {
		storeError(w, err)
		return
	}
	s.auditRequest(r, p, "message.view", "message", msg.ID.String(), map[string]any{"subject": msg.Subject})
	writeJSON(w, http.StatusOK, msg)
}

func (s *Server) handleDownloadAttachment(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	messageID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "invalid message id")
		return
	}
	attachmentID, err := uuid.Parse(r.PathValue("attachmentID"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "invalid attachment id")
		return
	}
	attachment, err := s.Store.GetAttachment(r.Context(), p.TenantID, messageID, attachmentID)
	if err != nil {
		storeError(w, err)
		return
	}
	plaintext, err := s.Archive.Read(r.Context(), p.TenantID, attachment.SHA256)
	if err != nil {
		s.Log.Error("decrypt attachment", "error", err, "sha256", attachment.SHA256)
		writeError(w, http.StatusInternalServerError, "decrypt_failed", "attachment could not be decrypted")
		return
	}
	s.auditRequest(r, p, "attachment.download", "attachment", attachment.ID.String(), map[string]any{
		"message_id": messageID.String(),
		"filename":   attachment.Filename,
		"sha256":     attachment.SHA256,
	})
	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": attachment.Filename})
	w.Header().Set("Content-Type", attachment.ContentType)
	w.Header().Set("Content-Disposition", disposition)
	w.Header().Set("Content-Length", strconv.Itoa(len(plaintext)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(plaintext)
}

func (s *Server) handleExportMessage(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "invalid message id")
		return
	}
	msg, err := s.Store.GetMessage(r.Context(), p.TenantID, id)
	if err != nil {
		storeError(w, err)
		return
	}
	raw, err := s.Archive.Read(r.Context(), p.TenantID, msg.RawBlobSHA256)
	if err != nil {
		s.Log.Error("decrypt message", "error", err, "message_id", msg.ID)
		writeError(w, http.StatusInternalServerError, "decrypt_failed", "message could not be decrypted")
		return
	}
	s.auditRequest(r, p, "message.export", "message", msg.ID.String(), map[string]any{"sha256": msg.RawSHA256})
	filename := fmt.Sprintf("message-%s.eml", msg.ID.String()[:8])
	w.Header().Set("Content-Type", "message/rfc822")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filename}))
	w.Header().Set("Content-Length", strconv.Itoa(len(raw)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

func (s *Server) handleLegalHold(w http.ResponseWriter, r *http.Request) {
	p := s.adminOnly(w, r)
	if p == nil {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "invalid message id")
		return
	}
	var req struct {
		Hold   bool   `json:"hold"`
		Reason string `json:"reason"`
	}
	if err := decodeJSON(w, r, &req, 16<<10); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid request body")
		return
	}
	req.Reason = strings.TrimSpace(req.Reason)
	if req.Hold && req.Reason == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "a reason is required when placing a message on legal hold")
		return
	}
	if _, err := s.Store.SetLegalHold(r.Context(), p.TenantID, id, p.UserID, req.Hold, req.Reason); err != nil {
		s.Log.Error("set legal hold", "error", err, "message_id", id, "hold", req.Hold)
		storeError(w, err)
		return
	}
	if s.Search != nil {
		if err := s.Search.UpdateLegalHold(r.Context(), id.String(), req.Hold); err != nil && s.Log != nil {
			s.Log.Warn("update legal hold search document", "error", err, "message_id", id)
			if markErr := s.Store.MarkSearchStale(r.Context(), p.TenantID, id); markErr != nil {
				s.Log.Warn("mark message for reindex", "error", markErr, "message_id", id)
			}
		}
	}
	action := "message.legal_hold.released"
	if req.Hold {
		action = "message.legal_hold.enabled"
	}
	s.auditRequest(r, p, action, "message", id.String(), map[string]any{"reason": req.Reason})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "legal_hold": req.Hold})
}

func (s *Server) handleDeleteMessage(w http.ResponseWriter, r *http.Request) {
	p := s.adminOnly(w, r)
	if p == nil {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "invalid message id")
		return
	}
	if err := s.Store.DeleteMessage(r.Context(), p.TenantID, id); err != nil {
		storeError(w, err)
		return
	}
	if s.Search != nil {
		if err := s.Search.DeleteDocument(r.Context(), id.String()); err != nil && s.Log != nil {
			s.Log.Warn("delete search document", "error", err, "message_id", id)
		}
	}
	s.auditRequest(r, p, "message.deleted", "message", id.String(), nil)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleAuditLogs(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	from, to, err := parseDateRange(q.Get("date_from"), q.Get("date_to"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_date", "dates must be YYYY-MM-DD or RFC3339")
		return
	}
	logs, err := s.Store.ListAudit(r.Context(), p.TenantID, store.AuditFilter{
		Action: strings.TrimSpace(q.Get("action")),
		Actor:  strings.TrimSpace(q.Get("actor")),
		From:   from,
		To:     to,
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		s.Log.Error("list audit logs", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not load audit logs")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"logs": logs, "limit": limit, "offset": offset})
}
