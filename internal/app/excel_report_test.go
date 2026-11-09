package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestExcelReportPythonPreflight(t *testing.T) {
	pythonBin, err := findPython()
	if err != nil {
		t.Skip(err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(pythonBin, "-m", "unittest", "internal.excelreport.excel_report_test")
	command.Dir = filepath.Clean(filepath.Join(wd, "..", ".."))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("Python preflight tests: %v\n%s", err, output)
	}
}

func TestExcelReportPreflightFailureSkipsCScorePromptsAndBackup(t *testing.T) {
	a, stdout, workbook, printable := newExcelReportTestApp(t, "")
	defer a.Close()
	if _, err := findPython(); err != nil {
		t.Skip(err)
	}

	originalRunner := runExcelReportScript
	t.Cleanup(func() { runExcelReportScript = originalRunner })
	calls := 0
	runExcelReportScript = func(_, _ string, args []string, _, _ io.Writer) error {
		calls++
		if len(args) == 0 || args[0] != "--preflight" {
			t.Fatalf("expected preflight call, got %v", args)
		}
		return errors.New("invalid workbook")
	}

	err := a.ExcelReport(ExcelReportOptions{Workbook: workbook, Printable: printable})
	if err == nil || !strings.Contains(err.Error(), "excel report preflight failed") {
		t.Fatalf("expected preflight error, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected only preflight call, got %d", calls)
	}
	if strings.Contains(stdout.String(), "C Score#1") {
		t.Fatalf("C-score prompt appeared before successful preflight:\n%s", stdout.String())
	}
	assertNoExcelReportBackup(t, workbook)
}

func TestExcelReportWritesCScoresAfterSuccessfulPreflight(t *testing.T) {
	a, _, workbook, printable := newExcelReportTestApp(t, "4\n8\n")
	defer a.Close()
	if _, err := findPython(); err != nil {
		t.Skip(err)
	}

	originalRunner := runExcelReportScript
	t.Cleanup(func() { runExcelReportScript = originalRunner })
	var argsLog [][]string
	var payloads []excelReportPayload
	runExcelReportScript = func(_, script string, args []string, _, _ io.Writer) error {
		scriptData, err := os.ReadFile(script)
		if err != nil {
			t.Fatalf("read embedded script: %v", err)
		}
		if !strings.Contains(string(scriptData), "def preflight_report(") {
			t.Fatal("temporary script does not contain the embedded report implementation")
		}
		argsLog = append(argsLog, append([]string(nil), args...))
		data, err := os.ReadFile(args[len(args)-1])
		if err != nil {
			t.Fatalf("read payload: %v", err)
		}
		var payload excelReportPayload
		if err := json.Unmarshal(data, &payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		payloads = append(payloads, payload)
		return nil
	}

	if err := a.ExcelReport(ExcelReportOptions{Workbook: workbook, Printable: printable}); err != nil {
		t.Fatalf("excel report: %v", err)
	}
	if len(argsLog) != 2 {
		t.Fatalf("expected preflight and write calls, got %v", argsLog)
	}
	if argsLog[0][0] != "--preflight" || argsLog[1][0] == "--preflight" {
		t.Fatalf("unexpected script call order: %v", argsLog)
	}
	if len(payloads) != 2 || len(payloads[0].Rows) != 1 || len(payloads[1].Rows) != 1 {
		t.Fatalf("unexpected payloads: %+v", payloads)
	}
	if payloads[0].Rows[0].CScore1 != "" || payloads[0].Rows[0].CScore2 != "" {
		t.Fatalf("preflight payload unexpectedly contains C scores: %+v", payloads[0].Rows[0])
	}
	if payloads[1].Rows[0].CScore1 != "4" || payloads[1].Rows[0].CScore2 != "8" {
		t.Fatalf("write payload missing C scores: %+v", payloads[1].Rows[0])
	}
	backups, err := filepath.Glob(workbook + ".*.bak")
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 {
		t.Fatalf("expected one workbook backup, got %v", backups)
	}
}

func newExcelReportTestApp(t *testing.T, input string) (*App, *bytes.Buffer, string, string) {
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

	exec := func(query string) {
		t.Helper()
		if _, err := a.db.Exec(query); err != nil {
			t.Fatalf("exec %q: %v", query, err)
		}
	}
	exec(`INSERT INTO terms(term_id, name, start_date, end_date) VALUES (1, 'Fall 2026', '2026-08-15', '2026-12-20')`)
	exec(`INSERT INTO courses(course_id, name) VALUES (1, 'APCSA')`)
	exec(`INSERT INTO course_years(course_year_id, course_id, name) VALUES (1, 1, 'APCSA 2026-27')`)
	exec(`INSERT INTO category_schemes(scheme_id, name) VALUES (1, 'Standard')`)
	exec(`INSERT INTO course_year_terms(course_year_id, term_id, scheme_id) VALUES (1, 1, 1)`)
	exec(`INSERT INTO sections(section_id, course_year_id, name) VALUES (1, 1, '12A')`)
	exec(`INSERT INTO categories(category_id, name) VALUES (1, 'Midterm')`)
	exec(`INSERT INTO category_scheme_weights(scheme_id, category_id, weight_percent) VALUES (1, 1, 100)`)
	exec(`INSERT INTO students(student_pk, first_name, last_name) VALUES (10, 'Alice', 'Brown')`)
	exec(`INSERT INTO section_enrollments(section_id, student_pk, term_id, start_date, status) VALUES (1, 10, 1, '2026-08-15', 'active')`)
	exec(`INSERT INTO assignments(assignment_id, course_year_id, term_id, category_id, title, max_points) VALUES (1, 1, 1, 1, 'Midterm Exam', 100)`)
	exec(`INSERT INTO grades(assignment_id, student_pk, score, flags_bitmask) VALUES (1, 10, 90, 0)`)
	a.setContext("context.term_id", 1)
	a.setContext("context.course_year_id", 1)
	a.setContext("context.section_id", 1)

	workbook := filepath.Join(home, "report.xls")
	if err := os.WriteFile(workbook, []byte("test workbook"), 0o600); err != nil {
		t.Fatal(err)
	}
	return a, &stdout, workbook, filepath.Join(home, "printable.xlsx")
}

func assertNoExcelReportBackup(t *testing.T, workbook string) {
	t.Helper()
	backups, err := filepath.Glob(workbook + ".*.bak")
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 0 {
		t.Fatalf("preflight failure created backups: %v", backups)
	}
}
