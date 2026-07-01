package cmd_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/davidpopovici01/grades/cmd"
)

func TestClassFlagLoadsSavedProfile(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)

	// Create a saved APCSA profile with a section selected.
	ctxDir := filepath.Join(env.home, "contexts")
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

	// Also set current_course so the profile is recognized as active.
	configPath := filepath.Join(env.home, "config.yaml")
	configContent := `context:
  current_course: APCSA
portal:
  server: ""
  key: ""
  remote_dir: "~/portal"
`
	if err := os.WriteFile(configPath, []byte(configContent), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	root := cmd.NewRootCmdWithClass(strings.NewReader(""), &stdout, &stderr, "APCSA")
	root.SetArgs([]string{})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v\nstderr:\n%s", err, stderr.String())
	}

	out := stdout.String()
	if !strings.Contains(out, "APCSA") {
		t.Errorf("dashboard missing APCSA:\n%s", out)
	}
	if !strings.Contains(out, "12A") {
		t.Errorf("dashboard missing section 12A:\n%s", out)
	}
	if !strings.Contains(out, "Fall 2026") {
		t.Errorf("dashboard missing term Fall 2026:\n%s", out)
	}
}

func TestContextProfilesCommandListsProfiles(t *testing.T) {
	env := newTestEnv(t)
	seedBaseData(t, env)

	ctxDir := filepath.Join(env.home, "contexts")
	if err := os.MkdirAll(ctxDir, 0o755); err != nil {
		t.Fatalf("mkdir contexts: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ctxDir, "APCSA.yaml"), []byte("context:\n  term_id: 1\n"), 0o644); err != nil {
		t.Fatalf("write APCSA profile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ctxDir, "APCSP.yaml"), []byte("context:\n  term_id: 1\n"), 0o644); err != nil {
		t.Fatalf("write APCSP profile: %v", err)
	}

	out := mustRun(t, env, "", "context", "profiles")
	if !strings.Contains(out, "APCSA") {
		t.Errorf("profiles missing APCSA:\n%s", out)
	}
	if !strings.Contains(out, "APCSP") {
		t.Errorf("profiles missing APCSP:\n%s", out)
	}
}
