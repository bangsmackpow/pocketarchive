// Package migrations embeds the PostgreSQL schema migrations that ship with the
// engine binary.
package migrations

import "embed"

// FS contains all .sql migrations in lexical order.
//
//go:embed *.sql
var FS embed.FS
