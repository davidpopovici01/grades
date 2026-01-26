package migrate_test

import (
	"path/filepath"
	"testing"

	"github.com/davidpopovici01/grades/internal/db"
	"github.com/davidpopovici01/grades/internal/migrate"
)

func TestUp_AppliesOnce(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.db")

	db, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	db.SetMaxOpenConns(1)
	defer db.Close()

	// Run migrations twice; second run should be a no-op
	if err := migrate.Up(db); err != nil {
		t.Fatalf("migrate up (1): %v", err)
	}
	if err := migrate.Up(db); err != nil {
		t.Fatalf("migrate up (2): %v", err)
	}

	// Assert at least one expected table exists
	var name string
	if err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name='students';`).Scan(&name); err != nil {
		t.Fatalf("students table missing: %v", err)
	}

	// Stronger: ensure migration version recorded exactly once
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version='001_init';`).Scan(&count); err != nil {
		t.Fatalf("schema_migrations query failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected schema_migrations to contain 001_init once, got %d", count)
	}
}
