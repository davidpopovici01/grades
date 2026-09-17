package app

import (
	"bytes"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveGradePassKeepsRedoPenaltyForBelowThresholdScore(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRADES_HOME", home)
	t.Setenv("GRADES_DB_PATH", filepath.Join(home, "grades.db"))
	t.Setenv("GRADES_NO_OPEN", "1")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	a, err := New(strings.NewReader(""), &stdout, &stderr)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	defer a.Close()

	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := a.db.Exec(query, args...); err != nil {
			t.Fatalf("exec %q: %v", query, err)
		}
	}
	exec(`INSERT INTO terms(term_id, name, start_date, end_date) VALUES (1, 'Fall 2026', '2026-08-15', '2026-12-20')`)
	exec(`INSERT INTO courses(course_id, name) VALUES (1, 'APCSA')`)
	exec(`INSERT INTO course_years(course_year_id, course_id, name) VALUES (1, 1, 'APCSA 2026-27')`)
	exec(`INSERT INTO categories(category_id, name) VALUES (1, 'Homework')`)
	exec(`INSERT INTO students(student_pk, first_name, last_name) VALUES (10, 'William', 'Wang')`)
	exec(`INSERT INTO assignments(assignment_id, course_year_id, term_id, category_id, title, max_points, pass_percent) VALUES (1, 1, 1, 1, 'U1Hw1', 14, 80)`)

	// Below-threshold score (11/14 = 78.6% < 80%) without a stored redo flag,
	// as produced by imports and other non-interactive entry paths.
	exec(`INSERT INTO grades(assignment_id, student_pk, score, flags_bitmask) VALUES (1, 10, 11, 0)`)

	prev, err := a.currentGrade(1, 10)
	if err != nil {
		t.Fatalf("current grade: %v", err)
	}
	if err := a.saveGrade(1, 10, gradeEntry{Flags: flagPass}, prev); err != nil {
		t.Fatalf("save grade: %v", err)
	}

	var score float64
	var flags int
	var redoCount int
	if err := a.db.QueryRow(`SELECT score, flags_bitmask, COALESCE(redo_count, 0) FROM grades WHERE assignment_id = 1 AND student_pk = 10`).
		Scan(&score, &flags, &redoCount); err != nil {
		t.Fatalf("query grade: %v", err)
	}
	if score != 14 {
		t.Fatalf("expected pass to fill max score 14, got %v", score)
	}
	if flags != flagPass|flagRedo {
		t.Fatalf("expected flags %d (pass+redo), got %d", flagPass|flagRedo, flags)
	}
	if redoCount != 0 {
		t.Fatalf("expected redo count to stay 0 for an inferred redo, got %d", redoCount)
	}

	meta, err := a.assignmentScoreMeta(1)
	if err != nil {
		t.Fatalf("score meta: %v", err)
	}
	record := GradeRecord{
		Score:     sql.NullFloat64{Float64: score, Valid: true},
		Flags:     flags,
		MaxPoints: meta.MaxPoints,
	}
	if percent := assignmentCountsAsPercent(record, meta); percent != 90 {
		t.Fatalf("expected redo pass to count 90%%, got %v", percent)
	}
}

func TestSaveGradePassDoesNotAddRedoForPassingScore(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRADES_HOME", home)
	t.Setenv("GRADES_DB_PATH", filepath.Join(home, "grades.db"))
	t.Setenv("GRADES_NO_OPEN", "1")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	a, err := New(strings.NewReader(""), &stdout, &stderr)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	defer a.Close()

	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := a.db.Exec(query, args...); err != nil {
			t.Fatalf("exec %q: %v", query, err)
		}
	}
	exec(`INSERT INTO terms(term_id, name, start_date, end_date) VALUES (1, 'Fall 2026', '2026-08-15', '2026-12-20')`)
	exec(`INSERT INTO courses(course_id, name) VALUES (1, 'APCSA')`)
	exec(`INSERT INTO course_years(course_year_id, course_id, name) VALUES (1, 1, 'APCSA 2026-27')`)
	exec(`INSERT INTO categories(category_id, name) VALUES (1, 'Homework')`)
	exec(`INSERT INTO students(student_pk, first_name, last_name) VALUES (10, 'Alice', 'Brown')`)
	exec(`INSERT INTO assignments(assignment_id, course_year_id, term_id, category_id, title, max_points, pass_percent) VALUES (1, 1, 1, 1, 'U1Hw1', 14, 80)`)
	// At-threshold score (12/14 = 85.7% >= 80%): a plain pass must not gain a redo penalty.
	exec(`INSERT INTO grades(assignment_id, student_pk, score, flags_bitmask) VALUES (1, 10, 12, 0)`)

	prev, err := a.currentGrade(1, 10)
	if err != nil {
		t.Fatalf("current grade: %v", err)
	}
	if err := a.saveGrade(1, 10, gradeEntry{Flags: flagPass}, prev); err != nil {
		t.Fatalf("save grade: %v", err)
	}

	var flags int
	if err := a.db.QueryRow(`SELECT flags_bitmask FROM grades WHERE assignment_id = 1 AND student_pk = 10`).Scan(&flags); err != nil {
		t.Fatalf("query grade: %v", err)
	}
	if flags != flagPass {
		t.Fatalf("expected flags %d (pass only), got %d", flagPass, flags)
	}
}
