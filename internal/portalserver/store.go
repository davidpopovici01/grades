package portalserver

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/davidpopovici01/grades/internal/portalauth"
	_ "modernc.org/sqlite"
)

// Store manages the SQLite-backed published portal data.
type Store struct {
	db *sql.DB
}

// NewStore opens or creates the published portal database.
func NewStore(dbPath string) (*Store, error) {
	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	conn.SetMaxOpenConns(1)
	conn.SetMaxIdleConns(1)

	if err := conn.Ping(); err != nil {
		_ = conn.Close()
		return nil, err
	}

	store := &Store{db: conn}
	if err := store.migrate(); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return store, nil
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) migrate() error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS published_accounts (
			student_pk            INTEGER PRIMARY KEY,
			username              TEXT NOT NULL UNIQUE,
			password_salt         TEXT NOT NULL,
			password_hash         TEXT NOT NULL,
			must_change_password  INTEGER NOT NULL DEFAULT 1,
			password_changed_at   TEXT NOT NULL,
			created_at            TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
			updated_at            TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
		);`,
		`CREATE TABLE IF NOT EXISTS published_courses (
			course_year_id INTEGER NOT NULL,
			term_id        INTEGER NOT NULL,
			course_name    TEXT NOT NULL,
			course_year_name TEXT NOT NULL DEFAULT '',
			term_name      TEXT NOT NULL,
			published_at   TEXT NOT NULL,
			PRIMARY KEY (course_year_id, term_id)
		);`, `CREATE TABLE IF NOT EXISTS published_students (
			student_pk      INTEGER NOT NULL,
			course_year_id  INTEGER NOT NULL,
			term_id         INTEGER NOT NULL,
			snapshot_json   TEXT NOT NULL,
			published_at    TEXT NOT NULL,
			PRIMARY KEY (student_pk, course_year_id, term_id)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_published_students_course ON published_students(course_year_id, term_id);`,
		`CREATE TABLE IF NOT EXISTS sub_assignments (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			course_year_id INTEGER NOT NULL,
			term_id INTEGER NOT NULL,
			title TEXT NOT NULL,
			language TEXT NOT NULL,
			instructions TEXT NOT NULL DEFAULT '',
			due_at TEXT,
			expected_filenames TEXT NOT NULL DEFAULT '',
			max_file_bytes INTEGER NOT NULL DEFAULT 262144,
			max_total_bytes INTEGER NOT NULL DEFAULT 1048576,
			late_cap_percent INTEGER NOT NULL DEFAULT 90,
			is_open INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
		);`,
		`CREATE INDEX IF NOT EXISTS idx_sub_assignments_course ON sub_assignments(course_year_id, term_id);`,
		`CREATE TABLE IF NOT EXISTS sub_tests (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			assignment_id INTEGER NOT NULL REFERENCES sub_assignments(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			visibility TEXT NOT NULL DEFAULT 'public',
			stored_name TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_sub_tests_assignment ON sub_tests(assignment_id);`,
		`CREATE TABLE IF NOT EXISTS sub_submissions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			assignment_id INTEGER NOT NULL REFERENCES sub_assignments(id) ON DELETE CASCADE,
			student_pk INTEGER NOT NULL,
			attempt INTEGER NOT NULL DEFAULT 1,
			submitted_at TEXT NOT NULL,
			is_late INTEGER NOT NULL DEFAULT 0,
			cap_percent INTEGER NOT NULL DEFAULT 100
		);`,
		`CREATE INDEX IF NOT EXISTS idx_sub_submissions_assignment ON sub_submissions(assignment_id, student_pk);`,
		`CREATE TABLE IF NOT EXISTS sub_files (
			submission_id INTEGER NOT NULL REFERENCES sub_submissions(id) ON DELETE CASCADE,
			filename TEXT NOT NULL,
			byte_size INTEGER NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_sub_files_submission ON sub_files(submission_id);`,
		`CREATE TABLE IF NOT EXISTS sub_test_runs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			submission_id INTEGER NOT NULL REFERENCES sub_submissions(id) ON DELETE CASCADE,
			test_id INTEGER NOT NULL REFERENCES sub_tests(id) ON DELETE CASCADE,
			visibility TEXT NOT NULL DEFAULT 'public',
			passed INTEGER NOT NULL DEFAULT 0,
			failed INTEGER NOT NULL DEFAULT 0,
			output TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'queued',
			triggered_by TEXT NOT NULL DEFAULT 'student',
			queued_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
			started_at TEXT,
			finished_at TEXT
		);`,
		`CREATE INDEX IF NOT EXISTS idx_sub_test_runs_submission ON sub_test_runs(submission_id);`,
		`CREATE TABLE IF NOT EXISTS sub_plag_runs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			assignment_id INTEGER NOT NULL REFERENCES sub_assignments(id) ON DELETE CASCADE,
			status TEXT NOT NULL DEFAULT 'queued',
			report_path TEXT NOT NULL DEFAULT '',
			message TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
			finished_at TEXT
		);`,
		`CREATE INDEX IF NOT EXISTS idx_sub_plag_runs_assignment ON sub_plag_runs(assignment_id);`,
		`CREATE TABLE IF NOT EXISTS sub_plag_pairs (
			run_id INTEGER NOT NULL REFERENCES sub_plag_runs(id) ON DELETE CASCADE,
			student_a INTEGER NOT NULL,
			student_b INTEGER NOT NULL,
			similarity REAL NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_sub_plag_pairs_run ON sub_plag_pairs(run_id);`,
		`CREATE TABLE IF NOT EXISTS activity_events (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			student_pk INTEGER,
			username   TEXT NOT NULL DEFAULT '',
			kind       TEXT NOT NULL,
			detail     TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
		);`,
		`CREATE INDEX IF NOT EXISTS idx_activity_events_created ON activity_events(id DESC);`,
	}
	for _, stmt := range statements {
		if _, err := s.db.Exec(stmt); err != nil {
			return err
		}
	}
	// Databases created before course_year_name existed need the column added.
	if err := s.addColumnIfMissing("published_courses", "course_year_name", `TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	// Databases created before plagiarism run messages existed need the column added.
	if err := s.addColumnIfMissing("sub_plag_runs", "message", `TEXT NOT NULL DEFAULT ''`); err != nil {
		return err
	}
	// Databases created before activity tracking existed need the column added.
	return s.addColumnIfMissing("published_accounts", "last_seen_at", `TEXT`)
}

// addColumnIfMissing adds a column to a table when it does not exist yet.
func (s *Store) addColumnIfMissing(table, column, decl string) error {
	var count int
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, table, column).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	_, err := s.db.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s`, table, column, decl))
	return err
}

// GetAccountByUsername returns the account for a username, or nil if not found.
func (s *Store) GetAccountByUsername(username string) (*portalauth.Account, error) {
	var acc portalauth.Account
	var mustChange int
	err := s.db.QueryRow(`
		SELECT student_pk, username, password_salt, password_hash, must_change_password, password_changed_at
		FROM published_accounts
		WHERE lower(username) = lower(?)`, username).
		Scan(&acc.StudentID, &acc.Username, &acc.PasswordSalt, &acc.PasswordHash, &mustChange, &acc.PasswordChangedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	acc.MustChangePassword = mustChange != 0
	return &acc, nil
}

// GetAccountByStudentID returns the account for a student ID, or nil if not found.
func (s *Store) GetAccountByStudentID(studentID int) (*portalauth.Account, error) {
	var acc portalauth.Account
	var mustChange int
	err := s.db.QueryRow(`
		SELECT student_pk, username, password_salt, password_hash, must_change_password, password_changed_at
		FROM published_accounts
		WHERE student_pk = ?`, studentID).
		Scan(&acc.StudentID, &acc.Username, &acc.PasswordSalt, &acc.PasswordHash, &mustChange, &acc.PasswordChangedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	acc.MustChangePassword = mustChange != 0
	return &acc, nil
}

// dbtx is satisfied by both *sql.DB and *sql.Tx, so the write helpers can run
// standalone or inside a transaction.
type dbtx interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

// PublishCourse atomically applies a publish request: account upserts, the
// course upsert, and all student snapshot upserts happen in a single
// transaction, so a mid-publish failure cannot leave a half-published course.
func (s *Store) PublishCourse(req *PublishRequest) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if len(req.IDMap) > 0 {
		if err := remapStudentIDs(tx, req.IDMap); err != nil {
			return fmt.Errorf("remap student IDs: %w", err)
		}
	}

	for _, acc := range req.Accounts {
		if err := upsertAccount(tx, acc); err != nil {
			return fmt.Errorf("upsert account %d: %w", acc.StudentID, err)
		}
	}

	c := req.Course
	if err := upsertCourse(tx, c.CourseYearID, c.TermID, c.CourseName, c.CourseYearName, c.TermName, c.PublishedAt); err != nil {
		return fmt.Errorf("upsert course %d/%d: %w", c.CourseYearID, c.TermID, err)
	}

	for _, student := range req.Students {
		if err := upsertStudentSnapshot(tx, student.StudentID, c.CourseYearID, c.TermID, student.Snapshot, c.PublishedAt); err != nil {
			return fmt.Errorf("upsert snapshot for student %d: %w", student.StudentID, err)
		}
	}

	keepIDs := make([]int, 0, len(req.Students))
	for _, student := range req.Students {
		keepIDs = append(keepIDs, student.StudentID)
	}
	if err := deleteOtherStudentSnapshots(tx, c.CourseYearID, c.TermID, keepIDs); err != nil {
		return fmt.Errorf("remove stale snapshots for course %d/%d: %w", c.CourseYearID, c.TermID, err)
	}

	return tx.Commit()
}

const studentIDRemapOffset = 1_000_000_000

// studentIDKeyedTables lists every server table keyed by student ID, so a
// renumbering moves accounts, snapshots, and submissions together.
var studentIDKeyedTables = []struct {
	table  string
	column string
}{
	{"published_accounts", "student_pk"},
	{"published_students", "student_pk"},
	{"sub_submissions", "student_pk"},
	{"sub_plag_pairs", "student_a"},
	{"sub_plag_pairs", "student_b"},
}

// remapStudentIDs renumbers server-side student IDs to the publisher's current
// local IDs, matched by username (the stable cross-database identity).
// Accounts the publisher no longer knows are deleted so their IDs and
// usernames cannot collide with the remapped ones.
func remapStudentIDs(db dbtx, idMap []PortalIDMapping) error {
	newIDByUsername := make(map[string]int, len(idMap))
	for _, m := range idMap {
		newIDByUsername[strings.ToLower(m.Username)] = m.StudentID
	}

	rows, err := db.Query(`SELECT student_pk, username FROM published_accounts`)
	if err != nil {
		return err
	}
	moves := map[int]int{}
	var orphans []int
	for rows.Next() {
		var id int
		var username string
		if err := rows.Scan(&id, &username); err != nil {
			rows.Close()
			return err
		}
		newID, ok := newIDByUsername[strings.ToLower(username)]
		switch {
		case !ok:
			orphans = append(orphans, id)
		case newID != id:
			moves[id] = newID
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}

	for _, id := range orphans {
		if _, err := db.Exec(`DELETE FROM published_accounts WHERE student_pk = ?`, id); err != nil {
			return err
		}
	}

	for oldID := range moves {
		for _, ref := range studentIDKeyedTables {
			if _, err := db.Exec(
				fmt.Sprintf(`UPDATE %s SET %s = %s + ? WHERE %s = ?`, ref.table, ref.column, ref.column, ref.column),
				studentIDRemapOffset, oldID); err != nil {
				return err
			}
		}
	}
	for oldID, newID := range moves {
		for _, ref := range studentIDKeyedTables {
			if _, err := db.Exec(
				fmt.Sprintf(`UPDATE %s SET %s = ? WHERE %s = ?`, ref.table, ref.column, ref.column),
				newID, oldID+studentIDRemapOffset); err != nil {
				return err
			}
		}
	}
	return nil
}

// deleteOtherStudentSnapshots removes snapshots for a course that are not in
// the published roster: students removed from the course, or rows left under
// old IDs after local student IDs were renumbered.
func deleteOtherStudentSnapshots(db dbtx, courseYearID, termID int, keepIDs []int) error {
	if len(keepIDs) == 0 {
		_, err := db.Exec(`DELETE FROM published_students WHERE course_year_id = ? AND term_id = ?`, courseYearID, termID)
		return err
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(keepIDs)), ",")
	args := make([]any, 0, len(keepIDs)+2)
	args = append(args, courseYearID, termID)
	for _, id := range keepIDs {
		args = append(args, id)
	}
	_, err := db.Exec(
		`DELETE FROM published_students WHERE course_year_id = ? AND term_id = ? AND student_pk NOT IN (`+placeholders+`)`,
		args...)
	return err
}

// upsertAccount inserts or updates an account, preserving a newer VPS-side password.
func upsertAccount(db dbtx, acc portalauth.Account) error {
	publishedAt, err := time.Parse(time.RFC3339, acc.PasswordChangedAt)
	if err != nil {
		return fmt.Errorf("invalid password_changed_at %q: %w", acc.PasswordChangedAt, err)
	}

	var existing portalauth.Account
	err = db.QueryRow(`
		SELECT student_pk, username, password_salt, password_hash, must_change_password, password_changed_at
		FROM published_accounts WHERE student_pk = ?`, acc.StudentID).
		Scan(&existing.StudentID, &existing.Username, &existing.PasswordSalt, &existing.PasswordHash, &existing.MustChangePassword, &existing.PasswordChangedAt)
	if err != nil && err != sql.ErrNoRows {
		return err
	}

	// Preserve a newer VPS-side password, but only when the stored row is
	// the same student (same username). After a local ID renumbering the
	// row stored under this student_pk may belong to someone else.
	keepAt := publishedAt
	if err == nil && existing.Username == acc.Username {
		existingAt, err := time.Parse(time.RFC3339, existing.PasswordChangedAt)
		if err == nil && existingAt.After(keepAt) {
			acc = existing
			keepAt = existingAt
		}
	}

	// A row with the same username under a different student_pk is stale
	// (local student IDs were renumbered). Adopt its password when newer,
	// then remove it so the UNIQUE username constraint cannot collide.
	var stale portalauth.Account
	staleErr := db.QueryRow(`
		SELECT student_pk, username, password_salt, password_hash, must_change_password, password_changed_at
		FROM published_accounts WHERE username = ? AND student_pk != ?`, acc.Username, acc.StudentID).
		Scan(&stale.StudentID, &stale.Username, &stale.PasswordSalt, &stale.PasswordHash, &stale.MustChangePassword, &stale.PasswordChangedAt)
	if staleErr != nil && staleErr != sql.ErrNoRows {
		return staleErr
	}
	if staleErr == nil {
		if staleAt, parseErr := time.Parse(time.RFC3339, stale.PasswordChangedAt); parseErr == nil && staleAt.After(keepAt) {
			incomingID := acc.StudentID
			acc = stale
			acc.StudentID = incomingID
		}
		if _, err := db.Exec(`DELETE FROM published_accounts WHERE student_pk = ?`, stale.StudentID); err != nil {
			return err
		}
	}

	mustChange := 0
	if acc.MustChangePassword {
		mustChange = 1
	}

	_, err = db.Exec(`
		INSERT INTO published_accounts(student_pk, username, password_salt, password_hash, must_change_password, password_changed_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(student_pk) DO UPDATE SET
			username = excluded.username,
			password_salt = excluded.password_salt,
			password_hash = excluded.password_hash,
			must_change_password = excluded.must_change_password,
			password_changed_at = excluded.password_changed_at,
			updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')`,
		acc.StudentID, acc.Username, acc.PasswordSalt, acc.PasswordHash, mustChange, acc.PasswordChangedAt)
	return err
}

// UpdateAccountPassword updates the password fields for an existing account.
func (s *Store) UpdateAccountPassword(studentID int, username, salt, hash string, mustChange bool, changedAt time.Time) error {
	mustChangeInt := 0
	if mustChange {
		mustChangeInt = 1
	}
	res, err := s.db.Exec(`
		UPDATE published_accounts
		SET username = ?, password_salt = ?, password_hash = ?, must_change_password = ?, password_changed_at = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')
		WHERE student_pk = ?`,
		username, salt, hash, mustChangeInt, changedAt.UTC().Format(time.RFC3339), studentID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return fmt.Errorf("account not found: %d", studentID)
	}
	return nil
}

// upsertCourse inserts or updates a published course.
func upsertCourse(db dbtx, courseYearID, termID int, courseName, courseYearName, termName, publishedAt string) error {
	_, err := db.Exec(`
		INSERT INTO published_courses(course_year_id, term_id, course_name, course_year_name, term_name, published_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(course_year_id, term_id) DO UPDATE SET
			course_name = excluded.course_name,
			course_year_name = excluded.course_year_name,
			term_name = excluded.term_name,
			published_at = excluded.published_at`,
		courseYearID, termID, courseName, courseYearName, termName, publishedAt)
	return err
}

// GetCourse returns the published course, or nil if not found.
func (s *Store) GetCourse(courseYearID, termID int) (*CourseInfo, error) {
	var c CourseInfo
	err := s.db.QueryRow(`
		SELECT course_year_id, term_id, course_name, course_year_name, term_name, published_at
		FROM published_courses
		WHERE course_year_id = ? AND term_id = ?`, courseYearID, termID).
		Scan(&c.CourseYearID, &c.TermID, &c.CourseName, &c.CourseYearName, &c.TermName, &c.PublishedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// ListCourses returns all published courses, newest year and term first
// (course_year_id and term_id increase with each new year/term on the laptop).
func (s *Store) ListCourses() ([]CourseInfo, error) {
	rows, err := s.db.Query(`
		SELECT course_year_id, term_id, course_name, course_year_name, term_name, published_at
		FROM published_courses
		ORDER BY course_year_id DESC, term_id DESC, course_name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	courses := []CourseInfo{}
	for rows.Next() {
		var c CourseInfo
		if err := rows.Scan(&c.CourseYearID, &c.TermID, &c.CourseName, &c.CourseYearName, &c.TermName, &c.PublishedAt); err != nil {
			return nil, err
		}
		courses = append(courses, c)
	}
	return courses, rows.Err()
}

// DeleteCourse removes a published course and all its student snapshots.
func (s *Store) DeleteCourse(courseYearID, termID int) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`DELETE FROM published_students WHERE course_year_id = ? AND term_id = ?`, courseYearID, termID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM published_courses WHERE course_year_id = ? AND term_id = ?`, courseYearID, termID); err != nil {
		return err
	}
	return tx.Commit()
}

// SnapshotRow is one student's raw snapshot JSON for server-side parsing.
type SnapshotRow struct {
	StudentID int
	Username  string
	Raw       []byte
}

// ListSnapshotRowsForCourse returns raw student snapshots with usernames,
// ordered like ListStudentsForCourse.
func (s *Store) ListSnapshotRowsForCourse(courseYearID, termID int) ([]SnapshotRow, error) {
	rows, err := s.db.Query(`
		SELECT ps.student_pk, ps.snapshot_json, COALESCE(pa.username, '')
		FROM published_students ps
		LEFT JOIN published_accounts pa ON pa.student_pk = ps.student_pk
		WHERE ps.course_year_id = ? AND ps.term_id = ?
		ORDER BY ps.student_pk`, courseYearID, termID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []SnapshotRow{}
	for rows.Next() {
		var row SnapshotRow
		if err := rows.Scan(&row.StudentID, &row.Raw, &row.Username); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// upsertStudentSnapshot inserts or updates a student's course snapshot.
func upsertStudentSnapshot(db dbtx, studentID, courseYearID, termID int, snapshot []byte, publishedAt string) error {
	_, err := db.Exec(`
		INSERT INTO published_students(student_pk, course_year_id, term_id, snapshot_json, published_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(student_pk, course_year_id, term_id) DO UPDATE SET
			snapshot_json = excluded.snapshot_json,
			published_at = excluded.published_at`,
		studentID, courseYearID, termID, snapshot, publishedAt)
	return err
}

// GetStudentSnapshots returns all snapshots for a student.
func (s *Store) GetStudentSnapshots(studentID int) ([]StudentSnapshot, error) {
	rows, err := s.db.Query(`
		SELECT ps.course_year_id, ps.term_id, pc.course_name, pc.course_year_name, pc.term_name, ps.snapshot_json, ps.published_at
		FROM published_students ps
		JOIN published_courses pc ON pc.course_year_id = ps.course_year_id AND pc.term_id = ps.term_id
		WHERE ps.student_pk = ?
		ORDER BY pc.course_name, pc.term_name`, studentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	snapshots := []StudentSnapshot{}
	for rows.Next() {
		var snap StudentSnapshot
		var raw []byte
		if err := rows.Scan(&snap.CourseYearID, &snap.TermID, &snap.CourseName, &snap.CourseYearName, &snap.TermName, &raw, &snap.PublishedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &snap.Snapshot); err != nil {
			return nil, err
		}
		snapshots = append(snapshots, snap)
	}
	return snapshots, rows.Err()
}

// ListStudentsForCourse returns basic student info for a published course.
func (s *Store) ListStudentsForCourse(courseYearID, termID int) ([]AdminStudent, error) {
	rows, err := s.db.Query(`
		SELECT ps.student_pk, ps.snapshot_json, pa.username
		FROM published_students ps
		LEFT JOIN published_accounts pa ON pa.student_pk = ps.student_pk
		WHERE ps.course_year_id = ? AND ps.term_id = ?
		ORDER BY ps.student_pk`, courseYearID, termID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	students := []AdminStudent{}
	for rows.Next() {
		var st AdminStudent
		var raw []byte
		var username sql.NullString
		if err := rows.Scan(&st.StudentID, &raw, &username); err != nil {
			return nil, err
		}
		st.Username = username.String
		var snapshot map[string]any
		if err := json.Unmarshal(raw, &snapshot); err != nil {
			return nil, err
		}
		st.FirstName, _ = snapshot["firstName"].(string)
		st.LastName, _ = snapshot["lastName"].(string)
		st.ChineseName, _ = snapshot["chineseName"].(string)
		st.WeightedTotal, _ = snapshot["weightedTotal"].(float64)
		st.LetterGrade, _ = snapshot["letterGrade"].(string)
		students = append(students, st)
	}
	return students, rows.Err()
}

// CourseInfo describes a published course for listing.
type CourseInfo struct {
	CourseYearID   int    `json:"courseYearId"`
	TermID         int    `json:"termId"`
	CourseName     string `json:"courseName"`
	CourseYearName string `json:"courseYearName"`
	TermName       string `json:"termName"`
	PublishedAt    string `json:"publishedAt"`
}

// StudentSnapshot bundles a student's course snapshot with course metadata.
type StudentSnapshot struct {
	CourseYearID   int            `json:"courseYearId"`
	TermID         int            `json:"termId"`
	CourseName     string         `json:"courseName"`
	CourseYearName string         `json:"courseYearName"`
	TermName       string         `json:"termName"`
	PublishedAt    string         `json:"publishedAt"`
	Snapshot       map[string]any `json:"snapshot"`
}

// AdminStudent is a lightweight student row for the admin course view.
type AdminStudent struct {
	StudentID     int     `json:"studentId"`
	FirstName     string  `json:"firstName"`
	LastName      string  `json:"lastName"`
	ChineseName   string  `json:"chineseName,omitempty"`
	Username      string  `json:"username,omitempty"`
	WeightedTotal float64 `json:"weightedTotal"`
	LetterGrade   string  `json:"letterGrade"`
}

// PublishRequest is the payload sent by the CLI to publish a course snapshot.
type PublishRequest struct {
	Accounts []portalauth.Account `json:"accounts"`
	IDMap    []PortalIDMapping    `json:"idMap"`
	Course   CourseInfo           `json:"course"`
	Students []struct {
		StudentID int             `json:"studentId"`
		Snapshot  json.RawMessage `json:"snapshot"`
	} `json:"students"`
}

// PortalIDMapping tells the server which student ID each username belongs to
// in the publisher's database right now, so the server can re-key its stored
// data after the publisher renumbered local student IDs.
type PortalIDMapping struct {
	StudentID int    `json:"studentId"`
	Username  string `json:"username"`
}
