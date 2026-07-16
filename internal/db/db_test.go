package db_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	internaldb "github.com/jiaobendaye/warehouse/internal/db"
)

// TestOpen_CreatesFileAndAppliesMigrations verifies that calling Open
// creates a SQLite file at the given path and that the initial migration
// has been applied (i.e. the accessories table exists).
func TestOpen_CreatesFileAndAppliesMigrations(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	db, err := internaldb.Open(dbPath)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer db.Close()

	var name string
	row := db.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name='accessories'`,
	)
	if err := row.Scan(&name); err != nil {
		t.Fatalf("expected accessories table after migration: %v", err)
	}
	if name != "accessories" {
		t.Fatalf("expected table name 'accessories', got %q", name)
	}
}

// TestOpen_AppliesAllInitialMigrations verifies that the inventory_flow
// table from 0001_init.sql also exists after Open.
func TestOpen_AppliesAllInitialMigrations(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test2.db")

	db, err := internaldb.Open(dbPath)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer db.Close()

	var name string
	row := db.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name='inventory_flow'`,
	)
	if err := row.Scan(&name); err != nil {
		t.Fatalf("expected inventory_flow table after migration: %v", err)
	}
	if name != "inventory_flow" {
		t.Fatalf("expected table name 'inventory_flow', got %q", name)
	}
}

// TestOpen_AddsStallColumn verifies the 0002_add_stall.sql migration ran
// and the accessories table now has a stall column with the documented
// default.
func TestOpen_AddsStallColumn(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test3.db")

	db, err := internaldb.Open(dbPath)
	if err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	defer db.Close()

	// PRAGMA table_info returns one row per column with cid, name, type,
	// notnull, dflt_value, pk. We assert the stall column exists with
	// NOT NULL and the default '未分配'.
	rows, err := db.Query(`PRAGMA table_info(accessories)`)
	if err != nil {
		t.Fatalf("PRAGMA: %v", err)
	}
	defer rows.Close()

	var (
		gotColumn bool
		gotType   string
		gotNotNull int
		gotDefault sql.NullString
	)
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if name == "stall" {
			gotColumn = true
			gotType = ctype
			gotNotNull = notnull
			gotDefault = dflt
		}
	}
	if !gotColumn {
		t.Fatalf("stall column missing from accessories table")
	}
	if gotType != "TEXT" {
		t.Errorf("stall type = %q, want TEXT", gotType)
	}
	if gotNotNull != 1 {
		t.Errorf("stall notnull = %d, want 1", gotNotNull)
	}
	if !gotDefault.Valid {
		t.Errorf("stall default missing, want 未分配")
	} else {
		// PRAGMA table_info returns the SQL literal, which is wrapped in
		// single quotes for string defaults. Strip them before comparing.
		dflt := gotDefault.String
		if len(dflt) >= 2 && dflt[0] == '\'' && dflt[len(dflt)-1] == '\'' {
			dflt = dflt[1 : len(dflt)-1]
		}
		if dflt != "未分配" {
			t.Errorf("stall default = %q, want 未分配", dflt)
		}
	}
}