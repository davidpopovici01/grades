package portalserver

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// SubAssignment is a code-submission assignment published to a course.
type SubAssignment struct {
	ID                int64    `json:"id"`
	CourseYearID      int      `json:"courseYearId"`
	TermID            int      `json:"termId"`
	Title             string   `json:"title"`
	Language          string   `json:"language"`
	Instructions      string   `json:"instructions"`
	DueAt             *string  `json:"dueAt"`
	ExpectedFilenames []string `json:"expectedFilenames"`
	MaxFileBytes      int64    `json:"maxFileBytes"`
	MaxTotalBytes     int64    `json:"maxTotalBytes"`
	LateCapPercent    int      `json:"lateCapPercent"`
	IsOpen            bool     `json:"isOpen"`
	CreatedAt         string   `json:"createdAt"`
}

// SubTest is an uploaded harness file for an assignment.
type SubTest struct {
	ID           int64  `json:"id"`
	AssignmentID int64  `json:"assignmentId"`
	Name         string `json:"name"`
	Visibility   string `json:"visibility"`
	StoredName   string `json:"-"`
}

// SubSubmission is one student's uploaded attempt.
type SubSubmission struct {
	ID           int64  `json:"id"`
	AssignmentID int64  `json:"assignmentId"`
	StudentPK    int    `json:"studentPk"`
	Attempt      int    `json:"attempt"`
	SubmittedAt  string `json:"submittedAt"`
	IsLate       bool   `json:"isLate"`
	CapPercent   int    `json:"capPercent"`
}

// SubFile is one uploaded file of a submission.
type SubFile struct {
	SubmissionID int64  `json:"submissionId"`
	Filename     string `json:"name"`
	ByteSize     int64  `json:"size"`
}

// SubTestRun is one queued or finished execution of a test against a submission.
type SubTestRun struct {
	ID           int64   `json:"id"`
	SubmissionID int64   `json:"submissionId"`
	TestID       int64   `json:"testId"`
	TestName     string  `json:"testName"`
	Visibility   string  `json:"visibility"`
	Passed       int     `json:"passed"`
	Failed       int     `json:"failed"`
	Output       string  `json:"output"`
	Status       string  `json:"status"`
	TriggeredBy  string  `json:"triggeredBy"`
	QueuedAt     string  `json:"queuedAt"`
	StartedAt    *string `json:"startedAt"`
	FinishedAt   *string `json:"finishedAt"`
}

// SubPlagRun is one plagiarism-detection job for an assignment.
type SubPlagRun struct {
	ID           int64   `json:"id"`
	AssignmentID int64   `json:"assignmentId"`
	Status       string  `json:"status"`
	ReportPath   string  `json:"reportPath"`
	Message      string  `json:"message"`
	CreatedAt    string  `json:"createdAt"`
	FinishedAt   *string `json:"finishedAt"`
}

// SubPlagPair is one pairwise similarity result.
type SubPlagPair struct {
	RunID      int64   `json:"runId"`
	StudentA   int     `json:"studentA"`
	StudentB   int     `json:"studentB"`
	Similarity float64 `json:"similarity"`
}

// dbScanner is satisfied by both *sql.Row and *sql.Rows.
type dbScanner interface {
	Scan(dest ...any) error
}

const subAssignmentCols = `id, course_year_id, term_id, title, language, instructions, due_at,
	expected_filenames, max_file_bytes, max_total_bytes, late_cap_percent, is_open, created_at`

func scanSubAssignment(row dbScanner) (*SubAssignment, error) {
	var a SubAssignment
	var dueAt sql.NullString
	var filenames string
	var isOpen int
	err := row.Scan(&a.ID, &a.CourseYearID, &a.TermID, &a.Title, &a.Language, &a.Instructions,
		&dueAt, &filenames, &a.MaxFileBytes, &a.MaxTotalBytes, &a.LateCapPercent, &isOpen, &a.CreatedAt)
	if err != nil {
		return nil, err
	}
	if dueAt.Valid {
		a.DueAt = &dueAt.String
	}
	for _, name := range strings.Split(filenames, "\n") {
		if name = strings.TrimSpace(name); name != "" {
			a.ExpectedFilenames = append(a.ExpectedFilenames, name)
		}
	}
	a.IsOpen = isOpen != 0
	return &a, nil
}

// CreateSubAssignment inserts a new assignment and returns its id.
func (s *Store) CreateSubAssignment(a *SubAssignment) (int64, error) {
	res, err := s.db.Exec(`
		INSERT INTO sub_assignments(course_year_id, term_id, title, language, instructions, due_at,
			expected_filenames, max_file_bytes, max_total_bytes, late_cap_percent, is_open)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.CourseYearID, a.TermID, a.Title, a.Language, a.Instructions, nullString(a.DueAt),
		strings.Join(a.ExpectedFilenames, "\n"), a.MaxFileBytes, a.MaxTotalBytes, a.LateCapPercent, boolInt(a.IsOpen))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateSubAssignment replaces an assignment's editable fields.
func (s *Store) UpdateSubAssignment(a *SubAssignment) error {
	res, err := s.db.Exec(`
		UPDATE sub_assignments SET course_year_id = ?, term_id = ?, title = ?, language = ?,
			instructions = ?, due_at = ?, expected_filenames = ?, max_file_bytes = ?,
			max_total_bytes = ?, late_cap_percent = ?, is_open = ?
		WHERE id = ?`,
		a.CourseYearID, a.TermID, a.Title, a.Language, a.Instructions, nullString(a.DueAt),
		strings.Join(a.ExpectedFilenames, "\n"), a.MaxFileBytes, a.MaxTotalBytes, a.LateCapPercent,
		boolInt(a.IsOpen), a.ID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return fmt.Errorf("assignment not found: %d", a.ID)
	}
	return nil
}

// GetSubAssignment returns the assignment, or nil if not found.
func (s *Store) GetSubAssignment(id int64) (*SubAssignment, error) {
	a, err := scanSubAssignment(s.db.QueryRow(
		`SELECT `+subAssignmentCols+` FROM sub_assignments WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return a, nil
}

// ListAllSubAssignments returns every assignment, newest first.
func (s *Store) ListAllSubAssignments() ([]SubAssignment, error) {
	return s.listSubAssignments(`SELECT ` + subAssignmentCols + ` FROM sub_assignments ORDER BY id DESC`)
}

// ListSubAssignmentsForCourse returns a course's assignments, newest first.
func (s *Store) ListSubAssignmentsForCourse(courseYearID, termID int) ([]SubAssignment, error) {
	return s.listSubAssignments(`SELECT `+subAssignmentCols+` FROM sub_assignments
		WHERE course_year_id = ? AND term_id = ? ORDER BY id DESC`, courseYearID, termID)
}

func (s *Store) listSubAssignments(query string, args ...any) ([]SubAssignment, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	assignments := []SubAssignment{}
	for rows.Next() {
		a, err := scanSubAssignment(rows)
		if err != nil {
			return nil, err
		}
		assignments = append(assignments, *a)
	}
	return assignments, rows.Err()
}

// DeleteSubAssignment removes an assignment and all its tests, submissions,
// files, runs, and plagiarism results in one transaction.
func (s *Store) DeleteSubAssignment(id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	statements := []string{
		`DELETE FROM sub_plag_pairs WHERE run_id IN (SELECT id FROM sub_plag_runs WHERE assignment_id = ?)`,
		`DELETE FROM sub_plag_runs WHERE assignment_id = ?`,
		`DELETE FROM sub_test_runs WHERE submission_id IN (SELECT id FROM sub_submissions WHERE assignment_id = ?)`,
		`DELETE FROM sub_files WHERE submission_id IN (SELECT id FROM sub_submissions WHERE assignment_id = ?)`,
		`DELETE FROM sub_submissions WHERE assignment_id = ?`,
		`DELETE FROM sub_tests WHERE assignment_id = ?`,
		`DELETE FROM sub_assignments WHERE id = ?`,
	}
	for _, stmt := range statements {
		if _, err := tx.Exec(stmt, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// CreateSubTest inserts a test and returns its id.
func (s *Store) CreateSubTest(t *SubTest) (int64, error) {
	res, err := s.db.Exec(`
		INSERT INTO sub_tests(assignment_id, name, visibility, stored_name) VALUES (?, ?, ?, ?)`,
		t.AssignmentID, t.Name, t.Visibility, t.StoredName)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// ListSubTests returns an assignment's tests in creation order.
func (s *Store) ListSubTests(assignmentID int64) ([]SubTest, error) {
	rows, err := s.db.Query(`
		SELECT id, assignment_id, name, visibility, stored_name
		FROM sub_tests WHERE assignment_id = ? ORDER BY id`, assignmentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tests := []SubTest{}
	for rows.Next() {
		var t SubTest
		if err := rows.Scan(&t.ID, &t.AssignmentID, &t.Name, &t.Visibility, &t.StoredName); err != nil {
			return nil, err
		}
		tests = append(tests, t)
	}
	return tests, rows.Err()
}

// CountPublicSubTests returns how many public tests an assignment has.
func (s *Store) CountPublicSubTests(assignmentID int64) (int, error) {
	var n int
	err := s.db.QueryRow(`
		SELECT COUNT(*) FROM sub_tests WHERE assignment_id = ? AND visibility = 'public'`, assignmentID).Scan(&n)
	return n, err
}

// GetSubTest returns the test, or nil if not found.
func (s *Store) GetSubTest(id int64) (*SubTest, error) {
	var t SubTest
	err := s.db.QueryRow(`
		SELECT id, assignment_id, name, visibility, stored_name FROM sub_tests WHERE id = ?`, id).
		Scan(&t.ID, &t.AssignmentID, &t.Name, &t.Visibility, &t.StoredName)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// DeleteSubTest removes a test row; the caller removes the stored file.
func (s *Store) DeleteSubTest(id int64) error {
	_, err := s.db.Exec(`DELETE FROM sub_tests WHERE id = ?`, id)
	return err
}

// CreateSubSubmission inserts a submission and its files atomically, assigning
// the next attempt number for this student and assignment.
func (s *Store) CreateSubSubmission(sub *SubSubmission, files []SubFile) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	var attempt int
	if err := tx.QueryRow(`
		SELECT COALESCE(MAX(attempt), 0) + 1 FROM sub_submissions
		WHERE assignment_id = ? AND student_pk = ?`, sub.AssignmentID, sub.StudentPK).Scan(&attempt); err != nil {
		return 0, err
	}
	sub.Attempt = attempt

	res, err := tx.Exec(`
		INSERT INTO sub_submissions(assignment_id, student_pk, attempt, submitted_at, is_late, cap_percent)
		VALUES (?, ?, ?, ?, ?, ?)`,
		sub.AssignmentID, sub.StudentPK, attempt, sub.SubmittedAt, boolInt(sub.IsLate), sub.CapPercent)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	sub.ID = id
	for _, f := range files {
		if _, err := tx.Exec(`
			INSERT INTO sub_files(submission_id, filename, byte_size) VALUES (?, ?, ?)`,
			id, f.Filename, f.ByteSize); err != nil {
			return 0, err
		}
	}
	return id, tx.Commit()
}

// DeleteSubSubmission removes a submission with its files and runs.
func (s *Store) DeleteSubSubmission(id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	for _, stmt := range []string{
		`DELETE FROM sub_test_runs WHERE submission_id = ?`,
		`DELETE FROM sub_files WHERE submission_id = ?`,
		`DELETE FROM sub_submissions WHERE id = ?`,
	} {
		if _, err := tx.Exec(stmt, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func scanSubSubmission(row dbScanner) (*SubSubmission, error) {
	var sub SubSubmission
	var isLate int
	err := row.Scan(&sub.ID, &sub.AssignmentID, &sub.StudentPK, &sub.Attempt,
		&sub.SubmittedAt, &isLate, &sub.CapPercent)
	if err != nil {
		return nil, err
	}
	sub.IsLate = isLate != 0
	return &sub, nil
}

// GetSubSubmission returns the submission, or nil if not found.
func (s *Store) GetSubSubmission(id int64) (*SubSubmission, error) {
	sub, err := scanSubSubmission(s.db.QueryRow(`
		SELECT id, assignment_id, student_pk, attempt, submitted_at, is_late, cap_percent
		FROM sub_submissions WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return sub, nil
}

// ListSubSubmissions returns a student's submissions for an assignment,
// newest first.
func (s *Store) ListSubSubmissions(assignmentID int64, studentPK int) ([]SubSubmission, error) {
	rows, err := s.db.Query(`
		SELECT id, assignment_id, student_pk, attempt, submitted_at, is_late, cap_percent
		FROM sub_submissions WHERE assignment_id = ? AND student_pk = ? ORDER BY id DESC`,
		assignmentID, studentPK)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	subs := []SubSubmission{}
	for rows.Next() {
		sub, err := scanSubSubmission(rows)
		if err != nil {
			return nil, err
		}
		subs = append(subs, *sub)
	}
	return subs, rows.Err()
}

// LatestSubSubmission returns the student's most recent submission, or nil.
func (s *Store) LatestSubSubmission(assignmentID int64, studentPK int) (*SubSubmission, error) {
	sub, err := scanSubSubmission(s.db.QueryRow(`
		SELECT id, assignment_id, student_pk, attempt, submitted_at, is_late, cap_percent
		FROM sub_submissions WHERE assignment_id = ? AND student_pk = ? ORDER BY id DESC LIMIT 1`,
		assignmentID, studentPK))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return sub, nil
}

// LatestSubmissionsByAssignment returns each submitting student's most recent
// submission, keyed by student pk.
func (s *Store) LatestSubmissionsByAssignment(assignmentID int64) (map[int]*SubSubmission, error) {
	rows, err := s.db.Query(`
		SELECT id, assignment_id, student_pk, attempt, submitted_at, is_late, cap_percent
		FROM sub_submissions
		WHERE assignment_id = ?
		AND id IN (SELECT MAX(id) FROM sub_submissions WHERE assignment_id = ? GROUP BY student_pk)`,
		assignmentID, assignmentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	latest := map[int]*SubSubmission{}
	for rows.Next() {
		sub, err := scanSubSubmission(rows)
		if err != nil {
			return nil, err
		}
		latest[sub.StudentPK] = sub
	}
	return latest, rows.Err()
}

// ListSubFiles returns the files of a submission.
func (s *Store) ListSubFiles(submissionID int64) ([]SubFile, error) {
	rows, err := s.db.Query(`
		SELECT submission_id, filename, byte_size FROM sub_files
		WHERE submission_id = ? ORDER BY filename`, submissionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	files := []SubFile{}
	for rows.Next() {
		var f SubFile
		if err := rows.Scan(&f.SubmissionID, &f.Filename, &f.ByteSize); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

// CreateSubTestRun inserts a queued run row and returns its id.
func (s *Store) CreateSubTestRun(r *SubTestRun) (int64, error) {
	res, err := s.db.Exec(`
		INSERT INTO sub_test_runs(submission_id, test_id, visibility, triggered_by)
		VALUES (?, ?, ?, ?)`,
		r.SubmissionID, r.TestID, r.Visibility, r.TriggeredBy)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// GetSubTestRun returns the run, or nil if not found.
func (s *Store) GetSubTestRun(id int64) (*SubTestRun, error) {
	r, err := scanSubTestRun(s.db.QueryRow(`
		SELECT r.id, r.submission_id, r.test_id, COALESCE(t.name, ''), r.visibility, r.passed, r.failed,
			r.output, r.status, r.triggered_by, r.queued_at, r.started_at, r.finished_at
		FROM sub_test_runs r LEFT JOIN sub_tests t ON t.id = r.test_id
		WHERE r.id = ?`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return r, nil
}

const subTestRunSelect = `
	SELECT r.id, r.submission_id, r.test_id, COALESCE(t.name, ''), r.visibility, r.passed, r.failed,
		r.output, r.status, r.triggered_by, r.queued_at, r.started_at, r.finished_at
	FROM sub_test_runs r LEFT JOIN sub_tests t ON t.id = r.test_id`

func scanSubTestRun(row dbScanner) (*SubTestRun, error) {
	var r SubTestRun
	var startedAt, finishedAt sql.NullString
	err := row.Scan(&r.ID, &r.SubmissionID, &r.TestID, &r.TestName, &r.Visibility, &r.Passed, &r.Failed,
		&r.Output, &r.Status, &r.TriggeredBy, &r.QueuedAt, &startedAt, &finishedAt)
	if err != nil {
		return nil, err
	}
	if startedAt.Valid {
		r.StartedAt = &startedAt.String
	}
	if finishedAt.Valid {
		r.FinishedAt = &finishedAt.String
	}
	return &r, nil
}

// RunsForSubmission returns all runs of a submission, newest first.
func (s *Store) RunsForSubmission(submissionID int64) ([]SubTestRun, error) {
	rows, err := s.db.Query(subTestRunSelect+`
		WHERE r.submission_id = ? ORDER BY r.id DESC`, submissionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	runs := []SubTestRun{}
	for rows.Next() {
		r, err := scanSubTestRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, *r)
	}
	return runs, rows.Err()
}

// LatestRunsForSubmission returns the newest run per test for a submission;
// visibility ("" for all) restricts the result. Runs whose test was deleted
// are excluded so they never contribute to scores.
func (s *Store) LatestRunsForSubmission(submissionID int64, visibility string) ([]SubTestRun, error) {
	query := subTestRunSelect + `
		WHERE r.submission_id = ? AND t.id IS NOT NULL
		AND r.id IN (SELECT MAX(id) FROM sub_test_runs WHERE submission_id = ? GROUP BY test_id)`
	args := []any{submissionID, submissionID}
	if visibility != "" {
		query += ` AND r.visibility = ?`
		args = append(args, visibility)
	}
	rows, err := s.db.Query(query+` ORDER BY r.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	runs := []SubTestRun{}
	for rows.Next() {
		r, err := scanSubTestRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, *r)
	}
	return runs, rows.Err()
}

// StartTestRun marks a queued run as running.
func (s *Store) StartTestRun(id int64) error {
	_, err := s.db.Exec(`
		UPDATE sub_test_runs SET status = 'running',
			started_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')
		WHERE id = ? AND status = 'queued'`, id)
	return err
}

// FinishTestRun records the outcome of a run.
func (s *Store) FinishTestRun(id int64, status string, passed, failed int, output string) error {
	_, err := s.db.Exec(`
		UPDATE sub_test_runs SET status = ?, passed = ?, failed = ?, output = ?,
			finished_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')
		WHERE id = ?`, status, passed, failed, output, id)
	return err
}

// StudentRunState returns the most recent student-triggered run enqueue time
// and the number of student-triggered runs still queued or running.
func (s *Store) StudentRunState(assignmentID int64, studentPK int) (lastQueued time.Time, active int, err error) {
	var last sql.NullString
	err = s.db.QueryRow(`
		SELECT MAX(r.queued_at),
			COALESCE(SUM(CASE WHEN r.status IN ('queued','running') THEN 1 ELSE 0 END), 0)
		FROM sub_test_runs r
		JOIN sub_submissions sub ON sub.id = r.submission_id
		WHERE sub.assignment_id = ? AND sub.student_pk = ? AND r.triggered_by = 'student'`,
		assignmentID, studentPK).Scan(&last, &active)
	if err != nil {
		return time.Time{}, 0, err
	}
	if last.Valid {
		if t, perr := time.Parse(time.RFC3339Nano, last.String); perr == nil {
			lastQueued = t
		}
	}
	return lastQueued, active, nil
}

// CreateSubPlagRun inserts a queued plagiarism run and returns its id.
func (s *Store) CreateSubPlagRun(assignmentID int64) (int64, error) {
	res, err := s.db.Exec(`INSERT INTO sub_plag_runs(assignment_id) VALUES (?)`, assignmentID)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// GetSubPlagRun returns the run, or nil if not found.
func (s *Store) GetSubPlagRun(id int64) (*SubPlagRun, error) {
	r, err := scanSubPlagRun(s.db.QueryRow(`
		SELECT id, assignment_id, status, report_path, message, created_at, finished_at
		FROM sub_plag_runs WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return r, nil
}

// LatestSubPlagRun returns the newest plagiarism run for an assignment, or nil.
func (s *Store) LatestSubPlagRun(assignmentID int64) (*SubPlagRun, error) {
	r, err := scanSubPlagRun(s.db.QueryRow(`
		SELECT id, assignment_id, status, report_path, message, created_at, finished_at
		FROM sub_plag_runs WHERE assignment_id = ? ORDER BY id DESC LIMIT 1`, assignmentID))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return r, nil
}

func scanSubPlagRun(row dbScanner) (*SubPlagRun, error) {
	var r SubPlagRun
	var finishedAt sql.NullString
	err := row.Scan(&r.ID, &r.AssignmentID, &r.Status, &r.ReportPath, &r.Message, &r.CreatedAt, &finishedAt)
	if err != nil {
		return nil, err
	}
	if finishedAt.Valid {
		r.FinishedAt = &finishedAt.String
	}
	return &r, nil
}

// SetSubPlagRunStatus updates a plagiarism run; finished_at is set for
// terminal statuses. message carries a short admin-visible reason for
// error/unavailable outcomes ("" to clear).
func (s *Store) SetSubPlagRunStatus(id int64, status, reportPath, message string) error {
	_, err := s.db.Exec(`
		UPDATE sub_plag_runs SET status = ?, report_path = ?, message = ?,
			finished_at = CASE WHEN ? IN ('done','error','unavailable')
				THEN strftime('%Y-%m-%dT%H:%M:%fZ','now') ELSE finished_at END
		WHERE id = ?`, status, reportPath, message, status, id)
	return err
}

// ReplaceSubPlagPairs stores the pairs of a run, replacing any earlier ones.
func (s *Store) ReplaceSubPlagPairs(runID int64, pairs []SubPlagPair) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`DELETE FROM sub_plag_pairs WHERE run_id = ?`, runID); err != nil {
		return err
	}
	for _, p := range pairs {
		if _, err := tx.Exec(`
			INSERT INTO sub_plag_pairs(run_id, student_a, student_b, similarity) VALUES (?, ?, ?, ?)`,
			runID, p.StudentA, p.StudentB, p.Similarity); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListSubPlagPairs returns the pairs of a run, most similar first.
func (s *Store) ListSubPlagPairs(runID int64) ([]SubPlagPair, error) {
	rows, err := s.db.Query(`
		SELECT run_id, student_a, student_b, similarity FROM sub_plag_pairs
		WHERE run_id = ? ORDER BY similarity DESC`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	pairs := []SubPlagPair{}
	for rows.Next() {
		var p SubPlagPair
		if err := rows.Scan(&p.RunID, &p.StudentA, &p.StudentB, &p.Similarity); err != nil {
			return nil, err
		}
		pairs = append(pairs, p)
	}
	return pairs, rows.Err()
}

func nullString(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
