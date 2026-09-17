package portalserver

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// testRunTimeout caps a single test execution.
const testRunTimeout = 30 * time.Second

// maxOutputBytes caps the output stored on a test run.
const maxOutputBytes = 64 << 10

// graderQueueSize bounds the number of pending jobs.
const graderQueueSize = 512

// graderJob is one unit of grading work: either a test run or a plagiarism run.
type graderJob struct {
	testRunID int64
	plagRunID int64
}

// Grader executes test runs and plagiarism jobs one at a time.
type Grader struct {
	store          *Store
	submissionsDir string
	jplagJar       string
	jobs           chan graderJob
	stop           chan struct{}
	running        atomic.Int32
	// execMu serializes harness execution so a synchronous sample run cannot
	// overlap a queued run.
	execMu sync.Mutex
}

func newGrader(store *Store, submissionsDir, jplagJar string) *Grader {
	return &Grader{
		store:          store,
		submissionsDir: submissionsDir,
		jplagJar:       jplagJar,
		jobs:           make(chan graderJob, graderQueueSize),
		stop:           make(chan struct{}),
	}
}

func (g *Grader) run() {
	for {
		select {
		case job := <-g.jobs:
			g.running.Add(1)
			if job.testRunID != 0 {
				g.executeTestRun(job.testRunID)
			} else if job.plagRunID != 0 {
				g.executePlagRun(job.plagRunID)
			}
			g.running.Add(-1)
		case <-g.stop:
			return
		}
	}
}

func (g *Grader) close() {
	select {
	case <-g.stop:
	default:
		close(g.stop)
	}
}

// enqueueTest adds a test run to the queue; false when the queue is full.
func (g *Grader) enqueueTest(runID int64) bool {
	return g.enqueue(graderJob{testRunID: runID})
}

// enqueuePlag adds a plagiarism run to the queue; false when the queue is full.
func (g *Grader) enqueuePlag(runID int64) bool {
	return g.enqueue(graderJob{plagRunID: runID})
}

func (g *Grader) enqueue(job graderJob) bool {
	select {
	case g.jobs <- job:
		return true
	default:
		return false
	}
}

// queueDepth reports pending and in-flight jobs for the admin UI.
func (g *Grader) queueDepth() (depth int, running bool) {
	return len(g.jobs), g.running.Load() > 0
}

// assignmentDir returns the on-disk directory of an assignment.
func assignmentDir(base string, id int64) string {
	return filepath.Join(base, fmt.Sprintf("assignment_%d", id))
}

// testsDir returns the directory holding an assignment's harness files.
func testsDir(base string, assignmentID int64) string {
	return filepath.Join(assignmentDir(base, assignmentID), "tests")
}

// submissionDir returns the directory of one uploaded attempt.
func submissionDir(base string, assignmentID int64, studentPK, attempt int) string {
	return filepath.Join(assignmentDir(base, assignmentID),
		fmt.Sprintf("student_%d", studentPK), fmt.Sprintf("attempt_%d", attempt))
}

// sampleDir returns the directory holding an assignment's sample solution.
func sampleDir(base string, assignmentID int64) string {
	return filepath.Join(assignmentDir(base, assignmentID), "sample")
}

// basecodeDir returns the directory holding an assignment's optional base
// code (starter/template code subtracted from plagiarism comparisons).
func basecodeDir(base string, assignmentID int64) string {
	return filepath.Join(assignmentDir(base, assignmentID), "basecode")
}

// executeTestRun stages, executes, and records one test run.
func (g *Grader) executeTestRun(runID int64) {
	run, err := g.store.GetSubTestRun(runID)
	if err != nil || run == nil || run.Status != "queued" {
		return
	}
	if err := g.store.StartTestRun(runID); err != nil {
		log.Printf("grader: start run %d: %v", runID, err)
		return
	}

	finish := func(status string, passed, failed int, output string) {
		if err := g.store.FinishTestRun(runID, status, passed, failed, output); err != nil {
			log.Printf("grader: finish run %d: %v", runID, err)
		}
	}

	sub, err := g.store.GetSubSubmission(run.SubmissionID)
	if err != nil || sub == nil {
		finish("error", 0, 0, "submission not found")
		return
	}
	asg, err := g.store.GetSubAssignment(sub.AssignmentID)
	if err != nil || asg == nil {
		finish("error", 0, 0, "assignment not found")
		return
	}
	test, err := g.store.GetSubTest(run.TestID)
	if err != nil || test == nil {
		finish("error", 0, 0, "test not found")
		return
	}

	files, err := g.store.ListSubFiles(sub.ID)
	if err != nil {
		finish("error", 0, 0, "failed to list submission files")
		return
	}
	names := make([]string, 0, len(files))
	for _, f := range files {
		names = append(names, f.Filename)
	}
	srcDir := submissionDir(g.submissionsDir, asg.ID, sub.StudentPK, sub.Attempt)
	harnessPath := filepath.Join(testsDir(g.submissionsDir, asg.ID), test.StoredName)
	finish(g.runHarness(asg.Language, srcDir, names, harnessPath, test.StoredName))
}

// runHarness stages srcFiles from srcDir plus the harness into a fresh
// workspace, compiles (Java) and executes it, and reports the outcome.
// Executions are serialized via execMu so an admin's sample run cannot
// overlap a queued run on a small host.
func (g *Grader) runHarness(language, srcDir string, srcFiles []string, harnessPath, harnessName string) (status string, passed, failed int, output string) {
	g.execMu.Lock()
	defer g.execMu.Unlock()

	fail := func(status, msg string) (string, int, int, string) {
		return status, 0, 0, msg
	}

	runsDir := filepath.Join(g.submissionsDir, "runs")
	if err := os.MkdirAll(runsDir, 0o755); err != nil {
		return fail("error", "failed to create workspace")
	}
	stage, err := os.MkdirTemp(runsDir, "run-")
	if err != nil {
		return fail("error", "failed to create workspace")
	}
	defer func() { _ = os.RemoveAll(stage) }()

	for _, name := range srcFiles {
		if err := copyFile(filepath.Join(srcDir, name), filepath.Join(stage, name)); err != nil {
			return fail("error", fmt.Sprintf("failed to stage %s", name))
		}
	}
	if err := copyFile(harnessPath, filepath.Join(stage, harnessName)); err != nil {
		return fail("error", "failed to stage test harness")
	}

	var stdout, stderr string
	var exitCode int
	var timedOut bool

	switch language {
	case "java":
		javac, err := exec.LookPath("javac")
		if err != nil {
			return fail("unavailable", "javac is not installed on the server")
		}
		javaBin, err := exec.LookPath("java")
		if err != nil {
			return fail("unavailable", "java is not installed on the server")
		}
		var javaFiles []string
		for _, name := range srcFiles {
			if strings.HasSuffix(strings.ToLower(name), ".java") {
				javaFiles = append(javaFiles, name)
			}
		}
		javaFiles = append(javaFiles, harnessName)
		// Bound the compiler JVM's heap too: HotSpot defaults max heap to a
		// quarter of host RAM, which on a large host exceeds the sandbox
		// address-space cap and kills javac before it starts.
		compileArgs := append([]string{"-J-Xmx512m", "-encoding", "UTF-8"}, javaFiles...)
		_, compileErr, compileCode, compileTimeout := runSandboxed(stage, testRunTimeout, javaLimits, javac, compileArgs...)
		if compileTimeout {
			return "timeout", 0, 1, "compilation timed out"
		}
		if compileCode != 0 {
			if strings.TrimSpace(compileErr) == "" {
				// A JVM that dies before producing diagnostics (e.g. killed by
				// a resource cap) prints nothing — still record something useful.
				compileErr = fmt.Sprintf("compilation failed (javac exit %d, no output)", compileCode)
			}
			return "done", 0, 1, compileErr
		}
		mainClass := strings.TrimSuffix(harnessName, filepath.Ext(harnessName))
		stdout, stderr, exitCode, timedOut = runSandboxed(stage, testRunTimeout, javaLimits, javaBin, "-Xmx256m", mainClass)
	case "python":
		python, err := exec.LookPath("python3")
		if err != nil {
			return fail("unavailable", "python3 is not installed on the server")
		}
		stdout, stderr, exitCode, timedOut = runSandboxed(stage, testRunTimeout, defaultLimits, python, harnessName)
	default:
		return fail("error", fmt.Sprintf("no test runner for language %q", language))
	}

	passed, failed = parsePassFail(stdout)
	status = "done"
	switch {
	case timedOut:
		status = "timeout"
	case passed+failed == 0 && exitCode != 0:
		status = "error"
	}
	output = strings.TrimRight(stdout, "\n") + stderrSection(stderr)
	if len(output) > maxOutputBytes {
		output = output[:maxOutputBytes]
	}
	return status, passed, failed, output
}

// stderrSection appends captured stderr, separated from the PASS/FAIL stdout.
func stderrSection(stderr string) string {
	stderr = strings.TrimRight(stderr, "\n")
	if stderr == "" {
		return ""
	}
	return "\n" + stderr
}

// parsePassFail counts PASS:/FAIL: lines (leading whitespace allowed).
func parsePassFail(output string) (passed, failed int) {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimLeft(line, " \t")
		switch {
		case strings.HasPrefix(line, "PASS:"):
			passed++
		case strings.HasPrefix(line, "FAIL:"):
			failed++
		}
	}
	return passed, failed
}

// copyFile copies one file, preserving no metadata.
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

// sandboxLimits are the prlimit resource caps applied to one test process.
type sandboxLimits struct {
	cpuSeconds   int
	addressSpace int64
	maxProcesses int
	maxFileSize  int64
}

var defaultLimits = sandboxLimits{cpuSeconds: 25, addressSpace: 512 << 20, maxProcesses: 32, maxFileSize: 32 << 20}

// javaLimits give the JVM room to start: it reserves gigabytes of virtual
// address space (heap, compressed class space, metaspace) and spawns dozens
// of threads, so the default caps kill the VM before main() runs. The real
// heap stays bounded by -Xmx, CPU time by the cpu cap and the wall timeout.
var javaLimits = sandboxLimits{cpuSeconds: 25, addressSpace: 4 << 30, maxProcesses: 256, maxFileSize: 32 << 20}

// runSandboxed executes a command under prlimit with a timeout, killing the
// whole process group when the deadline passes.
func runSandboxed(dir string, timeout time.Duration, limits sandboxLimits, name string, args ...string) (stdout, stderr string, exitCode int, timedOut bool) {
	if prlimit, err := exec.LookPath("prlimit"); err == nil {
		args = append([]string{
			fmt.Sprintf("--cpu=%d", limits.cpuSeconds),
			fmt.Sprintf("--as=%d", limits.addressSpace),
			fmt.Sprintf("--nproc=%d", limits.maxProcesses),
			fmt.Sprintf("--fsize=%d", limits.maxFileSize),
			"--core=0", "--", name}, args...)
		name = prlimit
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = 5 * time.Second

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &limitedWriter{w: &outBuf, limit: maxOutputBytes * 4}
	cmd.Stderr = &limitedWriter{w: &errBuf, limit: maxOutputBytes * 4}

	err := cmd.Run()
	timedOut = ctx.Err() == context.DeadlineExceeded
	exitCode = 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else if !timedOut {
			exitCode = -1
			errBuf.WriteString(err.Error())
		}
	}
	if timedOut && exitCode == 0 {
		exitCode = -1
	}
	return outBuf.String(), errBuf.String(), exitCode, timedOut
}

// limitedWriter discards bytes beyond limit so runaway output cannot exhaust memory.
type limitedWriter struct {
	w     *bytes.Buffer
	limit int
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	if remaining := l.limit - l.w.Len(); remaining > 0 {
		if len(p) > remaining {
			l.w.Write(p[:remaining])
		} else {
			l.w.Write(p)
		}
	}
	return len(p), nil
}
