package migrate

import (
	"database/sql"
	_ "embed"
	"fmt"
	"time"
)

//go:embed sql/001_init.sql
var initSQL string

//go:embed sql/002_assignment_weight.sql
var assignmentWeightSQL string

//go:embed sql/003_category_aliases.sql
var categoryAliasesSQL string

//go:embed sql/004_grading_models.sql
var gradingModelsSQL string

//go:embed sql/005_pass_rates.sql
var passRatesSQL string

//go:embed sql/006_assignment_exports.sql
var assignmentExportsSQL string

type migration struct {
	version string
	sql     string
}

var migrations = []migration{
	{version: "001_init", sql: initSQL},
	{version: "002_assignment_weight", sql: assignmentWeightSQL},
	{version: "003_category_aliases", sql: categoryAliasesSQL},
	{version: "004_grading_models", sql: gradingModelsSQL},
	{version: "005_pass_rates", sql: passRatesSQL},
	{version: "006_assignment_exports", sql: assignmentExportsSQL},
}

func Up(db *sql.DB) error {
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TEXT NOT NULL
		);`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	for _, m := range migrations {
		var count int
		if err := db.QueryRow(`SELECT COUNT(1) FROM schema_migrations WHERE version = ?`, m.version).Scan(&count); err != nil {
			return fmt.Errorf("check schema_migrations for %s: %w", m.version, err)
		}
		if count > 0 {
			continue
		}
		if err := apply(db, m); err != nil {
			return err
		}
	}
	return nil
}

func Down(db *sql.DB) error {
	statements := []string{
		`DROP TABLE IF EXISTS grade_audit;`,
		`DROP TABLE IF EXISTS grades;`,
		`DROP TABLE IF EXISTS assignment_curves;`,
		`DROP TABLE IF EXISTS assignment_exports;`,
		`DROP TABLE IF EXISTS assignments;`,
		`DROP TABLE IF EXISTS category_grading_policies;`,
		`DROP TABLE IF EXISTS category_aliases;`,
		`DROP TABLE IF EXISTS section_enrollments;`,
		`DROP TABLE IF EXISTS sections;`,
		`DROP TABLE IF EXISTS course_year_terms;`,
		`DROP TABLE IF EXISTS course_years;`,
		`DROP TABLE IF EXISTS courses;`,
		`DROP TABLE IF EXISTS category_scheme_weights;`,
		`DROP TABLE IF EXISTS category_schemes;`,
		`DROP TABLE IF EXISTS categories;`,
		`DROP TABLE IF EXISTS terms;`,
		`DROP TABLE IF EXISTS students;`,
		`DROP TABLE IF EXISTS schema_migrations;`,
	}
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return fmt.Errorf("drop schema: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit down migration: %w", err)
	}
	return nil
}

func apply(db *sql.DB, m migration) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx for %s: %w", m.version, err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(m.sql); err != nil {
		return fmt.Errorf("apply %s: %w", m.version, err)
	}
	appliedAt := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES (?, ?)`, m.version, appliedAt); err != nil {
		return fmt.Errorf("record %s: %w", m.version, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit %s: %w", m.version, err)
	}
	return nil
}
