package portalserver

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/davidpopovici01/grades/internal/portalauth"
)

// handleLogin validates credentials and issues a JWT cookie.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	_ = s.maybeReloadAccounts()

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	acc, ok := s.accounts[strings.ToLower(strings.TrimSpace(req.Username))]
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}

	if !portalauth.VerifyPassword(req.Password, acc.PasswordSalt, acc.PasswordHash) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}

	token, err := s.jwt.Sign(acc.StudentID, acc.Username, tokenDuration)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create session"})
		return
	}

	s.setTokenCookie(w, token)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                 true,
		"studentId":          acc.StudentID,
		"username":           acc.Username,
		"mustChangePassword": acc.MustChangePassword,
	})
}

// handleLogout clears the JWT cookie.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	s.clearTokenCookie(w)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleMe returns the current authenticated student's info.
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	_ = s.maybeReloadAccounts()
	claims, err := s.readToken(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	acc, ok := s.students[claims.StudentID]
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "account not found"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"studentId":          acc.StudentID,
		"username":           acc.Username,
		"mustChangePassword": acc.MustChangePassword,
	})
}

// handleGrades serves the student's grade snapshot JSON file.
func (s *Server) handleGrades(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	_ = s.maybeReloadAccounts()

	claims, err := s.readToken(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	path := filepath.Join(s.config.DataDir, "students", strconv.Itoa(claims.StudentID)+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "grade data not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read grades"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

// handleChangePassword allows a student to change their own password.
func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	claims, err := s.readToken(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	var req struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if len(strings.TrimSpace(req.NewPassword)) < 8 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "new password must be at least 8 characters"})
		return
	}

	acc, ok := s.students[claims.StudentID]
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "account not found"})
		return
	}

	if !portalauth.VerifyPassword(req.CurrentPassword, acc.PasswordSalt, acc.PasswordHash) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "current password is incorrect"})
		return
	}

	hash, salt, err := portalauth.HashPassword(req.NewPassword)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not hash password"})
		return
	}

	changedAt := time.Now().UTC().Format(time.RFC3339)
	updated := portalauth.Account{
		StudentID:         acc.StudentID,
		Username:          acc.Username,
		PasswordSalt:      salt,
		PasswordHash:      hash,
		MustChangePassword: false,
		PasswordChangedAt: changedAt,
	}

	s.mu.Lock()
	s.passwordChanges[acc.StudentID] = updated
	s.accounts[strings.ToLower(acc.Username)] = updated
	s.students[acc.StudentID] = updated
	s.mu.Unlock()

	if err := s.savePasswordChanges(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not save password"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleIndex serves the course index metadata.
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	_ = s.maybeReloadAccounts()

	path := filepath.Join(s.config.DataDir, "index.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "index not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read index"})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}
