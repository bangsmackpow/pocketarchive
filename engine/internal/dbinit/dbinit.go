// Package dbinit provisions the least-privileged PostgreSQL role and database
// used by the engine. It is intentionally a separate, idempotent operation from
// migrations so the long-running engine never needs superuser credentials.
package dbinit

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

type Result struct {
	RoleCreated     bool
	DatabaseCreated bool
}

// Ensure creates the application role and database if needed, then resets the
// role password to the configured value. adminURL may be empty, in which case
// pgx reads the standard PG* environment variables.
func Ensure(ctx context.Context, adminURL, role, database, password string) (Result, error) {
	role = strings.TrimSpace(role)
	database = strings.TrimSpace(database)
	if role == "" || database == "" {
		return Result{}, errors.New("application role and database name are required")
	}
	if password == "" {
		return Result{}, errors.New("application database password is required")
	}

	conn, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		return Result{}, fmt.Errorf("connect as PostgreSQL administrator: %w", err)
	}
	defer conn.Close(context.WithoutCancel(ctx))

	roleIdent := pgx.Identifier{role}.Sanitize()
	dbIdent := pgx.Identifier{database}.Sanitize()

	var quotedPassword string
	if err := conn.QueryRow(ctx, `SELECT quote_literal($1)`, password).Scan(&quotedPassword); err != nil {
		return Result{}, fmt.Errorf("quote role password: %w", err)
	}

	var roleExists bool
	if err := conn.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname = $1)`, role).Scan(&roleExists); err != nil {
		return Result{}, fmt.Errorf("check role: %w", err)
	}
	if roleExists {
		_, err = conn.Exec(ctx, fmt.Sprintf(
			`ALTER ROLE %s WITH LOGIN PASSWORD %s NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS`,
			roleIdent, quotedPassword))
	} else {
		_, err = conn.Exec(ctx, fmt.Sprintf(
			`CREATE ROLE %s LOGIN PASSWORD %s NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS`,
			roleIdent, quotedPassword))
	}
	if err != nil {
		return Result{}, fmt.Errorf("ensure role %q: %w", role, err)
	}

	var databaseExists bool
	if err := conn.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)`, database).Scan(&databaseExists); err != nil {
		return Result{}, fmt.Errorf("check database: %w", err)
	}
	if databaseExists {
		_, err = conn.Exec(ctx, fmt.Sprintf(`ALTER DATABASE %s OWNER TO %s`, dbIdent, roleIdent))
		if err != nil {
			return Result{}, fmt.Errorf("ensure database owner: %w", err)
		}
	} else {
		_, err = conn.Exec(ctx, fmt.Sprintf(`CREATE DATABASE %s OWNER %s`, dbIdent, roleIdent))
		if err != nil {
			return Result{}, fmt.Errorf("create database %q: %w", database, err)
		}
	}

	return Result{RoleCreated: !roleExists, DatabaseCreated: !databaseExists}, nil
}
