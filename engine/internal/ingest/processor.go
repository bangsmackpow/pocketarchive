// Package ingest parses journal, mbox and PST content into immutable archived
// messages with encrypted blobs.
package ingest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jhillyerd/enmime/v2"

	"github.com/bangsmackpow/pocketarchive/engine/internal/archive"
	"github.com/bangsmackpow/pocketarchive/engine/internal/store"
)

type Processor struct {
	Store   *store.Store
	Archive *archive.Service
	Log     *slog.Logger
}

type Input struct {
	TenantID      uuid.UUID
	SourceType    string
	SourceID      *uuid.UUID
	ImportJobID   *uuid.UUID
	RetentionDays int
	Raw           []byte
	// JournalEnvelope is set for Office 365 journal reports and is archived as a
	// separate blob without affecting the inner message identity.
	JournalEnvelope []byte
	EnvelopeFrom    string
	EnvelopeTo      []string
}

type Result struct {
	MessageID       uuid.UUID
	Duplicate       bool
	AttachmentCount int
	Subject         string
}

var errNotMessage = errors.New("not an email message")

var (
	tagPattern   = regexp.MustCompile(`(?is)<script.*?</script>|<style.*?</style>|<[^>]+>`)
	spacePattern = regexp.MustCompile(`\s+`)
)

// Process parses, encrypts and stores one original message.
func (p *Processor) Process(ctx context.Context, in Input) (Result, error) {
	if in.TenantID == uuid.Nil {
		return Result{}, errors.New("tenant is required")
	}
	if len(in.Raw) == 0 {
		return Result{}, errNotMessage
	}
	switch in.SourceType {
	case "journal", "pst", "mbox", "eml":
	default:
		return Result{}, fmt.Errorf("unsupported source type %q", in.SourceType)
	}
	if in.RetentionDays <= 0 {
		in.RetentionDays = 2557
	}

	env, err := parseMessage(in.Raw)
	if err != nil {
		return Result{}, err
	}

	subject := strings.TrimSpace(env.GetHeader("Subject"))
	fromAddr, fromName := parseFrom(env)
	to := addressList(env, "To")
	cc := addressList(env, "Cc")
	bcc := addressList(env, "Bcc")
	msgID := strings.TrimSpace(env.GetHeader("Message-ID"))

	// Journal reports sometimes omit recipient headers on the inner message;
	// fall back to the SMTP envelope captured by the receiver.
	if len(to) == 0 && len(in.EnvelopeTo) > 0 {
		for _, addr := range in.EnvelopeTo {
			if addr = strings.ToLower(strings.TrimSpace(addr)); addr != "" {
				to = append(to, addr)
			}
		}
	}

	bodyText := strings.TrimSpace(env.Text)
	bodyHTML := SanitizeHTML(env.HTML)
	if bodyText == "" && bodyHTML != "" {
		bodyText = htmlToText(bodyHTML)
	}

	receivedAt := time.Now().UTC()
	sentAt := receivedAt
	if d, err := env.Date(); err == nil && !d.IsZero() {
		sentAt = d.UTC()
	}

	rawSHA, _, err := p.Archive.Ensure(ctx, in.TenantID, in.Raw)
	if err != nil {
		return Result{}, fmt.Errorf("store raw message: %w", err)
	}

	var journalSHA *string
	if len(in.JournalEnvelope) > 0 {
		sha, _, err := p.Archive.Ensure(ctx, in.TenantID, in.JournalEnvelope)
		if err != nil {
			return Result{}, fmt.Errorf("store journal envelope: %w", err)
		}
		journalSHA = &sha
	}

	type storedAttachment struct {
		meta store.Attachment
	}
	parts := make([]*enmime.Part, 0, len(env.Attachments)+len(env.Inlines))
	parts = append(parts, env.Attachments...)
	parts = append(parts, env.Inlines...)

	attachments := make([]storedAttachment, 0, len(parts))
	filenames := make([]string, 0, len(parts))
	for i, part := range parts {
		name := strings.TrimSpace(part.FileName)
		if name == "" {
			name = fmt.Sprintf("attachment-%d", i+1)
		}
		name = safeFilename(name)
		sha, _, err := p.Archive.Ensure(ctx, in.TenantID, part.Content)
		if err != nil {
			return Result{}, fmt.Errorf("store attachment %q: %w", name, err)
		}
		contentType := part.ContentType
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		attachments = append(attachments, storedAttachment{
			meta: store.Attachment{
				TenantID:    in.TenantID,
				Filename:    name,
				ContentType: contentType,
				ContentID:   strings.Trim(part.ContentID, "<>"),
				SizeBytes:   int64(len(part.Content)),
				SHA256:      sha,
			},
		})
		filenames = append(filenames, name)
	}

	duplicate, err := p.Store.MessageSHAExists(ctx, in.TenantID, rawSHA)
	if err != nil {
		return Result{}, err
	}

	headers := make(map[string][]string, len(env.GetHeaderKeys()))
	for _, key := range env.GetHeaderKeys() {
		headers[key] = env.GetHeaderValues(key)
	}

	retainUntil := receivedAt.Add(time.Duration(in.RetentionDays) * 24 * time.Hour)
	msg := &store.Message{
		TenantID:              in.TenantID,
		ImportJobID:           in.ImportJobID,
		SourceID:              in.SourceID,
		SourceType:            in.SourceType,
		MessageID:             msgID,
		Subject:               subject,
		FromAddr:              fromAddr,
		FromName:              fromName,
		ToAddrs:               to,
		CcAddrs:               cc,
		BccAddrs:              bcc,
		SentAt:                sentAt,
		ReceivedAt:            receivedAt,
		SizeBytes:             int64(len(in.Raw)),
		RawSHA256:             rawSHA,
		RawBlobSHA256:         rawSHA,
		JournalEnvelopeSHA256: journalSHA,
		Headers:               headers,
		BodyText:              bodyText,
		BodyHTML:              bodyHTML,
		HasAttachments:        len(attachments) > 0,
		AttachmentCount:       len(attachments),
		AttachmentFilenames:   filenames,
		RetainUntil:           retainUntil,
	}

	err = p.Store.WithTenant(ctx, in.TenantID, func(tx pgx.Tx) error {
		if err := store.InsertMessageTx(ctx, tx, msg); err != nil {
			return err
		}
		for i := range attachments {
			a := attachments[i].meta
			a.MessageID = msg.ID
			if err := store.InsertAttachmentTx(ctx, tx, &a); err != nil {
				return err
			}
		}
		return store.InsertAuditTx(ctx, tx, store.AuditEntry{
			TenantID:   in.TenantID,
			Action:     "message.ingested",
			TargetType: "message",
			TargetID:   msg.ID.String(),
			Detail: map[string]any{
				"source_type":   in.SourceType,
				"duplicate":     duplicate,
				"subject":       subject,
				"size_bytes":    len(in.Raw),
				"envelope_from": in.EnvelopeFrom,
				"envelope_to":   in.EnvelopeTo,
			},
		})
	})
	if err != nil {
		return Result{}, err
	}

	if p.Log != nil {
		p.Log.Info("archived message",
			"tenant_id", in.TenantID,
			"message_id", msg.ID,
			"source", in.SourceType,
			"attachments", len(attachments),
			"duplicate", duplicate)
	}
	return Result{
		MessageID:       msg.ID,
		Duplicate:       duplicate,
		AttachmentCount: len(attachments),
		Subject:         subject,
	}, nil
}

func parseMessage(raw []byte) (*enmime.Envelope, error) {
	env, err := enmime.ReadEnvelope(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errNotMessage, err)
	}
	if env.Root == nil {
		return nil, errNotMessage
	}
	return env, nil
}

func parseFrom(env *enmime.Envelope) (string, string) {
	addrs, err := env.AddressList("From")
	if err != nil || len(addrs) == 0 {
		for _, header := range []string{"Return-Path", "Sender"} {
			if addrs, err = env.AddressList(header); err == nil && len(addrs) > 0 {
				break
			}
		}
	}
	if len(addrs) == 0 {
		return "", ""
	}
	return strings.ToLower(addrs[0].Address), addrs[0].Name
}

func addressList(env *enmime.Envelope, header string) []string {
	addrs, err := env.AddressList(header)
	if err != nil {
		return []string{}
	}
	out := make([]string, 0, len(addrs))
	for _, a := range addrs {
		if a.Address != "" {
			out = append(out, strings.ToLower(a.Address))
		}
	}
	return out
}

func safeFilename(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return "attachment"
	}
	if len(name) > 200 {
		name = name[:200]
	}
	return name
}

func htmlToText(s string) string {
	s = tagPattern.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	return strings.TrimSpace(spacePattern.ReplaceAllString(s, " "))
}
