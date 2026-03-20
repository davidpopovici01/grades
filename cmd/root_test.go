package cmd_test

import (
	"bytes"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/davidpopovici01/grades/cmd"
	"github.com/davidpopovici01/grades/internal/db"
	"github.com/davidpopovici01/grades/internal/migrate"
)

const (
	testFlagLate    = 1 << 0
	testFlagMissing = 1 << 1
	testFlagRedo    = 1 << 3
)

func TestContextAndListCommands(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)

	mustRun(t, env, "", "use", "year", "1")
	mustRun(t, env, "", "use", "term", "1")
	mustRun(t, env, "", "use", "course", "1")
	mustRun(t, env, "", "use", "section", "1")

	dashboard := mustRun(t, env, "")
	assertContains(t, dashboard, "2026-27")
	assertContains(t, dashboard, "Fall 2026")
	assertContains(t, dashboard, "APCSA")
	assertContains(t, dashboard, "12A")

	assertContains(t, mustRun(t, env, "", "list", "years"), "2026-27")
	assertContains(t, mustRun(t, env, "", "list", "terms"), "Fall 2026")
	assertContains(t, mustRun(t, env, "", "list", "courses"), "APCSA")
	assertContains(t, mustRun(t, env, "", "list", "sections"), "12A")

	mustRun(t, env, "", "clear", "section")
	cleared := mustRun(t, env, "")
	assertContains(t, cleared, "Section:\t(none)")
}

func TestClearSectionKeepsCurrentAssignment(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "1")
	mustRun(t, env, "", "use", "course", "1")
	mustRun(t, env, "", "use", "section", "1")
	mustRun(t, env, "Quiz\n10\nExam\n", "assignments", "add")

	before := mustRun(t, env, "", "")
	assertContains(t, before, "Assignment:\tQuiz")

	mustRun(t, env, "", "clear", "section")

	after := mustRun(t, env, "", "")
	assertContains(t, after, "Section:\t(none)")
	assertContains(t, after, "Assignment:\tQuiz")
}

func TestListStudentsShowsHelpfulContextError(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	dbConn := openSeedDB(t, env)
	defer dbConn.Close()
	if _, err := dbConn.Exec(`INSERT INTO terms(term_id, name, start_date, end_date) VALUES (2, 'Fall 2027', '2027-08-15', '2027-12-20')`); err != nil {
		t.Fatalf("insert extra term: %v", err)
	}

	_, errText := runWithError(t, env, "", "list", "students")
	assertContains(t, errText, "students list needs year, term, and course context")
	assertContains(t, errText, `Run: grades use year "2026-27"`)
	assertContains(t, errText, `Run: grades use term "Fall 2026"`)
	assertContains(t, errText, `Run: grades use course 1`)
}

func TestListStudentsUsesAllSectionsWhenSectionIsUnset(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	dbConn := openSeedDB(t, env)
	defer dbConn.Close()
	if _, err := dbConn.Exec(`INSERT INTO sections(section_id, course_year_id, name) VALUES (2, 1, '12B')`); err != nil {
		t.Fatalf("insert extra section: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO students(student_pk, first_name, last_name, school_student_id) VALUES (10, 'Alice', 'Brown', '3001'), (11, 'Bob', 'Zhang', '3002')`); err != nil {
		t.Fatalf("insert students: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO section_enrollments(section_id, student_pk, term_id, start_date, status) VALUES (1, 10, 1, '2026-08-15', 'active'), (2, 11, 1, '2026-08-15', 'active')`); err != nil {
		t.Fatalf("insert enrollments: %v", err)
	}

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")

	out := mustRun(t, env, "", "list", "students")
	assertContains(t, out, "Using all sections in the current course.")
	assertContains(t, out, "Alice Brown")
	assertContains(t, out, "Bob Zhang")
}

func TestRemoveStudentUsesAllSectionsAndNameLookupWhenSectionUnset(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	dbConn := openSeedDB(t, env)
	defer dbConn.Close()
	if _, err := dbConn.Exec(`INSERT INTO sections(section_id, course_year_id, name) VALUES (2, 1, '12B')`); err != nil {
		t.Fatalf("insert extra section: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO students(student_pk, first_name, last_name, school_student_id) VALUES (10, 'Alice', 'Brown', '3001')`); err != nil {
		t.Fatalf("insert student: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO section_enrollments(section_id, student_pk, term_id, start_date, status) VALUES (1, 10, 1, '2026-08-15', 'active'), (2, 10, 1, '2026-08-15', 'active')`); err != nil {
		t.Fatalf("insert enrollments: %v", err)
	}

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")

	out := mustRun(t, env, "", "students", "remove", "ali")
	assertContains(t, out, "Removed Alice Brown from 2 section(s)")

	var count int
	if err := dbConn.QueryRow(`SELECT COUNT(*) FROM section_enrollments WHERE student_pk = 10`).Scan(&count); err != nil {
		t.Fatalf("count enrollments: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected student to be removed from all current-course sections, got %d enrollments", count)
	}
}

func TestRemoveStudentFromSelectedSectionRemovesAcrossTermsAndRosterTemplate(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	dbConn := openSeedDB(t, env)
	defer dbConn.Close()
	if _, err := dbConn.Exec(`INSERT INTO terms(term_id, name, start_date, end_date) VALUES (2, 'Spring 2027', '2027-01-10', '2027-06-01')`); err != nil {
		t.Fatalf("insert second term: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO course_year_terms(course_year_id, term_id) VALUES (1, 2)`); err != nil {
		t.Fatalf("insert second course year term: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO students(student_pk, first_name, last_name, school_student_id) VALUES (10, 'Alice', 'Brown', '3001')`); err != nil {
		t.Fatalf("insert student: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO section_enrollments(section_id, student_pk, term_id, start_date, status) VALUES (1, 10, 1, '2026-08-15', 'active'), (1, 10, 2, '2027-01-10', 'active')`); err != nil {
		t.Fatalf("insert enrollments: %v", err)
	}

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")
	mustRun(t, env, "", "use", "section", "12A")

	out := mustRun(t, env, "", "students", "remove", "Alice")
	assertContains(t, out, "Removed Alice Brown")

	var count int
	if err := dbConn.QueryRow(`SELECT COUNT(*) FROM section_enrollments WHERE student_pk = 10`).Scan(&count); err != nil {
		t.Fatalf("count enrollments after remove: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected student to be removed from selected section across all terms, got %d enrollments", count)
	}

	mustRun(t, env, "", "import", "setup-csv")
	templatePath := filepath.Join(env.home, "roster_setup.csv")
	data, err := os.ReadFile(templatePath)
	if err != nil {
		t.Fatalf("read roster template: %v", err)
	}
	content := string(data)
	assertNotContains(t, content, "2026-27,APCSA,12A,3001,Alice,Brown,")
}

func TestStudentManagementAndImport(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")
	mustRun(t, env, "", "use", "section", "12A")

	added := mustRun(t, env, "Tony\nZhang\n\n\n", "students", "add")
	assertContains(t, added, "Added student: Tony Zhang")

	listed := mustRun(t, env, "", "students", "list")
	assertContains(t, listed, "Tony Zhang")

	studentID := firstStudentID(t, env)
	shown := mustRun(t, env, "", "students", "show", studentID)
	assertContains(t, shown, "Tony Zhang")
	assertContains(t, shown, "Student ID:\t(none)")

	importPath := filepath.Join(env.home, "students.csv")
	if err := os.WriteFile(importPath, []byte("first_name,last_name\nIvy,Chen\n"), 0o644); err != nil {
		t.Fatalf("write import csv: %v", err)
	}
	imported := mustRun(t, env, "", "import", "students", importPath)
	assertContains(t, imported, "Imported 1 students")
	assertContains(t, mustRun(t, env, "", "list", "students"), "Ivy Chen")

	removed := mustRun(t, env, "", "students", "remove", "Tony", "Zhang")
	assertContains(t, removed, "Removed Tony Zhang")
}

func TestRosterImportCreatesSectionsAndSkipsExampleRow(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	dbConn := openSeedDB(t, env)
	defer dbConn.Close()
	if _, err := dbConn.Exec(`INSERT INTO sections(section_id, course_year_id, name) VALUES (2, 1, '12B')`); err != nil {
		t.Fatalf("insert extra section: %v", err)
	}

	importPath := filepath.Join(env.home, "roster.csv")
	csvData := strings.Join([]string{
		"year,course,section,student_id,first_name,last_name",
		"# example row - importer ignores rows whose first cell starts with #,APCSA,12A,3001,Alice,Brown",
		"2026-27,APCSA,12A,3001,Alice,Brown",
		"2026-27,APCSA,12B,3001,Alice,Brown",
		"2026-27,APCSA,12B,3002,Bob,Zhang",
	}, "\n")
	if err := os.WriteFile(importPath, []byte(csvData), 0o644); err != nil {
		t.Fatalf("write roster csv: %v", err)
	}

	out := mustRun(t, env, "", "import", "roster", importPath)
	assertContains(t, out, "Imported 3 roster row(s)")

	var studentCount int
	if err := dbConn.QueryRow(`SELECT COUNT(*) FROM students`).Scan(&studentCount); err != nil {
		t.Fatalf("count students: %v", err)
	}
	if studentCount != 2 {
		t.Fatalf("expected 2 students, got %d", studentCount)
	}

	var sectionCount int
	if err := dbConn.QueryRow(`SELECT COUNT(*) FROM sections`).Scan(&sectionCount); err != nil {
		t.Fatalf("count sections: %v", err)
	}
	if sectionCount != 2 {
		t.Fatalf("expected 2 sections, got %d", sectionCount)
	}

	var enrollmentCount int
	if err := dbConn.QueryRow(`SELECT COUNT(*) FROM section_enrollments`).Scan(&enrollmentCount); err != nil {
		t.Fatalf("count enrollments: %v", err)
	}
	if enrollmentCount != 3 {
		t.Fatalf("expected 3 enrollments, got %d", enrollmentCount)
	}
}

func TestRosterImportFailsWhenSetupRecordsAreMissing(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)

	importPath := filepath.Join(env.home, "roster_missing_setup.csv")
	csvData := strings.Join([]string{
		"year,course,section,student_id,first_name,last_name",
		"2026-27,APCSA,12B,3001,Alice,Brown",
	}, "\n")
	if err := os.WriteFile(importPath, []byte(csvData), 0o644); err != nil {
		t.Fatalf("write roster csv: %v", err)
	}

	_, errText := runWithError(t, env, "", "import", "roster", importPath)
	assertContains(t, errText, "section not found for course-year: 12B")
	assertContains(t, errText, "Fix the CSV and run `grades import` again")
}

func TestSetupCSVTemplateIncludesSkippedExampleRow(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	seedStudents(t, env, [][3]string{{"Alice", "Brown", "3001"}, {"Bob", "Zhang", "3002"}})

	templatePath := filepath.Join(env.home, "roster_setup.csv")
	out := mustRun(t, env, "", "import", "setup-csv", templatePath)
	assertContains(t, out, "Created roster setup CSV")

	data, err := os.ReadFile(templatePath)
	if err != nil {
		t.Fatalf("read setup csv: %v", err)
	}
	content := string(data)
	assertContains(t, content, "year,course,section,student_id,first_name,last_name,chinese_name")
	assertContains(t, content, "# example row - importer ignores rows whose first cell starts with #")
	assertContains(t, content, "2026-27,APCSA,12A,3001,Alice,Brown,")
	assertContains(t, content, "2026-27,APCSA,12A,3002,Bob,Zhang,")
}

func TestImportWizardCreatesDefaultTemplateWhenMissing(t *testing.T) {
	env := newTestEnv(t)

	out := mustRun(t, env, "\n", "import")
	assertContains(t, out, "Import roster: [C]reate/open default, [P]ick another file:")
	assertContains(t, out, "Default roster file not found:")
	assertContains(t, out, "Created roster setup CSV")

	templatePath := filepath.Join(env.home, "roster_setup.csv")
	if _, err := os.Stat(templatePath); err != nil {
		t.Fatalf("expected setup csv at %s: %v", templatePath, err)
	}
}

func TestImportWizardImportsDefaultRosterCSV(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)

	importPath := filepath.Join(env.home, "roster_setup.csv")
	csvData := strings.Join([]string{
		"year,course,section,student_id,first_name,last_name",
		"2026-27,APCSA,12A,3001,Alice,Brown",
	}, "\n")
	if err := os.WriteFile(importPath, []byte(csvData), 0o644); err != nil {
		t.Fatalf("write wizard roster csv: %v", err)
	}

	out := mustRun(t, env, "\n", "import")
	assertContains(t, out, "Import roster: [I]mport default, [C]reate/open default, [P]ick another file:")
	assertContains(t, out, "Imported 1 roster row(s)")
}

func TestImportWizardCreateDefaultOverwritesExistingTemplate(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	seedStudents(t, env, [][3]string{{"Alice", "Brown", "3001"}})

	templatePath := filepath.Join(env.home, "roster_setup.csv")
	if err := os.WriteFile(templatePath, []byte("stale,data\nold,row\n"), 0o644); err != nil {
		t.Fatalf("write stale template: %v", err)
	}

	out := mustRun(t, env, "c\n", "import")
	assertContains(t, out, "Created roster setup CSV")

	data, err := os.ReadFile(templatePath)
	if err != nil {
		t.Fatalf("read overwritten template: %v", err)
	}
	content := string(data)
	assertContains(t, content, "year,course,section,student_id,first_name,last_name,chinese_name")
	assertContains(t, content, "2026-27,APCSA,12A,3001,Alice,Brown")
	assertNotContains(t, content, "stale,data")
	assertNotContains(t, content, "old,row")
}

func TestImportWizardCreatesPickedFileWhenMissing(t *testing.T) {
	env := newTestEnv(t)

	importPath := filepath.Join(env.home, "custom_roster.csv")
	out := mustRun(t, env, "p\n"+importPath+"\n", "import")
	assertContains(t, out, "Roster CSV file path:")

	if _, err := os.Stat(importPath); err != nil {
		t.Fatalf("expected custom setup csv at %s: %v", importPath, err)
	}
}

func TestRosterImportEnrollsAcrossConfiguredTerms(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	dbConn := openSeedDB(t, env)
	defer dbConn.Close()
	if _, err := dbConn.Exec(`INSERT INTO terms(term_id, name, start_date, end_date) VALUES (2, 'Spring 2027', '2027-01-10', '2027-06-01')`); err != nil {
		t.Fatalf("insert second term: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO course_year_terms(course_year_id, term_id) VALUES (1, 2)`); err != nil {
		t.Fatalf("insert course year terms: %v", err)
	}

	importPath := filepath.Join(env.home, "roster_terms.csv")
	csvData := strings.Join([]string{
		"year,course,section,student_id,first_name,last_name",
		"2026-27,APCSA,12A,3001,Alice,Brown",
	}, "\n")
	if err := os.WriteFile(importPath, []byte(csvData), 0o644); err != nil {
		t.Fatalf("write roster csv: %v", err)
	}

	out := mustRun(t, env, "", "import", "roster", importPath)
	assertContains(t, out, "Imported 1 roster row(s)")

	var enrollmentCount int
	if err := dbConn.QueryRow(`SELECT COUNT(*) FROM section_enrollments WHERE student_pk = 1`).Scan(&enrollmentCount); err != nil {
		t.Fatalf("count student enrollments: %v", err)
	}
	if enrollmentCount != 2 {
		t.Fatalf("expected 2 enrollments across configured terms, got %d", enrollmentCount)
	}
}

func TestRosterImportAcceptsUTF8BOMHeader(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)

	importPath := filepath.Join(env.home, "bom_roster.csv")
	csvData := "\ufeffyear,course,section,student_id,first_name,last_name\n2026-27,APCSA,12A,3001,Alice,Brown\n"
	if err := os.WriteFile(importPath, []byte(csvData), 0o644); err != nil {
		t.Fatalf("write bom roster csv: %v", err)
	}

	out := mustRun(t, env, "", "import", "roster", importPath)
	assertContains(t, out, "Imported 1 roster row(s)")
}

func TestRosterImportStoresChineseName(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	dbConn := openSeedDB(t, env)
	defer dbConn.Close()

	importPath := filepath.Join(env.home, "roster_chinese.csv")
	csvData := strings.Join([]string{
		"year,course,section,student_id,first_name,last_name,chinese_name",
		"2026-27,APCSA,12A,3001,Alice,Brown,艾丽丝",
	}, "\n")
	if err := os.WriteFile(importPath, []byte(csvData), 0o644); err != nil {
		t.Fatalf("write roster csv: %v", err)
	}

	out := mustRun(t, env, "", "import", "roster", importPath)
	assertContains(t, out, "Imported 1 roster row(s)")

	var chineseName string
	if err := dbConn.QueryRow(`SELECT COALESCE(chinese_name, '') FROM students WHERE school_student_id = '3001'`).Scan(&chineseName); err != nil {
		t.Fatalf("query chinese name: %v", err)
	}
	if chineseName != "艾丽丝" {
		t.Fatalf("expected chinese name 艾丽丝, got %q", chineseName)
	}
}

func TestRosterImportAllowsMissingOptionalChineseNameColumnValues(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)

	importPath := filepath.Join(env.home, "roster_optional_chinese.csv")
	csvData := strings.Join([]string{
		"year,course,section,student_id,first_name,last_name,chinese_name",
		"2026-27,APCSA,12A,3001,Alice,Brown",
	}, "\n")
	if err := os.WriteFile(importPath, []byte(csvData), 0o644); err != nil {
		t.Fatalf("write roster csv: %v", err)
	}

	out := mustRun(t, env, "", "import", "roster", importPath)
	assertContains(t, out, "Imported 1 roster row(s)")
}

func TestAssignmentManagement(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")

	created := mustRun(t, env, "   \nMidterm   Exam \n0\n100\n \n", "assignments", "add")
	assertContains(t, created, "Title cannot be blank. Retry.")
	assertContains(t, created, "max score must be greater than 0. Retry.")
	assertContains(t, created, "Added assignment")
	assertContains(t, created, "Category: General")
	assertContains(t, created, "Switched to assignment: Midterm Exam")

	listed := mustRun(t, env, "", "assignments", "list")
	assertContains(t, listed, "Midterm Exam")

	assignmentID := firstAssignmentID(t, env)
	dashboard := mustRun(t, env, "")
	assertContains(t, dashboard, "Assignment:\tMidterm Exam")
	shown := mustRun(t, env, "", "assignments", "show", assignmentID)
	assertContains(t, shown, "Midterm Exam")
	assertContains(t, shown, "Max score:\t100")
	assertContains(t, shown, "Pass rate:\tcategory default (80.0%)")

	mustRun(t, env, "", "use", "assignment", "Midterm Exam")
	updated := mustRun(t, env, "", "assignments", "max", "120")
	assertContains(t, updated, "Updated max score to 120")
	passRate := mustRun(t, env, "", "assignments", "pass-rate", "raw")
	assertContains(t, passRate, "Updated assignment pass rate to raw")

	reshown := mustRun(t, env, "", "assignments", "show", assignmentID)
	assertContains(t, reshown, "Max score:\t120")
	assertContains(t, reshown, "Pass rate:\traw")

	deleted := mustRun(t, env, "", "assignments", "delete", assignmentID)
	assertContains(t, deleted, "Deleted assignment")
}

func TestAssignmentAddRejectsCaseInsensitiveDuplicateTitle(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")
	mustRun(t, env, "Midterm Exam\n100\nExam\n", "assignments", "add")

	_, errText := runWithError(t, env, "midterm exam\n", "assignments", "add")
	assertContains(t, errText, "assignment already exists: Midterm Exam")
}

func TestCategoryImportDoesNotCreateCaseInsensitiveDuplicateCategory(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	dbConn := openSeedDB(t, env)
	defer dbConn.Close()

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")

	importPath := filepath.Join(env.home, "categories_case.csv")
	csvData := strings.Join([]string{
		"category,weight,scheme,pass_rate",
		"homework,40,completion,80",
	}, "\n")
	if err := os.WriteFile(importPath, []byte(csvData), 0o644); err != nil {
		t.Fatalf("write category csv: %v", err)
	}

	out := mustRun(t, env, "", "categories", "import", importPath)
	assertContains(t, out, "Imported 1 category row(s)")

	var count int
	if err := dbConn.QueryRow(`SELECT COUNT(*) FROM categories WHERE lower(name) = 'homework'`).Scan(&count); err != nil {
		t.Fatalf("count homework categories: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 homework category after case-insensitive import, got %d", count)
	}
}

func TestGradesGradebookStatsAndExport(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	seedStudents(t, env, [][3]string{{"Alice", "Brown", "3001"}, {"Bob", "Zhang", "3002"}})
	seedAssignment(t, env, "Project", 50)

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")
	mustRun(t, env, "", "use", "section", "12A")
	mustRun(t, env, "", "categories", "set-weight", "Exam", "25")
	mustRun(t, env, "Midterm Exam\n100\nExam\n", "assignments", "add")
	mustRun(t, env, "", "use", "assignment", "Midterm Exam")

	entryInput := "ali\n79\nbz\n88l\nundo\nbz\nm\n\n"
	enterOut := mustRun(t, env, entryInput, "grades", "enter")
	assertContains(t, enterOut, "Matched: Alice Brown")
	assertContains(t, enterOut, "Previous entry removed for Bob Zhang.")

	gradesOut := mustRun(t, env, "", "grades", "show")
	assertContains(t, gradesOut, "Alice Brown")
	assertContains(t, gradesOut, "79 (redo)")
	assertContains(t, gradesOut, "Bob Zhang")
	assertContains(t, gradesOut, "M")

	dbConn := openSeedDB(t, env)
	defer dbConn.Close()
	var projectID int
	if err := dbConn.QueryRow(`SELECT assignment_id FROM assignments WHERE title = 'Project'`).Scan(&projectID); err != nil {
		t.Fatalf("project id: %v", err)
	}
	aliceID := studentIDByName(t, dbConn, "Alice", "Brown")
	bobID := studentIDByName(t, dbConn, "Bob", "Zhang")
	if _, err := dbConn.Exec(`INSERT INTO grades(assignment_id, student_pk, score, flags_bitmask) VALUES (?, ?, 45, 0), (?, ?, 40, 0)`, projectID, aliceID, projectID, bobID); err != nil {
		t.Fatalf("seed project grades: %v", err)
	}

	gradebook := mustRun(t, env, "", "gradebook")
	assertContains(t, gradebook, "Midterm Exam")
	assertContains(t, gradebook, "Project")

	assignmentStats := mustRun(t, env, "", "stats", "assignment")
	assertContains(t, assignmentStats, "Average: 39.5")
	assertContains(t, assignmentStats, "Highest: 79")
	assertContains(t, assignmentStats, "Lowest: 0")

	sectionStats := mustRun(t, env, "", "stats", "section")
	assertContains(t, sectionStats, "Average: 41.0")

	studentStats := mustRun(t, env, "", "stats", "student", studentIDString(aliceID))
	assertContains(t, studentStats, "Average: 62.0")

	exportPath := filepath.Join(env.home, "midterm.csv")
	exported := mustRun(t, env, "y\n", "assignments", "export", exportPath)
	assertContains(t, exported, "Assignment Name:\tMidterm Exam")
	assertContains(t, exported, "Category:\tExam")
	assertContains(t, exported, "Max Points:\t100.0")
	assertContains(t, exported, "Exported grades")
	data, err := os.ReadFile(exportPath)
	if err != nil {
		t.Fatalf("read export: %v", err)
	}
	assertContains(t, string(data), "Teacher Name:,David Popovici,")
	assertContains(t, string(data), "Assignment Name:,Midterm Exam,")
	assertContains(t, string(data), "Points Possible:,100.0,")
	assertContains(t, string(data), "Student Num,Student Name,Score")
	assertContains(t, string(data), "Brown, Alice")
}

func TestShowGradesUsesAllSectionsWhenSectionIsUnset(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	dbConn := openSeedDB(t, env)
	defer dbConn.Close()
	if _, err := dbConn.Exec(`INSERT INTO sections(section_id, course_year_id, name) VALUES (2, 1, '12B')`); err != nil {
		t.Fatalf("insert extra section: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO students(student_pk, first_name, last_name, school_student_id) VALUES (10, 'Alice', 'Brown', '3001'), (11, 'Bob', 'Zhang', '3002')`); err != nil {
		t.Fatalf("insert students: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO section_enrollments(section_id, student_pk, term_id, start_date, status) VALUES (1, 10, 1, '2026-08-15', 'active'), (2, 11, 1, '2026-08-15', 'active')`); err != nil {
		t.Fatalf("insert enrollments: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO assignments(assignment_id, course_year_id, term_id, category_id, title, max_points) VALUES (1, 1, 1, 1, 'Midterm Exam', 100)`); err != nil {
		t.Fatalf("insert assignment: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO grades(assignment_id, student_pk, score, flags_bitmask) VALUES (1, 10, 90, 0), (1, 11, 80, 0)`); err != nil {
		t.Fatalf("insert grades: %v", err)
	}

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")
	mustRun(t, env, "", "use", "assignment", "Midterm Exam")

	out := mustRun(t, env, "", "grades", "show")
	assertContains(t, out, "Using all sections in the current course.")
	assertContains(t, out, "Alice Brown")
	assertContains(t, out, "Bob Zhang")
}

func TestGradesEnterUsesAllSectionsWhenSectionIsUnset(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	dbConn := openSeedDB(t, env)
	defer dbConn.Close()
	if _, err := dbConn.Exec(`INSERT INTO sections(section_id, course_year_id, name) VALUES (2, 1, '12B')`); err != nil {
		t.Fatalf("insert extra section: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO students(student_pk, first_name, last_name, school_student_id) VALUES (10, 'Alice', 'Brown', '3001'), (11, 'Bob', 'Zhang', '3002')`); err != nil {
		t.Fatalf("insert students: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO section_enrollments(section_id, student_pk, term_id, start_date, status) VALUES (1, 10, 1, '2026-08-15', 'active'), (2, 11, 1, '2026-08-15', 'active')`); err != nil {
		t.Fatalf("insert enrollments: %v", err)
	}

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")
	mustRun(t, env, "Midterm Exam\n100\nExam\n", "assignments", "add")
	mustRun(t, env, "", "use", "assignment", "Midterm Exam")

	out := mustRun(t, env, "bz\n88\n\n", "grades", "enter")
	assertContains(t, out, "Using all sections in the current course.")
	assertContains(t, out, "Matched: Bob Zhang")

	show := mustRun(t, env, "", "grades", "show")
	assertContains(t, show, "Bob Zhang")
	assertContains(t, show, "88")
}

func TestGradebookUsesAllSectionsWhenSectionIsUnset(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	dbConn := openSeedDB(t, env)
	defer dbConn.Close()
	if _, err := dbConn.Exec(`INSERT INTO sections(section_id, course_year_id, name) VALUES (2, 1, '12B')`); err != nil {
		t.Fatalf("insert extra section: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO students(student_pk, first_name, last_name, school_student_id) VALUES (10, 'Alice', 'Brown', '3001'), (11, 'Bob', 'Zhang', '3002')`); err != nil {
		t.Fatalf("insert students: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO section_enrollments(section_id, student_pk, term_id, start_date, status) VALUES (1, 10, 1, '2026-08-15', 'active'), (2, 11, 1, '2026-08-15', 'active')`); err != nil {
		t.Fatalf("insert enrollments: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO assignments(assignment_id, course_year_id, term_id, category_id, title, max_points) VALUES (1, 1, 1, 1, 'Midterm Exam', 100)`); err != nil {
		t.Fatalf("insert assignment: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO grades(assignment_id, student_pk, score, flags_bitmask) VALUES (1, 10, 90, 0), (1, 11, 80, 0)`); err != nil {
		t.Fatalf("insert grades: %v", err)
	}

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")

	out := mustRun(t, env, "", "gradebook")
	assertContains(t, out, "Using all sections in the current course.")
	assertContains(t, out, "Alice Brown")
	assertContains(t, out, "Bob Zhang")
	assertContains(t, out, "Midterm Exam")
}

func TestGradesEnterRetriesInvalidScore(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	seedStudents(t, env, [][3]string{{"Alice", "Brown", "3001"}})

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")
	mustRun(t, env, "", "use", "section", "12A")
	mustRun(t, env, "", "categories", "set-weight", "Exam", "5")
	mustRun(t, env, "Quiz\n10\nExam\n", "assignments", "add")
	mustRun(t, env, "", "use", "assignment", "Quiz")

	out := mustRun(t, env, " ali \nabc\n 9 \n\n", "grades", "enter")
	assertContains(t, out, "invalid score: abc. Retry.")
	assertContains(t, out, "Recorded 9")

	gradesOut := mustRun(t, env, "", "grades", "show")
	assertContains(t, gradesOut, "Alice Brown")
	assertContains(t, gradesOut, "9")
}

func TestDatabaseCommands(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)

	assertContains(t, mustRun(t, env, "", "migrate", "up"), "Migrations applied.")
	assertContains(t, mustRun(t, env, "", "migrate", "down"), "Database schema dropped.")

	dbConn, err := db.Open(filepath.Join(env.home, "grades.db"))
	if err != nil {
		t.Fatalf("open db after down: %v", err)
	}
	defer dbConn.Close()
	var count int
	if err := dbConn.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='students'`).Scan(&count); err != nil {
		t.Fatalf("schema lookup after down: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected students table to be dropped, got %d", count)
	}

	assertContains(t, mustRun(t, env, "", "db", "reset"), "Database reset complete.")
	if err := dbConn.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='students'`).Scan(&count); err != nil {
		t.Fatalf("schema lookup after reset: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected students table to exist after reset, got %d", count)
	}
}

func TestDatabaseBackupCreatesUsableFile(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)

	backupPath := filepath.Join(env.home, "backup.db")
	out := mustRun(t, env, "", "db", "backup", backupPath)
	assertContains(t, out, "Database backup created:")

	info, err := os.Stat(backupPath)
	if err != nil {
		t.Fatalf("stat backup file: %v", err)
	}
	if info.Size() == 0 {
		t.Fatalf("expected backup file to be non-empty")
	}

	dbConn, err := db.Open(backupPath)
	if err != nil {
		t.Fatalf("open backup db: %v", err)
	}
	defer dbConn.Close()
	var count int
	if err := dbConn.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatalf("query backup schema: %v", err)
	}
}

func TestDatabaseBackupUsesDefaultBackupDirectory(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)

	out := mustRun(t, env, "", "db", "backup")
	assertContains(t, out, "Database backup created:")

	backupDir := filepath.Clean(filepath.Join(env.home, "..", "gradesBackups"))
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatalf("read backup dir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("expected at least one backup file in %s", backupDir)
	}
}

func TestSetupWizard(t *testing.T) {
	env := newTestEnv(t)
	input := strings.Join([]string{
		"   ",
		"Biology",
		"2",
		"1",
		"Blue",
		"2",
		" ",
		"Anna   ",
		"  Lee  ",
		"",
		"",
		"Ben",
		"Ng",
		"",
		"",
		"",
	}, "\n")
	out := mustRun(t, env, input, "setup")
	assertContains(t, out, "Course name cannot be blank. Retry.")
	assertContains(t, out, "Blue student 1 first name cannot be blank. Retry.")
	assertContains(t, out, "Created course: Biology")
	assertContains(t, out, "Using year: "+currentYearLabel())
	assertContains(t, out, "Created 2 term(s) and 1 section(s)")

	dashboard := mustRun(t, env, "")
	assertContains(t, dashboard, currentYearLabel())
	assertContains(t, dashboard, "Term 1")
	assertContains(t, dashboard, "Biology")
	assertContains(t, dashboard, "Blue")

	students := mustRun(t, env, "", "students", "list")
	assertContains(t, students, "Anna Lee")
	assertContains(t, students, "Ben Ng")

	terms := mustRun(t, env, "", "list", "terms")
	assertContains(t, terms, "Term 1")
	assertContains(t, terms, "Term 2")
}

func TestAssignmentCategoryAliasAndOverviewAndPassRedo(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	seedStudents(t, env, [][3]string{{"Alice", "Brown", ""}, {"Bob", "Zhang", ""}})

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")
	mustRun(t, env, "", "use", "section", "12A")

	added := mustRun(t, env, "Lab 1\n10\nLabwork\nalias\n1\n", "assignments", "add")
	assertContains(t, added, "Existing categories:")
	assertContains(t, added, "Category: Exam")

	weights := mustRun(t, env, "", "categories", "set-weight", "Exam", "40")
	assertContains(t, weights, "Set category weight: Exam = 40.0%")
	assertContains(t, weights, "Total weight:\t40.0%")
	categoryList := mustRun(t, env, "", "categories", "list")
	assertContains(t, categoryList, "Category")
	assertContains(t, categoryList, "Exam")
	assertContains(t, categoryList, "40.0%")
	assertContains(t, categoryList, "Pass-rate")
	assertContains(t, categoryList, "80.0%")

	mustRun(t, env, "", "use", "assignment", "Lab 1")
	enter := mustRun(t, env, "ali\np\nbz\nr\n\n", "grades", "enter")
	assertContains(t, enter, "Recorded P")
	assertContains(t, enter, "Recorded R")

	show := mustRun(t, env, "", "grades", "show")
	assertContains(t, show, "P")
	assertContains(t, show, "R (redo)")

	overview := mustRun(t, env, "", "overview")
	assertContains(t, overview, "Alice Brown")
	assertContains(t, overview, "OK")
	assertContains(t, overview, "Bob Zhang")
	assertContains(t, overview, "Redo: Lab 1")
}

func TestAssignmentAddAcceptsListedCategoryIDAtPrompt(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")
	mustRun(t, env, "", "use", "section", "12A")

	out := mustRun(t, env, "Lab 2\n10\n1\n", "assignments", "add")
	assertContains(t, out, "Existing categories:")
	assertContains(t, out, "Category: Exam")
}

func TestIndexedOutputsUseIDOrder(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	dbConn := openSeedDB(t, env)
	defer dbConn.Close()

	if _, err := dbConn.Exec(`INSERT INTO categories(category_id, name) VALUES (2, 'Homework'), (3, 'Alpha')`); err != nil {
		t.Fatalf("insert categories: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO students(student_pk, first_name, last_name, school_student_id) VALUES (10, 'Bob', 'Zhang', '3001'), (11, 'Alice', 'Brown', '3002')`); err != nil {
		t.Fatalf("insert students: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO section_enrollments(section_id, student_pk, term_id, start_date, status) VALUES (1, 10, 1, '2026-08-15', 'active'), (1, 11, 1, '2026-08-15', 'active')`); err != nil {
		t.Fatalf("insert enrollments: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO assignments(assignment_id, course_year_id, term_id, category_id, title, max_points) VALUES (1, 1, 1, 1, 'Zeta Quiz', 10), (2, 1, 1, 1, 'Alpha Quiz', 10)`); err != nil {
		t.Fatalf("insert assignments: %v", err)
	}

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")
	mustRun(t, env, "", "use", "section", "12A")

	categoryPrompt := mustRun(t, env, "Lab 2\n10\n1\n", "assignments", "add")
	examIndex := strings.Index(categoryPrompt, "1\tExam")
	homeworkIndex := strings.Index(categoryPrompt, "2\tHomework")
	alphaIndex := strings.Index(categoryPrompt, "3\tAlpha")
	if examIndex == -1 || homeworkIndex == -1 || alphaIndex == -1 {
		t.Fatalf("expected category IDs in output:\n%s", categoryPrompt)
	}
	if !(examIndex < homeworkIndex && homeworkIndex < alphaIndex) {
		t.Fatalf("expected categories in ID order:\n%s", categoryPrompt)
	}

	students := mustRun(t, env, "", "students", "list")
	bobIndex := strings.Index(students, "10\tBob Zhang")
	aliceIndex := strings.Index(students, "11\tAlice Brown")
	if bobIndex == -1 || aliceIndex == -1 {
		t.Fatalf("expected student IDs in output:\n%s", students)
	}
	if bobIndex > aliceIndex {
		t.Fatalf("expected students in ID order:\n%s", students)
	}

	assignments := mustRun(t, env, "", "assignments", "list")
	zetaIndex := strings.Index(assignments, "1\tZeta Quiz")
	alphaAssignmentIndex := strings.Index(assignments, "2\tAlpha Quiz")
	if zetaIndex == -1 || alphaAssignmentIndex == -1 {
		t.Fatalf("expected assignment IDs in output:\n%s", assignments)
	}
	if zetaIndex > alphaAssignmentIndex {
		t.Fatalf("expected assignments in ID order:\n%s", assignments)
	}
}

func TestCategoriesImportRejectsNumericOnlyCategoryName(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")

	importPath := filepath.Join(env.home, "bad_categories.csv")
	csvData := strings.Join([]string{
		"category,weight,scheme,pass_rate",
		"123,40,completion,80",
	}, "\n")
	if err := os.WriteFile(importPath, []byte(csvData), 0o644); err != nil {
		t.Fatalf("write categories csv: %v", err)
	}

	_, errText := runWithError(t, env, "", "categories", "import", importPath)
	assertContains(t, errText, "category name cannot be only numbers")
}

func TestCategoriesImport(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")

	importPath := filepath.Join(env.home, "categories.csv")
	csvData := strings.Join([]string{
		"category,weight,scheme,pass_rate",
		"Homework,40,completion,75",
		"Exam,60,average,raw",
	}, "\n")
	if err := os.WriteFile(importPath, []byte(csvData), 0o644); err != nil {
		t.Fatalf("write categories csv: %v", err)
	}

	out := mustRun(t, env, "", "categories", "import", importPath)
	assertContains(t, out, "Imported 2 category row(s)")

	listed := mustRun(t, env, "", "categories", "list")
	assertContains(t, listed, "Homework")
	assertContains(t, listed, "40.0%")
	assertContains(t, listed, "Pass-rate")
	assertContains(t, listed, "75.0%")
	assertContains(t, listed, "Exam")
	assertContains(t, listed, "60.0%")
	assertContains(t, listed, "Raw average")
	assertContains(t, listed, "raw")
	assertContains(t, listed, "Total weight:\t100.0%")
}

func TestCategoriesImportWizardCreatesDefaultTemplateWhenMissing(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")

	out := mustRun(t, env, "\n", "categories", "import")
	assertContains(t, out, "Import categories: [C]reate/open default, [P]ick another file:")
	assertContains(t, out, "Default category file not found:")
	assertContains(t, out, "Created category setup CSV")

	templatePath := filepath.Join(env.home, "categories_setup.csv")
	if _, err := os.Stat(templatePath); err != nil {
		t.Fatalf("expected category setup csv at %s: %v", templatePath, err)
	}
}

func TestCategoriesImportWizardImportsDefaultCSV(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")

	importPath := filepath.Join(env.home, "categories_setup.csv")
	csvData := strings.Join([]string{
		"category,weight,scheme,pass_rate",
		"Homework,40,completion,80",
	}, "\n")
	if err := os.WriteFile(importPath, []byte(csvData), 0o644); err != nil {
		t.Fatalf("write categories setup csv: %v", err)
	}

	out := mustRun(t, env, "\n", "categories", "import")
	assertContains(t, out, "Import categories: [I]mport default, [C]reate/open default, [P]ick another file:")
	assertContains(t, out, "Imported 1 category row(s)")
}

func TestMissingDefaultsMarkLateAndLatePersistence(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	seedStudents(t, env, [][3]string{{"Alice", "Brown", ""}, {"Bob", "Zhang", ""}})

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")
	mustRun(t, env, "", "use", "section", "12A")

	mustRun(t, env, "Homework 1\n10\nHomework\nnew\ny\n", "assignments", "add")
	mustRun(t, env, "", "use", "assignment", "Homework 1")

	initial := mustRun(t, env, "", "grades", "show")
	assertContains(t, initial, "Alice Brown")
	assertContains(t, initial, "M")
	assertContains(t, initial, "Bob Zhang")

	mustRun(t, env, "ali\np\n\n", "grades", "enter")
	late := mustRun(t, env, "", "grades", "mark-late")
	assertContains(t, late, "Marked 1 missing grade(s) as late.")

	dbConn := openSeedDB(t, env)
	defer dbConn.Close()
	bobID := studentIDByName(t, dbConn, "Bob", "Zhang")
	var bobScore sql.NullFloat64
	var bobFlags int
	if err := dbConn.QueryRow(`SELECT score, flags_bitmask FROM grades WHERE assignment_id = 1 AND student_pk = ?`, bobID).Scan(&bobScore, &bobFlags); err != nil {
		t.Fatalf("query bob late grade: %v", err)
	}
	if bobScore.Valid {
		t.Fatalf("expected Bob's late-marked grade to have no score, got %v", bobScore.Float64)
	}
	if bobFlags != testFlagLate {
		t.Fatalf("expected Bob's late-marked flags to be late only, got %d", bobFlags)
	}

	afterLate := mustRun(t, env, "", "grades", "show")
	assertContains(t, afterLate, "Bob Zhang")
	assertContains(t, afterLate, "L")

	mustRun(t, env, "bz\n8\n\n", "grades", "enter")
	finalGrades := mustRun(t, env, "", "grades", "show")
	assertContains(t, finalGrades, "Alice Brown")
	assertContains(t, finalGrades, "P")
	assertContains(t, finalGrades, "Bob Zhang")
	assertContains(t, finalGrades, "8L")
}

func TestMarkLateUndoRestoresMissing(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	seedStudents(t, env, [][3]string{{"Alice", "Brown", ""}, {"Bob", "Zhang", ""}})

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")
	mustRun(t, env, "", "use", "section", "12A")

	mustRun(t, env, "Homework 1\n10\nHomework\nnew\ny\n", "assignments", "add")
	mustRun(t, env, "", "use", "assignment", "Homework 1")
	mustRun(t, env, "ali\np\n\n", "enter")

	late := mustRun(t, env, "", "mark-late")
	assertContains(t, late, "Marked 1 missing grade(s) as late.")

	dbConn := openSeedDB(t, env)
	defer dbConn.Close()
	bobID := studentIDByName(t, dbConn, "Bob", "Zhang")

	afterLate := mustRun(t, env, "", "show")
	assertContains(t, afterLate, "Bob Zhang")
	assertContains(t, afterLate, "L")

	undo := mustRun(t, env, "", "mark-late", "-undo")
	assertContains(t, undo, "Restored 1 late grade(s) back to missing.")

	var bobScore sql.NullFloat64
	var bobFlags int
	if err := dbConn.QueryRow(`SELECT score, flags_bitmask FROM grades WHERE assignment_id = 1 AND student_pk = ?`, bobID).Scan(&bobScore, &bobFlags); err != nil {
		t.Fatalf("query bob restored grade: %v", err)
	}
	if !bobScore.Valid || bobScore.Float64 != 0 {
		t.Fatalf("expected Bob's restored missing grade score to be 0, got valid=%v value=%v", bobScore.Valid, bobScore.Float64)
	}
	if bobFlags != testFlagMissing {
		t.Fatalf("expected Bob's restored flags to be missing only, got %d", bobFlags)
	}

	afterUndo := mustRun(t, env, "", "show")
	assertContains(t, afterUndo, "Alice Brown")
	assertContains(t, afterUndo, "P")
	assertContains(t, afterUndo, "Bob Zhang")
	assertContains(t, afterUndo, "M")
	assertNotContains(t, afterUndo, "Bob Zhang  L")
}

func TestMarkLateDoesNotCountAlreadyLateMissingRows(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	dbConn := openSeedDB(t, env)
	defer dbConn.Close()
	if _, err := dbConn.Exec(`INSERT INTO students(student_pk, first_name, last_name) VALUES (10, 'Alice', 'Brown'), (11, 'Bob', 'Zhang')`); err != nil {
		t.Fatalf("insert students: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO section_enrollments(section_id, student_pk, term_id, start_date, status) VALUES (1, 10, 1, '2026-08-15', 'active'), (1, 11, 1, '2026-08-15', 'active')`); err != nil {
		t.Fatalf("insert enrollments: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO assignments(assignment_id, course_year_id, term_id, category_id, title, max_points) VALUES (1, 1, 1, 1, 'Homework 1', 10)`); err != nil {
		t.Fatalf("insert assignment: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO grades(assignment_id, student_pk, score, flags_bitmask, redo_count) VALUES (1, 10, 0, ?, 0), (1, 11, NULL, ?, 0)`,
		testFlagMissing, testFlagMissing|testFlagLate); err != nil {
		t.Fatalf("insert grades: %v", err)
	}

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")
	mustRun(t, env, "", "use", "section", "12A")
	mustRun(t, env, "", "use", "assignment", "Homework 1")

	out := mustRun(t, env, "", "mark-late")
	assertContains(t, out, "Marked 1 missing grade(s) as late.")
}

func TestClearLateAndClearRedo(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	seedStudents(t, env, [][3]string{{"Alice", "Brown", "3001"}, {"Bob", "Zhang", "3002"}})

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")
	mustRun(t, env, "", "use", "section", "12A")
	mustRun(t, env, "Homework 1\n10\nHomework\nnew\ny\n", "assignments", "add")
	mustRun(t, env, "", "use", "assignment", "Homework 1")
	mustRun(t, env, "ali\n8l\nbz\nr\n\n", "grades", "enter")

	dbConn := openSeedDB(t, env)
	defer dbConn.Close()

	clearedLate := mustRun(t, env, "", "grades", "clear-late", "ali")
	assertContains(t, clearedLate, "Cleared late")
	clearedRedo := mustRun(t, env, "", "grades", "clear-redo", "bz")
	assertContains(t, clearedRedo, "Cleared redo")

	show := mustRun(t, env, "", "grades", "show")
	assertContains(t, show, "Alice Brown")
	assertContains(t, show, "Alice Brown  8")
	assertContains(t, show, "Bob Zhang")
	assertNotContains(t, show, "R (redo)")
}

func TestClearRedoAcceptsMultiWordStudentName(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	seedStudents(t, env, [][3]string{{"Alice", "Brown", "3001"}})

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")
	mustRun(t, env, "", "use", "section", "12A")
	mustRun(t, env, "Quiz\n10\nExam\n", "assignments", "add")
	mustRun(t, env, "", "use", "assignment", "Quiz")
	mustRun(t, env, "ali\n7\n\n", "enter")

	out := mustRun(t, env, "", "clear-redo", "Alice", "Brown")
	assertContains(t, out, "Cleared redo for Alice Brown")
}

func TestCheatGradeIsLockedHiddenFromOverviewAndClearable(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	seedStudents(t, env, [][3]string{{"Noah", "Zeng", "3001"}})

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")
	mustRun(t, env, "", "use", "section", "12A")
	mustRun(t, env, "Quiz\n10\nExam\n", "assignments", "add")
	mustRun(t, env, "", "use", "assignment", "Quiz")
	mustRun(t, env, "noa\ncheat\n\n", "enter")

	show := mustRun(t, env, "", "show")
	assertContains(t, show, "Noah Zeng")
	assertContains(t, show, "\x1b[30;47m0\x1b[0m")

	gradebook := mustRun(t, env, "", "gradebook")
	assertContains(t, gradebook, "\x1b[30;47m0\x1b[0m")

	overview := mustRun(t, env, "", "overview")
	assertContains(t, overview, "Noah Zeng")
	assertContains(t, overview, "OK")
	assertNotContains(t, overview, "Redo:")
	assertNotContains(t, overview, "Missing:")
	assertNotContains(t, overview, "Late:")

	_, errText := runWithError(t, env, "noa\n9\n\n", "enter")
	assertContains(t, errText, "this grade is marked cheat and cannot be changed; use clear-cheat first")

	clearOut := mustRun(t, env, "", "clear-cheat", "Noah")
	assertContains(t, clearOut, "Cleared cheat for Noah Zeng")

	mustRun(t, env, "noa\n9\n\n", "enter")
	showAfterClear := mustRun(t, env, "", "show")
	assertContains(t, showAfterClear, "9")
	assertNotContains(t, showAfterClear, "\x1b[30;47m0\x1b[0m")
}

func TestPassCommandMarksStudentPassAndKeepsFlags(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	seedStudents(t, env, [][3]string{{"Noah", "Zeng", "3001"}})

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")
	mustRun(t, env, "", "use", "section", "12A")
	mustRun(t, env, "Quiz\n10\nExam\n", "assignments", "add")
	mustRun(t, env, "", "use", "assignment", "Quiz")
	mustRun(t, env, "noa\n7l\n\n", "enter")

	out := mustRun(t, env, "", "pass", "Noah")
	assertContains(t, out, "Recorded PASS for Noah Zeng")

	show := mustRun(t, env, "", "show")
	assertContains(t, show, "Noah Zeng")
	assertContains(t, show, "P (late)")
}

func TestRedoListAndPassRespectSelectedSection(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	dbConn := openSeedDB(t, env)
	defer dbConn.Close()
	if _, err := dbConn.Exec(`INSERT INTO sections(section_id, course_year_id, name) VALUES (2, 1, '12B')`); err != nil {
		t.Fatalf("insert extra section: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO students(student_pk, first_name, last_name, school_student_id) VALUES (10, 'Alice', 'Brown', '3001'), (11, 'Bob', 'Zhang', '3002')`); err != nil {
		t.Fatalf("insert students: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO section_enrollments(section_id, student_pk, term_id, start_date, status) VALUES (1, 10, 1, '2026-08-15', 'active'), (2, 11, 1, '2026-08-15', 'active')`); err != nil {
		t.Fatalf("insert enrollments: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO assignments(assignment_id, course_year_id, term_id, category_id, title, max_points) VALUES (1, 1, 1, 1, 'HW1', 10)`); err != nil {
		t.Fatalf("insert assignment: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO grades(assignment_id, student_pk, score, flags_bitmask) VALUES (1, 10, 7, ?), (1, 11, 7, ?)`, testFlagRedo, testFlagRedo); err != nil {
		t.Fatalf("insert grades: %v", err)
	}

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")
	mustRun(t, env, "", "use", "section", "12A")

	list := mustRun(t, env, "", "redo", "list", "Alice")
	assertContains(t, list, "Redo assignments for Alice Brown")
	assertContains(t, list, "HW1")

	_, errText := runWithError(t, env, "", "redo", "list", "Bob")
	assertContains(t, errText, `no student matched "bob"`)

	out := mustRun(t, env, "", "redo", "pass", "Alice")
	assertContains(t, out, "Recorded PASS for Alice Brown on HW1")

	mustRun(t, env, "", "use", "assignment", "HW1")
	show := mustRun(t, env, "", "show")
	assertContains(t, show, "Alice Brown")
	assertContains(t, show, "P")
}

func TestRedoCommandsUseAllSectionsWhenNoSectionSelected(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	dbConn := openSeedDB(t, env)
	defer dbConn.Close()
	if _, err := dbConn.Exec(`INSERT INTO sections(section_id, course_year_id, name) VALUES (2, 1, '12B')`); err != nil {
		t.Fatalf("insert extra section: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO students(student_pk, first_name, last_name, school_student_id) VALUES (10, 'Bob', 'Zhang', '3002')`); err != nil {
		t.Fatalf("insert student: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO section_enrollments(section_id, student_pk, term_id, start_date, status) VALUES (2, 10, 1, '2026-08-15', 'active')`); err != nil {
		t.Fatalf("insert enrollment: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO assignments(assignment_id, course_year_id, term_id, category_id, title, max_points) VALUES (1, 1, 1, 1, 'HW1', 10), (2, 1, 1, 1, 'HW2', 10)`); err != nil {
		t.Fatalf("insert assignments: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO grades(assignment_id, student_pk, score, flags_bitmask) VALUES (1, 10, 7, ?), (2, 10, 6, ?)`, testFlagRedo, testFlagRedo); err != nil {
		t.Fatalf("insert grades: %v", err)
	}

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")

	list := mustRun(t, env, "", "redo", "list", "Bob")
	assertContains(t, list, "Using all sections in the current course.")
	assertContains(t, list, "HW1")
	assertContains(t, list, "HW2")

	pass := mustRun(t, env, "2\n", "redo", "pass", "Bob")
	assertContains(t, pass, "Using all sections in the current course.")
	assertContains(t, pass, "Choose assignment:")
	assertContains(t, pass, "Recorded PASS for Bob Zhang on HW2")
}

func TestStudentsShowUsesNameLookupAndShowsDetailedBreakdown(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	seedStudents(t, env, [][3]string{{"Alice", "Brown", "3001"}})

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")
	mustRun(t, env, "", "use", "section", "12A")

	mustRun(t, env, "Homework 1\n10\nHomework\nnew\ny\n", "assignments", "add")
	mustRun(t, env, "", "categories", "set-scheme", "Homework", "completion")
	mustRun(t, env, "", "categories", "set-weight", "Homework", "40")
	mustRun(t, env, "", "use", "assignment", "Homework 1")
	mustRun(t, env, "ali\n8l\n\n", "enter")

	mustRun(t, env, "Unit Test\n100\nExam\n", "assignments", "add")
	mustRun(t, env, "", "categories", "set-scheme", "Exam", "average")
	mustRun(t, env, "", "categories", "set-weight", "Exam", "60")
	mustRun(t, env, "", "use", "assignment", "Unit Test")
	mustRun(t, env, "ali\n50\n\n", "enter")

	out := mustRun(t, env, "", "students", "show", "Ali", "Brown")
	assertContains(t, out, "Alice Brown")
	assertContains(t, out, "GPA (weighted total):\t66.0%")
	assertContains(t, out, "Category Totals")
	assertContains(t, out, "Homework")
	assertContains(t, out, "90.0%")
	assertContains(t, out, "Exam")
	assertContains(t, out, "50.0%")
	assertContains(t, out, "Assignments")
	assertContains(t, out, "Homework 1")
	assertContains(t, out, "Unit Test")
	assertContains(t, out, "Counts As")
	assertContains(t, out, "90.0%")
	assertContains(t, out, "late")
}

func TestAssignmentExportUsesAllSectionsAndPowerSchoolFormat(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	dbConn := openSeedDB(t, env)
	defer dbConn.Close()
	if _, err := dbConn.Exec(`INSERT INTO sections(section_id, course_year_id, name) VALUES (2, 1, '12B')`); err != nil {
		t.Fatalf("insert extra section: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO students(student_pk, first_name, last_name, chinese_name, powerschool_num) VALUES (10, 'Alice', 'Brown', 'Anji', '100401'), (11, 'Bob', 'Zhang', '', '100402')`); err != nil {
		t.Fatalf("insert students: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO section_enrollments(section_id, student_pk, term_id, start_date, status) VALUES (1, 10, 1, '2026-08-15', 'active'), (2, 11, 1, '2026-08-15', 'active')`); err != nil {
		t.Fatalf("insert enrollments: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO assignments(assignment_id, course_year_id, term_id, category_id, title, max_points) VALUES (1, 1, 1, 1, 'HW1', 1)`); err != nil {
		t.Fatalf("insert assignment: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO grades(assignment_id, student_pk, score, flags_bitmask) VALUES (1, 10, 1, 0), (1, 11, 0, ?)`, testFlagMissing); err != nil {
		t.Fatalf("insert grades: %v", err)
	}

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")
	mustRun(t, env, "", "use", "section", "12A")
	mustRun(t, env, "", "use", "assignment", "HW1")

	exportPath := filepath.Join(env.home, "hw1_export.csv")
	out := mustRun(t, env, "y\n", "assignments", "export", exportPath)
	assertContains(t, out, "Assignment Name:\tHW1")
	assertContains(t, out, "Category:\tExam")
	assertContains(t, out, "Max Points:\t100.0")
	assertContains(t, out, "Marked assignment as exported.")

	data, err := os.ReadFile(exportPath)
	if err != nil {
		t.Fatalf("read export: %v", err)
	}
	content := string(data)
	assertContains(t, content, "Assignment Name:,HW1,")
	assertContains(t, content, "Points Possible:,100.0,")
	assertContains(t, content, "100401,\"Brown, Anji Alice\",100.00")
	assertContains(t, content, "100402,\"Zhang, Bob\",0.00")
}

func TestAssignmentExportUsesCountedScoreForCompletionAssignments(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	dbConn := openSeedDB(t, env)
	defer dbConn.Close()
	if _, err := dbConn.Exec(`INSERT INTO categories(category_id, name) VALUES (2, 'Homework')`); err != nil {
		t.Fatalf("insert category: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO category_grading_policies(course_year_id, term_id, category_id, scheme_key, default_pass_percent) VALUES (1, 1, 2, 'completion', 80)`); err != nil {
		t.Fatalf("insert category policy: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO students(student_pk, first_name, last_name, powerschool_num) VALUES (10, 'Alice', 'Brown', '100401')`); err != nil {
		t.Fatalf("insert student: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO section_enrollments(section_id, student_pk, term_id, start_date, status) VALUES (1, 10, 1, '2026-08-15', 'active')`); err != nil {
		t.Fatalf("insert enrollment: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO assignments(assignment_id, course_year_id, term_id, category_id, title, max_points) VALUES (1, 1, 1, 2, 'HW1', 10)`); err != nil {
		t.Fatalf("insert assignment: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO grades(assignment_id, student_pk, score, flags_bitmask) VALUES (1, 10, 8, ?)`, testFlagRedo); err != nil {
		t.Fatalf("insert grade: %v", err)
	}

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")
	mustRun(t, env, "", "use", "assignment", "HW1")

	exportPath := filepath.Join(env.home, "hw1_completion_export.csv")
	mustRun(t, env, "y\n", "assignments", "export", exportPath)

	data, err := os.ReadFile(exportPath)
	if err != nil {
		t.Fatalf("read export: %v", err)
	}
	content := string(data)
	assertContains(t, content, "Points Possible:,100.0,")
	assertContains(t, content, `100401,"Brown, Alice",90.00`)
}

func TestAssignmentExportUsesZeroForBelowPassCompletionAndScorelessRows(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	dbConn := openSeedDB(t, env)
	defer dbConn.Close()
	if _, err := dbConn.Exec(`INSERT INTO categories(category_id, name) VALUES (2, 'Homework')`); err != nil {
		t.Fatalf("insert category: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO category_grading_policies(course_year_id, term_id, category_id, scheme_key, default_pass_percent) VALUES (1, 1, 2, 'completion', 80)`); err != nil {
		t.Fatalf("insert category policy: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO students(student_pk, first_name, last_name, powerschool_num) VALUES (10, 'Alice', 'Brown', '100401'), (11, 'Bob', 'Zhang', '100402'), (12, 'Carol', 'Lin', '100403')`); err != nil {
		t.Fatalf("insert students: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO section_enrollments(section_id, student_pk, term_id, start_date, status) VALUES (1, 10, 1, '2026-08-15', 'active'), (1, 11, 1, '2026-08-15', 'active'), (1, 12, 1, '2026-08-15', 'active')`); err != nil {
		t.Fatalf("insert enrollments: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO assignments(assignment_id, course_year_id, term_id, category_id, title, max_points) VALUES (1, 1, 1, 2, 'HW1', 10)`); err != nil {
		t.Fatalf("insert assignment: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO grades(assignment_id, student_pk, score, flags_bitmask) VALUES (1, 10, 7, 0), (1, 11, NULL, ?), (1, 12, NULL, ?)`, testFlagRedo, testFlagLate); err != nil {
		t.Fatalf("insert grades: %v", err)
	}

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")
	mustRun(t, env, "", "use", "assignment", "HW1")

	exportPath := filepath.Join(env.home, "hw1_zero_export.csv")
	mustRun(t, env, "y\n", "assignments", "export", exportPath)

	data, err := os.ReadFile(exportPath)
	if err != nil {
		t.Fatalf("read export: %v", err)
	}
	content := string(data)
	assertContains(t, content, `100401,"Brown, Alice",0.00`)
	assertContains(t, content, `100402,"Zhang, Bob",0.00`)
	assertContains(t, content, `100403,"Lin, Carol",0.00`)
}

func TestAssignmentExportTracksConfirmedExportAndRequiresReexportAfterChange(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	dbConn := openSeedDB(t, env)
	defer dbConn.Close()
	if _, err := dbConn.Exec(`INSERT INTO students(student_pk, first_name, last_name, powerschool_num) VALUES (10, 'Alice', 'Brown', '100401')`); err != nil {
		t.Fatalf("insert student: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO section_enrollments(section_id, student_pk, term_id, start_date, status) VALUES (1, 10, 1, '2026-08-15', 'active')`); err != nil {
		t.Fatalf("insert enrollment: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO assignments(assignment_id, course_year_id, term_id, category_id, title, max_points) VALUES (1, 1, 1, 1, 'HW1', 10)`); err != nil {
		t.Fatalf("insert assignment: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO grades(assignment_id, student_pk, score, flags_bitmask) VALUES (1, 10, 9, 0)`); err != nil {
		t.Fatalf("insert grade: %v", err)
	}

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")
	mustRun(t, env, "", "use", "assignment", "HW1")

	exportPath := filepath.Join(env.home, "tracked_export.csv")
	first := mustRun(t, env, "y\n", "assignments", "export", exportPath)
	assertContains(t, first, "This assignment has not been exported yet.")
	assertContains(t, first, "Marked assignment as exported.")

	second := mustRun(t, env, "y\n", "assignments", "export", exportPath)
	assertContains(t, second, "This assignment matches the last confirmed export.")

	if _, err := dbConn.Exec(`UPDATE grades SET score = 8 WHERE assignment_id = 1 AND student_pk = 10`); err != nil {
		t.Fatalf("update grade: %v", err)
	}

	third := mustRun(t, env, "y\n", "assignments", "export", exportPath)
	assertContains(t, third, "This assignment changed since its last export.")
	assertContains(t, third, "Marked assignment as exported.")
}

func TestExportWalksPendingAssignmentsAndOnlyMarksConfirmedOnes(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	dbConn := openSeedDB(t, env)
	defer dbConn.Close()
	if _, err := dbConn.Exec(`INSERT INTO students(student_pk, first_name, last_name, powerschool_num) VALUES (10, 'Alice', 'Brown', '100401')`); err != nil {
		t.Fatalf("insert student: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO section_enrollments(section_id, student_pk, term_id, start_date, status) VALUES (1, 10, 1, '2026-08-15', 'active')`); err != nil {
		t.Fatalf("insert enrollment: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO assignments(assignment_id, course_year_id, term_id, category_id, title, max_points) VALUES (1, 1, 1, 1, 'HW1', 10), (2, 1, 1, 1, 'HW2', 10)`); err != nil {
		t.Fatalf("insert assignments: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO grades(assignment_id, student_pk, score, flags_bitmask) VALUES (1, 10, 9, 0), (2, 10, 8, 0)`); err != nil {
		t.Fatalf("insert grades: %v", err)
	}

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")

	out := mustRun(t, env, "y\nn\n", "export")
	assertContains(t, out, "[1/2] HW1")
	assertContains(t, out, "[2/2] HW2")
	assertContains(t, out, "Marked assignment as exported.")
	assertContains(t, out, "Export not confirmed. This assignment still needs export.")

	var count int
	if err := dbConn.QueryRow(`SELECT COUNT(*) FROM assignment_exports`).Scan(&count); err != nil {
		t.Fatalf("count assignment exports: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly one confirmed assignment export, got %d", count)
	}
}

func TestAssignmentsExportAllUsesPendingFlow(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	dbConn := openSeedDB(t, env)
	defer dbConn.Close()
	if _, err := dbConn.Exec(`INSERT INTO students(student_pk, first_name, last_name, powerschool_num) VALUES (10, 'Alice', 'Brown', '100401')`); err != nil {
		t.Fatalf("insert student: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO section_enrollments(section_id, student_pk, term_id, start_date, status) VALUES (1, 10, 1, '2026-08-15', 'active')`); err != nil {
		t.Fatalf("insert enrollment: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO assignments(assignment_id, course_year_id, term_id, category_id, title, max_points) VALUES (1, 1, 1, 1, 'HW1', 10)`); err != nil {
		t.Fatalf("insert assignment: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO grades(assignment_id, student_pk, score, flags_bitmask) VALUES (1, 10, 9, 0)`); err != nil {
		t.Fatalf("insert grade: %v", err)
	}

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")

	out := mustRun(t, env, "y\n", "assignments", "export", "-all")
	assertContains(t, out, "[1/1] HW1")
	assertContains(t, out, "Marked assignment as exported.")
}

func TestImportPowerSchoolNumbersUpdatesRosterFromExportCSV(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	dbConn := openSeedDB(t, env)
	defer dbConn.Close()
	if _, err := dbConn.Exec(`INSERT INTO students(student_pk, first_name, last_name, school_student_id) VALUES (10, 'Alice', 'Brown', '3001'), (11, 'Bob', 'Zhang', '3002')`); err != nil {
		t.Fatalf("insert students: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO section_enrollments(section_id, student_pk, term_id, start_date, status) VALUES (1, 10, 1, '2026-08-15', 'active'), (1, 11, 1, '2026-08-15', 'active')`); err != nil {
		t.Fatalf("insert enrollments: %v", err)
	}

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")

	importPath := filepath.Join(env.home, "powerschool_import.csv")
	csvData := strings.Join([]string{
		"Teacher Name:,David Popovici,",
		"Class:,A.P. Computer Science A,",
		"Assignment Name:,hw1,",
		"Due Date:,2026-03-18,",
		"Points Possible:,1.0,",
		"Extra Points:,0.0,",
		"Score Type:,POINTS,",
		"Student Num,Student Name,Score",
		`100401,"Brown, Alice",`,
		`100402,"Zhang, Bob",`,
	}, "\n")
	if err := os.WriteFile(importPath, []byte(csvData), 0o644); err != nil {
		t.Fatalf("write powerschool import csv: %v", err)
	}

	out := mustRun(t, env, "", "students", "import-powerschool", importPath)
	assertContains(t, out, "Updated 2 student PowerSchool number(s)")

	var alicePS, bobPS string
	if err := dbConn.QueryRow(`SELECT COALESCE(powerschool_num, '') FROM students WHERE student_pk = 10`).Scan(&alicePS); err != nil {
		t.Fatalf("query alice powerschool num: %v", err)
	}
	if err := dbConn.QueryRow(`SELECT COALESCE(powerschool_num, '') FROM students WHERE student_pk = 11`).Scan(&bobPS); err != nil {
		t.Fatalf("query bob powerschool num: %v", err)
	}
	if alicePS != "100401" || bobPS != "100402" {
		t.Fatalf("expected powerschool numbers to be updated, got alice=%q bob=%q", alicePS, bobPS)
	}
}

func TestImportPowerSchoolNumbersCanCreateMissingStudentAndEnrollInChosenSection(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	dbConn := openSeedDB(t, env)
	defer dbConn.Close()
	if _, err := dbConn.Exec(`INSERT INTO sections(section_id, course_year_id, name) VALUES (2, 1, '12B')`); err != nil {
		t.Fatalf("insert extra section: %v", err)
	}

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")

	importPath := filepath.Join(env.home, "powerschool_create.csv")
	csvData := strings.Join([]string{
		"Teacher Name:,David Popovici,",
		"Class:,A.P. Computer Science A,",
		"Assignment Name:,hw1,",
		"Due Date:,2026-03-18,",
		"Points Possible:,1.0,",
		"Extra Points:,0.0,",
		"Score Type:,POINTS,",
		"Student Num,Student Name,Score",
		`100499,"Chen, Amy",`,
	}, "\n")
	if err := os.WriteFile(importPath, []byte(csvData), 0o644); err != nil {
		t.Fatalf("write powerschool create csv: %v", err)
	}

	out := mustRun(t, env, "y\n2\n", "students", "import-powerschool", importPath)
	assertContains(t, out, `No existing student matched "Chen, Amy". Create a new student? [y/N]:`)
	assertContains(t, out, "Sections:")
	assertContains(t, out, "Updated 1 student PowerSchool number(s)")

	var count int
	if err := dbConn.QueryRow(`SELECT COUNT(*) FROM students WHERE first_name = 'Amy' AND last_name = 'Chen' AND powerschool_num = '100499'`).Scan(&count); err != nil {
		t.Fatalf("count created student: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected one created student, got %d", count)
	}
	if err := dbConn.QueryRow(`SELECT COUNT(*) FROM section_enrollments WHERE section_id = 2 AND term_id = 1 AND student_pk = (SELECT student_pk FROM students WHERE first_name = 'Amy' AND last_name = 'Chen')`).Scan(&count); err != nil {
		t.Fatalf("count new enrollment: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected new student to be enrolled in section 2")
	}
}

func TestImportPowerSchoolNumbersUpdatesChineseNameFromPowerSchoolRow(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	dbConn := openSeedDB(t, env)
	defer dbConn.Close()
	if _, err := dbConn.Exec(`INSERT INTO students(student_pk, first_name, last_name, chinese_name, school_student_id) VALUES (10, 'Schuyler', 'Hou', 'Quantong', '3001')`); err != nil {
		t.Fatalf("insert student: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO section_enrollments(section_id, student_pk, term_id, start_date, status) VALUES (1, 10, 1, '2026-08-15', 'active')`); err != nil {
		t.Fatalf("insert enrollment: %v", err)
	}

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")

	importPath := filepath.Join(env.home, "powerschool_chinese_update.csv")
	csvData := strings.Join([]string{
		"Teacher Name:,David Popovici,",
		"Class:,A.P. Computer Science A,",
		"Assignment Name:,hw1,",
		"Due Date:,2026-03-18,",
		"Points Possible:,1.0,",
		"Extra Points:,0.0,",
		"Score Type:,POINTS,",
		"Student Num,Student Name,Score",
		`100388,"Hou, Xuantong Schuyler",`,
	}, "\n")
	if err := os.WriteFile(importPath, []byte(csvData), 0o644); err != nil {
		t.Fatalf("write powerschool csv: %v", err)
	}

	out := mustRun(t, env, "", "students", "import-powerschool", importPath)
	assertContains(t, out, "Updated 1 student PowerSchool number(s)")

	var chineseName, psNum string
	if err := dbConn.QueryRow(`SELECT COALESCE(chinese_name, ''), COALESCE(powerschool_num, '') FROM students WHERE student_pk = 10`).Scan(&chineseName, &psNum); err != nil {
		t.Fatalf("query updated student: %v", err)
	}
	if chineseName != "Xuantong" {
		t.Fatalf("expected chinese name to be updated to Xuantong, got %q", chineseName)
	}
	if psNum != "100388" {
		t.Fatalf("expected powerschool number to be updated, got %q", psNum)
	}
}

func TestInactiveStudentsAreHiddenFromNormalLists(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	dbConn := openSeedDB(t, env)
	defer dbConn.Close()
	if _, err := dbConn.Exec(`INSERT INTO students(student_pk, first_name, last_name, school_student_id, status) VALUES (10, 'Alice', 'Brown', '3001', 'active'), (11, 'Bob', 'Zhang', '3002', 'active')`); err != nil {
		t.Fatalf("insert students: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO section_enrollments(section_id, student_pk, term_id, start_date, status) VALUES (1, 10, 1, '2026-08-15', 'active'), (1, 11, 1, '2026-08-15', 'active')`); err != nil {
		t.Fatalf("insert enrollments: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO assignments(assignment_id, course_year_id, term_id, category_id, title, max_points) VALUES (1, 1, 1, 1, 'Quiz', 10)`); err != nil {
		t.Fatalf("insert assignment: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO grades(assignment_id, student_pk, score, flags_bitmask) VALUES (1, 10, 9, 0), (1, 11, 8, 0)`); err != nil {
		t.Fatalf("insert grades: %v", err)
	}

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")
	mustRun(t, env, "", "use", "section", "12A")
	mustRun(t, env, "", "use", "assignment", "Quiz")

	out := mustRun(t, env, "", "students", "deactivate", "Bob")
	assertContains(t, out, "Set Bob Zhang to inactive")

	list := mustRun(t, env, "", "students", "list")
	assertContains(t, list, "Alice Brown")
	assertNotContains(t, list, "Bob Zhang")

	gradebook := mustRun(t, env, "", "gradebook")
	assertContains(t, gradebook, "Alice Brown")
	assertNotContains(t, gradebook, "Bob Zhang")
}

func TestAssignmentExportLeavesInactiveStudentsBlank(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	dbConn := openSeedDB(t, env)
	defer dbConn.Close()
	if _, err := dbConn.Exec(`INSERT INTO students(student_pk, first_name, last_name, powerschool_num, status) VALUES (10, 'Alice', 'Brown', '100401', 'active'), (11, 'Bob', 'Zhang', '100402', 'inactive')`); err != nil {
		t.Fatalf("insert students: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO section_enrollments(section_id, student_pk, term_id, start_date, status) VALUES (1, 10, 1, '2026-08-15', 'active'), (1, 11, 1, '2026-08-15', 'active')`); err != nil {
		t.Fatalf("insert enrollments: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO assignments(assignment_id, course_year_id, term_id, category_id, title, max_points) VALUES (1, 1, 1, 1, 'HW1', 10)`); err != nil {
		t.Fatalf("insert assignment: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO grades(assignment_id, student_pk, score, flags_bitmask) VALUES (1, 10, 9, 0), (1, 11, 8, 0)`); err != nil {
		t.Fatalf("insert grades: %v", err)
	}

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")
	mustRun(t, env, "", "use", "assignment", "HW1")

	exportPath := filepath.Join(env.home, "hw1_inactive_export.csv")
	mustRun(t, env, "y\n", "assignments", "export", exportPath)

	data, err := os.ReadFile(exportPath)
	if err != nil {
		t.Fatalf("read export: %v", err)
	}
	content := string(data)
	assertContains(t, content, `100401,"Brown, Alice",90.00`)
	assertContains(t, content, `100402,"Zhang, Bob",`)
	assertNotContains(t, content, `100402,"Zhang, Bob",8.00`)
	assertNotContains(t, content, `100402,"Zhang, Bob",0.00`)
}

func TestFillPassFillsBlankAndMissingEntries(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	seedStudents(t, env, [][3]string{{"Alice", "Brown", "3001"}, {"Bob", "Zhang", "3002"}, {"Carol", "Lin", "3003"}})

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")
	mustRun(t, env, "", "use", "section", "12A")
	mustRun(t, env, "Quiz\n10\nExam\n", "assignments", "add")
	mustRun(t, env, "", "use", "assignment", "Quiz")

	dbConn := openSeedDB(t, env)
	defer dbConn.Close()
	aliceID := studentIDByName(t, dbConn, "Alice", "Brown")
	carolID := studentIDByName(t, dbConn, "Carol", "Lin")
	if _, err := dbConn.Exec(`UPDATE grades SET score = NULL, flags_bitmask = 0 WHERE assignment_id = 1 AND student_pk = ?`, aliceID); err != nil {
		t.Fatalf("clear alice grade to blank: %v", err)
	}
	if _, err := dbConn.Exec(`DELETE FROM grades WHERE assignment_id = 1 AND student_pk = ?`, carolID); err != nil {
		t.Fatalf("delete carol grade row: %v", err)
	}

	out := mustRun(t, env, "", "fill", "pass")
	assertContains(t, out, "Filled 3 blank/missing grade(s) with PASS")

	show := mustRun(t, env, "", "show")
	assertContains(t, show, "Alice Brown")
	assertContains(t, show, "Carol Lin")
	assertContains(t, show, "P")
	assertContains(t, show, "Bob Zhang")
	assertContains(t, show, "Bob Zhang")
	assertNotContains(t, show, "Bob Zhang  M")
}

func TestLowScoreSetsRedoFlag(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	seedStudents(t, env, [][3]string{{"Alice", "Brown", "3001"}})

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")
	mustRun(t, env, "", "use", "section", "12A")
	mustRun(t, env, "Quiz\n10\nExam\n", "assignments", "add")
	mustRun(t, env, "", "use", "assignment", "Quiz")
	mustRun(t, env, "ali\n7\n\n", "grades", "enter")

	show := mustRun(t, env, "", "grades", "show")
	assertContains(t, show, "Alice Brown")
	assertContains(t, show, "7 (redo)")
}

func TestFailAliasSetsRedoFlag(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	seedStudents(t, env, [][3]string{{"Alice", "Brown", "3001"}})

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")
	mustRun(t, env, "", "use", "section", "12A")
	mustRun(t, env, "Quiz\n10\nExam\n", "assignments", "add")
	mustRun(t, env, "", "use", "assignment", "Quiz")
	mustRun(t, env, "ali\nfail\n\n", "grades", "enter")

	show := mustRun(t, env, "", "grades", "show")
	assertContains(t, show, "R (redo)")
}

func TestLateOnlyAndScorePlusRedoInputs(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	seedStudents(t, env, [][3]string{{"Alice", "Brown", "3001"}, {"Bob", "Zhang", "3002"}})

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")
	mustRun(t, env, "", "use", "section", "12A")
	mustRun(t, env, "Quiz\n20\nExam\n", "assignments", "add")
	mustRun(t, env, "", "use", "assignment", "Quiz")
	mustRun(t, env, "ali\nl\nbz\n19r\n\n", "grades", "enter")

	show := mustRun(t, env, "", "grades", "show")
	assertContains(t, show, "Alice Brown")
	assertContains(t, show, "L")
	assertContains(t, show, "Bob Zhang")
	assertContains(t, show, "19 (redo)")
}

func TestEnterLastNameModePromptsInOrder(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	seedStudents(t, env, [][3]string{{"Bob", "Zhang", "3002"}, {"Alice", "Brown", "3001"}})

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")
	mustRun(t, env, "", "use", "section", "12A")
	mustRun(t, env, "Quiz\n10\nExam\n", "assignments", "add")
	mustRun(t, env, "", "use", "assignment", "Quiz")

	out := mustRun(t, env, "8\n9\n", "enter", "-lastname")
	alicePrompt := strings.Index(out, "Alice Brown:")
	bobPrompt := strings.Index(out, "Bob Zhang:")
	if alicePrompt == -1 || bobPrompt == -1 {
		t.Fatalf("expected both last-name prompts in output:\n%s", out)
	}
	if alicePrompt > bobPrompt {
		t.Fatalf("expected Alice Brown prompt before Bob Zhang prompt:\n%s", out)
	}
	assertContains(t, out, "Recorded 8 for Alice Brown")
	assertContains(t, out, "Recorded 9 for Bob Zhang")

	show := mustRun(t, env, "", "show")
	assertContains(t, show, "Alice Brown")
	assertContains(t, show, "8")
	assertContains(t, show, "Bob Zhang")
	assertContains(t, show, "9")
}

func TestEnterLastNameModeUsesChineseNameWhenLastNamesMatch(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	dbConn := openSeedDB(t, env)
	defer dbConn.Close()
	if _, err := dbConn.Exec(`INSERT INTO students(student_pk, first_name, last_name, chinese_name, school_student_id) VALUES (10, 'Amy', 'Chen', 'Li Mei', '3001'), (11, 'Ben', 'Chen', 'Wang Lei', '3002')`); err != nil {
		t.Fatalf("insert students: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO section_enrollments(section_id, student_pk, term_id, start_date, status) VALUES (1, 10, 1, '2026-08-15', 'active'), (1, 11, 1, '2026-08-15', 'active')`); err != nil {
		t.Fatalf("insert enrollments: %v", err)
	}

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")
	mustRun(t, env, "", "use", "section", "12A")
	mustRun(t, env, "Quiz\n10\nExam\n", "assignments", "add")
	mustRun(t, env, "", "use", "assignment", "Quiz")

	out := mustRun(t, env, "8\n9\n", "enter", "-lastname")
	amyPrompt := strings.Index(out, "Amy Chen:")
	benPrompt := strings.Index(out, "Ben Chen:")
	if amyPrompt == -1 || benPrompt == -1 {
		t.Fatalf("expected both Chen prompts in output:\n%s", out)
	}
	if amyPrompt > benPrompt {
		t.Fatalf("expected Amy Chen prompt before Ben Chen prompt because Li Mei sorts before Wang Lei:\n%s", out)
	}
}

func TestRedoFlagStaysAfterHigherScoreUntilCleared(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	seedStudents(t, env, [][3]string{{"Alice", "Brown", "3001"}})

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")
	mustRun(t, env, "", "use", "section", "12A")
	mustRun(t, env, "Quiz\n10\nExam\n", "assignments", "add")
	mustRun(t, env, "", "use", "assignment", "Quiz")
	mustRun(t, env, "ali\n7\n\n", "grades", "enter")
	mustRun(t, env, "ali\n9\n\n", "grades", "enter")

	show := mustRun(t, env, "", "grades", "show")
	assertContains(t, show, "9 (redo)")

	mustRun(t, env, "", "grades", "clear-redo", "ali")
	showAfterClear := mustRun(t, env, "", "grades", "show")
	assertContains(t, showAfterClear, "Alice Brown")
	assertContains(t, showAfterClear, "9")
	assertNotContains(t, showAfterClear, "9 (redo)")
}

func TestOverviewShowsRedoForLegacyLowScoreWithoutRedoFlag(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	dbConn := openSeedDB(t, env)
	defer dbConn.Close()
	if _, err := dbConn.Exec(`INSERT INTO students(student_pk, first_name, last_name, school_student_id) VALUES (10, 'Arina', 'Zeng', '3001')`); err != nil {
		t.Fatalf("insert student: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO section_enrollments(section_id, student_pk, term_id, start_date, status) VALUES (1, 10, 1, '2026-08-15', 'active')`); err != nil {
		t.Fatalf("insert enrollment: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO assignments(assignment_id, course_year_id, term_id, category_id, title, max_points) VALUES (1, 1, 1, 1, 'HW1', 100)`); err != nil {
		t.Fatalf("insert assignment: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO grades(assignment_id, student_pk, score, flags_bitmask) VALUES (1, 10, 79, 0)`); err != nil {
		t.Fatalf("insert grade: %v", err)
	}

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")

	overview := mustRun(t, env, "", "overview")
	assertContains(t, overview, "Arina Zeng")
	assertContains(t, overview, "Redo: HW1")
}

func TestOverviewShowsActiveRedoForRedoFlagBelowEightyPercent(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	seedStudents(t, env, [][3]string{{"Arina", "Zeng", "3001"}})

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")
	mustRun(t, env, "", "use", "section", "12A")
	mustRun(t, env, "HW1\n10\nHomework\nnew\ny\n", "assignments", "add")
	mustRun(t, env, "", "use", "assignment", "HW1")
	mustRun(t, env, "ari\n7\n\n", "grades", "enter")

	overview := mustRun(t, env, "", "overview")
	assertContains(t, overview, "Arina Zeng")
	assertContains(t, overview, "Redo: HW1")
}

func TestOverviewDoesNotDuplicateAssignmentsAcrossMultipleSections(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	dbConn := openSeedDB(t, env)
	defer dbConn.Close()
	if _, err := dbConn.Exec(`INSERT INTO sections(section_id, course_year_id, name) VALUES (2, 1, '12B')`); err != nil {
		t.Fatalf("insert extra section: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO students(student_pk, first_name, last_name, school_student_id) VALUES (10, 'Noah', 'Zeng', '3001')`); err != nil {
		t.Fatalf("insert student: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO section_enrollments(section_id, student_pk, term_id, start_date, status) VALUES (1, 10, 1, '2026-08-15', 'active'), (2, 10, 1, '2026-08-15', 'active')`); err != nil {
		t.Fatalf("insert enrollments: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO assignments(assignment_id, course_year_id, term_id, category_id, title, max_points) VALUES (1, 1, 1, 1, 'HW3', 10), (2, 1, 1, 1, 'HW4', 10)`); err != nil {
		t.Fatalf("insert assignments: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO grades(assignment_id, student_pk, score, flags_bitmask) VALUES (1, 10, 0, ?), (2, 10, 0, ?)`, testFlagMissing, testFlagMissing); err != nil {
		t.Fatalf("insert grades: %v", err)
	}

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")

	overview := mustRun(t, env, "", "overview")
	assertContains(t, overview, "Noah Zeng")
	assertContains(t, overview, "Missing: HW3, HW4")
	assertNotContains(t, overview, "HW3, HW3")
	assertNotContains(t, overview, "HW4, HW4")
}

func TestGradebookShowsPassingNumericGradesInGreen(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	seedStudents(t, env, [][3]string{{"Alice", "Brown", "3001"}})

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")
	mustRun(t, env, "", "use", "section", "12A")
	mustRun(t, env, "HW1\n10\nHomework\nnew\ny\n", "assignments", "add")
	mustRun(t, env, "", "use", "assignment", "HW1")
	mustRun(t, env, "ali\n8\n\n", "grades", "enter")

	gradebook := mustRun(t, env, "", "gradebook")
	assertContains(t, gradebook, "Alice Brown")
	assertContains(t, gradebook, "\x1b[32m8\x1b[0m")
}

func TestGradebookWrapsWideAssignmentSetsIntoChunks(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	seedStudents(t, env, [][3]string{{"Alice", "Brown", "3001"}})

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")
	mustRun(t, env, "", "use", "section", "12A")

	dbConn := openSeedDB(t, env)
	defer dbConn.Close()
	for i := 1; i <= 8; i++ {
		title := fmt.Sprintf("Long Homework Assignment %d", i)
		if _, err := dbConn.Exec(`INSERT INTO assignments(assignment_id, course_year_id, term_id, category_id, title, max_points) VALUES (?, 1, 1, 1, ?, 10)`, i, title); err != nil {
			t.Fatalf("insert assignment %d: %v", i, err)
		}
	}
	aliceID := studentIDByName(t, dbConn, "Alice", "Brown")
	if _, err := dbConn.Exec(`INSERT INTO grades(assignment_id, student_pk, score, flags_bitmask) VALUES (1, ?, 10, 0), (8, ?, 9, 0)`, aliceID, aliceID); err != nil {
		t.Fatalf("insert grades: %v", err)
	}

	gradebook := mustRun(t, env, "", "gradebook")
	assertContains(t, gradebook, "Assignments 1-")
	assertContains(t, gradebook, "of 8")
	if strings.Count(gradebook, "Student") < 2 {
		t.Fatalf("expected wrapped gradebook with repeated student header:\n%s", gradebook)
	}
	assertContains(t, gradebook, "Long Homework Assignment 1")
	assertContains(t, gradebook, "Long Homework Assignment 8")
}

func TestCategorySchemesCurvesAndCategoryScores(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	seedStudents(t, env, [][3]string{{"Alice", "Brown", ""}, {"Bob", "Zhang", ""}})

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")
	mustRun(t, env, "", "use", "section", "12A")

	mustRun(t, env, "Homework 1\n10\nHomework\nnew\ny\n", "assignments", "add")
	mustRun(t, env, "", "categories", "set-scheme", "Homework", "completion")
	mustRun(t, env, "", "categories", "set-weight", "Homework", "40")
	mustRun(t, env, "", "use", "assignment", "Homework 1")
	mustRun(t, env, "ali\np\nbz\nr\n\n", "grades", "enter")

	mustRun(t, env, "Unit Test\n100\nExam\n", "assignments", "add")
	mustRun(t, env, "", "categories", "set-scheme", "Exam", "average")
	mustRun(t, env, "", "categories", "set-weight", "Exam", "60")
	mustRun(t, env, "", "use", "assignment", "Unit Test")
	mustRun(t, env, "ali\n50\nbz\n100\n\n", "grades", "enter")

	curveSet := mustRun(t, env, "", "assignments", "curve", "set", "100", "50")
	assertContains(t, curveSet, "Set assignment curve: anchor 100.0, lift 50.0")
	curveShow := mustRun(t, env, "", "assignments", "curve", "show")
	assertContains(t, curveShow, "Curve anchor:\t100.0")
	assertContains(t, curveShow, "Curve lift:\t50.0")

	schemes := mustRun(t, env, "", "categories", "schemes")
	assertContains(t, schemes, "completion")
	assertContains(t, schemes, "total-points")

	scores := mustRun(t, env, "", "categories", "scores")
	assertContains(t, scores, "Homework")
	assertContains(t, scores, "Exam")
	assertContains(t, scores, "Alice Brown")
	assertContains(t, scores, "100.0%")
	assertContains(t, scores, "70.7%")
	assertContains(t, scores, "Bob Zhang")
	assertContains(t, scores, "0.0%")
	assertContains(t, scores, "100.0%")
	assertContains(t, scores, "60.0%")

	totals := mustRun(t, env, "", "categories", "totals")
	assertContains(t, totals, "Homework")
	assertContains(t, totals, "Exam")
	assertContains(t, totals, "Alice Brown")
	assertContains(t, totals, "Bob Zhang")
}

func TestAssignmentCurveTarget(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	seedStudents(t, env, [][3]string{{"Alice", "Brown", ""}, {"Bob", "Zhang", ""}})

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")
	mustRun(t, env, "", "use", "section", "12A")

	mustRun(t, env, "Curve Test\n100\nExam\n", "assignments", "add")
	mustRun(t, env, "", "use", "assignment", "Curve Test")
	mustRun(t, env, "ali\n50\nbz\n100\n\n", "grades", "enter")

	targetOut := mustRun(t, env, "", "assignments", "curve", "target", "85")
	assertContains(t, targetOut, "Set assignment curve target 85.0")

	show := mustRun(t, env, "", "assignments", "curve", "show")
	assertContains(t, show, "Curve anchor:\t100.0")
	assertNotContains(t, show, "Curve lift:\t0.0")
}

func TestBareCommandShowsHelp(t *testing.T) {
	env := newTestEnv(t)

	out := mustRun(t, env, "", "categories")
	assertContains(t, out, "set-weight")
	assertContains(t, out, "totals")
}

func TestContextAndSystemGroupsShowCanonicalHelp(t *testing.T) {
	env := newTestEnv(t)

	contextHelp := mustRun(t, env, "", "context")
	assertContains(t, contextHelp, "use")
	assertContains(t, contextHelp, "clear")
	assertContains(t, contextHelp, "list")

	systemHelp := mustRun(t, env, "", "system")
	assertContains(t, systemHelp, "db")
	assertContains(t, systemHelp, "migrate")
	assertContains(t, systemHelp, "repair")
}

func TestDashboardShowsHelpfulNextStepHints(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)

	dashboard := mustRun(t, env, "")
	assertContains(t, dashboard, "grades setup to add a new course")
	assertContains(t, dashboard, "grades use assignment <name> to switch to an assignment")

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")
	mustRun(t, env, "", "use", "section", "12A")
	mustRun(t, env, "Quiz\n10\nExam\n", "assignments", "add")
	mustRun(t, env, "", "use", "assignment", "Quiz")

	dashboardWithAssignment := mustRun(t, env, "")
	assertContains(t, dashboardWithAssignment, "grades enter to enter grades")
	assertContains(t, dashboardWithAssignment, "grades show to review the current assignment")
}

func TestPublishCommandWritesStudentPortalSnapshots(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	seedStudents(t, env, [][3]string{{"Alice", "Brown", "3001"}, {"Bob", "Zhang", "3002"}})

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")
	mustRun(t, env, "", "categories", "set-scheme", "Exam", "average")
	mustRun(t, env, "", "categories", "set-weight", "Exam", "100")
	mustRun(t, env, "Midterm Exam\n100\nExam\n", "assignments", "add")
	mustRun(t, env, "", "use", "assignment", "Midterm Exam")
	mustRun(t, env, "ali\n88\nbz\n91\n\n", "grades", "enter")
	mustRun(t, env, "", "web", "accounts", "init", "TempPass123")

	out := mustRun(t, env, "", "publish")
	assertContains(t, out, "Published student portal")
	assertContains(t, out, "Published 2 student snapshot(s)")

	indexPath := filepath.Join(env.home, "..", "gradesPublished", "index.json")
	studentPath := filepath.Join(env.home, "..", "gradesPublished", "students", "1.json")
	indexData, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read published index: %v", err)
	}
	studentData, err := os.ReadFile(studentPath)
	if err != nil {
		t.Fatalf("read published student snapshot: %v", err)
	}
	assertContains(t, string(indexData), `"studentCount": 2`)
	assertContains(t, string(studentData), `"username": "3001"`)
	assertContains(t, string(studentData), `"weightedTotalLabel": "88.0%"`)
}

func TestAssignmentExportAlsoPublishesStudentPortal(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	seedStudents(t, env, [][3]string{{"Alice", "Brown", "3001"}})

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")
	mustRun(t, env, "", "categories", "set-scheme", "Exam", "average")
	mustRun(t, env, "", "categories", "set-weight", "Exam", "100")
	mustRun(t, env, "Midterm Exam\n100\nExam\n", "assignments", "add")
	mustRun(t, env, "", "use", "assignment", "Midterm Exam")
	mustRun(t, env, "ali\n92\n\n", "grades", "enter")
	mustRun(t, env, "", "web", "accounts", "init", "TempPass123")

	exportPath := filepath.Join(env.home, "midterm.csv")
	out := mustRun(t, env, "y\n", "assignments", "export", exportPath)
	assertContains(t, out, "Exported grades")
	assertContains(t, out, "Published student portal")

	studentPath := filepath.Join(env.home, "..", "gradesPublished", "students", "1.json")
	data, err := os.ReadFile(studentPath)
	if err != nil {
		t.Fatalf("read published student snapshot after export: %v", err)
	}
	assertContains(t, string(data), `"weightedTotalLabel": "92.0%"`)
}

func TestWebAccountsResetAcceptsMultiWordStudentReference(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	seedStudents(t, env, [][3]string{{"Alice", "Brown", "3001"}})

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")
	mustRun(t, env, "", "web", "accounts", "init", "TempPass123")

	out := mustRun(t, env, "", "web", "accounts", "reset", "Alice", "Brown", "--password", "FreshPass456")
	assertContains(t, out, "Reset portal password for Alice Brown")
	assertContains(t, out, "Temporary password:\tFreshPass456")
}

func TestRepairAuditAndApplyNormalizeLegacyGradeRows(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	dbConn := openSeedDB(t, env)
	defer dbConn.Close()

	if _, err := dbConn.Exec(`INSERT INTO categories(category_id, name) VALUES (2, 'Homework')`); err != nil {
		t.Fatalf("insert category: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO category_grading_policies(course_year_id, term_id, category_id, scheme_key, default_pass_percent) VALUES (1, 1, 2, 'completion', 80)`); err != nil {
		t.Fatalf("insert category policy: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO students(student_pk, first_name, last_name) VALUES (10, 'Alice', 'Brown'), (11, 'Bob', 'Zhang'), (12, 'Carol', 'Lin')`); err != nil {
		t.Fatalf("insert students: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO assignments(assignment_id, course_year_id, term_id, category_id, title, max_points) VALUES (1, 1, 1, 1, 'Exam 1', 100), (2, 1, 1, 2, 'HW 1', 10), (3, 1, 1, 2, 'HW 2', 10)`); err != nil {
		t.Fatalf("insert assignments: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO grades(assignment_id, student_pk, score, flags_bitmask, redo_count) VALUES (1, 10, 0, ?, 0), (2, 11, 7, 0, 0), (3, 12, 0, ?, 1)`,
		testFlagMissing|testFlagLate, testFlagRedo); err != nil {
		t.Fatalf("insert grades: %v", err)
	}

	audit := mustRun(t, env, "", "repair", "audit")
	assertContains(t, audit, "Repair audit")
	assertContains(t, audit, "missing+late -> late only: 1")
	assertContains(t, audit, "low scores -> add redo: 1")
	assertContains(t, audit, "redo zero-score -> scoreless redo: 1")
	assertContains(t, audit, "total repairs: 3")

	apply := mustRun(t, env, "", "repair", "apply")
	assertContains(t, apply, "Applied repairs")
	assertContains(t, apply, "total repairs: 3")

	var aliceScore sql.NullFloat64
	var aliceFlags int
	if err := dbConn.QueryRow(`SELECT score, flags_bitmask FROM grades WHERE assignment_id = 1 AND student_pk = 10`).Scan(&aliceScore, &aliceFlags); err != nil {
		t.Fatalf("query alice repaired grade: %v", err)
	}
	if aliceScore.Valid {
		t.Fatalf("expected alice repaired score to be null")
	}
	if aliceFlags != testFlagLate {
		t.Fatalf("expected alice flags to be late only, got %d", aliceFlags)
	}

	var bobFlags int
	if err := dbConn.QueryRow(`SELECT flags_bitmask FROM grades WHERE assignment_id = 2 AND student_pk = 11`).Scan(&bobFlags); err != nil {
		t.Fatalf("query bob repaired flags: %v", err)
	}
	if bobFlags != testFlagRedo {
		t.Fatalf("expected bob flags to be redo only, got %d", bobFlags)
	}

	var carolScore sql.NullFloat64
	if err := dbConn.QueryRow(`SELECT score FROM grades WHERE assignment_id = 3 AND student_pk = 12`).Scan(&carolScore); err != nil {
		t.Fatalf("query carol repaired score: %v", err)
	}
	if carolScore.Valid {
		t.Fatalf("expected carol repaired score to be null")
	}
}

func TestTopLevelGradeCommandsWork(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)
	seedStudents(t, env, [][3]string{{"Alice", "Brown", "3001"}})

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")
	mustRun(t, env, "", "use", "section", "12A")
	mustRun(t, env, "Quiz\n10\nExam\n", "assignments", "add")
	mustRun(t, env, "", "use", "assignment", "Quiz")

	enter := mustRun(t, env, "ali\n9\n\n", "enter")
	assertContains(t, enter, "Matched: Alice Brown")

	show := mustRun(t, env, "", "show")
	assertContains(t, show, "Alice Brown")
	assertContains(t, show, "9")
}

type testEnv struct {
	home string
}

func newTestEnv(t *testing.T) testEnv {
	t.Helper()
	home := filepath.Join(t.TempDir(), ".grades")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("mkdir test home: %v", err)
	}
	t.Setenv("GRADES_HOME", home)
	t.Setenv("GRADES_DB_PATH", filepath.Join(home, "grades.db"))
	return testEnv{home: home}
}

func seedBaseData(t *testing.T, env testEnv) {
	t.Helper()
	dbConn := openSeedDB(t, env)
	defer dbConn.Close()
	statements := []string{
		`INSERT INTO terms(term_id, name, start_date, end_date) VALUES (1, 'Fall 2026', '2026-08-15', '2026-12-20')`,
		`INSERT INTO courses(course_id, name) VALUES (1, 'APCSA')`,
		`INSERT INTO course_years(course_year_id, course_id, name) VALUES (1, 1, 'APCSA 2026-27')`,
		`INSERT INTO course_year_terms(course_year_id, term_id) VALUES (1, 1)`,
		`INSERT INTO sections(section_id, course_year_id, name) VALUES (1, 1, '12A')`,
		`INSERT INTO categories(category_id, name) VALUES (1, 'Exam')`,
	}
	for _, stmt := range statements {
		if _, err := dbConn.Exec(stmt); err != nil {
			t.Fatalf("seed base data: %v", err)
		}
	}
}

func seedStudents(t *testing.T, env testEnv, students [][3]string) {
	t.Helper()
	dbConn := openSeedDB(t, env)
	defer dbConn.Close()
	for _, student := range students {
		var res sql.Result
		var err error
		if student[2] == "" {
			res, err = dbConn.Exec(`INSERT INTO students(first_name, last_name) VALUES (?, ?)`, student[0], student[1])
		} else {
			res, err = dbConn.Exec(`INSERT INTO students(first_name, last_name, school_student_id) VALUES (?, ?, ?)`, student[0], student[1], student[2])
		}
		if err != nil {
			t.Fatalf("seed student: %v", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			t.Fatalf("seed student id: %v", err)
		}
		if _, err := dbConn.Exec(`INSERT INTO section_enrollments(section_id, student_pk, term_id, start_date, status) VALUES (1, ?, 1, '2026-08-15', 'active')`, id); err != nil {
			t.Fatalf("seed enrollment: %v", err)
		}
	}
}

func seedAssignment(t *testing.T, env testEnv, title string, maxPoints int) {
	t.Helper()
	dbConn := openSeedDB(t, env)
	defer dbConn.Close()
	if _, err := dbConn.Exec(`INSERT INTO assignments(course_year_id, term_id, category_id, title, max_points) VALUES (1, 1, 1, ?, ?)`, title, maxPoints); err != nil {
		t.Fatalf("seed assignment %s: %v", title, err)
	}
}

func openSeedDB(t *testing.T, env testEnv) *sql.DB {
	t.Helper()
	dbConn, err := db.Open(filepath.Join(env.home, "grades.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := migrate.Up(dbConn); err != nil {
		t.Fatalf("migrate up: %v", err)
	}
	return dbConn
}

func mustRun(t *testing.T, env testEnv, stdin string, args ...string) string {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	root := cmd.NewRootCmd(strings.NewReader(stdin), &stdout, &stderr)
	root.SetArgs(args)
	err := root.Execute()
	if err != nil {
		t.Fatalf("run %v: %v\nstdout:\n%s\nstderr:\n%s", args, err, stdout.String(), stderr.String())
	}
	return stdout.String()
}

func runWithError(t *testing.T, env testEnv, stdin string, args ...string) (string, string) {
	t.Helper()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	root := cmd.NewRootCmd(strings.NewReader(stdin), &stdout, &stderr)
	root.SetArgs(args)
	err := root.Execute()
	if err == nil {
		t.Fatalf("expected %v to fail", args)
	}
	return stdout.String(), err.Error()
}

func assertContains(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("expected output to contain %q\noutput:\n%s", want, got)
	}
}

func assertNotContains(t *testing.T, got, want string) {
	t.Helper()
	if strings.Contains(got, want) {
		t.Fatalf("expected output not to contain %q\noutput:\n%s", want, got)
	}
}

func firstStudentID(t *testing.T, env testEnv) string {
	t.Helper()
	dbConn := openSeedDB(t, env)
	defer dbConn.Close()
	var id int
	if err := dbConn.QueryRow(`SELECT student_pk FROM students ORDER BY student_pk LIMIT 1`).Scan(&id); err != nil {
		t.Fatalf("first student id: %v", err)
	}
	return studentIDString(id)
}

func studentIDByName(t *testing.T, dbConn *sql.DB, first, last string) int {
	t.Helper()
	var id int
	if err := dbConn.QueryRow(`SELECT student_pk FROM students WHERE first_name = ? AND last_name = ?`, first, last).Scan(&id); err != nil {
		t.Fatalf("student id by name: %v", err)
	}
	return id
}

func firstAssignmentID(t *testing.T, env testEnv) string {
	t.Helper()
	dbConn := openSeedDB(t, env)
	defer dbConn.Close()
	var id int
	if err := dbConn.QueryRow(`SELECT assignment_id FROM assignments WHERE title = 'Midterm Exam'`).Scan(&id); err != nil {
		t.Fatalf("first assignment id: %v", err)
	}
	return studentIDString(id)
}

func studentIDString(id int) string {
	return strconv.Itoa(id)
}

func currentYearLabel() string {
	currentYear := time.Now().Year()
	return fmt.Sprintf("%d-%02d", currentYear, (currentYear+1)%100)
}
