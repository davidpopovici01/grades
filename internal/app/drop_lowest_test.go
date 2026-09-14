package app

import (
	"database/sql"
	"math"
	"testing"
)

func scoreRecord(score float64) GradeRecord {
	return GradeRecord{Score: sql.NullFloat64{Float64: score, Valid: true}}
}

func TestDropLowestIDsDisabledOrTooFew(t *testing.T) {
	candidates := []dropCandidate{
		{id: 1, maxPoints: 100, percent: 50},
		{id: 2, maxPoints: 100, percent: 90},
	}
	if got := dropLowestIDs(candidates, 0); len(got) != 0 {
		t.Fatalf("expected no drops when n is 0, got %v", got)
	}
	if got := dropLowestIDs(candidates, -2); len(got) != 0 {
		t.Fatalf("expected no drops when n is negative, got %v", got)
	}
	if got := dropLowestIDs([]dropCandidate{{id: 1, maxPoints: 100, percent: 50}}, 3); len(got) != 0 {
		t.Fatalf("expected no drops with a single candidate, got %v", got)
	}
}

func TestDropLowestIDsKeepsAtLeastOne(t *testing.T) {
	candidates := []dropCandidate{
		{id: 1, maxPoints: 100, percent: 40},
		{id: 2, maxPoints: 100, percent: 60},
		{id: 3, maxPoints: 100, percent: 95},
	}
	got := dropLowestIDs(candidates, 5)
	if len(got) != 2 || !got[1] || !got[2] || got[3] {
		t.Fatalf("expected only the two lowest dropped, got %v", got)
	}
}

func TestDropLowestIDSTieBreak(t *testing.T) {
	candidates := []dropCandidate{
		{id: 1, maxPoints: 50, percent: 80},
		{id: 2, maxPoints: 100, percent: 80},
		{id: 3, maxPoints: 100, percent: 80},
		{id: 4, maxPoints: 100, percent: 100},
	}
	// Equal percents: higher max points dropped first, then lower assignment ID.
	got := dropLowestIDs(candidates, 2)
	if len(got) != 2 || !got[2] || !got[3] {
		t.Fatalf("expected higher-point ties dropped first (ids 2 and 3), got %v", got)
	}
}

func TestCalculateCategoryScoreDropLowestAverage(t *testing.T) {
	rule := CategoryRule{SchemeKey: "average", DropLowest: 2}
	assignments := []AssignmentScoreMeta{
		{ID: 1, MaxPoints: 100, Anchor: 100, Lift: 1},
		{ID: 2, MaxPoints: 100, Anchor: 100, Lift: 1},
		{ID: 3, MaxPoints: 100, Anchor: 100, Lift: 1},
		{ID: 4, MaxPoints: 100, Anchor: 100, Lift: 1},
	}
	grades := map[int]GradeRecord{
		1: scoreRecord(100),
		2: scoreRecord(80),
		3: scoreRecord(60),
		4: scoreRecord(40),
	}
	score, ok := calculateCategoryScore(rule, assignments, grades)
	if !ok {
		t.Fatalf("expected category score to be included")
	}
	if score != 90 {
		t.Fatalf("expected average of remaining 100 and 80 to be 90, got %v", score)
	}
}

func TestCalculateCategoryScoreDropLowestAbsorbsMissing(t *testing.T) {
	rule := CategoryRule{SchemeKey: "average", DropLowest: 1}
	assignments := []AssignmentScoreMeta{
		{ID: 1, MaxPoints: 100, Anchor: 100, Lift: 1},
		{ID: 2, MaxPoints: 100, Anchor: 100, Lift: 1},
	}
	grades := map[int]GradeRecord{
		1: scoreRecord(100),
		2: {Flags: flagMissing},
	}
	score, ok := calculateCategoryScore(rule, assignments, grades)
	if !ok {
		t.Fatalf("expected category score to be included")
	}
	if score != 100 {
		t.Fatalf("expected missing assignment to be dropped, got %v", score)
	}
}

func TestCalculateCategoryScoreDropLowestTotalPoints(t *testing.T) {
	rule := CategoryRule{SchemeKey: "total-points", DropLowest: 1}
	assignments := []AssignmentScoreMeta{
		{ID: 1, MaxPoints: 100, Anchor: 100, Lift: 1},
		{ID: 2, MaxPoints: 50, Anchor: 100, Lift: 1},
		{ID: 3, MaxPoints: 100, Anchor: 100, Lift: 1},
	}
	grades := map[int]GradeRecord{
		1: scoreRecord(100),
		2: scoreRecord(30),
		3: scoreRecord(40),
	}
	score, ok := calculateCategoryScore(rule, assignments, grades)
	if !ok {
		t.Fatalf("expected category score to be included")
	}
	// 40% assignment dropped from both numerator and denominator: (100+30)/(100+50).
	want := (100.0 + 30.0) / 150.0 * 100
	if math.Abs(score-want) > 0.0001 {
		t.Fatalf("expected %v, got %v", want, score)
	}
}

func TestCalculateCategoryScoreDropLowestKeepsAtLeastOne(t *testing.T) {
	rule := CategoryRule{SchemeKey: "average", DropLowest: 10}
	assignments := []AssignmentScoreMeta{
		{ID: 1, MaxPoints: 100, Anchor: 100, Lift: 1},
		{ID: 2, MaxPoints: 100, Anchor: 100, Lift: 1},
	}
	grades := map[int]GradeRecord{
		1: scoreRecord(50),
		2: scoreRecord(90),
	}
	score, ok := calculateCategoryScore(rule, assignments, grades)
	if !ok {
		t.Fatalf("expected category score to be included")
	}
	if score != 90 {
		t.Fatalf("expected only the best assignment to remain, got %v", score)
	}
}

func TestCalculateCategoryScoreDropLowestZeroIsNoOp(t *testing.T) {
	assignments := []AssignmentScoreMeta{
		{ID: 1, MaxPoints: 100, Anchor: 100, Lift: 1},
		{ID: 2, MaxPoints: 100, Anchor: 100, Lift: 1},
	}
	grades := map[int]GradeRecord{
		1: scoreRecord(100),
		2: scoreRecord(50),
	}
	withDrop, ok := calculateCategoryScore(CategoryRule{SchemeKey: "average"}, assignments, grades)
	if !ok {
		t.Fatalf("expected category score to be included")
	}
	if withDrop != 75 {
		t.Fatalf("expected unchanged average of 75 with drop disabled, got %v", withDrop)
	}
}

func TestPortalCategoryScoreDropLowestParity(t *testing.T) {
	rule := CategoryRule{SchemeKey: "average", DropLowest: 1}
	items := []portalAssignmentDetail{
		{AssignmentID: 1, studentAssignmentDetail: studentAssignmentDetail{
			Grade:  GradeRecord{Score: sql.NullFloat64{Float64: 100, Valid: true}, MaxPoints: 100},
			Anchor: 100,
			Lift:   1,
		}},
		{AssignmentID: 2, studentAssignmentDetail: studentAssignmentDetail{
			Grade:  GradeRecord{Score: sql.NullFloat64{Float64: 80, Valid: true}, MaxPoints: 100},
			Anchor: 100,
			Lift:   1,
		}},
		{AssignmentID: 3, studentAssignmentDetail: studentAssignmentDetail{
			Grade:  GradeRecord{Flags: flagMissing, MaxPoints: 100},
			Anchor: 100,
			Lift:   1,
		}},
	}
	score, ok := portalCategoryScore(rule, items)
	if !ok {
		t.Fatalf("expected portal category score to be included")
	}
	if score != 90 {
		t.Fatalf("expected portal average of remaining 100 and 80 to be 90, got %v", score)
	}
	_, _, dropped := portalCategoryScoreWithDrops(rule, items)
	if len(dropped) != 1 || !dropped[3] {
		t.Fatalf("expected assignment 3 (missing) to be marked dropped, got %v", dropped)
	}
}

func TestParseDropLowestSetting(t *testing.T) {
	for _, raw := range []string{"", "off", "none", "no", "0"} {
		n, err := parseDropLowestSetting(raw)
		if err != nil || n != 0 {
			t.Fatalf("expected %q to parse as 0, got %d, %v", raw, n, err)
		}
	}
	n, err := parseDropLowestSetting("3")
	if err != nil || n != 3 {
		t.Fatalf("expected 3, got %d, %v", n, err)
	}
	for _, raw := range []string{"-1", "abc", "1.5"} {
		if _, err := parseDropLowestSetting(raw); err == nil {
			t.Fatalf("expected %q to be rejected", raw)
		}
	}
}

func TestCategoryImportRecordParsesDropLowest(t *testing.T) {
	headers := map[string]int{"category": 0, "weight": 1, "drop_lowest": 2}
	record, err := categoryImportRecordFromRow(headers, []string{"Homework", "40", "2"})
	if err != nil {
		t.Fatalf("parse row: %v", err)
	}
	if record.DropLowest == nil || *record.DropLowest != "2" {
		t.Fatalf("expected drop_lowest to be parsed, got %+v", record)
	}
	record, err = categoryImportRecordFromRow(headers, []string{"Homework", "40", ""})
	if err != nil {
		t.Fatalf("parse row: %v", err)
	}
	if record.DropLowest != nil {
		t.Fatalf("expected empty drop_lowest to stay unset, got %q", *record.DropLowest)
	}
}
