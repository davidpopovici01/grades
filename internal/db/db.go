package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// Open opens a sqlite database at dbPath and applies required pragmas.
// For v0.1 we keep a single connection to avoid per-connection pragma surprises.
func Open(dbPath string) (*sql.DB, error) {
	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}

	// CLI-friendly: one connection, stable pragmas
	conn.SetMaxOpenConns(1)
	conn.SetMaxIdleConns(1)
	conn.SetConnMaxLifetime(0)

	// Ensure file is usable now
	if err := conn.Ping(); err != nil {
		_ = conn.Close()
		return nil, err
	}

	// Apply and verify pragmas on a specific connection
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	c, err := conn.Conn(ctx)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	defer c.Close()

	if _, err := c.ExecContext(ctx, "PRAGMA foreign_keys = ON;"); err != nil {
		_ = conn.Close()
		return nil, err
	}

	var enabled int
	if err := c.QueryRowContext(ctx, "PRAGMA foreign_keys;").Scan(&enabled); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if enabled != 1 {
		_ = conn.Close()
		return nil, fmt.Errorf("sqlite foreign key enforcement is OFF (expected ON)")
	}

	return conn, nil
}
