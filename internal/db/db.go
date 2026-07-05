// Package db opens a SQLite connection for warehouse storage and applies
// embedded SQL migrations on first use.
package db

import (
	"database/sql"
	"fmt"

	// pure-Go SQLite driver (no CGO required).
	_ "modernc.org/sqlite"
)

// Open returns a *sql.DB connected to the SQLite file at path. The file is
// created if missing. PRAGMAs (WAL, foreign keys, busy timeout) are applied
// before migrations run. On a fresh database, all migration files under
// migrations/ are applied in lexical order.
func Open(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf(
		"file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)",
		path,
	)
	d, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("sql.Open: %w", err)
	}
	if err := d.Ping(); err != nil {
		_ = d.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	if err := applyMigrations(d); err != nil {
		_ = d.Close()
		return nil, fmt.Errorf("apply migrations: %w", err)
	}
	return d, nil
}