// Package search wraps the Meilisearch HTTP API. Every document and query is
// scoped by tenant_id.
package search

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/bangsmackpow/pocketarchive/engine/internal/store"
)

type Client struct {
	baseURL string
	apiKey  string
	index   string
	http    *http.Client
}

func NewClient(baseURL, apiKey, index string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		index:   index,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) IndexName() string { return c.index }

func (c *Client) EnsureIndex(ctx context.Context) error {
	status, raw, err := c.do(ctx, http.MethodGet, "/indexes/"+url.PathEscape(c.index), nil)
	if err != nil {
		return err
	}
	if status == http.StatusNotFound {
		body := map[string]any{"uid": c.index, "primaryKey": "id"}
		status, raw, err = c.do(ctx, http.MethodPost, "/indexes", body)
		if err != nil {
			return err
		}
		if status != http.StatusCreated && status != http.StatusAccepted && status != http.StatusConflict {
			return fmt.Errorf("create meilisearch index: status %d: %s", status, truncate(raw))
		}
		if err := c.waitTask(ctx, raw); err != nil {
			return fmt.Errorf("create meilisearch index task: %w", err)
		}
	} else if status != http.StatusOK {
		return fmt.Errorf("get meilisearch index: status %d: %s", status, truncate(raw))
	}

	settings := map[string]any{
		"searchableAttributes": []string{
			"subject", "body_text", "from_addr", "from_name", "to_addrs", "cc_addrs", "attachment_filenames",
		},
		"filterableAttributes": []string{
			"tenant_id", "from_addr", "to_addrs", "has_attachments", "source_type", "legal_hold", "sent_at",
		},
		"sortableAttributes": []string{"sent_at", "received_at"},
	}
	status, raw, err = c.do(ctx, http.MethodPatch, "/indexes/"+url.PathEscape(c.index)+"/settings", settings)
	if err != nil {
		return err
	}
	if status != http.StatusAccepted && status != http.StatusOK {
		return fmt.Errorf("update meilisearch settings: status %d: %s", status, truncate(raw))
	}
	if err := c.waitTask(ctx, raw); err != nil {
		return fmt.Errorf("meilisearch settings task: %w", err)
	}
	return nil
}

func (c *Client) waitTask(ctx context.Context, raw []byte) error {
	var task struct {
		TaskUID int64 `json:"taskUid"`
	}
	if err := json.Unmarshal(raw, &task); err != nil || task.TaskUID == 0 {
		return nil
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		status, body, err := c.do(ctx, http.MethodGet, fmt.Sprintf("/tasks/%d", task.TaskUID), nil)
		if err != nil {
			return err
		}
		if status != http.StatusOK {
			return fmt.Errorf("task status %d: %s", status, truncate(body))
		}
		var state struct {
			Status string `json:"status"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(body, &state); err != nil {
			return err
		}
		switch state.Status {
		case "succeeded":
			return nil
		case "failed", "canceled":
			msg := "task failed"
			if state.Error != nil && state.Error.Message != "" {
				msg = state.Error.Message
			}
			return errors.New(msg)
		}
		if time.Now().After(deadline) {
			return errors.New("timed out waiting for meilisearch task")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(150 * time.Millisecond):
		}
	}
}

func (c *Client) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("meilisearch health status %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) IndexDocuments(ctx context.Context, docs []store.SearchDocument) error {
	if len(docs) == 0 {
		return nil
	}
	path := "/indexes/" + url.PathEscape(c.index) + "/documents?primaryKey=id"
	status, raw, err := c.do(ctx, http.MethodPost, path, docs)
	if err != nil {
		return err
	}
	if status == http.StatusNotFound {
		if err := c.EnsureIndex(ctx); err != nil {
			return err
		}
		status, raw, err = c.do(ctx, http.MethodPost, path, docs)
		if err != nil {
			return err
		}
	}
	if status != http.StatusAccepted && status != http.StatusOK {
		return fmt.Errorf("index documents: status %d: %s", status, truncate(raw))
	}
	return nil
}

func (c *Client) UpdateLegalHold(ctx context.Context, id string, hold bool) error {
	doc := map[string]any{"id": id, "legal_hold": hold}
	status, raw, err := c.do(ctx, http.MethodPut,
		"/indexes/"+url.PathEscape(c.index)+"/documents?primaryKey=id", []map[string]any{doc})
	if err != nil {
		return err
	}
	if status != http.StatusAccepted && status != http.StatusOK {
		return fmt.Errorf("update search document: status %d: %s", status, truncate(raw))
	}
	return nil
}

func (c *Client) DeleteDocument(ctx context.Context, id string) error {
	status, raw, err := c.do(ctx, http.MethodDelete,
		"/indexes/"+url.PathEscape(c.index)+"/documents/"+url.PathEscape(id), nil)
	if err != nil {
		return err
	}
	if status != http.StatusAccepted && status != http.StatusOK && status != http.StatusNotFound {
		return fmt.Errorf("delete search document: status %d: %s", status, truncate(raw))
	}
	return nil
}

type SearchRequest struct {
	TenantID      uuid.UUID
	Query         string
	From          string
	To            string
	DateFrom      *time.Time
	DateTo        *time.Time
	HasAttachment *bool
	SourceTypes   []string
	LegalHold     *bool
	Page          int
	Limit         int
}

type Hit struct {
	ID                  string    `json:"id"`
	Subject             string    `json:"subject"`
	FromAddr            string    `json:"from_addr"`
	FromName            string    `json:"from_name"`
	ToAddrs             []string  `json:"to_addrs"`
	CcAddrs             []string  `json:"cc_addrs"`
	SentAt              time.Time `json:"sent_at"`
	ReceivedAt          time.Time `json:"received_at"`
	HasAttachments      bool      `json:"has_attachments"`
	AttachmentCount     int       `json:"attachment_count"`
	AttachmentFilenames []string  `json:"attachment_filenames"`
	SourceType          string    `json:"source_type"`
	LegalHold           bool      `json:"legal_hold"`
	SizeBytes           int64     `json:"size_bytes"`
	MessageID           string    `json:"message_id"`
}

type Result struct {
	Hits     []Hit `json:"hits"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	Limit    int   `json:"limit"`
	TookMS   int   `json:"took_ms"`
	Fallback bool  `json:"fallback"`
}

func (c *Client) Search(ctx context.Context, req SearchRequest) (Result, error) {
	if req.Limit <= 0 || req.Limit > 100 {
		req.Limit = 25
	}
	if req.Page < 1 {
		req.Page = 1
	}
	filters := []string{`tenant_id = ` + quote(req.TenantID.String())}
	if req.From != "" {
		filters = append(filters, `from_addr = `+quote(strings.ToLower(req.From)))
	}
	if req.To != "" {
		filters = append(filters, `to_addrs = `+quote(strings.ToLower(req.To)))
	}
	if req.DateFrom != nil {
		filters = append(filters, fmt.Sprintf("sent_at >= %d", req.DateFrom.Unix()))
	}
	if req.DateTo != nil {
		filters = append(filters, fmt.Sprintf("sent_at <= %d", req.DateTo.Unix()))
	}
	if req.HasAttachment != nil {
		filters = append(filters, fmt.Sprintf("has_attachments = %t", *req.HasAttachment))
	}
	if req.LegalHold != nil {
		filters = append(filters, fmt.Sprintf("legal_hold = %t", *req.LegalHold))
	}
	if len(req.SourceTypes) > 0 {
		quoted := make([]string, 0, len(req.SourceTypes))
		for _, s := range req.SourceTypes {
			if s == "journal" || s == "pst" || s == "mbox" || s == "eml" {
				quoted = append(quoted, quote(s))
			}
		}
		if len(quoted) > 0 {
			filters = append(filters, "source_type IN ["+strings.Join(quoted, ", ")+"]")
		}
	}

	payload := map[string]any{
		"q":      req.Query,
		"filter": filters,
		"sort":   []string{"sent_at:desc"},
		"limit":  req.Limit,
		"offset": (req.Page - 1) * req.Limit,
	}
	status, raw, err := c.do(ctx, http.MethodPost, "/indexes/"+url.PathEscape(c.index)+"/search", payload)
	if err != nil {
		return Result{}, err
	}
	if status == http.StatusNotFound {
		if err := c.EnsureIndex(ctx); err != nil {
			return Result{}, err
		}
		status, raw, err = c.do(ctx, http.MethodPost, "/indexes/"+url.PathEscape(c.index)+"/search", payload)
		if err != nil {
			return Result{}, err
		}
	}
	if status != http.StatusOK {
		return Result{}, fmt.Errorf("search: status %d: %s", status, truncate(raw))
	}
	var resp struct {
		Hits               []rawHit `json:"hits"`
		EstimatedTotalHits int64    `json:"estimatedTotalHits"`
		TotalHits          int64    `json:"totalHits"`
		ProcessingTimeMS   int      `json:"processingTimeMs"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return Result{}, fmt.Errorf("decode search response: %w", err)
	}
	total := resp.EstimatedTotalHits
	if resp.TotalHits > 0 {
		total = resp.TotalHits
	}
	out := Result{Total: total, Page: req.Page, Limit: req.Limit, TookMS: resp.ProcessingTimeMS, Hits: []Hit{}}
	for _, h := range resp.Hits {
		to := h.ToAddrs
		if to == nil {
			to = []string{}
		}
		cc := h.CcAddrs
		if cc == nil {
			cc = []string{}
		}
		filenames := h.AttachmentFilenames
		if filenames == nil {
			filenames = []string{}
		}
		out.Hits = append(out.Hits, Hit{
			ID:                  h.ID,
			Subject:             h.Subject,
			FromAddr:            h.FromAddr,
			FromName:            h.FromName,
			ToAddrs:             to,
			CcAddrs:             cc,
			SentAt:              time.Unix(h.SentAt, 0).UTC(),
			ReceivedAt:          time.Unix(h.ReceivedAt, 0).UTC(),
			HasAttachments:      h.HasAttachments,
			AttachmentCount:     h.AttachmentCount,
			AttachmentFilenames: filenames,
			SourceType:          h.SourceType,
			LegalHold:           h.LegalHold,
			SizeBytes:           h.SizeBytes,
			MessageID:           h.MessageID,
		})
	}
	return out, nil
}

type rawHit struct {
	ID                  string   `json:"id"`
	Subject             string   `json:"subject"`
	FromAddr            string   `json:"from_addr"`
	FromName            string   `json:"from_name"`
	ToAddrs             []string `json:"to_addrs"`
	CcAddrs             []string `json:"cc_addrs"`
	SentAt              int64    `json:"sent_at"`
	ReceivedAt          int64    `json:"received_at"`
	HasAttachments      bool     `json:"has_attachments"`
	AttachmentCount     int      `json:"attachment_count"`
	AttachmentFilenames []string `json:"attachment_filenames"`
	SourceType          string   `json:"source_type"`
	LegalHold           bool     `json:"legal_hold"`
	SizeBytes           int64    `json:"size_bytes"`
	MessageID           string   `json:"message_id"`
}

func DocumentFromMessage(m store.Message) store.SearchDocument {
	filenames := m.AttachmentFilenames
	if filenames == nil {
		filenames = []string{}
	}
	body := m.BodyText
	if len(body) > 100_000 {
		body = body[:100_000]
	}
	return store.SearchDocument{
		ID:                  m.ID.String(),
		TenantID:            m.TenantID.String(),
		Subject:             m.Subject,
		BodyText:            body,
		FromAddr:            m.FromAddr,
		FromName:            m.FromName,
		ToAddrs:             m.ToAddrs,
		CcAddrs:             m.CcAddrs,
		SentAt:              m.SentAt.Unix(),
		ReceivedAt:          m.ReceivedAt.Unix(),
		HasAttachments:      m.HasAttachments,
		AttachmentCount:     m.AttachmentCount,
		AttachmentFilenames: filenames,
		SourceType:          m.SourceType,
		LegalHold:           m.LegalHold,
		SizeBytes:           m.SizeBytes,
		MessageID:           m.MessageID,
		RetainUntil:         m.RetainUntil.Unix(),
	}
}

func (c *Client) do(ctx context.Context, method, path string, payload any) (int, []byte, error) {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return 0, nil, err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return 0, nil, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("meilisearch request: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return resp.StatusCode, nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return resp.StatusCode, raw, errors.New("meilisearch rejected the master key")
	}
	return resp.StatusCode, raw, nil
}

func quote(s string) string {
	// JSON string escaping is valid in Meilisearch filter expressions.
	b, _ := json.Marshal(s)
	return string(b)
}

func truncate(s []byte) string {
	if len(s) > 500 {
		return string(s[:500]) + "..."
	}
	return string(s)
}
