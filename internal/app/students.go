package app

import (
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
	switch detail.SchemeKey {
	case "completion":
		return fmt.Sprintf("%.1f%%", completionPercent(detail.Grade, detail.Grade.PassPercent, detail.Anchor, detail.Lift))
	default:
		return fmt.Sprintf("%.1f%%", recordPercent(detail.Grade, detail.Anchor, detail.Lift))
	}
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
	first = normalizeSpaces(first)
	last = normalizeSpaces(last)
	chineseName = normalizeSpaces(chineseName)
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
