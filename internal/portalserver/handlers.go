package portalserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
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
		s.logActivity(0, req.Username, activityLoginFailed, "")
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}

	token, err := s.jwt.Sign(acc.StudentID, acc.Username, tokenDuration)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create session"})
		return
	}

	// Expire any legacy host-only cookie so it cannot shadow the new
	// domain-wide cookie while old sessions roll over.
	if s.config.CookieDomain != "" {
		http.SetCookie(w, &http.Cookie{
			Name:     cookieName,
			Value:    "",
			Path:     "/",
			HttpOnly: true,
			Secure:   s.config.CookieSecure,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   -1,
		})
	}
	s.setTokenCookie(w, token)
	s.logActivity(acc.StudentID, acc.Username, activityLogin, "")
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

	s.logActivity(acc.StudentID, acc.Username, activityPasswordChange, "")
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

// snapshotAssignment mirrors the per-assignment fields of the published
// student snapshot that the admin grades overview needs.
type snapshotAssignment struct {
	AssignmentID int      `json:"assignmentId"`
	Title        string   `json:"title"`
	CategoryID   int      `json:"categoryId"`
	CategoryName string   `json:"categoryName"`
	MaxPoints    int      `json:"maxPoints"`
	PassPercent  *float64 `json:"passPercent"`
	Score        *float64 `json:"score"`
	Flags        []string `json:"flags"`
}

// snapshotGrades mirrors the student-level fields of the published snapshot.
type snapshotGrades struct {
	FirstName     string               `json:"firstName"`
	LastName      string               `json:"lastName"`
	ChineseName   string               `json:"chineseName"`
	WeightedTotal float64              `json:"weightedTotal"`
	LetterGrade   string               `json:"letterGrade"`
	Assignments   []snapshotAssignment `json:"assignments"`
}

// pendingAction mirrors the CLI gradebook rules: a missing flag always means
// missing (the CLI stores it as score 0 + flag), and redo is pending when the
// score is missing or below the pass rate — including an unflagged failing
// score (hasPendingRedo in grades.go).
func pendingAction(a snapshotAssignment) (missing, redo bool) {
	cheat, passFlag, hasRedo := false, false, false
	for _, f := range a.Flags {
		switch f {
		case "missing":
			missing = true
		case "cheat":
			cheat = true
		case "pass":
			passFlag = true
		case "redo":
			hasRedo = true
		}
	}
	if missing {
		return true, false
	}
	if cheat || passFlag {
		return false, false
	}
	if a.PassPercent == nil || *a.PassPercent <= 0 || a.MaxPoints <= 0 {
		return false, false
	}
	passing := a.Score != nil && (*a.Score/float64(a.MaxPoints))*100 >= *a.PassPercent
	if hasRedo {
		return false, !passing
	}
	return false, a.Score != nil && !passing
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
		rows, err := s.store.ListSnapshotRowsForCourse(courseYearID, termID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list students"})
			return
		}

		students := []map[string]any{}
		columns := []map[string]any{}
		seenColumns := map[int]bool{}
		for _, row := range rows {
			var snap snapshotGrades
			if err := json.Unmarshal(row.Raw, &snap); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to parse snapshot"})
				return
			}
			grades := map[string]any{}
			missing, redo := 0, 0
			for _, a := range snap.Assignments {
				if !seenColumns[a.AssignmentID] {
					seenColumns[a.AssignmentID] = true
					columns = append(columns, map[string]any{
						"id": a.AssignmentID, "title": a.Title, "maxPoints": a.MaxPoints, "categoryName": a.CategoryName,
					})
				}
				label := ""
				if a.Score != nil {
					label = fmt.Sprintf("%s/%d", strconv.FormatFloat(*a.Score, 'f', -1, 64), a.MaxPoints)
				}
				flags := a.Flags
				if flags == nil {
					flags = []string{}
				}
				pendingMissing, pendingRedo := pendingAction(a)
				if pendingMissing {
					missing++
				}
				if pendingRedo {
					redo++
				}
				// Show chips for pending actions even when the flag itself is
				// absent (an unflagged failing score still needs a redo).
				display := append([]string{}, flags...)
				if pendingRedo && !slices.Contains(display, "redo") {
					display = append(display, "redo")
				}
				grades[strconv.Itoa(a.AssignmentID)] = map[string]any{
					"score":   a.Score,
					"flags":   display,
					"pending": map[string]bool{"missing": pendingMissing, "redo": pendingRedo},
					"label":   label,
				}
			}
			students = append(students, map[string]any{
				"studentId":     row.StudentID,
				"username":      row.Username,
				"firstName":     snap.FirstName,
				"lastName":      snap.LastName,
				"chineseName":   snap.ChineseName,
				"weightedTotal": snap.WeightedTotal,
				"letterGrade":   snap.LetterGrade,
				"missing":       missing,
				"redo":          redo,
				"grades":        grades,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"courseName":     course.CourseName,
			"courseYearName": course.CourseYearName,
			"termName":       course.TermName,
			"publishedAt":    course.PublishedAt,
			"assignments":    columns,
			"students":       students,
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
