package portalserver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/davidpopovici01/grades/internal/portalauth"
)

func publishTestAccount(t *testing.T, server *Server, studentID int, username, password string) {
	t.Helper()
	hash, salt, err := portalauth.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	publishReq := PublishRequest{
		Accounts: []portalauth.Account{{
			StudentID:          studentID,
			Username:           username,
			PasswordSalt:       salt,
			PasswordHash:       hash,
			MustChangePassword: false,
			PasswordChangedAt:  time.Now().UTC().Format(time.RFC3339),
		}},
		Course: CourseInfo{
			CourseYearID: 1,
			TermID:       1,
			CourseName:   "Test Course",
			TermName:     "Fall 2026",
			PublishedAt:  time.Now().UTC().Format(time.RFC3339),
		},
	}
	body, _ := json.Marshal(publishReq)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/publish", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-teacher-token")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("publish failed: %d %s", rec.Code, rec.Body.String())
	}
}

func loginForCookie(t *testing.T, server *Server, username, password string) *http.Cookie {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login failed: %d %s", rec.Code, rec.Body.String())
	}
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == cookieName {
			return cookie
		}
	}
	t.Fatalf("login response had no %s cookie", cookieName)
	return nil
}

func TestLoginPresenceAndActivityEndpoint(t *testing.T) {
	server := newTestServer(t)
	publishTestAccount(t, server, 1, "john.doe", "testpass")

	// Failed login records a login_failed event and issues no cookie.
	body, _ := json.Marshal(map[string]string{"username": "john.doe", "password": "wrong"})
	req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for bad password, got %d", rec.Code)
	}

	// Successful login records a login event.
	cookie := loginForCookie(t, server, "john.doe", "testpass")

	// An authenticated request marks the account as seen.
	req = httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req.AddCookie(cookie)
	rec = httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("me failed: %d %s", rec.Code, rec.Body.String())
	}

	// The admin endpoint requires the teacher token.
	req = httptest.NewRequest(http.MethodGet, "/api/admin/activity", nil)
	rec = httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without admin token, got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/admin/activity?limit=10", nil)
	req.Header.Set("Authorization", "Bearer test-teacher-token")
	rec = httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("activity failed: %d %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Events   []ActivityEvent   `json:"events"`
		Accounts []AccountActivity `json:"accounts"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode activity: %v", err)
	}

	kinds := map[string]bool{}
	for _, ev := range payload.Events {
		kinds[ev.Kind] = true
		if ev.Username != "john.doe" {
			t.Fatalf("expected event username john.doe, got %q", ev.Username)
		}
	}
	if !kinds[activityLogin] || !kinds[activityLoginFailed] {
		t.Fatalf("expected login and login_failed events, got %v", kinds)
	}

	if len(payload.Accounts) != 1 || payload.Accounts[0].Username != "john.doe" {
		t.Fatalf("expected one account john.doe, got %+v", payload.Accounts)
	}
	if payload.Accounts[0].LastSeenAt == nil {
		t.Fatalf("expected last_seen_at to be set after login + me")
	}
}

func TestTouchLastSeenThrottled(t *testing.T) {
	server := newTestServer(t)
	publishTestAccount(t, server, 1, "jane.doe", "testpass")

	lastSeen := func() *string {
		t.Helper()
		var value *string
		if err := server.store.db.QueryRow(`SELECT last_seen_at FROM published_accounts WHERE student_pk = 1`).Scan(&value); err != nil {
			t.Fatalf("read last_seen_at: %v", err)
		}
		return value
	}

	if got := lastSeen(); got != nil {
		t.Fatalf("expected last_seen_at to start NULL, got %q", *got)
	}
	if err := server.store.TouchLastSeen(1); err != nil {
		t.Fatalf("touch: %v", err)
	}
	first := lastSeen()
	if first == nil {
		t.Fatalf("expected last_seen_at to be set")
	}
	if err := server.store.TouchLastSeen(1); err != nil {
		t.Fatalf("second touch: %v", err)
	}
	if got := lastSeen(); got == nil || *got != *first {
		t.Fatalf("expected throttled second touch to keep %q, got %v", *first, got)
	}

	// An entry older than the throttle interval is refreshed.
	old := time.Now().UTC().Add(-2 * lastSeenMinInterval).Format(time.RFC3339)
	if _, err := server.store.db.Exec(`UPDATE published_accounts SET last_seen_at = ? WHERE student_pk = 1`, old); err != nil {
		t.Fatalf("age last_seen_at: %v", err)
	}
	if err := server.store.TouchLastSeen(1); err != nil {
		t.Fatalf("touch after aging: %v", err)
	}
	if got := lastSeen(); got == nil || *got <= old {
		t.Fatalf("expected last_seen_at to refresh past %q, got %v", old, got)
	}
}

func TestRecentActivityOrderingAndLimit(t *testing.T) {
	server := newTestServer(t)
	publishTestAccount(t, server, 1, "john.doe", "testpass")

	if err := server.store.LogActivity(1, "john.doe", activityLogin, ""); err != nil {
		t.Fatalf("log login: %v", err)
	}
	if err := server.store.LogActivity(0, "john.doe", activityLoginFailed, ""); err != nil {
		t.Fatalf("log failed login: %v", err)
	}
	if err := server.store.LogActivity(1, "john.doe", activitySubmit, "HW3"); err != nil {
		t.Fatalf("log submit: %v", err)
	}

	events, err := server.store.RecentActivity(2)
	if err != nil {
		t.Fatalf("recent activity: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected limit 2, got %d", len(events))
	}
	if events[0].Kind != activitySubmit || events[1].Kind != activityLoginFailed {
		t.Fatalf("expected newest first (submit, login_failed), got %s, %s", events[0].Kind, events[1].Kind)
	}
	if events[0].Detail != "HW3" {
		t.Fatalf("expected detail HW3, got %q", events[0].Detail)
	}
	if events[0].CreatedAt == "" {
		t.Fatalf("expected created_at to be populated")
	}
}

func TestAccountActivityNeverSeen(t *testing.T) {
	server := newTestServer(t)
	publishTestAccount(t, server, 1, "john.doe", "testpass")

	accounts, err := server.store.AccountActivity()
	if err != nil {
		t.Fatalf("account activity: %v", err)
	}
	if len(accounts) != 1 || accounts[0].Username != "john.doe" {
		t.Fatalf("expected one account, got %+v", accounts)
	}
	if accounts[0].LastSeenAt != nil {
		t.Fatalf("expected never-seen account to have NULL last_seen_at, got %q", *accounts[0].LastSeenAt)
	}
}
