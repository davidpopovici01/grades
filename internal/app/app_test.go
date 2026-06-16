package app

import (
	"bytes"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewWritesStartupTraceWhenEnabled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GRADES_HOME", home)
	t.Setenv("GRADES_DB_PATH", filepath.Join(home, "grades.db"))
	t.Setenv("GRADES_NO_OPEN", "1")
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

func TestCalculateCategoryScoreCountsMissingLateAndExplicitZero(t *testing.T) {
	rule := CategoryRule{SchemeKey: "average"}
	assignments := []AssignmentScoreMeta{
		{ID: 1, MaxPoints: 10, Anchor: 100, Lift: 1},
		{ID: 2, MaxPoints: 10, Anchor: 100, Lift: 1},
		{ID: 3, MaxPoints: 10, Anchor: 100, Lift: 1},
		{ID: 4, MaxPoints: 10, Anchor: 100, Lift: 1},
	}
	grades := map[int]GradeRecord{
		1: {Score: sql.NullFloat64{Float64: 8, Valid: true}},
		2: {Score: sql.NullFloat64{Float64: 0, Valid: true}},
		3: {Flags: flagMissing},
		4: {Flags: flagLate},
	}

	score, ok := calculateCategoryScore(rule, assignments, grades)
	if !ok {
		t.Fatalf("expected category score to be included")
	}
	if score != 20 {
		t.Fatalf("expected average to include explicit zero, missing, and late as counted zeroes, got %v", score)
	}
}

func TestPortalCategoryScoreCountsMissingLateAndExplicitZero(t *testing.T) {
	rule := CategoryRule{SchemeKey: "average"}
	items := []portalAssignmentDetail{
		{studentAssignmentDetail: studentAssignmentDetail{
			Grade:  GradeRecord{Score: sql.NullFloat64{Float64: 8, Valid: true}, MaxPoints: 10},
			Anchor: 100,
			Lift:   1,
		}},
		{studentAssignmentDetail: studentAssignmentDetail{
			Grade:  GradeRecord{Score: sql.NullFloat64{Float64: 0, Valid: true}, MaxPoints: 10},
			Anchor: 100,
			Lift:   1,
		}},
		{studentAssignmentDetail: studentAssignmentDetail{
			Grade:  GradeRecord{Flags: flagMissing, MaxPoints: 10},
			Anchor: 100,
			Lift:   1,
		}},
		{studentAssignmentDetail: studentAssignmentDetail{
			Grade:  GradeRecord{Flags: flagLate, MaxPoints: 10},
			Anchor: 100,
			Lift:   1,
		}},
	}

	score, ok := portalCategoryScore(rule, items)
	if !ok {
		t.Fatalf("expected portal category score to be included")
	}
	if score != 20 {
		t.Fatalf("expected portal average to include explicit zero, missing, and late as counted zeroes, got %v", score)
	}
}

func TestEffectiveAssignmentPercentPassPenalty(t *testing.T) {
	pass := sql.NullFloat64{Valid: true, Float64: 0}
	cases := []struct {
		name    string
		record  GradeRecord
		pass    sql.NullFloat64
		want    float64
	}{
		{"pass only", GradeRecord{Flags: flagPass}, pass, 100},
		{"pass late", GradeRecord{Flags: flagPass | flagLate}, pass, 90},
		{"pass redo", GradeRecord{Flags: flagPass | flagRedo}, pass, 90},
		{"pass late redo", GradeRecord{Flags: flagPass | flagLate | flagRedo}, pass, 80},
		{"raw late", GradeRecord{Score: sql.NullFloat64{Valid: true, Float64: 80}, MaxPoints: 100, Flags: flagLate}, pass, 70},
		{"raw late 100", GradeRecord{Score: sql.NullFloat64{Valid: true, Float64: 100}, MaxPoints: 100, Flags: flagLate}, pass, 90},
		{"raw redo", GradeRecord{Score: sql.NullFloat64{Valid: true, Float64: 100}, MaxPoints: 100, Flags: flagRedo}, pass, 90},
		{"raw late redo", GradeRecord{Score: sql.NullFloat64{Valid: true, Float64: 100}, MaxPoints: 100, Flags: flagLate | flagRedo}, pass, 80},
		{"completion pass late", GradeRecord{Score: sql.NullFloat64{Valid: true, Float64: 80}, MaxPoints: 100, Flags: flagLate}, sql.NullFloat64{Valid: true, Float64: 70}, 90},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := effectiveAssignmentPercent(tc.record, tc.pass, 100, 1)
			if got != tc.want {
				t.Fatalf("effectiveAssignmentPercent() = %v, want %v", got, tc.want)
			}
		})
	}
}
