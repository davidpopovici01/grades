package app

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var errExportNotConfirmed = errors.New("export not confirmed")

func (a *App) ImportStudents(file string) error {
	ctx := a.context()
	if ctx.SectionID == 0 || ctx.TermID == 0 {
		return errors.New("set term and section first")
	}
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	reader := csv.NewReader(f)
	reader.FieldsPerRecord = -1
	rows, err := reader.ReadAll()
	if err != nil {
		return err
	}
	if len(rows) < 2 {
		return errors.New("student import file is empty")
	}
	headers := map[string]int{}
	for idx, header := range rows[0] {
		headers[strings.ToLower(strings.TrimSpace(header))] = idx
	}
	for _, header := range []string{"first_name", "last_name"} {
		if _, ok := headers[header]; !ok {
			return fmt.Errorf("missing column: %s", header)
		}
	}
	count := 0
	for _, row := range rows[1:] {
		studentID := ""
		if idx, ok := headers["student_id"]; ok && idx < len(row) {
			studentID = row[idx]
		}
		chineseName := ""
		if idx, ok := headers["chinese_name"]; ok && idx < len(row) {
			chineseName = row[idx]
		}
		student, err := a.upsertStudent(row[headers["first_name"]], row[headers["last_name"]], chineseName, studentID)
		if err != nil {
			return err
		}
		if err := a.enrollStudent(ctx.SectionID, ctx.TermID, student.ID); err != nil {
			return err
		}
		count++
	}
	fmt.Fprintf(a.out, "Imported %d students\n", count)
	return nil
}

func (a *App) RunImportWizard() error {
	defaultFile := filepath.Join(a.homeDir, "roster_setup.csv")
	defaultExists := true
	if _, err := os.Stat(defaultFile); os.IsNotExist(err) {
		defaultExists = false
	} else if err != nil {
		return err
	}

	action, err := a.promptFileImportAction("roster", defaultExists)
	if err != nil {
		return err
	}

	switch action {
	case "import-default":
		if !defaultExists {
			fmt.Fprintf(a.out, "Default roster file not found: %s\n", defaultFile)
			return a.WriteRosterSetupCSV(defaultFile)
		}
		return a.ImportRosterWithGuidance(defaultFile)
	case "create-default":
		if !defaultExists {
			fmt.Fprintf(a.out, "Default roster file not found: %s\n", defaultFile)
		}
		return a.WriteRosterSetupCSV(defaultFile)
	default:
		file, err := a.promptPath("Roster CSV file path")
		if err != nil {
			return err
		}
		if file == "" {
			fmt.Fprintf(a.out, "Default roster file not found: %s\n", defaultFile)
			return a.WriteRosterSetupCSV(defaultFile)
		}
		if _, err := os.Stat(file); err == nil {
			return a.ImportRosterWithGuidance(file)
		}
		fmt.Fprintf(a.out, "Roster file not found: %s\n", file)
		return a.WriteRosterSetupCSV(file)
	}
}

func (a *App) ImportRosterWithGuidance(file string) error {
	if err := a.ImportRoster(file); err != nil {
		if openErr := openFile(file); openErr == nil {
			fmt.Fprintf(a.out, "Import failed. Opened %s so you can fix it.\n", file)
		} else {
			fmt.Fprintf(a.out, "Import failed. Could not open %s automatically: %v\n", file, openErr)
		}
		return fmt.Errorf("%w\nFix the CSV and run `grades import` again", err)
	}
	return nil
}

func (a *App) ImportRoster(file string) error {
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()

	reader := csv.NewReader(f)
	reader.FieldsPerRecord = -1
	rows, err := reader.ReadAll()
	if err != nil {
		return err
	}
	if len(rows) < 2 {
		return errors.New("roster import file is empty")
	}

	headers := map[string]int{}
	for idx, header := range rows[0] {
		headers[normalizedCSVHeader(header)] = idx
	}
	for _, header := range []string{"year", "course", "section", "first_name", "last_name"} {
		if _, ok := headers[header]; !ok {
			return fmt.Errorf("missing column: %s", header)
		}
	}

	count := 0
	for rowIndex, row := range rows[1:] {
		if isSkippedImportRow(row) {
			continue
		}
		record, err := rosterRecordFromRow(headers, row)
		if err != nil {
			return fmt.Errorf("row %d: %w", rowIndex+2, err)
		}

		courseID, err := a.lookupCourseID(record.Course)
		if err != nil {
			return err
		}
		courseYearID, err := a.lookupCourseYearID(courseID, record.Course, record.Year)
		if err != nil {
			return err
		}
		termIDs, err := a.courseYearTermIDs(courseYearID)
		if err != nil {
			return err
		}
		sectionID, err := a.lookupSectionID(courseYearID, record.Section)
		if err != nil {
			return err
		}
		student, err := a.upsertStudent(record.FirstName, record.LastName, record.ChineseName, record.StudentID)
		if err != nil {
			return err
		}
		for _, termID := range termIDs {
			if err := a.enrollStudent(sectionID, termID, student.ID); err != nil {
				return err
			}
		}
		count++
	}

	fmt.Fprintf(a.out, "Imported %d roster row(s)\n", count)
	return nil
}

func (a *App) WriteRosterSetupCSV(file string) error {
	if strings.TrimSpace(file) == "" {
		file = filepath.Join(a.homeDir, "roster_setup.csv")
	}

	existingRows, err := a.rosterSetupRows()
	if err != nil {
		return err
	}

	f, err := os.Create(file)
	if err != nil {
		return rosterTemplateWriteError(file, err)
	}
	defer f.Close()

	writer := csv.NewWriter(f)
	rows := [][]string{
		{"year", "course", "section", "student_id", "first_name", "last_name", "chinese_name"},
		{"# example row - importer ignores rows whose first cell starts with #", "APCSA", "12A", "3001", "Alice", "Brown", "Ai Li Si"},
	}
	rows = append(rows, existingRows...)
	for _, row := range rows {
		if err := writer.Write(row); err != nil {
			return err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return err
	}

	if err := openFile(file); err != nil {
		fmt.Fprintf(a.out, "Created roster setup CSV: %s\n", file)
		fmt.Fprintf(a.out, "Could not open it automatically: %v\n", err)
		return nil
	}

	fmt.Fprintf(a.out, "Created roster setup CSV: %s\n", file)
	fmt.Fprintln(a.out, "Opened roster setup CSV.")
	return nil
}

func rosterTemplateWriteError(file string, err error) error {
	if isFileLockedError(err) {
		return fmt.Errorf("could not overwrite %s because it is open in another program\nClose the CSV file and run `grades import` again", file)
	}
	return err
}

func isFileLockedError(err error) bool {
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "used by another process") ||
		strings.Contains(text, "sharing violation") ||
		strings.Contains(text, "lock violation")
}

func (a *App) rosterSetupRows() ([][]string, error) {
	rows, err := a.db.Query(`
		SELECT DISTINCT course_years.name, courses.name, sections.name,
		       COALESCE(students.school_student_id, ''), students.first_name, students.last_name, COALESCE(students.chinese_name, '')
		FROM section_enrollments
		JOIN sections ON sections.section_id = section_enrollments.section_id
		JOIN course_years ON course_years.course_year_id = sections.course_year_id
		JOIN courses ON courses.course_id = course_years.course_id
		JOIN students ON students.student_pk = section_enrollments.student_pk
		ORDER BY course_years.name, sections.name, students.last_name, students.first_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out [][]string
	for rows.Next() {
		var courseYearName string
		var courseName string
		var sectionName string
		var studentID string
		var firstName string
		var lastName string
		var chineseName string
		if err := rows.Scan(&courseYearName, &courseName, &sectionName, &studentID, &firstName, &lastName, &chineseName); err != nil {
			return nil, err
		}
		out = append(out, []string{
			courseYearLabel(courseYearName),
			courseName,
			sectionName,
			studentID,
			firstName,
			lastName,
			chineseName,
		})
	}
	return out, rows.Err()
}

type assignmentExportMeta struct {
	Title      string
	Category   string
	CourseName string
	DueDate    string
	DueDateRaw string
	MaxPoints  float64
}

type assignmentExportRow struct {
	PowerSchoolNum string
	StudentName    string
	Score          string
}

type assignmentExportDocument struct {
	Meta       assignmentExportMeta
	Rows       []assignmentExportRow
	CSV        []byte
	StableHash string
}

type pendingAssignmentExport struct {
	AssignmentID int
	Title        string
	NeedsExport  bool
	WasExported  bool
}

func (a *App) ExportGrades(file string) error {
	ctx := a.context()
	if ctx.AssignmentID == 0 || ctx.TermID == 0 || ctx.CourseYearID == 0 {
		return errors.New("set year, term, course, and assignment first")
	}
	return a.exportAssignmentByID(ctx.AssignmentID, ctx.TermID, ctx.CourseYearID, file, true)
}

func (a *App) ExportPendingAssignments() error {
	ctx := a.context()
	if ctx.TermID == 0 || ctx.CourseYearID == 0 {
		return errors.New("set year, term, and course first")
	}
	assignments, err := a.pendingAssignmentsForExport(ctx.TermID, ctx.CourseYearID)
	if err != nil {
		return err
	}
	if len(assignments) == 0 {
		fmt.Fprintln(a.out, "No unexported or modified assignments.")
		return nil
	}
	for i, assignment := range assignments {
		fmt.Fprintf(a.out, "[%d/%d] %s\n", i+1, len(assignments), assignment.Title)
		if err := a.exportAssignmentByID(assignment.AssignmentID, ctx.TermID, ctx.CourseYearID, "", false); err != nil {
			if errors.Is(err, errExportNotConfirmed) {
				continue
			}
			return err
		}
	}
	return nil
}

func (a *App) exportAssignmentByID(assignmentID, termID, courseYearID int, file string, announceIfPending bool) error {
	document, err := a.buildAssignmentExportDocument(assignmentID, termID, courseYearID)
	if err != nil {
		return err
	}
	if announceIfPending {
		if needsExport, wasExported, err := a.assignmentNeedsExport(assignmentID, document.StableHash); err != nil {
			return err
		} else {
			switch {
			case !wasExported:
				fmt.Fprintln(a.out, colorOrange("This assignment has not been exported yet."))
			case needsExport:
				fmt.Fprintln(a.out, colorOrange("This assignment changed since its last export."))
			default:
				fmt.Fprintln(a.out, colorGreen("This assignment matches the last confirmed export."))
			}
		}
	}
	fmt.Fprintf(a.out, "Assignment Name:\t%s\n", document.Meta.Title)
	fmt.Fprintf(a.out, "Category:\t%s\n", document.Meta.Category)
	fmt.Fprintf(a.out, "Max Points:\t%.1f\n", document.Meta.MaxPoints)

	if strings.TrimSpace(file) == "" {
		file, err = a.defaultAssignmentExportPath(document.Meta.Title)
		if err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(file, document.CSV, 0o644); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Exported grades to %s\n", file)

	ok, err := a.promptYesNo("Was the export successful?", false)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(a.out, colorOrange("Export not confirmed. This assignment still needs export."))
		return errExportNotConfirmed
	}
	if err := a.recordAssignmentExport(assignmentID, document.StableHash, file); err != nil {
		return err
	}
	fmt.Fprintln(a.out, colorGreen("Marked assignment as exported."))
	return nil
}

func (a *App) exportAssignmentMeta(assignmentID int) (assignmentExportMeta, error) {
	var meta assignmentExportMeta
	var dueDate sql.NullString
	err := a.db.QueryRow(`
		SELECT assignments.title,
		       categories.name,
		       courses.name,
		       assignments.assigned_date,
		       CAST(assignments.max_points AS REAL)
		FROM assignments
		JOIN categories ON categories.category_id = assignments.category_id
		JOIN course_years ON course_years.course_year_id = assignments.course_year_id
		JOIN courses ON courses.course_id = course_years.course_id
		WHERE assignments.assignment_id = ?`, assignmentID).
		Scan(&meta.Title, &meta.Category, &meta.CourseName, &dueDate, &meta.MaxPoints)
	if err != nil {
		return assignmentExportMeta{}, err
	}
	if dueDate.Valid && strings.TrimSpace(dueDate.String) != "" {
		meta.DueDate = dueDate.String
		meta.DueDateRaw = dueDate.String
	} else {
		meta.DueDate = time.Now().Format("2006-01-02")
	}
	return meta, nil
}

func (a *App) buildAssignmentExportDocument(assignmentID, termID, courseYearID int) (assignmentExportDocument, error) {
	if err := a.ensureDefaultGradesForAssignment(assignmentID, courseYearID, termID); err != nil {
		return assignmentExportDocument{}, err
	}
	meta, err := a.exportAssignmentMeta(assignmentID)
	if err != nil {
		return assignmentExportDocument{}, err
	}
	rows, err := a.assignmentExportRows(assignmentID, termID, courseYearID)
	if err != nil {
		return assignmentExportDocument{}, err
	}
	csvBytes, err := renderAssignmentExportCSV(meta, rows)
	if err != nil {
		return assignmentExportDocument{}, err
	}
	stableHash := hashAssignmentExport(meta, rows)
	return assignmentExportDocument{
		Meta:       meta,
		Rows:       rows,
		CSV:        csvBytes,
		StableHash: stableHash,
	}, nil
}

func renderAssignmentExportCSV(meta assignmentExportMeta, rows []assignmentExportRow) ([]byte, error) {
	var buffer bytes.Buffer
	writer := csv.NewWriter(&buffer)
	headerRows := [][]string{
		{"Teacher Name:", "David Popovici", ""},
		{"Class:", meta.CourseName, ""},
		{"Assignment Name:", meta.Title, ""},
		{"Due Date:", meta.DueDate, ""},
		{"Points Possible:", "100.0", ""},
		{"Extra Points:", "0.0", ""},
		{"Score Type:", "POINTS", ""},
		{"Student Num", "Student Name", "Score"},
	}
	for _, row := range headerRows {
		if err := writer.Write(row); err != nil {
			return nil, err
		}
	}
	for _, row := range rows {
		if err := writer.Write([]string{row.PowerSchoolNum, row.StudentName, row.Score}); err != nil {
			return nil, err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func hashAssignmentExport(meta assignmentExportMeta, rows []assignmentExportRow) string {
	var buffer strings.Builder
	buffer.WriteString(meta.Title)
	buffer.WriteString("\n")
	buffer.WriteString(meta.Category)
	buffer.WriteString("\n")
	buffer.WriteString(meta.CourseName)
	buffer.WriteString("\n")
	buffer.WriteString(fmt.Sprintf("%.4f", meta.MaxPoints))
	buffer.WriteString("\n")
	buffer.WriteString(meta.DueDateRaw)
	buffer.WriteString("\n")
	for _, row := range rows {
		buffer.WriteString(row.PowerSchoolNum)
		buffer.WriteString("|")
		buffer.WriteString(row.StudentName)
		buffer.WriteString("|")
		buffer.WriteString(row.Score)
		buffer.WriteString("\n")
	}
	sum := sha256.Sum256([]byte(buffer.String()))
	return hex.EncodeToString(sum[:])
}

func (a *App) assignmentExportRows(assignmentID, termID, courseYearID int) ([]assignmentExportRow, error) {
	rows, err := a.db.Query(`
		SELECT DISTINCT students.student_pk,
		       COALESCE(students.powerschool_num, ''),
		       students.last_name,
		       COALESCE(students.chinese_name, ''),
		       students.first_name,
		       students.status,
		       grades.score,
		       COALESCE(grades.flags_bitmask, 0),
		       assignments.max_points,
		       COALESCE(category_grading_policies.scheme_key, 'average'),
		       COALESCE(assignments.pass_percent, category_grading_policies.default_pass_percent)
		FROM section_enrollments
		JOIN sections ON sections.section_id = section_enrollments.section_id
		JOIN students ON students.student_pk = section_enrollments.student_pk
		JOIN assignments ON assignments.assignment_id = ?
		LEFT JOIN grades ON grades.assignment_id = ? AND grades.student_pk = students.student_pk
		LEFT JOIN category_grading_policies
		  ON category_grading_policies.course_year_id = assignments.course_year_id
		 AND category_grading_policies.term_id = assignments.term_id
		 AND category_grading_policies.category_id = assignments.category_id
		WHERE section_enrollments.term_id = ? AND sections.course_year_id = ?
		ORDER BY students.last_name, students.first_name`, assignmentID, assignmentID, termID, courseYearID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []assignmentExportRow
	for rows.Next() {
		var studentID int
		var powerSchoolNum, lastName, chineseName, firstName, status string
		var score sql.NullFloat64
		var passPercent sql.NullFloat64
		var maxPoints int
		var schemeKey string
		var flags int
		if err := rows.Scan(&studentID, &powerSchoolNum, &lastName, &chineseName, &firstName, &status, &score, &flags, &maxPoints, &schemeKey, &passPercent); err != nil {
			return nil, err
		}
		_ = studentID
		record := GradeRecord{Score: score, Flags: flags, MaxPoints: maxPoints, PassPercent: passPercent}
		out = append(out, assignmentExportRow{
			PowerSchoolNum: powerSchoolNum,
			StudentName:    formatPowerSchoolExportName(lastName, chineseName, firstName),
			Score:          powerschoolExportScore(record, schemeKey, status),
		})
	}
	return out, rows.Err()
}

func formatPowerSchoolExportName(lastName, chineseName, firstName string) string {
	right := normalizeSpaces(strings.TrimSpace(chineseName + " " + firstName))
	return fmt.Sprintf("%s, %s", lastName, right)
}

func powerschoolExportScore(record GradeRecord, schemeKey, status string) string {
	if !strings.EqualFold(status, "active") {
		return ""
	}
	if schemeKey == "completion" {
		effective := completionPercent(record, record.PassPercent, 100, 0)
		return strconv.FormatFloat(effective, 'f', 2, 64)
	}
	if record.Flags&flagMissing != 0 || !record.Score.Valid {
		return "0.00"
	}
	if record.MaxPoints <= 0 {
		return "0.00"
	}
	scaled := (record.Score.Float64 / float64(record.MaxPoints)) * 100
	return strconv.FormatFloat(scaled, 'f', 2, 64)
}

func (a *App) pendingAssignmentsForExport(termID, courseYearID int) ([]pendingAssignmentExport, error) {
	rows, err := a.db.Query(`
		SELECT assignment_id, title
		FROM assignments
		WHERE term_id = ? AND course_year_id = ?
		ORDER BY assignment_id`, termID, courseYearID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []pendingAssignmentExport
	for rows.Next() {
		var item pendingAssignmentExport
		if err := rows.Scan(&item.AssignmentID, &item.Title); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	var out []pendingAssignmentExport
	for _, item := range items {
		document, err := a.buildAssignmentExportDocument(item.AssignmentID, termID, courseYearID)
		if err != nil {
			return nil, err
		}
		needsExport, wasExported, err := a.assignmentNeedsExport(item.AssignmentID, document.StableHash)
		if err != nil {
			return nil, err
		}
		item.NeedsExport = needsExport
		item.WasExported = wasExported
		if needsExport {
			out = append(out, item)
		}
	}
	return out, nil
}

func (a *App) assignmentNeedsExport(assignmentID int, currentHash string) (bool, bool, error) {
	var storedHash string
	err := a.db.QueryRow(`SELECT export_hash FROM assignment_exports WHERE assignment_id = ?`, assignmentID).Scan(&storedHash)
	if errors.Is(err, sql.ErrNoRows) {
		return true, false, nil
	}
	if err != nil {
		return false, false, err
	}
	return storedHash != currentHash, true, nil
}

func (a *App) recordAssignmentExport(assignmentID int, exportHash, exportPath string) error {
	_, err := a.db.Exec(`
		INSERT INTO assignment_exports(assignment_id, export_hash, export_path, exported_at)
		VALUES (?, ?, ?, strftime('%Y-%m-%dT%H:%M:%fZ','now'))
		ON CONFLICT(assignment_id) DO UPDATE SET
			export_hash = excluded.export_hash,
			export_path = excluded.export_path,
			exported_at = excluded.exported_at`,
		assignmentID, exportHash, exportPath)
	return err
}

func (a *App) defaultAssignmentExportPath(title string) (string, error) {
	exportDir := filepath.Clean(filepath.Join(a.homeDir, "..", "gradesExports"))
	filename := sanitizeFilename(title) + ".csv"
	return filepath.Join(exportDir, filename), nil
}

func sanitizeFilename(name string) string {
	name = normalizeSpaces(name)
	replacer := strings.NewReplacer(
		`<`, "_",
		`>`, "_",
		`:`, "_",
		`"`, "_",
		`/`, "_",
		`\`, "_",
		`|`, "_",
		`?`, "_",
		`*`, "_",
	)
	name = replacer.Replace(name)
	name = strings.ReplaceAll(name, " ", "_")
	if name == "" {
		return "assignment_export"
	}
	return name
}

func (a *App) ImportPowerSchoolNumbers(file string) error {
	if err := a.ensureStudentCommandContext(); err != nil {
		return err
	}
	ctx := a.context()
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()

	reader := csv.NewReader(f)
	reader.FieldsPerRecord = -1
	rows, err := reader.ReadAll()
	if err != nil {
		return err
	}
	headerIndex := -1
	for i, row := range rows {
		if len(row) >= 3 && strings.EqualFold(strings.TrimSpace(row[0]), "Student Num") && strings.EqualFold(strings.TrimSpace(row[1]), "Student Name") {
			headerIndex = i
			break
		}
	}
	if headerIndex == -1 {
		return errors.New("missing Student Num/Student Name header row")
	}

	students, err := a.studentsForCourseTerm(ctx.CourseYearID, ctx.TermID, false)
	if err != nil {
		return err
	}
	byName := map[string][]Student{}
	for _, student := range students {
		key := strings.ToLower(normalizeSpaces(student.LastName + ", " + student.FirstName))
		byName[key] = append(byName[key], student)
	}

	updated := 0
	for _, row := range rows[headerIndex+1:] {
		if len(row) < 2 {
			continue
		}
		psNum := normalizeSpaces(row[0])
		name := normalizeSpaces(row[1])
		if psNum == "" || name == "" {
			continue
		}
		matches := byName[strings.ToLower(name)]
		switch {
		case len(matches) == 1:
			if _, err := a.db.Exec(`UPDATE students SET powerschool_num = ? WHERE student_pk = ?`, psNum, matches[0].ID); err != nil {
				return err
			}
			updated++
			continue
		case len(matches) > 1:
			return fmt.Errorf("multiple students match %s in current course", name)
		}

		first, last, chineseName := parsePowerSchoolStudentName(name)
		if first == "" || last == "" {
			return fmt.Errorf("could not parse student name: %s", name)
		}
		if candidate, ok := exactPowerSchoolStudentMatch(students, first, last); ok {
			if err := a.updatePowerSchoolStudent(candidate.ID, psNum, chineseName); err != nil {
				return err
			}
			updated++
			continue
		}
		if candidate, ok, err := a.confirmSimilarPowerSchoolStudent(students, first, last, psNum); err != nil {
			return err
		} else if ok {
			if err := a.updatePowerSchoolStudent(candidate.ID, psNum, chineseName); err != nil {
				return err
			}
			updated++
			continue
		}

		create, err := a.promptYesNo(fmt.Sprintf(`No existing student matched "%s". Create a new student?`, name), false)
		if err != nil {
			return err
		}
		if !create {
			continue
		}
		sectionID, err := a.promptSectionForNewStudent(ctx.CourseYearID)
		if err != nil {
			return err
		}
		student, err := a.createStudentWithPowerSchoolNumber(first, last, chineseName, psNum)
		if err != nil {
			return err
		}
		if err := a.enrollStudent(sectionID, ctx.TermID, student.ID); err != nil {
			return err
		}
		students = append(students, student)
		byName[strings.ToLower(normalizeSpaces(student.LastName+", "+student.FirstName))] = append(byName[strings.ToLower(normalizeSpaces(student.LastName+", "+student.FirstName))], student)
		updated++
	}
	fmt.Fprintf(a.out, "Updated %d student PowerSchool number(s)\n", updated)
	return nil
}

func (a *App) studentsForCourseTerm(courseYearID, termID int, activeOnly bool) ([]Student, error) {
	query := `
		SELECT DISTINCT students.student_pk, students.first_name, students.last_name,
		       COALESCE(students.chinese_name,''), COALESCE(students.school_student_id,''), COALESCE(students.powerschool_num,'')
		FROM section_enrollments
		JOIN students ON students.student_pk = section_enrollments.student_pk
		JOIN sections ON sections.section_id = section_enrollments.section_id
		WHERE section_enrollments.term_id = ? AND sections.course_year_id = ?`
	if activeOnly {
		query += ` AND students.status = 'active'`
	}
	query += ` ORDER BY students.student_pk`
	rows, err := a.db.Query(query, termID, courseYearID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var students []Student
	for rows.Next() {
		var student Student
		if err := rows.Scan(&student.ID, &student.FirstName, &student.LastName, &student.ChineseName, &student.SchoolStudentID, &student.PowerSchoolNum); err != nil {
			return nil, err
		}
		students = append(students, student)
	}
	return students, rows.Err()
}

func parsePowerSchoolStudentName(name string) (first, last, chinese string) {
	parts := strings.SplitN(name, ",", 2)
	if len(parts) != 2 {
		return "", "", ""
	}
	last = normalizeSpaces(parts[0])
	right := strings.Fields(normalizeSpaces(parts[1]))
	if len(right) == 0 {
		return "", "", ""
	}
	first = right[len(right)-1]
	if len(right) > 1 {
		chinese = strings.Join(right[:len(right)-1], " ")
	}
	return first, last, chinese
}

func (a *App) confirmSimilarPowerSchoolStudent(students []Student, first, last, psNum string) (Student, bool, error) {
	query := strings.TrimSpace(first + " " + last)
	candidate, err := matchStudent(students, map[int]bool{}, query)
	if err != nil {
		return Student{}, false, nil
	}
	ok, err := a.promptYesNo(fmt.Sprintf(`Use existing student "%s %s" for PowerSchool row "%s, %s" (%s)?`, candidate.FirstName, candidate.LastName, last, first, psNum), false)
	if err != nil {
		return Student{}, false, err
	}
	if !ok {
		return Student{}, false, nil
	}
	return candidate, true, nil
}

func exactPowerSchoolStudentMatch(students []Student, first, last string) (Student, bool) {
	var matches []Student
	for _, student := range students {
		if strings.EqualFold(student.FirstName, first) && strings.EqualFold(student.LastName, last) {
			matches = append(matches, student)
		}
	}
	if len(matches) != 1 {
		return Student{}, false
	}
	return matches[0], true
}

func (a *App) promptSectionForNewStudent(courseYearID int) (int, error) {
	items, err := a.namedIDsForQuery(`SELECT section_id, name FROM sections WHERE course_year_id = ? ORDER BY section_id`, courseYearID)
	if err != nil {
		return 0, err
	}
	if len(items) == 0 {
		return 0, errors.New("no sections exist for the current course")
	}
	fmt.Fprintln(a.out, "Sections:")
	for _, item := range items {
		fmt.Fprintf(a.out, "%d\t%s\n", item.ID, item.Name)
	}
	for {
		raw, err := a.promptNonEmpty("Section")
		if err != nil {
			return 0, err
		}
		id, _, err := a.lookupSection(raw, courseYearID)
		if err != nil {
			fmt.Fprintln(a.out, retryMessage(err.Error()))
			continue
		}
		return id, nil
	}
}

func (a *App) createStudentWithPowerSchoolNumber(first, last, chineseName, psNum string) (Student, error) {
	res, err := a.db.Exec(`INSERT INTO students(first_name, last_name, chinese_name, powerschool_num, status) VALUES (?, ?, ?, ?, 'active')`, first, last, chineseName, psNum)
	if err != nil {
		return Student{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Student{}, err
	}
	return Student{ID: int(id), FirstName: first, LastName: last, ChineseName: chineseName, PowerSchoolNum: psNum}, nil
}

func (a *App) updatePowerSchoolStudent(studentID int, psNum, chineseName string) error {
	if strings.TrimSpace(chineseName) != "" {
		_, err := a.db.Exec(`UPDATE students SET powerschool_num = ?, chinese_name = ? WHERE student_pk = ?`, psNum, chineseName, studentID)
		return err
	}
	_, err := a.db.Exec(`UPDATE students SET powerschool_num = ? WHERE student_pk = ?`, psNum, studentID)
	return err
}

func exportGrade(record GradeRecord) (score string, status string) {
	if record.Flags&flagMissing != 0 {
		if record.Flags&flagLate != 0 {
			return "0", "missing late"
		}
		return "0", "missing"
	}
	if record.Score.Valid {
		score = strconv.FormatFloat(record.Score.Float64, 'f', -1, 64)
	}
	if record.Flags&flagRedo != 0 {
		status = "redo"
	}
	if record.Flags&flagLate != 0 {
		if status == "" {
			status = "late"
		} else {
			status += " late"
		}
	}
	return score, status
}

type rosterRecord struct {
	Year        string
	Course      string
	Section     string
	StudentID   string
	FirstName   string
	LastName    string
	ChineseName string
}

func rosterRecordFromRow(headers map[string]int, row []string) (rosterRecord, error) {
	get := func(key string) string {
		idx, ok := headers[key]
		if !ok || idx >= len(row) {
			return ""
		}
		return normalizeSpaces(row[idx])
	}

	record := rosterRecord{
		Year:        get("year"),
		Course:      get("course"),
		Section:     get("section"),
		StudentID:   get("student_id"),
		FirstName:   get("first_name"),
		LastName:    get("last_name"),
		ChineseName: get("chinese_name"),
	}

	switch {
	case record.Year == "":
		return rosterRecord{}, errors.New("year cannot be blank")
	case record.Course == "":
		return rosterRecord{}, errors.New("course cannot be blank")
	case record.Section == "":
		return rosterRecord{}, errors.New("section cannot be blank")
	case record.FirstName == "":
		return rosterRecord{}, errors.New("first_name cannot be blank")
	case record.LastName == "":
		return rosterRecord{}, errors.New("last_name cannot be blank")
	default:
		return record, nil
	}
}

func isSkippedImportRow(row []string) bool {
	if len(row) == 0 {
		return true
	}

	allBlank := true
	for _, cell := range row {
		if strings.TrimSpace(cell) != "" {
			allBlank = false
			break
		}
	}
	if allBlank {
		return true
	}

	return strings.HasPrefix(strings.TrimSpace(row[0]), "#")
}

func normalizedCSVHeader(header string) string {
	header = strings.TrimPrefix(header, "\ufeff")
	return strings.ToLower(strings.TrimSpace(header))
}

func (a *App) promptPath(label string) (string, error) {
	raw, err := a.prompt(label)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(raw), nil
}

func (a *App) promptFileImportAction(kind string, defaultExists bool) (string, error) {
	for {
		label := fmt.Sprintf("Import %s: [I]mport default, [C]reate/open default, [P]ick another file", kind)
		if !defaultExists {
			label = fmt.Sprintf("Import %s: [C]reate/open default, [P]ick another file", kind)
		}
		raw, err := a.prompt(label)
		if err != nil {
			return "", err
		}
		answer := strings.ToLower(strings.TrimSpace(raw))
		switch answer {
		case "":
			if defaultExists {
				return "import-default", nil
			}
			return "create-default", nil
		case "i", "import":
			if defaultExists {
				return "import-default", nil
			}
		case "c", "create", "open":
			return "create-default", nil
		case "p", "pick", "path", "file":
			return "pick-file", nil
		}
		fmt.Fprintln(a.out, retryMessage("please choose import, create, or pick"))
	}
}

func (a *App) lookupCourseID(name string) (int, error) {
	var id int
	err := a.db.QueryRow(`SELECT course_id FROM courses WHERE lower(name)=lower(?)`, name).Scan(&id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("course not found: %s", name)
		}
		return 0, err
	}
	return id, nil
}

func (a *App) lookupCourseYearID(courseID int, courseName, year string) (int, error) {
	var id int
	name := fmt.Sprintf("%s %s", courseName, year)
	err := a.db.QueryRow(`SELECT course_year_id FROM course_years WHERE course_id = ? AND lower(name)=lower(?)`, courseID, name).Scan(&id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("course-year not found: %s", name)
		}
		return 0, err
	}
	return id, nil
}

func (a *App) lookupSectionID(courseYearID int, name string) (int, error) {
	var id int
	err := a.db.QueryRow(`SELECT section_id FROM sections WHERE course_year_id = ? AND lower(name)=lower(?)`, courseYearID, name).Scan(&id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, fmt.Errorf("section not found for course-year: %s", name)
		}
		return 0, err
	}
	return id, nil
}

func (a *App) courseYearTermIDs(courseYearID int) ([]int, error) {
	rows, err := a.db.Query(`
		SELECT course_year_terms.term_id
		FROM course_year_terms
		JOIN terms ON terms.term_id = course_year_terms.term_id
		WHERE course_year_terms.course_year_id = ?
		ORDER BY terms.start_date, terms.name`, courseYearID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("no terms configured for course-year: %d", courseYearID)
	}
	return ids, nil
}

func openFile(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", path)
	case "darwin":
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Start()
}
