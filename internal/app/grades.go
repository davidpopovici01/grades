package app

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func (a *App) EnterGradesInteractive(byLastName bool) error {
	ctx := a.context()
	if ctx.AssignmentID == 0 || ctx.TermID == 0 || ctx.CourseYearID == 0 {
		return errors.New("set year, term, course, and assignment first")
	}
	if err := a.ensureDefaultGradesForCurrentAssignment(); err != nil {
		return err
	}
	students, scope, err := a.studentsForList()
	if err != nil {
		return err
	}
	if len(students) == 0 {
		fmt.Fprintln(a.out, "No students enrolled.")
		return nil
	}
	if scope != "" {
		fmt.Fprintln(a.out, scope)
	}
	if byLastName {
		sortStudentsForLastNameEntry(students)
		return a.enterGradesByLastName(ctx.AssignmentID, students)
	}
	graded := map[int]bool{}
	history := []gradeHistory{}
	for {
		query, err := a.prompt("Search")
		if err != nil {
			if errors.Is(err, io.EOF) {
				fmt.Fprintln(a.out, "Finished grade entry.")
				return nil
			}
			return err
		}
		query = strings.TrimSpace(query)
		switch strings.ToLower(query) {
		case "", "done", "quit", "q", "exit":
			fmt.Fprintln(a.out, "Finished grade entry.")
			return nil
		case "undo", "u":
			if len(history) == 0 {
				fmt.Fprintln(a.out, "Nothing to undo.")
				continue
			}
			last := history[len(history)-1]
			history = history[:len(history)-1]
			if err := a.restoreGrade(ctx.AssignmentID, last.Student.ID, last.Prev); err != nil {
				return err
			}
			delete(graded, last.Student.ID)
			fmt.Fprintf(a.out, "Previous entry removed for %s %s.\n", last.Student.FirstName, last.Student.LastName)
			continue
		}

		student, err := matchStudent(students, graded, query)
		if err != nil {
			fmt.Fprintln(a.out, err.Error())
			continue
		}
		fmt.Fprintf(a.out, "Matched: %s %s\n", student.FirstName, student.LastName)
		var entry gradeEntry
		for {
			scoreRaw, err := a.prompt("Score")
			if err != nil {
				return err
			}
			entry, err = parseGradeInput(scoreRaw)
			if err != nil {
				fmt.Fprintln(a.out, retryMessage(err.Error()))
				continue
			}
			break
		}
		prev, err := a.currentGrade(ctx.AssignmentID, student.ID)
		if err != nil {
			return err
		}
		if err := a.saveGrade(ctx.AssignmentID, student.ID, entry, prev); err != nil {
			return err
		}
		graded[student.ID] = true
		history = append(history, gradeHistory{Student: student, Prev: prev})
		fmt.Fprintf(a.out, "Recorded %s\n", formatGradeValue(entry))
	}
}

func sortStudentsForLastNameEntry(students []Student) {
	sort.SliceStable(students, func(i, j int) bool {
		leftLast := strings.ToLower(students[i].LastName)
		rightLast := strings.ToLower(students[j].LastName)
		if leftLast != rightLast {
			return leftLast < rightLast
		}
		leftChinese := strings.ToLower(students[i].ChineseName)
		rightChinese := strings.ToLower(students[j].ChineseName)
		if leftChinese != rightChinese {
			if leftChinese == "" {
				return false
			}
			if rightChinese == "" {
				return true
			}
			return leftChinese < rightChinese
		}
		leftFirst := strings.ToLower(students[i].FirstName)
		rightFirst := strings.ToLower(students[j].FirstName)
		if leftFirst != rightFirst {
			return leftFirst < rightFirst
		}
		return students[i].ID < students[j].ID
	})
}

func (a *App) enterGradesByLastName(assignmentID int, students []Student) error {
	history := []gradeHistory{}
	index := 0
	for index < len(students) {
		student := students[index]
		scoreRaw, err := a.prompt(fmt.Sprintf("%s %s", student.FirstName, student.LastName))
		if err != nil {
			if errors.Is(err, io.EOF) {
				fmt.Fprintln(a.out, "Finished grade entry.")
				return nil
			}
			return err
		}
		scoreRaw = strings.TrimSpace(scoreRaw)
		switch strings.ToLower(scoreRaw) {
		case "done", "quit", "q", "exit":
			fmt.Fprintln(a.out, "Finished grade entry.")
			return nil
		case "undo", "u":
			if len(history) == 0 {
				fmt.Fprintln(a.out, "Nothing to undo.")
				continue
			}
			last := history[len(history)-1]
			history = history[:len(history)-1]
			if err := a.restoreGrade(assignmentID, last.Student.ID, last.Prev); err != nil {
				return err
			}
			index = max(index-1, 0)
			fmt.Fprintf(a.out, "Previous entry removed for %s %s.\n", last.Student.FirstName, last.Student.LastName)
			continue
		case "":
			index++
			continue
		}

		entry, err := parseGradeInput(scoreRaw)
		if err != nil {
			fmt.Fprintln(a.out, retryMessage(err.Error()))
			continue
		}
		prev, err := a.currentGrade(assignmentID, student.ID)
		if err != nil {
			return err
		}
		if err := a.saveGrade(assignmentID, student.ID, entry, prev); err != nil {
			return err
		}
		history = append(history, gradeHistory{Student: student, Prev: prev})
		fmt.Fprintf(a.out, "Recorded %s for %s %s\n", formatGradeValue(entry), student.FirstName, student.LastName)
		index++
	}
	fmt.Fprintln(a.out, "Finished grade entry.")
	return nil
}

func (a *App) ShowGrades() error {
	ctx := a.context()
	if ctx.AssignmentID == 0 || ctx.TermID == 0 || ctx.CourseYearID == 0 {
		return errors.New("set year, term, course, and assignment first")
	}
	var title string
	if err := a.db.QueryRow(`SELECT title FROM assignments WHERE assignment_id = ?`, ctx.AssignmentID).Scan(&title); err != nil {
		return err
	}
	fmt.Fprintln(a.out, title)
	fmt.Fprintln(a.out)
	if err := a.ensureDefaultGradesForCurrentAssignment(); err != nil {
		return err
	}
	records, scope, err := a.assignmentGradesForList()
	if err != nil {
		return err
	}
	if scope != "" {
		fmt.Fprintln(a.out, scope)
	}
	nameWidth := 0
	for _, record := range records {
		nameWidth = max(nameWidth, visibleWidth(record.Name))
	}
	for _, record := range records {
		fmt.Fprintf(a.out, "%s  %s\n", padVisibleRight(record.Name, nameWidth), displayGrade(record))
	}
	fmt.Fprintln(a.out)
	fmt.Fprintln(a.out, "M = Missing")
	fmt.Fprintln(a.out, "L = Late")
	fmt.Fprintln(a.out, "P = Pass")
	fmt.Fprintln(a.out, "R = Redo")
	return nil
}

func (a *App) ShowGradebook() error {
	ctx := a.context()
	if ctx.TermID == 0 || ctx.CourseYearID == 0 {
		return errors.New("set year, term, and course first")
	}
	assignments, err := a.assignmentsForContext()
	if err != nil {
		return err
	}
	students, scope, err := a.studentsForList()
	if err != nil {
		return err
	}
	grid, err := a.gradebookGrid(assignments, students)
	if err != nil {
		return err
	}
	if scope != "" {
		fmt.Fprintln(a.out, scope)
	}
	chunks := gradebookChunks(assignments, grid, gradebookMaxWidth(), 5)
	for chunkIdx, chunk := range chunks {
		if len(chunks) > 1 {
			fmt.Fprintf(a.out, "Assignments %d-%d of %d\n", chunk.Start+1, chunk.End, len(assignments))
		}
		renderGradebookChunk(a.out, chunk.Assignments, chunk.Rows)
		if chunkIdx < len(chunks)-1 {
			fmt.Fprintln(a.out)
		}
	}
	return nil
}

type gradebookChunk struct {
	Start       int
	End         int
	Assignments []Assignment
	Rows        []GradebookRow
}

func gradebookChunks(assignments []Assignment, grid []GradebookRow, maxWidth, maxColumns int) []gradebookChunk {
	if len(assignments) == 0 {
		return []gradebookChunk{{Start: 0, End: 0, Assignments: nil, Rows: grid}}
	}
	if maxWidth < 60 {
		maxWidth = 60
	}
	if maxColumns < 1 {
		maxColumns = 1
	}
	nameWidth := visibleWidth("Student")
	for _, row := range grid {
		nameWidth = max(nameWidth, visibleWidth(row.Name))
	}
	current := gradebookChunk{Start: 0}
	currentWidth := nameWidth
	var chunks []gradebookChunk
	for idx, assignment := range assignments {
		columnWidth := visibleWidth(assignment.Title)
		for _, row := range grid {
			columnWidth = max(columnWidth, visibleWidth(row.Values[idx]))
		}
		addedWidth := columnWidth + 2
		tooWide := len(current.Assignments) > 0 && currentWidth+addedWidth > maxWidth
		tooManyColumns := len(current.Assignments) >= maxColumns
		if tooWide || tooManyColumns {
			current.End = current.Start + len(current.Assignments)
			current.Rows = sliceGradebookRows(grid, current.Start, current.End)
			chunks = append(chunks, current)
			current = gradebookChunk{Start: idx}
			currentWidth = nameWidth
		}
		current.Assignments = append(current.Assignments, assignment)
		currentWidth += addedWidth
	}
	current.End = current.Start + len(current.Assignments)
	current.Rows = sliceGradebookRows(grid, current.Start, current.End)
	chunks = append(chunks, current)
	return chunks
}

func gradebookMaxWidth() int {
	raw := strings.TrimSpace(os.Getenv("COLUMNS"))
	if raw == "" {
		return 100
	}
	width, err := strconv.Atoi(raw)
	if err != nil || width <= 0 {
		return 100
	}
	return width
}

func sliceGradebookRows(rows []GradebookRow, start, end int) []GradebookRow {
	out := make([]GradebookRow, 0, len(rows))
	for _, row := range rows {
		sliced := GradebookRow{
			StudentID: row.StudentID,
			Name:      row.Name,
			Values:    append([]string(nil), row.Values[start:end]...),
		}
		out = append(out, sliced)
	}
	return out
}

func renderGradebookChunk(out io.Writer, assignments []Assignment, grid []GradebookRow) {
	headers := []string{"Student"}
	for _, assignment := range assignments {
		headers = append(headers, assignment.Title)
	}
	widths := make([]int, len(headers))
	for idx, header := range headers {
		widths[idx] = visibleWidth(header)
	}
	for _, row := range grid {
		widths[0] = max(widths[0], visibleWidth(row.Name))
		for idx, value := range row.Values {
			widths[idx+1] = max(widths[idx+1], visibleWidth(value))
		}
	}
	for idx, header := range headers {
		if idx > 0 {
			fmt.Fprint(out, "  ")
		}
		fmt.Fprint(out, padVisibleRight(header, widths[idx]))
	}
	fmt.Fprintln(out)
	for _, row := range grid {
		fmt.Fprint(out, padVisibleRight(row.Name, widths[0]))
		for idx, value := range row.Values {
			fmt.Fprint(out, "  ")
			fmt.Fprint(out, padVisibleRight(value, widths[idx+1]))
		}
		fmt.Fprintln(out)
	}
}

func (a *App) ShowOverview() error {
	ctx := a.context()
	if ctx.TermID == 0 || ctx.CourseYearID == 0 {
		return errors.New("set year, term, and course first")
	}
	students, _, err := a.studentsForList()
	if err != nil {
		return err
	}
	if len(students) == 0 {
		fmt.Fprintln(a.out, "No students found.")
		return nil
	}
	statuses, err := a.studentStatusGroups(students)
	if err != nil {
		return err
	}
	nameWidth := 0
	for _, student := range students {
		nameWidth = max(nameWidth, visibleWidth(student.FirstName+" "+student.LastName))
	}
	for _, student := range students {
		name := student.FirstName + " " + student.LastName
		status := statuses[student.ID]
		if len(status) == 0 {
			fmt.Fprintf(a.out, "%s  %s\n", padVisibleRight(name, nameWidth), colorGreen("OK"))
			continue
		}
		fmt.Fprintf(a.out, "%s  %s\n", padVisibleRight(name, nameWidth), colorRed(strings.Join(status, "  ")))
	}
	return nil
}

type studentOverview struct {
	Missing []string
	Late    []string
	Redo    []string
}

type redoAssignment struct {
	ID       int
	Title    string
	Category string
	Record   GradeRecord
}

func (a *App) studentStatusGroups(students []Student) (map[int][]string, error) {
	ctx := a.context()
	rows, err := a.db.Query(`
		SELECT DISTINCT students.student_pk, assignments.assignment_id, assignments.title, grades.score, COALESCE(grades.flags_bitmask, 0), assignments.max_points
		FROM assignments
		JOIN section_enrollments ON section_enrollments.term_id = assignments.term_id
		JOIN sections ON sections.section_id = section_enrollments.section_id
		JOIN students ON students.student_pk = section_enrollments.student_pk
		LEFT JOIN grades ON grades.assignment_id = assignments.assignment_id AND grades.student_pk = students.student_pk
		WHERE assignments.term_id = ? AND assignments.course_year_id = ? AND sections.course_year_id = assignments.course_year_id AND (? = 0 OR section_enrollments.section_id = ?)
		ORDER BY students.last_name, students.first_name, assignments.title`, ctx.TermID, ctx.CourseYearID, ctx.SectionID, ctx.SectionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	statuses := map[int]*studentOverview{}
	for rows.Next() {
		var studentID, assignmentID, flags, maxPoints int
		var title string
		var score sql.NullFloat64
		if err := rows.Scan(&studentID, &assignmentID, &title, &score, &flags, &maxPoints); err != nil {
			return nil, err
		}
		_ = assignmentID
		if _, ok := statuses[studentID]; !ok {
			statuses[studentID] = &studentOverview{}
		}
		record := GradeRecord{Score: score, Flags: flags, MaxPoints: maxPoints}
		switch {
		case flags&flagMissing != 0:
			statuses[studentID].Missing = append(statuses[studentID].Missing, title)
		case hasActiveRedo(record, sql.NullFloat64{Float64: 80, Valid: true}):
			statuses[studentID].Redo = append(statuses[studentID].Redo, title)
		case flags&flagLate != 0 && !score.Valid:
			statuses[studentID].Late = append(statuses[studentID].Late, title)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result := map[int][]string{}
	for _, student := range students {
		status := statuses[student.ID]
		if status == nil {
			continue
		}
		var parts []string
		if len(status.Late) > 0 {
			parts = append(parts, "Late: "+strings.Join(status.Late, ", "))
		}
		if len(status.Redo) > 0 {
			parts = append(parts, "Redo: "+strings.Join(status.Redo, ", "))
		}
		if len(status.Missing) > 0 {
			parts = append(parts, "Missing: "+strings.Join(status.Missing, ", "))
		}
		result[student.ID] = parts
	}
	return result, nil
}

func hasActiveRedo(record GradeRecord, passPercent sql.NullFloat64) bool {
	if record.Flags&flagLocked0 != 0 {
		return false
	}
	if record.Score.Valid && record.MaxPoints > 0 && passPercent.Valid && passPercent.Float64 > 0 &&
		(record.Score.Float64/float64(record.MaxPoints))*100 < passPercent.Float64 {
		return true
	}
	if record.Flags&flagRedo == 0 {
		return false
	}
	if !record.Score.Valid {
		return true
	}
	if record.MaxPoints <= 0 {
		return true
	}
	return false
}

func isPassingNumericGrade(record GradeRecord, passPercent sql.NullFloat64) bool {
	if record.Flags&(flagRedo|flagMissing|flagLocked0) != 0 || !record.Score.Valid || record.MaxPoints <= 0 {
		return false
	}
	if !passPercent.Valid || passPercent.Float64 <= 0 {
		return false
	}
	return (record.Score.Float64/float64(record.MaxPoints))*100 >= passPercent.Float64
}

func (a *App) ShowAssignmentStats() error {
	ctx := a.context()
	if ctx.AssignmentID == 0 {
		return errors.New("set an assignment first")
	}
	stats, err := a.statsForQuery(`SELECT score FROM grades WHERE assignment_id = ? AND score IS NOT NULL`, ctx.AssignmentID)
	if err != nil {
		return err
	}
	return printStats(a.out, stats)
}

func (a *App) ShowSectionStats() error {
	ctx := a.context()
	if ctx.SectionID == 0 || ctx.TermID == 0 || ctx.CourseYearID == 0 {
		return errors.New("set year, term, course, and section first")
	}
	stats, err := a.statsForQuery(`
		SELECT grades.score
		FROM grades
		JOIN assignments ON assignments.assignment_id = grades.assignment_id
		JOIN section_enrollments ON section_enrollments.student_pk = grades.student_pk
		WHERE assignments.term_id = ? AND assignments.course_year_id = ? AND section_enrollments.section_id = ? AND section_enrollments.term_id = ? AND grades.score IS NOT NULL`,
		ctx.TermID, ctx.CourseYearID, ctx.SectionID, ctx.TermID)
	if err != nil {
		return err
	}
	return printStats(a.out, stats)
}

func (a *App) ShowStudentStats(studentID string) error {
	ctx := a.context()
	if ctx.TermID == 0 || ctx.CourseYearID == 0 {
		return errors.New("set year, term, and course first")
	}
	id, err := strconv.Atoi(strings.TrimSpace(studentID))
	if err != nil {
		return fmt.Errorf("invalid student id: %s", studentID)
	}
	stats, err := a.statsForQuery(`
		SELECT grades.score
		FROM grades
		JOIN assignments ON assignments.assignment_id = grades.assignment_id
		WHERE assignments.term_id = ? AND assignments.course_year_id = ? AND grades.student_pk = ? AND grades.score IS NOT NULL`,
		ctx.TermID, ctx.CourseYearID, id)
	if err != nil {
		return err
	}
	return printStats(a.out, stats)
}

func (a *App) assignmentGradesForList() ([]GradeRecord, string, error) {
	students, scope, err := a.studentsForList()
	if err != nil {
		return nil, "", err
	}
	records := make([]GradeRecord, 0, len(students))
	for _, student := range students {
		record, err := a.currentGrade(a.context().AssignmentID, student.ID)
		if err != nil {
			return nil, "", err
		}
		if record == nil {
			record = &GradeRecord{StudentID: student.ID, Name: student.FirstName + " " + student.LastName}
		}
		records = append(records, *record)
	}
	return records, scope, nil
}

func (a *App) gradebookGrid(assignments []Assignment, students []Student) ([]GradebookRow, error) {
	values := map[int]map[int]string{}
	rows, err := a.db.Query(`
		SELECT grades.assignment_id,
		       grades.student_pk,
		       grades.score,
		       grades.flags_bitmask,
		       assignments.max_points,
		       COALESCE(grades.redo_count, 0),
		       COALESCE(assignments.pass_percent, category_grading_policies.default_pass_percent, 80)
		FROM grades
		JOIN assignments ON assignments.assignment_id = grades.assignment_id
		LEFT JOIN category_grading_policies
		  ON category_grading_policies.course_year_id = assignments.course_year_id
		 AND category_grading_policies.term_id = assignments.term_id
		 AND category_grading_policies.category_id = assignments.category_id
		WHERE grades.assignment_id IN (
			SELECT assignment_id FROM assignments WHERE term_id = ? AND course_year_id = ?
		)`, a.context().TermID, a.context().CourseYearID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var assignmentID, studentID, flags, maxPoints, redoCount int
		var score sql.NullFloat64
		var passPercent sql.NullFloat64
		if err := rows.Scan(&assignmentID, &studentID, &score, &flags, &maxPoints, &redoCount, &passPercent); err != nil {
			return nil, err
		}
		if _, ok := values[studentID]; !ok {
			values[studentID] = map[int]string{}
		}
		values[studentID][assignmentID] = displayGradebookCell(GradeRecord{Score: score, Flags: flags, MaxPoints: maxPoints, RedoCount: redoCount, PassPercent: passPercent})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var grid []GradebookRow
	for _, student := range students {
		row := GradebookRow{Name: student.FirstName + " " + student.LastName, StudentID: student.ID, Values: make([]string, len(assignments))}
		for idx, assignment := range assignments {
			row.Values[idx] = values[student.ID][assignment.ID]
		}
		grid = append(grid, row)
	}
	return grid, nil
}

func parseGradeInput(raw string) (gradeEntry, error) {
	raw = strings.ToLower(strings.TrimSpace(raw))
	switch raw {
	case "m":
		score := 0.0
		return gradeEntry{Score: &score, Flags: flagMissing}, nil
	case "p", "pass":
		return gradeEntry{Flags: flagPass}, nil
	case "r", "f", "fail":
		return gradeEntry{Flags: flagRedo}, nil
	case "x", "cheat", "plag", "plagiarism":
		score := 0.0
		return gradeEntry{Score: &score, Flags: flagLocked0}, nil
	case "":
		return gradeEntry{}, errors.New("score cannot be empty")
	}
	flags := 0
	for len(raw) > 0 {
		last := raw[len(raw)-1]
		switch last {
		case 'l':
			flags |= flagLate
			raw = raw[:len(raw)-1]
		case 'r':
			flags |= flagRedo
			raw = raw[:len(raw)-1]
		default:
			goto parseScore
		}
	}

parseScore:
	if raw == "" {
		if flags != 0 {
			return gradeEntry{Flags: flags}, nil
		}
		return gradeEntry{}, errors.New("score cannot be empty")
	}
	score, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return gradeEntry{}, fmt.Errorf("invalid score: %s", raw)
	}
	return gradeEntry{Score: &score, Flags: flags}, nil
}

func matchStudent(students []Student, graded map[int]bool, query string) (Student, error) {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return Student{}, fmt.Errorf("no student matched %q", query)
	}
	tokens := strings.Fields(query)
	var matches []Student
	for _, student := range students {
		if graded[student.ID] {
			continue
		}
		if studentMatchesQuery(student, query, tokens) {
			matches = append(matches, student)
		}
	}
	switch len(matches) {
	case 0:
		return Student{}, fmt.Errorf("no student matched %q", query)
	case 1:
		return matches[0], nil
	default:
		return Student{}, fmt.Errorf("multiple students matched %q", query)
	}
}

func studentMatchesQuery(student Student, query string, tokens []string) bool {
	first := strings.ToLower(student.FirstName)
	last := strings.ToLower(student.LastName)
	chinese := strings.ToLower(student.ChineseName)
	initials := ""
	if student.FirstName != "" && student.LastName != "" {
		initials = strings.ToLower(string(student.FirstName[0]) + string(student.LastName[0]))
	}
	fullName := strings.TrimSpace(first + " " + last)
	reversedName := strings.TrimSpace(last + " " + first)

	if strings.HasPrefix(first, query) || strings.HasPrefix(last, query) || strings.HasPrefix(initials, query) ||
		(chinese != "" && strings.HasPrefix(chinese, query)) || strings.HasPrefix(fullName, query) || strings.HasPrefix(reversedName, query) {
		return true
	}
	if len(tokens) <= 1 {
		return false
	}
	for _, token := range tokens {
		if !(strings.HasPrefix(first, token) ||
			strings.HasPrefix(last, token) ||
			(chinese != "" && strings.HasPrefix(chinese, token)) ||
			strings.HasPrefix(initials, token)) {
			return false
		}
	}
	return true
}

func (a *App) currentGrade(assignmentID, studentID int) (*GradeRecord, error) {
	var record GradeRecord
	err := a.db.QueryRow(`
		SELECT grades.student_pk, students.first_name || ' ' || students.last_name, grades.score, grades.flags_bitmask, assignments.max_points, COALESCE(grades.redo_count, 0)
		FROM grades
		JOIN students ON students.student_pk = grades.student_pk
		JOIN assignments ON assignments.assignment_id = grades.assignment_id
		WHERE grades.assignment_id = ? AND grades.student_pk = ?`, assignmentID, studentID).
		Scan(&record.StudentID, &record.Name, &record.Score, &record.Flags, &record.MaxPoints, &record.RedoCount)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func (a *App) saveGrade(assignmentID, studentID int, entry gradeEntry, prev *GradeRecord) error {
	var maxPoints float64
	if err := a.db.QueryRow(`SELECT max_points FROM assignments WHERE assignment_id = ?`, assignmentID).Scan(&maxPoints); err != nil {
		return err
	}
	score := sql.NullFloat64{}
	if entry.Flags&flagPass != 0 {
		score = sql.NullFloat64{Float64: maxPoints, Valid: true}
	} else if entry.Score != nil {
		score = sql.NullFloat64{Float64: *entry.Score, Valid: true}
	}
	action := "INSERT"
	var oldScore any
	var oldFlags any
	redoCount := 0
	if prev != nil {
		action = "UPDATE"
		oldFlags = prev.Flags
		redoCount = prev.RedoCount
		if prev.Score.Valid {
			oldScore = prev.Score.Float64
		}
	}
	if entry.Flags&flagRedo != 0 {
		redoCount++
	}
	if prev != nil && prev.Flags&flagLocked0 != 0 {
		return errors.New("this grade is marked cheat and cannot be changed; use clear-cheat first")
	}
	if prev != nil && prev.Flags&flagLate != 0 && entry.Flags&flagLate == 0 {
		entry.Flags |= flagLate
	}
	if prev != nil && prev.Flags&flagRedo != 0 && entry.Flags&flagRedo == 0 {
		entry.Flags |= flagRedo
	}
	if score.Valid && entry.Flags&(flagMissing|flagPass|flagLocked0) == 0 && maxPoints > 0 && score.Float64/maxPoints < 0.8 {
		entry.Flags |= flagRedo
	}
	_, err := a.db.Exec(`
		INSERT INTO grades(assignment_id, student_pk, score, flags_bitmask, redo_count)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(assignment_id, student_pk) DO UPDATE SET score = excluded.score, flags_bitmask = excluded.flags_bitmask, redo_count = excluded.redo_count, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')`,
		assignmentID, studentID, score, entry.Flags, redoCount)
	if err != nil {
		return err
	}
	var newScore any
	if score.Valid {
		newScore = score.Float64
	}
	_, err = a.db.Exec(`
		INSERT INTO grade_audit(assignment_id, student_pk, action, old_score, new_score, old_flags, new_flags)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		assignmentID, studentID, action, oldScore, newScore, oldFlags, entry.Flags)
	return err
}

func (a *App) restoreGrade(assignmentID, studentID int, prev *GradeRecord) error {
	if prev == nil {
		_, err := a.db.Exec(`DELETE FROM grades WHERE assignment_id = ? AND student_pk = ?`, assignmentID, studentID)
		return err
	}
	_, err := a.db.Exec(`
		INSERT INTO grades(assignment_id, student_pk, score, flags_bitmask, redo_count)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(assignment_id, student_pk) DO UPDATE SET score = excluded.score, flags_bitmask = excluded.flags_bitmask, redo_count = excluded.redo_count, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')`,
		assignmentID, studentID, prev.Score, prev.Flags, prev.RedoCount)
	return err
}

func (a *App) ClearLate(studentID string) error {
	return a.clearGradeFlag(studentID, flagLate, "late")
}

func (a *App) ClearRedo(studentID string) error {
	return a.clearGradeFlag(studentID, flagRedo, "redo")
}

func (a *App) ClearCheat(studentID string) error {
	return a.clearGradeFlag(studentID, flagLocked0, "cheat")
}

func (a *App) PassStudent(studentID string) error {
	return a.applyGradeEntryToStudent(studentID, gradeEntry{Flags: flagPass}, "pass")
}

func (a *App) ListStudentRedo(studentID string) error {
	student, assignments, scope, err := a.redoAssignmentsForStudent(studentID)
	if err != nil {
		return err
	}
	if scope != "" {
		fmt.Fprintln(a.out, scope)
	}
	fmt.Fprintf(a.out, "Redo assignments for %s %s\n", student.FirstName, student.LastName)
	for idx, assignment := range assignments {
		fmt.Fprintf(a.out, "%d\t%s\t%s\t%s\n", idx+1, assignment.Title, assignment.Category, displayGradePlain(assignment.Record))
	}
	return nil
}

func (a *App) PassStudentRedo(studentID string) error {
	student, assignments, scope, err := a.redoAssignmentsForStudent(studentID)
	if err != nil {
		return err
	}
	if scope != "" {
		fmt.Fprintln(a.out, scope)
	}

	assignment := assignments[0]
	if len(assignments) > 1 {
		fmt.Fprintf(a.out, "Redo assignments for %s %s\n", student.FirstName, student.LastName)
		for idx, item := range assignments {
			fmt.Fprintf(a.out, "%d\t%s\t%s\t%s\n", idx+1, item.Title, item.Category, displayGradePlain(item.Record))
		}
		choice, err := a.promptRedoAssignmentChoice(len(assignments))
		if err != nil {
			return err
		}
		assignment = assignments[choice-1]
	}

	prev, err := a.currentGrade(assignment.ID, student.ID)
	if err != nil {
		return err
	}
	if err := a.saveGrade(assignment.ID, student.ID, gradeEntry{Flags: flagPass}, prev); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Recorded PASS for %s %s on %s\n", student.FirstName, student.LastName, assignment.Title)
	return nil
}

func (a *App) FillPass() error {
	ctx := a.context()
	if ctx.AssignmentID == 0 || ctx.TermID == 0 || ctx.CourseYearID == 0 {
		return errors.New("set year, term, course, and assignment first")
	}
	students, _, err := a.studentsForList()
	if err != nil {
		return err
	}
	count := 0
	for _, student := range students {
		prev, err := a.currentGrade(ctx.AssignmentID, student.ID)
		if err != nil {
			return err
		}
		if prev != nil {
			if prev.Flags != flagMissing && (prev.Score.Valid || prev.Flags != 0) {
				continue
			}
		}
		if err := a.saveGrade(ctx.AssignmentID, student.ID, gradeEntry{Flags: flagPass}, prev); err != nil {
			return err
		}
		count++
	}
	fmt.Fprintf(a.out, "Filled %d blank/missing grade(s) with PASS\n", count)
	return nil
}

func (a *App) applyGradeEntryToStudent(studentID string, entry gradeEntry, label string) error {
	ctx := a.context()
	if ctx.AssignmentID == 0 || ctx.TermID == 0 || ctx.CourseYearID == 0 {
		return errors.New("set year, term, course, and assignment first")
	}
	if strings.TrimSpace(studentID) == "" {
		var err error
		studentID, err = a.prompt("Student")
		if err != nil {
			return err
		}
	}
	if err := a.ensureDefaultGradesForCurrentAssignment(); err != nil {
		return err
	}
	student, err := a.resolveStudentReference(studentID)
	if err != nil {
		return err
	}
	prev, err := a.currentGrade(ctx.AssignmentID, student.ID)
	if err != nil {
		return err
	}
	if err := a.saveGrade(ctx.AssignmentID, student.ID, entry, prev); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Recorded %s for %s %s\n", strings.ToUpper(label), student.FirstName, student.LastName)
	return nil
}

func (a *App) redoAssignmentsForStudent(studentID string) (Student, []redoAssignment, string, error) {
	ctx := a.context()
	if ctx.TermID == 0 || ctx.CourseYearID == 0 {
		return Student{}, nil, "", errors.New("set year, term, and course first")
	}
	if strings.TrimSpace(studentID) == "" {
		var err error
		studentID, err = a.prompt("Student")
		if err != nil {
			return Student{}, nil, "", err
		}
	}
	student, err := a.resolveStudentReference(studentID)
	if err != nil {
		return Student{}, nil, "", err
	}
	scope := ""
	if ctx.SectionID == 0 {
		scope = "Using all sections in the current course."
	}

	rows, err := a.db.Query(`
		SELECT assignments.assignment_id,
		       assignments.title,
		       categories.name,
		       grades.score,
		       COALESCE(grades.flags_bitmask, 0),
		       assignments.max_points,
		       COALESCE(grades.redo_count, 0),
		       COALESCE(assignments.pass_percent, category_grading_policies.default_pass_percent, 80)
		FROM assignments
		JOIN categories ON categories.category_id = assignments.category_id
		LEFT JOIN grades ON grades.assignment_id = assignments.assignment_id AND grades.student_pk = ?
		LEFT JOIN category_grading_policies
		  ON category_grading_policies.course_year_id = assignments.course_year_id
		 AND category_grading_policies.term_id = assignments.term_id
		 AND category_grading_policies.category_id = assignments.category_id
		WHERE assignments.course_year_id = ? AND assignments.term_id = ?
		ORDER BY assignments.assignment_id`, student.ID, ctx.CourseYearID, ctx.TermID)
	if err != nil {
		return Student{}, nil, "", err
	}
	defer rows.Close()

	var assignments []redoAssignment
	for rows.Next() {
		var item redoAssignment
		var passPercent sql.NullFloat64
		if err := rows.Scan(&item.ID, &item.Title, &item.Category, &item.Record.Score, &item.Record.Flags, &item.Record.MaxPoints, &item.Record.RedoCount, &passPercent); err != nil {
			return Student{}, nil, "", err
		}
		item.Record.PassPercent = passPercent
		if hasActiveRedo(item.Record, passPercent) {
			assignments = append(assignments, item)
		}
	}
	if err := rows.Err(); err != nil {
		return Student{}, nil, "", err
	}
	if len(assignments) == 0 {
		return Student{}, nil, "", fmt.Errorf("%s %s has no active redo assignments", student.FirstName, student.LastName)
	}
	return student, assignments, scope, nil
}

func (a *App) promptRedoAssignmentChoice(maxChoice int) (int, error) {
	for {
		raw, err := a.prompt("Choose assignment")
		if err != nil {
			return 0, err
		}
		choice, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil || choice < 1 || choice > maxChoice {
			fmt.Fprintln(a.out, retryMessage(fmt.Sprintf("enter a number between 1 and %d", maxChoice)))
			continue
		}
		return choice, nil
	}
}

func (a *App) clearGradeFlag(studentID string, flag int, label string) error {
	ctx := a.context()
	if ctx.AssignmentID == 0 || ctx.TermID == 0 || ctx.CourseYearID == 0 {
		return errors.New("set year, term, course, and assignment first")
	}
	if strings.TrimSpace(studentID) == "" {
		var err error
		studentID, err = a.prompt("Student")
		if err != nil {
			return err
		}
	}
	student, err := a.resolveStudentReference(studentID)
	if err != nil {
		return err
	}
	prev, err := a.currentGrade(ctx.AssignmentID, student.ID)
	if err != nil {
		return err
	}
	if prev == nil {
		return fmt.Errorf("%s %s has no grade for the current assignment", student.FirstName, student.LastName)
	}
	if prev.Flags&flag == 0 {
		return fmt.Errorf("%s %s is not marked %s for the current assignment", student.FirstName, student.LastName, label)
	}

	newFlags := prev.Flags &^ flag
	action := "UPDATE"
	if !prev.Score.Valid && newFlags == 0 {
		action = "DELETE"
		if _, err := a.db.Exec(`DELETE FROM grades WHERE assignment_id = ? AND student_pk = ?`, ctx.AssignmentID, student.ID); err != nil {
			return err
		}
	} else {
		if _, err := a.db.Exec(`
			UPDATE grades
			SET flags_bitmask = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')
			WHERE assignment_id = ? AND student_pk = ?`,
			newFlags, ctx.AssignmentID, student.ID); err != nil {
			return err
		}
	}

	var oldScore any
	if prev.Score.Valid {
		oldScore = prev.Score.Float64
	}
	var newScore any
	if prev.Score.Valid && action != "DELETE" {
		newScore = prev.Score.Float64
	}
	var newFlagsValue any
	if action != "DELETE" {
		newFlagsValue = newFlags
	}
	_, err = a.db.Exec(`
		INSERT INTO grade_audit(assignment_id, student_pk, action, old_score, new_score, old_flags, new_flags, reason)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		ctx.AssignmentID, student.ID, action, oldScore, newScore, prev.Flags, newFlagsValue, "clear-"+label)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Cleared %s for %s %s\n", label, student.FirstName, student.LastName)
	return nil
}

func (a *App) resolveStudentReference(value string) (Student, error) {
	value = strings.TrimSpace(value)
	students, _, err := a.studentsForList()
	if err != nil {
		return Student{}, err
	}
	if id, err := strconv.Atoi(value); err == nil {
		for _, student := range students {
			if student.ID == id {
				return student, nil
			}
		}
		return Student{}, fmt.Errorf("student not found: %s", value)
	}
	return matchStudent(students, map[int]bool{}, value)
}

func formatGradeValue(entry gradeEntry) string {
	if entry.Flags&flagMissing != 0 {
		return "M"
	}
	if entry.Flags&flagLocked0 != 0 {
		return "CHEAT"
	}
	if entry.Flags&flagPass != 0 {
		if entry.Flags&flagLate != 0 {
			return "PL"
		}
		return "P"
	}
	if entry.Flags&flagRedo != 0 {
		if entry.Flags&flagLate != 0 {
			return "RL"
		}
		return "R"
	}
	if entry.Score == nil {
		return ""
	}
	if entry.Flags&flagLate != 0 {
		return fmt.Sprintf("%.0fL", *entry.Score)
	}
	return fmt.Sprintf("%.0f", *entry.Score)
}

func displayGradePlain(record GradeRecord) string {
	switch {
	case record.Flags&flagMissing != 0:
		if record.Flags&flagLate != 0 {
			return "M/L"
		}
		return "M"
	case record.Flags&flagLocked0 != 0:
		return "0 (cheat)"
	case record.Flags&flagPass != 0:
		if record.Flags&flagLate != 0 {
			return "P (late)"
		}
		return "P"
	case record.Flags&flagRedo != 0:
		if record.Score.Valid {
			value := fmt.Sprintf("%.0f", record.Score.Float64)
			if record.Flags&flagLate != 0 {
				value += "L"
			}
			return value + " (redo)"
		}
		if record.Flags&flagLate != 0 {
			return "R (redo, late)"
		}
		return "R (redo)"
	case !record.Score.Valid && record.Flags&flagLate != 0:
		return "L"
	case !record.Score.Valid:
		return ""
	}
	value := fmt.Sprintf("%.0f", record.Score.Float64)
	if record.Flags&flagLate != 0 {
		value += "L"
	}
	if record.MaxPoints > 0 && record.Score.Float64/float64(record.MaxPoints) < 0.8 {
		return value + " (redo)"
	}
	return value
}

func visibleWidth(s string) int {
	return len(ansiPattern.ReplaceAllString(s, ""))
}

func padVisibleRight(s string, width int) string {
	padding := width - visibleWidth(s)
	if padding <= 0 {
		return s
	}
	return s + strings.Repeat(" ", padding)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func displayGrade(record GradeRecord) string {
	switch {
	case record.Flags&flagMissing != 0:
		if record.Flags&flagLate != 0 {
			return colorRed("M/L")
		}
		return colorRed("M")
	case record.Flags&flagLocked0 != 0:
		return colorBlackOnWhite("0")
	case record.Flags&flagPass != 0:
		if record.Flags&flagLate != 0 {
			return colorGreen("P (late)")
		}
		return colorGreen("P")
	case record.Flags&flagRedo != 0:
		if record.Score.Valid {
			value := fmt.Sprintf("%.0f", record.Score.Float64)
			if record.Flags&flagLate != 0 {
				value += "L"
			}
			return colorRed(value + " (redo)")
		}
		if record.Flags&flagLate != 0 {
			return colorRed("R (redo, late)")
		}
		return colorRed("R (redo)")
	case !record.Score.Valid && record.Flags&flagLate != 0:
		return colorRed("L")
	case !record.Score.Valid:
		return ""
	}
	value := fmt.Sprintf("%.0f", record.Score.Float64)
	if record.Flags&flagLate != 0 {
		value += "L"
	}
	if record.MaxPoints > 0 && record.Score.Float64/float64(record.MaxPoints) < 0.8 {
		return colorRed(value + " (redo)")
	}
	return value
}

func displayGradebookCell(record GradeRecord) string {
	if record.Flags&flagLocked0 != 0 {
		return colorBlackOnWhite("0")
	}
	base := displayGrade(record)
	if record.Flags&flagRedo != 0 || record.Flags&flagMissing != 0 || !record.Score.Valid || record.MaxPoints <= 0 {
		return base
	}
	if isPassingNumericGrade(record, record.PassPercent) {
		return colorGreen(base)
	}
	return base
}
