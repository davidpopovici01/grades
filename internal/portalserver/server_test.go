package portalserver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/davidpopovici01/grades/internal/portalauth"
)

func TestPortalServerLoginAndGrades(t *testing.T) {
	// Create temporary data directory.
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "data")
	staticDir := filepath.Join(tmpDir, "static")
	os.MkdirAll(dataDir, 0755)
	os.MkdirAll(staticDir, 0755)

	// Write test accounts.
	accounts := portalauth.AccountList{
		Version: 1,
		Accounts: []portalauth.Account{
			{
				StudentID:          1,
				Username:           "john.doe",
				PasswordSalt:       "aabbccdd",
				PasswordHash:       "",
				MustChangePassword: false,
			},
		},
	}
	// Compute a real password hash.
	hash, salt, err := portalauth.HashPassword("testpass")
	if err != nil {
		t.Fatal(err)
	}
	accounts.Accounts[0].PasswordSalt = salt
	accounts.Accounts[0].PasswordHash = hash

	accountsData, _ := json.Marshal(accounts)
	os.WriteFile(filepath.Join(dataDir, "accounts.json"), accountsData, 0644)

	// Write test grade snapshot.
	grades := map[string]any{
		"studentId":   1,
		"firstName":   "John",
		"lastName":    "Doe",
		"courseName":  "Test Course",
		"termName":    "Fall 2026",
		"weightedTotal": 85.5,
		"categories":  []any{},
		"assignments": []any{},
	}
	os.MkdirAll(filepath.Join(dataDir, "students"), 0755)
	gradesData, _ := json.Marshal(grades)
	os.WriteFile(filepath.Join(dataDir, "students", "1.json"), gradesData, 0644)

	// Write a dummy index.html.
	os.WriteFile(filepath.Join(staticDir, "index.html"), []byte("<html></html>"), 0644)

	// Create server.
	server, err := NewServer(Config{
		DataDir:      dataDir,
		StaticDir:    staticDir,
		JWTSecret:    []byte("test-secret-key-that-is-long-enough"),
		CookieSecure: false,
	})
	if err != nil {
		t.Fatal(err)
	}

	handler := server.Handler()

	// Test login.
	loginBody, _ := json.Marshal(map[string]string{"username": "john.doe", "password": "testpass"})
	req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(loginBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
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
	if gradesResp["firstName"] != "John" {
		t.Fatalf("unexpected grades response: %v", gradesResp)
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
