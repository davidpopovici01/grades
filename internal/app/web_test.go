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

	if err := portalApp.InitStudentPortalAccounts("TempPass123", false); err != nil {
		t.Fatalf("init accounts: %v", err)
	}

	// No staticDir: the preview falls back to the legacy template page.
	server := &portalServer{
		app:      portalApp,
		now:      portalFixedNow,
		sessions: map[string]portalSession{},
	}
	testServer := httptest.NewServer(server.routes())
	defer testServer.Close()

	client := newPortalHTTPClient(t)

	var login map[string]any
	status := portalJSONRequest(t, client, http.MethodPost, testServer.URL+"/api/login", map[string]string{
		"username": "alice.brown",
		"password": "TempPass123",
	}, &login)
	if status != http.StatusOK {
		t.Fatalf("login status: got %d", status)
	}
	if login["mustChangePassword"] != false {
		t.Fatalf("expected mustChangePassword false, got %#v", login["mustChangePassword"])
	}
	if login["username"] != "alice.brown" {
		t.Fatalf("expected login to return username alice.brown, got %#v", login["username"])
	}
	if login["studentId"] != float64(1) {
		t.Fatalf("expected login to return studentId 1, got %#v", login["studentId"])
	}

	var me map[string]any
	status = portalJSONRequest(t, client, http.MethodGet, testServer.URL+"/api/me", nil, &me)
	if status != http.StatusOK {
		t.Fatalf("me status: got %d", status)
	}
	if me["username"] != "alice.brown" {
		t.Fatalf("expected username alice.brown, got %#v", me["username"])
	}

	// /api/grades matches the VPS portal shape: {studentId, courses: [...]},
	// built live from the local database.
	var grades struct {
		StudentID int                   `json:"studentId"`
		Courses   []portalStudentCourse `json:"courses"`
	}
	status = portalJSONRequest(t, client, http.MethodGet, testServer.URL+"/api/grades", nil, &grades)
	if status != http.StatusOK {
		t.Fatalf("grades status: got %d", status)
	}
	if grades.StudentID != 1 {
		t.Fatalf("expected studentId 1, got %d", grades.StudentID)
	}
	if len(grades.Courses) != 1 {
		t.Fatalf("expected 1 course, got %d", len(grades.Courses))
	}
	course := grades.Courses[0]
	if course.CourseYearID != 1 || course.TermID != 1 {
		t.Fatalf("unexpected course ids: %+v", course)
	}
	if course.CourseName != "APCSA" || course.TermName != "Fall 2026" {
		t.Fatalf("unexpected course names: %+v", course)
	}
	if course.PublishedAt == "" {
		t.Fatalf("expected publishedAt on course entry")
	}
	snapshot := course.Snapshot
	if snapshot.FirstName != "Alice" || len(snapshot.Assignments) != 2 {
		t.Fatalf("unexpected grades payload: %+v", snapshot)
	}
	if snapshot.Assignments[1].MaxPoints != 100 {
		t.Fatalf("expected final max points 100, got %d", snapshot.Assignments[1].MaxPoints)
	}
	if snapshot.WeightedTotalLabel == "" {
		t.Fatalf("expected weighted total label in grades payload")
	}
	if snapshot.Assignments[0].Flags == nil || len(snapshot.Assignments[0].Flags) != 2 {
		t.Fatalf("expected multiple flags for first assignment, got %+v", snapshot.Assignments[0].Flags)
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
	if !strings.Contains(page, "Student Grades Portal") {
		t.Fatalf("expected legacy fallback page without portal-web/dist")
	}
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
		"username": "alice.brown",
		"password": "BetterPass456",
	}, &login)
	if status != http.StatusOK {
		t.Fatalf("new password login status: got %d", status)
	}
}

// TestPortalServerServesSPAWhenDistExists verifies that the preview serves the
// built React SPA: static assets directly and index.html for client-side routes.
func TestPortalServerServesSPAWhenDistExists(t *testing.T) {
	portalApp, home := newPortalTestApp(t)
	defer portalApp.Close()
	seedPortalData(t, home)

	portalApp.v.Set("context.year", "2026-27")
	portalApp.v.Set("context.term_id", 1)
	portalApp.v.Set("context.course_year_id", 1)
	if err := portalApp.v.WriteConfig(); err != nil {
		t.Fatalf("write config: %v", err)
	}

	dist := filepath.Join(t.TempDir(), "dist")
	if err := os.MkdirAll(filepath.Join(dist, "assets"), 0o755); err != nil {
		t.Fatalf("mkdir dist: %v", err)
	}
	indexHTML := `<!doctype html><html><body><div id="root"></div><script src="/assets/app.js"></script></body></html>`
	if err := os.WriteFile(filepath.Join(dist, "index.html"), []byte(indexHTML), 0o644); err != nil {
		t.Fatalf("write index.html: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dist, "assets", "app.js"), []byte("console.log('spa')"), 0o644); err != nil {
		t.Fatalf("write asset: %v", err)
	}

	server := &portalServer{app: portalApp, staticDir: dist, now: portalFixedNow, sessions: map[string]portalSession{}}
	testServer := httptest.NewServer(server.routes())
	defer testServer.Close()

	// Client-side routes and the root all get the SPA index.html.
	for _, path := range []string{"/", "/what-if", "/change-password", "/no-such-route"} {
		resp, err := http.Get(testServer.URL + path)
		if err != nil {
			t.Fatalf("get %s: %v", path, err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("get %s: status %d", path, resp.StatusCode)
		}
		if !strings.Contains(string(body), `id="root"`) {
			t.Fatalf("expected SPA index.html for %s, got:\n%s", path, body)
		}
	}

	// Real static files are served with their content.
	resp, err := http.Get(testServer.URL + "/assets/app.js")
	if err != nil {
		t.Fatalf("get asset: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read asset: %v", err)
	}
	if string(body) != "console.log('spa')" {
		t.Fatalf("unexpected asset body: %s", body)
	}

	// API routes still answer as JSON, not index.html.
	status := portalJSONRequest(t, newPortalHTTPClient(t), http.MethodGet, testServer.URL+"/api/me", nil, &map[string]any{})
	if status != http.StatusUnauthorized {
		t.Fatalf("expected 401 for /api/me without session, got %d", status)
	}

	// Unknown API paths (e.g. admin routes, which the preview does not have)
	// get a JSON 404, never the index.html fallback.
	status = portalJSONRequest(t, newPortalHTTPClient(t), http.MethodGet, testServer.URL+"/api/admin/courses", nil, &map[string]any{})
	if status != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown /api path, got %d", status)
	}
}

// TestLocatePortalWebDist verifies the built frontend is discovered relative to
// the working directory and that "" is returned when it is missing.
func TestLocatePortalWebDist(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	if got := locatePortalWebDist(); got != "" {
		t.Fatalf("expected no dist found, got %q", got)
	}

	dist := filepath.Join(tmp, "portal-web", "dist")
	if err := os.MkdirAll(dist, 0o755); err != nil {
		t.Fatalf("mkdir dist: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dist, "index.html"), []byte("<html></html>"), 0o644); err != nil {
		t.Fatalf("write index.html: %v", err)
	}
	if got := locatePortalWebDist(); got != dist {
		t.Fatalf("expected %q, got %q", dist, got)
	}
}

// TestPortalCoursesForStudentMultipleCourses verifies /api/grades lists one
// entry per enrolled course, ordered by course name.
func TestPortalCoursesForStudentMultipleCourses(t *testing.T) {
	portalApp, home := newPortalTestApp(t)
	defer portalApp.Close()
	seedPortalData(t, home)

	conn, err := db.Open(filepath.Join(home, "grades.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	statements := []string{
		`INSERT INTO terms(term_id, name, start_date, end_date) VALUES (2, 'Spring 2027', '2027-01-10', '2027-06-20')`,
		`INSERT INTO courses(course_id, name) VALUES (2, 'APCSP')`,
		`INSERT INTO course_years(course_year_id, course_id, name) VALUES (2, 2, 'APCSP 2026-27')`,
		`INSERT INTO course_year_terms(course_year_id, term_id) VALUES (2, 2)`,
		`INSERT INTO sections(section_id, course_year_id, name) VALUES (2, 2, '12C')`,
		`INSERT INTO section_enrollments(section_id, student_pk, term_id, start_date, status) VALUES (2, 1, 2, '2027-01-10', 'active')`,
	}
	for _, stmt := range statements {
		if _, err := conn.Exec(stmt); err != nil {
			conn.Close()
			t.Fatalf("seed stmt failed: %v", err)
		}
	}
	conn.Close()

	portalApp.v.Set("context.year", "2026-27")
	portalApp.v.Set("context.term_id", 1)
	portalApp.v.Set("context.course_year_id", 1)
	if err := portalApp.v.WriteConfig(); err != nil {
		t.Fatalf("write config: %v", err)
	}

	courses, err := portalApp.portalCoursesForStudent(1)
	if err != nil {
		t.Fatalf("portal courses for student: %v", err)
	}
	if len(courses) != 2 {
		t.Fatalf("expected 2 courses, got %d", len(courses))
	}
	if courses[0].CourseName != "APCSA" || courses[1].CourseName != "APCSP" {
		t.Fatalf("expected courses ordered by name, got %q then %q", courses[0].CourseName, courses[1].CourseName)
	}
	if courses[0].Snapshot.FirstName != "Alice" || len(courses[0].Snapshot.Assignments) != 2 {
		t.Fatalf("unexpected APCSA snapshot: %+v", courses[0].Snapshot)
	}
	if courses[1].Snapshot.StudentID != 1 || len(courses[1].Snapshot.Assignments) != 0 {
		t.Fatalf("unexpected APCSP snapshot: %+v", courses[1].Snapshot)
	}

	// A student enrolled in only one course gets only that course.
	courses, err = portalApp.portalCoursesForStudent(2)
	if err != nil {
		t.Fatalf("portal courses for student 2: %v", err)
	}
	if len(courses) != 1 || courses[0].CourseName != "APCSA" {
		t.Fatalf("expected only APCSA for student 2, got %+v", courses)
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
	t.Setenv("GRADES_NO_OPEN", "1")
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

func TestPortalImprovementTipsRespectShowInOverview(t *testing.T) {
	portalApp, home := newPortalTestApp(t)
	defer portalApp.Close()
	conn, err := db.Open(filepath.Join(home, "grades.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := migrate.Up(conn); err != nil {
		conn.Close()
		t.Fatalf("migrate: %v", err)
	}
	statements := []string{
		`INSERT INTO terms(term_id, name, start_date, end_date) VALUES (1, 'Fall 2026', '2026-08-15', '2026-12-20')`,
		`INSERT INTO courses(course_id, name) VALUES (1, 'APCSA')`,
		`INSERT INTO course_years(course_year_id, course_id, name) VALUES (1, 1, 'APCSA 2026-27')`,
		`INSERT INTO course_year_terms(course_year_id, term_id) VALUES (1, 1)`,
		`INSERT INTO sections(section_id, course_year_id, name) VALUES (1, 1, '12A')`,
		`INSERT INTO categories(category_id, name) VALUES (1, 'Homework'), (2, 'Quiz')`,
		`INSERT INTO students(student_pk, first_name, last_name, school_student_id) VALUES (1, 'Alice', 'Brown', '3001')`,
		`INSERT INTO section_enrollments(section_id, student_pk, term_id, start_date, status) VALUES (1, 1, 1, '2026-08-15', 'active')`,
		`INSERT INTO category_schemes(scheme_id, name) VALUES (1, 'Default')`,
		`INSERT INTO category_scheme_weights(scheme_id, category_id, weight_percent) VALUES (1, 1, 50), (1, 2, 50)`,
		`UPDATE course_year_terms SET scheme_id = 1 WHERE course_year_id = 1 AND term_id = 1`,
		`INSERT INTO category_grading_policies(course_year_id, term_id, category_id, scheme_key, default_pass_percent) VALUES (1, 1, 1, 'completion', 80), (1, 1, 2, 'completion', 80)`,
		`INSERT INTO assignments(assignment_id, course_year_id, term_id, category_id, title, max_points, pass_percent) VALUES (1, 1, 1, 1, 'HW1', 10, 80), (2, 1, 1, 2, 'Quiz 1', 10, 80)`,
		`INSERT INTO grades(assignment_id, student_pk, score, flags_bitmask, redo_count) VALUES (1, 1, 0, 2, 0), (2, 1, 0, 2, 0)`,
	}
	for _, stmt := range statements {
		if _, err := conn.Exec(stmt); err != nil {
			conn.Close()
			t.Fatalf("seed stmt failed: %v", err)
		}
	}
	conn.Close()

	portalApp.v.Set("context.year", "2026-27")
	portalApp.v.Set("context.term_id", 1)
	portalApp.v.Set("context.course_year_id", 1)
	if err := portalApp.v.WriteConfig(); err != nil {
		t.Fatalf("write config: %v", err)
	}

	snapshot, err := portalApp.buildPortalCourseSnapshot(1, 1)
	if err != nil {
		t.Fatalf("build portal course snapshot: %v", err)
	}
	if len(snapshot.Students) != 1 {
		t.Fatalf("expected 1 student snapshot, got %d", len(snapshot.Students))
	}
	grades := snapshot.Students[0]

	if len(grades.Categories) != 2 {
		t.Fatalf("expected 2 categories, got %d", len(grades.Categories))
	}
	if len(grades.Assignments) != 2 {
		t.Fatalf("expected 2 assignments, got %d", len(grades.Assignments))
	}

	// Quiz is hidden from overview by default, Homework is visible.
	var homeworkVisible, quizVisible bool
	for _, cat := range grades.Categories {
		if cat.CategoryName == "Homework" {
			homeworkVisible = cat.ShowInOverview
		}
		if cat.CategoryName == "Quiz" {
			quizVisible = cat.ShowInOverview
		}
	}
	if !homeworkVisible {
		t.Fatalf("expected Homework to be visible in overview")
	}
	if quizVisible {
		t.Fatalf("expected Quiz to be hidden from overview by default")
	}

	for _, a := range grades.Assignments {
		if a.CategoryName == "Quiz" && a.ShowInOverview {
			t.Fatalf("expected Quiz assignment to have ShowInOverview false")
		}
		if a.CategoryName == "Homework" && !a.ShowInOverview {
			t.Fatalf("expected Homework assignment to have ShowInOverview true")
		}
	}

	// Only the Homework missing assignment should appear in tips.
	foundHW := false
	foundQuiz := false
	for _, tip := range grades.ImprovementTips {
		if strings.Contains(tip, "HW1") {
			foundHW = true
		}
		if strings.Contains(tip, "Quiz 1") {
			foundQuiz = true
		}
	}
	if !foundHW {
		t.Fatalf("expected improvement tip for HW1")
	}
	if foundQuiz {
		t.Fatalf("did not expect improvement tip for Quiz 1")
	}
}

func portalFixedNow() time.Time {
	return time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)
}
