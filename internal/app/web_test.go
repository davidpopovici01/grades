package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/davidpopovici01/grades/internal/db"
	"github.com/davidpopovici01/grades/internal/migrate"
)

func TestStudentPortalWorkflow(t *testing.T) {
	portalApp, home := newPortalTestApp(t)
	defer portalApp.Close()
	seedPortalData(t, home)

	portalApp.v.Set("context.year", "2026-27")
	portalApp.v.Set("context.term_id", 1)
	portalApp.v.Set("context.course_year_id", 1)
	if err := portalApp.v.WriteConfig(); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if err := portalApp.InitStudentPortalAccounts("TempPass123"); err != nil {
		t.Fatalf("init accounts: %v", err)
	}
	if err := portalApp.PublishStudentPortal(""); err != nil {
		t.Fatalf("publish portal: %v", err)
	}

	server := &portalServer{
		app:        portalApp,
		publishDir: filepath.Join(home, "..", "gradesPublished"),
		now:        portalFixedNow,
		sessions:   map[string]portalSession{},
	}
	testServer := httptest.NewServer(server.routes())
	defer testServer.Close()

	client := newPortalHTTPClient(t)

	var login map[string]any
	status := portalJSONRequest(t, client, http.MethodPost, testServer.URL+"/api/login", map[string]string{
		"username": "3001",
		"password": "TempPass123",
	}, &login)
	if status != http.StatusOK {
		t.Fatalf("login status: got %d", status)
	}
	if login["mustChangePassword"] != true {
		t.Fatalf("expected mustChangePassword true, got %#v", login["mustChangePassword"])
	}

	var me map[string]any
	status = portalJSONRequest(t, client, http.MethodGet, testServer.URL+"/api/me", nil, &me)
	if status != http.StatusOK {
		t.Fatalf("me status: got %d", status)
	}
	if me["username"] != "3001" {
		t.Fatalf("expected username 3001, got %#v", me["username"])
	}

	var grades portalStudentSnapshot
	status = portalJSONRequest(t, client, http.MethodGet, testServer.URL+"/api/grades", nil, &grades)
	if status != http.StatusOK {
		t.Fatalf("grades status: got %d", status)
	}
	if grades.FirstName != "Alice" || len(grades.Assignments) != 2 {
		t.Fatalf("unexpected grades payload: %+v", grades)
	}
	if grades.Assignments[1].MaxPoints != 100 {
		t.Fatalf("expected final max points 100, got %d", grades.Assignments[1].MaxPoints)
	}
	if grades.WeightedTotalLabel == "" {
		t.Fatalf("expected weighted total label in grades payload")
	}
	if grades.Assignments[0].Flags == nil || len(grades.Assignments[0].Flags) != 2 {
		t.Fatalf("expected multiple flags for first assignment, got %+v", grades.Assignments[0].Flags)
	}

	resp, err := client.Get(testServer.URL + "/")
	if err != nil {
		t.Fatalf("get portal page: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read portal page: %v", err)
	}
	page := string(body)
	if strings.Contains(page, "Keeping password change inline is enough for now") {
		t.Fatalf("internal password guidance text leaked into user page")
	}
	if strings.Contains(page, "late penalty") {
		t.Fatalf("late penalty advice leaked into user page")
	}
	if strings.Contains(page, "Submissions") {
		t.Fatalf("submissions UI leaked into user page")
	}

	var changed map[string]any
	status = portalJSONRequest(t, client, http.MethodPost, testServer.URL+"/api/change-password", map[string]string{
		"currentPassword": "TempPass123",
		"newPassword":     "BetterPass456",
	}, &changed)
	if status != http.StatusOK {
		t.Fatalf("change password status: got %d", status)
	}

	status = portalJSONRequest(t, client, http.MethodGet, testServer.URL+"/api/me", nil, &me)
	if status != http.StatusOK {
		t.Fatalf("me after password change status: got %d", status)
	}
	if me["mustChangePassword"] != false {
		t.Fatalf("expected mustChangePassword false after change, got %#v", me["mustChangePassword"])
	}

	status = portalJSONRequest(t, client, http.MethodPost, testServer.URL+"/api/logout", nil, &changed)
	if status != http.StatusOK {
		t.Fatalf("logout status: got %d", status)
	}

	var errPayload map[string]any
	status = portalJSONRequest(t, client, http.MethodPost, testServer.URL+"/api/login", map[string]string{
		"username": "3001",
		"password": "TempPass123",
	}, &errPayload)
	if status != http.StatusUnauthorized {
		t.Fatalf("expected old password login failure, got %d", status)
	}

	status = portalJSONRequest(t, client, http.MethodPost, testServer.URL+"/api/login", map[string]string{
		"username": "3001",
		"password": "BetterPass456",
	}, &login)
	if status != http.StatusOK {
		t.Fatalf("new password login status: got %d", status)
	}
}

func newPortalTestApp(t *testing.T) (*App, string) {
	t.Helper()
	home := filepath.Join(t.TempDir(), ".grades")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatalf("mkdir home: %v", err)
	}
	t.Setenv("GRADES_HOME", home)
	t.Setenv("GRADES_DB_PATH", filepath.Join(home, "grades.db"))
	app, err := New(strings.NewReader(""), ioDiscard{}, ioDiscard{})
	if err != nil {
		t.Fatalf("new app: %v", err)
	}
	return app, home
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }

func seedPortalData(t *testing.T, home string) {
	t.Helper()
	conn, err := db.Open(filepath.Join(home, "grades.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer conn.Close()
	if err := migrate.Up(conn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	statements := []string{
		`INSERT INTO terms(term_id, name, start_date, end_date) VALUES (1, 'Fall 2026', '2026-08-15', '2026-12-20')`,
		`INSERT INTO courses(course_id, name) VALUES (1, 'APCSA')`,
		`INSERT INTO course_years(course_year_id, course_id, name) VALUES (1, 1, 'APCSA 2026-27')`,
		`INSERT INTO course_year_terms(course_year_id, term_id) VALUES (1, 1)`,
		`INSERT INTO sections(section_id, course_year_id, name) VALUES (1, 1, '12A')`,
		`INSERT INTO categories(category_id, name) VALUES (1, 'Homework'), (2, 'Exam')`,
		`INSERT INTO students(student_pk, first_name, last_name, school_student_id) VALUES (1, 'Alice', 'Brown', '3001'), (2, 'Bob', 'Zhang', '3002')`,
		`INSERT INTO section_enrollments(section_id, student_pk, term_id, start_date, status) VALUES (1, 1, 1, '2026-08-15', 'active'), (1, 2, 1, '2026-08-15', 'active')`,
		`INSERT INTO category_schemes(scheme_id, name) VALUES (1, 'Default')`,
		`INSERT INTO category_scheme_weights(scheme_id, category_id, weight_percent) VALUES (1, 1, 40), (1, 2, 60)`,
		`UPDATE course_year_terms SET scheme_id = 1 WHERE course_year_id = 1 AND term_id = 1`,
		`INSERT INTO category_grading_policies(course_year_id, term_id, category_id, scheme_key, default_pass_percent) VALUES (1, 1, 1, 'completion', 80), (1, 1, 2, 'average', 0)`,
		`INSERT INTO assignments(assignment_id, course_year_id, term_id, category_id, title, max_points, pass_percent) VALUES (1, 1, 1, 1, 'HW1', 10, 80), (2, 1, 1, 2, 'Final Exam', 100, 0)`,
		`INSERT INTO grades(assignment_id, student_pk, score, flags_bitmask, redo_count) VALUES (1, 1, 10, 9, 1), (1, 2, 7, 0, 0), (2, 1, 88, 0, 0), (2, 2, 91, 0, 0)`,
	}
	for _, stmt := range statements {
		if _, err := conn.Exec(stmt); err != nil {
			t.Fatalf("seed stmt failed: %v", err)
		}
	}
}

func newPortalHTTPClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookie jar: %v", err)
	}
	return &http.Client{Jar: jar}
}

func portalJSONRequest(t *testing.T, client *http.Client, method, url string, body any, out any) int {
	t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request: %v", err)
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil && !errors.Is(err, io.EOF) {
			t.Fatalf("decode response: %v", err)
		}
	}
	return resp.StatusCode
}

func portalFixedNow() time.Time {
	return time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)
}
