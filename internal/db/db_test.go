package db_test

import (
	"path/filepath"
	"testing"

	"github.com/davidpopovici01/grades/internal/db"
)

func TestOpen_EnforcesForeignKeys(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.db")

	conn, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer conn.Close()

	// Create schema with FK
	_, err = conn.Exec(`
		CREATE TABLE parent(id INTEGER PRIMARY KEY);
		CREATE TABLE child(
			id INTEGER PRIMARY KEY,
			parent_id INTEGER NOT NULL,
			FOREIGN KEY(parent_id) REFERENCES parent(id)
		);
	`)
	if err != nil {
		t.Fatalf("create tables: %v", err)
	}

	// Violating FK must fail if FK enforcement is truly ON
	_, err = conn.Exec(`INSERT INTO child(id, parent_id) VALUES (1, 999);`)
	if err == nil {
		t.Fatalf("expected FK constraint error, got nil (FK enforcement likely OFF)")
	}
}
