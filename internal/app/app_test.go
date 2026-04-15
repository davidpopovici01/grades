package app

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewWritesStartupTraceWhenEnabled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRADES_HOME", home)
	t.Setenv("GRADES_DB_PATH", filepath.Join(home, "grades.db"))
	t.Setenv("GRADES_STARTUP_TRACE", "1")

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app, err := New(strings.NewReader(""), &stdout, &stderr)
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	defer app.Close()

	trace := stderr.String()
	if !strings.Contains(trace, "startup load config:") {
		t.Fatalf("expected startup trace to include config timing, got:\n%s", trace)
	}
	if !strings.Contains(trace, "startup open db:") {
		t.Fatalf("expected startup trace to include db timing, got:\n%s", trace)
	}
	if !strings.Contains(trace, "startup migrate:") {
		t.Fatalf("expected startup trace to include migration timing, got:\n%s", trace)
	}
	if !strings.Contains(trace, "startup total:") {
		t.Fatalf("expected startup trace to include total timing, got:\n%s", trace)
	}
}

func TestSlowStartupHintDisabledByDefault(t *testing.T) {
	trace := &startupTrace{
		out:     ioDiscard{},
		enabled: false,
		started: time.Now().Add(-6 * time.Second),
		last:    time.Now(),
	}
	var buf bytes.Buffer
	trace.out = &buf
	trace.finish()
	if !strings.Contains(buf.String(), "set GRADES_STARTUP_TRACE=1 for breakdown") {
		t.Fatalf("expected slow-start hint, got %q", buf.String())
	}
}
