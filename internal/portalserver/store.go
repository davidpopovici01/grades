package portalserver

import (
	"database/sql"
	"encoding/json"
	"fmt"
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
	}
	for _, stmt := range statements {
		if _, err := s.db.Exec(stmt); err != nil {
			return err
		}
	}
	// Databases created before course_year_name existed need the column added.
	return s.addColumnIfMissing("published_courses", "course_year_name", `TEXT NOT NULL DEFAULT ''`)
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

	return tx.Commit()
}

// upsertAccount inserts or updates an account, preserving a newer VPS-side password.
func upsertAccount(db dbtx, acc portalauth.Account) error {
	publishedAt, err := time.Parse(time.RFC3339, acc.PasswordChangedAt)
	if err != nil {
		return fmt.Errorf("invalid password_changed_at %q: %w", acc.PasswordChangedAt, err)
	}

	var existing portalauth.Account
	var existingChangedAt string
	err = db.QueryRow(`
		SELECT student_pk, username, password_salt, password_hash, must_change_password, password_changed_at
		FROM published_accounts WHERE student_pk = ?`, acc.StudentID).
		Scan(&existing.StudentID, &existing.Username, &existing.PasswordSalt, &existing.PasswordHash, &existing.MustChangePassword, &existingChangedAt)
	if err != nil && err != sql.ErrNoRows {
		return err
	}

	// If existing has a newer password change, keep it.
	if err == nil {
		existingAt, err := time.Parse(time.RFC3339, existingChangedAt)
		if err == nil && existingAt.After(publishedAt) {
			acc = existing
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

// ListCourses returns all published courses ordered by name.
func (s *Store) ListCourses() ([]CourseInfo, error) {
	rows, err := s.db.Query(`
		SELECT course_year_id, term_id, course_name, course_year_name, term_name, published_at
		FROM published_courses
		ORDER BY course_name, term_name`)
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
	Course   CourseInfo           `json:"course"`
	Students []struct {
		StudentID int             `json:"studentId"`
		Snapshot  json.RawMessage `json:"snapshot"`
	} `json:"students"`
}
