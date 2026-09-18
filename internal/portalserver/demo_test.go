package portalserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/davidpopovici01/grades/internal/portalauth"
)

const demoTestPassword = "demo-pass-123"

// newDemoTestServer creates a test server with the demo account enabled.
func newDemoTestServer(t *testing.T) *Server {
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
		StaticDir:      staticDir,
		DBPath:         filepath.Join(tmpDir, "portal.db"),
		JWTSecret:      []byte("test-secret-key-that-is-long-enough"),
		TeacherToken:   "test-teacher-token",
		MaterialsDir:   filepath.Join(tmpDir, "materials"),
		SubmissionsDir: filepath.Join(tmpDir, "submissions"),
		DemoPassword:   demoTestPassword,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := server.Close(); err != nil {
			t.Errorf("closing server: %v", err)
		}
	})
	return server
}

// loginDemo logs in as the demo account through the normal login flow.
func loginDemo(t *testing.T, handler http.Handler) []*http.Cookie {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": demoUsername, "password": demoTestPassword})
	req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("demo login failed: %d %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["mustChangePassword"] != false {
		t.Fatalf("demo account must not require a password change: %v", resp)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("no session cookie set")
	}
	return cookies
}

func demoRequest(t *testing.T, handler http.Handler, cookies []*http.Cookie, method, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestSeedDemoCreatesAccountCourseAndSnapshot(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "portal.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("closing store: %v", err)
		}
	})

	if err := store.SeedDemo(demoTestPassword); err != nil {
		t.Fatalf("SeedDemo: %v", err)
	}
	// A second seeding is idempotent (and applies a rotated password).
	if err := store.SeedDemo(demoTestPassword); err != nil {
		t.Fatalf("second SeedDemo: %v", err)
	}

	acc, err := store.GetAccountByUsername(demoUsername)
	if err != nil || acc == nil {
		t.Fatalf("demo account missing (acc=%+v, err=%v)", acc, err)
	}
	if acc.StudentID != demoStudentID {
		t.Errorf("expected student_pk %d, got %d", demoStudentID, acc.StudentID)
	}
	if acc.MustChangePassword {
		t.Error("demo account must not require a password change")
	}
	if !portalauth.VerifyPassword(demoTestPassword, acc.PasswordSalt, acc.PasswordHash) {
		t.Error("demo password does not verify")
	}
	if _, err := time.Parse(time.RFC3339, acc.PasswordChangedAt); err != nil {
		t.Errorf("invalid password_changed_at %q: %v", acc.PasswordChangedAt, err)
	}

	course, err := store.GetCourse(demoCourseYearID, demoTermID)
	if err != nil || course == nil {
		t.Fatalf("demo course missing (course=%+v, err=%v)", course, err)
	}
	if course.CourseName != "Sample APCSA" || course.CourseYearName != "2026-27" || course.TermName != "Term 1" {
		t.Errorf("unexpected demo course: %+v", course)
	}

	snapshots, err := store.GetStudentSnapshots(demoStudentID)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshots) != 1 {
		t.Fatalf("expected 1 demo snapshot, got %d", len(snapshots))
	}
	snap := snapshots[0].Snapshot
	if snap["firstName"] != "Demo" || snap["letterGrade"] != "C-" || snap["weightedTotal"] != 71.5 {
		t.Errorf("unexpected demo snapshot: %v", snap)
	}

	assignments, err := store.ListSubAssignmentsForCourse(demoCourseYearID, demoTermID)
	if err != nil {
		t.Fatal(err)
	}
	if len(assignments) != 1 {
		t.Fatalf("expected exactly 1 demo assignment after reseeding, got %d", len(assignments))
	}
}

func TestDemoLoginGradesMaterialsAndSubmissions(t *testing.T) {
	server := newDemoTestServer(t)
	handler := server.Handler()
	cookies := loginDemo(t, handler)

	// /api/grades returns the sample snapshot.
	rec := demoRequest(t, handler, cookies, http.MethodGet, "/api/grades", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("grades failed: %d %s", rec.Code, rec.Body.String())
	}
	var gradesResp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &gradesResp); err != nil {
		t.Fatal(err)
	}
	courses, ok := gradesResp["courses"].([]any)
	if !ok || len(courses) != 1 {
		t.Fatalf("unexpected grades response: %v", gradesResp)
	}
	course := courses[0].(map[string]any)
	if course["courseYearName"] != "2026-27" {
		t.Errorf("unexpected course: %v", course)
	}
	snapshot := course["snapshot"].(map[string]any)
	if snapshot["firstName"] != "Demo" || snapshot["weightedTotalLabel"] != "71.5%" {
		t.Errorf("unexpected snapshot: %v", snapshot)
	}
	if cats, ok := snapshot["categories"].([]any); !ok || len(cats) != 3 {
		t.Errorf("expected 3 categories, got %v", snapshot["categories"])
	}
	if items, ok := snapshot["assignments"].([]any); !ok || len(items) != 8 {
		t.Errorf("expected 8 assignments, got %v", snapshot["assignments"])
	}

	// /api/materials returns the seeded demo materials.
	rec = demoRequest(t, handler, cookies, http.MethodGet, "/api/materials", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("materials failed: %d %s", rec.Code, rec.Body.String())
	}
	var materialsResp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &materialsResp); err != nil {
		t.Fatal(err)
	}
	matCourses, ok := materialsResp["courses"].([]any)
	if !ok || len(matCourses) != 1 {
		t.Fatalf("expected demo materials course, got %v", materialsResp)
	}
	matCourse := matCourses[0].(map[string]any)
	cats, ok := matCourse["categories"].([]any)
	if !ok || len(cats) != 1 {
		t.Fatalf("expected 1 materials category, got %v", matCourse)
	}
	files, ok := cats[0].(map[string]any)["files"].([]any)
	if !ok || len(files) != 2 {
		t.Errorf("expected 2 demo material files, got %v", cats[0])
	}

	// /api/submissions lists the seeded sample assignment.
	rec = demoRequest(t, handler, cookies, http.MethodGet, "/api/submissions", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("submissions failed: %d %s", rec.Code, rec.Body.String())
	}
	var subsResp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &subsResp); err != nil {
		t.Fatal(err)
	}
	subCourses, ok := subsResp["courses"].([]any)
	if !ok || len(subCourses) != 1 {
		t.Fatalf("expected demo submissions course, got %v", subsResp)
	}
	subAssignments, ok := subCourses[0].(map[string]any)["assignments"].([]any)
	if !ok || len(subAssignments) != 1 {
		t.Fatalf("expected 1 demo submission assignment, got %v", subCourses[0])
	}
}

func TestPublishCourseKeepsDemoAccount(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "portal.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("closing store: %v", err)
		}
	})

	if err := store.SeedDemo(demoTestPassword); err != nil {
		t.Fatalf("SeedDemo: %v", err)
	}

	// A publish whose IDMap does not know the demo username must leave the
	// demo account and its snapshot untouched (negative IDs are skipped by
	// the remap).
	now := time.Now().UTC().Format(time.RFC3339)
	publish := &PublishRequest{
		Accounts: []portalauth.Account{
			{StudentID: 5, Username: "alice.brown", PasswordSalt: "s", PasswordHash: "h", PasswordChangedAt: now},
		},
		IDMap: []PortalIDMapping{
			{StudentID: 5, Username: "alice.brown"},
		},
		Course: CourseInfo{CourseYearID: 1, TermID: 1, CourseName: "AP CSA", TermName: "T1", PublishedAt: now},
	}
	if err := store.PublishCourse(publish); err != nil {
		t.Fatalf("publish: %v", err)
	}

	acc, err := store.GetAccountByUsername(demoUsername)
	if err != nil || acc == nil {
		t.Fatalf("demo account must survive publish (acc=%+v, err=%v)", acc, err)
	}
	if acc.StudentID != demoStudentID {
		t.Errorf("demo account must stay under student_pk %d, got %d", demoStudentID, acc.StudentID)
	}
	if !portalauth.VerifyPassword(demoTestPassword, acc.PasswordSalt, acc.PasswordHash) {
		t.Error("demo password must survive publish")
	}
	snapshots, err := store.GetStudentSnapshots(demoStudentID)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshots) != 1 {
		t.Errorf("demo snapshot must survive publish, got %d", len(snapshots))
	}
}

func TestDemoAccountIsReadOnly(t *testing.T) {
	server := newDemoTestServer(t)
	handler := server.Handler()
	cookies := loginDemo(t, handler)

	assignments, err := server.store.ListSubAssignmentsForCourse(demoCourseYearID, demoTermID)
	if err != nil || len(assignments) != 1 {
		t.Fatalf("demo assignment missing (n=%d, err=%v)", len(assignments), err)
	}
	assignmentID := assignments[0].ID

	readOnly := []struct {
		method string
		path   string
		body   []byte
	}{
		{http.MethodPost, "/api/change-password", []byte(`{"currentPassword":"` + demoTestPassword + `","newPassword":"new-password-123"}`)},
		{http.MethodPost, fmt.Sprintf("/api/submissions/%d/files", assignmentID), nil},
		{http.MethodPost, fmt.Sprintf("/api/submissions/%d/slot/grader.py", assignmentID), nil},
		{http.MethodDelete, fmt.Sprintf("/api/submissions/%d/slot/grader.py", assignmentID), nil},
		{http.MethodPost, fmt.Sprintf("/api/submissions/%d/submit", assignmentID), nil},
		{http.MethodPost, fmt.Sprintf("/api/submissions/%d/test", assignmentID), nil},
	}
	for _, tc := range readOnly {
		rec := demoRequest(t, handler, cookies, tc.method, tc.path, tc.body)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s: expected 403, got %d (%s)", tc.method, tc.path, rec.Code, rec.Body.String())
		}
		var body map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s %s: invalid JSON: %v", tc.method, tc.path, err)
		}
		if body["error"] != "demo account is read-only" {
			t.Errorf("%s %s: unexpected error %q", tc.method, tc.path, body["error"])
		}
	}

	// Read-only endpoints still work for the demo account.
	rec := demoRequest(t, handler, cookies, http.MethodGet, fmt.Sprintf("/api/submissions/%d", assignmentID), nil)
	if rec.Code != http.StatusOK {
		t.Errorf("GET submission detail: expected 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	// No staged file yet, but the slot GET is allowed (404, not 403).
	rec = demoRequest(t, handler, cookies, http.MethodGet, fmt.Sprintf("/api/submissions/%d/slot/grader.py", assignmentID), nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET submission slot: expected 404, got %d (%s)", rec.Code, rec.Body.String())
	}
	rec = demoRequest(t, handler, cookies, http.MethodGet, "/api/me", nil)
	if rec.Code != http.StatusOK {
		t.Errorf("GET /api/me: expected 200, got %d", rec.Code)
	}

	// The shared password still works after the rejected change attempt.
	loginDemo(t, handler)
}

func TestDemoNotSeededWithoutPassword(t *testing.T) {
	server := newTestServer(t)
	handler := server.Handler()

	acc, err := server.store.GetAccountByUsername(demoUsername)
	if err != nil || acc != nil {
		t.Fatalf("demo account must not exist without a demo password (acc=%+v, err=%v)", acc, err)
	}

	body, _ := json.Marshal(map[string]string{"username": demoUsername, "password": demoTestPassword})
	req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for demo login without a seeded account, got %d", rec.Code)
	}
}

func TestClearDemoRemovesSeededData(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "portal.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("closing store: %v", err)
		}
	})

	if err := store.SeedDemo(demoTestPassword); err != nil {
		t.Fatal(err)
	}
	if err := store.ClearDemo(); err != nil {
		t.Fatal(err)
	}

	acc, err := store.GetAccountByUsername(demoUsername)
	if err != nil || acc != nil {
		t.Errorf("demo account should be removed (acc=%+v, err=%v)", acc, err)
	}
	course, err := store.GetCourse(demoCourseYearID, demoTermID)
	if err != nil || course != nil {
		t.Errorf("demo course should be removed (course=%+v, err=%v)", course, err)
	}
	snaps, err := store.GetStudentSnapshots(demoStudentID)
	if err != nil || len(snaps) != 0 {
		t.Errorf("demo snapshots should be removed (snaps=%d, err=%v)", len(snaps), err)
	}
	var assignments int
	if err := store.db.QueryRow(
		`SELECT COUNT(*) FROM sub_assignments WHERE course_year_id = ? AND term_id = ?`,
		demoCourseYearID, demoTermID).Scan(&assignments); err != nil {
		t.Fatal(err)
	}
	if assignments != 0 {
		t.Errorf("demo submission assignments should be removed, found %d", assignments)
	}
}

// seedRealDemoAccount inserts a real student account that owns the reserved
// demo username (possible for accounts created before the CLI reserved it).
func seedRealDemoAccount(t *testing.T, store *Store, studentID int) {
	t.Helper()
	hash, salt, err := portalauth.HashPassword("real-student-pass")
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.db.Exec(`
		INSERT INTO published_accounts(student_pk, username, password_salt, password_hash, must_change_password, password_changed_at)
		VALUES (?, ?, ?, ?, 0, ?)`,
		studentID, demoUsername, salt, hash, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		t.Fatal(err)
	}
}

func TestSeedDemoFailsWhenRealStudentOwnsUsername(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "portal.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("closing store: %v", err)
		}
	})

	seedRealDemoAccount(t, store, 42)

	if err := store.SeedDemo(demoTestPassword); !errors.Is(err, errDemoUsernameTaken) {
		t.Fatalf("expected errDemoUsernameTaken, got %v", err)
	}

	// The real student's account must be untouched.
	acc, err := store.GetAccountByUsername(demoUsername)
	if err != nil || acc == nil {
		t.Fatalf("real account missing after failed seed (acc=%+v, err=%v)", acc, err)
	}
	if acc.StudentID != 42 {
		t.Errorf("expected real student_pk 42, got %d", acc.StudentID)
	}
	if !portalauth.VerifyPassword("real-student-pass", acc.PasswordSalt, acc.PasswordHash) {
		t.Error("real student password must survive the failed seed")
	}
}

func TestServerDisablesDemoWhenRealStudentOwnsUsername(t *testing.T) {
	tmpDir := t.TempDir()
	staticDir := filepath.Join(tmpDir, "static")
	if err := os.MkdirAll(staticDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("<html></html>"), 0644); err != nil {
		t.Fatal(err)
	}

	store, err := NewStore(filepath.Join(tmpDir, "portal.db"))
	if err != nil {
		t.Fatal(err)
	}
	seedRealDemoAccount(t, store, 42)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	server, err := NewServer(Config{
		StaticDir:      staticDir,
		DBPath:         filepath.Join(tmpDir, "portal.db"),
		JWTSecret:      []byte("test-secret-key-that-is-long-enough"),
		TeacherToken:   "test-teacher-token",
		MaterialsDir:   filepath.Join(tmpDir, "materials"),
		SubmissionsDir: filepath.Join(tmpDir, "submissions"),
		DemoPassword:   demoTestPassword,
	})
	if err != nil {
		t.Fatalf("server must start despite the demo username conflict: %v", err)
	}
	t.Cleanup(func() {
		if err := server.Close(); err != nil {
			t.Errorf("closing server: %v", err)
		}
	})
	if server.demoEnabled {
		t.Error("demo must be disabled when a real student owns the username")
	}

	// The real student logs in with their own password and is not read-only.
	body, _ := json.Marshal(map[string]string{"username": demoUsername, "password": "real-student-pass"})
	req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("real student login failed: %d %s", rec.Code, rec.Body.String())
	}
	change, _ := json.Marshal(map[string]string{"currentPassword": "real-student-pass", "newPassword": "new-real-pass-1"})
	rec = demoRequest(t, server.Handler(), rec.Result().Cookies(), http.MethodPost, "/api/change-password", change)
	if rec.Code == http.StatusForbidden {
		t.Error("real student must not be treated as the read-only demo account")
	}
}
