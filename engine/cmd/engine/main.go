// Command engine is the PocketArchive backend. It can run the HTTP API, SMTP
// journal listener and background workers together or individually.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/bangsmackpow/pocketarchive/engine/internal/api"
	"github.com/bangsmackpow/pocketarchive/engine/internal/archive"
	"github.com/bangsmackpow/pocketarchive/engine/internal/auth"
	"github.com/bangsmackpow/pocketarchive/engine/internal/bootstrap"
	"github.com/bangsmackpow/pocketarchive/engine/internal/config"
	"github.com/bangsmackpow/pocketarchive/engine/internal/crypto"
	"github.com/bangsmackpow/pocketarchive/engine/internal/dbinit"
	"github.com/bangsmackpow/pocketarchive/engine/internal/ingest"
	"github.com/bangsmackpow/pocketarchive/engine/internal/ratelimit"
	"github.com/bangsmackpow/pocketarchive/engine/internal/search"
	"github.com/bangsmackpow/pocketarchive/engine/internal/smtp"
	"github.com/bangsmackpow/pocketarchive/engine/internal/storage"
	"github.com/bangsmackpow/pocketarchive/engine/internal/store"
	"github.com/bangsmackpow/pocketarchive/engine/internal/worker"
)

const version = "0.1.0"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cfg := config.Load()
	log := newLogger(cfg)

	var err error
	switch os.Args[1] {
	case "serve":
		err = runServe(cfg, log, os.Args[2:])
	case "db-init":
		err = runDBInit(cfg, log)
	case "migrate":
		err = runMigrate(cfg, log)
	case "bootstrap":
		err = runBootstrap(cfg, log, os.Args[2:])
	case "reindex":
		err = runReindex(cfg, log)
	case "version", "-version", "--version":
		fmt.Println("pocketarchive-engine", version)
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		log.Error("engine stopped", "error", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `pocketarchive-engine

Usage:
  engine serve [--enable-api] [--enable-smtp] [--enable-worker]
  engine db-init
  engine migrate
  engine bootstrap [--email EMAIL] [--password PASSWORD]
  engine reindex
  engine version
`)
}

func runServe(cfg config.Config, log *slog.Logger, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	enableAPI := fs.Bool("enable-api", cfg.APIEnabled, "enable the HTTP API")
	enableSMTP := fs.Bool("enable-smtp", cfg.SMTPEnabled, "enable the SMTP journal listener")
	enableWorker := fs.Bool("enable-worker", cfg.WorkerEnabled, "enable ingestion and indexing workers")
	_ = fs.Parse(args)
	cfg.APIEnabled, cfg.SMTPEnabled, cfg.WorkerEnabled = *enableAPI, *enableSMTP, *enableWorker
	if !cfg.APIEnabled && !cfg.SMTPEnabled && !cfg.WorkerEnabled {
		return errors.New("all components are disabled")
	}
	if err := cfg.ValidateServe(); err != nil {
		return err
	}
	master, err := crypto.ParseMasterKey(cfg.MasterKEK)
	if err != nil {
		return fmt.Errorf("ARCHIVE_MASTER_KEK: %w", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		return err
	}
	boot, err := bootstrap.Ensure(ctx, st, master, cfg)
	if err != nil {
		return err
	}
	log.Info("default tenant ready", "tenant_id", boot.Tenant.ID, "slug", boot.Tenant.Slug)
	if boot.AdminCreated {
		log.Warn("initial administrator created; change the bootstrap password after first login", "email", boot.AdminEmail)
	}
	if boot.NeedsAdmin {
		return errors.New("no administrator exists: set BOOTSTRAP_ADMIN_EMAIL and BOOTSTRAP_ADMIN_PASSWORD, or run `engine bootstrap --email ... --password ...`")
	}

	searchClient := search.NewClient(cfg.MeiliURL, cfg.MeiliMasterKey, cfg.MeiliIndex)
	if err := searchClient.EnsureIndex(ctx); err != nil {
		log.Warn("meilisearch is not ready; search and indexing will retry", "error", err)
	}

	blobStore, err := storage.NewBlobStore(cfg.BlobPath)
	if err != nil {
		return err
	}
	archiveSvc := &archive.Service{Store: st, Blobs: blobStore, Master: master}
	authMgr, err := auth.NewManager(cfg.SessionSecret, cfg.SessionTTL)
	if err != nil {
		return err
	}
	limiter, err := ratelimit.New(cfg.ValkeyURL, 10, 15*time.Minute)
	if err != nil {
		return err
	}
	defer limiter.Close()

	errCh := make(chan error, 4)
	var httpSrv *http.Server
	var smtpSrv interface {
		ListenAndServe() error
		Shutdown(context.Context) error
	}

	if cfg.APIEnabled {
		apiServer := api.New(cfg, st, searchClient, archiveSvc, authMgr, limiter, log)
		httpSrv = &http.Server{
			Addr:              cfg.APIAddr,
			Handler:           apiServer.Handler(),
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       5 * time.Minute,
			WriteTimeout:      5 * time.Minute,
			IdleTimeout:       90 * time.Second,
			MaxHeaderBytes:    1 << 20,
		}
		go func() {
			log.Info("HTTP API listening", "addr", cfg.APIAddr)
			if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errCh <- fmt.Errorf("http api: %w", err)
			}
		}()
	}

	if cfg.SMTPEnabled {
		tenant, err := st.GetTenantBySlug(ctx, cfg.SMTPTenantSlug)
		if err != nil {
			return fmt.Errorf("SMTP tenant %q: %w", cfg.SMTPTenantSlug, err)
		}
		backend, err := smtp.NewBackend(ctx, st, smtp.BackendConfig{
			SpoolRoot:      cfg.SpoolPath,
			Tenant:         tenant,
			AllowedDomains: cfg.SMTPAllowedDomains,
			AllowedCIDRs:   cfg.SMTPAllowedCIDRs,
			AuthUsername:   cfg.SMTPAuthUsername,
			AuthPassword:   cfg.SMTPAuthPassword,
			MaxBytes:       cfg.SMTPMaxBytes,
		}, log)
		if err != nil {
			return err
		}
		srv, err := smtp.NewServer(cfg, backend, log)
		if err != nil {
			return err
		}
		smtpSrv = srv
		go func() {
			log.Info("SMTP journal receiver listening", "addr", cfg.SMTPAddr)
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errCh <- fmt.Errorf("smtp: %w", err)
			}
		}()
	}

	workerCtx, cancelWorker := context.WithCancel(ctx)
	if cfg.WorkerEnabled {
		proc := &ingest.Processor{Store: st, Archive: archiveSvc, Log: log}
		w := worker.New(st, proc, searchClient, cfg, log)
		go w.Run(workerCtx)
		log.Info("workers started", "concurrency", cfg.WorkerConcurrency)
	}

	var runErr error
	select {
	case <-ctx.Done():
		log.Info("shutdown signal received")
	case runErr = <-errCh:
		log.Error("component failed", "error", runErr)
	}
	cancelWorker()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if httpSrv != nil {
		if err := httpSrv.Shutdown(shutdownCtx); err != nil {
			log.Warn("http shutdown", "error", err)
		}
	}
	if smtpSrv != nil {
		if err := smtpSrv.Shutdown(shutdownCtx); err != nil {
			log.Warn("smtp shutdown", "error", err)
		}
	}
	return runErr
}

func runDBInit(cfg config.Config, log *slog.Logger) error {
	if err := cfg.ValidateDBInit(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	res, err := dbinit.Ensure(ctx, cfg.AdminDatabaseURL, cfg.ArchiveDBUser, cfg.ArchiveDBName, cfg.ArchiveDBPassword)
	if err != nil {
		return err
	}
	log.Info("application database initialized",
		"role", cfg.ArchiveDBUser,
		"database", cfg.ArchiveDBName,
		"role_created", res.RoleCreated,
		"database_created", res.DatabaseCreated,
	)
	return nil
}

func runMigrate(cfg config.Config, log *slog.Logger) error {
	if err := cfg.ValidateDatabase(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	st, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		return err
	}
	log.Info("migrations applied")
	return nil
}

func runBootstrap(cfg config.Config, log *slog.Logger, args []string) error {
	fs := flag.NewFlagSet("bootstrap", flag.ExitOnError)
	email := fs.String("email", cfg.BootstrapAdminEmail, "initial admin email")
	password := fs.String("password", cfg.BootstrapAdminPass, "initial admin password (minimum 12 characters)")
	slug := fs.String("tenant-slug", cfg.DefaultTenantSlug, "default tenant slug")
	name := fs.String("tenant-name", cfg.DefaultTenantName, "default tenant name")
	_ = fs.Parse(args)
	if err := cfg.ValidateDatabase(); err != nil {
		return err
	}
	master, err := crypto.ParseMasterKey(cfg.MasterKEK)
	if err != nil {
		return fmt.Errorf("ARCHIVE_MASTER_KEK: %w", err)
	}
	cfg.BootstrapAdminEmail = *email
	cfg.BootstrapAdminPass = *password
	cfg.DefaultTenantSlug = *slug
	cfg.DefaultTenantName = *name

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	st, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		return err
	}
	res, err := bootstrap.Ensure(ctx, st, master, cfg)
	if err != nil {
		return err
	}
	if res.AdminCreated {
		log.Info("administrator created", "email", res.AdminEmail, "tenant", res.Tenant.Slug)
	} else {
		log.Info("tenant ready", "tenant", res.Tenant.Slug, "tenant_id", res.Tenant.ID)
		if res.NeedsAdmin {
			log.Warn("no administrator was created; provide --email and --password (minimum 12 characters)")
		}
	}
	return nil
}

func runReindex(cfg config.Config, log *slog.Logger) error {
	if err := cfg.ValidateDatabase(); err != nil {
		return err
	}
	if cfg.MeiliURL == "" {
		return errors.New("MEILI_URL is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	st, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()
	client := search.NewClient(cfg.MeiliURL, cfg.MeiliMasterKey, cfg.MeiliIndex)
	if err := client.EnsureIndex(ctx); err != nil {
		return err
	}
	tenants, err := st.ListTenants(ctx)
	if err != nil {
		return err
	}
	for _, tenant := range tenants {
		if err := st.ClearSearchIndexed(ctx, tenant.ID); err != nil {
			return err
		}
		offset := 0
		for {
			messages, err := st.ListMessagesBatch(ctx, tenant.ID, 200, offset)
			if err != nil {
				return err
			}
			if len(messages) == 0 {
				break
			}
			docs := make([]store.SearchDocument, 0, len(messages))
			ids := make([]uuid.UUID, 0, len(messages))
			for _, m := range messages {
				docs = append(docs, search.DocumentFromMessage(m))
				ids = append(ids, m.ID)
			}
			if err := client.IndexDocuments(ctx, docs); err != nil {
				return err
			}
			if err := st.SetSearchIndexed(ctx, tenant.ID, ids); err != nil {
				return err
			}
			offset += len(messages)
			log.Info("reindexed messages", "tenant", tenant.Slug, "count", offset)
		}
	}
	log.Info("reindex complete")
	return nil
}

func newLogger(cfg config.Config) *slog.Logger {
	level := slog.LevelInfo
	switch strings.ToLower(cfg.LogLevel) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if cfg.Env == "production" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}
	return slog.New(handler)
}
