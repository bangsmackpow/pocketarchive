package worker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/bangsmackpow/pocketarchive/engine/internal/ingest"
	"github.com/bangsmackpow/pocketarchive/engine/internal/store"
)

func (w *Worker) importLoop(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		worked := false
		for _, tenant := range w.tenants(ctx) {
			job, ok, err := w.Store.ClaimImportJob(ctx, tenant.ID)
			if err != nil {
				w.Log.Error("claim import job", "tenant_id", tenant.ID, "error", err)
				continue
			}
			if !ok {
				continue
			}
			worked = true
			w.processImport(ctx, tenant, job)
		}
		if !worked && !w.sleep(ctx, 2*time.Second) {
			return
		}
	}
}

type importCounters struct {
	total, processed, duplicates, errors int
}

func (w *Worker) processImport(ctx context.Context, tenant store.Tenant, job store.ImportJob) {
	log := w.Log.With("tenant_id", tenant.ID, "import_id", job.ID, "kind", job.Kind)
	log.Info("starting import", "filename", job.OriginalFilename, "bytes", job.SizeBytes)

	counters := &importCounters{}
	updateProgress := func() {
		if err := w.Store.UpdateImportProgress(context.WithoutCancel(ctx), tenant.ID, job.ID,
			counters.total, counters.processed, counters.duplicates, counters.errors); err != nil {
			log.Warn("update import progress", "error", err)
		}
	}

	processRaw := func(raw []byte, ref string) {
		res, err := w.Ingest.Process(ctx, ingest.Input{
			TenantID:      tenant.ID,
			SourceType:    job.Kind,
			ImportJobID:   &job.ID,
			RetentionDays: tenant.RetentionDays,
			Raw:           raw,
		})
		if err != nil {
			counters.errors++
			_ = w.Store.InsertImportError(context.WithoutCancel(ctx), tenant.ID, job.ID, ref, err.Error())
			return
		}
		counters.processed++
		if res.Duplicate {
			counters.duplicates++
		}
	}

	var fatal error
	switch job.Kind {
	case "mbox":
		total, err := ingest.CountMboxMessages(job.SpoolPath)
		if err != nil {
			fatal = err
			break
		}
		counters.total = total
		updateProgress()

		f, err := os.Open(job.SpoolPath)
		if err != nil {
			fatal = err
			break
		}
		reader := ingest.NewMboxReader(f)
		for {
			raw, err := reader.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				fatal = err
				break
			}
			if len(raw) == 0 {
				continue
			}
			processRaw(raw, fmt.Sprintf("message-%d", counters.processed+counters.errors+1))
			if (counters.processed+counters.errors)%10 == 0 {
				updateProgress()
			}
			if ctx.Err() != nil {
				fatal = ctx.Err()
				break
			}
		}
		_ = f.Close()
	case "pst":
		if err := os.MkdirAll(filepath.Join(w.Config.SpoolPath, "imports"), 0o750); err != nil {
			fatal = err
			break
		}
		workspace, err := os.MkdirTemp(filepath.Join(w.Config.SpoolPath, "imports"), "pst-*")
		if err != nil {
			fatal = err
			break
		}
		defer os.RemoveAll(workspace)
		files, err := ingest.ExtractPST(ctx, job.SpoolPath, workspace, w.Config.PSTTimeout, log)
		if err != nil {
			fatal = err
			break
		}
		counters.total = len(files)
		updateProgress()
		for _, file := range files {
			raw, err := os.ReadFile(file)
			if err != nil {
				counters.errors++
				_ = w.Store.InsertImportError(context.WithoutCancel(ctx), tenant.ID, job.ID, file, "read message: "+err.Error())
				continue
			}
			processRaw(raw, file)
			if (counters.processed+counters.errors)%10 == 0 {
				updateProgress()
			}
			if ctx.Err() != nil {
				fatal = ctx.Err()
				break
			}
		}
	default:
		fatal = fmt.Errorf("unsupported import kind %q", job.Kind)
	}

	updateProgress()
	status := "completed"
	errText := ""
	if fatal != nil {
		status = "failed"
		errText = fatal.Error()
		log.Error("import failed", "error", fatal)
	} else {
		_ = os.Remove(job.SpoolPath)
		log.Info("import complete", "processed", counters.processed, "duplicates", counters.duplicates, "errors", counters.errors)
	}
	if err := w.Store.FinishImportJob(context.WithoutCancel(ctx), tenant.ID, job.ID, status, errText); err != nil {
		log.Error("finish import job", "error", err)
	}
	_ = w.Store.InsertAudit(context.WithoutCancel(ctx), store.AuditEntry{
		TenantID:   tenant.ID,
		Action:     "import." + status,
		TargetType: "import_job",
		TargetID:   job.ID.String(),
		Detail: map[string]any{
			"processed":  counters.processed,
			"duplicates": counters.duplicates,
			"errors":     counters.errors,
			"total":      counters.total,
		},
	})
}
