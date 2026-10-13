package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testRubricFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "rubric.docx"))
	if err != nil {
		t.Fatalf("read rubric fixture: %v", err)
	}
	return data
}

func TestParseRubricDocx(t *testing.T) {
	rubric, err := ParseRubricDocx(testRubricFixture(t))
	if err != nil {
		t.Fatalf("parse rubric: %v", err)
	}
	if rubric.Title != "Unit 1 Coding Project" {
		t.Fatalf("expected title %q, got %q", "Unit 1 Coding Project", rubric.Title)
	}
	if len(rubric.Criteria) != 8 {
		t.Fatalf("expected 8 criteria, got %d", len(rubric.Criteria))
	}
	if rubric.Criteria[0].Name != "Code Organization" {
		t.Fatalf("expected first criterion %q, got %q", "Code Organization", rubric.Criteria[0].Name)
	}
	for _, criterion := range rubric.Criteria {
		if criterion.MaxPoints != 5 {
			t.Fatalf("expected criterion %q max 5, got %d", criterion.Name, criterion.MaxPoints)
		}
	}
	if rubric.Total() != 40 {
		t.Fatalf("expected total 40, got %d", rubric.Total())
	}
}

func TestFillRubricDocx(t *testing.T) {
	scores := []int{4, 5, 3, 4, 5, 4, 3, 4}
	filled, err := FillRubricDocx(testRubricFixture(t), "Alice Wang", scores)
	if err != nil {
		t.Fatalf("fill rubric: %v", err)
	}
	doc, err := readDocxDocumentXML(filled)
	if err != nil {
		t.Fatalf("re-open filled rubric: %v", err)
	}
	for _, want := range []string{"Name: Alice Wang", "Total: 32/40", "4/5", "5/5", "3/5"} {
		if !strings.Contains(doc, want) {
			t.Fatalf("expected filled document to contain %q", want)
		}
	}
	if strings.Contains(doc, "____/5") {
		t.Fatalf("expected all score blanks to be filled")
	}
}

func TestParseRubricKey(t *testing.T) {
	cases := []struct {
		name   string
		input  []byte
		action rubricKeyAction
		digit  int
	}{
		{"up arrow", []byte("\x1b[A"), rubricActionUp, -1},
		{"down arrow", []byte("\x1b[B"), rubricActionDown, -1},
		{"escape alone", []byte("\x1b"), rubricActionCancel, -1},
		{"digit", []byte("4"), rubricActionDigit, 4},
		{"enter", []byte("\r"), rubricActionDown, -1},
		{"save", []byte("s"), rubricActionSave, -1},
		{"cancel", []byte("q"), rubricActionCancel, -1},
		{"ctrl-c", []byte{0x03}, rubricActionCancel, -1},
		{"late toggle", []byte("l"), rubricActionToggleLate, -1},
		{"redo toggle", []byte("r"), rubricActionToggleRedo, -1},
		{"unknown", []byte("z"), rubricActionNone, -1},
		{"empty", nil, rubricActionNone, -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			action, digit := parseRubricKey(tc.input)
			if action != tc.action || digit != tc.digit {
				t.Fatalf("parseRubricKey(%q) = (%d, %d), want (%d, %d)", tc.input, action, digit, tc.action, tc.digit)
			}
		})
	}
}

func setupRubricTestApp(t *testing.T, input string) (*App, *bytes.Buffer) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("GRADES_HOME", home)
	t.Setenv("GRADES_DB_PATH", filepath.Join(home, "grades.db"))
	t.Setenv("GRADES_NO_OPEN", "1")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	a, err := New(strings.NewReader(input), &stdout, &stderr)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}

	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := a.db.Exec(query, args...); err != nil {
			t.Fatalf("exec %q: %v", query, err)
		}
	}
	exec(`INSERT INTO terms(term_id, name, start_date, end_date) VALUES (1, 'Fall 2026', '2026-08-15', '2026-12-20')`)
	exec(`INSERT INTO courses(course_id, name) VALUES (1, 'APCSA')`)
	exec(`INSERT INTO course_years(course_year_id, course_id, name) VALUES (1, 1, 'APCSA 2026-27')`)
	exec(`INSERT INTO categories(category_id, name) VALUES (1, 'Project')`)
	exec(`INSERT INTO sections(section_id, course_year_id, name) VALUES (1, 1, 'APCSA2')`)
	exec(`INSERT INTO students(student_pk, first_name, last_name) VALUES (10, 'Alice', 'Brown')`)
	exec(`INSERT INTO section_enrollments(section_id, student_pk, term_id, start_date) VALUES (1, 10, 1, '2026-08-15')`)
	exec(`INSERT INTO assignments(assignment_id, course_year_id, term_id, category_id, title, max_points) VALUES (1, 1, 1, 1, 'Unit 1 Coding Project', 40)`)
	a.setContext("context.term_id", 1)
	a.setContext("context.course_year_id", 1)
	a.setContext("context.assignment_id", 1)
	return a, &stdout
}

func TestRubricEnterRecordsGradeAndFile(t *testing.T) {
	input := "y\n" + // confirm link
		"alice\n" + // search student
		"4\n5\n3\n4\n5\n4\n3\n4\n" + // 8 criteria = 32
		"\n" + // accept flags
		"done\n"
	a, stdout := setupRubricTestApp(t, input)
	defer a.Close()

	if err := a.LinkRubric(filepath.Join("testdata", "rubric.docx")); err != nil {
		t.Fatalf("link rubric: %v", err)
	}
	if err := a.RubricEnter(); err != nil {
		t.Fatalf("rubric enter: %v", err)
	}

	var score float64
	if err := a.db.QueryRow(`SELECT score FROM grades WHERE assignment_id = 1 AND student_pk = 10`).Scan(&score); err != nil {
		t.Fatalf("query grade: %v", err)
	}
	if score != 32 {
		t.Fatalf("expected grade 32, got %v", score)
	}

	var total float64
	var filePath string
	if err := a.db.QueryRow(`SELECT total, file_path FROM rubric_scores WHERE assignment_id = 1 AND student_pk = 10`).Scan(&total, &filePath); err != nil {
		t.Fatalf("query rubric scores: %v", err)
	}
	if total != 32 {
		t.Fatalf("expected rubric total 32, got %v", total)
	}
	if _, err := os.Stat(filePath); err != nil {
		t.Fatalf("expected rubric file to exist: %v", err)
	}
	if !strings.Contains(stdout.String(), "Saved ") {
		t.Fatalf("expected save confirmation, got:\n%s", stdout.String())
	}
}

func TestRubricEnterUpdatesExistingRubric(t *testing.T) {
	input := "y\n" +
		"alice\n" +
		"4\n5\n3\n4\n5\n4\n3\n4\n" +
		"\n" +
		"alice\n" + // re-grade the same student
		"\n4\n\n\n\n\n\n\n" + // keep all but criterion 2: 5 -> 4
		"\n" +
		"done\n"
	a, stdout := setupRubricTestApp(t, input)
	defer a.Close()

	if err := a.LinkRubric(filepath.Join("testdata", "rubric.docx")); err != nil {
		t.Fatalf("link rubric: %v", err)
	}
	if err := a.RubricEnter(); err != nil {
		t.Fatalf("rubric enter: %v", err)
	}

	var score float64
	if err := a.db.QueryRow(`SELECT score FROM grades WHERE assignment_id = 1 AND student_pk = 10`).Scan(&score); err != nil {
		t.Fatalf("query grade: %v", err)
	}
	if score != 31 {
		t.Fatalf("expected updated grade 31, got %v", score)
	}
	var auditRows int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM grade_audit WHERE assignment_id = 1 AND student_pk = 10 AND action = 'UPDATE'`).Scan(&auditRows); err != nil {
		t.Fatalf("query audit: %v", err)
	}
	if auditRows == 0 {
		t.Fatalf("expected an audit row for the rubric update")
	}
	if !strings.Contains(stdout.String(), "already has a rubric") {
		t.Fatalf("expected existing-rubric warning, got:\n%s", stdout.String())
	}
}

func TestRubricListShowsDoneAndPending(t *testing.T) {
	input := "y\n" +
		"alice\n" +
		"4\n5\n3\n4\n5\n4\n3\n4\n" +
		"\n" +
		"done\n"
	a, _ := setupRubricTestApp(t, input)
	defer a.Close()

	if err := a.LinkRubric(filepath.Join("testdata", "rubric.docx")); err != nil {
		t.Fatalf("link rubric: %v", err)
	}
	if err := a.RubricEnter(); err != nil {
		t.Fatalf("rubric enter: %v", err)
	}
	if _, err := a.db.Exec(`INSERT INTO students(student_pk, first_name, last_name) VALUES (11, 'Bob', 'Smith')`); err != nil {
		t.Fatalf("add student: %v", err)
	}
	if _, err := a.db.Exec(`INSERT INTO section_enrollments(section_id, student_pk, term_id, start_date) VALUES (1, 11, 1, '2026-08-15')`); err != nil {
		t.Fatalf("enroll student: %v", err)
	}

	var listOut bytes.Buffer
	a.out = &listOut
	if err := a.RubricList(); err != nil {
		t.Fatalf("rubric list: %v", err)
	}
	out := listOut.String()
	if !strings.Contains(out, "32/40") {
		t.Fatalf("expected graded student total in list, got:\n%s", out)
	}
	if !strings.Contains(out, "—") {
		t.Fatalf("expected pending marker in list, got:\n%s", out)
	}
}

func TestRubricEnterWithoutLinkFails(t *testing.T) {
	a, _ := setupRubricTestApp(t, "")
	defer a.Close()
	err := a.RubricEnter()
	if err == nil || !strings.Contains(err.Error(), "rubric link") {
		t.Fatalf("expected link guidance error, got %v", err)
	}
}
