package portalserver

import (
	"encoding/json"
	"net/http"
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

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	acc, err := s.store.GetAccountByUsername(req.Username)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load account"})
		return
	}
	if acc == nil {
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
	claims, err := s.readToken(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	acc, err := s.store.GetAccountByStudentID(claims.StudentID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load account"})
		return
	}
	if acc == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "account not found"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"studentId":          acc.StudentID,
		"username":           acc.Username,
		"mustChangePassword": acc.MustChangePassword,
	})
}

// handleGrades serves the student's published course snapshots.
func (s *Server) handleGrades(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	claims, err := s.readToken(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	snapshots, err := s.store.GetStudentSnapshots(claims.StudentID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read grades"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"studentId": claims.StudentID,
		"courses":   snapshots,
	})
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

	acc, err := s.store.GetAccountByStudentID(claims.StudentID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load account"})
		return
	}
	if acc == nil {
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

	changedAt := time.Now().UTC()
	if err := s.store.UpdateAccountPassword(acc.StudentID, acc.Username, salt, hash, false, changedAt); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not save password"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleIndex serves the published course index.
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	courses, err := s.store.ListCourses()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read index"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"courses": courses})
}

// handleAdminPublish atomically publishes a course snapshot and its accounts.
func (s *Server) handleAdminPublish(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var req PublishRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if req.Course.CourseYearID == 0 || req.Course.TermID == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "courseYearId and termId are required"})
		return
	}

	if err := s.store.PublishCourse(&req); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to publish course"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"students":  len(req.Students),
		"accounts":  len(req.Accounts),
		"published": req.Course.PublishedAt,
	})
}

// handleAdminListCourses returns all published courses.
func (s *Server) handleAdminListCourses(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	courses, err := s.store.ListCourses()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list courses"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"courses": courses})
}

// handleAdminCourseRoutes handles /api/admin/courses/{courseYearId}/{termId}/students and DELETE.
func (s *Server) handleAdminCourseRoutes(w http.ResponseWriter, r *http.Request) {
	// Path format: /api/admin/courses/{courseYearId}/{termId}[/students]
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/admin/courses/"), "/")
	if len(parts) < 2 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}

	courseYearID, err := strconv.Atoi(parts[0])
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid courseYearId"})
		return
	}
	termID, err := strconv.Atoi(parts[1])
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid termId"})
		return
	}

	if len(parts) == 2 && r.Method == http.MethodDelete {
		if err := s.store.DeleteCourse(courseYearID, termID); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete course"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}

	if len(parts) == 3 && parts[2] == "students" && r.Method == http.MethodGet {
		course, err := s.store.GetCourse(courseYearID, termID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load course"})
			return
		}
		if course == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "course not found"})
			return
		}
		students, err := s.store.ListStudentsForCourse(courseYearID, termID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list students"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"courseName": course.CourseName,
			"termName":   course.TermName,
			"students":   students,
		})
		return
	}

	writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
}

// handleAdminResetPassword resets a student's password and returns the new temporary password.
func (s *Server) handleAdminResetPassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/admin/students/"), "/")
	if len(parts) != 2 || parts[1] != "reset-password" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}

	studentID, err := strconv.Atoi(parts[0])
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid studentId"})
		return
	}

	acc, err := s.store.GetAccountByStudentID(studentID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load account"})
		return
	}
	if acc == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "account not found"})
		return
	}

	password, err := portalauth.RandomOrMemorablePassword(true)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not generate password"})
		return
	}

	hash, salt, err := portalauth.HashPassword(password)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not hash password"})
		return
	}

	changedAt := time.Now().UTC()
	if err := s.store.UpdateAccountPassword(studentID, acc.Username, salt, hash, true, changedAt); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not save password"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                true,
		"studentId":         studentID,
		"username":          acc.Username,
		"temporaryPassword": password,
	})
}
