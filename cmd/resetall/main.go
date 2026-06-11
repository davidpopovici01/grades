package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/davidpopovici01/grades/internal/db"
	"github.com/davidpopovici01/grades/internal/portalauth"
	_ "modernc.org/sqlite"
)

func main() {
	homeDir := os.Getenv("GRADES_HOME")
	if homeDir == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintln(os.Stderr, "cannot find home dir:", err)
			os.Exit(1)
		}
		homeDir = filepath.Join(userHome, ".grades")
	}

	dbPath := os.Getenv("GRADES_DB_PATH")
	if dbPath == "" {
		dbPath = filepath.Join(homeDir, "grades.db")
	}

	conn, err := db.Open(dbPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot open db:", err)
		os.Exit(1)
	}
	defer conn.Close()

	// Get current context.
	var courseYearID, termID int
	row := conn.QueryRow(`SELECT course_year_id, term_id FROM course_year_terms LIMIT 1`)
	if err := row.Scan(&courseYearID, &termID); err != nil {
		fmt.Fprintln(os.Stderr, "no course/term found:", err)
		os.Exit(1)
	}

	// Get all students in current course/term.
	rows, err := conn.Query(`
		SELECT DISTINCT students.student_pk, students.first_name, students.last_name, COALESCE(student_accounts.username, '')
		FROM section_enrollments
		JOIN students ON students.student_pk = section_enrollments.student_pk
		JOIN sections ON sections.section_id = section_enrollments.section_id
		LEFT JOIN student_accounts ON student_accounts.student_pk = students.student_pk
		WHERE sections.course_year_id = ? AND section_enrollments.term_id = ?
		ORDER BY students.last_name, students.first_name`, courseYearID, termID)
	if err != nil {
		fmt.Fprintln(os.Stderr, "query students:", err)
		os.Exit(1)
	}

	type studentInfo struct {
		studentID int
		name      string
		username  string
	}
	var students []studentInfo

	for rows.Next() {
		var studentID int
		var firstName, lastName, existingUsername string
		if err := rows.Scan(&studentID, &firstName, &lastName, &existingUsername); err != nil {
			fmt.Fprintln(os.Stderr, "scan student:", err)
			os.Exit(1)
		}
		students = append(students, studentInfo{
			studentID: studentID,
			name:      firstName + " " + lastName,
			username:  existingUsername,
		})
	}
	rows.Close()

	if err := rows.Err(); err != nil {
		fmt.Fprintln(os.Stderr, "rows error:", err)
		os.Exit(1)
	}

	type result struct {
		name     string
		username string
		password string
	}
	var results []result

	for _, s := range students {
		password, err := portalauth.MemorablePassword(3)
		if err != nil {
			fmt.Fprintln(os.Stderr, "generate password:", err)
			os.Exit(1)
		}

		hash, salt, err := portalauth.HashPassword(password)
		if err != nil {
			fmt.Fprintln(os.Stderr, "hash password:", err)
			os.Exit(1)
		}

		// Generate username if missing.
		username := s.username
		if username == "" {
			username = generateUsername(s.name, conn)
		}

		changedAt := time.Now().UTC().Format(time.RFC3339)
		_, err = conn.Exec(`
			INSERT INTO student_accounts(student_pk, username, password_salt, password_hash, must_change_password, password_changed_at, updated_at)
			VALUES (?, ?, ?, ?, 0, ?, datetime('now'))
			ON CONFLICT(student_pk) DO UPDATE SET
				username = excluded.username,
				password_salt = excluded.password_salt,
				password_hash = excluded.password_hash,
				must_change_password = excluded.must_change_password,
				password_changed_at = excluded.password_changed_at,
				updated_at = excluded.updated_at`,
			s.studentID, username, salt, hash, changedAt)
		if err != nil {
			fmt.Fprintln(os.Stderr, "update account:", err)
			os.Exit(1)
		}

		results = append(results, result{
			name:     s.name,
			username: username,
			password: password,
		})
	}

	// Print table.
	fmt.Println("Student portal accounts")
	fmt.Println("Name\t\t\tUsername\tPassword")
	for _, r := range results {
		fmt.Printf("%-23s\t%s\t%s\n", r.name, r.username, r.password)
	}
	fmt.Printf("\nReset %d account(s)\n", len(results))
}

func generateUsername(name string, db *sql.DB) string {
	parts := strings.SplitN(name, " ", 2)
	first := ""
	last := ""
	if len(parts) > 0 {
		first = parts[0]
	}
	if len(parts) > 1 {
		last = parts[1]
	}
	base := strings.ToLower(strings.TrimSpace(first) + "." + strings.TrimSpace(last))
	base = strings.ReplaceAll(base, " ", ".")
	base = strings.ReplaceAll(base, "-", ".")
	username := base
	for i := 2; ; i++ {
		var count int
		err := db.QueryRow(`SELECT COUNT(*) FROM student_accounts WHERE username = ?`, username).Scan(&count)
		if err != nil {
			break
		}
		if count == 0 {
			break
		}
		username = fmt.Sprintf("%s%d", base, i)
	}
	return username
}
