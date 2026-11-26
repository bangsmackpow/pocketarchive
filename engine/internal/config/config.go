package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the fully resolved runtime configuration. Environment variables are
// read once at startup; components never read the environment directly.
type Config struct {
	Env      string
	LogLevel string

	APIAddr       string
	APIEnabled    bool
	SMTPEnabled   bool
	WorkerEnabled bool

	DatabaseURL string
	BlobPath    string
	SpoolPath   string

	// AdminDatabaseURL is used only by `engine db-init`. When empty, pgx reads
	// the standard PG* environment variables (PGHOST, PGPORT, PGUSER, ...).
	AdminDatabaseURL  string
	ArchiveDBUser     string
	ArchiveDBName     string
	ArchiveDBPassword string

	MasterKEK string

	MeiliURL       string
	MeiliMasterKey string
	MeiliIndex     string

	ValkeyURL string

	SessionSecret string
	SessionTTL    time.Duration

	DefaultTenantSlug    string
	DefaultTenantName    string
	DefaultRetentionDays int
	BootstrapAdminEmail  string
	BootstrapAdminPass   string

	SMTPAddr           string
	SMTPHostname       string
	SMTPTLSCertFile    string
	SMTPTLSKeyFile     string
	SMTPAllowedDomains []string
	SMTPAllowedCIDRs   []string
	SMTPAuthUsername   string
	SMTPAuthPassword   string
	SMTPMaxBytes       int64
	SMTPTenantSlug     string

	MaxUploadBytes    int64
	MaxChunkBytes     int64
	WorkerConcurrency int
	PSTTimeout        time.Duration
}

// Load reads configuration from the environment. It does not validate
// component-specific requirements; call ValidateServe where appropriate.
func Load() Config {
	return Config{
		Env:      env("ARCHIVE_ENV", "development"),
		LogLevel: env("LOG_LEVEL", "info"),

		APIAddr:       env("API_ADDR", ":8080"),
		APIEnabled:    envBool("ENABLE_API", true),
		SMTPEnabled:   envBool("ENABLE_SMTP", true),
		WorkerEnabled: envBool("ENABLE_WORKER", true),

		DatabaseURL: env("DATABASE_URL", ""),
		BlobPath:    env("BLOB_STORAGE_PATH", "/data/blobs"),
		SpoolPath:   env("SPOOL_PATH", "/data/spool"),

		AdminDatabaseURL:  env("ADMIN_DATABASE_URL", ""),
		ArchiveDBUser:     env("ARCHIVE_DB_USER", "archive_app"),
		ArchiveDBName:     env("ARCHIVE_DB_NAME", "archive"),
		ArchiveDBPassword: env("ARCHIVE_DB_PASSWORD", ""),

		MasterKEK: env("ARCHIVE_MASTER_KEK", ""),

		MeiliURL:       env("MEILI_URL", "http://localhost:7700"),
		MeiliMasterKey: env("MEILI_MASTER_KEY", ""),
		MeiliIndex:     env("MEILI_INDEX", "messages"),

		ValkeyURL: env("VALKEY_URL", ""),

		SessionSecret: env("SESSION_SECRET", ""),
		SessionTTL:    envDuration("SESSION_TTL", 12*time.Hour),

		DefaultTenantSlug:    env("DEFAULT_TENANT_SLUG", "default"),
		DefaultTenantName:    env("DEFAULT_TENANT_NAME", "Default Archive"),
		DefaultRetentionDays: envInt("DEFAULT_RETENTION_DAYS", 2557),
		BootstrapAdminEmail:  env("BOOTSTRAP_ADMIN_EMAIL", ""),
		BootstrapAdminPass:   env("BOOTSTRAP_ADMIN_PASSWORD", ""),

		SMTPAddr:           env("SMTP_ADDR", ":2525"),
		SMTPHostname:       env("SMTP_HOSTNAME", "mail.archive.local"),
		SMTPTLSCertFile:    env("SMTP_TLS_CERT_FILE", ""),
		SMTPTLSKeyFile:     env("SMTP_TLS_KEY_FILE", ""),
		SMTPAllowedDomains: envList("SMTP_ALLOWED_DOMAINS"),
		SMTPAllowedCIDRs:   envList("SMTP_ALLOWED_IPS"),
		SMTPAuthUsername:   env("SMTP_AUTH_USERNAME", ""),
		SMTPAuthPassword:   env("SMTP_AUTH_PASSWORD", ""),
		SMTPMaxBytes:       envInt64("SMTP_MAX_MESSAGE_BYTES", 100*1024*1024),
		SMTPTenantSlug:     env("SMTP_TENANT_SLUG", env("DEFAULT_TENANT_SLUG", "default")),

		MaxUploadBytes:    envInt64("MAX_UPLOAD_BYTES", 5*1024*1024*1024),
		MaxChunkBytes:     envInt64("MAX_CHUNK_BYTES", 32*1024*1024),
		WorkerConcurrency: envInt("WORKER_CONCURRENCY", 2),
		PSTTimeout:        envDuration("PST_TIMEOUT", 2*time.Hour),
	}
}

// ValidateServe checks the settings required to run the engine.
func (c Config) ValidateServe() error {
	var missing []string
	if !c.hasDatabaseTarget() {
		missing = append(missing, "DATABASE_URL or PGHOST+PGDATABASE+PGUSER")
	}
	if c.MasterKEK == "" {
		missing = append(missing, "ARCHIVE_MASTER_KEK")
	}
	if len(c.SessionSecret) < 32 {
		missing = append(missing, "SESSION_SECRET (minimum 32 characters)")
	}
	if c.APIEnabled && c.MeiliURL == "" {
		missing = append(missing, "MEILI_URL")
	}
	if len(missing) > 0 {
		return fmt.Errorf("invalid configuration: missing or invalid %s", strings.Join(missing, ", "))
	}
	if c.WorkerConcurrency < 1 {
		return errors.New("WORKER_CONCURRENCY must be at least 1")
	}
	if c.DefaultTenantSlug == "" {
		return errors.New("DEFAULT_TENANT_SLUG must not be empty")
	}
	return nil
}

// ValidateDatabase checks the connection settings used by migrate, bootstrap
// and reindex.
func (c Config) ValidateDatabase() error {
	if c.hasDatabaseTarget() {
		return nil
	}
	return errors.New("database configuration missing: set DATABASE_URL or PGHOST, PGDATABASE and PGUSER")
}

// ValidateDBInit checks the settings required by `engine db-init`.
func (c Config) ValidateDBInit() error {
	if c.ArchiveDBPassword == "" {
		return errors.New("ARCHIVE_DB_PASSWORD is required for db-init")
	}
	if c.AdminDatabaseURL != "" {
		return nil
	}
	if os.Getenv("PGHOST") == "" || os.Getenv("PGDATABASE") == "" || os.Getenv("PGUSER") == "" {
		return errors.New("admin database configuration missing: set ADMIN_DATABASE_URL or PGHOST, PGDATABASE and PGUSER")
	}
	return nil
}

func (c Config) hasDatabaseTarget() bool {
	if c.DatabaseURL != "" {
		return true
	}
	return os.Getenv("PGHOST") != "" && os.Getenv("PGDATABASE") != "" && os.Getenv("PGUSER") != ""
}

func env(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

func envInt(key string, fallback int) int {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func envInt64(key string, fallback int64) int64 {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return fallback
	}
	return n
}

func envDuration(key string, fallback time.Duration) time.Duration {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}

func envList(key string) []string {
	v := os.Getenv(key)
	if v == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.ToLower(strings.TrimSpace(p))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
