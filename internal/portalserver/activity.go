package portalserver

import (
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// Activity event kinds recorded in activity_events.
const (
	activityLogin          = "login"
	activityLoginFailed    = "login_failed"
	activitySubmit         = "submit"
	activityUpload         = "upload"
	activityDownload       = "download"
	activityPasswordChange = "password_change"
)

// ActivityEvent is one recorded portal action.
type ActivityEvent struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	Kind      string `json:"kind"`
	Detail    string `json:"detail"`
	CreatedAt string `json:"createdAt"`
}

// AccountActivity is the last-seen state of one portal account.
type AccountActivity struct {
	Username   string  `json:"username"`
	LastSeenAt *string `json:"lastSeenAt"`
}

// lastSeenMinInterval bounds last_seen_at writes to one per student per interval.
const lastSeenMinInterval = time.Minute

// LogActivity records one portal activity event. Failures are returned to the
// caller, which should log them without failing the user's request.
func (s *Store) LogActivity(studentPK int, username, kind, detail string) error {
	var pk any
	if studentPK > 0 {
		pk = studentPK
	}
	_, err := s.db.Exec(`
		INSERT INTO activity_events(student_pk, username, kind, detail)
		VALUES (?, ?, ?, ?)`, pk, username, kind, detail)
	return err
}

// TouchLastSeen marks the account active now, writing at most once per
// lastSeenMinInterval per student.
func (s *Store) TouchLastSeen(studentPK int) error {
	now := time.Now().UTC()
	cutoff := now.Add(-lastSeenMinInterval).Format(time.RFC3339)
	_, err := s.db.Exec(`
		UPDATE published_accounts
		SET last_seen_at = ?
		WHERE student_pk = ? AND (last_seen_at IS NULL OR last_seen_at < ?)`,
		now.Format(time.RFC3339), studentPK, cutoff)
	return err
}

// RecentActivity returns the newest activity events, most recent first.
func (s *Store) RecentActivity(limit int) ([]ActivityEvent, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.Query(`
		SELECT id, username, kind, detail, created_at
		FROM activity_events
		ORDER BY id DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []ActivityEvent{}
	for rows.Next() {
		var ev ActivityEvent
		if err := rows.Scan(&ev.ID, &ev.Username, &ev.Kind, &ev.Detail, &ev.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, ev)
	}
	return events, rows.Err()
}

// AccountActivity returns every account with its last-seen timestamp
// (NULL when the student has never used the portal).
func (s *Store) AccountActivity() ([]AccountActivity, error) {
	rows, err := s.db.Query(`
		SELECT username, last_seen_at
		FROM published_accounts
		ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	accounts := []AccountActivity{}
	for rows.Next() {
		var acc AccountActivity
		if err := rows.Scan(&acc.Username, &acc.LastSeenAt); err != nil {
			return nil, err
		}
		accounts = append(accounts, acc)
	}
	return accounts, rows.Err()
}

// logActivity records an event and only logs failures; used by handlers where
// losing an event is acceptable but failing the request is not.
func (s *Server) logActivity(studentPK int, username, kind, detail string) {
	if err := s.store.LogActivity(studentPK, username, kind, detail); err != nil {
		fmt.Printf("activity log failed (%s %s): %v\n", kind, username, err)
	}
}

// handleAdminActivity returns recent portal activity events plus the
// last-seen timestamp of every account.
func (s *Server) handleAdminActivity(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	events, err := s.store.RecentActivity(limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read activity"})
		return
	}
	accounts, err := s.store.AccountActivity()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read accounts"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"events":   events,
		"accounts": accounts,
	})
}
