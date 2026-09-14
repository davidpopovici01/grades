package portalserver

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// maxUploadBytes caps a single material upload.
const maxUploadBytes = 100 << 20 // 100 MB

// uploadTempPrefix marks in-progress uploads inside a course directory.
const uploadTempPrefix = ".upload-"

// courseMetaFile stores per-course category metadata (names and order).
const courseMetaFile = "_meta.json"

// MaterialFile describes one downloadable document.
type MaterialFile struct {
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	Modified string `json:"modified"`
}

// MaterialCategory groups files under a teacher-chosen heading.
type MaterialCategory struct {
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Files []MaterialFile `json:"files"`
}

// CourseMaterials bundles a course with its uploaded files.
type CourseMaterials struct {
	CourseYearID   int                `json:"courseYearId"`
	TermID         int                `json:"termId"`
	CourseName     string             `json:"courseName"`
	CourseYearName string             `json:"courseYearName"`
	TermName       string             `json:"termName"`
	Files          []MaterialFile     `json:"files"`      // uncategorized, course root
	Categories     []MaterialCategory `json:"categories"` // ordered by the teacher
}

// courseMeta is the on-disk _meta.json contents.
type courseMeta struct {
	Categories []categoryMeta `json:"categories"`
}

type categoryMeta struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// materialsDir returns the storage directory for a course's materials.
func (s *Server) materialsDir(courseYearID, termID int, courseName string) string {
	return filepath.Join(s.config.MaterialsDir,
		fmt.Sprintf("%d-%d-%s", courseYearID, termID, slugify(courseName)))
}

// slugify converts a name into a filesystem-safe fragment.
func slugify(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	lastDash := true // trim leading dashes
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case !lastDash:
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.TrimRight(b.String(), "-")
	if out == "" {
		out = "course"
	}
	return out
}

// sanitizeFilename strips path components and rejects unsafe names.
func sanitizeFilename(name string) (string, error) {
	name = filepath.Base(strings.ReplaceAll(strings.TrimSpace(name), "\\", "/"))
	if name == "" || name == "." || name == ".." ||
		strings.HasPrefix(name, uploadTempPrefix) || name == courseMetaFile {
		return "", fmt.Errorf("invalid file name")
	}
	return name, nil
}

// sanitizeCategoryName validates a teacher-chosen category display name.
func sanitizeCategoryName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 100 || strings.ContainsAny(name, "/\\\x00") {
		return "", fmt.Errorf("invalid category name")
	}
	return name, nil
}

// validCategoryID reports whether id is a safe category directory name.
func validCategoryID(id string) bool {
	if id == "" || id != slugify(id) || id == "course" {
		return false
	}
	return true
}

// listMaterialFiles returns the regular files in dir, or nil when the
// directory does not exist (no materials uploaded yet).
func listMaterialFiles(dir string) ([]MaterialFile, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	files := []MaterialFile{}
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), uploadTempPrefix) || e.Name() == courseMetaFile {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, MaterialFile{
			Name:     e.Name(),
			Size:     info.Size(),
			Modified: info.ModTime().UTC().Format(time.RFC3339),
		})
	}
	return files, nil
}

// readCourseMeta loads _meta.json from dir, synthesizing it from on-disk
// category directories when absent and appending unknown directories at the
// end, so manually copied files always show up.
func readCourseMeta(dir string) (courseMeta, error) {
	var meta courseMeta
	if data, err := os.ReadFile(filepath.Join(dir, courseMetaFile)); err == nil {
		if err := json.Unmarshal(data, &meta); err != nil {
			return courseMeta{}, fmt.Errorf("corrupt %s: %w", courseMetaFile, err)
		}
	}

	known := map[string]bool{}
	for _, c := range meta.Categories {
		known[c.ID] = true
	}
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return meta, nil
	}
	if err != nil {
		return meta, err
	}
	for _, e := range entries {
		if e.IsDir() && !known[e.Name()] && validCategoryID(e.Name()) {
			meta.Categories = append(meta.Categories, categoryMeta{ID: e.Name(), Name: e.Name()})
			known[e.Name()] = true
		}
	}
	return meta, nil
}

// writeCourseMeta atomically persists _meta.json in dir.
func writeCourseMeta(dir string, meta courseMeta) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, uploadTempPrefix+"meta-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, courseMetaFile))
}

// listCourseMaterials assembles the full ordered structure of a course.
func listCourseMaterials(dir string) (files []MaterialFile, categories []MaterialCategory, err error) {
	files, err = listMaterialFiles(dir)
	if err != nil {
		return nil, nil, err
	}
	meta, err := readCourseMeta(dir)
	if err != nil {
		return nil, nil, err
	}
	categories = []MaterialCategory{}
	for _, c := range meta.Categories {
		catFiles, err := listMaterialFiles(filepath.Join(dir, c.ID))
		if err != nil {
			return nil, nil, err
		}
		categories = append(categories, MaterialCategory{ID: c.ID, Name: c.Name, Files: catFiles})
	}
	return files, categories, nil
}

// handleMaterials lists materials for every course the student is enrolled in.
func (s *Server) handleMaterials(w http.ResponseWriter, r *http.Request) {
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

	courses := []CourseMaterials{}
	for _, snap := range snapshots {
		files, categories, err := listCourseMaterials(s.materialsDir(snap.CourseYearID, snap.TermID, snap.CourseName))
		if err != nil {
			continue
		}
		// Hide empty categories from students; skip courses with nothing at all.
		nonEmpty := []MaterialCategory{}
		for _, c := range categories {
			if len(c.Files) > 0 {
				nonEmpty = append(nonEmpty, c)
			}
		}
		if len(files) == 0 && len(nonEmpty) == 0 {
			continue
		}
		courses = append(courses, CourseMaterials{
			CourseYearID:   snap.CourseYearID,
			TermID:         snap.TermID,
			CourseName:     snap.CourseName,
			CourseYearName: snap.CourseYearName,
			TermName:       snap.TermName,
			Files:          files,
			Categories:     nonEmpty,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"courses": courses})
}

// handleMaterialDownload serves a single material to an enrolled student.
// Paths:
//
//	/api/materials/download/{courseYearID}/{termID}/{filename}
//	/api/materials/download/{courseYearID}/{termID}/{categoryID}/{filename}
func (s *Server) handleMaterialDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	claims, err := s.readToken(r)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}

	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/materials/download/"), "/")
	if len(parts) != 3 && len(parts) != 4 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	courseYearID, err1 := strconv.Atoi(parts[0])
	termID, err2 := strconv.Atoi(parts[1])
	categoryID := ""
	namePart := parts[2]
	if len(parts) == 4 {
		categoryID = parts[2]
		namePart = parts[3]
		if !validCategoryID(categoryID) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
	}
	name, err3 := sanitizeFilename(namePart)
	if err1 != nil || err2 != nil || err3 != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}

	snapshots, err := s.store.GetStudentSnapshots(claims.StudentID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read courses"})
		return
	}
	courseName := ""
	for _, snap := range snapshots {
		if snap.CourseYearID == courseYearID && snap.TermID == termID {
			courseName = snap.CourseName
			break
		}
	}
	if courseName == "" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "not enrolled in this course"})
		return
	}

	path := filepath.Join(s.materialsDir(courseYearID, termID, courseName), name)
	if categoryID != "" {
		path = filepath.Join(s.materialsDir(courseYearID, termID, courseName), categoryID, name)
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	s.logActivity(claims.StudentID, claims.Username, activityDownload, name)
	http.ServeFile(w, r, path)
}

// adminCourse resolves the course query params to a published course.
func (s *Server) adminCourse(w http.ResponseWriter, r *http.Request) *CourseInfo {
	courseYearID, err1 := strconv.Atoi(r.URL.Query().Get("courseYearId"))
	termID, err2 := strconv.Atoi(r.URL.Query().Get("termId"))
	if err1 != nil || err2 != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "courseYearId and termId are required"})
		return nil
	}
	course, err := s.store.GetCourse(courseYearID, termID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to load course"})
		return nil
	}
	if course == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "course not published"})
		return nil
	}
	return course
}

// adminTargetDir resolves the destination directory for a category query
// param ("" = uncategorized course root), creating it when missing.
func (s *Server) adminTargetDir(w http.ResponseWriter, r *http.Request, course *CourseInfo) string {
	dir := s.materialsDir(course.CourseYearID, course.TermID, course.CourseName)
	categoryID := r.URL.Query().Get("category")
	if categoryID != "" {
		if !validCategoryID(categoryID) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid category"})
			return ""
		}
		dir = filepath.Join(dir, categoryID)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create directory"})
		return ""
	}
	return dir
}

// handleAdminMaterials lists the full ordered structure of a course.
func (s *Server) handleAdminMaterials(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	course := s.adminCourse(w, r)
	if course == nil {
		return
	}
	files, categories, err := listCourseMaterials(s.materialsDir(course.CourseYearID, course.TermID, course.CourseName))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list materials"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"files": files, "categories": categories})
}

// handleAdminMaterialUpload streams uploaded files (multiple "file" parts)
// into the target directory via temp files and atomic renames.
func (s *Server) handleAdminMaterialUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	course := s.adminCourse(w, r)
	if course == nil {
		return
	}
	dir := s.adminTargetDir(w, r, course)
	if dir == "" {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes*10+1<<20)
	reader, err := r.MultipartReader()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "multipart form with file parts required"})
		return
	}

	uploaded := []string{}
	failed := []map[string]string{}
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		if part.FormName() != "file" || part.FileName() == "" {
			continue
		}
		name, err := sanitizeFilename(part.FileName())
		if err != nil {
			failed = append(failed, map[string]string{"name": part.FileName(), "error": err.Error()})
			continue
		}
		if err := storeUpload(dir, name, part); err != nil {
			failed = append(failed, map[string]string{"name": name, "error": err.Error()})
			continue
		}
		uploaded = append(uploaded, name)
	}
	if len(uploaded) == 0 && len(failed) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no files in request"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"uploaded": uploaded, "failed": failed})
}

// storeUpload streams one part into dir/name, enforcing the per-file size cap.
func storeUpload(dir, name string, src io.Reader) error {
	tmp, err := os.CreateTemp(dir, uploadTempPrefix+"*")
	if err != nil {
		return fmt.Errorf("failed to store upload")
	}
	tmpName := tmp.Name()
	n, copyErr := io.Copy(tmp, io.LimitReader(src, maxUploadBytes+1))
	closeErr := tmp.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("failed to store upload")
	}
	if n > maxUploadBytes {
		_ = os.Remove(tmpName)
		return fmt.Errorf("file exceeds 100 MB")
	}
	if err := os.Rename(tmpName, filepath.Join(dir, name)); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("failed to store upload")
	}
	return nil
}

// handleAdminMaterialDelete removes one material, optionally from a category.
func (s *Server) handleAdminMaterialDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	course := s.adminCourse(w, r)
	if course == nil {
		return
	}
	name, err := sanitizeFilename(r.URL.Query().Get("file"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	dir := s.materialsDir(course.CourseYearID, course.TermID, course.CourseName)
	if categoryID := r.URL.Query().Get("category"); categoryID != "" {
		if !validCategoryID(categoryID) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid category"})
			return
		}
		dir = filepath.Join(dir, categoryID)
	}
	if err := os.Remove(filepath.Join(dir, name)); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// adminMetaRequest handles the shared boilerplate of the category-mutating
// endpoints: resolve the course, read the JSON body, and load the meta.
func (s *Server) adminMetaRequest(w http.ResponseWriter, r *http.Request, body any) (*CourseInfo, string, courseMeta, bool) {
	course := s.adminCourse(w, r)
	if course == nil {
		return nil, "", courseMeta{}, false
	}
	if err := json.NewDecoder(r.Body).Decode(body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return nil, "", courseMeta{}, false
	}
	dir := s.materialsDir(course.CourseYearID, course.TermID, course.CourseName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create course directory"})
		return nil, "", courseMeta{}, false
	}
	meta, err := readCourseMeta(dir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read categories"})
		return nil, "", courseMeta{}, false
	}
	return course, dir, meta, true
}

// handleAdminMaterialCategories dispatches category creation (POST) and
// deletion (DELETE).
func (s *Server) handleAdminMaterialCategories(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.handleAdminCategoryCreate(w, r)
	case http.MethodDelete:
		s.handleAdminCategoryDelete(w, r)
	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

// handleAdminCategoryCreate adds a category to a course.
func (s *Server) handleAdminCategoryCreate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	_, dir, meta, ok := s.adminMetaRequest(w, r, &body)
	if !ok {
		return
	}
	name, err := sanitizeCategoryName(body.Name)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	// Dedupe the slug id against existing categories and on-disk directories.
	id := slugify(name)
	taken := map[string]bool{}
	for _, c := range meta.Categories {
		taken[c.ID] = true
	}
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				taken[e.Name()] = true
			}
		}
	}
	base := id
	for i := 2; taken[id]; i++ {
		id = fmt.Sprintf("%s-%d", base, i)
	}

	meta.Categories = append(meta.Categories, categoryMeta{ID: id, Name: name})
	if err := writeCourseMeta(dir, meta); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save categories"})
		return
	}
	if err := os.MkdirAll(filepath.Join(dir, id), 0o755); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create category directory"})
		return
	}
	writeJSON(w, http.StatusOK, MaterialCategory{ID: id, Name: name, Files: []MaterialFile{}})
}

// handleAdminCategoryRename changes a category's display name; files on disk
// are untouched because the directory is keyed by the stable id.
func (s *Server) handleAdminCategoryRename(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	_, dir, meta, ok := s.adminMetaRequest(w, r, &body)
	if !ok {
		return
	}
	name, err := sanitizeCategoryName(body.Name)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	found := false
	for i := range meta.Categories {
		if meta.Categories[i].ID == body.ID {
			meta.Categories[i].Name = name
			found = true
			break
		}
	}
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "category not found"})
		return
	}
	if err := writeCourseMeta(dir, meta); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save categories"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleAdminCategoryReorder saves a new category order. Any existing
// categories missing from the submitted list are appended, never lost.
func (s *Server) handleAdminCategoryReorder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		IDs []string `json:"ids"`
	}
	_, dir, meta, ok := s.adminMetaRequest(w, r, &body)
	if !ok {
		return
	}
	byID := map[string]categoryMeta{}
	for _, c := range meta.Categories {
		byID[c.ID] = c
	}
	reordered := []categoryMeta{}
	used := map[string]bool{}
	for _, id := range body.IDs {
		if c, exists := byID[id]; exists && !used[id] {
			reordered = append(reordered, c)
			used[id] = true
		}
	}
	for _, c := range meta.Categories {
		if !used[c.ID] {
			reordered = append(reordered, c)
		}
	}
	meta.Categories = reordered
	if err := writeCourseMeta(dir, meta); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save categories"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleAdminCategoryDelete removes an empty category.
func (s *Server) handleAdminCategoryDelete(w http.ResponseWriter, r *http.Request) {
	course := s.adminCourse(w, r)
	if course == nil {
		return
	}
	id := r.URL.Query().Get("id")
	if !validCategoryID(id) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid category"})
		return
	}
	dir := s.materialsDir(course.CourseYearID, course.TermID, course.CourseName)

	files, err := listMaterialFiles(filepath.Join(dir, id))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read category"})
		return
	}
	if len(files) > 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "category is not empty; move or delete its files first"})
		return
	}

	meta, err := readCourseMeta(dir)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read categories"})
		return
	}
	kept := []categoryMeta{}
	found := false
	for _, c := range meta.Categories {
		if c.ID == id {
			found = true
			continue
		}
		kept = append(kept, c)
	}
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "category not found"})
		return
	}
	meta.Categories = kept
	if err := writeCourseMeta(dir, meta); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save categories"})
		return
	}
	// Best effort: the directory is empty of regular files by this point.
	_ = os.Remove(filepath.Join(dir, id))
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleAdminMaterialRename renames a file within its category.
func (s *Server) handleAdminMaterialRename(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Category string `json:"category"`
		File     string `json:"file"`
		NewName  string `json:"newName"`
	}
	course := s.adminCourse(w, r)
	if course == nil {
		return
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	oldName, err := sanitizeFilename(body.File)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	newName, err := sanitizeFilename(body.NewName)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	dir := s.materialsDir(course.CourseYearID, course.TermID, course.CourseName)
	if body.Category != "" {
		if !validCategoryID(body.Category) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid category"})
			return
		}
		dir = filepath.Join(dir, body.Category)
	}
	if _, err := os.Stat(filepath.Join(dir, oldName)); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if _, err := os.Stat(filepath.Join(dir, newName)); err == nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "a file with that name already exists"})
		return
	}
	if err := os.Rename(filepath.Join(dir, oldName), filepath.Join(dir, newName)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to rename"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleAdminMaterialMove moves a file between categories ("" = uncategorized).
func (s *Server) handleAdminMaterialMove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		FromCategory string `json:"fromCategory"`
		ToCategory   string `json:"toCategory"`
		File         string `json:"file"`
	}
	course := s.adminCourse(w, r)
	if course == nil {
		return
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	name, err := sanitizeFilename(body.File)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	for _, id := range []string{body.FromCategory, body.ToCategory} {
		if id != "" && !validCategoryID(id) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid category"})
			return
		}
	}
	if body.FromCategory == body.ToCategory {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "source and destination are the same"})
		return
	}

	base := s.materialsDir(course.CourseYearID, course.TermID, course.CourseName)
	srcDir, dstDir := base, base
	if body.FromCategory != "" {
		srcDir = filepath.Join(base, body.FromCategory)
	}
	if body.ToCategory != "" {
		dstDir = filepath.Join(base, body.ToCategory)
	}
	if _, err := os.Stat(filepath.Join(srcDir, name)); err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if _, err := os.Stat(filepath.Join(dstDir, name)); err == nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "a file with that name already exists there"})
		return
	}
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create category directory"})
		return
	}
	if err := os.Rename(filepath.Join(srcDir, name), filepath.Join(dstDir, name)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to move"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
