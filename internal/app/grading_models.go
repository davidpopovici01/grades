package app

import (
	"database/sql"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
)

func gradingSchemes() []GradingSchemeDefinition {
	return []GradingSchemeDefinition{
		{
			Key:         "completion",
			Label:       "Completion",
			Description: "Homework/quiz style: starts at 100, minus 10 for each redo and minus 10 if late, then averaged.",
		},
		{
			Key:         "average",
			Label:       "Average Raw",
			Description: "Average of assignment percentages in the category after assignment curves are applied.",
		},
		{
			Key:         "total-points",
			Label:       "Total Points",
			Description: "Add curved points across the category and divide by total possible points.",
		},
	}
}

func resolveGradingScheme(value string) (GradingSchemeDefinition, error) {
	value = strings.ToLower(normalizeSpaces(value))
	switch value {
	case "completion", "complete", "homework", "quiz", "quizzes":
		return gradingSchemes()[0], nil
	case "average", "average raw", "raw", "projects", "tests", "test", "project":
		return gradingSchemes()[1], nil
	case "total-points", "total points", "totals", "midterm", "final", "finals":
		return gradingSchemes()[2], nil
	default:
		for _, scheme := range gradingSchemes() {
			if strings.EqualFold(scheme.Key, value) || strings.EqualFold(scheme.Label, value) {
				return scheme, nil
			}
		}
		return GradingSchemeDefinition{}, fmt.Errorf("unknown grading scheme: %s", value)
	}
}

func schemeLabel(key string) string {
	for _, scheme := range gradingSchemes() {
		if scheme.Key == key {
			return scheme.Label
		}
	}
	return key
}

func (a *App) ListGradingSchemes() error {
	for _, scheme := range gradingSchemes() {
		fmt.Fprintf(a.out, "%s\t%s\t%s\n", scheme.Key, scheme.Label, scheme.Description)
	}
	return nil
}

func (a *App) SetCategoryScheme(value, schemeValue string) error {
	ctx := a.context()
	if ctx.TermID == 0 || ctx.CourseYearID == 0 {
		return errors.New("set year, term, and course first")
	}
	categoryID, categoryName, err := a.resolveCategoryInteractive(value)
	if err != nil {
		return err
	}
	scheme, err := resolveGradingScheme(schemeValue)
	if err != nil {
		return err
	}
	rule, exists, err := a.categoryPolicy(ctx.CourseYearID, ctx.TermID, categoryID)
	if err != nil {
		return err
	}
	passPercent := rule.DefaultPassPercent
	if scheme.Key != "completion" {
		passPercent = sql.NullFloat64{Float64: 0, Valid: true}
	} else if !exists {
		passPercent = sql.NullFloat64{Float64: 80, Valid: true}
	}
	if err := a.upsertCategoryPolicy(ctx.CourseYearID, ctx.TermID, categoryID, scheme.Key, passPercent); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Set grading scheme: %s = %s\n", categoryName, scheme.Label)
	return nil
}

func (a *App) categoryRulesForContext(courseYearID, termID int) ([]CategoryRule, error) {
	rows, err := a.db.Query(`
		SELECT categories.category_id,
		       categories.name,
		       COALESCE(category_scheme_weights.weight_percent, 0),
		       category_scheme_weights.weight_percent IS NOT NULL,
		       COALESCE(category_grading_policies.scheme_key, 'average'),
		       category_grading_policies.default_pass_percent
		FROM categories
		LEFT JOIN category_scheme_weights
		  ON category_scheme_weights.category_id = categories.category_id
		 AND category_scheme_weights.scheme_id = (
		       SELECT scheme_id
		       FROM course_year_terms
		       WHERE course_year_id = ? AND term_id = ?
		 )
		LEFT JOIN category_grading_policies
		  ON category_grading_policies.course_year_id = ?
		 AND category_grading_policies.term_id = ?
		 AND category_grading_policies.category_id = categories.category_id
		WHERE categories.category_id IN (
			SELECT DISTINCT category_id
			FROM assignments
			WHERE course_year_id = ? AND term_id = ?
		)
		OR categories.category_id IN (
			SELECT category_id
			FROM category_scheme_weights
			WHERE scheme_id = (
				SELECT scheme_id
				FROM course_year_terms
				WHERE course_year_id = ? AND term_id = ?
			)
		)
		OR categories.category_id IN (
			SELECT category_id
			FROM category_grading_policies
			WHERE course_year_id = ? AND term_id = ?
		)
		ORDER BY categories.name`,
		courseYearID, termID,
		courseYearID, termID,
		courseYearID, termID,
		courseYearID, termID,
		courseYearID, termID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var rules []CategoryRule
	for rows.Next() {
		var rule CategoryRule
		if err := rows.Scan(&rule.CategoryID, &rule.CategoryName, &rule.WeightPercent, &rule.HasWeight, &rule.SchemeKey, &rule.DefaultPassPercent); err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, rows.Err()
}

func (a *App) ensureDefaultGradesForAssignment(assignmentID, courseYearID, termID int) error {
	_, err := a.db.Exec(`
		INSERT INTO grades(assignment_id, student_pk, score, flags_bitmask, redo_count)
		SELECT ?, section_enrollments.student_pk, 0, ?, 0
		FROM section_enrollments
		JOIN sections ON sections.section_id = section_enrollments.section_id
		WHERE section_enrollments.term_id = ? AND sections.course_year_id = ?
		ON CONFLICT(assignment_id, student_pk) DO NOTHING`,
		assignmentID, flagMissing, termID, courseYearID)
	return err
}

func (a *App) ensureDefaultGradesForStudent(sectionID, termID, studentID int) error {
	var courseYearID int
	if err := a.db.QueryRow(`SELECT course_year_id FROM sections WHERE section_id = ?`, sectionID).Scan(&courseYearID); err != nil {
		return err
	}
	rows, err := a.db.Query(`SELECT assignment_id FROM assignments WHERE course_year_id = ? AND term_id = ?`, courseYearID, termID)
	if err != nil {
		return err
	}
	var assignmentIDs []int
	for rows.Next() {
		var assignmentID int
		if err := rows.Scan(&assignmentID); err != nil {
			_ = rows.Close()
			return err
		}
		assignmentIDs = append(assignmentIDs, assignmentID)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, assignmentID := range assignmentIDs {
		if _, err := a.db.Exec(`
			INSERT INTO grades(assignment_id, student_pk, score, flags_bitmask, redo_count)
			VALUES (?, ?, 0, ?, 0)
			ON CONFLICT(assignment_id, student_pk) DO NOTHING`,
			assignmentID, studentID, flagMissing); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) MarkMissingLate() error {
	ctx := a.context()
	if ctx.AssignmentID == 0 {
		return errors.New("set an assignment first")
	}
	if err := a.ensureDefaultGradesForCurrentAssignment(); err != nil {
		return err
	}
	res, err := a.db.Exec(`
		UPDATE grades
		SET score = NULL,
		    flags_bitmask = (flags_bitmask | ?) & ~?
		WHERE assignment_id = ?
		  AND (flags_bitmask & ?) != 0
		  AND (score IS NOT NULL OR (flags_bitmask & ?) = 0)`,
		flagLate, flagMissing, ctx.AssignmentID, flagMissing, flagLate)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Marked %d missing grade(s) as late.\n", affected)
	return nil
}

func (a *App) UndoMarkMissingLate() error {
	ctx := a.context()
	if ctx.AssignmentID == 0 {
		return errors.New("set an assignment first")
	}
	res, err := a.db.Exec(`
		UPDATE grades
		SET score = 0,
		    flags_bitmask = (flags_bitmask | ?) & ~?
		WHERE assignment_id = ? AND score IS NULL AND (flags_bitmask & ?) != 0 AND (flags_bitmask & ?) = 0`,
		flagMissing, flagLate, ctx.AssignmentID, flagLate, flagMissing)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Restored %d late grade(s) back to missing.\n", affected)
	return nil
}

func (a *App) ensureDefaultGradesForCurrentAssignment() error {
	ctx := a.context()
	if ctx.AssignmentID == 0 {
		return errors.New("set an assignment first")
	}
	var courseYearID, termID int
	if err := a.db.QueryRow(`SELECT course_year_id, term_id FROM assignments WHERE assignment_id = ?`, ctx.AssignmentID).Scan(&courseYearID, &termID); err != nil {
		return err
	}
	return a.ensureDefaultGradesForAssignment(ctx.AssignmentID, courseYearID, termID)
}

func (a *App) AssignmentCurve() (AssignmentCurve, error) {
	ctx := a.context()
	if ctx.AssignmentID == 0 {
		return AssignmentCurve{}, errors.New("set an assignment first")
	}
	return a.assignmentCurveByID(ctx.AssignmentID)
}

func (a *App) assignmentCurveByID(assignmentID int) (AssignmentCurve, error) {
	curve := AssignmentCurve{AssignmentID: assignmentID, AnchorPercent: 100, LiftPercent: 0}
	err := a.db.QueryRow(`SELECT assignment_id, anchor_percent, lift_percent FROM assignment_curves WHERE assignment_id = ?`, assignmentID).
		Scan(&curve.AssignmentID, &curve.AnchorPercent, &curve.LiftPercent)
	if errors.Is(err, sql.ErrNoRows) {
		return curve, nil
	}
	return curve, err
}

func (a *App) ShowAssignmentCurve() error {
	curve, err := a.AssignmentCurve()
	if err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Curve anchor:\t%.1f\n", curve.AnchorPercent)
	fmt.Fprintf(a.out, "Curve lift:\t%.1f\n", curve.LiftPercent)
	return nil
}

func (a *App) SetAssignmentCurve(anchorRaw, liftRaw string) error {
	ctx := a.context()
	if ctx.AssignmentID == 0 {
		return errors.New("set an assignment first")
	}
	anchor, err := parsePositiveFloat(anchorRaw, "anchor")
	if err != nil {
		return err
	}
	lift, err := parseRangeFloat(liftRaw, "lift", 0, 100)
	if err != nil {
		return err
	}
	if _, err := a.db.Exec(`
		INSERT INTO assignment_curves(assignment_id, anchor_percent, lift_percent)
		VALUES (?, ?, ?)
		ON CONFLICT(assignment_id) DO UPDATE SET anchor_percent = excluded.anchor_percent, lift_percent = excluded.lift_percent, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')`,
		ctx.AssignmentID, anchor, lift); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Set assignment curve: anchor %.1f, lift %.1f\n", anchor, lift)
	return nil
}

func (a *App) TuneAssignmentCurve(targetRaw string) error {
	ctx := a.context()
	if ctx.AssignmentID == 0 {
		return errors.New("set an assignment first")
	}
	if err := a.ensureDefaultGradesForCurrentAssignment(); err != nil {
		return err
	}
	target, err := parsePositiveFloat(targetRaw, "target average")
	if err != nil {
		return err
	}
	curve, err := a.assignmentCurveByID(ctx.AssignmentID)
	if err != nil {
		return err
	}
	records, err := a.assignmentGradesForPopulation()
	if err != nil {
		return err
	}
	if len(records) == 0 {
		return errors.New("no students found for curve tuning")
	}
	low, high := 0.0, 100.0
	for i := 0; i < 40; i++ {
		mid := (low + high) / 2
		avg := averageCurvedPercent(records, curve.AnchorPercent, mid)
		if avg < target {
			low = mid
		} else {
			high = mid
		}
	}
	lift := math.Round(high*10) / 10
	if _, err := a.db.Exec(`
		INSERT INTO assignment_curves(assignment_id, anchor_percent, lift_percent)
		VALUES (?, ?, ?)
		ON CONFLICT(assignment_id) DO UPDATE SET anchor_percent = excluded.anchor_percent, lift_percent = excluded.lift_percent, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')`,
		ctx.AssignmentID, curve.AnchorPercent, lift); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Set assignment curve target %.1f with anchor %.1f and lift %.1f\n", target, curve.AnchorPercent, lift)
	return nil
}

func parsePositiveFloat(raw, label string) (float64, error) {
	value, err := parseRangeFloat(raw, label, 0, 1000)
	if err != nil {
		return 0, err
	}
	if value <= 0 {
		return 0, fmt.Errorf("%s must be greater than 0", label)
	}
	return value, nil
}

func parseRangeFloat(raw, label string, min, max float64) (float64, error) {
	raw = strings.TrimSpace(strings.TrimSuffix(raw, "%"))
	if raw == "" {
		return 0, fmt.Errorf("%s cannot be blank", label)
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid %s: %s", label, raw)
	}
	if value < min || value > max {
		return 0, fmt.Errorf("%s must be between %.0f and %.0f", label, min, max)
	}
	return value, nil
}

func curvedPercent(rawPercent, anchorPercent, liftPercent float64) float64 {
	if rawPercent <= 0 {
		return 0
	}
	rawExponent := (100 - liftPercent) / 100
	anchorExponent := liftPercent / 100
	return math.Pow(rawPercent, rawExponent) * math.Pow(anchorPercent, anchorExponent)
}

func averageCurvedPercent(records []GradeRecord, anchor, lift float64) float64 {
	if len(records) == 0 {
		return 0
	}
	total := 0.0
	for _, record := range records {
		total += recordPercent(record, anchor, lift)
	}
	return total / float64(len(records))
}

func recordPercent(record GradeRecord, anchor, lift float64) float64 {
	if !record.Score.Valid || record.MaxPoints <= 0 || record.Flags&flagMissing != 0 {
		return 0
	}
	rawPercent := (record.Score.Float64 / float64(record.MaxPoints)) * 100
	return curvedPercent(rawPercent, anchor, lift)
}

func completionPercent(record GradeRecord, passPercent sql.NullFloat64, anchor, lift float64) float64 {
	if !passPercent.Valid || passPercent.Float64 <= 0 {
		return recordPercent(record, anchor, lift)
	}
	if record.Flags&flagMissing != 0 || !record.Score.Valid || record.MaxPoints <= 0 {
		return 0
	}
	if (record.Score.Float64/float64(record.MaxPoints))*100 < passPercent.Float64 {
		return 0
	}
	score := 100.0
	if record.Flags&flagRedo != 0 {
		score -= 10
	}
	if record.Flags&flagLate != 0 {
		score -= 10
	}
	if score < 0 {
		return 0
	}
	return score
}

func (a *App) ShowCategoryScores() error {
	ctx := a.context()
	if ctx.TermID == 0 || ctx.CourseYearID == 0 {
		return errors.New("set year, term, and course first")
	}
	students, scope, err := a.studentsForList()
	if err != nil {
		return err
	}
	if len(students) == 0 {
		fmt.Fprintln(a.out, "No students found.")
		return nil
	}
	if scope != "" {
		fmt.Fprintln(a.out, scope)
	}
	rules, err := a.categoryRulesForContext(ctx.CourseYearID, ctx.TermID)
	if err != nil {
		return err
	}
	scores, weighted, err := a.categoryScoresByStudent(students, rules)
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(a.out, 0, 0, 2, ' ', 0)
	fmt.Fprint(tw, "Student")
	for _, rule := range rules {
		fmt.Fprintf(tw, "\t%s", rule.CategoryName)
	}
	fmt.Fprint(tw, "\tWeighted")
	fmt.Fprintln(tw)
	for _, student := range students {
		fmt.Fprintf(tw, "%s %s", student.FirstName, student.LastName)
		for _, rule := range rules {
			fmt.Fprintf(tw, "\t%s", scores[student.ID][rule.CategoryID])
		}
		fmt.Fprintf(tw, "\t%s", weighted[student.ID])
		fmt.Fprintln(tw)
	}
	return tw.Flush()
}

func (a *App) categoryScoresByStudent(students []Student, rules []CategoryRule) (map[int]map[int]string, map[int]string, error) {
	ctx := a.context()
	rows, err := a.db.Query(`
		SELECT assignments.assignment_id,
		       assignments.category_id,
		       assignments.max_points,
		       COALESCE(assignment_curves.anchor_percent, 100),
		       COALESCE(assignment_curves.lift_percent, 0),
		       COALESCE(assignments.pass_percent, category_grading_policies.default_pass_percent)
		FROM assignments
		LEFT JOIN assignment_curves ON assignment_curves.assignment_id = assignments.assignment_id
		LEFT JOIN category_grading_policies
		  ON category_grading_policies.course_year_id = assignments.course_year_id
		 AND category_grading_policies.term_id = assignments.term_id
		 AND category_grading_policies.category_id = assignments.category_id
		WHERE assignments.course_year_id = ? AND assignments.term_id = ?
		ORDER BY assignments.assignment_id`, ctx.CourseYearID, ctx.TermID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	assignments := map[int]AssignmentScoreMeta{}
	assignmentsByCategory := map[int][]AssignmentScoreMeta{}
	for rows.Next() {
		var meta AssignmentScoreMeta
		if err := rows.Scan(&meta.ID, &meta.CategoryID, &meta.MaxPoints, &meta.Anchor, &meta.Lift, &meta.PassPercent); err != nil {
			return nil, nil, err
		}
		assignments[meta.ID] = meta
		assignmentsByCategory[meta.CategoryID] = append(assignmentsByCategory[meta.CategoryID], meta)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	gradeRows, err := a.db.Query(`
		SELECT grades.assignment_id, grades.student_pk, grades.score, grades.flags_bitmask, assignments.max_points, grades.redo_count
		FROM grades
		JOIN assignments ON assignments.assignment_id = grades.assignment_id
		WHERE assignments.course_year_id = ? AND assignments.term_id = ?`, ctx.CourseYearID, ctx.TermID)
	if err != nil {
		return nil, nil, err
	}
	defer gradeRows.Close()

	gradesByStudent := map[int]map[int]GradeRecord{}
	for gradeRows.Next() {
		var assignmentID int
		var record GradeRecord
		if err := gradeRows.Scan(&assignmentID, &record.StudentID, &record.Score, &record.Flags, &record.MaxPoints, &record.RedoCount); err != nil {
			return nil, nil, err
		}
		if _, ok := gradesByStudent[record.StudentID]; !ok {
			gradesByStudent[record.StudentID] = map[int]GradeRecord{}
		}
		gradesByStudent[record.StudentID][assignmentID] = record
	}
	if err := gradeRows.Err(); err != nil {
		return nil, nil, err
	}

	values := map[int]map[int]string{}
	weighted := map[int]string{}
	for _, student := range students {
		values[student.ID] = map[int]string{}
		weightedTotal := 0.0
		totalWeight := 0.0
		for _, rule := range rules {
			score, ok := calculateCategoryScore(rule, assignmentsByCategory[rule.CategoryID], gradesByStudent[student.ID])
			if !ok {
				values[student.ID][rule.CategoryID] = ""
				continue
			}
			values[student.ID][rule.CategoryID] = fmt.Sprintf("%.1f%%", score)
			if rule.HasWeight {
				weightedTotal += score * rule.WeightPercent
				totalWeight += rule.WeightPercent
			}
		}
		if totalWeight > 0 {
			weighted[student.ID] = fmt.Sprintf("%.1f%%", weightedTotal/totalWeight)
		} else {
			weighted[student.ID] = ""
		}
	}
	return values, weighted, nil
}

func calculateCategoryScore(rule CategoryRule, assignments []AssignmentScoreMeta, grades map[int]GradeRecord) (float64, bool) {
	if len(assignments) == 0 {
		return 0, false
	}
	switch rule.SchemeKey {
	case "completion":
		total := 0.0
		for _, assignment := range assignments {
			record := grades[assignment.ID]
			record.MaxPoints = assignment.MaxPoints
			total += completionPercent(record, assignment.PassPercent, assignment.Anchor, assignment.Lift)
		}
		return total / float64(len(assignments)), true
	case "total-points":
		sum := 0.0
		maxTotal := 0.0
		for _, assignment := range assignments {
			record := grades[assignment.ID]
			record.MaxPoints = assignment.MaxPoints
			maxTotal += float64(assignment.MaxPoints)
			sum += (recordPercent(record, assignment.Anchor, assignment.Lift) / 100) * float64(assignment.MaxPoints)
		}
		if maxTotal == 0 {
			return 0, false
		}
		return (sum / maxTotal) * 100, true
	default:
		total := 0.0
		for _, assignment := range assignments {
			record := grades[assignment.ID]
			record.MaxPoints = assignment.MaxPoints
			total += recordPercent(record, assignment.Anchor, assignment.Lift)
		}
		return total / float64(len(assignments)), true
	}
}

func (a *App) assignmentGradesForPopulation() ([]GradeRecord, error) {
	ctx := a.context()
	students, _, err := a.studentsForList()
	if err != nil {
		return nil, err
	}
	sort.Slice(students, func(i, j int) bool {
		if students[i].LastName == students[j].LastName {
			return students[i].FirstName < students[j].FirstName
		}
		return students[i].LastName < students[j].LastName
	})
	records := make([]GradeRecord, 0, len(students))
	for _, student := range students {
		record, err := a.currentGrade(ctx.AssignmentID, student.ID)
		if err != nil {
			return nil, err
		}
		if record == nil {
			record = &GradeRecord{StudentID: student.ID, Name: student.FirstName + " " + student.LastName}
		}
		records = append(records, *record)
	}
	return records, nil
}
