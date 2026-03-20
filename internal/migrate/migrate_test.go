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

	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version='002_assignment_weight';`).Scan(&count); err != nil {
		t.Fatalf("schema_migrations query failed for 002: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected schema_migrations to contain 002_assignment_weight once, got %d", count)
	}

	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version='003_category_aliases';`).Scan(&count); err != nil {
		t.Fatalf("schema_migrations query failed for 003: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected schema_migrations to contain 003_category_aliases once, got %d", count)
	}

	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version='004_grading_models';`).Scan(&count); err != nil {
		t.Fatalf("schema_migrations query failed for 004: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected schema_migrations to contain 004_grading_models once, got %d", count)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version='005_pass_rates';`).Scan(&count); err != nil {
		t.Fatalf("schema_migrations query failed for 005: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected schema_migrations to contain 005_pass_rates once, got %d", count)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version='006_assignment_exports';`).Scan(&count); err != nil {
		t.Fatalf("schema_migrations query failed for 006: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected schema_migrations to contain 006_assignment_exports once, got %d", count)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version='007_student_portal';`).Scan(&count); err != nil {
		t.Fatalf("schema_migrations query failed for 007: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected schema_migrations to contain 007_student_portal once, got %d", count)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version='008_submissions';`).Scan(&count); err != nil {
		t.Fatalf("schema_migrations query failed for 008: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected schema_migrations to contain 008_submissions once, got %d", count)
	}

	var weightColumn int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('assignments') WHERE name='weight_percent';`).Scan(&weightColumn); err != nil {
		t.Fatalf("pragma_table_info query failed: %v", err)
	}
	if weightColumn != 1 {
		t.Fatalf("expected assignments.weight_percent column to exist, got %d", weightColumn)
	}

	var aliasTable int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='category_aliases';`).Scan(&aliasTable); err != nil {
		t.Fatalf("category_aliases table query failed: %v", err)
	}
	if aliasTable != 1 {
		t.Fatalf("expected category_aliases table to exist, got %d", aliasTable)
	}

	var policyTable int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='category_grading_policies';`).Scan(&policyTable); err != nil {
		t.Fatalf("category_grading_policies table query failed: %v", err)
	}
	if policyTable != 1 {
		t.Fatalf("expected category_grading_policies table to exist, got %d", policyTable)
	}

	var curveTable int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='assignment_curves';`).Scan(&curveTable); err != nil {
		t.Fatalf("assignment_curves table query failed: %v", err)
	}
	if curveTable != 1 {
		t.Fatalf("expected assignment_curves table to exist, got %d", curveTable)
	}

	var redoCountColumn int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('grades') WHERE name='redo_count';`).Scan(&redoCountColumn); err != nil {
		t.Fatalf("pragma_table_info grades query failed: %v", err)
	}
	if redoCountColumn != 1 {
		t.Fatalf("expected grades.redo_count column to exist, got %d", redoCountColumn)
	}

	var assignmentPassColumn int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('assignments') WHERE name='pass_percent';`).Scan(&assignmentPassColumn); err != nil {
		t.Fatalf("pragma_table_info assignments pass_percent query failed: %v", err)
	}
	if assignmentPassColumn != 1 {
		t.Fatalf("expected assignments.pass_percent column to exist, got %d", assignmentPassColumn)
	}

	var categoryPassColumn int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('category_grading_policies') WHERE name='default_pass_percent';`).Scan(&categoryPassColumn); err != nil {
		t.Fatalf("pragma_table_info category_grading_policies default_pass_percent query failed: %v", err)
	}
	if categoryPassColumn != 1 {
		t.Fatalf("expected category_grading_policies.default_pass_percent column to exist, got %d", categoryPassColumn)
	}

	var assignmentExportsTable int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='assignment_exports';`).Scan(&assignmentExportsTable); err != nil {
		t.Fatalf("assignment_exports table query failed: %v", err)
	}
	if assignmentExportsTable != 1 {
		t.Fatalf("expected assignment_exports table to exist, got %d", assignmentExportsTable)
	}

	var studentAccountsTable int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='student_accounts';`).Scan(&studentAccountsTable); err != nil {
		t.Fatalf("student_accounts table query failed: %v", err)
	}
	if studentAccountsTable != 1 {
		t.Fatalf("expected student_accounts table to exist, got %d", studentAccountsTable)
	}

	var submissionPoliciesTable int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='submission_policies';`).Scan(&submissionPoliciesTable); err != nil {
		t.Fatalf("submission_policies table query failed: %v", err)
	}
	if submissionPoliciesTable != 1 {
		t.Fatalf("expected submission_policies table to exist, got %d", submissionPoliciesTable)
	}
}
