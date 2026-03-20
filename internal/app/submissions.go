package app

import (
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type SubmissionPolicy struct {
	AssignmentID      int
	AssignmentKind    string
	Enabled           bool
	DueAt             string
	LateAllowed       bool
	LateCapPercent    float64
	ExpectedFileCount int
	ExpectedFilenames []string
	Instructions      string
}

type portalSubmissionAssignment struct {
	AssignmentID      int                    `json:"assignmentId"`
	Title             string                 `json:"title"`
	AssignmentKind    string                 `json:"assignmentKind"`
	CategoryName      string                 `json:"categoryName"`
	DueAt             string                 `json:"dueAt,omitempty"`
	LateAllowed       bool                   `json:"lateAllowed"`
	LateCapPercent    float64                `json:"lateCapPercent"`
	ExpectedFileCount int                    `json:"expectedFileCount"`
	ExpectedFilenames []string               `json:"expectedFilenames"`
	Instructions      string                 `json:"instructions"`
	LastSubmittedAt   string                 `json:"lastSubmittedAt,omitempty"`
	LastStatus        string                 `json:"lastStatus"`
	AttemptCount      int                    `json:"attemptCount"`
	GradeLabel        string                 `json:"gradeLabel,omitempty"`
	Files             []portalSubmissionFile `json:"files"`
}

type portalSubmissionFile struct {
	OriginalName string `json:"originalName"`
	ByteSize     int64  `json:"byteSize"`
}

type submissionQueueRow struct {
	Student     Student
	SubmittedAt string
	Status      string
	GradeLabel  string
	IsLate      bool
	AttemptID   int
}

func (a *App) ConfigureCurrentAssignmentSubmission(kind, dueAt string, lateAllowed bool, lateCap float64, expectedCount int, expectedFilenames []string, instructions string) error {
	ctx := a.context()
	if ctx.AssignmentID == 0 {
		return errors.New("set an assignment first")
	}
	kind = strings.TrimSpace(strings.ToLower(kind))
	if kind == "" {
		kind = "submission"
	}
	if expectedCount <= 0 {
		return errors.New("expected file count must be greater than 0")
	}
	if lateCap < 0 || lateCap > 100 {
		return errors.New("late cap percent must be between 0 and 100")
	}
	if dueAt != "" {
		if _, err := time.Parse(time.RFC3339, dueAt); err != nil {
			return fmt.Errorf("due time must be RFC3339, for example 2026-03-20T23:59:00Z")
		}
	}
	_, err := a.db.Exec(`
		INSERT INTO submission_policies(assignment_id, assignment_kind, enabled, due_at, late_allowed, late_cap_percent, expected_file_count, expected_filenames, instructions, updated_at)
		VALUES (?, ?, 1, ?, ?, ?, ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%fZ','now'))
		ON CONFLICT(assignment_id) DO UPDATE SET
			assignment_kind = excluded.assignment_kind,
			enabled = excluded.enabled,
			due_at = excluded.due_at,
			late_allowed = excluded.late_allowed,
			late_cap_percent = excluded.late_cap_percent,
			expected_file_count = excluded.expected_file_count,
			expected_filenames = excluded.expected_filenames,
			instructions = excluded.instructions,
			updated_at = excluded.updated_at`,
		ctx.AssignmentID, kind, nullableString(dueAt), boolToInt(lateAllowed), lateCap, expectedCount, strings.Join(expectedFilenames, "\n"), instructions)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Configured submissions for assignment %d\n", ctx.AssignmentID)
	fmt.Fprintf(a.out, "Submission type:\t%s\n", kind)
	if dueAt != "" {
		fmt.Fprintf(a.out, "Due at:\t%s\n", dueAt)
	}
	fmt.Fprintf(a.out, "Expected files:\t%d\n", expectedCount)
	if len(expectedFilenames) > 0 {
		fmt.Fprintf(a.out, "Allowed names:\t%s\n", strings.Join(expectedFilenames, ", "))
	}
	if lateAllowed {
		fmt.Fprintf(a.out, "Late policy:\tallowed, capped at %.0f%%\n", lateCap)
	} else {
		fmt.Fprintln(a.out, "Late policy:\tnot allowed")
	}
	return nil
}

func (a *App) ShowCurrentAssignmentSubmissionPolicy() error {
	ctx := a.context()
	if ctx.AssignmentID == 0 {
		return errors.New("set an assignment first")
	}
	policy, err := a.submissionPolicyByAssignment(ctx.AssignmentID)
	if err != nil {
		return err
	}
	if policy == nil || !policy.Enabled {
		fmt.Fprintln(a.out, "Submissions are not configured for the current assignment.")
		return nil
	}
	fmt.Fprintln(a.out, "Submission policy")
	fmt.Fprintf(a.out, "Type:\t%s\n", fallback(policy.AssignmentKind))
	fmt.Fprintf(a.out, "Due at:\t%s\n", fallback(policy.DueAt))
	fmt.Fprintf(a.out, "Late allowed:\t%t\n", policy.LateAllowed)
	fmt.Fprintf(a.out, "Late cap:\t%.0f%%\n", policy.LateCapPercent)
	fmt.Fprintf(a.out, "Expected files:\t%d\n", policy.ExpectedFileCount)
	fmt.Fprintf(a.out, "Allowed names:\t%s\n", fallback(strings.Join(policy.ExpectedFilenames, ", ")))
	fmt.Fprintf(a.out, "Instructions:\t%s\n", fallback(policy.Instructions))
	return nil
}

func (a *App) ExportCurrentAssignmentSubmissions(dir string) error {
	ctx := a.context()
	if ctx.AssignmentID == 0 {
		return errors.New("set an assignment first")
	}
	if strings.TrimSpace(dir) == "" {
		dir = filepath.Join(a.homeDir, "..", "gradesSubmissions", fmt.Sprintf("assignment_%d", ctx.AssignmentID))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	rows, err := a.db.Query(`
		SELECT submission_attempts.attempt_id,
		       submission_attempts.student_pk,
		       students.first_name,
		       students.last_name,
		       submission_attempts.submitted_at,
		       submission_attempts.is_late,
		       submission_attempts.cap_percent
		FROM submission_attempts
		JOIN students ON students.student_pk = submission_attempts.student_pk
		WHERE submission_attempts.assignment_id = ?
		ORDER BY submission_attempts.student_pk, submission_attempts.submitted_at DESC`, ctx.AssignmentID)
	if err != nil {
		return err
	}
	defer rows.Close()
	latest := map[int]struct {
		AttemptID   int
		FirstName   string
		LastName    string
		SubmittedAt string
		IsLate      int
		CapPercent  float64
	}{}
	for rows.Next() {
		var studentID int
		item := struct {
			AttemptID   int
			FirstName   string
			LastName    string
			SubmittedAt string
			IsLate      int
			CapPercent  float64
		}{}
		if err := rows.Scan(&item.AttemptID, &studentID, &item.FirstName, &item.LastName, &item.SubmittedAt, &item.IsLate, &item.CapPercent); err != nil {
			return err
		}
		if _, exists := latest[studentID]; !exists {
			latest[studentID] = item
		}
	}
	manifestPath := filepath.Join(dir, "manifest.csv")
	manifestFile, err := os.Create(manifestPath)
	if err != nil {
		return err
	}
	defer manifestFile.Close()
	writer := csv.NewWriter(manifestFile)
	_ = writer.Write([]string{"student", "submitted_at", "late", "cap_percent", "file"})
	exported := 0
	for _, attempt := range latest {
		files, err := a.submissionFilesForAttempt(attempt.AttemptID)
		if err != nil {
			return err
		}
		for _, file := range files {
			data, err := os.ReadFile(filepath.Join(a.homeDir, file.RelativePath))
			if err != nil {
				return err
			}
			targetName := fmt.Sprintf("%s_%s_%s__%s", sanitizeFilename(attempt.LastName), sanitizeFilename(attempt.FirstName), attempt.SubmittedAt[:10], sanitizeFilename(file.OriginalName))
			if err := os.WriteFile(filepath.Join(dir, targetName), data, 0o644); err != nil {
				return err
			}
			_ = writer.Write([]string{
				strings.TrimSpace(attempt.FirstName + " " + attempt.LastName),
				attempt.SubmittedAt,
				strconv.FormatBool(attempt.IsLate != 0),
				fmt.Sprintf("%.0f", attempt.CapPercent),
				targetName,
			})
			exported++
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Exported %d submission file(s) to %s\n", exported, dir)
	return nil
}

func (a *App) ShowCurrentAssignmentSubmissionQueue() error {
	rows, err := a.currentAssignmentSubmissionQueue()
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		fmt.Fprintln(a.out, "No students found.")
		return nil
	}
	for _, row := range rows {
		late := ""
		if row.IsLate {
			late = "late"
		}
		fmt.Fprintf(a.out, "%s %s\t%s\t%s\t%s\t%s\n", row.Student.FirstName, row.Student.LastName, row.Status, fallback(row.SubmittedAt), fallback(row.GradeLabel), late)
	}
	return nil
}

func (a *App) RunCurrentAssignmentSubmissionMarkingWizard() error {
	ctx := a.context()
	if ctx.AssignmentID == 0 || ctx.CourseYearID == 0 || ctx.TermID == 0 {
		return errors.New("set year, term, course, and assignment first")
	}
	rows, err := a.currentAssignmentSubmissionQueue()
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		fmt.Fprintln(a.out, "No students found.")
		return nil
	}
	exportFirst, err := a.promptYesNo("Export latest submissions before marking?", true)
	if err != nil {
		return err
	}
	if exportFirst {
		if err := a.ExportCurrentAssignmentSubmissions(""); err != nil {
			return err
		}
	}
	for _, row := range rows {
		if row.Status == "Not submitted" || row.Status == "Graded" {
			continue
		}
		fmt.Fprintf(a.out, "\n%s %s\n", row.Student.FirstName, row.Student.LastName)
		fmt.Fprintf(a.out, "Submitted:\t%s\n", fallback(row.SubmittedAt))
		if row.IsLate {
			fmt.Fprintln(a.out, "Status:\tSubmitted late")
		}
		if row.GradeLabel != "" {
			fmt.Fprintf(a.out, "Current grade:\t%s\n", row.GradeLabel)
		}
		scoreRaw, err := a.promptOptional("Score or flag input (blank to skip)")
		if err != nil {
			return err
		}
		if strings.TrimSpace(scoreRaw) == "" {
			continue
		}
		entry, err := parseGradeInput(scoreRaw)
		if err != nil {
			return err
		}
		prev, err := a.currentGrade(ctx.AssignmentID, row.Student.ID)
		if err != nil {
			return err
		}
		if err := a.saveGrade(ctx.AssignmentID, row.Student.ID, entry, prev); err != nil {
			return err
		}
		fmt.Fprintf(a.out, "Recorded %s for %s %s\n", formatGradeValue(entry), row.Student.FirstName, row.Student.LastName)
	}
	return nil
}

func (a *App) currentAssignmentSubmissionQueue() ([]submissionQueueRow, error) {
	ctx := a.context()
	policy, err := a.submissionPolicyByAssignment(ctx.AssignmentID)
	if err != nil {
		return nil, err
	}
	if policy == nil || !policy.Enabled {
		return nil, errors.New("submissions are not configured for the current assignment")
	}
	students, err := a.studentsForCourseTerm(ctx.CourseYearID, ctx.TermID, false)
	if err != nil {
		return nil, err
	}
	var rows []submissionQueueRow
	for _, student := range students {
		row := submissionQueueRow{Student: student, Status: "Not submitted"}
		var attemptID int
		var submittedAt string
		var isLate int
		err := a.db.QueryRow(`
			SELECT attempt_id, submitted_at, is_late
			FROM submission_attempts
			WHERE assignment_id = ? AND student_pk = ?
			ORDER BY submitted_at DESC
			LIMIT 1`, ctx.AssignmentID, student.ID).Scan(&attemptID, &submittedAt, &isLate)
		if err == nil {
			row.AttemptID = attemptID
			row.SubmittedAt = submittedAt
			row.IsLate = isLate != 0
			if row.IsLate {
				row.Status = "Submitted late"
			} else {
				row.Status = "Submitted"
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		record, err := a.currentGrade(ctx.AssignmentID, student.ID)
		if err != nil {
			return nil, err
		}
		if submissionHasTeacherGrade(record) {
			row.GradeLabel = displayGradePlain(*record)
			row.Status = "Graded"
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Student.LastName == rows[j].Student.LastName {
			return rows[i].Student.FirstName < rows[j].Student.FirstName
		}
		return rows[i].Student.LastName < rows[j].Student.LastName
	})
	return rows, nil
}

func (a *App) submissionPolicyByAssignment(assignmentID int) (*SubmissionPolicy, error) {
	var policy SubmissionPolicy
	var dueAt sql.NullString
	var expectedNames, instructions string
	var enabled, lateAllowed int
	err := a.db.QueryRow(`
		SELECT assignment_id, assignment_kind, enabled, due_at, late_allowed, late_cap_percent, expected_file_count, expected_filenames, instructions
		FROM submission_policies
		WHERE assignment_id = ?`, assignmentID).
		Scan(&policy.AssignmentID, &policy.AssignmentKind, &enabled, &dueAt, &lateAllowed, &policy.LateCapPercent, &policy.ExpectedFileCount, &expectedNames, &instructions)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	policy.Enabled = enabled != 0
	policy.LateAllowed = lateAllowed != 0
	if dueAt.Valid {
		policy.DueAt = dueAt.String
	}
	policy.ExpectedFilenames = splitSubmissionFilenames(expectedNames)
	policy.Instructions = instructions
	return &policy, nil
}

func splitSubmissionFilenames(raw string) []string {
	var out []string
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

type submissionFileRecord struct {
	OriginalName string
	StoredName   string
	RelativePath string
	ByteSize     int64
}

func (a *App) submissionFilesForAttempt(attemptID int) ([]submissionFileRecord, error) {
	rows, err := a.db.Query(`SELECT original_name, stored_name, relative_path, byte_size FROM submission_files WHERE attempt_id = ? ORDER BY submission_file_id`, attemptID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []submissionFileRecord
	for rows.Next() {
		var item submissionFileRecord
		if err := rows.Scan(&item.OriginalName, &item.StoredName, &item.RelativePath, &item.ByteSize); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (a *App) submissionAssignmentsForStudent(studentID, courseYearID, termID int) ([]portalSubmissionAssignment, error) {
	rows, err := a.db.Query(`
		SELECT assignments.assignment_id, assignments.title, categories.name, submission_policies.assignment_kind,
		       submission_policies.due_at, submission_policies.late_allowed, submission_policies.late_cap_percent,
		       submission_policies.expected_file_count, submission_policies.expected_filenames, submission_policies.instructions
		FROM assignments
		JOIN categories ON categories.category_id = assignments.category_id
		JOIN submission_policies ON submission_policies.assignment_id = assignments.assignment_id
		WHERE assignments.course_year_id = ? AND assignments.term_id = ? AND submission_policies.enabled = 1
		ORDER BY assignments.assignment_id`, courseYearID, termID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []portalSubmissionAssignment
	for rows.Next() {
		var item portalSubmissionAssignment
		var dueAt sql.NullString
		var lateAllowed int
		var expectedRaw string
		if err := rows.Scan(&item.AssignmentID, &item.Title, &item.CategoryName, &item.AssignmentKind, &dueAt, &lateAllowed, &item.LateCapPercent, &item.ExpectedFileCount, &expectedRaw, &item.Instructions); err != nil {
			return nil, err
		}
		if dueAt.Valid {
			item.DueAt = dueAt.String
		}
		item.LateAllowed = lateAllowed != 0
		item.ExpectedFilenames = splitSubmissionFilenames(expectedRaw)
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for idx := range out {
		if err := a.fillStudentSubmissionStatus(&out[idx], studentID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (a *App) fillStudentSubmissionStatus(item *portalSubmissionAssignment, studentID int) error {
	record, err := a.currentGrade(item.AssignmentID, studentID)
	if err != nil {
		return err
	}
	graded := submissionHasTeacherGrade(record)
	if graded {
		item.GradeLabel = displayGradePlain(*record)
	}
	rows, err := a.db.Query(`
		SELECT attempt_id, submitted_at, is_late
		FROM submission_attempts
		WHERE assignment_id = ? AND student_pk = ?
		ORDER BY submitted_at DESC`, item.AssignmentID, studentID)
	if err != nil {
		return err
	}
	defer rows.Close()
	type attemptRow struct {
		AttemptID   int
		SubmittedAt string
		IsLate      int
	}
	var attempts []attemptRow
	first := true
	for rows.Next() {
		var attempt attemptRow
		if err := rows.Scan(&attempt.AttemptID, &attempt.SubmittedAt, &attempt.IsLate); err != nil {
			return err
		}
		attempts = append(attempts, attempt)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, attempt := range attempts {
		item.AttemptCount++
		if first {
			item.LastSubmittedAt = attempt.SubmittedAt
			if attempt.IsLate != 0 {
				item.LastStatus = "Submitted late"
			} else {
				item.LastStatus = "Submitted"
			}
			files, err := a.submissionFilesForAttempt(attempt.AttemptID)
			if err != nil {
				return err
			}
			for _, file := range files {
				item.Files = append(item.Files, portalSubmissionFile{OriginalName: file.OriginalName, ByteSize: file.ByteSize})
			}
			first = false
		}
	}
	if item.AttemptCount == 0 {
		item.LastStatus = "Not submitted"
	} else if graded {
		item.LastStatus = "Graded"
	}
	return nil
}

func submissionHasTeacherGrade(record *GradeRecord) bool {
	if record == nil {
		return false
	}
	if record.Score.Valid && record.Flags&flagMissing == 0 {
		return true
	}
	return (record.Flags &^ flagMissing) != 0
}

func (a *App) SubmitAssignmentFiles(studentID, assignmentID int, files []*multipart.FileHeader) error {
	policy, err := a.submissionPolicyByAssignment(assignmentID)
	if err != nil {
		return err
	}
	if policy == nil || !policy.Enabled {
		return errors.New("submissions are not enabled for this assignment")
	}
	if len(files) == 0 {
		return errors.New("attach at least one file")
	}
	if policy.ExpectedFileCount > 0 && len(files) != policy.ExpectedFileCount {
		return fmt.Errorf("expected %d file(s)", policy.ExpectedFileCount)
	}
	if len(policy.ExpectedFilenames) > 0 {
		allowed := map[string]bool{}
		for _, name := range policy.ExpectedFilenames {
			allowed[strings.ToLower(name)] = true
		}
		for _, file := range files {
			if !allowed[strings.ToLower(file.Filename)] {
				return fmt.Errorf("unexpected filename: %s", file.Filename)
			}
		}
	}
	submittedAt := time.Now().UTC()
	isLate := false
	capPercent := 100.0
	if policy.DueAt != "" {
		dueAt, err := time.Parse(time.RFC3339, policy.DueAt)
		if err == nil && submittedAt.After(dueAt) {
			if !policy.LateAllowed {
				return errors.New("late submissions are not allowed")
			}
			isLate = true
			capPercent = policy.LateCapPercent
		}
	}
	result, err := a.db.Exec(`
		INSERT INTO submission_attempts(assignment_id, student_pk, submitted_at, is_late, cap_percent)
		VALUES (?, ?, ?, ?, ?)`,
		assignmentID, studentID, submittedAt.Format(time.RFC3339), boolToInt(isLate), capPercent)
	if err != nil {
		return err
	}
	attemptID, err := result.LastInsertId()
	if err != nil {
		return err
	}
	root := filepath.Join(a.homeDir, "portalUploads", fmt.Sprintf("assignment_%d", assignmentID), fmt.Sprintf("student_%d", studentID), fmt.Sprintf("attempt_%d", attemptID))
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	for idx, header := range files {
		source, err := header.Open()
		if err != nil {
			return err
		}
		defer source.Close()
		stored := fmt.Sprintf("%02d_%s", idx+1, sanitizeFilename(header.Filename))
		relativePath := filepath.Join("portalUploads", fmt.Sprintf("assignment_%d", assignmentID), fmt.Sprintf("student_%d", studentID), fmt.Sprintf("attempt_%d", attemptID), stored)
		targetPath := filepath.Join(a.homeDir, relativePath)
		target, err := os.Create(targetPath)
		if err != nil {
			return err
		}
		size, err := io.Copy(target, source)
		closeErr := target.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		if _, err := a.db.Exec(`
			INSERT INTO submission_files(attempt_id, original_name, stored_name, relative_path, byte_size)
			VALUES (?, ?, ?, ?, ?)`, attemptID, header.Filename, stored, relativePath, size); err != nil {
			return err
		}
	}
	return nil
}
