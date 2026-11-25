package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/bangsmackpow/pocketarchive/engine/internal/store"
)

type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	var body errorBody
	body.Error.Code = code
	body.Error.Message = message
	writeJSON(w, status, body)
}

func storeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "resource not found")
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, "conflict", err.Error())
	case errors.Is(err, store.ErrLegalHold):
		writeError(w, http.StatusConflict, "legal_hold", "message is under legal hold and cannot be deleted")
	case errors.Is(err, store.ErrRetention):
		writeError(w, http.StatusConflict, "retention", "message is still within its retention period")
	default:
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any, maxBytes int64) error {
	if maxBytes <= 0 {
		maxBytes = 1 << 20
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	return nil
}

func parseDateRange(from, to string) (start, end *time.Time, err error) {
	if from != "" {
		t, e := parseDate(from, false)
		if e != nil {
			return nil, nil, e
		}
		start = &t
	}
	if to != "" {
		t, e := parseDate(to, true)
		if e != nil {
			return nil, nil, e
		}
		end = &t
	}
	return start, end, nil
}

func parseDate(v string, endOfDay bool) (time.Time, error) {
	v = strings.TrimSpace(v)
	if len(v) == 10 {
		t, err := time.ParseInLocation("2006-01-02", v, time.UTC)
		if err != nil {
			return time.Time{}, err
		}
		if endOfDay {
			t = t.Add(24*time.Hour - time.Second)
		}
		return t, nil
	}
	return time.Parse(time.RFC3339, v)
}
