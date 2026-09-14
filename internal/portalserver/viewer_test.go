package portalserver

import (
	"archive/zip"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newViewerTestServer builds a server whose JPlag jar is a fake zip holding a
// minimal report viewer, plus a portal static dir with its own index page.
func newViewerTestServer(t *testing.T) *Server {
	t.Helper()
	tmpDir := t.TempDir()
	staticDir := filepath.Join(tmpDir, "static")
	if err := os.MkdirAll(filepath.Join(staticDir, "assets"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("portal-index"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staticDir, "assets", "portal.js"), []byte("portal-js"), 0644); err != nil {
		t.Fatal(err)
	}

	jarPath := filepath.Join(tmpDir, "jplag.jar")
	jar, err := os.Create(jarPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(jar)
	for name, content := range map[string]string{
		"report-viewer/index.html":        "viewer-index",
		"report-viewer/assets/viewer.js":  "viewer-js",
		"report-viewer/favicon.ico":       "viewer-favicon",
		"report-viewer/nested/deep/x.txt": "viewer-deep",
		"unrelated/file.txt":              "not-viewer",
	} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := jar.Close(); err != nil {
		t.Fatal(err)
	}

	server, err := NewServer(Config{
		StaticDir:      staticDir,
		DBPath:         filepath.Join(tmpDir, "portal.db"),
		JWTSecret:      []byte("test-secret-key-that-is-long-enough"),
		TeacherToken:   "test-teacher-token",
		SubmissionsDir: filepath.Join(tmpDir, "submissions"),
		JPlagJar:       jarPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { server.Close() })
	return server
}

func get(t *testing.T, handler http.Handler, path string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestPlagViewerServing(t *testing.T) {
	server := newViewerTestServer(t)
	handler := server.Handler()

	for path, want := range map[string]string{
		"/overview":           "viewer-index",
		"/comparison/1/2":     "viewer-index",
		"/cluster/3":          "viewer-index",
		"/info":               "viewer-index",
		"/assets/viewer.js":   "viewer-js",
		"/favicon.ico":        "viewer-favicon",
		"/nested/deep/x.txt":  "viewer-deep",
		"/":                   "portal-index",
		"/admin/submissions":  "portal-index",
		"/assets/portal.js":   "portal-js",
		"/unrelated/file.txt": "portal-index", // only report-viewer/* is extracted
	} {
		rec := get(t, handler, path)
		if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != want {
			t.Errorf("GET %s = %d %q, want 200 %q", path, rec.Code, strings.TrimSpace(rec.Body.String()), want)
		}
	}
}

func TestPlagViewerMissing(t *testing.T) {
	server, handler := newTestServerWithSubmissions(t) // jar path does not exist
	_ = server

	rec := get(t, handler, "/overview")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET /overview without viewer = %d, want 503", rec.Code)
	}
	rec = get(t, handler, "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / without viewer = %d, want 200", rec.Code)
	}
}

func TestAdminSessionCookie(t *testing.T) {
	server := newViewerTestServer(t)
	handler := server.Handler()

	// The session endpoint requires bearer auth and sets the cookie.
	req := httptest.NewRequest(http.MethodPost, "/api/admin/session", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("session without token = %d, want 401", rec.Code)
	}
	req = httptest.NewRequest(http.MethodPost, "/api/admin/session", nil)
	req.Header.Set("Authorization", "Bearer test-teacher-token")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("session with token = %d %s", rec.Code, rec.Body.String())
	}
	var sessionCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == adminCookieName {
			sessionCookie = c
		}
	}
	if sessionCookie == nil || sessionCookie.Value != "test-teacher-token" || !sessionCookie.HttpOnly {
		t.Fatalf("session cookie: %+v", sessionCookie)
	}

	// The cookie alone authenticates admin endpoints; a wrong value does not.
	if rec := get(t, handler, "/api/admin/submissions/queue", sessionCookie); rec.Code != http.StatusOK {
		t.Fatalf("admin endpoint with cookie = %d", rec.Code)
	}
	bad := &http.Cookie{Name: adminCookieName, Value: "wrong"}
	if rec := get(t, handler, "/api/admin/submissions/queue", bad); rec.Code != http.StatusUnauthorized {
		t.Fatalf("admin endpoint with bad cookie = %d, want 401", rec.Code)
	}
	if rec := get(t, handler, "/api/admin/submissions/queue"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("admin endpoint without auth = %d, want 401", rec.Code)
	}
}
