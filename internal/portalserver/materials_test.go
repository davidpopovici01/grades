package portalserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/davidpopovici01/grades/internal/portalauth"
)

// newMaterialsTestServer publishes course 1/1 (student 1) and course 2/1
// (student 2) and returns the server, its handler, and student 1's cookies.
func newMaterialsTestServer(t *testing.T, cookieDomain string) (*Server, http.Handler, []*http.Cookie) {
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
		CookieDomain: cookieDomain,
		MaterialsDir: filepath.Join(tmpDir, "materials"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { server.Close() })

	now := time.Now().UTC().Format(time.RFC3339)
	publish := func(studentID int, username string, courseYearID, termID int, courseName string) {
		t.Helper()
		hash, salt, err := portalauth.HashPassword("testpass")
		if err != nil {
			t.Fatal(err)
		}
		err = server.store.PublishCourse(&PublishRequest{
			Accounts: []portalauth.Account{
				{StudentID: studentID, Username: username, PasswordSalt: salt, PasswordHash: hash, PasswordChangedAt: now},
			},
			Course: CourseInfo{CourseYearID: courseYearID, TermID: termID, CourseName: courseName, TermName: "Fall 2026", PublishedAt: now},
			Students: []struct {
				StudentID int             `json:"studentId"`
				Snapshot  json.RawMessage `json:"snapshot"`
			}{
				{StudentID: studentID, Snapshot: json.RawMessage(`{"firstName":"A"}`)},
			},
		})
		if err != nil {
			t.Fatalf("publish: %v", err)
		}
	}
	publish(1, "john.doe", 1, 1, "Test Course")
	publish(2, "jane.doe", 2, 1, "Other Course")

	handler := server.Handler()
	loginBody, _ := json.Marshal(map[string]string{"username": "john.doe", "password": "testpass"})
	req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(loginBody))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login failed: %d %s", rec.Code, rec.Body.String())
	}
	return server, handler, rec.Result().Cookies()
}

func uploadMaterial(t *testing.T, handler http.Handler, courseYearID, termID int, filename, content, token string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/admin/materials/upload?courseYearId=%d&termId=%d", courseYearID, termID), &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestMaterialsUploadListDownloadDelete(t *testing.T) {
	_, handler, cookies := newMaterialsTestServer(t, "")

	// Upload requires the teacher token.
	if rec := uploadMaterial(t, handler, 1, 1, "notes.pdf", "hello world", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("upload without token: got %d, want 401", rec.Code)
	}

	// Upload to an unpublished course is a 404.
	if rec := uploadMaterial(t, handler, 99, 9, "notes.pdf", "hello world", "test-teacher-token"); rec.Code != http.StatusNotFound {
		t.Fatalf("upload to unknown course: got %d, want 404", rec.Code)
	}

	// Upload a file.
	if rec := uploadMaterial(t, handler, 1, 1, "notes.pdf", "hello world", "test-teacher-token"); rec.Code != http.StatusOK {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body.String())
	}

	// The student sees it in the materials list.
	req := httptest.NewRequest(http.MethodGet, "/api/materials", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("materials list: %d %s", rec.Code, rec.Body.String())
	}
	var listResp struct {
		Courses []CourseMaterials `json:"courses"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listResp); err != nil {
		t.Fatal(err)
	}
	if len(listResp.Courses) != 1 || len(listResp.Courses[0].Files) != 1 {
		t.Fatalf("unexpected materials list: %+v", listResp)
	}
	file := listResp.Courses[0].Files[0]
	if file.Name != "notes.pdf" || file.Size != int64(len("hello world")) {
		t.Fatalf("unexpected file entry: %+v", file)
	}

	// The student can download it.
	req = httptest.NewRequest(http.MethodGet, "/api/materials/download/1/1/notes.pdf", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("download: %d %s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "hello world" {
		t.Fatalf("download body = %q", rec.Body.String())
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment") || !strings.Contains(cd, "notes.pdf") {
		t.Fatalf("Content-Disposition = %q", cd)
	}

	// Path traversal stays inside the course directory. (Forward-slash ".."
	// segments never even reach the handler — ServeMux redirects them away.)
	req = httptest.NewRequest(http.MethodGet, "/api/materials/download/1/1/..%5C..%5Cportal.db", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("traversal: got %d, want 404", rec.Code)
	}

	// Delete removes it and the course drops out of the student's list.
	req = httptest.NewRequest(http.MethodDelete, "/api/admin/materials/delete?courseYearId=1&termId=1&file=notes.pdf", nil)
	req.Header.Set("Authorization", "Bearer test-teacher-token")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/materials", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if err := json.Unmarshal(rec.Body.Bytes(), &listResp); err != nil {
		t.Fatal(err)
	}
	if len(listResp.Courses) != 0 {
		t.Fatalf("expected no courses after delete, got %+v", listResp.Courses)
	}
}

func TestMaterialsRequireLoginAndEnrollment(t *testing.T) {
	server, handler, _ := newMaterialsTestServer(t, "")

	if rec := uploadMaterial(t, handler, 1, 1, "notes.pdf", "secret", "test-teacher-token"); rec.Code != http.StatusOK {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body.String())
	}

	// No cookie: 401 on list and download.
	for _, path := range []string{"/api/materials", "/api/materials/download/1/1/notes.pdf"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s without cookie: got %d, want 401", path, rec.Code)
		}
	}

	// Student 2 is not enrolled in course 1/1: 403 on download.
	token, err := server.jwt.Sign(2, "jane.doe", tokenDuration)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/materials/download/1/1/notes.pdf", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: token})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("unenrolled download: got %d, want 403", rec.Code)
	}
}

// TestLoginSetsDomainCookie verifies that with CookieDomain configured the
// session cookie is domain-wide and the legacy host-only cookie is expired.
func TestLoginSetsDomainCookie(t *testing.T) {
	_, handler, _ := newMaterialsTestServer(t, "mrpopovici.com")

	loginBody, _ := json.Marshal(map[string]string{"username": "john.doe", "password": "testpass"})
	req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(loginBody))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login failed: %d %s", rec.Code, rec.Body.String())
	}

	var sessionCookie, legacyClear *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name != cookieName {
			continue
		}
		if c.MaxAge < 0 {
			legacyClear = c
		} else {
			sessionCookie = c
		}
	}
	if sessionCookie == nil {
		t.Fatal("no session cookie set")
	}
	if sessionCookie.Domain != "mrpopovici.com" {
		t.Fatalf("session cookie Domain = %q, want mrpopovici.com", sessionCookie.Domain)
	}
	if legacyClear == nil {
		t.Fatal("expected a legacy host-only cookie to be expired on login")
	}
	if legacyClear.Domain != "" {
		t.Fatalf("legacy clear cookie should be host-only, got Domain=%q", legacyClear.Domain)
	}
}

// TestLogoutClearsBothCookieVariants verifies logout expires the domain-wide
// and the host-only cookie.
func TestLogoutClearsBothCookieVariants(t *testing.T) {
	_, handler, cookies := newMaterialsTestServer(t, "mrpopovici.com")

	req := httptest.NewRequest(http.MethodPost, "/api/logout", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("logout: %d %s", rec.Code, rec.Body.String())
	}

	var domainClear, hostClear bool
	for _, c := range rec.Result().Cookies() {
		if c.Name != cookieName || c.MaxAge >= 0 {
			continue
		}
		if c.Domain == "mrpopovici.com" {
			domainClear = true
		}
		if c.Domain == "" {
			hostClear = true
		}
	}
	if !domainClear || !hostClear {
		t.Fatalf("logout must clear both cookie variants (domain=%v, host=%v)", domainClear, hostClear)
	}
}
