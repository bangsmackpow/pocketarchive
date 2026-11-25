package api

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/bangsmackpow/pocketarchive/engine/internal/store"
)

func (s *Server) handleCreateImport(w http.ResponseWriter, r *http.Request) {
	p := s.adminOnly(w, r)
	if p == nil {
		return
	}
	var req struct {
		Filename  string `json:"filename"`
		SizeBytes int64  `json:"size_bytes"`
	}
	if err := decodeJSON(w, r, &req, 16<<10); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "invalid request body")
		return
	}
	name := safeImportFilename(req.Filename)
	if name == "" {
		writeError(w, http.StatusBadRequest, "invalid_filename", "filename is required")
		return
	}
	ext := strings.ToLower(filepath.Ext(name))
	var kind string
	switch ext {
	case ".pst":
		kind = "pst"
	case ".mbox":
		kind = "mbox"
	default:
		writeError(w, http.StatusBadRequest, "invalid_file_type", "only .pst and .mbox files are supported")
		return
	}
	if req.SizeBytes < 0 || (s.Config.MaxUploadBytes > 0 && req.SizeBytes > s.Config.MaxUploadBytes) {
		writeError(w, http.StatusRequestEntityTooLarge, "file_too_large", "upload exceeds the configured maximum size")
		return
	}

	jobID := uuid.New()
	dir := filepath.Join(s.Config.SpoolPath, "imports", p.TenantID.String(), jobID.String())
	if err := os.MkdirAll(dir, 0o750); err != nil {
		s.Log.Error("create import spool", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not create upload workspace")
		return
	}
	job, err := s.Store.CreateImportJob(r.Context(), store.ImportJob{
		ID:               jobID,
		TenantID:         p.TenantID,
		Kind:             kind,
		OriginalFilename: name,
		SpoolPath:        filepath.Join(dir, name),
		SizeBytes:        req.SizeBytes,
		CreatedBy:        &p.UserID,
	})
	if err != nil {
		_ = os.RemoveAll(dir)
		s.Log.Error("create import job", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not create import job")
		return
	}
	s.auditRequest(r, p, "import.created", "import_job", job.ID.String(), map[string]any{
		"filename": name,
		"kind":     kind,
		"size":     req.SizeBytes,
	})
	writeJSON(w, http.StatusCreated, job)
}

func (s *Server) handleUploadChunk(w http.ResponseWriter, r *http.Request) {
	p := s.adminOnly(w, r)
	if p == nil {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "invalid import id")
		return
	}
	index, err := strconv.Atoi(r.PathValue("index"))
	if err != nil || index < 0 {
		writeError(w, http.StatusBadRequest, "invalid_chunk", "invalid chunk index")
		return
	}
	maxChunk := s.Config.MaxChunkBytes
	if maxChunk <= 0 {
		maxChunk = 32 << 20
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxChunk+1)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "chunk_too_large", "chunk exceeds the maximum size")
		return
	}
	if int64(len(data)) > maxChunk {
		writeError(w, http.StatusRequestEntityTooLarge, "chunk_too_large", "chunk exceeds the maximum size")
		return
	}
	if len(data) == 0 {
		writeError(w, http.StatusBadRequest, "empty_chunk", "chunk is empty")
		return
	}
	job, err := s.Store.AppendImportChunk(r.Context(), p.TenantID, id, index, data, s.Config.MaxUploadBytes)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			writeError(w, http.StatusConflict, "chunk_conflict", err.Error())
			return
		}
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id":              job.ID,
		"bytes_received":  job.BytesReceived,
		"chunks_received": job.ChunksReceived,
	})
}

func (s *Server) handleCompleteImport(w http.ResponseWriter, r *http.Request) {
	p := s.adminOnly(w, r)
	if p == nil {
		return
	}
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "invalid import id")
		return
	}
	job, err := s.Store.CompleteImport(r.Context(), p.TenantID, id)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			writeError(w, http.StatusConflict, "incomplete_upload", err.Error())
			return
		}
		storeError(w, err)
		return
	}
	s.auditRequest(r, p, "import.queued", "import_job", job.ID.String(), map[string]any{
		"filename": job.OriginalFilename,
		"size":     job.SizeBytes,
	})
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) handleListImports(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	jobs, err := s.Store.ListImportJobs(r.Context(), p.TenantID, limit)
	if err != nil {
		s.Log.Error("list import jobs", "error", err)
		writeError(w, http.StatusInternalServerError, "internal", "could not load import jobs")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"jobs": jobs})
}

func (s *Server) handleGetImport(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "invalid import id")
		return
	}
	job, err := s.Store.GetImportJob(r.Context(), p.TenantID, id)
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (s *Server) handleImportErrors(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r.Context())
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_id", "invalid import id")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	errs, err := s.Store.ListImportErrors(r.Context(), p.TenantID, id, limit)
	if err != nil {
		storeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"errors": errs})
}

func safeImportFilename(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 || r == '/' {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return ""
	}
	if len(name) > 200 {
		ext := filepath.Ext(name)
		name = name[:200-len(ext)] + ext
	}
	return name
}
