package migrate

import (
	"database/sql"
	_ "embed"
	"fmt"
	"time"
)

//go:embed sql/001_init.sql
var initSQL string

const initVersion = "001_init"

func Up(db *sql.DB) error {
	// Ensure migrations table exists
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TEXT NOT NULL
		);
	`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	// Has 001 already been applied?
	var count int
	if err := db.QueryRow(`SELECT COUNT(1) FROM schema_migrations WHERE version = ?;`, initVersion).Scan(&count); err != nil {
		return fmt.Errorf("check schema_migrations: %w", err)
	}
	if count > 0 {
		return nil // already migrated
	}

	// Apply migration in a transaction
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback() // no-op if already committed
	}()

	if _, err := tx.Exec(initSQL); err != nil {
		return fmt.Errorf("apply %s: %w", initVersion, err)
	}

	appliedAt := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES (?, ?);`, initVersion, appliedAt); err != nil {
		return fmt.Errorf("record migration: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
