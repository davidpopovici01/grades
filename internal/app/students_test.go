package app

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestSortStudentsRenumbersStudentsAndKeepsReferences(t *testing.T) {
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
	exec(`INSERT INTO course_year_terms(course_year_id, term_id) VALUES (1, 1)`)
	exec(`INSERT INTO sections(section_id, course_year_id, name) VALUES (1, 1, '12A')`)
	exec(`INSERT INTO categories(category_id, name) VALUES (1, 'Exam')`)
	exec(`INSERT INTO students(student_pk, first_name, last_name) VALUES (10, 'Bob', 'Zhang')`)
	exec(`INSERT INTO students(student_pk, first_name, last_name) VALUES (11, 'Alice', 'Brown')`)
	exec(`INSERT INTO students(student_pk, first_name, last_name) VALUES (12, 'Carol', 'Brown')`)
	exec(`INSERT INTO section_enrollments(section_id, student_pk, term_id, start_date, status) VALUES (1, 10, 1, '2026-08-15', 'active')`)
	exec(`INSERT INTO section_enrollments(section_id, student_pk, term_id, start_date, status) VALUES (1, 11, 1, '2026-08-15', 'active')`)
	exec(`INSERT INTO section_enrollments(section_id, student_pk, term_id, start_date, status) VALUES (1, 12, 1, '2026-08-15', 'active')`)
	exec(`INSERT INTO assignments(assignment_id, course_year_id, term_id, category_id, title, max_points) VALUES (1, 1, 1, 1, 'Midterm', 100)`)
	exec(`INSERT INTO grades(assignment_id, student_pk, score, flags_bitmask) VALUES (1, 10, 80, 0), (1, 11, 90, 0), (1, 12, 70, 0)`)
	exec(`INSERT INTO student_accounts(student_pk, username, password_salt, password_hash) VALUES (11, 'alice.brown', 'salt', 'hash')`)

	if err := a.SortStudents(); err != nil {
		t.Fatalf("sort students: %v", err)
	}
	if !strings.Contains(stdout.String(), "Sorted 3 students") {
		t.Fatalf("expected sort confirmation, got:\n%s", stdout.String())
	}

	rows, err := a.db.Query(`SELECT student_pk, first_name, last_name FROM students ORDER BY student_pk`)
	if err != nil {
		t.Fatalf("query students: %v", err)
	}
	defer rows.Close()
	type row struct {
		id    int
		first string
		last  string
	}
	var got []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.first, &r.last); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, r)
	}
	want := []row{{1, "Alice", "Brown"}, {2, "Carol", "Brown"}, {3, "Bob", "Zhang"}}
	if len(got) != len(want) {
		t.Fatalf("expected %d students, got %d", len(want), len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("row %d: expected %+v, got %+v", i, want[i], got[i])
		}
	}

	var score float64
	if err := a.db.QueryRow(`SELECT score FROM grades WHERE assignment_id = 1 AND student_pk = 1`).Scan(&score); err != nil {
		t.Fatalf("query Alice grade: %v", err)
	}
	if score != 90 {
		t.Fatalf("expected Alice's grade to follow her new ID, got %v", score)
	}
	var username string
	if err := a.db.QueryRow(`SELECT username FROM student_accounts WHERE student_pk = 1`).Scan(&username); err != nil {
		t.Fatalf("query Alice portal account: %v", err)
	}
	if username != "alice.brown" {
		t.Fatalf("expected portal account to follow new ID, got %q", username)
	}

	if _, err := a.db.Exec(`INSERT INTO grades(assignment_id, student_pk, score, flags_bitmask) VALUES (1, 999, 50, 0)`); err == nil {
		t.Fatalf("expected foreign key enforcement to stay enabled after sort")
	}

	var seq int
	if err := a.db.QueryRow(`SELECT seq FROM sqlite_sequence WHERE name = 'students'`).Scan(&seq); err != nil {
		t.Fatalf("query sqlite_sequence: %v", err)
	}
	if seq != 3 {
		t.Fatalf("expected sqlite_sequence seq 3, got %d", seq)
	}

	stdout.Reset()
	if err := a.SortStudents(); err != nil {
		t.Fatalf("second sort: %v", err)
	}
	if !strings.Contains(stdout.String(), "already sorted") {
		t.Fatalf("expected already-sorted message, got:\n%s", stdout.String())
	}
}

func newEditTestApp(t *testing.T, input string) (*App, *bytes.Buffer) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("GRADES_HOME", home)
	t.Setenv("GRADES_DB_PATH", filepath.Join(home, "grades.db"))
	t.Setenv("GRADES_NO_OPEN", "1")

	stdout := &bytes.Buffer{}
	var stderr bytes.Buffer
	a, err := New(strings.NewReader(input), stdout, &stderr)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	t.Cleanup(func() { a.Close() })

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
	exec(`INSERT INTO students(student_pk, first_name, last_name, powerschool_num) VALUES (1, 'Alice', 'Brown', '100401')`)
	exec(`INSERT INTO students(student_pk, first_name, last_name, school_student_id) VALUES (2, 'Bob', 'Zhang', '251302')`)
	exec(`INSERT INTO section_enrollments(section_id, student_pk, term_id, start_date, status) VALUES (1, 1, 1, '2026-08-15', 'active')`)
	exec(`INSERT INTO section_enrollments(section_id, student_pk, term_id, start_date, status) VALUES (1, 2, 1, '2026-08-15', 'active')`)

	a.v.Set("context.term_id", 1)
	a.v.Set("context.course_year_id", 1)
	a.v.Set("context.section_id", 1)
	return a, stdout
}

func TestEditStudentUpdatesFieldsAndKeepsDefaults(t *testing.T) {
	a, stdout := newEditTestApp(t, "Alicia\n\nZhang Alice\n251301\n-\n")

	if err := a.EditStudentInteractive("Alice"); err != nil {
		t.Fatalf("edit student: %v", err)
	}
	if !strings.Contains(stdout.String(), "Updated student: Alicia Brown") {
		t.Fatalf("expected update confirmation, got:\n%s", stdout.String())
	}

	var first, last, chinese string
	var schoolID, psNum *string
	err := a.db.QueryRow(`SELECT first_name, last_name, COALESCE(chinese_name, ''), school_student_id, powerschool_num FROM students WHERE student_pk = 1`).
		Scan(&first, &last, &chinese, &schoolID, &psNum)
	if err != nil {
		t.Fatalf("query student: %v", err)
	}
	if first != "Alicia" || last != "Brown" || chinese != "Zhang Alice" {
		t.Fatalf("unexpected names: %q %q %q", first, last, chinese)
	}
	if schoolID == nil || *schoolID != "251301" {
		t.Fatalf("expected student ID 251301, got %v", schoolID)
	}
	if psNum != nil {
		t.Fatalf("expected PowerSchool number cleared, got %v", *psNum)
	}
}

func TestEditStudentRejectsDuplicateStudentID(t *testing.T) {
	a, _ := newEditTestApp(t, "\n\n\n251302\n\n")

	err := a.EditStudentInteractive("Alice")
	if err == nil || !strings.Contains(err.Error(), "already belongs to Bob Zhang") {
		t.Fatalf("expected duplicate ID error, got %v", err)
	}
}

func TestEditStudentIDAssignsIDsInLoop(t *testing.T) {
	a, stdout := newEditTestApp(t, "Alice\n251301\nNobody\nBob\n\n\n")

	if err := a.EditStudentIDInteractive(); err != nil {
		t.Fatalf("edit student id: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "Set Alice Brown student ID to 251301") {
		t.Fatalf("expected ID assignment confirmation, got:\n%s", out)
	}
	if !strings.Contains(out, `no student matched "nobody"`) {
		t.Fatalf("expected unmatched-student retry message, got:\n%s", out)
	}

	var schoolID *string
	if err := a.db.QueryRow(`SELECT school_student_id FROM students WHERE student_pk = 1`).Scan(&schoolID); err != nil {
		t.Fatalf("query student: %v", err)
	}
	if schoolID == nil || *schoolID != "251301" {
		t.Fatalf("expected student ID 251301, got %v", schoolID)
	}

	var bobID *string
	if err := a.db.QueryRow(`SELECT school_student_id FROM students WHERE student_pk = 2`).Scan(&bobID); err != nil {
		t.Fatalf("query Bob: %v", err)
	}
	if bobID == nil || *bobID != "251302" {
		t.Fatalf("expected Bob's ID unchanged at 251302, got %v", bobID)
	}
}

func TestEditStudentIDRejectsDuplicate(t *testing.T) {
	a, stdout := newEditTestApp(t, "Alice\n251302\n\n")

	if err := a.EditStudentIDInteractive(); err != nil {
		t.Fatalf("edit student id: %v", err)
	}
	if !strings.Contains(stdout.String(), "already belongs to Bob Zhang") {
		t.Fatalf("expected duplicate ID message, got:\n%s", stdout.String())
	}
	var schoolID *string
	if err := a.db.QueryRow(`SELECT school_student_id FROM students WHERE student_pk = 1`).Scan(&schoolID); err != nil {
		t.Fatalf("query student: %v", err)
	}
	if schoolID != nil {
		t.Fatalf("expected Alice's ID to stay empty, got %v", *schoolID)
	}
}

func TestUpsertStudentCapitalizesNames(t *testing.T) {
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

	student, err := a.upsertStudent("alice", "brown", "", "")
	if err != nil {
		t.Fatalf("upsert student: %v", err)
	}
	if student.FirstName != "Alice" || student.LastName != "Brown" {
		t.Fatalf("expected capitalized names, got %q %q", student.FirstName, student.LastName)
	}

	var first, last string
	if err := a.db.QueryRow(`SELECT first_name, last_name FROM students WHERE student_pk = ?`, student.ID).Scan(&first, &last); err != nil {
		t.Fatalf("query student: %v", err)
	}
	if first != "Alice" || last != "Brown" {
		t.Fatalf("expected capitalized names in db, got %q %q", first, last)
	}
}

func TestCapitalizeName(t *testing.T) {
	cases := map[string]string{
		"alice":    "Alice",
		"Alice":    "Alice",
		"mcDonald": "McDonald",
		"":         "",
		"张伟":       "张伟",
	}
	for input, want := range cases {
		if got := capitalizeName(input); got != want {
			t.Fatalf("capitalizeName(%q) = %q, want %q", input, got, want)
		}
	}
}
