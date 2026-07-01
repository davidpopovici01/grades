package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupProfileTestApp(t *testing.T) (*App, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("GRADES_HOME", home)
	t.Setenv("GRADES_DB_PATH", filepath.Join(home, "grades.db"))
	t.Setenv("GRADES_NO_OPEN", "1")

	var stdout bytes.Buffer
	app, err := New(strings.NewReader(""), &stdout, &stdout)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(func() { _ = app.Close() })

	// Minimal seeded data: one term and one course-year.
	if _, err := app.db.Exec(`INSERT INTO terms(name, start_date, end_date) VALUES ('Fall 2026', '2026-09-01', '2026-12-15')`); err != nil {
		t.Fatalf("insert term: %v", err)
	}
	if _, err := app.db.Exec(`INSERT INTO courses(name) VALUES ('APCSA')`); err != nil {
		t.Fatalf("insert course: %v", err)
	}
	if _, err := app.db.Exec(`INSERT INTO course_years(course_id, name) VALUES (1, 'APCSA 2026-27')`); err != nil {
		t.Fatalf("insert course year: %v", err)
	}
	if _, err := app.db.Exec(`INSERT INTO sections(course_year_id, name) VALUES (1, '12A')`); err != nil {
		t.Fatalf("insert section: %v", err)
	}

	return app, home
}

func TestProfileLoadingFromCurrentCourse(t *testing.T) {
	app, home := setupProfileTestApp(t)

	// Seed a profile file for APCSA.
	ctxDir := filepath.Join(home, "contexts")
	if err := os.MkdirAll(ctxDir, 0o755); err != nil {
		t.Fatalf("mkdir contexts: %v", err)
	}
	profilePath := filepath.Join(ctxDir, "APCSA.yaml")
	content := `context:
  year: "2026-27"
  term_id: 1
  course_year_id: 1
  section_id: 1
  assignment_id: 0
`
	if err := os.WriteFile(profilePath, []byte(content), 0o644); err != nil {
		t.Fatalf("write profile: %v", err)
	}

	// Point main config at APCSA profile.
	app.v.Set("context.current_course", "APCSA")
	if err := app.v.WriteConfig(); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// Re-create app; it should load the profile.
	if err := app.Close(); err != nil {
		t.Fatalf("close app: %v", err)
	}
	var stdout bytes.Buffer
	app2, err := New(strings.NewReader(""), &stdout, &stdout)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	defer app2.Close()

	ctx := app2.context()
	if ctx.Year != "2026-27" {
		t.Errorf("year: got %q, want 2026-27", ctx.Year)
	}
	if ctx.TermID != 1 {
		t.Errorf("term_id: got %d, want 1", ctx.TermID)
	}
	if ctx.CourseYearID != 1 {
		t.Errorf("course_year_id: got %d, want 1", ctx.CourseYearID)
	}
	if ctx.SectionID != 1 {
		t.Errorf("section_id: got %d, want 1", ctx.SectionID)
	}
}

func TestLegacyContextMigration(t *testing.T) {
	app, _ := setupProfileTestApp(t)

	// Simulate legacy single-file context.
	app.v.Set("context.year", "2026-27")
	app.v.Set("context.term_id", 1)
	app.v.Set("context.course_year_id", 1)
	app.v.Set("context.section_id", 1)
	app.v.Set("context.assignment_id", 0)
	if err := app.v.WriteConfig(); err != nil {
		t.Fatalf("write legacy config: %v", err)
	}

	if err := app.Close(); err != nil {
		t.Fatalf("close app: %v", err)
	}
	var stdout bytes.Buffer
	app2, err := New(strings.NewReader(""), &stdout, &stdout)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	defer app2.Close()

	if app2.profileName != "APCSA" {
		t.Errorf("profile name: got %q, want APCSA", app2.profileName)
	}
	ctx := app2.context()
	if ctx.TermID != 1 || ctx.SectionID != 1 {
		t.Errorf("context not migrated: %+v", ctx)
	}
	if app2.v.GetString("context.current_course") != "APCSA" {
		t.Errorf("current_course not set in main config")
	}
}

func TestUseCourseYearSwitchesProfiles(t *testing.T) {
	app, _ := setupProfileTestApp(t)

	if err := app.UseYear("2026-27"); err != nil {
		t.Fatalf("use year: %v", err)
	}
	if err := app.UseTerm("Fall 2026"); err != nil {
		t.Fatalf("use term: %v", err)
	}
	if err := app.UseCourseYear("APCSA"); err != nil {
		t.Fatalf("use course: %v", err)
	}
	if err := app.UseSection("12A"); err != nil {
		t.Fatalf("use section: %v", err)
	}

	// Switch to a different course; this creates a new profile.
	if _, err := app.db.Exec(`INSERT INTO courses(name) VALUES ('APCSP')`); err != nil {
		t.Fatalf("insert APCSP course: %v", err)
	}
	if _, err := app.db.Exec(`INSERT INTO course_years(course_id, name) VALUES (2, 'APCSP 2026-27')`); err != nil {
		t.Fatalf("insert APCSP course year: %v", err)
	}
	if _, err := app.db.Exec(`INSERT INTO sections(course_year_id, name) VALUES (2, '12B')`); err != nil {
		t.Fatalf("insert APCSP section: %v", err)
	}

	if err := app.UseCourseYear("APCSP"); err != nil {
		t.Fatalf("switch course: %v", err)
	}
	if err := app.UseSection("12B"); err != nil {
		t.Fatalf("use APCSP section: %v", err)
	}

	// Re-open and select APCSA; previous APCSA context should be restored.
	if err := app.Close(); err != nil {
		t.Fatalf("close app: %v", err)
	}
	var stdout bytes.Buffer
	app2, err := NewWithClass(strings.NewReader(""), &stdout, &stdout, "APCSA")
	if err != nil {
		t.Fatalf("new app with class: %v", err)
	}
	defer app2.Close()

	ctx := app2.context()
	if ctx.CourseYearID != 1 {
		t.Errorf("APCSA course_year_id: got %d, want 1", ctx.CourseYearID)
	}
	if ctx.SectionID != 1 {
		t.Errorf("APCSA section_id: got %d, want 1", ctx.SectionID)
	}
}

func TestSectionAndAssignmentSavedToActiveProfile(t *testing.T) {
	app, home := setupProfileTestApp(t)

	if err := app.UseYear("2026-27"); err != nil {
		t.Fatalf("use year: %v", err)
	}
	if err := app.UseTerm("Fall 2026"); err != nil {
		t.Fatalf("use term: %v", err)
	}
	if err := app.UseCourseYear("APCSA"); err != nil {
		t.Fatalf("use course: %v", err)
	}
	if err := app.UseSection("12A"); err != nil {
		t.Fatalf("use section: %v", err)
	}

	// Insert an assignment and select it.
	if _, err := app.db.Exec(`INSERT INTO categories(name) VALUES ('Exam')`); err != nil {
		t.Fatalf("insert category: %v", err)
	}
	if _, err := app.db.Exec(`INSERT INTO assignments(course_year_id, term_id, category_id, title, max_points) VALUES (1, 1, 1, 'Quiz', 10)`); err != nil {
		t.Fatalf("insert assignment: %v", err)
	}
	if err := app.UseAssignment("Quiz"); err != nil {
		t.Fatalf("use assignment: %v", err)
	}

	profilePath := filepath.Join(home, "contexts", "APCSA.yaml")
	data, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatalf("read profile: %v", err)
	}
	if !strings.Contains(string(data), "section_id: 1") {
		t.Errorf("profile missing section_id: %s", data)
	}
	if !strings.Contains(string(data), "assignment_id: 1") {
		t.Errorf("profile missing assignment_id: %s", data)
	}
}

func TestClassEnvVarLoadsProfile(t *testing.T) {
	_, home := setupProfileTestApp(t)

	ctxDir := filepath.Join(home, "contexts")
	if err := os.MkdirAll(ctxDir, 0o755); err != nil {
		t.Fatalf("mkdir contexts: %v", err)
	}
	profilePath := filepath.Join(ctxDir, "APCSA.yaml")
	content := `context:
  year: "2026-27"
  term_id: 1
  course_year_id: 1
  section_id: 1
  assignment_id: 0
`
	if err := os.WriteFile(profilePath, []byte(content), 0o644); err != nil {
		t.Fatalf("write profile: %v", err)
	}

	t.Setenv("GRADES_CONTEXT", "APCSA")

	var stdout bytes.Buffer
	app, err := New(strings.NewReader(""), &stdout, &stdout)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	defer app.Close()

	if app.profileName != "APCSA" {
		t.Errorf("profile name: got %q, want APCSA", app.profileName)
	}
	if app.context().SectionID != 1 {
		t.Errorf("section_id: got %d, want 1", app.context().SectionID)
	}
	if app.v.GetString("context.current_course") != "APCSA" {
		t.Errorf("current_course not updated to APCSA")
	}
}

func TestExplicitClassUpdatesCurrentCourse(t *testing.T) {
	_, home := setupProfileTestApp(t)

	ctxDir := filepath.Join(home, "contexts")
	if err := os.MkdirAll(ctxDir, 0o755); err != nil {
		t.Fatalf("mkdir contexts: %v", err)
	}
	profilePath := filepath.Join(ctxDir, "APCSA.yaml")
	content := `context:
  year: "2026-27"
  term_id: 1
  course_year_id: 1
  section_id: 1
  assignment_id: 0
`
	if err := os.WriteFile(profilePath, []byte(content), 0o644); err != nil {
		t.Fatalf("write profile: %v", err)
	}

	var stdout bytes.Buffer
	app, err := NewWithClass(strings.NewReader(""), &stdout, &stdout, "APCSA")
	if err != nil {
		t.Fatalf("new app with class: %v", err)
	}
	defer app.Close()

	if app.v.GetString("context.current_course") != "APCSA" {
		t.Errorf("current_course not updated to APCSA")
	}

	// Re-open without --class; plain grades should now load APCSA.
	if err := app.Close(); err != nil {
		t.Fatalf("close app: %v", err)
	}
	app2, err := New(strings.NewReader(""), &stdout, &stdout)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	defer app2.Close()

	if app2.profileName != "APCSA" {
		t.Errorf("plain grades did not load APCSA: got %q", app2.profileName)
	}
}
