package app

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportGradesHighlightsChangedScores(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRADES_HOME", home)
	t.Setenv("GRADES_DB_PATH", filepath.Join(home, "grades.db"))
	t.Setenv("GRADES_NO_OPEN", "1")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	a, err := New(strings.NewReader("y\n"), &stdout, &stderr)
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
	exec(`INSERT INTO course_year_terms(course_year_id, term_id) VALUES (1, 1)`)
	exec(`INSERT INTO sections(section_id, course_year_id, name) VALUES (1, 1, '12A')`)
	exec(`INSERT INTO categories(category_id, name) VALUES (1, 'Exam')`)
	exec(`INSERT INTO students(student_pk, first_name, last_name, powerschool_num) VALUES (10, 'Alice', 'Brown', '100401')`)
	exec(`INSERT INTO students(student_pk, first_name, last_name, powerschool_num) VALUES (11, 'Bob', 'Zhang', '100402')`)
	exec(`INSERT INTO section_enrollments(section_id, student_pk, term_id, start_date, status) VALUES (1, 10, 1, '2026-08-15', 'active')`)
	exec(`INSERT INTO section_enrollments(section_id, student_pk, term_id, start_date, status) VALUES (1, 11, 1, '2026-08-15', 'active')`)
	exec(`INSERT INTO assignments(assignment_id, course_year_id, term_id, category_id, title, max_points) VALUES (1, 1, 1, 1, 'Midterm', 100)`)
	exec(`INSERT INTO grades(assignment_id, student_pk, score, flags_bitmask) VALUES (1, 10, 90, 0), (1, 11, 80, 0)`)

	a.v.Set("context.term_id", 1)
	a.v.Set("context.course_year_id", 1)
	a.v.Set("context.section_id", 1)
	a.v.Set("context.assignment_id", 1)

	exportPath := filepath.Join(home, "midterm.csv")
	if err := a.ExportGrades(exportPath); err != nil {
		t.Fatalf("first export: %v", err)
	}
	first := stdout.String()
	if !strings.Contains(first, "Brown, Alice") || !strings.Contains(first, "90.00") {
		t.Fatalf("expected first export to list Alice, got:\n%s", first)
	}
	if !strings.Contains(first, colorGreen("90.00")) {
		t.Fatalf("expected first export to highlight new Alice score, got:\n%s", first)
	}

	exec(`UPDATE grades SET score = 95 WHERE assignment_id = 1 AND student_pk = 10`)

	stdout.Reset()
	a.in.Reset(strings.NewReader("y\n"))
	if err := a.ExportGrades(exportPath); err != nil {
		t.Fatalf("second export: %v", err)
	}
	second := stdout.String()
	if !strings.Contains(second, "This assignment changed since its last export.") {
		t.Fatalf("expected second export to detect change, got:\n%s", second)
	}
	if !strings.Contains(second, colorGreen("95.00")) {
		t.Fatalf("expected changed Alice score to be highlighted green, got:\n%s", second)
	}
	if strings.Contains(second, colorGreen("80.00")) {
		t.Fatalf("expected unchanged Bob score not to be highlighted green, got:\n%s", second)
	}
}

func TestAssignmentExportRowsMapUsesPowerSchoolNumOrName(t *testing.T) {
	rows := []assignmentExportRow{
		{PowerSchoolNum: "100401", StudentName: "Brown, Alice", Score: "90.00"},
		{PowerSchoolNum: "", StudentName: "Zhang, Bob", Score: "80.00"},
	}
	m := assignmentExportRowsMap(rows)
	if got, want := m["100401"], "90.00"; got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
	if got, want := m["Zhang, Bob"], "80.00"; got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}
