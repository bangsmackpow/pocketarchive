package worker

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/bangsmackpow/pocketarchive/engine/internal/search"
	"github.com/bangsmackpow/pocketarchive/engine/internal/store"
)

func (w *Worker) indexLoop(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		worked := false
		if w.Search != nil {
			for _, tenant := range w.tenants(ctx) {
				messages, err := w.Store.ListUnindexed(ctx, tenant.ID, 100)
				if err != nil {
					w.Log.Error("list unindexed messages", "tenant_id", tenant.ID, "error", err)
					continue
				}
				if len(messages) == 0 {
					continue
				}
				docs := make([]store.SearchDocument, 0, len(messages))
				ids := make([]uuid.UUID, 0, len(messages))
				for _, m := range messages {
					docs = append(docs, search.DocumentFromMessage(m))
					ids = append(ids, m.ID)
				}
				if err := w.Search.IndexDocuments(ctx, docs); err != nil {
					w.Log.Error("index messages", "tenant_id", tenant.ID, "error", err)
					continue
				}
				if err := w.Store.SetSearchIndexed(ctx, tenant.ID, ids); err != nil {
					w.Log.Error("mark messages indexed", "tenant_id", tenant.ID, "error", err)
					continue
				}
				worked = true
			}
		}
		if !worked && !w.sleep(ctx, 3*time.Second) {
			return
		}
	}
}
