package portalserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/davidpopovici01/grades/internal/portalauth"
)

// newSubmissionsTestServer publishes course 1/1 with students 1 (john.doe) and
// 2 (jane.doe) and returns the server, its handler, and john's cookies.
func newSubmissionsTestServer(t *testing.T) (*Server, http.Handler, []*http.Cookie) {
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
		JPlagJar:       filepath.Join(tmpDir, "jplag.jar"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { server.Close() })

	now := time.Now().UTC().Format(time.RFC3339)
	accounts := []portalauth.Account{}
	for i, username := range []string{"john.doe", "jane.doe"} {
		hash, salt, err := portalauth.HashPassword("testpass")
		if err != nil {
			t.Fatal(err)
		}
		accounts = append(accounts, portalauth.Account{
			StudentID: i + 1, Username: username, PasswordSalt: salt, PasswordHash: hash, PasswordChangedAt: now,
		})
	}
	err = server.store.PublishCourse(&PublishRequest{
		Accounts: accounts,
		Course:   CourseInfo{CourseYearID: 1, TermID: 1, CourseName: "Test Course", TermName: "Fall 2026", PublishedAt: now},
		Students: []struct {
			StudentID int             `json:"studentId"`
			Snapshot  json.RawMessage `json:"snapshot"`
		}{
			{StudentID: 1, Snapshot: json.RawMessage(`{"firstName":"John","lastName":"Doe"}`)},
			{StudentID: 2, Snapshot: json.RawMessage(`{"firstName":"Jane","lastName":"Doe"}`)},
		},
	})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}

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

// studentCookies mints a session cookie without a login round trip.
func studentCookies(t *testing.T, server *Server, studentID int, username string) []*http.Cookie {
	t.Helper()
	token, err := server.jwt.Sign(studentID, username, tokenDuration)
	if err != nil {
		t.Fatal(err)
	}
	return []*http.Cookie{{Name: cookieName, Value: token}}
}

func adminRequest(t *testing.T, handler http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(data)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Authorization", "Bearer test-teacher-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// adminRawRequest issues an authenticated admin request with a raw text body.
func adminRawRequest(t *testing.T, handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-teacher-token")
	req.Header.Set("Content-Type", "text/plain")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// createTestAssignment creates an assignment via the admin endpoint and
// returns its id.
func createTestAssignment(t *testing.T, handler http.Handler, body map[string]any) int64 {
	t.Helper()
	base := map[string]any{
		"courseYearId":      1,
		"termId":            1,
		"title":             "HW1",
		"language":          "python",
		"expectedFilenames": "main.py",
	}
	for k, v := range body {
		base[k] = v
	}
	rec := adminRequest(t, handler, http.MethodPost, "/api/admin/submissions/assignments", base)
	if rec.Code != http.StatusOK {
		t.Fatalf("create assignment: %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Assignment struct {
			ID int64 `json:"id"`
		} `json:"assignment"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	return resp.Assignment.ID
}

// uploadTestFile uploads a harness file via the admin endpoint.
func uploadTestFile(t *testing.T, handler http.Handler, assignmentID int64, name, visibility, filename, content string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	if err := w.WriteField("name", name); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteField("visibility", visibility); err != nil {
		t.Fatal(err)
	}
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
		fmt.Sprintf("/api/admin/submissions/assignments/%d/tests", assignmentID), &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer test-teacher-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// uploadSubmission posts files (and optional extra form fields) to the student
// upload endpoint.
func uploadSubmission(t *testing.T, handler http.Handler, cookies []*http.Cookie, assignmentID int64,
	files map[string]string, fields map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if err := w.WriteField(k, fields[k]); err != nil {
			t.Fatal(err)
		}
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		part, err := w.CreateFormFile("file", name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write([]byte(files[name])); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/submissions/%d/files", assignmentID), &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func studentPost(t *testing.T, handler http.Handler, cookies []*http.Cookie, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// uploadSlot posts one file to a per-file slot, optionally with extra fields.
func uploadSlot(t *testing.T, handler http.Handler, cookies []*http.Cookie, assignmentID int64,
	slot, filename, content string, fields map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for k, v := range fields {
		if err := w.WriteField(k, v); err != nil {
			t.Fatal(err)
		}
	}
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
		fmt.Sprintf("/api/submissions/%d/slot/%s", assignmentID, slot), &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func studentGet(t *testing.T, handler http.Handler, cookies []*http.Cookie, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestParseExpectedFilenames(t *testing.T) {
	tests := []struct {
		name     string
		language string
		raw      string
		want     []string
		wantErr  bool
	}{
		{"single java", "java", "Main.java", []string{"Main.java"}, false},
		{"multiple python", "python", "main.py\nhelper.py\n", []string{"main.py", "helper.py"}, false},
		{"blank lines skipped", "python", "\nmain.py\n\n", []string{"main.py"}, false},
		{"missing code file", "java", "main.py", nil, true},
		{"mixed code and docs", "java", "Main.java\nreport.docx\ndata.xlsx", []string{"Main.java", "report.docx", "data.xlsx"}, false},
		{"alternative extensions", "files", "report.docx/pdf\nvideo.mp4", []string{"report.docx/pdf", "video.mp4"}, false},
		{"alternatives with dot prefix", "files", "report.docx/.pdf", []string{"report.docx/.pdf"}, false},
		{"alternative with full filename rejected", "files", "report.docx/other.pdf", nil, true},
		{"duplicate via alternatives", "files", "report.docx/pdf\nreport.pdf", nil, true},
		{"code ext via alternative", "java", "main.java/py", []string{"main.java/py"}, false},
		{"text with alternative", "text", "essay.txt/md", []string{"essay.txt/md"}, false},
		{"text accepts md", "text", "essay.md", []string{"essay.md"}, false},
		{"text requires txt or md", "text", "essay.docx", nil, true},
		{"text with extra files", "text", "essay.txt\nnotes.pdf", []string{"essay.txt", "notes.pdf"}, false},
		{"files any extension", "files", "presentation.xlsx\ndemo.mp4\ndoc.docx", []string{"presentation.xlsx", "demo.mp4", "doc.docx"}, false},
		{"uppercase extension ok", "python", "MAIN.PY", []string{"MAIN.PY"}, false},
		{"duplicate names", "python", "main.py\nMAIN.py", nil, true},
		{"paths rejected", "python", "../evil.py", nil, true},
		{"empty list", "python", "\n \n", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseExpectedFilenames(tt.language, tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if strings.Join(got, ",") != strings.Join(tt.want, ",") {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateUploadSet(t *testing.T) {
	expected := []string{"Main.java", "Helper.java"}
	tests := []struct {
		name    string
		files   []string
		wantErr string
	}{
		{"exact match", []string{"Main.java", "Helper.java"}, ""},
		{"case-insensitive", []string{"main.JAVA", "helper.java"}, ""},
		{"missing file", []string{"Main.java"}, "missing files: Helper.java"},
		{"extra file", []string{"Main.java", "Helper.java", "Extra.java"}, "unexpected file Extra.java"},
		{"duplicate file", []string{"Main.java", "main.java", "Helper.java"}, "unexpected file main.java"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := []uploadedFile{}
			for _, name := range tt.files {
				files = append(files, uploadedFile{name: name})
			}
			got := validateUploadSet(expected, files)
			if got != tt.wantErr {
				t.Fatalf("got %q, want %q", got, tt.wantErr)
			}
		})
	}
}

func TestSubmissionUploadValidation(t *testing.T) {
	_, handler, cookies := newSubmissionsTestServer(t)
	assignmentID := createTestAssignment(t, handler, map[string]any{
		"expectedFilenames": "main.py\nhelper.py",
	})

	// Missing a required file.
	rec := uploadSubmission(t, handler, cookies, assignmentID, map[string]string{"main.py": "print(1)"}, nil)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "missing files") {
		t.Fatalf("missing file: %d %s", rec.Code, rec.Body.String())
	}

	// An extra file.
	rec = uploadSubmission(t, handler, cookies, assignmentID, map[string]string{
		"main.py": "print(1)", "helper.py": "x = 1", "extra.py": "x = 2",
	}, nil)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "unexpected file") {
		t.Fatalf("extra file: %d %s", rec.Code, rec.Body.String())
	}

	// Wrong case is accepted as a set match.
	rec = uploadSubmission(t, handler, cookies, assignmentID, map[string]string{
		"MAIN.py": "print(1)", "Helper.py": "x = 1",
	}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("case-insensitive upload: %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Submission struct {
			IsLate     bool `json:"isLate"`
			CapPercent int  `json:"capPercent"`
			Files      []struct {
				Name string `json:"name"`
				Size int64  `json:"size"`
			} `json:"files"`
		} `json:"submission"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Submission.IsLate || resp.Submission.CapPercent != 100 {
		t.Fatalf("on-time upload marked late: %+v", resp.Submission)
	}
	if len(resp.Submission.Files) != 2 {
		t.Fatalf("expected 2 files, got %+v", resp.Submission.Files)
	}

	// Per-file limit.
	smallID := createTestAssignment(t, handler, map[string]any{
		"maxFileBytes": 10, "maxTotalBytes": 1000,
	})
	rec = uploadSubmission(t, handler, cookies, smallID, map[string]string{"main.py": "print('too long')"}, nil)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "exceeds") {
		t.Fatalf("per-file limit: %d %s", rec.Code, rec.Body.String())
	}

	// Total limit.
	tinyTotalID := createTestAssignment(t, handler, map[string]any{
		"expectedFilenames": "a.py\nb.py", "maxFileBytes": 100, "maxTotalBytes": 25,
	})
	rec = uploadSubmission(t, handler, cookies, tinyTotalID, map[string]string{
		"a.py": "xxxxxxxxxxxxxxxxxxxx", "b.py": "yyyyyyyyyyyyyyyyyyyy",
	}, nil)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "total limit") {
		t.Fatalf("total limit: %d %s", rec.Code, rec.Body.String())
	}

	// No cookie: 401.
	rec = uploadSubmission(t, handler, nil, assignmentID, map[string]string{
		"main.py": "print(1)", "helper.py": "x = 1",
	}, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated upload: got %d, want 401", rec.Code)
	}

	// A closed assignment rejects uploads.
	rec = adminRequest(t, handler, http.MethodPut,
		fmt.Sprintf("/api/admin/submissions/assignments/%d", assignmentID), map[string]any{
			"courseYearId": 1, "termId": 1, "title": "HW1", "language": "python",
			"expectedFilenames": "main.py\nhelper.py", "isOpen": false,
		})
	if rec.Code != http.StatusOK {
		t.Fatalf("close assignment: %d %s", rec.Code, rec.Body.String())
	}
	rec = uploadSubmission(t, handler, cookies, assignmentID, map[string]string{
		"main.py": "print(1)", "helper.py": "x = 1",
	}, nil)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "closed") {
		t.Fatalf("closed assignment: %d %s", rec.Code, rec.Body.String())
	}
}

func TestSubmissionLateFlow(t *testing.T) {
	server, handler, cookies := newSubmissionsTestServer(t)
	assignmentID := createTestAssignment(t, handler, map[string]any{
		"dueAt":          "2000-01-01T00:00:00Z",
		"lateCapPercent": 80,
	})

	// Without confirmLate the server rejects late uploads with a 409.
	rec := uploadSubmission(t, handler, cookies, assignmentID, map[string]string{"main.py": "print(1)"}, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("late upload without confirm: %d %s", rec.Code, rec.Body.String())
	}
	var lateResp struct {
		Late  bool   `json:"late"`
		DueAt string `json:"dueAt"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &lateResp); err != nil {
		t.Fatal(err)
	}
	if !lateResp.Late || lateResp.DueAt != "2000-01-01T00:00:00Z" {
		t.Fatalf("unexpected late response: %s", rec.Body.String())
	}

	// With confirmLate the upload is recorded as late with the penalty cap.
	rec = uploadSubmission(t, handler, cookies, assignmentID,
		map[string]string{"main.py": "print(1)"}, map[string]string{"confirmLate": "true"})
	if rec.Code != http.StatusOK {
		t.Fatalf("confirmed late upload: %d %s", rec.Code, rec.Body.String())
	}
	sub, err := server.store.LatestSubSubmission(assignmentID, 1)
	if err != nil || sub == nil {
		t.Fatalf("no submission stored: %v", err)
	}
	if !sub.IsLate || sub.CapPercent != 80 {
		t.Fatalf("late flags: isLate=%v capPercent=%d, want true/80", sub.IsLate, sub.CapPercent)
	}

	// A second upload bumps the attempt number.
	rec = uploadSubmission(t, handler, cookies, assignmentID,
		map[string]string{"main.py": "print(2)"}, map[string]string{"confirmLate": "true"})
	if rec.Code != http.StatusOK {
		t.Fatalf("second upload: %d %s", rec.Code, rec.Body.String())
	}
	sub, err = server.store.LatestSubSubmission(assignmentID, 1)
	if err != nil || sub == nil || sub.Attempt != 2 {
		t.Fatalf("attempt = %+v, want attempt 2", sub)
	}
	if _, err := os.Stat(submissionDir(server.config.SubmissionsDir, assignmentID, 1, 2)); err != nil {
		t.Fatalf("attempt 2 directory missing: %v", err)
	}
}

func TestSubmissionTestCooldown(t *testing.T) {
	server, handler, cookies := newSubmissionsTestServer(t)
	assignmentID := createTestAssignment(t, handler, nil)
	if rec := uploadTestFile(t, handler, assignmentID, "smoke", "public", "harness.py",
		"print('PASS: ok')\n"); rec.Code != http.StatusOK {
		t.Fatalf("upload test: %d %s", rec.Code, rec.Body.String())
	}

	// Testing before any upload is a 409.
	rec := studentPost(t, handler, cookies, fmt.Sprintf("/api/submissions/%d/test", assignmentID))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "no files uploaded yet") {
		t.Fatalf("test without upload: %d %s", rec.Code, rec.Body.String())
	}

	if rec := uploadSubmission(t, handler, cookies, assignmentID, map[string]string{"main.py": "print(1)"}, nil); rec.Code != http.StatusOK {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body.String())
	}

	// First test run is queued.
	rec = studentPost(t, handler, cookies, fmt.Sprintf("/api/submissions/%d/test", assignmentID))
	if rec.Code != http.StatusOK {
		t.Fatalf("first test: %d %s", rec.Code, rec.Body.String())
	}

	// An immediate second run hits the cooldown.
	rec = studentPost(t, handler, cookies, fmt.Sprintf("/api/submissions/%d/test", assignmentID))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("cooldown: got %d, want 429 (%s)", rec.Code, rec.Body.String())
	}
	var retryResp struct {
		RetryAfter int `json:"retryAfter"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &retryResp); err != nil {
		t.Fatal(err)
	}
	if retryResp.RetryAfter < 1 || retryResp.RetryAfter > 30 {
		t.Fatalf("retryAfter = %d, want 1..30", retryResp.RetryAfter)
	}

	// Another student is not affected by john's cooldown.
	jane := studentCookies(t, server, 2, "jane.doe")
	rec = studentPost(t, handler, jane, fmt.Sprintf("/api/submissions/%d/test", assignmentID))
	if rec.Code != http.StatusConflict { // jane has no upload yet: 409, not 429
		t.Fatalf("jane's first test: got %d, want 409 (%s)", rec.Code, rec.Body.String())
	}
}

// waitForRuns polls until the submission has n finished runs.
func waitForRuns(t *testing.T, store *Store, submissionID int64, n int) []SubTestRun {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		runs, err := store.RunsForSubmission(submissionID)
		if err != nil {
			t.Fatal(err)
		}
		finished := 0
		for _, r := range runs {
			if r.Status != "queued" && r.Status != "running" {
				finished++
			}
		}
		if len(runs) >= n && finished == len(runs) {
			return runs
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d finished runs", n)
	return nil
}

// TestAdminRunAggregatesAndSecretHiding exercises the full pipeline: student
// uploads, admin-triggered runs of public and secret tests, the admin
// aggregate view, and that student responses never mention secret tests.
func TestAdminRunAggregatesAndSecretHiding(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}
	server, handler, cookies := newSubmissionsTestServer(t)
	assignmentID := createTestAssignment(t, handler, nil)

	if rec := uploadTestFile(t, handler, assignmentID, "PublicCheck", "public", "harness_public.py", "content = open('main.py').read()\n"+
		"if 'hello' in content:\n    print('PASS: contains hello')\n"+
		"else:\n    print('FAIL: missing hello')\n"); rec.Code != http.StatusOK {
		t.Fatalf("upload public test: %d %s", rec.Code, rec.Body.String())
	}
	if rec := uploadTestFile(t, handler, assignmentID, "SecretCheck", "secret", "harness_secret.py",
		"print('PASS: hidden check')\n"); rec.Code != http.StatusOK {
		t.Fatalf("upload secret test: %d %s", rec.Code, rec.Body.String())
	}

	// John passes the public test; jane fails it. Both pass the secret test.
	if rec := uploadSubmission(t, handler, cookies, assignmentID,
		map[string]string{"main.py": "print('hello')\n"}, nil); rec.Code != http.StatusOK {
		t.Fatalf("john upload: %d %s", rec.Code, rec.Body.String())
	}
	jane := studentCookies(t, server, 2, "jane.doe")
	if rec := uploadSubmission(t, handler, jane, assignmentID,
		map[string]string{"main.py": "print('nope')\n"}, nil); rec.Code != http.StatusOK {
		t.Fatalf("jane upload: %d %s", rec.Code, rec.Body.String())
	}

	// Admin triggers all tests for everyone: 2 students x 2 tests.
	rec := adminRequest(t, handler, http.MethodPost,
		fmt.Sprintf("/api/admin/submissions/assignments/%d/run", assignmentID), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin run: %d %s", rec.Code, rec.Body.String())
	}
	var runResp struct {
		Queued int `json:"queued"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &runResp); err != nil {
		t.Fatal(err)
	}
	if runResp.Queued != 4 {
		t.Fatalf("queued = %d, want 4", runResp.Queued)
	}

	johnSub, err := server.store.LatestSubSubmission(assignmentID, 1)
	if err != nil || johnSub == nil {
		t.Fatal("john submission missing")
	}
	waitForRuns(t, server.store, johnSub.ID, 2)
	janeSub, err := server.store.LatestSubSubmission(assignmentID, 2)
	if err != nil || janeSub == nil {
		t.Fatal("jane submission missing")
	}
	waitForRuns(t, server.store, janeSub.ID, 2)

	// The admin aggregate shows public and secret sums per student.
	rec = adminRequest(t, handler, http.MethodGet,
		fmt.Sprintf("/api/admin/submissions/assignments/%d/submissions", assignmentID), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin submissions: %d %s", rec.Code, rec.Body.String())
	}
	var agg struct {
		Students []struct {
			StudentID        int `json:"studentId"`
			LatestSubmission *struct {
				ID int64 `json:"id"`
			} `json:"latestSubmission"`
			PublicPassed int  `json:"publicPassed"`
			PublicFailed int  `json:"publicFailed"`
			SecretPassed int  `json:"secretPassed"`
			SecretFailed int  `json:"secretFailed"`
			Untested     bool `json:"untested"`
		} `json:"students"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &agg); err != nil {
		t.Fatal(err)
	}
	if len(agg.Students) != 2 {
		t.Fatalf("expected 2 students, got %s", rec.Body.String())
	}
	byID := map[int]int{}
	for i, st := range agg.Students {
		byID[st.StudentID] = i
	}
	john := agg.Students[byID[1]]
	if john.PublicPassed != 1 || john.PublicFailed != 0 || john.SecretPassed != 1 || john.SecretFailed != 0 || john.Untested {
		t.Fatalf("john aggregate: %+v", john)
	}
	if john.LatestSubmission == nil || john.LatestSubmission.ID != johnSub.ID {
		t.Fatalf("john latest submission: %+v", john.LatestSubmission)
	}
	janeRow := agg.Students[byID[2]]
	if janeRow.PublicPassed != 0 || janeRow.PublicFailed != 1 || janeRow.SecretPassed != 1 || janeRow.Untested {
		t.Fatalf("jane aggregate: %+v", janeRow)
	}

	// The admin submission detail includes secret output.
	rec = adminRequest(t, handler, http.MethodGet,
		fmt.Sprintf("/api/admin/submissions/submissions/%d", johnSub.ID), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin submission detail: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "SecretCheck") {
		t.Fatalf("admin detail must include secret runs: %s", rec.Body.String())
	}

	// The student detail must never leak the secret test.
	rec = studentGet(t, handler, cookies, fmt.Sprintf("/api/submissions/%d", assignmentID))
	if rec.Code != http.StatusOK {
		t.Fatalf("student detail: %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "SecretCheck") || strings.Contains(rec.Body.String(), "secret") {
		t.Fatalf("student detail leaks secret test: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "PublicCheck") {
		t.Fatalf("student detail missing public runs: %s", rec.Body.String())
	}
	// Public run output is included so students can see why checks fail;
	// secret run output must not leak.
	if !strings.Contains(rec.Body.String(), "PASS: contains hello") {
		t.Fatalf("student detail missing public run output: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "hidden check") {
		t.Fatalf("student detail leaks secret run output: %s", rec.Body.String())
	}

	// The admin file download serves the uploaded source.
	rec = adminRequest(t, handler, http.MethodGet,
		fmt.Sprintf("/api/admin/submissions/submissions/%d/files/main.py", johnSub.ID), nil)
	if rec.Code != http.StatusOK || rec.Body.String() != "print('hello')\n" {
		t.Fatalf("admin file download: %d %q", rec.Code, rec.Body.String())
	}

	// Queue endpoint reports depth and running state.
	rec = adminRequest(t, handler, http.MethodGet, "/api/admin/submissions/queue", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"depth"`) {
		t.Fatalf("queue endpoint: %d %s", rec.Code, rec.Body.String())
	}

	// Deleting a test drops its past runs from the aggregates and the
	// student view.
	tests, err := server.store.ListSubTests(assignmentID)
	if err != nil || len(tests) != 2 {
		t.Fatalf("list tests: %v %+v", err, tests)
	}
	var publicTestID int64
	for _, test := range tests {
		if test.Visibility == "public" {
			publicTestID = test.ID
		}
	}
	rec = adminRequest(t, handler, http.MethodDelete,
		fmt.Sprintf("/api/admin/submissions/assignments/%d/tests/%d", assignmentID, publicTestID), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete public test: %d %s", rec.Code, rec.Body.String())
	}

	rec = adminRequest(t, handler, http.MethodGet,
		fmt.Sprintf("/api/admin/submissions/assignments/%d/submissions", assignmentID), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin submissions after delete: %d %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &agg); err != nil {
		t.Fatal(err)
	}
	john = agg.Students[byID[1]]
	if john.PublicPassed != 0 || john.PublicFailed != 0 || john.SecretPassed != 1 || john.SecretFailed != 0 {
		t.Fatalf("john aggregate after delete: %+v", john)
	}
	janeRow = agg.Students[byID[2]]
	if janeRow.PublicPassed != 0 || janeRow.PublicFailed != 0 {
		t.Fatalf("jane aggregate after delete: %+v", janeRow)
	}

	rec = studentGet(t, handler, cookies, fmt.Sprintf("/api/submissions/%d", assignmentID))
	if rec.Code != http.StatusOK {
		t.Fatalf("student detail after delete: %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "PublicCheck") {
		t.Fatalf("student detail still lists deleted test: %s", rec.Body.String())
	}

	// Admin endpoints require the teacher token.
	req := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/admin/submissions/assignments/%d/submissions", assignmentID), nil)
	recNoAuth := httptest.NewRecorder()
	handler.ServeHTTP(recNoAuth, req)
	if recNoAuth.Code != http.StatusUnauthorized {
		t.Fatalf("admin endpoint without token: got %d, want 401", recNoAuth.Code)
	}
}

// TestSampleSolutionAndInlineTestEdit covers overwriting a harness via PUT
// and the sample solution area: file CRUD plus running tests against the
// sample files.
func TestSampleSolutionAndInlineTestEdit(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available")
	}
	_, handler, _ := newSubmissionsTestServer(t)
	assignmentID := createTestAssignment(t, handler, nil)

	if rec := uploadTestFile(t, handler, assignmentID, "Check", "public", "harness.py",
		"content = open('main.py').read()\n"+
			"if 'hello' in content:\n    print('PASS: has hello')\n"+
			"else:\n    print('FAIL: missing hello')\n"); rec.Code != http.StatusOK {
		t.Fatalf("upload test: %d %s", rec.Code, rec.Body.String())
	}
	rec := adminRequest(t, handler, http.MethodGet,
		fmt.Sprintf("/api/admin/submissions/assignments/%d", assignmentID), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get assignment: %d %s", rec.Code, rec.Body.String())
	}
	var detail struct {
		Tests []struct {
			ID int64 `json:"id"`
		} `json:"tests"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil || len(detail.Tests) != 1 {
		t.Fatalf("tests: %v %s", err, rec.Body.String())
	}
	testID := detail.Tests[0].ID

	// Inline edit overwrites the harness content.
	updated := "print('FAIL: never passes')\n"
	rec = adminRawRequest(t, handler, http.MethodPut,
		fmt.Sprintf("/api/admin/submissions/assignments/%d/tests/%d", assignmentID, testID), updated)
	if rec.Code != http.StatusOK {
		t.Fatalf("edit test: %d %s", rec.Code, rec.Body.String())
	}
	rec = adminRawRequest(t, handler, http.MethodGet,
		fmt.Sprintf("/api/admin/submissions/assignments/%d/tests/%d", assignmentID, testID), "")
	if rec.Code != http.StatusOK || rec.Body.String() != updated {
		t.Fatalf("get edited test: %d %q", rec.Code, rec.Body.String())
	}

	// The sample area starts empty and cannot run.
	rec = adminRequest(t, handler, http.MethodPost,
		fmt.Sprintf("/api/admin/submissions/assignments/%d/sample/run", assignmentID), nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("run without sample files: got %d, want 400", rec.Code)
	}

	// Sample file CRUD.
	rec = adminRawRequest(t, handler, http.MethodPut,
		fmt.Sprintf("/api/admin/submissions/assignments/%d/sample/files/main.py", assignmentID),
		"print('hello')\n")
	if rec.Code != http.StatusOK {
		t.Fatalf("put sample file: %d %s", rec.Code, rec.Body.String())
	}
	rec = adminRequest(t, handler, http.MethodGet,
		fmt.Sprintf("/api/admin/submissions/assignments/%d/sample", assignmentID), nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "main.py") {
		t.Fatalf("list sample: %d %s", rec.Code, rec.Body.String())
	}
	rec = adminRawRequest(t, handler, http.MethodGet,
		fmt.Sprintf("/api/admin/submissions/assignments/%d/sample/files/main.py", assignmentID), "")
	if rec.Code != http.StatusOK || rec.Body.String() != "print('hello')\n" {
		t.Fatalf("get sample file: %d %q", rec.Code, rec.Body.String())
	}

	// Running tests on the sample uses the edited harness: 0 passed, 1 failed.
	rec = adminRequest(t, handler, http.MethodPost,
		fmt.Sprintf("/api/admin/submissions/assignments/%d/sample/run", assignmentID), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("sample run: %d %s", rec.Code, rec.Body.String())
	}
	var runResp struct {
		Results []struct {
			TestName string `json:"testName"`
			Status   string `json:"status"`
			Passed   int    `json:"passed"`
			Failed   int    `json:"failed"`
			Output   string `json:"output"`
		} `json:"results"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &runResp); err != nil || len(runResp.Results) != 1 {
		t.Fatalf("sample run results: %v %s", err, rec.Body.String())
	}
	got := runResp.Results[0]
	if got.Status != "done" || got.Passed != 0 || got.Failed != 1 || !strings.Contains(got.Output, "FAIL: never passes") {
		t.Fatalf("sample run result: %+v", got)
	}

	// Deleting the last sample file makes runs fail again.
	rec = adminRequest(t, handler, http.MethodDelete,
		fmt.Sprintf("/api/admin/submissions/assignments/%d/sample/files/main.py", assignmentID), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete sample file: %d %s", rec.Code, rec.Body.String())
	}
	rec = adminRequest(t, handler, http.MethodPost,
		fmt.Sprintf("/api/admin/submissions/assignments/%d/sample/run", assignmentID), nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("run after deleting sample files: got %d, want 400", rec.Code)
	}
}

// TestBasecodeFiles covers the base code file CRUD used for plagiarism runs.
func TestBasecodeFiles(t *testing.T) {
	server, handler, _ := newSubmissionsTestServer(t)
	assignmentID := createTestAssignment(t, handler, nil)

	// Starts empty.
	rec := adminRequest(t, handler, http.MethodGet,
		fmt.Sprintf("/api/admin/submissions/assignments/%d/basecode", assignmentID), nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"files":[]`) {
		t.Fatalf("list basecode: %d %s", rec.Code, rec.Body.String())
	}

	// Write, read back, list.
	rec = adminRawRequest(t, handler, http.MethodPut,
		fmt.Sprintf("/api/admin/submissions/assignments/%d/basecode/files/Template.java", assignmentID),
		"public class Template {}\n")
	if rec.Code != http.StatusOK {
		t.Fatalf("put basecode file: %d %s", rec.Code, rec.Body.String())
	}
	rec = adminRawRequest(t, handler, http.MethodGet,
		fmt.Sprintf("/api/admin/submissions/assignments/%d/basecode/files/Template.java", assignmentID), "")
	if rec.Code != http.StatusOK || rec.Body.String() != "public class Template {}\n" {
		t.Fatalf("get basecode file: %d %q", rec.Code, rec.Body.String())
	}
	rec = adminRequest(t, handler, http.MethodGet,
		fmt.Sprintf("/api/admin/submissions/assignments/%d/basecode", assignmentID), nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Template.java") {
		t.Fatalf("list after put: %d %s", rec.Code, rec.Body.String())
	}

	// The basecode dir is picked up for the plagiarism run.
	if entries, err := os.ReadDir(basecodeDir(server.config.SubmissionsDir, assignmentID)); err != nil || len(entries) != 1 {
		t.Fatalf("basecode dir: %v %v", entries, err)
	}

	// Delete.
	rec = adminRequest(t, handler, http.MethodDelete,
		fmt.Sprintf("/api/admin/submissions/assignments/%d/basecode/files/Template.java", assignmentID), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete basecode file: %d %s", rec.Code, rec.Body.String())
	}
	rec = adminRawRequest(t, handler, http.MethodGet,
		fmt.Sprintf("/api/admin/submissions/assignments/%d/basecode/files/Template.java", assignmentID), "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get after delete: got %d, want 404", rec.Code)
	}
}

// TestAdminAssignmentValidation covers create-time validation and defaults.
func TestAdminAssignmentValidation(t *testing.T) {
	_, handler, _ := newSubmissionsTestServer(t)

	base := map[string]any{
		"courseYearId": 1, "termId": 1, "title": "HW1",
		"language": "python", "expectedFilenames": "main.py",
	}
	bad := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"bad language", func(b map[string]any) { b["language"] = "rust" }},
		{"unpublished course", func(b map[string]any) { b["courseYearId"] = 99 }},
		{"wrong extension", func(b map[string]any) { b["expectedFilenames"] = "Main.java" }},
		{"missing title", func(b map[string]any) { b["title"] = " " }},
		{"no filenames", func(b map[string]any) { b["expectedFilenames"] = "" }},
		{"bad due date", func(b map[string]any) { b["dueAt"] = "next friday" }},
		{"bad cap", func(b map[string]any) { b["lateCapPercent"] = 150 }},
	}
	for _, tt := range bad {
		t.Run(tt.name, func(t *testing.T) {
			body := map[string]any{}
			for k, v := range base {
				body[k] = v
			}
			tt.mutate(body)
			rec := adminRequest(t, handler, http.MethodPost, "/api/admin/submissions/assignments", body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("got %d, want 400 (%s)", rec.Code, rec.Body.String())
			}
		})
	}

	// A minimal create resolves the documented defaults.
	rec := adminRequest(t, handler, http.MethodPost, "/api/admin/submissions/assignments", base)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var createResp struct {
		Assignment struct {
			ID             int64   `json:"id"`
			MaxFileBytes   int64   `json:"maxFileBytes"`
			MaxTotalBytes  int64   `json:"maxTotalBytes"`
			LateCapPercent int     `json:"lateCapPercent"`
			IsOpen         bool    `json:"isOpen"`
			DueAt          *string `json:"dueAt"`
		} `json:"assignment"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &createResp); err != nil {
		t.Fatal(err)
	}
	a := createResp.Assignment
	if a.MaxFileBytes != 262144 || a.MaxTotalBytes != 1048576 || a.LateCapPercent != 90 || !a.IsOpen || a.DueAt != nil {
		t.Fatalf("defaults: %+v", a)
	}

	// The list endpoint returns it, including with the course filter.
	rec = adminRequest(t, handler, http.MethodGet, "/api/admin/submissions/assignments?courseYearId=1&termId=1", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"id":`) {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	rec = adminRequest(t, handler, http.MethodGet, "/api/admin/submissions/assignments?courseYearId=9&termId=9", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"assignments":[]`) {
		t.Fatalf("filtered list: %d %s", rec.Code, rec.Body.String())
	}

	// The detail endpoint includes the tests list.
	rec = adminRequest(t, handler, http.MethodGet,
		fmt.Sprintf("/api/admin/submissions/assignments/%d", a.ID), nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"tests":[]`) {
		t.Fatalf("detail: %d %s", rec.Code, rec.Body.String())
	}

	// Delete removes it.
	rec = adminRequest(t, handler, http.MethodDelete,
		fmt.Sprintf("/api/admin/submissions/assignments/%d", a.ID), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
	rec = adminRequest(t, handler, http.MethodGet,
		fmt.Sprintf("/api/admin/submissions/assignments/%d", a.ID), nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get after delete: got %d, want 404", rec.Code)
	}
}

// TestAdminPlagiarismFlow queues a plagiarism run; without the JPlag jar the
// run finishes as unavailable, which is the graceful-degradation path.
func TestAdminPlagiarismFlow(t *testing.T) {
	server, handler, cookies := newSubmissionsTestServer(t)
	assignmentID := createTestAssignment(t, handler, nil)

	// Fewer than two submissions: rejected up front.
	rec := adminRequest(t, handler, http.MethodPost,
		fmt.Sprintf("/api/admin/submissions/assignments/%d/plagiarism", assignmentID), nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("plagiarism without submissions: %d %s", rec.Code, rec.Body.String())
	}

	if rec := uploadSubmission(t, handler, cookies, assignmentID, map[string]string{"main.py": "print(1)"}, nil); rec.Code != http.StatusOK {
		t.Fatalf("john upload: %d", rec.Code)
	}
	jane := studentCookies(t, server, 2, "jane.doe")
	if rec := uploadSubmission(t, handler, jane, assignmentID, map[string]string{"main.py": "print(2)"}, nil); rec.Code != http.StatusOK {
		t.Fatalf("jane upload: %d", rec.Code)
	}

	rec = adminRequest(t, handler, http.MethodPost,
		fmt.Sprintf("/api/admin/submissions/assignments/%d/plagiarism", assignmentID), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("plagiarism queue: %d %s", rec.Code, rec.Body.String())
	}
	var queueResp struct {
		RunID int64 `json:"runId"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &queueResp); err != nil {
		t.Fatal(err)
	}
	if queueResp.RunID == 0 {
		t.Fatal("expected a run id")
	}

	// The jar does not exist, so the run finishes as unavailable.
	deadline := time.Now().Add(10 * time.Second)
	var run *SubPlagRun
	for time.Now().Before(deadline) {
		var err error
		run, err = server.store.GetSubPlagRun(queueResp.RunID)
		if err != nil {
			t.Fatal(err)
		}
		if run.Status != "queued" && run.Status != "running" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if run == nil || run.Status != "unavailable" {
		t.Fatalf("run status = %+v, want unavailable", run)
	}

	rec = adminRequest(t, handler, http.MethodGet,
		fmt.Sprintf("/api/admin/submissions/assignments/%d/plagiarism", assignmentID), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("plagiarism get: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"unavailable"`) || !strings.Contains(rec.Body.String(), `"pairs":[]`) {
		t.Fatalf("plagiarism get body: %s", rec.Body.String())
	}

	// No finished report yet.
	rec = adminRequest(t, handler, http.MethodGet,
		fmt.Sprintf("/api/admin/submissions/assignments/%d/plagiarism/report", assignmentID), nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("report before a done run: got %d, want 404", rec.Code)
	}

	// A done run serves the report zip. A run recorded with a legacy .jplag
	// path falls back to the .zip sibling on disk.
	legacyPath := filepath.Join(server.config.SubmissionsDir, "plag", fmt.Sprintf("%d.jplag", queueResp.RunID))
	reportPath := strings.TrimSuffix(legacyPath, ".jplag") + ".zip"
	if err := os.MkdirAll(filepath.Dir(reportPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(reportPath, []byte("fake-jplag-zip"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := server.store.SetSubPlagRunStatus(queueResp.RunID, "done", legacyPath, ""); err != nil {
		t.Fatal(err)
	}
	rec = adminRequest(t, handler, http.MethodGet,
		fmt.Sprintf("/api/admin/submissions/assignments/%d/plagiarism/report", assignmentID), nil)
	if rec.Code != http.StatusOK || rec.Body.String() != "fake-jplag-zip" {
		t.Fatalf("report download: %d %q", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Header().Get("Content-Disposition"), ".zip") {
		t.Fatalf("report content disposition: %q", rec.Header().Get("Content-Disposition"))
	}

	// A JPlag 6 run serves the .jplag file at the stored path directly.
	if err := os.WriteFile(legacyPath, []byte("new-jplag-report"), 0644); err != nil {
		t.Fatal(err)
	}
	rec = adminRequest(t, handler, http.MethodGet,
		fmt.Sprintf("/api/admin/submissions/assignments/%d/plagiarism/report", assignmentID), nil)
	if rec.Code != http.StatusOK || rec.Body.String() != "new-jplag-report" {
		t.Fatalf("jplag report download: %d %q", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Header().Get("Content-Disposition"), ".jplag") {
		t.Fatalf("jplag report content disposition: %q", rec.Header().Get("Content-Disposition"))
	}
}

// TestTextAndFilesAssignments covers the non-code assignment types: text
// assignments reject test uploads, and files assignments additionally reject
// plagiarism runs. Both accept their respective filename sets.
func TestTextAndFilesAssignments(t *testing.T) {
	_, handler, cookies := newSubmissionsTestServer(t)

	create := func(language, filenames string) int64 {
		rec := adminRequest(t, handler, http.MethodPost, "/api/admin/submissions/assignments", map[string]any{
			"courseYearId": 1, "termId": 1, "title": "Essay " + language,
			"language": language, "expectedFilenames": filenames,
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("create %s assignment: %d %s", language, rec.Code, rec.Body.String())
		}
		var resp struct {
			Assignment struct {
				ID int64 `json:"id"`
			} `json:"assignment"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		return resp.Assignment.ID
	}

	textID := create("text", "essay.md")
	filesID := create("files", "presentation.xlsx\ndemo.mp4\ndoc.docx")

	// Test uploads are rejected for text and files assignments.
	for _, id := range []int64{textID, filesID} {
		rec := uploadTestFile(t, handler, id, "probe", "public", "probe.py", "print('PASS: x')")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("test upload on non-code assignment %d: %d %s", id, rec.Code, rec.Body.String())
		}
	}

	// Plagiarism is rejected for files assignments but allowed for text.
	rec := adminRequest(t, handler, http.MethodPost,
		fmt.Sprintf("/api/admin/submissions/assignments/%d/plagiarism", filesID), nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("files plagiarism: %d %s", rec.Code, rec.Body.String())
	}
	rec = adminRequest(t, handler, http.MethodPost,
		fmt.Sprintf("/api/admin/submissions/assignments/%d/plagiarism", textID), nil)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "at least two") {
		t.Fatalf("text plagiarism without submissions: %d %s", rec.Code, rec.Body.String())
	}

	// Uploads accept the declared filename sets (binary content included).
	if rec := uploadSubmission(t, handler, cookies, textID, map[string]string{"essay.md": "# Essay\n"}, nil); rec.Code != http.StatusOK {
		t.Fatalf("text upload: %d %s", rec.Code, rec.Body.String())
	}
	if rec := uploadSubmission(t, handler, cookies, filesID, map[string]string{
		"presentation.xlsx": "PK\x03\x04fake", "demo.mp4": "\x00\x00\x00\x18ftyp", "doc.docx": "PK\x03\x04fake",
	}, nil); rec.Code != http.StatusOK {
		t.Fatalf("files upload: %d %s", rec.Code, rec.Body.String())
	}
	// A files upload missing one declared name is still rejected.
	if rec := uploadSubmission(t, handler, cookies, filesID, map[string]string{
		"presentation.xlsx": "x", "demo.mp4": "y",
	}, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("files upload missing doc: %d %s", rec.Code, rec.Body.String())
	}

	// Student test endpoint has nothing to run on a text assignment.
	rec = studentPost(t, handler, cookies, fmt.Sprintf("/api/submissions/%d/test", textID))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("test on text assignment: %d %s", rec.Code, rec.Body.String())
	}
}

// TestSlotUploadFlow covers per-file uploads: files stage one at a time,
// nothing is recorded until the student explicitly submits the complete set,
// and the late-confirm flow applies at submit time.
func TestSlotUploadFlow(t *testing.T) {
	server, handler, cookies := newSubmissionsTestServer(t)
	assignmentID := createTestAssignment(t, handler, map[string]any{"expectedFilenames": "main.py\nhelper.py"})

	// Unexpected slot name is rejected.
	if rec := uploadSlot(t, handler, cookies, assignmentID, "evil.py", "evil.py", "x", nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("unexpected slot: %d %s", rec.Code, rec.Body.String())
	}

	// A picked file whose name does not match the slot is rejected.
	if rec := uploadSlot(t, handler, cookies, assignmentID, "main.py", "anything.py", "x", nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("mismatched file name: %d %s", rec.Code, rec.Body.String())
	}

	// First file stages a draft; no submission yet.
	rec := uploadSlot(t, handler, cookies, assignmentID, "main.py", "MAIN.PY", "print('main')", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("first slot upload: %d %s", rec.Code, rec.Body.String())
	}
	var draftResp struct {
		Draft struct {
			Files   []map[string]any `json:"files"`
			Missing []string         `json:"missing"`
		} `json:"draft"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &draftResp); err != nil {
		t.Fatal(err)
	}
	if len(draftResp.Draft.Files) != 1 || len(draftResp.Draft.Missing) != 1 || draftResp.Draft.Missing[0] != "helper.py" {
		t.Fatalf("draft after first upload: %+v", draftResp.Draft)
	}
	if latest, _ := server.store.LatestSubSubmission(assignmentID, 1); latest != nil {
		t.Fatalf("no submission expected yet, got %+v", latest)
	}

	// The draft is visible in the list endpoint.
	rec = studentGet(t, handler, cookies, "/api/submissions")
	if !strings.Contains(rec.Body.String(), `"draftFiles":[{"key":"main.py","name":"main.py"`) {
		t.Fatalf("list should show the staged file: %s", rec.Body.String())
	}

	// The staged file can be downloaded back.
	rec = studentGet(t, handler, cookies, fmt.Sprintf("/api/submissions/%d/slot/main.py", assignmentID))
	if rec.Code != http.StatusOK || rec.Body.String() != "print('main')" {
		t.Fatalf("download staged file: %d %q", rec.Code, rec.Body.String())
	}

	// Submitting with an incomplete draft fails and keeps the draft.
	rec = studentPost(t, handler, cookies, fmt.Sprintf("/api/submissions/%d/submit", assignmentID))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"missing":["helper.py"]`) {
		t.Fatalf("incomplete submit: %d %s", rec.Code, rec.Body.String())
	}

	// Removing the staged file makes the draft empty again.
	req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/submissions/%d/slot/main.py", assignmentID), nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	delRec := httptest.NewRecorder()
	handler.ServeHTTP(delRec, req)
	if delRec.Code != http.StatusOK || !strings.Contains(delRec.Body.String(), `"missing":["main.py","helper.py"]`) {
		t.Fatalf("delete slot: %d %s", delRec.Code, delRec.Body.String())
	}

	// Stage both files, then submit → attempt 1.
	uploadSlot(t, handler, cookies, assignmentID, "main.py", "main.py", "print('main')", nil)
	uploadSlot(t, handler, cookies, assignmentID, "helper.py", "helper.py", "X = 1", nil)
	rec = studentPost(t, handler, cookies, fmt.Sprintf("/api/submissions/%d/submit", assignmentID))
	var subResp struct {
		Submission struct {
			ID      int64 `json:"id"`
			Attempt int   `json:"attempt"`
		} `json:"submission"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &subResp); err != nil || subResp.Submission.ID == 0 {
		t.Fatalf("submit: %d %s", rec.Code, rec.Body.String())
	}

	// Submitting with nothing staged is rejected.
	if rec := studentPost(t, handler, cookies, fmt.Sprintf("/api/submissions/%d/submit", assignmentID)); rec.Code != http.StatusConflict {
		t.Fatalf("submit with empty draft: %d %s", rec.Code, rec.Body.String())
	}

	// Staging one changed file is enough when the rest were submitted before:
	// submit merges the staged file over the previous attempt.
	uploadSlot(t, handler, cookies, assignmentID, "main.py", "main.py", "print('v2')", nil)
	rec = studentPost(t, handler, cookies, fmt.Sprintf("/api/submissions/%d/submit", assignmentID))
	subResp.Submission = struct {
		ID      int64 `json:"id"`
		Attempt int   `json:"attempt"`
	}{}
	if err := json.Unmarshal(rec.Body.Bytes(), &subResp); err != nil || subResp.Submission.Attempt != 2 {
		t.Fatalf("merged submit should record attempt 2: %d %s", rec.Code, rec.Body.String())
	}
	dir := submissionDir(server.config.SubmissionsDir, assignmentID, 1, 2)
	if data, err := os.ReadFile(filepath.Join(dir, "main.py")); err != nil || string(data) != "print('v2')" {
		t.Fatalf("attempt 2 main.py: %v %q", err, data)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "helper.py")); err != nil || string(data) != "X = 1" {
		t.Fatalf("attempt 2 carried-over helper.py: %v %q", err, data)
	}

	// Late flow: submitting past the due date needs confirmLate.
	lateID := createTestAssignment(t, handler, map[string]any{
		"expectedFilenames": "main.py", "dueAt": "2020-01-01T00:00:00Z",
	})
	uploadSlot(t, handler, cookies, lateID, "main.py", "main.py", "print('late')", nil)
	rec = studentPost(t, handler, cookies, fmt.Sprintf("/api/submissions/%d/submit", lateID))
	if rec.Code != http.StatusConflict {
		t.Fatalf("late submit without confirm: %d %s", rec.Code, rec.Body.String())
	}
	rec = studentPost(t, handler, cookies, fmt.Sprintf("/api/submissions/%d/submit?confirmLate=true", lateID))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"isLate":true`) || !strings.Contains(rec.Body.String(), `"capPercent":90`) {
		t.Fatalf("confirmed late: %d %s", rec.Code, rec.Body.String())
	}
}

// TestSlotAlternativeExtensions covers slots with alternative extensions:
// report.docx/pdf accepts either file, stored under the submitted name.
func TestSlotAlternativeExtensions(t *testing.T) {
	server, handler, cookies := newSubmissionsTestServer(t)
	assignmentID := createTestAssignment(t, handler, map[string]any{
		"language": "files", "expectedFilenames": "report.docx/pdf\nnotes.txt",
	})

	// The slot is keyed by the first alternative; the picked file may use any
	// of the slot's extensions.
	rec := uploadSlot(t, handler, cookies, assignmentID, "report.docx", "report.pdf", "pdf-bytes", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"name":"report.pdf"`) {
		t.Fatalf("pdf into docx/pdf slot: %d %s", rec.Code, rec.Body.String())
	}

	// A wrong stem or unlisted extension is rejected.
	if rec := uploadSlot(t, handler, cookies, assignmentID, "report.docx", "other.pdf", "x", nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("wrong stem: %d %s", rec.Code, rec.Body.String())
	}
	if rec := uploadSlot(t, handler, cookies, assignmentID, "report.docx", "report.txt", "x", nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("unlisted extension: %d %s", rec.Code, rec.Body.String())
	}

	// Switching to the docx alternative replaces the staged pdf.
	rec = uploadSlot(t, handler, cookies, assignmentID, "report.docx", "report.docx", "docx-bytes", nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"name":"report.docx"`) {
		t.Fatalf("switch alternative: %d %s", rec.Code, rec.Body.String())
	}
	draftDirPath := draftDir(server.config.SubmissionsDir, assignmentID, 1)
	if entries, _ := os.ReadDir(draftDirPath); len(entries) != 1 || entries[0].Name() != "report.docx" {
		t.Fatalf("draft should hold only report.docx: %v", entries)
	}

	// Complete and submit; the attempt records the actual file name.
	uploadSlot(t, handler, cookies, assignmentID, "notes.txt", "notes.txt", "notes", nil)
	rec = studentPost(t, handler, cookies, fmt.Sprintf("/api/submissions/%d/submit", assignmentID))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"name":"report.docx"`) {
		t.Fatalf("submit: %d %s", rec.Code, rec.Body.String())
	}

	// The staged file downloads under its actual name from the slot key.
	uploadSlot(t, handler, cookies, assignmentID, "report.docx", "report.pdf", "pdf-v2", nil)
	rec = studentGet(t, handler, cookies, fmt.Sprintf("/api/submissions/%d/slot/report.docx", assignmentID))
	if rec.Code != http.StatusOK || rec.Body.String() != "pdf-v2" {
		t.Fatalf("download staged alternative: %d %q", rec.Code, rec.Body.String())
	}
}

// TestSubStoreCRUD exercises the submissions store round trip, including the
// latest-per-test semantics and the delete cascade.
func TestSubStoreCRUD(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "portal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	due := "2030-01-01T00:00:00Z"
	a := &SubAssignment{
		CourseYearID: 1, TermID: 1, Title: "HW", Language: "java",
		ExpectedFilenames: []string{"Main.java"}, MaxFileBytes: 1024,
		MaxTotalBytes: 4096, LateCapPercent: 90, IsOpen: true, DueAt: &due,
	}
	id, err := store.CreateSubAssignment(a)
	if err != nil {
		t.Fatal(err)
	}
	a.ID = id

	got, err := store.GetSubAssignment(id)
	if err != nil || got == nil {
		t.Fatalf("get: %v", err)
	}
	if got.Title != "HW" || got.Language != "java" || got.DueAt == nil || *got.DueAt != due ||
		len(got.ExpectedFilenames) != 1 || got.ExpectedFilenames[0] != "Main.java" ||
		got.MaxFileBytes != 1024 || !got.IsOpen {
		t.Fatalf("round trip mismatch: %+v", got)
	}

	got.Title = "HW2"
	got.IsOpen = false
	if err := store.UpdateSubAssignment(got); err != nil {
		t.Fatal(err)
	}
	got, err = store.GetSubAssignment(id)
	if err != nil || got.Title != "HW2" || got.IsOpen {
		t.Fatalf("update mismatch: %+v", got)
	}

	if all, err := store.ListAllSubAssignments(); err != nil || len(all) != 1 {
		t.Fatalf("list all: %v %v", all, err)
	}
	if byCourse, err := store.ListSubAssignmentsForCourse(1, 1); err != nil || len(byCourse) != 1 {
		t.Fatalf("list by course: %v %v", byCourse, err)
	}
	if none, err := store.ListSubAssignmentsForCourse(2, 2); err != nil || len(none) != 0 {
		t.Fatalf("list empty: %v %v", none, err)
	}

	// Tests.
	testID, err := store.CreateSubTest(&SubTest{AssignmentID: id, Name: "t1", Visibility: "public", StoredName: "T1.java"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateSubTest(&SubTest{AssignmentID: id, Name: "t2", Visibility: "secret", StoredName: "T2.java"}); err != nil {
		t.Fatal(err)
	}
	tests, err := store.ListSubTests(id)
	if err != nil || len(tests) != 2 {
		t.Fatalf("list tests: %v %v", tests, err)
	}

	// Submissions with incrementing attempts and files.
	sub := &SubSubmission{AssignmentID: id, StudentPK: 7, SubmittedAt: "2026-09-07T00:00:00Z", CapPercent: 100}
	if _, err := store.CreateSubSubmission(sub, []SubFile{{Filename: "Main.java", ByteSize: 42}}); err != nil {
		t.Fatal(err)
	}
	if sub.Attempt != 1 {
		t.Fatalf("first attempt = %d", sub.Attempt)
	}
	sub2 := &SubSubmission{AssignmentID: id, StudentPK: 7, SubmittedAt: "2026-09-08T00:00:00Z", IsLate: true, CapPercent: 90}
	if _, err := store.CreateSubSubmission(sub2, []SubFile{{Filename: "Main.java", ByteSize: 43}}); err != nil {
		t.Fatal(err)
	}
	if sub2.Attempt != 2 {
		t.Fatalf("second attempt = %d", sub2.Attempt)
	}
	latest, err := store.LatestSubSubmission(id, 7)
	if err != nil || latest == nil || latest.ID != sub2.ID || !latest.IsLate || latest.CapPercent != 90 {
		t.Fatalf("latest: %+v %v", latest, err)
	}
	if subs, err := store.ListSubSubmissions(id, 7); err != nil || len(subs) != 2 {
		t.Fatalf("list submissions: %v %v", subs, err)
	}
	files, err := store.ListSubFiles(sub.ID)
	if err != nil || len(files) != 1 || files[0].Filename != "Main.java" || files[0].ByteSize != 42 {
		t.Fatalf("files: %+v %v", files, err)
	}
	if byStudent, err := store.LatestSubmissionsByAssignment(id); err != nil || len(byStudent) != 1 || byStudent[7].ID != sub2.ID {
		t.Fatalf("latest by assignment: %v %v", byStudent, err)
	}

	// Runs: two runs for one test; only the latest counts.
	runID, err := store.CreateSubTestRun(&SubTestRun{SubmissionID: sub2.ID, TestID: testID, Visibility: "public", TriggeredBy: "student"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.StartTestRun(runID); err != nil {
		t.Fatal(err)
	}
	if err := store.FinishTestRun(runID, "done", 2, 1, "PASS: a\nFAIL: b"); err != nil {
		t.Fatal(err)
	}
	run, err := store.GetSubTestRun(runID)
	if err != nil || run.Status != "done" || run.Passed != 2 || run.Failed != 1 ||
		run.StartedAt == nil || run.FinishedAt == nil || run.TestName != "t1" {
		t.Fatalf("run round trip: %+v %v", run, err)
	}
	runID2, err := store.CreateSubTestRun(&SubTestRun{SubmissionID: sub2.ID, TestID: testID, Visibility: "public", TriggeredBy: "student"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.FinishTestRun(runID2, "done", 3, 0, "PASS: a\nPASS: b\nPASS: c"); err != nil {
		t.Fatal(err)
	}
	latestRuns, err := store.LatestRunsForSubmission(sub2.ID, "public")
	if err != nil || len(latestRuns) != 1 || latestRuns[0].ID != runID2 || latestRuns[0].Passed != 3 {
		t.Fatalf("latest runs: %+v %v", latestRuns, err)
	}

	// Student run state: nothing queued/running after both finished, but the
	// enqueue timestamp is visible for the cooldown.
	lastQueued, active, err := store.StudentRunState(id, 7)
	if err != nil {
		t.Fatal(err)
	}
	if lastQueued.IsZero() || active != 0 {
		t.Fatalf("run state: lastQueued=%v active=%d", lastQueued, active)
	}

	// Plagiarism runs and pairs.
	plagID, err := store.CreateSubPlagRun(id)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReplaceSubPlagPairs(plagID, []SubPlagPair{{RunID: plagID, StudentA: 7, StudentB: 8, Similarity: 0.95}}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSubPlagRunStatus(plagID, "done", "/tmp/report", ""); err != nil {
		t.Fatal(err)
	}
	plag, err := store.LatestSubPlagRun(id)
	if err != nil || plag == nil || plag.Status != "done" || plag.ReportPath != "/tmp/report" || plag.FinishedAt == nil {
		t.Fatalf("plag run: %+v %v", plag, err)
	}
	pairs, err := store.ListSubPlagPairs(plagID)
	if err != nil || len(pairs) != 1 || pairs[0].Similarity != 0.95 {
		t.Fatalf("pairs: %+v %v", pairs, err)
	}

	// Delete cascades to every child table.
	if err := store.DeleteSubAssignment(id); err != nil {
		t.Fatal(err)
	}
	if got, err := store.GetSubAssignment(id); err != nil || got != nil {
		t.Fatalf("assignment after delete: %+v %v", got, err)
	}
	if tests, err := store.ListSubTests(id); err != nil || len(tests) != 0 {
		t.Fatalf("tests after delete: %v %v", tests, err)
	}
	if latest, err := store.LatestSubSubmission(id, 7); err != nil || latest != nil {
		t.Fatalf("submission after delete: %+v %v", latest, err)
	}
	if runs, err := store.RunsForSubmission(sub2.ID); err != nil || len(runs) != 0 {
		t.Fatalf("runs after delete: %v %v", runs, err)
	}
	if plag, err := store.LatestSubPlagRun(id); err != nil || plag != nil {
		t.Fatalf("plag run after delete: %+v %v", plag, err)
	}
	if pairs, err := store.ListSubPlagPairs(plagID); err != nil || len(pairs) != 0 {
		t.Fatalf("pairs after delete: %v %v", pairs, err)
	}
}
