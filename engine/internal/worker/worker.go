// Package worker runs the archive ingestion and indexing loops.
package worker

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/bangsmackpow/pocketarchive/engine/internal/config"
	"github.com/bangsmackpow/pocketarchive/engine/internal/ingest"
	"github.com/bangsmackpow/pocketarchive/engine/internal/search"
	"github.com/bangsmackpow/pocketarchive/engine/internal/store"
)

type Worker struct {
	Store  *store.Store
	Ingest *ingest.Processor
	Search *search.Client
	Config config.Config
	Log    *slog.Logger
}

func New(st *store.Store, p *ingest.Processor, s *search.Client, cfg config.Config, log *slog.Logger) *Worker {
	return &Worker{Store: st, Ingest: p, Search: s, Config: cfg, Log: log}
}

func (w *Worker) Run(ctx context.Context) {
	var wg sync.WaitGroup
	concurrency := w.Config.WorkerConcurrency
	if concurrency < 1 {
		concurrency = 1
	}
	for i := 0; i < concurrency; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			w.importLoop(ctx)
		}()
		go func() {
			defer wg.Done()
			w.journalLoop(ctx)
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		w.indexLoop(ctx)
	}()
	wg.Wait()
}

func (w *Worker) sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func (w *Worker) tenants(ctx context.Context) []store.Tenant {
	list, err := w.Store.ListTenants(ctx)
	if err != nil {
		w.Log.Error("list tenants", "error", err)
		return nil
	}
	return list
}
