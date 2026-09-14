package portalserver

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParsePassFail(t *testing.T) {
	tests := []struct {
		name       string
		output     string
		wantPassed int
		wantFailed int
	}{
		{"mixed", "PASS: adds numbers\nFAIL: handles empty\n", 1, 1},
		{"leading whitespace", "  PASS: indented\n\tFAIL: tabbed\n", 1, 1},
		{"lowercase ignored", "pass: lower\nfail: lower\n", 0, 0},
		{"no markers", "hello world\n", 0, 0},
		{"no space after colon", "PASS:a\nPASS: b\n", 2, 0},
		{"not at line start", "prefix PASS: no\n", 0, 0},
		{"empty", "", 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			passed, failed := parsePassFail(tt.output)
			if passed != tt.wantPassed || failed != tt.wantFailed {
				t.Fatalf("parsePassFail(%q) = %d/%d, want %d/%d",
					tt.output, passed, failed, tt.wantPassed, tt.wantFailed)
			}
		})
	}
}

// stageRun builds an assignment with one test and one submission on disk and
// in the store, returning the queued run id.
func stageRun(t *testing.T, server *Server, language, studentFile, harnessName, harness, studentCode string) int64 {
	t.Helper()
	asgID, err := server.store.CreateSubAssignment(&SubAssignment{
		CourseYearID: 1, TermID: 1, Title: "HW", Language: language,
		ExpectedFilenames: []string{studentFile}, MaxFileBytes: 1024,
		MaxTotalBytes: 4096, LateCapPercent: 90, IsOpen: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	testID, err := server.store.CreateSubTest(&SubTest{
		AssignmentID: asgID, Name: "harness", Visibility: "public", StoredName: harnessName,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(testsDir(server.config.SubmissionsDir, asgID), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(testsDir(server.config.SubmissionsDir, asgID), harnessName),
		[]byte(harness), 0644); err != nil {
		t.Fatal(err)
	}
	sub := &SubSubmission{AssignmentID: asgID, StudentPK: 1, SubmittedAt: "2026-09-07T00:00:00Z", CapPercent: 100}
	subID, err := server.store.CreateSubSubmission(sub, []SubFile{{Filename: studentFile, ByteSize: int64(len(studentCode))}})
	if err != nil {
		t.Fatal(err)
	}
	dir := submissionDir(server.config.SubmissionsDir, asgID, 1, sub.Attempt)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, studentFile), []byte(studentCode), 0644); err != nil {
		t.Fatal(err)
	}
	runID, err := server.store.CreateSubTestRun(&SubTestRun{
		SubmissionID: subID, TestID: testID, Visibility: "public", TriggeredBy: "admin",
	})
	if err != nil {
		t.Fatal(err)
	}
	return runID
}

// stagePythonRun builds a python assignment with one test and one submission
// on disk and in the store, returning the queued run id.
func stagePythonRun(t *testing.T, server *Server, harness, studentCode string) int64 {
	t.Helper()
	return stageRun(t, server, "python", "main.py", "harness.py", harness, studentCode)
}

func TestExecuteTestRunPython(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}
	server, _ := newTestServerWithSubmissions(t)

	runID := stagePythonRun(t, server,
		"content = open('main.py').read()\n"+
			"if 'hello' in content:\n    print('PASS: has hello')\n"+
			"else:\n    print('FAIL: missing hello')\n",
		"print('hello')\n")
	server.grader.executeTestRun(runID)

	run, err := server.store.GetSubTestRun(runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "done" || run.Passed != 1 || run.Failed != 0 {
		t.Fatalf("run = %+v, want done 1/0", run)
	}
	if run.StartedAt == nil || run.FinishedAt == nil {
		t.Fatalf("timestamps missing: %+v", run)
	}
	// The workspace is deleted after the run.
	leftovers, err := filepath.Glob(filepath.Join(server.config.SubmissionsDir, "runs", "run-*"))
	if err != nil || len(leftovers) > 0 {
		t.Fatalf("workspace should be deleted, leftovers = %v, err = %v", leftovers, err)
	}
}

func TestExecuteTestRunPythonError(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}
	server, _ := newTestServerWithSubmissions(t)

	// A crash with no PASS/FAIL lines marks the run as an error.
	runID := stagePythonRun(t, server, "import sys; sys.exit(3)\n", "print('hello')\n")
	server.grader.executeTestRun(runID)

	run, err := server.store.GetSubTestRun(runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "error" || run.Passed != 0 || run.Failed != 0 {
		t.Fatalf("run = %+v, want error 0/0", run)
	}
}

// TestExecuteTestRunJava compiles and runs an interactive (Scanner) student
// program through a harness that redirects System.in/System.out, under the
// sandbox limits. Regression test: the JVM must get enough address space and
// threads to start.
func TestExecuteTestRunJava(t *testing.T) {
	if _, err := exec.LookPath("javac"); err != nil {
		t.Skip("javac not available")
	}
	if _, err := exec.LookPath("java"); err != nil {
		t.Skip("java not available")
	}
	server, _ := newTestServerWithSubmissions(t)

	harness := `import java.io.*;
public class WelcomeTest {
    public static void main(String[] args) {
        check("greets Alice", "Alice\n", "Alice");
        check("greets Bob", "Bob\n", "Bob");
    }
    static void check(String label, String input, String expected) {
        InputStream origIn = System.in;
        PrintStream origOut = System.out;
        try {
            System.setIn(new ByteArrayInputStream(input.getBytes()));
            ByteArrayOutputStream out = new ByteArrayOutputStream();
            System.setOut(new PrintStream(out));
            Welcome.main(new String[0]);
            boolean pass = out.toString().contains(expected);
            System.setOut(origOut);
            System.out.println((pass ? "PASS: " : "FAIL: ") + label);
        } catch (Exception e) {
            System.setOut(origOut);
            System.out.println("FAIL: " + label + " - " + e.getMessage());
        } finally {
            System.setIn(origIn);
            System.setOut(origOut);
        }
    }
}
`
	student := `import java.util.Scanner;
public class Welcome {
    public static void main(String[] args) {
        System.out.println("Hello! Could you tell me your name?");
        Scanner keyboard = new Scanner(System.in);
        System.out.println("Hi " + keyboard.next() + ", welcome to AP CSA!");
    }
}
`
	runID := stageRun(t, server, "java", "Welcome.java", "WelcomeTest.java", harness, student)
	server.grader.executeTestRun(runID)

	run, err := server.store.GetSubTestRun(runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "done" || run.Passed != 2 || run.Failed != 0 {
		t.Fatalf("run = %+v, want done 2/0", run)
	}
}

func TestExecuteTestRunUnavailable(t *testing.T) {
	server, _ := newTestServerWithSubmissions(t)

	// java assignments need javac; point the test at a guaranteed-missing
	// tool by using a java assignment only when javac is absent.
	if _, err := exec.LookPath("javac"); err == nil {
		t.Skip("javac is available")
	}
	asgID, err := server.store.CreateSubAssignment(&SubAssignment{
		CourseYearID: 1, TermID: 1, Title: "HW", Language: "java",
		ExpectedFilenames: []string{"Main.java"}, MaxFileBytes: 1024,
		MaxTotalBytes: 4096, LateCapPercent: 90, IsOpen: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	testID, err := server.store.CreateSubTest(&SubTest{
		AssignmentID: asgID, Name: "harness", Visibility: "public", StoredName: "Harness.java",
	})
	if err != nil {
		t.Fatal(err)
	}
	sub := &SubSubmission{AssignmentID: asgID, StudentPK: 1, SubmittedAt: "2026-09-07T00:00:00Z", CapPercent: 100}
	subID, err := server.store.CreateSubSubmission(sub, []SubFile{{Filename: "Main.java", ByteSize: 5}})
	if err != nil {
		t.Fatal(err)
	}
	// Stage the harness and the student file so the run reaches the binary check.
	if err := os.MkdirAll(testsDir(server.config.SubmissionsDir, asgID), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(testsDir(server.config.SubmissionsDir, asgID), "Harness.java"),
		[]byte("class Harness {}"), 0644); err != nil {
		t.Fatal(err)
	}
	dir := submissionDir(server.config.SubmissionsDir, asgID, 1, sub.Attempt)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Main.java"), []byte("class Main {}"), 0644); err != nil {
		t.Fatal(err)
	}
	runID, err := server.store.CreateSubTestRun(&SubTestRun{
		SubmissionID: subID, TestID: testID, Visibility: "public", TriggeredBy: "admin",
	})
	if err != nil {
		t.Fatal(err)
	}
	server.grader.executeTestRun(runID)

	run, err := server.store.GetSubTestRun(runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != "unavailable" || !strings.Contains(run.Output, "javac") {
		t.Fatalf("run = %+v, want unavailable mentioning javac", run)
	}
}

// newTestServerWithSubmissions creates a server with a temp submissions dir.
func newTestServerWithSubmissions(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	tmpDir := t.TempDir()
	staticDir := filepath.Join(tmpDir, "static")
	if err := os.MkdirAll(staticDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("<html></html>"), 0644); err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(Config{
		StaticDir:      staticDir,
		DBPath:         filepath.Join(tmpDir, "portal.db"),
		JWTSecret:      []byte("test-secret-key-that-is-long-enough"),
		TeacherToken:   "test-teacher-token",
		SubmissionsDir: filepath.Join(tmpDir, "submissions"),
		JPlagJar:       filepath.Join(tmpDir, "jplag.jar"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { server.Close() })
	return server, server.Handler()
}

func TestPlagErrorMessage(t *testing.T) {
	tagged := "some progress\n2026-09-11 [ERROR] CLI - Not enough valid submissions! (found 0 valid submissions)\n"
	if got := plagErrorMessage(tagged); got != "Not enough valid submissions! (found 0 valid submissions)" {
		t.Fatalf("tagged error: %q", got)
	}

	jvmCrash := "WARNING: A restricted method in java.lang.System has been called\r\n" +
		"Parsing Submissions   0% [                               ] 0/2\r\n" +
		"Error: LinkageError occurred while loading main class de.jplag.cli.CLI\n" +
		"java.lang.UnsupportedClassVersionError: class file version 69.0\n"
	got := plagErrorMessage(jvmCrash)
	if !strings.Contains(got, "UnsupportedClassVersionError") || strings.Contains(got, "WARNING") || strings.Contains(got, "=====") {
		t.Fatalf("jvm crash tail: %q", got)
	}

	if got := plagErrorMessage(""); got != "JPlag exited with an error" {
		t.Fatalf("empty output: %q", got)
	}
}

func TestJplagArgs(t *testing.T) {
	args := jplagArgs("python3", "/out/run1", "/out/run1/subs", "")
	joined := strings.Join(args, " ")
	for _, flag := range []string{"--frequency", "--match-merging", "--csv-export", "-l python3", "-M RUN"} {
		if !strings.Contains(joined, flag) {
			t.Errorf("args %v missing %q", args, flag)
		}
	}
	// Normalization is Java-only; JPlag rejects it for other languages.
	if strings.Contains(joined, "--normalize") {
		t.Errorf("python args must not include --normalize: %v", args)
	}
	if strings.Contains(joined, "-bc") {
		t.Errorf("args without base code must not include -bc: %v", args)
	}

	args = jplagArgs("java", "/out/run1", "/out/run1/subs", "/out/base")
	joined = strings.Join(args, " ")
	if !strings.Contains(joined, "--normalize") {
		t.Errorf("java args must include --normalize: %v", args)
	}
	if !strings.Contains(joined, "-bc /out/base") {
		t.Errorf("args with base code must include -bc: %v", args)
	}
}

func TestParsePlagCSV(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "results.csv")
	content := "submissionName1,submissionName2,averageSimilarity,maxSimilarity\r\n" +
		"john.doe,jane.doe,0.9234,0.9911\r\n" +
		"jane.doe,bob.smith,0.12,0.33\r\n" +
		"jane.doe,unknown.student,0.5,0.5\r\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	pkByName := map[string]int{"john.doe": 7, "jane.doe": 8, "bob.smith": 9}
	pairs, err := parsePlagCSV(path, 5, pkByName)
	if err != nil {
		t.Fatal(err)
	}
	// The unknown submission name is skipped.
	if len(pairs) != 2 {
		t.Fatalf("pairs = %+v", pairs)
	}
	if pairs[0].StudentA != 7 || pairs[0].StudentB != 8 || pairs[0].Similarity != 0.9234 || pairs[0].RunID != 5 {
		t.Fatalf("pair = %+v", pairs[0])
	}

	// A header with unexpected columns is an error.
	bad := filepath.Join(dir, "bad.csv")
	if err := os.WriteFile(bad, []byte("a,b\n1,2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := parsePlagCSV(bad, 5, pkByName); err == nil {
		t.Fatal("expected error for bad header")
	}
}

func TestPlagSubmissionNames(t *testing.T) {
	roster := []AdminStudent{
		{StudentID: 1, Username: "john.doe"},
		{StudentID: 2, Username: "jane.doe"},
		{StudentID: 3, Username: "weird name!"},
		{StudentID: 4, Username: "john.doe"}, // duplicate falls back to the id
	}
	latest := map[int]*SubSubmission{1: nil, 2: nil, 3: nil, 4: nil, 5: nil}
	dirByPK, pkByDir := plagSubmissionNames(roster, latest)

	if dirByPK[1] != "john.doe" || dirByPK[2] != "jane.doe" {
		t.Fatalf("dirByPK = %v", dirByPK)
	}
	if dirByPK[3] != "weird_name_" {
		t.Fatalf("sanitized name = %q", dirByPK[3])
	}
	if dirByPK[4] != "4" {
		t.Fatalf("duplicate username should fall back to id, got %q", dirByPK[4])
	}
	if dirByPK[5] != "5" {
		t.Fatalf("student without account should use id, got %q", dirByPK[5])
	}
	if len(pkByDir) != 5 || pkByDir["john.doe"] != 1 || pkByDir["4"] != 4 {
		t.Fatalf("pkByDir = %v", pkByDir)
	}
}
