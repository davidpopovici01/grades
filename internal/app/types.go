package app

import "database/sql"

const (
	flagLate    = 1 << 0
	flagMissing = 1 << 1
	flagPass    = 1 << 2
	flagRedo    = 1 << 3
	flagLocked0 = 1 << 4
)

type NamedID struct {
	ID   int
	Name string
}

type Student struct {
	ID              int
	FirstName       string
	LastName        string
	ChineseName     string
	SchoolStudentID string
	PowerSchoolNum  string
}

type Assignment struct {
	ID        int
	Title     string
	Category  string
	MaxPoints int
}

type GradeRecord struct {
	StudentID   int
	Name        string
	Score       sql.NullFloat64
	Flags       int
	MaxPoints   int
	RedoCount   int
	PassPercent sql.NullFloat64
}

type GradebookRow struct {
	StudentID int
	Name      string
	Values    []string
}

type Stats struct {
	Average float64
	Highest float64
	Lowest  float64
	Count   int
}

type gradeEntry struct {
	Score     *float64
	Flags     int
	ClearRedo bool
}

type gradeHistory struct {
	Student Student
	Prev    *GradeRecord
}

type CategoryRule struct {
	CategoryID         int
	CategoryName       string
	WeightPercent      float64
	HasWeight          bool
	SchemeKey          string
	DefaultPassPercent sql.NullFloat64
}

type GradingSchemeDefinition struct {
	Key         string
	Label       string
	Description string
}

type AssignmentCurve struct {
	AssignmentID  int
	AnchorPercent float64
	LiftPercent   float64
}

type AssignmentScoreMeta struct {
	ID          int
	CategoryID  int
	MaxPoints   int
	Anchor      float64
	Lift        float64
	PassPercent sql.NullFloat64
}
