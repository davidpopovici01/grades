package cmd_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAssignmentExportUsesCurvedScoreForAverageAssignments(t *testing.T) {
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
	if _, err := dbConn.Exec(`INSERT INTO assignments(assignment_id, course_year_id, term_id, category_id, title, max_points) VALUES (1, 1, 1, 1, 'Curve Test', 100)`); err != nil {
		t.Fatalf("insert assignment: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO grades(assignment_id, student_pk, score, flags_bitmask) VALUES (1, 10, 25, 0)`); err != nil {
		t.Fatalf("insert grade: %v", err)
	}
	if _, err := dbConn.Exec(`INSERT INTO assignment_curves(assignment_id, anchor_percent, lift_percent) VALUES (1, 100, 0.5)`); err != nil {
		t.Fatalf("insert curve: %v", err)
	}

	mustRun(t, env, "", "use", "year", "2026-27")
	mustRun(t, env, "", "use", "term", "Fall 2026")
	mustRun(t, env, "", "use", "course", "1")
	mustRun(t, env, "", "use", "assignment", "Curve Test")

	exportPath := filepath.Join(env.home, "curve_test_export.csv")
	mustRun(t, env, "y\n", "assignments", "export", exportPath)

	data, err := os.ReadFile(exportPath)
	if err != nil {
		t.Fatalf("read export: %v", err)
	}
	assertContains(t, string(data), `100401,"Brown, Alice",50.00`)
}
