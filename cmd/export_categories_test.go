package cmd_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCategoryExportUsesTotalsAndOnlyExportsConfirmedChanges(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	dbConn := openSeedDB(t, env)
	defer dbConn.Close()

	statements := []string{
		`INSERT INTO sections(section_id, course_year_id, name) VALUES (2, 1, '12B')`,
		`INSERT INTO categories(category_id, name) VALUES (2, 'Homework')`,
		`INSERT INTO students(student_pk, first_name, last_name, powerschool_num, status) VALUES
			(10, 'Alice', 'Brown', '100401', 'active'),
			(11, 'Bob', 'Zhang', '100402', 'inactive'),
			(12, 'Carol', 'Lin', '100403', 'active')`,
		`INSERT INTO section_enrollments(section_id, student_pk, term_id, start_date, status) VALUES
			(1, 10, 1, '2026-08-15', 'active'),
			(2, 11, 1, '2026-08-15', 'active'),
			(1, 12, 1, '2026-08-15', 'active')`,
		`INSERT INTO category_schemes(scheme_id, name) VALUES (1, 'APCSA Fall')`,
		`INSERT INTO category_scheme_weights(scheme_id, category_id, weight_percent) VALUES (1, 1, 60), (1, 2, 40)`,
		`UPDATE course_year_terms SET scheme_id = 1 WHERE course_year_id = 1 AND term_id = 1`,
		`INSERT INTO assignments(assignment_id, course_year_id, term_id, category_id, title, max_points) VALUES
			(1, 1, 1, 1, 'Midterm', 100),
			(2, 1, 1, 2, 'HW1', 20),
			(3, 1, 1, 2, 'HW2', 20)`,
		`INSERT INTO grades(assignment_id, student_pk, score, flags_bitmask) VALUES
			(1, 10, 90, 0), (1, 11, 80, 0),
			(2, 10, 17, 0), (2, 11, 20, 0),
			(3, 10, 20, 0), (3, 11, 20, 0)`,
	}
	for _, statement := range statements {
		if _, err := dbConn.Exec(statement); err != nil {
			t.Fatalf("seed category export: %v", err)
		}
	}

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")
	mustRun(t, env, "", "use", "section", "12A")

	first := mustRun(t, env, "y\ny\n", "categories", "export")
	assertContains(t, first, "[1/2] Exam")
	assertContains(t, first, "Assignment Name:\tExam")
	assertContains(t, first, "Category:\tExam")
	assertContains(t, first, "Weight of Total Grade:\t60.0%")
	assertContains(t, first, "[2/2] Homework")
	assertContains(t, first, "Weight of Total Grade:\t40.0%")

	exportDir := filepath.Clean(filepath.Join(env.home, "..", "gradesCategoryExports"))
	assignmentCategoryPath := filepath.Clean(filepath.Join(env.home, "..", "gradesExports", "Exam.csv"))
	if _, err := os.Stat(assignmentCategoryPath); !os.IsNotExist(err) {
		t.Fatalf("category export must not be written to the assignment export directory")
	}
	examPath := filepath.Join(exportDir, "Exam.csv")
	homeworkPath := filepath.Join(exportDir, "Homework.csv")
	examData, err := os.ReadFile(examPath)
	if err != nil {
		t.Fatalf("read exam category export: %v", err)
	}
	assertContains(t, string(examData), "Assignment Name:,Exam,")
	assertContains(t, string(examData), "Points Possible:,100.0,")
	assertContains(t, string(examData), `100401,"Brown, Alice",90.00`)
	assertContains(t, string(examData), "100403,\"Lin, Carol\",\n")
	assertContains(t, string(examData), "100402,\"Zhang, Bob\",\n")

	homeworkData, err := os.ReadFile(homeworkPath)
	if err != nil {
		t.Fatalf("read homework category export: %v", err)
	}
	assertContains(t, string(homeworkData), "Assignment Name:,Homework,")
	assertContains(t, string(homeworkData), `100401,"Brown, Alice",92.50`)

	unchanged := mustRun(t, env, "", "categories", "export")
	assertContains(t, unchanged, "No unexported or modified categories.")

	if _, err := dbConn.Exec(`UPDATE grades SET score = 18 WHERE assignment_id = 2 AND student_pk = 10`); err != nil {
		t.Fatalf("change homework grade: %v", err)
	}
	changed := mustRun(t, env, "y\n", "categories", "export")
	assertContains(t, changed, "[1/1] Homework")
	assertNotContains(t, changed, "Assignment Name:\tExam")
	assertContains(t, changed, "\x1b[32m95.00\x1b[0m")
	homeworkData, err = os.ReadFile(homeworkPath)
	if err != nil {
		t.Fatalf("read changed homework category export: %v", err)
	}
	assertContains(t, string(homeworkData), `100401,"Brown, Alice",95.00`)

	if _, err := dbConn.Exec(`UPDATE grades SET score = 80 WHERE assignment_id = 1 AND student_pk = 10`); err != nil {
		t.Fatalf("change exam grade: %v", err)
	}
	if _, err := dbConn.Exec(`UPDATE grades SET score = 19 WHERE assignment_id = 3 AND student_pk = 10`); err != nil {
		t.Fatalf("change second homework grade: %v", err)
	}
	partiallyConfirmed := mustRun(t, env, "n\ny\n", "categories", "export")
	assertContains(t, partiallyConfirmed, "Export not confirmed. This category still needs export.")
	assertContains(t, partiallyConfirmed, "Marked category as exported.")

	examRetry := mustRun(t, env, "y\n", "categories", "export")
	assertContains(t, examRetry, "[1/1] Exam")
	assertNotContains(t, examRetry, "Assignment Name:\tHomework")
}
