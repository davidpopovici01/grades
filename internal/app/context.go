package app

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

func (a *App) PrintDashboard() error {
	ctx := a.context()
	yearName, termName, courseName, sectionName, assignmentName, err := a.resolveContextNames(ctx)
	if err != nil {
		return err
	}

	fmt.Fprintln(a.out, "Grades CLI")
	fmt.Fprintln(a.out)
	fmt.Fprintln(a.out, "Context")
	fmt.Fprintf(a.out, "  Year:\t%s\n", fallback(yearName))
	fmt.Fprintf(a.out, "  Term:\t%s\n", fallback(termName))
	fmt.Fprintf(a.out, "  Course:\t%s\n", fallback(courseName))
	fmt.Fprintf(a.out, "  Section:\t%s\n", fallback(sectionName))
	fmt.Fprintf(a.out, "  Assignment:\t%s\n", fallback(assignmentName))
	fmt.Fprintln(a.out)
	fmt.Fprintln(a.out, "Next steps")
	fmt.Fprintln(a.out, "  - grades setup to add a new course")
	if ctx.CourseYearID == 0 || ctx.TermID == 0 {
		fmt.Fprintln(a.out, "  - grades students list after setting year, term, and course")
	} else {
		fmt.Fprintln(a.out, "  - grades students list")
	}
	if ctx.AssignmentID == 0 {
		fmt.Fprintln(a.out, "  - grades use assignment <name> to switch to an assignment")
	} else {
		fmt.Fprintln(a.out, "  - grades enter to enter grades")
		fmt.Fprintln(a.out, "  - grades show to review the current assignment")
	}
	return nil
}

func (a *App) UseYear(value string) error {
	years, err := a.availableYears()
	if err != nil {
		return err
	}
	selected, err := pickYear(years, value)
	if err != nil {
		return err
	}
	a.v.Set("context.year", selected)
	a.v.Set("context.course_year_id", 0)
	a.v.Set("context.section_id", 0)
	a.v.Set("context.assignment_id", 0)
	if err := a.v.WriteConfig(); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(a.out, "Using year: %s\n", selected)
	return nil
}

func (a *App) UseTerm(name string) error {
	id, display, err := a.lookupContextID("term", name, `SELECT term_id, name FROM terms WHERE lower(name)=lower(?)`, `SELECT term_id, name FROM terms WHERE term_id = ?`)
	if err != nil {
		return err
	}
	a.v.Set("context.term_id", id)
	a.v.Set("context.course_year_id", 0)
	a.v.Set("context.section_id", 0)
	a.v.Set("context.assignment_id", 0)
	if err := a.v.WriteConfig(); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(a.out, "Using term: %s\n", display)
	return nil
}

func (a *App) UseCourseYear(name string) error {
	ctx := a.context()
	if ctx.TermID == 0 {
		return errors.New("set a term first")
	}
	id, display, err := a.lookupCourseYear(name, ctx.Year)
	if err != nil {
		return err
	}
	a.v.Set("context.year", courseYearLabel(display))
	a.v.Set("context.course_year_id", id)
	a.v.Set("context.section_id", 0)
	a.v.Set("context.assignment_id", 0)
	if err := a.v.WriteConfig(); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(a.out, "Using course: %s\n", baseCourseName(display))
	return nil
}

func (a *App) UseSection(name string) error {
	ctx := a.context()
	if ctx.CourseYearID == 0 {
		return errors.New("set a course first")
	}
	id, display, err := a.lookupSection(name, ctx.CourseYearID)
	if err != nil {
		return err
	}
	a.v.Set("context.section_id", id)
	if err := a.v.WriteConfig(); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(a.out, "Using section: %s\n", display)
	return nil
}

func (a *App) UseAssignment(title string) error {
	ctx := a.context()
	if ctx.TermID == 0 || ctx.CourseYearID == 0 {
		return errors.New("set year, term, and course first")
	}
	id, display, err := a.lookupAssignment(title, ctx.TermID, ctx.CourseYearID)
	if err != nil {
		return err
	}
	a.v.Set("context.assignment_id", id)
	if err := a.v.WriteConfig(); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(a.out, "Using assignment: %s\n", display)
	return nil
}

func (a *App) ClearScope(scope string) error {
	switch scope {
	case "year":
		a.v.Set("context.year", "")
		a.v.Set("context.course_year_id", 0)
		a.v.Set("context.section_id", 0)
		a.v.Set("context.assignment_id", 0)
	case "term":
		a.v.Set("context.term_id", 0)
		a.v.Set("context.course_year_id", 0)
		a.v.Set("context.section_id", 0)
		a.v.Set("context.assignment_id", 0)
	case "course-year":
		a.v.Set("context.course_year_id", 0)
		a.v.Set("context.section_id", 0)
		a.v.Set("context.assignment_id", 0)
	case "section":
		a.v.Set("context.section_id", 0)
	case "assignment":
		a.v.Set("context.assignment_id", 0)
	default:
		return fmt.Errorf("unknown scope: %s", scope)
	}
	if err := a.v.WriteConfig(); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(a.out, "Cleared %s context\n", scope)
	return nil
}

func (a *App) resolveContextNames(ctx Context) (string, string, string, string, string, error) {
	lookup := func(query string, id int) (string, error) {
		if id == 0 {
			return "", nil
		}
		var name string
		err := a.db.QueryRow(query, id).Scan(&name)
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return name, err
	}

	termName, err := lookup(`SELECT name FROM terms WHERE term_id = ?`, ctx.TermID)
	if err != nil {
		return "", "", "", "", "", err
	}
	courseName, err := lookup(`SELECT name FROM course_years WHERE course_year_id = ?`, ctx.CourseYearID)
	if err != nil {
		return "", "", "", "", "", err
	}
	sectionName, err := lookup(`SELECT name FROM sections WHERE section_id = ?`, ctx.SectionID)
	if err != nil {
		return "", "", "", "", "", err
	}
	assignmentName, err := lookup(`SELECT title FROM assignments WHERE assignment_id = ?`, ctx.AssignmentID)
	if err != nil {
		return "", "", "", "", "", err
	}
	yearName := ctx.Year
	if yearName == "" && courseName != "" {
		yearName = courseYearLabel(courseName)
	}
	return yearName, termName, baseCourseName(courseName), sectionName, assignmentName, nil
}

func (a *App) lookupNamedID(query, name string) (int, string, error) {
	var id int
	var display string
	err := a.db.QueryRow(query, name).Scan(&id, &display)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", fmt.Errorf("not found: %s", name)
	}
	return id, display, err
}

func (a *App) lookupContextID(label, value, byNameQuery, byIDQuery string) (int, string, error) {
	value = normalizeSpaces(value)
	if id, err := strconv.Atoi(value); err == nil {
		var display string
		if err := a.db.QueryRow(byIDQuery, id).Scan(&id, &display); errors.Is(err, sql.ErrNoRows) {
			return 0, "", fmt.Errorf("%s not found: %s", label, value)
		} else if err != nil {
			return 0, "", err
		}
		return id, display, nil
	}
	var id int
	var display string
	err := a.db.QueryRow(byNameQuery, value).Scan(&id, &display)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", fmt.Errorf("%s not found: %s", label, value)
	}
	return id, display, err
}

func (a *App) lookupCourseYear(value, year string) (int, string, error) {
	value = normalizeSpaces(value)
	if id, err := strconv.Atoi(value); err == nil {
		var name string
		err := a.db.QueryRow(`SELECT course_year_id, name FROM course_years WHERE course_year_id = ?`, id).Scan(&id, &name)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, "", fmt.Errorf("course not found: %s", value)
		}
		if err != nil {
			return 0, "", err
		}
		if year != "" && courseYearLabel(name) != year {
			return 0, "", fmt.Errorf("course %s is not in year %s", value, year)
		}
		return id, name, nil
	}
	query := `SELECT course_year_id, name FROM course_years WHERE lower(name)=lower(?)`
	args := []any{value}
	if year != "" {
		query = `SELECT course_year_id, name FROM course_years WHERE lower(name)=lower(?) AND lower(name) LIKE lower(?)`
		args = []any{value, "% " + year}
	}
	var id int
	var name string
	err := a.db.QueryRow(query, args...).Scan(&id, &name)
	if errors.Is(err, sql.ErrNoRows) {
		if year != "" {
			baseMatches, err := a.db.Query(`SELECT course_year_id, name FROM course_years WHERE lower(name) LIKE lower(?) ORDER BY course_year_id`, value+" %")
			if err == nil {
				defer baseMatches.Close()
				for baseMatches.Next() {
					if err := baseMatches.Scan(&id, &name); err != nil {
						break
					}
					if courseYearLabel(name) == year && strings.EqualFold(baseCourseName(name), value) {
						return id, name, nil
					}
				}
			}
		}
		return 0, "", fmt.Errorf("course not found: %s", value)
	}
	return id, name, err
}

func (a *App) lookupSection(value string, courseYearID int) (int, string, error) {
	value = normalizeSpaces(value)
	if id, err := strconv.Atoi(value); err == nil {
		var display string
		err := a.db.QueryRow(`SELECT section_id, name FROM sections WHERE course_year_id = ? AND section_id = ?`, courseYearID, id).Scan(&id, &display)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, "", fmt.Errorf("section not found: %s", value)
		}
		return id, display, err
	}
	var id int
	var display string
	err := a.db.QueryRow(`SELECT section_id, name FROM sections WHERE course_year_id = ? AND lower(name)=lower(?)`, courseYearID, value).Scan(&id, &display)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", fmt.Errorf("section not found: %s", value)
	}
	return id, display, err
}

func (a *App) lookupAssignment(value string, termID, courseYearID int) (int, string, error) {
	value = normalizeSpaces(value)
	if id, err := strconv.Atoi(value); err == nil {
		var display string
		err := a.db.QueryRow(`
			SELECT assignment_id, title
			FROM assignments
			WHERE term_id = ? AND course_year_id = ? AND assignment_id = ?`,
			termID, courseYearID, id,
		).Scan(&id, &display)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, "", fmt.Errorf("assignment not found: %s", value)
		}
		return id, display, err
	}
	var id int
	var display string
	err := a.db.QueryRow(`
		SELECT assignment_id, title
		FROM assignments
		WHERE term_id = ? AND course_year_id = ? AND lower(title)=lower(?)`,
		termID, courseYearID, value,
	).Scan(&id, &display)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", fmt.Errorf("assignment not found: %s", value)
	}
	return id, display, err
}

func (a *App) ensureStudentCommandContext() error {
	if _, _, err := a.autoSelectContextDefaults(); err != nil {
		return err
	}
	ctx := a.context()
	if ctx.TermID != 0 && ctx.CourseYearID != 0 {
		return nil
	}
	return a.studentContextHelpError()
}

func (a *App) autoSelectContextDefaults() (bool, []string, error) {
	updated := false
	var notes []string
	ctx := a.context()

	if ctx.CourseYearID == 0 {
		items, err := a.listCourseYearItems(ctx.Year)
		if err != nil {
			return false, nil, err
		}
		if len(items) == 1 {
			a.v.Set("context.course_year_id", items[0].ID)
			ctx.CourseYearID = items[0].ID
			updated = true
			notes = append(notes, fmt.Sprintf("Auto-selected course: %s", baseCourseName(items[0].Name)))
		}
	}

	if ctx.TermID == 0 {
		items, err := a.namedIDs(`SELECT term_id, name FROM terms ORDER BY term_id`)
		if err != nil {
			return false, nil, err
		}
		if len(items) == 1 {
			a.v.Set("context.term_id", items[0].ID)
			ctx.TermID = items[0].ID
			updated = true
			notes = append(notes, fmt.Sprintf("Auto-selected term: %s", items[0].Name))
		}
	}

	if ctx.SectionID == 0 && ctx.CourseYearID != 0 {
		items, err := a.namedIDsForQuery(`SELECT section_id, name FROM sections WHERE course_year_id = ? ORDER BY section_id`, ctx.CourseYearID)
		if err != nil {
			return false, nil, err
		}
		if len(items) == 1 {
			a.v.Set("context.section_id", items[0].ID)
			ctx.SectionID = items[0].ID
			updated = true
			notes = append(notes, fmt.Sprintf("Auto-selected section: %s", items[0].Name))
		}
	}

	if updated {
		if err := a.v.WriteConfig(); err != nil {
			return false, nil, err
		}
	}
	return updated, notes, nil
}

func (a *App) studentContextHelpError() error {
	var lines []string
	lines = append(lines, "students list needs year, term, and course context.")

	if years, err := a.availableYears(); err == nil && len(years) > 0 {
		lines = append(lines, "Available years: "+strings.Join(years, ", "))
		lines = append(lines, fmt.Sprintf(`Run: grades use year "%s"`, years[0]))
	}

	if terms, err := a.namedIDs(`SELECT term_id, name FROM terms ORDER BY term_id`); err == nil && len(terms) > 0 {
		lines = append(lines, "Available terms: "+joinNames(terms))
		if len(terms) > 0 {
			lines = append(lines, fmt.Sprintf(`Run: grades use term "%s"`, terms[0].Name))
		}
	}
	if courses, err := a.listCourseYearItems(a.context().Year); err == nil && len(courses) > 0 {
		lines = append(lines, "Available courses: "+joinCourseNames(courses))
		lines = append(lines, fmt.Sprintf(`Run: grades use course %d`, courses[0].ID))
		ctx := a.context()
		courseYearID := ctx.CourseYearID
		if courseYearID == 0 && len(courses) == 1 {
			courseYearID = courses[0].ID
		}
		if courseYearID != 0 {
			if sections, err := a.namedIDsForQuery(`SELECT section_id, name FROM sections WHERE course_year_id = ? ORDER BY section_id`, courseYearID); err == nil && len(sections) > 0 {
				lines = append(lines, "Available sections: "+joinNames(sections))
				lines = append(lines, "Tip: if section is not set, students list uses all sections in the current course.")
			}
		}
	}

	return errors.New(strings.Join(lines, "\n"))
}

func (a *App) availableYears() ([]string, error) {
	rows, err := a.db.Query(`SELECT name FROM course_years ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	var years []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		year := courseYearLabel(name)
		if year == "" || seen[year] {
			continue
		}
		seen[year] = true
		years = append(years, year)
	}
	return years, rows.Err()
}

func (a *App) AvailableYearsForCLI() ([]string, error) {
	return a.availableYears()
}

func (a *App) ListYears() error {
	years, err := a.availableYears()
	if err != nil {
		return err
	}
	for idx, year := range years {
		fmt.Fprintf(a.out, "%d\t%s\n", idx+1, year)
	}
	return nil
}

func pickYear(years []string, value string) (string, error) {
	value = normalizeSpaces(value)
	if id, err := strconv.Atoi(value); err == nil {
		if id < 1 || id > len(years) {
			return "", fmt.Errorf("year not found: %s", value)
		}
		return years[id-1], nil
	}
	for _, year := range years {
		if strings.EqualFold(year, value) {
			return year, nil
		}
	}
	return "", fmt.Errorf("year not found: %s", value)
}

func (a *App) listCourseYearItems(year string) ([]NamedID, error) {
	query := `SELECT course_year_id, name FROM course_years ORDER BY course_year_id`
	args := []any{}
	if year != "" {
		query = `SELECT course_year_id, name FROM course_years WHERE lower(name) LIKE lower(?) ORDER BY course_year_id`
		args = append(args, "% "+year)
	}
	rows, err := a.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNamedIDs(rows)
}

func courseYearLabel(name string) string {
	name = normalizeSpaces(name)
	parts := strings.Split(name, " ")
	if len(parts) == 0 {
		return ""
	}
	last := parts[len(parts)-1]
	if strings.Contains(last, "-") {
		return last
	}
	return ""
}

func baseCourseName(name string) string {
	name = normalizeSpaces(name)
	label := courseYearLabel(name)
	if label == "" {
		return name
	}
	return strings.TrimSpace(strings.TrimSuffix(name, label))
}

func joinCourseNames(items []NamedID) string {
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, fmt.Sprintf("%d %s", item.ID, baseCourseName(item.Name)))
	}
	return strings.Join(names, ", ")
}

func (a *App) namedIDs(query string) ([]NamedID, error) {
	rows, err := a.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNamedIDs(rows)
}

func (a *App) namedIDsForQuery(query string, args ...any) ([]NamedID, error) {
	rows, err := a.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanNamedIDs(rows)
}

func scanNamedIDs(rows *sql.Rows) ([]NamedID, error) {
	var items []NamedID
	for rows.Next() {
		var item NamedID
		if err := rows.Scan(&item.ID, &item.Name); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func joinNames(items []NamedID) string {
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.Name)
	}
	return strings.Join(names, ", ")
}
