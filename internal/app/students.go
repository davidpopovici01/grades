package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"text/tabwriter"
)

func (a *App) ListTerms() error {
	return a.printNamedIDs(`SELECT term_id, name FROM terms ORDER BY term_id`)
}

func (a *App) ListCourseYears() error {
	items, err := a.listCourseYearItems(a.context().Year)
	if err != nil {
		return err
	}
	for _, item := range items {
		fmt.Fprintf(a.out, "%d\t%s\n", item.ID, baseCourseName(item.Name))
	}
	return nil
}

func (a *App) ListSections() error {
	ctx := a.context()
	if ctx.CourseYearID == 0 {
		return errors.New("set a year and course first")
	}
	rows, err := a.db.Query(`SELECT section_id, name FROM sections WHERE course_year_id = ? ORDER BY section_id`, ctx.CourseYearID)
	if err != nil {
		return err
	}
	defer rows.Close()
	return printRows(a.out, rows)
}

func (a *App) ListStudents() error {
	updated, notes, err := a.autoSelectContextDefaults()
	if err != nil {
		return err
	}
	if updated {
		for _, note := range notes {
			fmt.Fprintln(a.out, note)
		}
	}
	if err := a.ensureStudentCommandContext(); err != nil {
		return err
	}
	students, scope, err := a.studentsForList()
	if err != nil {
		return err
	}
	if scope != "" {
		fmt.Fprintln(a.out, scope)
	}
	for _, student := range students {
		fmt.Fprintf(a.out, "%d\t%s %s\n", student.ID, student.FirstName, student.LastName)
	}
	return nil
}

func (a *App) AddStudentInteractive() error {
	ctx := a.context()
	if ctx.SectionID == 0 || ctx.TermID == 0 {
		return errors.New("set term and section first")
	}
	first, err := a.promptNonEmpty("First name")
	if err != nil {
		return err
	}
	last, err := a.promptNonEmpty("Last name")
	if err != nil {
		return err
	}
	chineseName, err := a.promptOptional("Chinese name (optional)")
	if err != nil {
		return err
	}
	studentID, err := a.promptOptional("Student ID (optional)")
	if err != nil {
		return err
	}
	student, err := a.upsertStudent(first, last, chineseName, studentID)
	if err != nil {
		return err
	}
	if err := a.enrollStudent(ctx.SectionID, ctx.TermID, student.ID); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Added student: %s %s\n", student.FirstName, student.LastName)
	return nil
}

func (a *App) EditStudentInteractive(studentID string) error {
	if err := a.ensureStudentCommandContext(); err != nil {
		return err
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

	fmt.Fprintf(a.out, "Editing %s %s (ID %d). Press Enter to keep the current value; enter - to clear an optional field.\n",
		student.FirstName, student.LastName, student.ID)

	first, err := a.promptStudentField("First name", student.FirstName, false)
	if err != nil {
		return err
	}
	last, err := a.promptStudentField("Last name", student.LastName, false)
	if err != nil {
		return err
	}
	chineseName, err := a.promptStudentField("Chinese name", student.ChineseName, true)
	if err != nil {
		return err
	}
	schoolID, err := a.promptStudentField("Student ID", student.SchoolStudentID, true)
	if err != nil {
		return err
	}
	if schoolID != "" && schoolID != student.SchoolStudentID {
		owner, err := a.studentIDOwner("school_student_id", schoolID, student.ID)
		if err != nil {
			return err
		}
		if owner != "" {
			return fmt.Errorf("student ID %s already belongs to %s", schoolID, owner)
		}
	}
	powerSchoolNum, err := a.promptStudentField("PowerSchool number", student.PowerSchoolNum, true)
	if err != nil {
		return err
	}
	if powerSchoolNum != "" && powerSchoolNum != student.PowerSchoolNum {
		owner, err := a.studentIDOwner("powerschool_num", powerSchoolNum, student.ID)
		if err != nil {
			return err
		}
		if owner != "" {
			return fmt.Errorf("PowerSchool number %s already belongs to %s", powerSchoolNum, owner)
		}
	}

	first = capitalizeName(first)
	last = capitalizeName(last)
	chineseName = capitalizeName(chineseName)

	_, err = a.db.Exec(`
		UPDATE students
		SET first_name = ?, last_name = ?, chinese_name = ?, school_student_id = ?, powerschool_num = ?
		WHERE student_pk = ?`,
		first, last,
		nullIfEmpty(chineseName), nullIfEmpty(schoolID), nullIfEmpty(powerSchoolNum),
		student.ID)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Updated student: %s %s\n", first, last)
	return nil
}

func (a *App) EditStudentIDInteractive() error {
	fmt.Fprintln(a.out, "Assign student IDs. Press Enter at the Student prompt to finish; enter - as the ID to clear it.")
	for {
		ref, err := a.prompt("Student")
		if err != nil {
			return err
		}
		if strings.TrimSpace(ref) == "" {
			return nil
		}
		students, err := a.allStudents()
		if err != nil {
			return err
		}
		student, err := matchStudent(students, map[int]bool{}, ref)
		if err != nil {
			fmt.Fprintln(a.out, retryMessage(err.Error()))
			continue
		}
		schoolID, err := a.promptStudentField(fmt.Sprintf("Student ID for %s %s", student.FirstName, student.LastName), student.SchoolStudentID, true)
		if err != nil {
			return err
		}
		if schoolID == student.SchoolStudentID {
			continue
		}
		if schoolID != "" {
			owner, err := a.studentIDOwner("school_student_id", schoolID, student.ID)
			if err != nil {
				return err
			}
			if owner != "" {
				fmt.Fprintf(a.out, "Student ID %s already belongs to %s.\n", schoolID, owner)
				continue
			}
		}
		if _, err := a.db.Exec(`UPDATE students SET school_student_id = ? WHERE student_pk = ?`, nullIfEmpty(schoolID), student.ID); err != nil {
			return err
		}
		if schoolID == "" {
			fmt.Fprintf(a.out, "Cleared student ID for %s %s\n", student.FirstName, student.LastName)
		} else {
			fmt.Fprintf(a.out, "Set %s %s student ID to %s\n", student.FirstName, student.LastName, schoolID)
		}
	}
}

func (a *App) allStudents() ([]Student, error) {
	rows, err := a.db.Query(`
		SELECT student_pk, first_name, last_name,
		       COALESCE(chinese_name,''), COALESCE(school_student_id,''), COALESCE(powerschool_num,'')
		FROM students
		ORDER BY student_pk`)
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

func (a *App) promptStudentField(label, current string, clearable bool) (string, error) {
	for {
		raw, err := a.prompt(fmt.Sprintf("%s [%s]", label, current))
		if err != nil {
			return "", err
		}
		raw = normalizeSpaces(raw)
		switch {
		case raw == "":
			return current, nil
		case raw == "-" && clearable:
			return "", nil
		case raw == "-":
			fmt.Fprintf(a.out, "%s is required.\n", label)
			continue
		default:
			return raw, nil
		}
	}
}

func (a *App) studentIDOwner(column, value string, excludeID int) (string, error) {
	var name string
	err := a.db.QueryRow(
		fmt.Sprintf(`SELECT first_name || ' ' || last_name FROM students WHERE %s = ? AND student_pk != ?`, column),
		value, excludeID).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return name, nil
}

func nullIfEmpty(value string) sql.NullString {
	if value == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: value, Valid: true}
}

func (a *App) RemoveStudentInteractive(studentID string) error {
	ctx := a.context()
	if ctx.TermID == 0 || ctx.CourseYearID == 0 {
		return errors.New("set year, term, and course first")
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
	var res sql.Result
	if ctx.SectionID != 0 {
		res, err = a.db.Exec(`
			DELETE FROM section_enrollments
			WHERE section_id = ?
			  AND student_pk = ?
			  AND term_id IN (
				SELECT term_id
				FROM course_year_terms
				WHERE course_year_id = (
					SELECT course_year_id FROM sections WHERE section_id = ?
				)
			  )`, ctx.SectionID, student.ID, ctx.SectionID)
		if err != nil {
			return err
		}
	} else {
		res, err = a.db.Exec(`
			DELETE FROM section_enrollments
			WHERE student_pk = ? AND section_id IN (
				SELECT section_id FROM sections WHERE course_year_id = ?
			)`, student.ID, ctx.CourseYearID)
		if err != nil {
			return err
		}
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		if ctx.SectionID != 0 {
			return fmt.Errorf("%s %s is not enrolled in the current section", student.FirstName, student.LastName)
		}
		return fmt.Errorf("%s %s is not enrolled in the current course", student.FirstName, student.LastName)
	}
	if ctx.SectionID != 0 {
		fmt.Fprintf(a.out, "Removed %s %s\n", student.FirstName, student.LastName)
		return nil
	}
	fmt.Fprintf(a.out, "Removed %s %s from %d section(s)\n", student.FirstName, student.LastName, affected)
	return nil
}

func (a *App) SetStudentStatus(studentID, status string) error {
	if err := a.ensureStudentCommandContext(); err != nil {
		return err
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
	if _, err := a.db.Exec(`UPDATE students SET status = ? WHERE student_pk = ?`, status, student.ID); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Set %s %s to %s\n", student.FirstName, student.LastName, status)
	return nil
}

const studentSortOffset = 1_000_000_000

type studentPKReference struct {
	table  string
	column string
}

func (a *App) SortStudents() error {
	rows, err := a.db.Query(`SELECT student_pk, first_name, last_name FROM students ORDER BY lower(last_name), lower(first_name), student_pk`)
	if err != nil {
		return err
	}
	var studentIDs []int
	var names []string
	for rows.Next() {
		var id int
		var first, last string
		if err := rows.Scan(&id, &first, &last); err != nil {
			rows.Close()
			return err
		}
		studentIDs = append(studentIDs, id)
		names = append(names, strings.TrimSpace(first+" "+last))
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if len(studentIDs) == 0 {
		fmt.Fprintln(a.out, "No students to sort.")
		return nil
	}

	alreadySorted := true
	for i, id := range studentIDs {
		if id != i+1 {
			alreadySorted = false
			break
		}
	}
	if alreadySorted {
		fmt.Fprintln(a.out, "Students are already sorted by last name.")
		return nil
	}

	refs, err := a.studentPKReferences()
	if err != nil {
		return err
	}

	ctx := context.Background()
	conn, err := a.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return err
	}
	defer conn.ExecContext(context.Background(), `PRAGMA foreign_keys = ON`)

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	rollback := func(err error) error {
		_ = tx.Rollback()
		return err
	}

	if _, err := tx.ExecContext(ctx, `UPDATE students SET student_pk = student_pk + ?`, studentSortOffset); err != nil {
		return rollback(err)
	}
	for _, ref := range refs {
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`UPDATE %s SET %s = %s + ?`, quoteIdent(ref.table), quoteIdent(ref.column), quoteIdent(ref.column)), studentSortOffset); err != nil {
			return rollback(err)
		}
	}
	for i, oldID := range studentIDs {
		newID := i + 1
		res, err := tx.ExecContext(ctx, `UPDATE students SET student_pk = ? WHERE student_pk = ?`, newID, oldID+studentSortOffset)
		if err != nil {
			return rollback(err)
		}
		if affected, err := res.RowsAffected(); err != nil || affected != 1 {
			return rollback(fmt.Errorf("could not renumber student %s", names[i]))
		}
		for _, ref := range refs {
			if _, err := tx.ExecContext(ctx, fmt.Sprintf(`UPDATE %s SET %s = ? WHERE %s = ?`, quoteIdent(ref.table), quoteIdent(ref.column), quoteIdent(ref.column)), newID, oldID+studentSortOffset); err != nil {
				return rollback(err)
			}
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE sqlite_sequence SET seq = ? WHERE name = 'students'`, len(studentIDs)); err != nil {
		return rollback(err)
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	fmt.Fprintf(a.out, "Sorted %d students by last name (IDs were renumbered).\n", len(studentIDs))
	return nil
}

func (a *App) studentPKReferences() ([]studentPKReference, error) {
	tableRows, err := a.db.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'`)
	if err != nil {
		return nil, err
	}
	var tables []string
	for tableRows.Next() {
		var name string
		if err := tableRows.Scan(&name); err != nil {
			tableRows.Close()
			return nil, err
		}
		tables = append(tables, name)
	}
	if err := tableRows.Err(); err != nil {
		tableRows.Close()
		return nil, err
	}
	if err := tableRows.Close(); err != nil {
		return nil, err
	}

	var refs []studentPKReference
	for _, table := range tables {
		fkRows, err := a.db.Query(fmt.Sprintf(`PRAGMA foreign_key_list(%s)`, quoteIdent(table)))
		if err != nil {
			return nil, err
		}
		for fkRows.Next() {
			var id, seq int
			var refTable, from, onUpdate, onDelete, match string
			var to sql.NullString
			if err := fkRows.Scan(&id, &seq, &refTable, &from, &to, &onUpdate, &onDelete, &match); err != nil {
				fkRows.Close()
				return nil, err
			}
			if refTable == "students" && (!to.Valid || to.String == "student_pk") {
				refs = append(refs, studentPKReference{table: table, column: from})
			}
		}
		if err := fkRows.Err(); err != nil {
			fkRows.Close()
			return nil, err
		}
		if err := fkRows.Close(); err != nil {
			return nil, err
		}
	}
	return refs, nil
}

func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func (a *App) ShowStudent(studentID string) error {
	studentID = strings.TrimSpace(studentID)
	if studentID == "" {
		return errors.New("provide a student")
	}
	if err := a.ensureStudentCommandContext(); err != nil {
		return err
	}
	ctx := a.context()
	student, err := a.resolveStudentReference(studentID)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.out, "%s %s\n", student.FirstName, student.LastName)
	fmt.Fprintf(a.out, "Student ID:\t%s\n", fallback(student.SchoolStudentID))
	if student.ChineseName != "" {
		fmt.Fprintf(a.out, "Chinese name:\t%s\n", student.ChineseName)
	}
	if ctx.TermID == 0 || ctx.CourseYearID == 0 {
		return nil
	}

	rules, err := a.categoryRulesForContext(ctx.CourseYearID, ctx.TermID)
	if err != nil {
		return err
	}
	categoryScores, weighted, err := a.categoryScoresByStudent([]Student{student}, rules)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.out, "GPA (weighted total):\t%s\n", fallback(weighted[student.ID]))

	if len(rules) > 0 {
		fmt.Fprintln(a.out)
		fmt.Fprintln(a.out, "Category Totals")
		tw := tabwriter.NewWriter(a.out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "Category\tWeight\tTotal")
		for _, rule := range rules {
			weight := ""
			if rule.HasWeight {
				weight = fmt.Sprintf("%.1f%%", rule.WeightPercent)
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\n", rule.CategoryName, weight, categoryScores[student.ID][rule.CategoryID])
		}
		if err := tw.Flush(); err != nil {
			return err
		}
	}

	records, err := a.studentAssignmentDetails(student.ID)
	if err != nil {
		return err
	}
	if len(records) == 0 {
		return nil
	}

	fmt.Fprintln(a.out)
	fmt.Fprintln(a.out, "Assignments")
	tw := tabwriter.NewWriter(a.out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "Assignment\tCategory\tGrade\tCounts As\tFlags")
	for _, record := range records {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", record.Title, record.Category, displayGradePlain(record.Grade), record.CountsAs, strings.Join(record.Flags, ", "))
	}
	return tw.Flush()
}

type studentAssignmentDetail struct {
	Title     string
	Category  string
	SchemeKey string
	Anchor    float64
	Lift      float64
	Grade     GradeRecord
	CountsAs  string
	Flags     []string
}

func (a *App) studentAssignmentDetails(studentID int) ([]studentAssignmentDetail, error) {
	ctx := a.context()
	rows, err := a.db.Query(`
		SELECT assignments.title,
		       categories.name,
		       COALESCE(category_grading_policies.scheme_key, 'average'),
		       COALESCE(assignment_curves.anchor_percent, 100),
		       COALESCE(assignment_curves.lift_percent, 1),
		       grades.score,
		       COALESCE(grades.flags_bitmask, 0),
		       assignments.max_points,
		       COALESCE(grades.redo_count, 0),
		       COALESCE(assignments.pass_percent, category_grading_policies.default_pass_percent)
		FROM assignments
		JOIN categories ON categories.category_id = assignments.category_id
		LEFT JOIN grades ON grades.assignment_id = assignments.assignment_id AND grades.student_pk = ?
		LEFT JOIN assignment_curves ON assignment_curves.assignment_id = assignments.assignment_id
		LEFT JOIN category_grading_policies
		  ON category_grading_policies.course_year_id = assignments.course_year_id
		 AND category_grading_policies.term_id = assignments.term_id
		 AND category_grading_policies.category_id = assignments.category_id
		WHERE assignments.course_year_id = ? AND assignments.term_id = ?
		ORDER BY assignments.assignment_id`, studentID, ctx.CourseYearID, ctx.TermID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var details []studentAssignmentDetail
	for rows.Next() {
		var detail studentAssignmentDetail
		if err := rows.Scan(&detail.Title, &detail.Category, &detail.SchemeKey, &detail.Anchor, &detail.Lift, &detail.Grade.Score, &detail.Grade.Flags, &detail.Grade.MaxPoints, &detail.Grade.RedoCount, &detail.Grade.PassPercent); err != nil {
			return nil, err
		}
		detail.Flags = studentVisibleFlags(detail.Grade)
		detail.CountsAs = assignmentCountsAs(detail)
		details = append(details, detail)
	}
	return details, rows.Err()
}

func assignmentCountsAs(detail studentAssignmentDetail) string {
	return fmt.Sprintf("%.1f%%", effectiveAssignmentPercent(detail.Grade, detail.Grade.PassPercent, detail.Anchor, detail.Lift))
}

func studentVisibleFlags(record GradeRecord) []string {
	var flags []string
	if record.Flags&flagLocked0 != 0 {
		flags = append(flags, "cheat")
	}
	if record.Flags&flagLate != 0 {
		flags = append(flags, "late")
	}
	if record.Flags&flagRedo != 0 {
		flags = append(flags, "redo")
	}
	if record.Flags&flagMissing != 0 {
		flags = append(flags, "missing")
	}
	if record.Flags&flagPass != 0 {
		flags = append(flags, "pass")
	}
	return flags
}

func (a *App) studentByID(id int) (Student, error) {
	var student Student
	err := a.db.QueryRow(`
		SELECT student_pk, first_name, last_name, COALESCE(chinese_name,''), COALESCE(school_student_id,''), COALESCE(powerschool_num,'')
		FROM students
		WHERE student_pk = ?`, id).
		Scan(&student.ID, &student.FirstName, &student.LastName, &student.ChineseName, &student.SchoolStudentID, &student.PowerSchoolNum)
	if errors.Is(err, sql.ErrNoRows) {
		return Student{}, fmt.Errorf("student not found: %d", id)
	}
	return student, err
}

func (a *App) upsertStudent(first, last, chineseName, studentID string) (Student, error) {
	first = capitalizeName(normalizeSpaces(first))
	last = capitalizeName(normalizeSpaces(last))
	chineseName = capitalizeName(normalizeSpaces(chineseName))
	studentID = normalizeSpaces(studentID)
	if first == "" || last == "" {
		return Student{}, errors.New("student first name and last name are required")
	}

	if studentID == "" {
		return a.createOrReuseStudentWithoutExternalID(first, last, chineseName)
	}

	var student Student
	err := a.db.QueryRow(`
		SELECT student_pk, first_name, last_name, COALESCE(chinese_name,''), COALESCE(school_student_id,''), COALESCE(powerschool_num,'')
		FROM students
		WHERE school_student_id = ?`, studentID).
		Scan(&student.ID, &student.FirstName, &student.LastName, &student.ChineseName, &student.SchoolStudentID, &student.PowerSchoolNum)
	switch {
	case err == nil:
		_, err = a.db.Exec(`UPDATE students SET first_name = ?, last_name = ?, chinese_name = ? WHERE student_pk = ?`, first, last, chineseName, student.ID)
		if err != nil {
			return Student{}, err
		}
		student.FirstName = first
		student.LastName = last
		student.ChineseName = chineseName
		return student, nil
	case errors.Is(err, sql.ErrNoRows):
		res, err := a.db.Exec(`INSERT INTO students(first_name, last_name, chinese_name, school_student_id) VALUES (?, ?, ?, ?)`, first, last, chineseName, studentID)
		if err != nil {
			return Student{}, err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return Student{}, err
		}
		return Student{ID: int(id), FirstName: first, LastName: last, ChineseName: chineseName, SchoolStudentID: studentID}, nil
	default:
		return Student{}, err
	}
}

func (a *App) createOrReuseStudentWithoutExternalID(first, last, chineseName string) (Student, error) {
	rows, err := a.db.Query(`
		SELECT student_pk, first_name, last_name, COALESCE(chinese_name,''), COALESCE(school_student_id,''), COALESCE(powerschool_num,'')
		FROM students
		WHERE lower(first_name) = lower(?) AND lower(last_name) = lower(?) AND (school_student_id IS NULL OR school_student_id = '')
		ORDER BY student_pk`, first, last)
	if err != nil {
		return Student{}, err
	}
	defer rows.Close()

	var matches []Student
	for rows.Next() {
		var student Student
		if err := rows.Scan(&student.ID, &student.FirstName, &student.LastName, &student.ChineseName, &student.SchoolStudentID, &student.PowerSchoolNum); err != nil {
			return Student{}, err
		}
		matches = append(matches, student)
	}
	if err := rows.Err(); err != nil {
		return Student{}, err
	}
	if len(matches) == 1 {
		if matches[0].ChineseName != chineseName {
			if _, err := a.db.Exec(`UPDATE students SET chinese_name = ? WHERE student_pk = ?`, chineseName, matches[0].ID); err != nil {
				return Student{}, err
			}
			matches[0].ChineseName = chineseName
		}
		return matches[0], nil
	}
	if len(matches) > 1 {
		return Student{}, fmt.Errorf("multiple students already exist for %s %s without an external student ID; remove duplicates or provide an external student ID", first, last)
	}

	res, err := a.db.Exec(`INSERT INTO students(first_name, last_name, chinese_name) VALUES (?, ?, ?)`, first, last, chineseName)
	if err != nil {
		return Student{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Student{}, err
	}
	return Student{ID: int(id), FirstName: first, LastName: last, ChineseName: chineseName}, nil
}

func (a *App) enrollStudent(sectionID, termID, studentID int) error {
	_, err := a.db.Exec(`
		INSERT INTO section_enrollments(section_id, student_pk, term_id, start_date, status)
		VALUES (?, ?, ?, date('now'), 'active')
		ON CONFLICT(section_id, student_pk, term_id) DO UPDATE SET status = 'active', end_date = NULL`,
		sectionID, studentID, termID)
	if err != nil {
		return err
	}
	return a.ensureDefaultGradesForStudent(sectionID, termID, studentID)
}

func (a *App) sectionStudents() ([]Student, error) {
	ctx := a.context()
	if ctx.SectionID == 0 || ctx.TermID == 0 {
		return nil, errors.New("set term and section first")
	}
	rows, err := a.db.Query(`
		SELECT students.student_pk, students.first_name, students.last_name,
		       COALESCE(students.chinese_name,''), COALESCE(students.school_student_id,''), COALESCE(students.powerschool_num,'')
		FROM section_enrollments
		JOIN students ON students.student_pk = section_enrollments.student_pk
		WHERE section_enrollments.section_id = ? AND section_enrollments.term_id = ? AND students.status = 'active'
		ORDER BY students.student_pk`, ctx.SectionID, ctx.TermID)
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

func (a *App) studentsForList() ([]Student, string, error) {
	ctx := a.context()
	if ctx.TermID == 0 || ctx.CourseYearID == 0 {
		return nil, "", errors.New("set year, term, and course first")
	}
	if ctx.SectionID != 0 {
		students, err := a.sectionStudents()
		return students, "", err
	}

	rows, err := a.db.Query(`
		SELECT DISTINCT students.student_pk, students.first_name, students.last_name,
		       COALESCE(students.chinese_name,''), COALESCE(students.school_student_id,''), COALESCE(students.powerschool_num,'')
		FROM section_enrollments
		JOIN students ON students.student_pk = section_enrollments.student_pk
		JOIN sections ON sections.section_id = section_enrollments.section_id
		WHERE section_enrollments.term_id = ? AND sections.course_year_id = ? AND students.status = 'active'
		ORDER BY students.student_pk`, ctx.TermID, ctx.CourseYearID)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	var students []Student
	for rows.Next() {
		var student Student
		if err := rows.Scan(&student.ID, &student.FirstName, &student.LastName, &student.ChineseName, &student.SchoolStudentID, &student.PowerSchoolNum); err != nil {
			return nil, "", err
		}
		students = append(students, student)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	return students, "Using all sections in the current course.", nil
}

func (a *App) printNamedIDs(query string) error {
	rows, err := a.db.Query(query)
	if err != nil {
		return err
	}
	defer rows.Close()
	return printRows(a.out, rows)
}
