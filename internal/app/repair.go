package app

import (
	"database/sql"
	"fmt"
)

type repairAction struct {
	AssignmentID int
	StudentID    int
	OldScore     sql.NullFloat64
	NewScore     sql.NullFloat64
	OldFlags     int
	NewFlags     int
	Reason       string
}

type repairReport struct {
	MissingLate int
	LegacyRedo  int
	RedoZero    int
}

func (r repairReport) total() int {
	return r.MissingLate + r.LegacyRedo + r.RedoZero
}

func (r repairReport) lines() []string {
	return []string{
		fmt.Sprintf("missing+late -> late only: %d", r.MissingLate),
		fmt.Sprintf("low scores -> add redo: %d", r.LegacyRedo),
		fmt.Sprintf("redo zero-score -> scoreless redo: %d", r.RedoZero),
		fmt.Sprintf("total repairs: %d", r.total()),
	}
}

func (a *App) AuditRepairs() error {
	actions, report, err := a.collectRepairActions()
	if err != nil {
		return err
	}
	if len(actions) == 0 {
		fmt.Fprintln(a.out, "No repairs needed.")
		return nil
	}
	fmt.Fprintln(a.out, "Repair audit")
	for _, line := range report.lines() {
		fmt.Fprintln(a.out, line)
	}
	return nil
}

func (a *App) ApplyRepairs() error {
	actions, report, err := a.collectRepairActions()
	if err != nil {
		return err
	}
	if len(actions) == 0 {
		fmt.Fprintln(a.out, "No repairs needed.")
		return nil
	}

	tx, err := a.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, action := range actions {
		if _, err := tx.Exec(`
			UPDATE grades
			SET score = ?, flags_bitmask = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')
			WHERE assignment_id = ? AND student_pk = ?`,
			nullableFloat(action.NewScore), action.NewFlags, action.AssignmentID, action.StudentID); err != nil {
			return err
		}
		if _, err := tx.Exec(`
			INSERT INTO grade_audit(assignment_id, student_pk, action, old_score, new_score, old_flags, new_flags, reason)
			VALUES (?, ?, 'UPDATE', ?, ?, ?, ?, ?)`,
			action.AssignmentID, action.StudentID,
			nullableFloat(action.OldScore), nullableFloat(action.NewScore),
			action.OldFlags, action.NewFlags, action.Reason); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	fmt.Fprintln(a.out, "Applied repairs")
	for _, line := range report.lines() {
		fmt.Fprintln(a.out, line)
	}
	return nil
}

func (a *App) collectRepairActions() ([]repairAction, repairReport, error) {
	rows, err := a.db.Query(`
		SELECT grades.assignment_id,
		       grades.student_pk,
		       grades.score,
		       grades.flags_bitmask,
		       assignments.max_points,
		       COALESCE(assignments.pass_percent, category_grading_policies.default_pass_percent, 80)
		FROM grades
		JOIN assignments ON assignments.assignment_id = grades.assignment_id
		LEFT JOIN category_grading_policies
		  ON category_grading_policies.course_year_id = assignments.course_year_id
		 AND category_grading_policies.term_id = assignments.term_id
		 AND category_grading_policies.category_id = assignments.category_id`)
	if err != nil {
		return nil, repairReport{}, err
	}
	defer rows.Close()

	var actions []repairAction
	var report repairReport
	for rows.Next() {
		var assignmentID, studentID, flags, maxPoints int
		var score sql.NullFloat64
		var passPercent sql.NullFloat64
		if err := rows.Scan(&assignmentID, &studentID, &score, &flags, &maxPoints, &passPercent); err != nil {
			return nil, repairReport{}, err
		}

		newScore := score
		newFlags := flags
		reason := ""

		switch {
		case flags&(flagMissing|flagLate) == (flagMissing | flagLate):
			newScore = sql.NullFloat64{}
			newFlags = (flags | flagLate) &^ flagMissing
			reason = "repair-missing-late"
			report.MissingLate++
		case flags&flagRedo != 0 && flags&(flagMissing|flagPass|flagLate|flagLocked0) == 0 && score.Valid && score.Float64 == 0:
			newScore = sql.NullFloat64{}
			reason = "repair-redo-zero"
			report.RedoZero++
		case shouldRepairLegacyRedo(score, flags, maxPoints, passPercent):
			newFlags = flags | flagRedo
			reason = "repair-legacy-redo"
			report.LegacyRedo++
		}

		if reason == "" {
			continue
		}
		actions = append(actions, repairAction{
			AssignmentID: assignmentID,
			StudentID:    studentID,
			OldScore:     score,
			NewScore:     newScore,
			OldFlags:     flags,
			NewFlags:     newFlags,
			Reason:       reason,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, repairReport{}, err
	}
	return actions, report, nil
}

func shouldRepairLegacyRedo(score sql.NullFloat64, flags, maxPoints int, passPercent sql.NullFloat64) bool {
	if flags&(flagRedo|flagMissing|flagPass|flagLocked0) != 0 {
		return false
	}
	if !score.Valid || maxPoints <= 0 || !passPercent.Valid || passPercent.Float64 <= 0 {
		return false
	}
	return (score.Float64/float64(maxPoints))*100 < passPercent.Float64
}

func nullableFloat(value sql.NullFloat64) any {
	if !value.Valid {
		return nil
	}
	return value.Float64
}
