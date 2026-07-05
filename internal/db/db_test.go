package db_test

import (
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