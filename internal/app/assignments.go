package app

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

func (a *App) ListAssignments() error {
	ctx := a.context()
	if ctx.TermID == 0 || ctx.CourseYearID == 0 {
		return errors.New("set year, term, and course first")
	}
	rows, err := a.db.Query(`
		SELECT assignment_id, title || ' (' || categories.name || ', ' || CAST(assignments.max_points AS TEXT) || ' pts)'
		FROM assignments
		JOIN categories ON categories.category_id = assignments.category_id
		WHERE assignments.term_id = ? AND assignments.course_year_id = ?
		ORDER BY assignments.assignment_id`, ctx.TermID, ctx.CourseYearID)
	if err != nil {
		return err
	}
	defer rows.Close()
	return printRows(a.out, rows)
}

func (a *App) AddAssignmentInteractive() error {
	ctx := a.context()
	if ctx.TermID == 0 || ctx.CourseYearID == 0 {
		return errors.New("set year, term, and course first")
	}
	title, err := a.promptNonEmpty("Title")
	if err != nil {
		return err
	}
	if err := a.ensureAssignmentTitleAvailable(ctx.CourseYearID, ctx.TermID, title); err != nil {
		return err
	}
	maxPoints, err := a.promptIntRetry("Max score", func(v int) error {
		if v <= 0 {
			return errors.New("max score must be greater than 0")
		}
		return nil
	})
	if err != nil {
		return err
	}
	if err := a.printExistingCategories(); err != nil {
		return err
	}
	categoryName, err := a.promptOptional("Category")
	if err != nil {
		return err
	}
	var categoryID int
	categoryDisplay := categoryName
	if categoryName == "" {
		categoryDisplay = "General"
		categoryID, err = a.ensureCategory(categoryDisplay)
		if err != nil {
			return err
		}
	} else {
		categoryID, categoryDisplay, err = a.resolveCategoryInteractive(categoryName)
		if err != nil {
			return err
		}
	}
	passPercent, err := a.promptAssignmentPassRate(ctx.CourseYearID, ctx.TermID, categoryID)
	if err != nil {
		return err
	}
	res, err := a.db.Exec(`
		INSERT INTO assignments(course_year_id, term_id, category_id, title, max_points, pass_percent)
		VALUES (?, ?, ?, ?, ?, ?)`,
		ctx.CourseYearID, ctx.TermID, categoryID, title, maxPoints, nullablePassRateValue(passPercent))
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	if err := a.ensureDefaultGradesForAssignment(int(id), ctx.CourseYearID, ctx.TermID); err != nil {
		return err
	}
	a.v.Set("context.assignment_id", int(id))
	if err := a.v.WriteConfig(); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Added assignment %d: %s\n", id, title)
	fmt.Fprintf(a.out, "Category: %s\n", categoryDisplay)
	if passPercent.Valid {
		fmt.Fprintf(a.out, "Pass rate: %s\n", passPercentLabel(passPercent, false))
	}
	fmt.Fprintln(a.out, colorOrange("Switched to assignment: "+title))
	return nil
}

func (a *App) CreateAssignmentInteractive() error {
	return a.AddAssignmentInteractive()
}

func (a *App) printExistingCategories() error {
	items, err := a.namedIDs(`SELECT category_id, name FROM categories ORDER BY category_id`)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return nil
	}
	fmt.Fprintln(a.out, "Existing categories:")
	for _, item := range items {
		fmt.Fprintf(a.out, "%d\t%s\n", item.ID, item.Name)
	}
	return nil
}

func (a *App) ShowAssignment(idRaw string) error {
	id, err := a.assignmentIDFromInput(idRaw)
	if err != nil {
		return err
	}
	if id == 0 {
		id = a.context().AssignmentID
	}
	if id == 0 {
		return errors.New("set or provide an assignment first")
	}
	var assignment Assignment
	var assignmentPass sql.NullFloat64
	var categoryPass sql.NullFloat64
	err = a.db.QueryRow(`
		SELECT assignments.assignment_id, assignments.title, categories.name, assignments.max_points,
		       assignments.pass_percent,
		       category_grading_policies.default_pass_percent
		FROM assignments
		JOIN categories ON categories.category_id = assignments.category_id
		LEFT JOIN category_grading_policies
		  ON category_grading_policies.course_year_id = assignments.course_year_id
		 AND category_grading_policies.term_id = assignments.term_id
		 AND category_grading_policies.category_id = assignments.category_id
		WHERE assignments.assignment_id = ?`, id).
		Scan(&assignment.ID, &assignment.Title, &assignment.Category, &assignment.MaxPoints, &assignmentPass, &categoryPass)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("assignment not found: %d", id)
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(a.out, "%s\n", assignment.Title)
	fmt.Fprintf(a.out, "Category:\t%s\n", assignment.Category)
	fmt.Fprintf(a.out, "Max score:\t%d\n", assignment.MaxPoints)
	fmt.Fprintf(a.out, "Pass rate:\t%s\n", effectiveAssignmentPassRateLabel(assignmentPass, categoryPass))
	curve, err := a.assignmentCurveByID(assignment.ID)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Curve:\tanchor %.1f, lift %.1f\n", curve.AnchorPercent, curve.LiftPercent)
	return nil
}

func (a *App) DeleteAssignment(idRaw string) error {
	id, err := a.assignmentIDFromInput(idRaw)
	if err != nil {
		return err
	}
	if id == 0 {
		id = a.context().AssignmentID
	}
	if id == 0 {
		return errors.New("set or provide an assignment first")
	}
	res, err := a.db.Exec(`DELETE FROM assignments WHERE assignment_id = ?`, id)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return fmt.Errorf("assignment not found: %d", id)
	}
	if a.context().AssignmentID == id {
		a.v.Set("context.assignment_id", 0)
		if err := a.v.WriteConfig(); err != nil {
			return err
		}
	}
	fmt.Fprintf(a.out, "Deleted assignment %d\n", id)
	return nil
}

func (a *App) SetAssignmentMaxPoints(maxPointsRaw string) error {
	ctx := a.context()
	if ctx.AssignmentID == 0 {
		return errors.New("set an assignment first")
	}
	maxPoints, err := strconv.Atoi(strings.TrimSpace(maxPointsRaw))
	if err != nil {
		return fmt.Errorf("invalid max score: %s", maxPointsRaw)
	}
	if maxPoints <= 0 {
		return errors.New("max score must be greater than 0")
	}
	res, err := a.db.Exec(`UPDATE assignments SET max_points = ? WHERE assignment_id = ?`, maxPoints, ctx.AssignmentID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return fmt.Errorf("assignment not found: %d", ctx.AssignmentID)
	}
	fmt.Fprintf(a.out, "Updated max score to %d\n", maxPoints)
	return nil
}

func (a *App) SetAssignmentPassRate(raw string) error {
	ctx := a.context()
	if ctx.AssignmentID == 0 {
		return errors.New("set an assignment first")
	}
	passPercent, err := parseAssignmentPassRate(raw)
	if err != nil {
		return err
	}
	_, err = a.db.Exec(`UPDATE assignments SET pass_percent = ? WHERE assignment_id = ?`, nullablePassRateValue(passPercent), ctx.AssignmentID)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Updated assignment pass rate to %s\n", passPercentLabel(passPercent, false))
	return nil
}

func (a *App) promptAssignmentPassRate(courseYearID, termID, categoryID int) (sql.NullFloat64, error) {
	rule, exists, err := a.categoryPolicy(courseYearID, termID, categoryID)
	if err != nil {
		return sql.NullFloat64{}, err
	}
	defaultLabel := "80%"
	if exists {
		defaultLabel = passPercentLabel(rule.DefaultPassPercent, true)
	}
	raw, err := a.promptAssignmentPassRateInput(fmt.Sprintf(`Pass rate percent or "raw" (blank = %s)`, defaultLabel))
	if err != nil {
		return sql.NullFloat64{}, err
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		if exists {
			return sql.NullFloat64{}, nil
		}
		defaultPass := sql.NullFloat64{Float64: 80, Valid: true}
		if err := a.upsertCategoryPolicy(courseYearID, termID, categoryID, "completion", defaultPass); err != nil {
			return sql.NullFloat64{}, err
		}
		return sql.NullFloat64{}, nil
	}
	passPercent, schemeKey, err := parsePassRateSetting(raw)
	if err != nil {
		return sql.NullFloat64{}, err
	}
	if !exists {
		if err := a.upsertCategoryPolicy(courseYearID, termID, categoryID, schemeKey, passPercent); err != nil {
			return sql.NullFloat64{}, err
		}
		return sql.NullFloat64{}, nil
	}
	return passPercent, nil
}

func (a *App) promptAssignmentPassRateInput(label string) (string, error) {
	fmt.Fprintf(a.out, "%s: ", label)
	line, err := a.in.ReadString('\n')
	if err != nil {
		if errors.Is(err, io.EOF) {
			return strings.TrimSpace(line), nil
		}
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func parseAssignmentPassRate(raw string) (sql.NullFloat64, error) {
	raw = strings.ToLower(strings.TrimSpace(raw))
	switch raw {
	case "default", "inherit", "":
		return sql.NullFloat64{}, nil
	case "raw", "none", "off":
		return sql.NullFloat64{Float64: 0, Valid: true}, nil
	}
	value, err := parseRangeFloat(raw, "pass rate", 1, 100)
	if err != nil {
		return sql.NullFloat64{}, err
	}
	return sql.NullFloat64{Float64: value, Valid: true}, nil
}

func effectiveAssignmentPassRateLabel(assignmentPass, categoryPass sql.NullFloat64) string {
	if assignmentPass.Valid {
		return passPercentLabel(assignmentPass, false)
	}
	if categoryPass.Valid {
		return "category default (" + passPercentLabel(categoryPass, false) + ")"
	}
	return "category default (none)"
}

func (a *App) ensureCategory(name string) (int, error) {
	name = normalizeSpaces(name)
	if err := validateCategoryName(name); err != nil {
		return 0, err
	}
	var id int
	err := a.db.QueryRow(`SELECT category_id FROM categories WHERE lower(name)=lower(?)`, name).Scan(&id)
	switch {
	case err == nil:
		return id, nil
	case errors.Is(err, sql.ErrNoRows):
		res, err := a.db.Exec(`INSERT INTO categories(name) VALUES (?)`, name)
		if err != nil {
			return 0, err
		}
		lastID, err := res.LastInsertId()
		return int(lastID), err
	default:
		return 0, err
	}
}

func (a *App) ensureAssignmentTitleAvailable(courseYearID, termID int, title string) error {
	title = normalizeSpaces(title)
	var existing string
	err := a.db.QueryRow(`
		SELECT title
		FROM assignments
		WHERE course_year_id = ? AND term_id = ? AND lower(title) = lower(?)`,
		courseYearID, termID, title).Scan(&existing)
	switch {
	case err == nil:
		return fmt.Errorf("assignment already exists: %s", existing)
	case errors.Is(err, sql.ErrNoRows):
		return nil
	default:
		return err
	}
}

func validateCategoryName(name string) error {
	if name == "" {
		return errors.New("category name cannot be blank")
	}
	if _, err := strconv.Atoi(name); err == nil {
		return errors.New("category name cannot be only numbers")
	}
	return nil
}

func (a *App) resolveCategoryInteractive(name string) (int, string, error) {
	name = normalizeSpaces(name)
	if id, display, err := a.lookupCategory(name); err == nil {
		return id, display, nil
	}

	existing, err := a.namedIDs(`SELECT category_id, name FROM categories ORDER BY category_id`)
	if err != nil {
		return 0, "", err
	}
	if len(existing) == 0 {
		ok, err := a.promptYesNo(fmt.Sprintf(`Category "%s" does not exist. Create it as a new category?`, name), false)
		if err != nil {
			return 0, "", err
		}
		if !ok {
			return 0, "", errors.New("category creation cancelled")
		}
		id, err := a.ensureCategory(name)
		return id, name, err
	}

	for {
		answer, err := a.promptOptional(fmt.Sprintf(`Category "%s" does not exist. Type "alias" to map it to an existing category or "new" to create it`, name))
		if err != nil {
			return 0, "", err
		}
		switch strings.ToLower(answer) {
		case "alias", "a":
			fmt.Fprintln(a.out, "Existing categories:")
			for _, item := range existing {
				fmt.Fprintf(a.out, "%d\t%s\n", item.ID, item.Name)
			}
			target, err := a.promptNonEmpty("Existing category name or ID")
			if err != nil {
				return 0, "", err
			}
			id, display, err := a.lookupContextID("category", target, `SELECT category_id, name FROM categories WHERE lower(name)=lower(?)`, `SELECT category_id, name FROM categories WHERE category_id = ?`)
			if err != nil {
				fmt.Fprintln(a.out, retryMessage(err.Error()))
				continue
			}
			if err := a.createCategoryAlias(name, id); err != nil {
				return 0, "", err
			}
			return id, display, nil
		case "new", "n", "":
			ok, err := a.promptYesNo(fmt.Sprintf(`Create "%s" as a new category?`, name), false)
			if err != nil {
				return 0, "", err
			}
			if !ok {
				continue
			}
			id, err := a.ensureCategory(name)
			return id, name, err
		default:
			fmt.Fprintln(a.out, retryMessage(`please answer "alias" or "new"`))
		}
	}
}

func (a *App) lookupCategory(name string) (int, string, error) {
	name = normalizeSpaces(name)
	if id, err := strconv.Atoi(name); err == nil {
		var display string
		err := a.db.QueryRow(`SELECT category_id, name FROM categories WHERE category_id = ?`, id).Scan(&id, &display)
		if err == nil {
			return id, display, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, "", err
		}
	}
	var id int
	var display string
	err := a.db.QueryRow(`SELECT category_id, name FROM categories WHERE lower(name)=lower(?)`, name).Scan(&id, &display)
	if err == nil {
		return id, display, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, "", err
	}
	err = a.db.QueryRow(`
		SELECT categories.category_id, categories.name
		FROM category_aliases
		JOIN categories ON categories.category_id = category_aliases.category_id
		WHERE lower(category_aliases.alias_name)=lower(?)`, name).Scan(&id, &display)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", fmt.Errorf("category not found: %s", name)
	}
	return id, display, err
}

func (a *App) createCategoryAlias(alias string, categoryID int) error {
	_, err := a.db.Exec(`INSERT INTO category_aliases(alias_name, category_id) VALUES (?, ?)`, normalizeSpaces(alias), categoryID)
	return err
}

func (a *App) assignmentIDFromInput(idRaw string) (int, error) {
	idRaw = strings.TrimSpace(idRaw)
	if idRaw == "" {
		return 0, nil
	}
	id, err := strconv.Atoi(idRaw)
	if err != nil {
		return 0, fmt.Errorf("invalid assignment id: %s", idRaw)
	}
	return id, nil
}

func (a *App) assignmentsForContext() ([]Assignment, error) {
	ctx := a.context()
	rows, err := a.db.Query(`
		SELECT assignments.assignment_id, assignments.title, categories.name, assignments.max_points
		FROM assignments
		JOIN categories ON categories.category_id = assignments.category_id
		WHERE assignments.term_id = ? AND assignments.course_year_id = ?
		ORDER BY assignments.assignment_id`, ctx.TermID, ctx.CourseYearID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var assignments []Assignment
	for rows.Next() {
		var assignment Assignment
		if err := rows.Scan(&assignment.ID, &assignment.Title, &assignment.Category, &assignment.MaxPoints); err != nil {
			return nil, err
		}
		assignments = append(assignments, assignment)
	}
	return assignments, rows.Err()
}
