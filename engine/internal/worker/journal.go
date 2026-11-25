package worker

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"

	"github.com/bangsmackpow/pocketarchive/engine/internal/ingest"
	"github.com/bangsmackpow/pocketarchive/engine/internal/store"
)

func (w *Worker) journalLoop(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		worked := false
		for _, tenant := range w.tenants(ctx) {
			item, ok, err := w.Store.ClaimJournal(ctx, tenant.ID)
			if err != nil {
				w.Log.Error("claim journal item", "tenant_id", tenant.ID, "error", err)
				continue
			}
			if !ok {
				continue
			}
			worked = true
			w.processJournal(ctx, tenant, item)
		}
		if !worked && !w.sleep(ctx, time.Second) {
			return
		}
	}
}

func (w *Worker) processJournal(ctx context.Context, tenant store.Tenant, item store.JournalQueueItem) {
	log := w.Log.With("tenant_id", tenant.ID, "queue_id", item.ID)
	if err := w.handleJournal(ctx, tenant, item); err != nil {
		attempts := item.Attempts
		if attempts >= 3 {
			quarantine := w.quarantine(item, err)
			_ = w.Store.FailJournal(context.WithoutCancel(ctx), tenant.ID, item.ID, err.Error()+" "+quarantine)
			_ = w.Store.InsertAudit(context.WithoutCancel(ctx), store.AuditEntry{
				TenantID:   tenant.ID,
				Action:     "journal.quarantined",
				TargetType: "journal_queue",
				TargetID:   fmt.Sprintf("%d", item.ID),
				Detail: map[string]any{
					"error":      err.Error(),
					"spool_path": item.SpoolPath,
					"attempts":   attempts,
				},
			})
			log.Error("journal message quarantined", "error", err, "attempts", attempts)
			return
		}
		next := time.Now().Add(time.Duration(attempts*attempts) * time.Minute)
		_ = w.Store.RetryJournal(context.WithoutCancel(ctx), tenant.ID, item.ID, err.Error(), next)
		log.Warn("journal message will be retried", "error", err, "attempts", attempts, "next_attempt", next)
		return
	}
	_ = os.Remove(item.SpoolPath)
	if err := w.Store.MarkJournalDone(context.WithoutCancel(ctx), tenant.ID, item.ID); err != nil {
		log.Error("mark journal done", "error", err)
	}
}

func (w *Worker) handleJournal(ctx context.Context, tenant store.Tenant, item store.JournalQueueItem) error {
	raw, err := os.ReadFile(item.SpoolPath)
	if err != nil {
		return fmt.Errorf("read spool file: %w", err)
	}
	report, err := ingest.ParseJournal(raw)
	if err != nil {
		return err
	}
	envelopeFrom := report.EnvelopeFrom
	if envelopeFrom == "" {
		envelopeFrom = item.EnvelopeFrom
	}
	input := ingest.Input{
		TenantID:      tenant.ID,
		SourceType:    "journal",
		RetentionDays: tenant.RetentionDays,
		Raw:           report.InnerRaw,
		EnvelopeFrom:  envelopeFrom,
		EnvelopeTo:    report.EnvelopeTo,
	}
	if report.IsJournal {
		input.JournalEnvelope = report.EnvelopeRaw
	}
	_, err = w.Ingest.Process(ctx, input)
	return err
}

func (w *Worker) quarantine(item store.JournalQueueItem, cause error) string {
	dir := filepath.Join(w.Config.SpoolPath, "quarantine", item.TenantID.String())
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return ""
	}
	dest := filepath.Join(dir, uuid.NewString()+".eml")
	if err := os.Rename(item.SpoolPath, dest); err != nil {
		if copyErr := copyFile(item.SpoolPath, dest); copyErr != nil {
			w.Log.Error("failed to quarantine journal message", "error", copyErr, "cause", cause)
			return ""
		}
		_ = os.Remove(item.SpoolPath)
	}
	if err := os.WriteFile(dest+".error.txt", []byte(cause.Error()), 0o640); err != nil {
		w.Log.Warn("write quarantine note", "error", err)
	}
	return dest
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o640)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := out.ReadFrom(in); err != nil {
		return err
	}
	return out.Sync()
}
