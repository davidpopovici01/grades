package portalserver

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// testCooldown is the minimum interval between a student's test runs.
const testCooldown = 30 * time.Second

// maxTestFileBytes caps an uploaded harness file.
const maxTestFileBytes = 1 << 20

// cooldownTracker remembers the last student-triggered test enqueue per
// student+assignment, in memory, as the fast path; the database is the
// restart-safe backstop.
type cooldownTracker struct {
	mu   sync.Mutex
	last map[string]time.Time
}

func newCooldownTracker() *cooldownTracker {
	return &cooldownTracker{last: map[string]time.Time{}}
}

func cooldownKey(assignmentID int64, studentPK int) string {
	return strconv.FormatInt(assignmentID, 10) + ":" + strconv.Itoa(studentPK)
}

func (c *cooldownTracker) record(assignmentID int64, studentPK int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.last[cooldownKey(assignmentID, studentPK)] = time.Now()
}

// remaining returns the seconds until the student may test again (0 = now).
func (s *Server) cooldownRemaining(assignmentID int64, studentPK int) int {
	remaining := 0
	consider := func(since time.Time) {
		if since.IsZero() {
			return
		}
		if left := testCooldown - time.Since(since); left > 0 {
			if secs := int(math.Ceil(left.Seconds())); secs > remaining {
				remaining = secs
			}
		}
	}

	s.cooldowns.mu.Lock()
	since, ok := s.cooldowns.last[cooldownKey(assignmentID, studentPK)]
	s.cooldowns.mu.Unlock()
	if ok {
		consider(since)
	}

	lastQueued, active, err := s.store.StudentRunState(assignmentID, studentPK)
	if err == nil {
		consider(lastQueued)
		if active > 0 && remaining == 0 {
			remaining = 1
		}
	}
	return remaining
}

// enrolled reports whether the student has a snapshot in the course.
func (s *Server) enrolled(studentID, courseYearID, termID int) (bool, error) {
	snapshots, err := s.store.GetStudentSnapshots(studentID)
	if err != nil {
		return false, err
	}
	for _, snap := range snapshots {
		if snap.CourseYearID == courseYearID && snap.TermID == termID {
			return true, nil
		}
	}
	return false, nil
}

// parseDueAt accepts RFC3339 and the datetime-local form ("2006-01-02T15:04",
// interpreted as server-local time) and normalizes to UTC RFC3339.
func parseDueAt(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC().Format(time.RFC3339), nil
	}
	if t, err := time.ParseInLocation("2006-01-02T15:04", raw, time.Local); err == nil {
		return t.UTC().Format(time.RFC3339), nil
	}
	return "", fmt.Errorf("invalid due date")
}

// expectedSlot is one required file entry. The display may list alternative
// extensions ("report.docx/pdf"), meaning the student submits exactly one of
// the concrete names.
type expectedSlot struct {
	display string
	names   []string
}

// key identifies the slot in URLs and lookups: the first alternative.
func (s expectedSlot) key() string { return s.names[0] }

// match reports whether name is exactly one of the slot's alternatives,
// returning the alternative's canonical spelling. Case must match: a Java
// file renamed to the canonical spelling would no longer match the class
// name the student wrote, so mis-cased names are rejected instead.
func (s expectedSlot) match(name string) (string, bool) {
	for _, n := range s.names {
		if n == name {
			return n, true
		}
	}
	return "", false
}

// parseExpectedSlots converts raw entries into slots.
func parseExpectedSlots(entries []string) []expectedSlot {
	slots := make([]expectedSlot, 0, len(entries))
	for _, entry := range entries {
		parts := strings.Split(entry, "/")
		slot := expectedSlot{display: entry}
		first, err := sanitizeFilename(strings.TrimSpace(parts[0]))
		if err != nil {
			continue
		}
		stem := first
		if dot := strings.LastIndex(first, "."); dot > 0 {
			stem = first[:dot]
		}
		slot.names = append(slot.names, first)
		for _, part := range parts[1:] {
			part = strings.TrimSpace(strings.TrimPrefix(part, "."))
			if part != "" {
				slot.names = append(slot.names, stem+"."+part)
			}
		}
		if len(slot.names) > 0 {
			slots = append(slots, slot)
		}
	}
	return slots
}

// findSlot returns the slot whose key matches name (case-insensitive).
func findSlot(slots []expectedSlot, name string) *expectedSlot {
	for i := range slots {
		if strings.EqualFold(slots[i].key(), name) {
			return &slots[i]
		}
	}
	return nil
}

// parseExpectedFilenames validates the newline-separated filename list. Each
// line is a filename, optionally with alternative extensions
// ("report.docx/pdf" accepts report.docx or report.pdf). For code/text
// languages at least one entry must allow the language's extension; any other
// files (reports, data, media) may be required alongside.
func parseExpectedFilenames(language, raw string) ([]string, error) {
	exts := map[string][]string{
		"java":   {".java"},
		"python": {".py"},
		"text":   {".txt", ".md"},
	}[language]
	entries := []string{}
	seen := map[string]bool{}
	hasCode := false
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, "/")
		first, err := sanitizeFilename(strings.TrimSpace(parts[0]))
		if err != nil {
			return nil, fmt.Errorf("invalid filename %q", line)
		}
		names := []string{first}
		for _, part := range parts[1:] {
			part = strings.TrimSpace(strings.TrimPrefix(part, "."))
			if part == "" || strings.ContainsAny(part, "./\\ ") {
				return nil, fmt.Errorf("invalid alternative extension in %q (use name.ext1/ext2, e.g. report.docx/pdf)", line)
			}
			stem := first
			if dot := strings.LastIndex(first, "."); dot > 0 {
				stem = first[:dot]
			}
			names = append(names, stem+"."+part)
		}
		for _, name := range names {
			lower := strings.ToLower(name)
			if seen[lower] {
				return nil, fmt.Errorf("duplicate filename %q", name)
			}
			seen[lower] = true
			for _, ext := range exts {
				if strings.HasSuffix(lower, ext) {
					hasCode = true
					break
				}
			}
		}
		entries = append(entries, line)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("at least one expected filename is required")
	}
	if len(exts) > 0 && !hasCode {
		return nil, fmt.Errorf("at least one filename must end with %s", strings.Join(exts, " or "))
	}
	return entries, nil
}

// assignmentRequest is the admin create/update payload; expectedFilenames is
// newline-separated.
type assignmentRequest struct {
	CourseYearID      int    `json:"courseYearId"`
	TermID            int    `json:"termId"`
	Title             string `json:"title"`
	Language          string `json:"language"`
	Instructions      string `json:"instructions"`
	DueAt             string `json:"dueAt"`
	ExpectedFilenames string `json:"expectedFilenames"`
	MaxFileBytes      int64  `json:"maxFileBytes"`
	MaxTotalBytes     int64  `json:"maxTotalBytes"`
	LateCapPercent    int    `json:"lateCapPercent"`
	IsOpen            *bool  `json:"isOpen"`
}

// validateAssignmentRequest checks the payload and resolves defaults.
func (s *Server) validateAssignmentRequest(req *assignmentRequest) (*SubAssignment, string) {
	title := strings.TrimSpace(req.Title)
	if title == "" {
		return nil, "title is required"
	}
	if req.Language != "java" && req.Language != "python" && req.Language != "text" && req.Language != "files" {
		return nil, "language must be java, python, text, or files"
	}
	course, err := s.store.GetCourse(req.CourseYearID, req.TermID)
	if err != nil {
		return nil, "failed to load course"
	}
	if course == nil {
		return nil, "course not published"
	}
	filenames, err := parseExpectedFilenames(req.Language, req.ExpectedFilenames)
	if err != nil {
		return nil, err.Error()
	}
	dueAt, err := parseDueAt(req.DueAt)
	if err != nil {
		return nil, err.Error()
	}
	a := &SubAssignment{
		CourseYearID:      req.CourseYearID,
		TermID:            req.TermID,
		Title:             title,
		Language:          req.Language,
		Instructions:      req.Instructions,
		ExpectedFilenames: filenames,
		MaxFileBytes:      req.MaxFileBytes,
		MaxTotalBytes:     req.MaxTotalBytes,
		LateCapPercent:    req.LateCapPercent,
		IsOpen:            true,
	}
	if dueAt != "" {
		a.DueAt = &dueAt
	}
	if a.MaxFileBytes <= 0 {
		a.MaxFileBytes = 262144
	}
	if a.MaxTotalBytes <= 0 {
		a.MaxTotalBytes = 1048576
	}
	if a.LateCapPercent <= 0 {
		a.LateCapPercent = 90
	}
	if a.LateCapPercent > 100 {
		return nil, "lateCapPercent must be between 1 and 100"
	}
	if req.IsOpen != nil {
		a.IsOpen = *req.IsOpen
	}
	return a, ""
}

// adminAssignmentJSON renders an assignment for admin clients, with the
// newline-joined filename list matching the create/update payload.
func adminAssignmentJSON(a *SubAssignment, courseName string) map[string]any {
	out := map[string]any{
		"id":                a.ID,
		"courseYearId":      a.CourseYearID,
		"termId":            a.TermID,
		"title":             a.Title,
		"language":          a.Language,
		"instructions":      a.Instructions,
		"dueAt":             a.DueAt,
		"expectedFilenames": strings.Join(a.ExpectedFilenames, "\n"),
		"maxFileBytes":      a.MaxFileBytes,
		"maxTotalBytes":     a.MaxTotalBytes,
		"lateCapPercent":    a.LateCapPercent,
		"isOpen":            a.IsOpen,
		"createdAt":         a.CreatedAt,
	}
	if courseName != "" {
		out["courseName"] = courseName
	}
	return out
}

// studentAssignmentJSON renders an assignment for students (filenames as a list).
func studentAssignmentJSON(a *SubAssignment) map[string]any {
	return map[string]any{
		"id":                a.ID,
		"title":             a.Title,
		"language":          a.Language,
		"instructions":      a.Instructions,
		"dueAt":             a.DueAt,
		"expectedFilenames": a.ExpectedFilenames,
		"maxFileBytes":      a.MaxFileBytes,
		"maxTotalBytes":     a.MaxTotalBytes,
		"lateCapPercent":    a.LateCapPercent,
		"isOpen":            a.IsOpen,
	}
}

// handleAdminSubAssignments lists (GET, optional courseYearId/termId filter)
// or creates (POST) submission assignments.
func (s *Server) handleAdminSubAssignments(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		var assignments []SubAssignment
		var err error
		courseYearID, err1 := strconv.Atoi(r.URL.Query().Get("courseYearId"))
		termID, err2 := strconv.Atoi(r.URL.Query().Get("termId"))
		if err1 == nil && err2 == nil {
			assignments, err = s.store.ListSubAssignmentsForCourse(courseYearID, termID)
		} else {
			assignments, err = s.store.ListAllSubAssignments()
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list assignments"})
			return
		}
		courseNames := map[[2]int]string{}
		if courses, err := s.store.ListCourses(); err == nil {
			for _, c := range courses {
				courseNames[[2]int{c.CourseYearID, c.TermID}] = c.CourseName
			}
		}
		out := []map[string]any{}
		for i := range assignments {
			a := &assignments[i]
			out = append(out, adminAssignmentJSON(a, courseNames[[2]int{a.CourseYearID, a.TermID}]))
		}
		writeJSON(w, http.StatusOK, map[string]any{"assignments": out})
	case http.MethodPost:
		var req assignmentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		a, errMsg := s.validateAssignmentRequest(&req)
		if a == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": errMsg})
			return
		}
		id, err := s.store.CreateSubAssignment(a)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create assignment"})
			return
		}
		a.ID = id
		writeJSON(w, http.StatusOK, map[string]any{"assignment": adminAssignmentJSON(a, "")})
	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

// handleAdminSubAssignment handles GET/PUT/DELETE on one assignment.
func (s *Server) handleAdminSubAssignment(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid assignment id"})
		return
	}
	a, err := s.store.GetSubAssignment(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load assignment"})
		return
	}
	if a == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "assignment not found"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		tests, err := s.store.ListSubTests(id)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list tests"})
			return
		}
		out := []map[string]any{}
		for _, t := range tests {
			var size int64
			if info, err := os.Stat(filepath.Join(testsDir(s.config.SubmissionsDir, id), t.StoredName)); err == nil {
				size = info.Size()
			}
			out = append(out, map[string]any{
				"id": t.ID, "name": t.Name, "visibility": t.Visibility, "size": size,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"assignment": adminAssignmentJSON(a, ""), "tests": out})
	case http.MethodPut:
		var req assignmentRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		updated, errMsg := s.validateAssignmentRequest(&req)
		if updated == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": errMsg})
			return
		}
		updated.ID = id
		if err := s.store.UpdateSubAssignment(updated); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to update assignment"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"assignment": adminAssignmentJSON(updated, "")})
	case http.MethodDelete:
		if err := s.store.DeleteSubAssignment(id); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete assignment"})
			return
		}
		_ = os.RemoveAll(assignmentDir(s.config.SubmissionsDir, id))
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

// handleAdminSubTestUpload adds a harness file to an assignment (multipart:
// one "file" part plus "name" and "visibility" fields).
func (s *Server) handleAdminSubTestUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	a := s.adminAssignment(w, r)
	if a == nil {
		return
	}
	if a.Language == "text" || a.Language == "files" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "this assignment type does not support automated tests"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxTestFileBytes+1<<20)
	reader, err := r.MultipartReader()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "multipart form with a file part required"})
		return
	}

	var name, visibility, storedName string
	var content []byte
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "failed to read upload"})
			return
		}
		switch part.FormName() {
		case "name":
			data, _ := io.ReadAll(io.LimitReader(part, 1024))
			name = strings.TrimSpace(string(data))
		case "visibility":
			data, _ := io.ReadAll(io.LimitReader(part, 64))
			visibility = strings.TrimSpace(string(data))
		case "file":
			if part.FileName() == "" {
				continue
			}
			storedName, err = sanitizeFilename(part.FileName())
			if err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
				return
			}
			content, err = io.ReadAll(io.LimitReader(part, maxTestFileBytes+1))
			if err != nil || int64(len(content)) > maxTestFileBytes {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "test file too large"})
				return
			}
		}
	}
	if name == "" || len(content) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name and file are required"})
		return
	}
	if visibility == "" {
		visibility = "public"
	}
	if visibility != "public" && visibility != "secret" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "visibility must be public or secret"})
		return
	}
	ext := ".java"
	if a.Language == "python" {
		ext = ".py"
	}
	if !strings.HasSuffix(strings.ToLower(storedName), ext) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("test file must end with %s", ext)})
		return
	}

	dir := testsDir(s.config.SubmissionsDir, a.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create tests directory"})
		return
	}
	if _, err := os.Stat(filepath.Join(dir, storedName)); err == nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "a test file with that name already exists"})
		return
	}
	if err := os.WriteFile(filepath.Join(dir, storedName), content, 0o644); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to store test file"})
		return
	}

	test := SubTest{AssignmentID: a.ID, Name: name, Visibility: visibility, StoredName: storedName}
	id, err := s.store.CreateSubTest(&test)
	if err != nil {
		_ = os.Remove(filepath.Join(dir, storedName))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save test"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": id, "name": name, "visibility": visibility, "size": len(content),
	})
}

// handleAdminSubTestDelete removes a harness file and its row (DELETE),
// serves the harness file for preview/download (GET), or overwrites the
// harness content (PUT, raw text body).
func (s *Server) handleAdminSubTestDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete && r.Method != http.MethodGet && r.Method != http.MethodPut {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	a := s.adminAssignment(w, r)
	if a == nil {
		return
	}
	testID, err := strconv.ParseInt(r.PathValue("testId"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid test id"})
		return
	}
	test, err := s.store.GetSubTest(testID)
	if err != nil || test == nil || test.AssignmentID != a.ID {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "test not found"})
		return
	}
	path := filepath.Join(testsDir(s.config.SubmissionsDir, a.ID), test.StoredName)
	if r.Method == http.MethodPut {
		content, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxTestFileBytes+1))
		if err != nil || int64(len(content)) > maxTestFileBytes {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "test file too large"})
			return
		}
		if len(content) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "test file must not be empty"})
			return
		}
		if err := os.WriteFile(path, content, 0o644); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to store test file"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "size": len(content)})
		return
	}
	if r.Method == http.MethodGet {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "test file not found"})
			return
		}
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", test.StoredName))
		http.ServeFile(w, r, path)
		return
	}
	if err := s.store.DeleteSubTest(testID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete test"})
		return
	}
	_ = os.Remove(path)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// sampleFileCaps returns the per-file and total size caps for sample
// solution files, falling back to the submission defaults.
func sampleFileCaps(a *SubAssignment) (maxFile, maxTotal int64) {
	maxFile, maxTotal = a.MaxFileBytes, a.MaxTotalBytes
	if maxFile <= 0 {
		maxFile = 262144
	}
	if maxTotal <= 0 {
		maxTotal = 1048576
	}
	return maxFile, maxTotal
}

// auxFilesHandler lists the files of an assignment's auxiliary directory
// (kind "sample" = sample solution, "basecode" = plagiarism base code).
func (s *Server) auxFilesHandler(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		a := s.adminAssignment(w, r)
		if a == nil {
			return
		}
		files := []map[string]any{}
		entries, err := os.ReadDir(filepath.Join(assignmentDir(s.config.SubmissionsDir, a.ID), kind))
		if err != nil && !os.IsNotExist(err) {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list files"})
			return
		}
		for _, e := range entries {
			info, err := e.Info()
			if err != nil || e.IsDir() {
				continue
			}
			files = append(files, map[string]any{"name": e.Name(), "size": info.Size()})
		}
		writeJSON(w, http.StatusOK, map[string]any{"files": files})
	}
}

// auxFileHandler reads (GET), writes (PUT, raw text body), or removes
// (DELETE) one file of an assignment's auxiliary directory.
func (s *Server) auxFileHandler(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		a := s.adminAssignment(w, r)
		if a == nil {
			return
		}
		name, err := sanitizeFilename(r.PathValue("name"))
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		dir := filepath.Join(assignmentDir(s.config.SubmissionsDir, a.ID), kind)
		path := filepath.Join(dir, name)

		switch r.Method {
		case http.MethodGet:
			data, err := os.ReadFile(path)
			if err != nil {
				writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
				return
			}
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write(data)
		case http.MethodPut:
			maxFile, maxTotal := sampleFileCaps(a)
			content, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxFile+1))
			if err != nil || int64(len(content)) > maxFile {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("file exceeds the %d byte limit", maxFile)})
				return
			}
			var total int64
			entries, _ := os.ReadDir(dir)
			for _, e := range entries {
				if e.IsDir() || e.Name() == name {
					continue
				}
				if info, err := e.Info(); err == nil {
					total += info.Size()
				}
			}
			if total+int64(len(content)) > maxTotal {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("files exceed the %d byte total limit", maxTotal)})
				return
			}
			if err := os.MkdirAll(dir, 0o755); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create directory"})
				return
			}
			if err := os.WriteFile(path, content, 0o644); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to store file"})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "name": name, "size": len(content)})
		case http.MethodDelete:
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete file"})
				return
			}
			writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		default:
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		}
	}
}

// handleAdminSubSampleRun executes every test of the assignment against the
// sample solution files, synchronously, and returns per-test results.
func (s *Server) handleAdminSubSampleRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	a := s.adminAssignment(w, r)
	if a == nil {
		return
	}
	if a.Language != "java" && a.Language != "python" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "this assignment type does not support automated tests"})
		return
	}
	dir := sampleDir(s.config.SubmissionsDir, a.ID)
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list sample files"})
		return
	}
	names := []string{}
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no sample files yet — add files first"})
		return
	}
	tests, err := s.store.ListSubTests(a.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list tests"})
		return
	}
	results := []map[string]any{}
	for _, test := range tests {
		harnessPath := filepath.Join(testsDir(s.config.SubmissionsDir, a.ID), test.StoredName)
		status, passed, failed, output := s.grader.runHarness(a.Language, dir, names, harnessPath, test.StoredName)
		results = append(results, map[string]any{
			"testId":     test.ID,
			"testName":   test.Name,
			"visibility": test.Visibility,
			"status":     status,
			"passed":     passed,
			"failed":     failed,
			"output":     output,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}

// adminAssignment resolves the {id} path value to an assignment for admin handlers.
func (s *Server) adminAssignment(w http.ResponseWriter, r *http.Request) *SubAssignment {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid assignment id"})
		return nil
	}
	a, err := s.store.GetSubAssignment(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load assignment"})
		return nil
	}
	if a == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "assignment not found"})
		return nil
	}
	return a
}

// submissionJSON renders a submission with its files.
func (s *Server) submissionJSON(sub *SubSubmission) (map[string]any, error) {
	files, err := s.store.ListSubFiles(sub.ID)
	if err != nil {
		return nil, err
	}
	out := []SubFile{}
	out = append(out, files...)
	return map[string]any{
		"id":          sub.ID,
		"attempt":     sub.Attempt,
		"submittedAt": sub.SubmittedAt,
		"isLate":      sub.IsLate,
		"capPercent":  sub.CapPercent,
		"files":       out,
	}, nil
}

// handleAdminSubAssignmentSubmissions lists the roster with each student's
// latest submission and summed results from the latest run per test.
func (s *Server) handleAdminSubAssignmentSubmissions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	a := s.adminAssignment(w, r)
	if a == nil {
		return
	}
	roster, err := s.store.ListStudentsForCourse(a.CourseYearID, a.TermID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list students"})
		return
	}
	latest, err := s.store.LatestSubmissionsByAssignment(a.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list submissions"})
		return
	}

	students := []map[string]any{}
	for _, st := range roster {
		entry := map[string]any{
			"studentId":        st.StudentID,
			"username":         st.Username,
			"firstName":        st.FirstName,
			"lastName":         st.LastName,
			"latestSubmission": nil,
			"publicPassed":     0,
			"publicFailed":     0,
			"secretPassed":     0,
			"secretFailed":     0,
			"untested":         false,
			"running":          false,
		}
		sub := latest[st.StudentID]
		if sub != nil {
			subJSON, err := s.submissionJSON(sub)
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list files"})
				return
			}
			entry["latestSubmission"] = subJSON
			runs, err := s.store.LatestRunsForSubmission(sub.ID, "")
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list runs"})
				return
			}
			entry["untested"] = len(runs) == 0
			running := false
			for _, run := range runs {
				if run.Status == "queued" || run.Status == "running" {
					running = true
				}
				if run.Visibility == "secret" {
					entry["secretPassed"] = entry["secretPassed"].(int) + run.Passed
					entry["secretFailed"] = entry["secretFailed"].(int) + run.Failed
				} else {
					entry["publicPassed"] = entry["publicPassed"].(int) + run.Passed
					entry["publicFailed"] = entry["publicFailed"].(int) + run.Failed
				}
			}
			entry["running"] = running
		}
		students = append(students, entry)
	}
	writeJSON(w, http.StatusOK, map[string]any{"students": students})
}

// handleAdminSubRun queues all tests (public and secret) against the latest
// submission of every student (or one, with ?student=<pk>).
func (s *Server) handleAdminSubRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	a := s.adminAssignment(w, r)
	if a == nil {
		return
	}
	tests, err := s.store.ListSubTests(a.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list tests"})
		return
	}
	if len(tests) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no tests configured"})
		return
	}

	targets := []int{}
	if student := r.URL.Query().Get("student"); student != "" {
		pk, err := strconv.Atoi(student)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid student"})
			return
		}
		targets = []int{pk}
	} else {
		roster, err := s.store.ListStudentsForCourse(a.CourseYearID, a.TermID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list students"})
			return
		}
		for _, st := range roster {
			targets = append(targets, st.StudentID)
		}
	}

	queued := 0
	for _, pk := range targets {
		sub, err := s.store.LatestSubSubmission(a.ID, pk)
		if err != nil || sub == nil {
			continue
		}
		for _, test := range tests {
			if s.queueTestRun(sub.ID, test, "admin") {
				queued++
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"queued": queued})
}

// queueTestRun creates a queued run row and enqueues it; a full queue marks
// the row as an error instead of blocking.
func (s *Server) queueTestRun(submissionID int64, test SubTest, triggeredBy string) bool {
	runID, err := s.store.CreateSubTestRun(&SubTestRun{
		SubmissionID: submissionID,
		TestID:       test.ID,
		Visibility:   test.Visibility,
		TriggeredBy:  triggeredBy,
	})
	if err != nil {
		return false
	}
	if !s.grader.enqueueTest(runID) {
		_ = s.store.FinishTestRun(runID, "error", 0, 0, "grading queue is full")
		return false
	}
	return true
}

// handleAdminSubSubmissionDetail returns a submission with files and runs,
// including secret test output.
func (s *Server) handleAdminSubSubmissionDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid submission id"})
		return
	}
	sub, err := s.store.GetSubSubmission(id)
	if err != nil || sub == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "submission not found"})
		return
	}
	subJSON, err := s.submissionJSON(sub)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list files"})
		return
	}
	subJSON["assignmentId"] = sub.AssignmentID
	subJSON["studentPk"] = sub.StudentPK
	runs, err := s.store.RunsForSubmission(sub.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list runs"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"submission": subJSON, "files": subJSON["files"], "runs": runs})
}

// handleAdminSubSubmissionFile serves one uploaded file of a submission.
func (s *Server) handleAdminSubSubmissionFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid submission id"})
		return
	}
	sub, err := s.store.GetSubSubmission(id)
	if err != nil || sub == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "submission not found"})
		return
	}
	name, err := sanitizeFilename(r.PathValue("name"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	path := filepath.Join(submissionDir(s.config.SubmissionsDir, sub.AssignmentID, sub.StudentPK, sub.Attempt), name)
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	http.ServeFile(w, r, path)
}

// handleAdminSubAssignmentDownload streams a zip of every enrolled student's
// latest submission, one folder per student.
func (s *Server) handleAdminSubAssignmentDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	a := s.adminAssignment(w, r)
	if a == nil {
		return
	}
	roster, err := s.store.ListStudentsForCourse(a.CourseYearID, a.TermID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list students"})
		return
	}
	latest, err := s.store.LatestSubmissionsByAssignment(a.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list submissions"})
		return
	}

	zipName := zipSafeName(a.Title)
	if zipName == "" {
		zipName = fmt.Sprintf("assignment-%d", a.ID)
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", zipName+"-submissions.zip"))

	zw := zip.NewWriter(w)
	usedFolders := map[string]bool{}
	for _, st := range roster {
		sub := latest[st.StudentID]
		if sub == nil {
			continue
		}
		folder := zipSafeName(strings.TrimSpace(st.FirstName + " " + st.LastName))
		if folder == "" {
			folder = st.Username
		}
		if folder == "" {
			folder = fmt.Sprintf("student_%d", st.StudentID)
		}
		if usedFolders[folder] {
			folder = fmt.Sprintf("%s_%s", folder, st.Username)
		}
		usedFolders[folder] = true

		files, err := s.store.ListSubFiles(sub.ID)
		if err != nil {
			log.Printf("submissions download: list files for submission %d: %v", sub.ID, err)
			continue
		}
		dir := submissionDir(s.config.SubmissionsDir, sub.AssignmentID, sub.StudentPK, sub.Attempt)
		for _, f := range files {
			path := filepath.Join(dir, f.Filename)
			src, err := os.Open(path)
			if err != nil {
				log.Printf("submissions download: open %s: %v", path, err)
				continue
			}
			dst, err := zw.Create(folder + "/" + f.Filename)
			if err == nil {
				_, err = io.Copy(dst, src)
			}
			src.Close()
			if err != nil {
				log.Printf("submissions download: zip %s: %v", f.Filename, err)
			}
		}
	}
	if err := zw.Close(); err != nil {
		log.Printf("submissions download: close zip: %v", err)
	}
}

// zipSafeName turns a display string into a safe zip entry / file name,
// replacing path separators and control characters with underscores.
func zipSafeName(name string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '/' || r == '\\' || r < 32:
			return '_'
		}
		return r
	}, strings.TrimSpace(name))
}

// handleStudentSubmissionFile serves one recorded file of the student's own
// submission.
func (s *Server) handleStudentSubmissionFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	claims, err := s.readToken(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid submission id"})
		return
	}
	sub, err := s.store.GetSubSubmission(id)
	if err != nil || sub == nil || sub.StudentPK != claims.StudentID {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "submission not found"})
		return
	}
	name, err := sanitizeFilename(r.PathValue("name"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	files, err := s.store.ListSubFiles(sub.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list files"})
		return
	}
	recorded := false
	for _, f := range files {
		if f.Filename == name {
			recorded = true
			break
		}
	}
	if !recorded {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	path := filepath.Join(submissionDir(s.config.SubmissionsDir, sub.AssignmentID, sub.StudentPK, sub.Attempt), name)
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	http.ServeFile(w, r, path)
}

// handleAdminSubPlagiarism queues (POST) or reports (GET) a JPlag run.
func (s *Server) handleAdminSubPlagiarism(w http.ResponseWriter, r *http.Request) {
	a := s.adminAssignment(w, r)
	if a == nil {
		return
	}

	switch r.Method {
	case http.MethodPost:
		if a.Language == "files" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "plagiarism detection is not available for generic file submissions"})
			return
		}
		latest, err := s.store.LatestSubmissionsByAssignment(a.ID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list submissions"})
			return
		}
		if len(latest) < 2 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "need at least two students with submissions"})
			return
		}
		runID, err := s.store.CreateSubPlagRun(a.ID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create plagiarism run"})
			return
		}
		if !s.grader.enqueuePlag(runID) {
			_ = s.store.SetSubPlagRunStatus(runID, "error", "", "grading queue is full")
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "grading queue is full"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"runId": runID})
	case http.MethodGet:
		run, err := s.store.LatestSubPlagRun(a.ID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load plagiarism run"})
			return
		}
		if run == nil {
			writeJSON(w, http.StatusOK, map[string]any{"run": nil, "pairs": []any{}})
			return
		}
		names := map[int]string{}
		if roster, err := s.store.ListStudentsForCourse(a.CourseYearID, a.TermID); err == nil {
			for _, st := range roster {
				name := strings.TrimSpace(st.FirstName + " " + st.LastName)
				if name == "" {
					name = st.Username
				}
				names[st.StudentID] = name
			}
		}
		pairs, err := s.store.ListSubPlagPairs(run.ID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list pairs"})
			return
		}
		out := []map[string]any{}
		for _, p := range pairs {
			out = append(out, map[string]any{
				"studentA":   p.StudentA,
				"studentB":   p.StudentB,
				"nameA":      names[p.StudentA],
				"nameB":      names[p.StudentB],
				"similarity": p.Similarity,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"run": run, "pairs": out})
	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

// handleAdminSubPlagReport serves the report zip of the latest finished
// plagiarism run. The file opens in the JPlag report viewer
// (https://jplag.github.io/JPlag/).
func (s *Server) handleAdminSubPlagReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	a := s.adminAssignment(w, r)
	if a == nil {
		return
	}
	run, err := s.store.LatestSubPlagRun(a.ID)
	if err != nil || run == nil || run.Status != "done" || run.ReportPath == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no finished plagiarism report"})
		return
	}
	// Only serve a report artifact inside the plag directory. JPlag 5 runs
	// wrote a .zip file under a stored .jplag path; fall back to the .zip
	// sibling so those reports stay downloadable.
	wantDir := filepath.Join(s.config.SubmissionsDir, "plag")
	path := run.ReportPath
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		path = strings.TrimSuffix(path, ".jplag") + ".zip"
	}
	ext := filepath.Ext(path)
	if filepath.Dir(path) != wantDir || (ext != ".jplag" && ext != ".zip") {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no finished plagiarism report"})
		return
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "report file no longer exists on the server"})
		return
	}
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=%q", fmt.Sprintf("plag-report-assignment-%d-run-%d%s", a.ID, run.ID, ext)))
	http.ServeFile(w, r, path)
}

// handleAdminSubQueue reports the grading queue depth.
func (s *Server) handleAdminSubQueue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	depth, running := s.grader.queueDepth()
	writeJSON(w, http.StatusOK, map[string]any{"depth": depth, "running": running})
}

// studentAssignment resolves the {id} path value to an assignment the student
// is enrolled in.
func (s *Server) studentAssignment(w http.ResponseWriter, r *http.Request, studentID int) *SubAssignment {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid assignment id"})
		return nil
	}
	a, err := s.store.GetSubAssignment(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load assignment"})
		return nil
	}
	if a == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "assignment not found"})
		return nil
	}
	ok, err := s.enrolled(studentID, a.CourseYearID, a.TermID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read courses"})
		return nil
	}
	if !ok {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "not enrolled in this course"})
		return nil
	}
	return a
}

// handleStudentSubmissions lists submission-assignments of every enrolled course.
func (s *Server) handleStudentSubmissions(w http.ResponseWriter, r *http.Request) {
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
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read courses"})
		return
	}

	courses := []map[string]any{}
	for _, snap := range snapshots {
		assignments, err := s.store.ListSubAssignmentsForCourse(snap.CourseYearID, snap.TermID)
		if err != nil || len(assignments) == 0 {
			continue
		}
		out := []map[string]any{}
		for i := range assignments {
			a := &assignments[i]
			entry := studentAssignmentJSON(a)
			entry["latestSubmission"] = nil
			entry["publicPassed"] = 0
			entry["publicFailed"] = 0
			if n, err := s.store.CountPublicSubTests(a.ID); err == nil {
				entry["publicTestCount"] = n
			}
			if staged, _ := draftState(draftDir(s.config.SubmissionsDir, a.ID, claims.StudentID), a.ExpectedFilenames); len(staged) > 0 {
				entry["draftFiles"] = staged
			}
			entry["cooldownRemainingSeconds"] = s.cooldownRemaining(a.ID, claims.StudentID)
			latest, err := s.store.LatestSubSubmission(a.ID, claims.StudentID)
			if err == nil && latest != nil {
				entry["latestSubmission"] = map[string]any{
					"id":          latest.ID,
					"submittedAt": latest.SubmittedAt,
					"isLate":      latest.IsLate,
				}
				runs, err := s.store.LatestRunsForSubmission(latest.ID, "public")
				if err == nil {
					passed, failed := 0, 0
					for _, run := range runs {
						passed += run.Passed
						failed += run.Failed
					}
					entry["publicPassed"] = passed
					entry["publicFailed"] = failed
				}
			}
			out = append(out, entry)
		}
		courses = append(courses, map[string]any{
			"courseYearId":   snap.CourseYearID,
			"termId":         snap.TermID,
			"courseName":     snap.CourseName,
			"courseYearName": snap.CourseYearName,
			"termName":       snap.TermName,
			"assignments":    out,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"courses": courses})
}

// handleStudentSubmissionDetail returns the assignment, the student's attempt
// history, and the latest public run results. Secret tests are never included.
func (s *Server) handleStudentSubmissionDetail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	claims, err := s.readToken(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	a := s.studentAssignment(w, r, claims.StudentID)
	if a == nil {
		return
	}

	subs, err := s.store.ListSubSubmissions(a.ID, claims.StudentID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list submissions"})
		return
	}
	out := []map[string]any{}
	for i := range subs {
		subJSON, err := s.submissionJSON(&subs[i])
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list files"})
			return
		}
		out = append(out, subJSON)
	}

	latestRuns := []map[string]any{}
	if len(subs) > 0 {
		runs, err := s.store.LatestRunsForSubmission(subs[0].ID, "public")
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list runs"})
			return
		}
		for _, run := range runs {
			latestRuns = append(latestRuns, map[string]any{
				"testName":   run.TestName,
				"passed":     run.Passed,
				"failed":     run.Failed,
				"status":     run.Status,
				"output":     run.Output,
				"finishedAt": run.FinishedAt,
			})
		}
	}
	asgJSON := studentAssignmentJSON(a)
	if n, err := s.store.CountPublicSubTests(a.ID); err == nil {
		asgJSON["publicTestCount"] = n
	}
	draftFiles, draftMissing := draftState(draftDir(s.config.SubmissionsDir, a.ID, claims.StudentID), a.ExpectedFilenames)
	writeJSON(w, http.StatusOK, map[string]any{
		"assignment":       asgJSON,
		"draft":            map[string]any{"files": draftFiles, "missing": draftMissing},
		"submissions":      out,
		"latestPublicRuns": latestRuns,
	})
}

// handleStudentSubmissionUpload stores one attempt's files (all in one
// multipart request); it never queues tests.
func (s *Server) handleStudentSubmissionUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	claims, err := s.readToken(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	a := s.studentAssignment(w, r, claims.StudentID)
	if a == nil {
		return
	}
	if !a.IsOpen {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "assignment is closed"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, a.MaxTotalBytes+1<<20)
	reader, err := r.MultipartReader()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "multipart form with file parts required"})
		return
	}

	files := []uploadedFile{}
	confirmLate := false
	var total int64
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "upload too large or malformed"})
			return
		}
		if part.FormName() == "confirmLate" {
			data, _ := io.ReadAll(io.LimitReader(part, 64))
			confirmLate = strings.TrimSpace(string(data)) == "true"
			continue
		}
		if part.FormName() != "file" || part.FileName() == "" {
			continue
		}
		name, err := sanitizeFilename(part.FileName())
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		data, err := io.ReadAll(io.LimitReader(part, a.MaxFileBytes+1))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "failed to read upload"})
			return
		}
		if int64(len(data)) > a.MaxFileBytes {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("file %s exceeds %d bytes", name, a.MaxFileBytes)})
			return
		}
		total += int64(len(data))
		if total > a.MaxTotalBytes {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("files exceed the total limit of %d bytes", a.MaxTotalBytes)})
			return
		}
		files = append(files, uploadedFile{name: name, data: data})
	}
	if len(files) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no files in request"})
		return
	}
	if errMsg := validateUploadSet(a.ExpectedFilenames, files); errMsg != "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": errMsg})
		return
	}

	late := false
	if a.DueAt != nil {
		if due, err := time.Parse(time.RFC3339, *a.DueAt); err == nil && time.Now().After(due) {
			late = true
		}
	}
	if late && !confirmLate {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error": "late submission",
			"late":  true,
			"dueAt": a.DueAt,
		})
		return
	}

	capPercent := 100
	if late {
		capPercent = a.LateCapPercent
	}
	sub := &SubSubmission{
		AssignmentID: a.ID,
		StudentPK:    claims.StudentID,
		SubmittedAt:  time.Now().UTC().Format(time.RFC3339),
		IsLate:       late,
		CapPercent:   capPercent,
	}
	subFiles := []SubFile{}
	for _, f := range files {
		subFiles = append(subFiles, SubFile{Filename: f.name, ByteSize: int64(len(f.data))})
	}
	if _, err := s.store.CreateSubSubmission(sub, subFiles); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save submission"})
		return
	}

	dir := submissionDir(s.config.SubmissionsDir, a.ID, claims.StudentID, sub.Attempt)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		_ = s.store.DeleteSubSubmission(sub.ID)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to store files"})
		return
	}
	for _, f := range files {
		if err := storeUpload(dir, f.name, bytes.NewReader(f.data)); err != nil {
			_ = s.store.DeleteSubSubmission(sub.ID)
			_ = os.RemoveAll(dir)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to store files"})
			return
		}
	}

	subJSON, err := s.submissionJSON(sub)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save submission"})
		return
	}
	s.logActivity(claims.StudentID, claims.Username, activitySubmit, a.Title)
	writeJSON(w, http.StatusOK, map[string]any{"submission": subJSON})
}

// uploadedFile is one file part of a submission upload.
type uploadedFile struct {
	name string
	data []byte
}

// draftDir returns the student's staging directory for an assignment.
func draftDir(base string, assignmentID int64, studentPK int) string {
	return filepath.Join(assignmentDir(base, assignmentID), fmt.Sprintf("student_%d", studentPK), "draft")
}

// draftState lists staged files per expected slot and the slots still missing.
// File entries carry the slot display, slot key, actual stored name, and size.
func draftState(dir string, expected []string) (files []map[string]any, missing []string) {
	files = []map[string]any{}
	var entries []os.DirEntry
	if e, err := os.ReadDir(dir); err == nil {
		entries = e
	}
	for _, slot := range parseExpectedSlots(expected) {
		found := false
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			canonical, ok := slot.match(e.Name())
			if !ok {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			files = append(files, map[string]any{"slot": slot.display, "key": slot.key(), "name": canonical, "size": info.Size()})
			found = true
			break
		}
		if !found {
			missing = append(missing, slot.display)
		}
	}
	return files, missing
}

// draftFileFor returns the on-disk name of the staged file matching slot, if any.
func draftFileFor(dir string, slot expectedSlot) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if canonical, ok := slot.match(e.Name()); ok {
			return canonical
		}
	}
	return ""
}

// handleStudentSubmissionSlot stages (POST), un-stages (DELETE), or downloads
// (GET) one file of an assignment. Staged files live in a draft directory and
// become a submission attempt only when the student explicitly submits.
func (s *Server) handleStudentSubmissionSlot(w http.ResponseWriter, r *http.Request) {
	claims, err := s.readToken(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	a := s.studentAssignment(w, r, claims.StudentID)
	if a == nil {
		return
	}
	raw, err := sanitizeFilename(r.PathValue("name"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid file name"})
		return
	}
	slot := findSlot(parseExpectedSlots(a.ExpectedFilenames), raw)
	if slot == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("unexpected file %s", raw)})
		return
	}
	draft := draftDir(s.config.SubmissionsDir, a.ID, claims.StudentID)

	switch r.Method {
	case http.MethodGet:
		name := draftFileFor(draft, *slot)
		if name == "" {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "no staged file"})
			return
		}
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
		http.ServeFile(w, r, filepath.Join(draft, name))
	case http.MethodDelete:
		if name := draftFileFor(draft, *slot); name != "" {
			_ = os.Remove(filepath.Join(draft, name))
		}
		files, missing := draftState(draft, a.ExpectedFilenames)
		writeJSON(w, http.StatusOK, map[string]any{"draft": map[string]any{"files": files, "missing": missing}})
	case http.MethodPost:
		if !a.IsOpen {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "assignment is closed"})
			return
		}
		s.handleSlotUpload(w, r, a, slot, draft, claims)
	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

// handleSlotUpload stages one file into the draft. The picked file must match
// one of the slot's alternatives exactly (case-sensitive); it is stored under
// the matched alternative's canonical spelling.
func (s *Server) handleSlotUpload(w http.ResponseWriter, r *http.Request, a *SubAssignment, slot *expectedSlot, draft string, claims *PortalClaims) {
	r.Body = http.MaxBytesReader(w, r.Body, a.MaxFileBytes+1<<20)
	reader, err := r.MultipartReader()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "multipart form with a file part required"})
		return
	}

	var data []byte
	canonical := ""
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "upload too large or malformed"})
			return
		}
		if part.FormName() != "file" || part.FileName() == "" {
			continue
		}
		picked, err := sanitizeFilename(part.FileName())
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		matched, ok := slot.match(picked)
		if !ok {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("file must be named exactly %s (got %q) — names are case-sensitive, rename the file first", strings.Join(slot.names, " or "), picked)})
			return
		}
		canonical = matched
		data, err = io.ReadAll(io.LimitReader(part, a.MaxFileBytes+1))
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "failed to read upload"})
			return
		}
		if int64(len(data)) > a.MaxFileBytes {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("file %s exceeds %d bytes", canonical, a.MaxFileBytes)})
			return
		}
	}
	if data == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no file in request"})
		return
	}

	if err := os.MkdirAll(draft, 0o755); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to stage file"})
		return
	}
	// Remove any other alternative of this slot so the draft holds exactly one
	// file per slot.
	if entries, err := os.ReadDir(draft); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if match, ok := slot.match(e.Name()); ok && match != canonical {
				_ = os.Remove(filepath.Join(draft, e.Name()))
			}
		}
	}
	if err := storeUpload(draft, canonical, bytes.NewReader(data)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to stage file"})
		return
	}

	s.logActivity(claims.StudentID, claims.Username, activityUpload, fmt.Sprintf("%s: %s", a.Title, canonical))
	files, missing := draftState(draft, a.ExpectedFilenames)
	writeJSON(w, http.StatusOK, map[string]any{"draft": map[string]any{"files": files, "missing": missing}})
}

// handleStudentSubmissionSubmit finalizes the staged draft into a new
// submission attempt. The draft must hold the complete expected file set.
func (s *Server) handleStudentSubmissionSubmit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	claims, err := s.readToken(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	a := s.studentAssignment(w, r, claims.StudentID)
	if a == nil {
		return
	}
	if !a.IsOpen {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "assignment is closed"})
		return
	}
	confirmLate := r.URL.Query().Get("confirmLate") == "true"

	draft := draftDir(s.config.SubmissionsDir, a.ID, claims.StudentID)
	files, missing := draftState(draft, a.ExpectedFilenames)
	if len(files) == 0 {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "no staged files — upload at least one file first"})
		return
	}

	// Files not staged now may come from the previous submission: at least one
	// new staged file plus a previously submitted remainder is enough.
	latest, err := s.store.LatestSubSubmission(a.ID, claims.StudentID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load submission"})
		return
	}
	var prevFiles []SubFile
	if latest != nil {
		prevFiles, err = s.store.ListSubFiles(latest.ID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load submission"})
			return
		}
	}
	prevByName := map[string]SubFile{}
	for _, f := range prevFiles {
		prevByName[strings.ToLower(f.Filename)] = f
	}
	slots := parseExpectedSlots(a.ExpectedFilenames)
	if len(missing) > 0 {
		kept := missing[:0]
		for _, name := range missing {
			slot := findSlot(slots, name)
			covered := false
			if slot != nil {
				for _, alt := range slot.names {
					if _, ok := prevByName[strings.ToLower(alt)]; ok {
						covered = true
						break
					}
				}
			}
			if !covered {
				kept = append(kept, name)
			}
		}
		missing = kept
	}
	if len(missing) > 0 {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":   "incomplete submission",
			"missing": missing,
		})
		return
	}

	// The recorded attempt holds the merged set: staged files win, the rest
	// come from the previous submission.
	stagedBySlot := map[string]map[string]any{}
	for _, f := range files {
		stagedBySlot[strings.ToLower(f["key"].(string))] = f
	}
	type mergedFile struct {
		name     string
		size     int64
		fromDisk string // directory to read from (draft or previous attempt dir)
	}
	prevDir := ""
	if latest != nil {
		prevDir = submissionDir(s.config.SubmissionsDir, a.ID, claims.StudentID, latest.Attempt)
	}
	subFiles := []SubFile{}
	merged := []mergedFile{}
	var total int64
	for _, slot := range slots {
		if staged, ok := stagedBySlot[strings.ToLower(slot.key())]; ok {
			name := staged["name"].(string)
			size := staged["size"].(int64)
			subFiles = append(subFiles, SubFile{Filename: name, ByteSize: size})
			merged = append(merged, mergedFile{name: name, fromDisk: draft})
			total += size
			continue
		}
		for _, alt := range slot.names {
			if prev, ok := prevByName[strings.ToLower(alt)]; ok {
				name := prev.Filename
				subFiles = append(subFiles, SubFile{Filename: name, ByteSize: prev.ByteSize})
				merged = append(merged, mergedFile{name: name, fromDisk: prevDir})
				total += prev.ByteSize
				break
			}
		}
	}
	if total > a.MaxTotalBytes {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("files exceed the total limit of %d bytes", a.MaxTotalBytes)})
		return
	}

	late := false
	if a.DueAt != nil {
		if due, err := time.Parse(time.RFC3339, *a.DueAt); err == nil && time.Now().After(due) {
			late = true
		}
	}
	if late && !confirmLate {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error": "late submission",
			"late":  true,
			"dueAt": a.DueAt,
		})
		return
	}

	capPercent := 100
	if late {
		capPercent = a.LateCapPercent
	}
	sub := &SubSubmission{
		AssignmentID: a.ID,
		StudentPK:    claims.StudentID,
		SubmittedAt:  time.Now().UTC().Format(time.RFC3339),
		IsLate:       late,
		CapPercent:   capPercent,
	}
	if _, err := s.store.CreateSubSubmission(sub, subFiles); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save submission"})
		return
	}

	dir := submissionDir(s.config.SubmissionsDir, a.ID, claims.StudentID, sub.Attempt)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		_ = s.store.DeleteSubSubmission(sub.ID)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to store files"})
		return
	}
	for _, f := range merged {
		var err error
		if f.fromDisk == draft {
			err = os.Rename(filepath.Join(draft, f.name), filepath.Join(dir, f.name))
		} else {
			err = copyFile(filepath.Join(f.fromDisk, f.name), filepath.Join(dir, f.name))
		}
		if err != nil {
			_ = s.store.DeleteSubSubmission(sub.ID)
			_ = os.RemoveAll(dir)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to store files"})
			return
		}
	}
	_ = os.Remove(draft)

	subJSON, err := s.submissionJSON(sub)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save submission"})
		return
	}
	s.logActivity(claims.StudentID, claims.Username, activitySubmit, a.Title)
	writeJSON(w, http.StatusOK, map[string]any{"submission": subJSON})
}

// validateUploadSet requires exactly one file per expected slot, matching any
// of the slot's alternatives exactly (case-sensitive).
func validateUploadSet(expected []string, files []uploadedFile) string {
	slots := parseExpectedSlots(expected)
	matched := make([]bool, len(slots))
	for _, f := range files {
		ok := false
		for i, slot := range slots {
			if matched[i] {
				continue
			}
			if _, yes := slot.match(f.name); yes {
				matched[i] = true
				ok = true
				break
			}
		}
		if !ok {
			// A case-only mismatch gets a targeted message so the student knows
			// to rename the file rather than re-upload the same thing.
			for _, slot := range slots {
				for _, n := range slot.names {
					if strings.EqualFold(n, f.name) {
						return fmt.Sprintf("file %s must be named exactly %s (names are case-sensitive)", f.name, n)
					}
				}
			}
			return fmt.Sprintf("unexpected file %s", f.name)
		}
	}
	missing := []string{}
	for i, slot := range slots {
		if !matched[i] {
			missing = append(missing, slot.display)
		}
	}
	if len(missing) > 0 {
		return fmt.Sprintf("missing files: %s", strings.Join(missing, ", "))
	}
	return ""
}

// handleStudentSubmissionTest queues public tests against the student's
// latest submission, enforcing a 30 s cooldown.
func (s *Server) handleStudentSubmissionTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	claims, err := s.readToken(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	a := s.studentAssignment(w, r, claims.StudentID)
	if a == nil {
		return
	}

	latest, err := s.store.LatestSubSubmission(a.ID, claims.StudentID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load submission"})
		return
	}
	if latest == nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "no files uploaded yet"})
		return
	}

	if retryAfter := s.cooldownRemaining(a.ID, claims.StudentID); retryAfter > 0 {
		writeJSON(w, http.StatusTooManyRequests, map[string]any{
			"error":      "please wait before testing again",
			"retryAfter": retryAfter,
		})
		return
	}

	tests, err := s.store.ListSubTests(a.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list tests"})
		return
	}
	queued := 0
	for _, test := range tests {
		if test.Visibility != "public" {
			continue
		}
		if s.queueTestRun(latest.ID, test, "student") {
			queued++
		}
	}
	if queued == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no public tests configured"})
		return
	}
	s.cooldowns.record(a.ID, claims.StudentID)
	writeJSON(w, http.StatusOK, map[string]any{"queued": queued})
}
