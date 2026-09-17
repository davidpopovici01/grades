package portalserver

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/davidpopovici01/grades/internal/portalauth"
)

// TestStoreMigratesOlderDatabases opens a database whose published_courses
// table predates the course_year_name column and verifies NewStore adds it.
func TestStoreMigratesOlderDatabases(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "old.db")
	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = conn.Exec(`CREATE TABLE published_courses (
		course_year_id INTEGER NOT NULL,
		term_id        INTEGER NOT NULL,
		course_name    TEXT NOT NULL,
		term_name      TEXT NOT NULL,
		published_at   TEXT NOT NULL,
		PRIMARY KEY (course_year_id, term_id)
	);`)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore on old schema: %v", err)
	}
	defer store.Close()

	var count int
	if err := store.db.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('published_courses') WHERE name = 'course_year_name'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected course_year_name column after migration, count=%d", count)
	}
}

func TestPortalServerLoginAndGrades(t *testing.T) {
	// Create temporary directories.
	tmpDir := t.TempDir()
	staticDir := filepath.Join(tmpDir, "static")
	os.MkdirAll(staticDir, 0755)

	// Write a dummy index.html.
	os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("<html></html>"), 0644)

	// Create server.
	server, err := NewServer(Config{
		StaticDir:    staticDir,
		DBPath:       filepath.Join(tmpDir, "portal.db"),
		JWTSecret:    []byte("test-secret-key-that-is-long-enough"),
		TeacherToken: "test-teacher-token",
		CookieSecure: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	// Publish test data via the admin endpoint.
	hash, salt, err := portalauth.HashPassword("testpass")
	if err != nil {
		t.Fatal(err)
	}

	publishReq := PublishRequest{
		Accounts: []portalauth.Account{
			{
				StudentID:          1,
				Username:           "john.doe",
				PasswordSalt:       salt,
				PasswordHash:       hash,
				MustChangePassword: false,
				PasswordChangedAt:  time.Now().UTC().Format(time.RFC3339),
			},
		},
		Course: CourseInfo{
			CourseYearID:   1,
			TermID:         1,
			CourseName:     "Test Course",
			CourseYearName: "2026-27",
			TermName:       "Fall 2026",
			PublishedAt:    time.Now().UTC().Format(time.RFC3339),
		},
		Students: []struct {
			StudentID int             `json:"studentId"`
			Snapshot  json.RawMessage `json:"snapshot"`
		}{
			{
				StudentID: 1,
				Snapshot: json.RawMessage(`{
					"studentId": 1,
					"firstName": "John",
					"lastName": "Doe",
					"courseName": "Test Course",
					"termName": "Fall 2026",
					"weightedTotal": 85.5,
					"categories": [],
					"assignments": []
				}`),
			},
		},
	}

	publishBody, _ := json.Marshal(publishReq)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/publish", bytes.NewReader(publishBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-teacher-token")
	rec := httptest.NewRecorder()
	handler := server.Handler()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("publish failed: %d %s", rec.Code, rec.Body.String())
	}

	// Test login.
	loginBody, _ := json.Marshal(map[string]string{"username": "john.doe", "password": "testpass"})
	req = httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(loginBody))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("login failed: %d %s", rec.Code, rec.Body.String())
	}

	var loginResp map[string]any
	json.Unmarshal(rec.Body.Bytes(), &loginResp)
	if loginResp["ok"] != true {
		t.Fatalf("login response ok=false")
	}

	// Extract cookies.
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("no session cookie set")
	}

	// Test /api/me with cookie.
	req = httptest.NewRequest(http.MethodGet, "/api/me", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("me failed: %d %s", rec.Code, rec.Body.String())
	}

	// Test /api/grades with cookie.
	req = httptest.NewRequest(http.MethodGet, "/api/grades", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("grades failed: %d %s", rec.Code, rec.Body.String())
	}

	var gradesResp map[string]any
	json.Unmarshal(rec.Body.Bytes(), &gradesResp)
	courses, ok := gradesResp["courses"].([]any)
	if !ok || len(courses) != 1 {
		t.Fatalf("unexpected grades response: %v", gradesResp)
	}
	course := courses[0].(map[string]any)
	if course["courseYearName"] != "2026-27" {
		t.Fatalf("expected courseYearName to round-trip, got %v", course)
	}
	snapshot := course["snapshot"].(map[string]any)
	if snapshot["firstName"] != "John" {
		t.Fatalf("unexpected snapshot: %v", snapshot)
	}

	// Test bad login.
	loginBody, _ = json.Marshal(map[string]string{"username": "john.doe", "password": "wrong"})
	req = httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(loginBody))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for bad login, got %d", rec.Code)
	}
}

// TestUnknownAPIPathReturnsJSON404 ensures unmatched /api/* paths never fall
// through to the SPA's index.html (which the frontend would parse as null).
func TestUnknownAPIPathReturnsJSON404(t *testing.T) {
	server := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/unknown", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown /api path, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("expected JSON content type, got %q", ct)
	}
}

// newTestServer creates a server backed by temp-dir static files and database.
func newTestServer(t *testing.T) *Server {
	t.Helper()
	tmpDir := t.TempDir()
	staticDir := filepath.Join(tmpDir, "static")
	if err := os.MkdirAll(staticDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("<html></html>"), 0644); err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(Config{
		StaticDir:    staticDir,
		DBPath:       filepath.Join(tmpDir, "portal.db"),
		JWTSecret:    []byte("test-secret-key-that-is-long-enough"),
		TeacherToken: "test-teacher-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { server.Close() })
	return server
}

func TestCORSMiddleware(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := corsMiddleware(inner)

	tests := []struct {
		name     string
		origin   string
		wantACAO string
	}{
		{"localhost origin allowed", "http://localhost:5173", "http://localhost:5173"},
		{"127.0.0.1 origin allowed", "http://127.0.0.1:3000", "http://127.0.0.1:3000"},
		{"foreign origin rejected", "https://evil.example.com", ""},
		{"localhost-suffix origin rejected", "https://localhost.evil.com", ""},
		{"garbage origin rejected", "not a url", ""},
		{"no origin header", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/index", nil)
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if got := rec.Header().Get("Access-Control-Allow-Origin"); got != tt.wantACAO {
				t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, tt.wantACAO)
			}
			if tt.wantACAO == "" {
				if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "" {
					t.Errorf("unexpected Access-Control-Allow-Credentials: %q", got)
				}
			}
		})
	}

	// An allowed origin gets credentials and DELETE among the allowed methods.
	req := httptest.NewRequest(http.MethodGet, "/api/index", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("Access-Control-Allow-Credentials = %q, want %q", got, "true")
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(got, "DELETE") {
		t.Errorf("Access-Control-Allow-Methods = %q, want it to contain DELETE", got)
	}

	// OPTIONS preflight is short-circuited with 204.
	req = httptest.NewRequest(http.MethodOptions, "/api/admin/courses/1/1", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Errorf("preflight status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}

func TestPublishCourseRollback(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "portal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Now().UTC().Format(time.RFC3339)

	// Seed an account that must survive the rolled-back publish untouched.
	seed := &PublishRequest{
		Accounts: []portalauth.Account{
			{StudentID: 1, Username: "taken", PasswordSalt: "s", PasswordHash: "h", PasswordChangedAt: now},
		},
		Course: CourseInfo{CourseYearID: 1, TermID: 1, CourseName: "Seed", TermName: "T1", PublishedAt: now},
	}
	if err := store.PublishCourse(seed); err != nil {
		t.Fatalf("seed publish: %v", err)
	}

	// The second account fails on an invalid timestamp after the first
	// account was already written; the transaction must roll back everything.
	bad := &PublishRequest{
		Accounts: []portalauth.Account{
			{StudentID: 2, Username: "fresh", PasswordSalt: "s", PasswordHash: "h", PasswordChangedAt: now},
			{StudentID: 3, Username: "other", PasswordSalt: "s", PasswordHash: "h", PasswordChangedAt: "not-a-time"},
		},
		Course: CourseInfo{CourseYearID: 2, TermID: 1, CourseName: "Rolled Back", TermName: "T1", PublishedAt: now},
		Students: []struct {
			StudentID int             `json:"studentId"`
			Snapshot  json.RawMessage `json:"snapshot"`
		}{
			{StudentID: 2, Snapshot: json.RawMessage(`{"firstName":"Ann"}`)},
		},
	}
	if err := store.PublishCourse(bad); err == nil {
		t.Fatal("expected publish to fail on invalid password_changed_at")
	}

	if acc, err := store.GetAccountByStudentID(2); err != nil || acc != nil {
		t.Errorf("account 2 must not survive rollback (acc=%+v, err=%v)", acc, err)
	}
	if course, err := store.GetCourse(2, 1); err != nil || course != nil {
		t.Errorf("course 2/1 must not survive rollback (course=%+v, err=%v)", course, err)
	}
	students, err := store.ListStudentsForCourse(2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(students) != 0 {
		t.Errorf("no snapshots must survive rollback, got %d", len(students))
	}

	// The seed data must be untouched.
	if acc, err := store.GetAccountByStudentID(1); err != nil || acc == nil {
		t.Errorf("seed account missing after rollback (acc=%+v, err=%v)", acc, err)
	}
}

func TestPublishCourseMigratesRenumberedAccount(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "portal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	old := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	newer := time.Now().UTC().Format(time.RFC3339)

	// Seed: alice.brown lives under student_pk 1 with a VPS-changed (newer)
	// password, as left behind before local student IDs were renumbered.
	seed := &PublishRequest{
		Accounts: []portalauth.Account{
			{StudentID: 1, Username: "alice.brown", PasswordSalt: "vps-salt", PasswordHash: "vps-hash", PasswordChangedAt: newer},
			{StudentID: 2, Username: "bob.zhang", PasswordSalt: "s", PasswordHash: "h", PasswordChangedAt: old},
		},
		Course: CourseInfo{CourseYearID: 1, TermID: 1, CourseName: "Seed", TermName: "T1", PublishedAt: newer},
	}
	if err := store.PublishCourse(seed); err != nil {
		t.Fatalf("seed publish: %v", err)
	}

	// After renumbering, alice.brown arrives under student_pk 5 with an
	// older password. The publish must succeed, move her account to the new
	// ID, and keep the newer VPS-side password.
	renumbered := &PublishRequest{
		Accounts: []portalauth.Account{
			{StudentID: 5, Username: "alice.brown", PasswordSalt: "local-salt", PasswordHash: "local-hash", PasswordChangedAt: old},
			{StudentID: 6, Username: "bob.zhang", PasswordSalt: "s", PasswordHash: "h", PasswordChangedAt: old},
		},
		Course: CourseInfo{CourseYearID: 1, TermID: 1, CourseName: "Seed", TermName: "T1", PublishedAt: newer},
	}
	if err := store.PublishCourse(renumbered); err != nil {
		t.Fatalf("renumbered publish: %v", err)
	}

	acc, err := store.GetAccountByStudentID(5)
	if err != nil || acc == nil {
		t.Fatalf("alice.brown must exist under student_pk 5 (acc=%+v, err=%v)", acc, err)
	}
	if acc.PasswordHash != "vps-hash" {
		t.Errorf("expected newer VPS-side password to be kept, got hash %q", acc.PasswordHash)
	}
	if acc, err := store.GetAccountByStudentID(1); err != nil || acc != nil {
		t.Errorf("stale account under student_pk 1 must be removed (acc=%+v, err=%v)", acc, err)
	}
	if acc, err := store.GetAccountByStudentID(6); err != nil || acc == nil {
		t.Errorf("bob.zhang must exist under student_pk 6 (acc=%+v, err=%v)", acc, err)
	}
	if acc, err := store.GetAccountByStudentID(2); err != nil || acc != nil {
		t.Errorf("stale account under student_pk 2 must be removed (acc=%+v, err=%v)", acc, err)
	}
}

func TestPublishCoursePreservesServerPasswordAcrossPublishes(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "portal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	old := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	newer := time.Now().UTC()

	localAccount := portalauth.Account{StudentID: 1, Username: "alice.brown", PasswordSalt: "local-salt", PasswordHash: "local-hash", PasswordChangedAt: old}
	seed := &PublishRequest{
		Accounts: []portalauth.Account{localAccount},
		Course:   CourseInfo{CourseYearID: 1, TermID: 1, CourseName: "Seed", TermName: "T1", PublishedAt: newer.Format(time.RFC3339)},
	}
	if err := store.PublishCourse(seed); err != nil {
		t.Fatalf("seed publish: %v", err)
	}

	// The student changes her password on the portal website.
	if err := store.UpdateAccountPassword(1, "alice.brown", "vps-salt", "vps-hash", false, newer); err != nil {
		t.Fatalf("website password change: %v", err)
	}

	// Every later publish still carries the old local password; the newer
	// website-side password must survive each one, not just the first.
	for i := 0; i < 2; i++ {
		republish := &PublishRequest{
			Accounts: []portalauth.Account{localAccount},
			Course:   CourseInfo{CourseYearID: 1, TermID: 1, CourseName: "Seed", TermName: "T1", PublishedAt: newer.Format(time.RFC3339)},
		}
		if err := store.PublishCourse(republish); err != nil {
			t.Fatalf("republish %d: %v", i+1, err)
		}
		acc, err := store.GetAccountByStudentID(1)
		if err != nil || acc == nil {
			t.Fatalf("account missing after republish %d (acc=%+v, err=%v)", i+1, acc, err)
		}
		if acc.PasswordHash != "vps-hash" {
			t.Fatalf("republish %d reset the website-side password: got hash %q", i+1, acc.PasswordHash)
		}
		if _, err := time.Parse(time.RFC3339, acc.PasswordChangedAt); err != nil {
			t.Fatalf("republish %d stored an invalid password_changed_at %q", i+1, acc.PasswordChangedAt)
		}
	}
}

func TestPublishCourseRemovesStaleSnapshots(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "portal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	type studentSnapshot = struct {
		StudentID int             `json:"studentId"`
		Snapshot  json.RawMessage `json:"snapshot"`
	}

	seed := &PublishRequest{
		Course: CourseInfo{CourseYearID: 1, TermID: 1, CourseName: "Seed", TermName: "T1", PublishedAt: now},
		Students: []studentSnapshot{
			{StudentID: 1, Snapshot: json.RawMessage(`{"firstName":"Alice"}`)},
			{StudentID: 2, Snapshot: json.RawMessage(`{"firstName":"Bob"}`)},
		},
	}
	if err := store.PublishCourse(seed); err != nil {
		t.Fatalf("seed publish: %v", err)
	}

	// After local IDs were renumbered, the roster arrives under new IDs.
	renumbered := &PublishRequest{
		Course: CourseInfo{CourseYearID: 1, TermID: 1, CourseName: "Seed", TermName: "T1", PublishedAt: now},
		Students: []studentSnapshot{
			{StudentID: 5, Snapshot: json.RawMessage(`{"firstName":"Alice"}`)},
			{StudentID: 6, Snapshot: json.RawMessage(`{"firstName":"Bob"}`)},
		},
	}
	if err := store.PublishCourse(renumbered); err != nil {
		t.Fatalf("renumbered publish: %v", err)
	}

	students, err := store.ListStudentsForCourse(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(students) != 2 {
		t.Fatalf("expected 2 students after renumbered publish, got %d", len(students))
	}
	for _, st := range students {
		if st.StudentID != 5 && st.StudentID != 6 {
			t.Errorf("stale snapshot for student_pk %d survived publish", st.StudentID)
		}
	}
}

func TestPublishCourseRemapsStudentIDsByUsername(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "portal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	old := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	newer := time.Now().UTC().Format(time.RFC3339)

	// Seed the server as it looked before local IDs were renumbered:
	// alice.brown under pk 1 (with a VPS-changed password), bob.zhang under
	// pk 2, and carol.chen under pk 9, whose account no longer exists locally.
	seed := &PublishRequest{
		Accounts: []portalauth.Account{
			{StudentID: 1, Username: "alice.brown", PasswordSalt: "vps-salt", PasswordHash: "vps-hash", PasswordChangedAt: newer},
			{StudentID: 2, Username: "bob.zhang", PasswordSalt: "s", PasswordHash: "h", PasswordChangedAt: old},
			{StudentID: 9, Username: "carol.chen", PasswordSalt: "s", PasswordHash: "h", PasswordChangedAt: old},
		},
		Course: CourseInfo{CourseYearID: 1, TermID: 1, CourseName: "Seed", TermName: "T1", PublishedAt: newer},
	}
	if err := store.PublishCourse(seed); err != nil {
		t.Fatalf("seed publish: %v", err)
	}

	// Student-keyed server data from before the renumbering.
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := store.db.Exec(query, args...); err != nil {
			t.Fatalf("exec %q: %v", query, err)
		}
	}
	exec(`INSERT INTO sub_assignments(id, course_year_id, term_id, title, language) VALUES (1, 1, 1, 'HW1', 'python')`)
	exec(`INSERT INTO sub_submissions(id, assignment_id, student_pk, attempt, submitted_at) VALUES (1, 1, 1, 1, ?), (2, 1, 2, 1, ?)`, old, old)

	// Publish after renumbering: alice is now pk 5, bob pk 6, carol is gone.
	renumbered := &PublishRequest{
		Accounts: []portalauth.Account{
			{StudentID: 5, Username: "alice.brown", PasswordSalt: "local-salt", PasswordHash: "local-hash", PasswordChangedAt: old},
			{StudentID: 6, Username: "bob.zhang", PasswordSalt: "s", PasswordHash: "h", PasswordChangedAt: old},
		},
		IDMap: []PortalIDMapping{
			{StudentID: 5, Username: "alice.brown"},
			{StudentID: 6, Username: "bob.zhang"},
		},
		Course: CourseInfo{CourseYearID: 1, TermID: 1, CourseName: "Seed", TermName: "T1", PublishedAt: newer},
	}
	if err := store.PublishCourse(renumbered); err != nil {
		t.Fatalf("renumbered publish: %v", err)
	}

	acc, err := store.GetAccountByStudentID(5)
	if err != nil || acc == nil {
		t.Fatalf("alice.brown must exist under pk 5 (acc=%+v, err=%v)", acc, err)
	}
	if acc.PasswordHash != "vps-hash" {
		t.Errorf("expected newer VPS-side password to be kept, got %q", acc.PasswordHash)
	}
	if acc, err := store.GetAccountByStudentID(6); err != nil || acc == nil {
		t.Errorf("bob.zhang must exist under pk 6 (acc=%+v, err=%v)", acc, err)
	}
	for _, staleID := range []int{1, 2, 9} {
		if acc, err := store.GetAccountByStudentID(staleID); err != nil || acc != nil {
			t.Errorf("account under old pk %d must be gone (acc=%+v, err=%v)", staleID, acc, err)
		}
	}

	var submissionOwners []int
	rows, err := store.db.Query(`SELECT student_pk FROM sub_submissions ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		submissionOwners = append(submissionOwners, id)
	}
	if len(submissionOwners) != 2 || submissionOwners[0] != 5 || submissionOwners[1] != 6 {
		t.Errorf("expected submissions re-keyed to 5 and 6, got %v", submissionOwners)
	}
}

func TestAdminCourseStudentsResponse(t *testing.T) {
	server := newTestServer(t)
	handler := server.Handler()

	now := time.Now().UTC().Format(time.RFC3339)
	err := server.store.PublishCourse(&PublishRequest{
		Course: CourseInfo{CourseYearID: 7, TermID: 2, CourseName: "AP CSA", TermName: "Spring 2026", PublishedAt: now},
		Students: []struct {
			StudentID int             `json:"studentId"`
			Snapshot  json.RawMessage `json:"snapshot"`
		}{
			{StudentID: 42, Snapshot: json.RawMessage(`{"firstName":"Jane","lastName":"Doe","weightedTotal":91.2,"letterGrade":"A"}`)},
		},
	})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/courses/7/2/students", nil)
	req.Header.Set("Authorization", "Bearer test-teacher-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("students endpoint: %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		CourseName string         `json:"courseName"`
		TermName   string         `json:"termName"`
		Students   []AdminStudent `json:"students"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.CourseName != "AP CSA" || resp.TermName != "Spring 2026" {
		t.Errorf("unexpected course/term names: %+v", resp)
	}
	if len(resp.Students) != 1 || resp.Students[0].FirstName != "Jane" {
		t.Errorf("unexpected students: %+v", resp.Students)
	}

	// An unknown course returns 404.
	req = httptest.NewRequest(http.MethodGet, "/api/admin/courses/99/9/students", nil)
	req.Header.Set("Authorization", "Bearer test-teacher-token")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown course: got %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestEmptyListsAreJSONArrays(t *testing.T) {
	server := newTestServer(t)
	handler := server.Handler()

	// /api/index on an empty store must return an empty array, not null.
	req := httptest.NewRequest(http.MethodGet, "/api/index", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("index: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"courses":[]`) {
		t.Errorf("index body = %s, want it to contain \"courses\":[]", rec.Body.String())
	}

	// Same for the admin list endpoint.
	req = httptest.NewRequest(http.MethodGet, "/api/admin/courses", nil)
	req.Header.Set("Authorization", "Bearer test-teacher-token")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin courses: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"courses":[]`) {
		t.Errorf("admin courses body = %s, want it to contain \"courses\":[]", rec.Body.String())
	}
}
