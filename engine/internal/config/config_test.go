package config

import "testing"

func TestValidateDatabaseUsesPGEnvironment(t *testing.T) {
	t.Setenv("PGHOST", "postgres")
	t.Setenv("PGDATABASE", "archive")
	t.Setenv("PGUSER", "archive_app")

	cfg := Config{}
	if err := cfg.ValidateDatabase(); err != nil {
		t.Fatalf("expected PG* environment to satisfy database validation: %v", err)
	}
}

func TestValidateDatabaseMissing(t *testing.T) {
	t.Setenv("PGHOST", "")
	t.Setenv("PGDATABASE", "")
	t.Setenv("PGUSER", "")

	cfg := Config{}
	if err := cfg.ValidateDatabase(); err == nil {
		t.Fatal("expected missing database configuration to fail")
	}
}

func TestValidateDBInit(t *testing.T) {
	t.Setenv("PGHOST", "postgres")
	t.Setenv("PGDATABASE", "postgres")
	t.Setenv("PGUSER", "postgres")
	t.Setenv("ADMIN_DATABASE_URL", "")

	cfg := Config{ArchiveDBPassword: "secret"}
	if err := cfg.ValidateDBInit(); err != nil {
		t.Fatalf("expected db-init config to validate: %v", err)
	}

	cfg.ArchiveDBPassword = ""
	if err := cfg.ValidateDBInit(); err == nil {
		t.Fatal("expected missing ARCHIVE_DB_PASSWORD to fail")
	}
}

func TestValidateServeAcceptsPGEnvironment(t *testing.T) {
	t.Setenv("PGHOST", "postgres")
	t.Setenv("PGDATABASE", "archive")
	t.Setenv("PGUSER", "archive_app")

	cfg := Config{
		DatabaseURL:       "",
		MasterKEK:         "unused-here",
		SessionSecret:     "0123456789abcdef0123456789abcdef",
		MeiliURL:          "http://meilisearch:7700",
		APIEnabled:        true,
		WorkerConcurrency: 1,
		DefaultTenantSlug: "default",
	}
	if err := cfg.ValidateServe(); err != nil {
		t.Fatalf("expected serve config to validate: %v", err)
	}
}
