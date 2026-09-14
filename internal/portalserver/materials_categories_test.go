package portalserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

// postJSON sends a JSON POST/DELETE to the admin API and returns the recorder.
func adminJSON(t *testing.T, handler http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-teacher-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// uploadMaterialsBulk posts several files in one multipart request.
func uploadMaterialsBulk(t *testing.T, handler http.Handler, category string, files map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	for name, content := range files {
		part, err := w.CreateFormFile("file", name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("/api/admin/materials/upload?courseYearId=1&termId=1&category=%s", category)
	req := httptest.NewRequest(http.MethodPost, path, &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer test-teacher-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// studentMaterials returns the decoded /api/materials response for cookies.
func studentMaterials(t *testing.T, handler http.Handler, cookies []*http.Cookie) struct {
	Courses []CourseMaterials `json:"courses"`
} {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/materials", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("materials list: %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Courses []CourseMaterials `json:"courses"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	return resp
}

func adminMaterials(t *testing.T, handler http.Handler) (files []MaterialFile, categories []MaterialCategory) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/materials?courseYearId=1&termId=1", nil)
	req.Header.Set("Authorization", "Bearer test-teacher-token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin materials: %d %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Files      []MaterialFile     `json:"files"`
		Categories []MaterialCategory `json:"categories"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	return resp.Files, resp.Categories
}

func TestMaterialCategoriesFlow(t *testing.T) {
	_, handler, cookies := newMaterialsTestServer(t, "")

	// Create two categories.
	rec := adminJSON(t, handler, http.MethodPost, "/api/admin/materials/categories?courseYearId=1&termId=1",
		map[string]string{"name": "Unit 1"})
	if rec.Code != http.StatusOK {
		t.Fatalf("create category: %d %s", rec.Code, rec.Body.String())
	}
	var cat MaterialCategory
	if err := json.Unmarshal(rec.Body.Bytes(), &cat); err != nil {
		t.Fatal(err)
	}
	if cat.ID != "unit-1" || cat.Name != "Unit 1" {
		t.Fatalf("unexpected category: %+v", cat)
	}
	rec = adminJSON(t, handler, http.MethodPost, "/api/admin/materials/categories?courseYearId=1&termId=1",
		map[string]string{"name": "Unit 2"})
	if rec.Code != http.StatusOK {
		t.Fatalf("create category 2: %d %s", rec.Code, rec.Body.String())
	}

	// Bulk upload two files into unit-1 and one into the course root.
	rec = uploadMaterialsBulk(t, handler, "unit-1", map[string]string{"a.pdf": "aaa", "b.pdf": "bbb"})
	if rec.Code != http.StatusOK {
		t.Fatalf("bulk upload: %d %s", rec.Code, rec.Body.String())
	}
	var upResp struct {
		Uploaded []string            `json:"uploaded"`
		Failed   []map[string]string `json:"failed"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &upResp); err != nil {
		t.Fatal(err)
	}
	if len(upResp.Uploaded) != 2 || len(upResp.Failed) != 0 {
		t.Fatalf("unexpected upload result: %+v", upResp)
	}
	rec = uploadMaterialsBulk(t, handler, "", map[string]string{"syllabus.pdf": "root"})
	if rec.Code != http.StatusOK {
		t.Fatalf("root upload: %d %s", rec.Code, rec.Body.String())
	}

	// Admin sees both categories (in creation order) and the root file.
	files, categories := adminMaterials(t, handler)
	if len(files) != 1 || files[0].Name != "syllabus.pdf" {
		t.Fatalf("unexpected root files: %+v", files)
	}
	if len(categories) != 2 || categories[0].ID != "unit-1" || categories[1].ID != "unit-2" {
		t.Fatalf("unexpected categories: %+v", categories)
	}
	if len(categories[0].Files) != 2 || len(categories[1].Files) != 0 {
		t.Fatalf("unexpected category files: %+v", categories)
	}

	// Student sees the root file and only the non-empty category.
	resp := studentMaterials(t, handler, cookies)
	if len(resp.Courses) != 1 {
		t.Fatalf("unexpected courses: %+v", resp.Courses)
	}
	if len(resp.Courses[0].Files) != 1 || len(resp.Courses[0].Categories) != 1 {
		t.Fatalf("unexpected student view: %+v", resp.Courses[0])
	}

	// Rename the category: display name changes, files stay put.
	rec = adminJSON(t, handler, http.MethodPost, "/api/admin/materials/categories/rename?courseYearId=1&termId=1",
		map[string]string{"id": "unit-1", "name": "Week 1"})
	if rec.Code != http.StatusOK {
		t.Fatalf("rename category: %d %s", rec.Code, rec.Body.String())
	}
	_, categories = adminMaterials(t, handler)
	if categories[0].Name != "Week 1" || categories[0].ID != "unit-1" || len(categories[0].Files) != 2 {
		t.Fatalf("rename lost files or id: %+v", categories[0])
	}

	// Reorder: unit-2 first.
	rec = adminJSON(t, handler, http.MethodPost, "/api/admin/materials/categories/reorder?courseYearId=1&termId=1",
		map[string][]string{"ids": {"unit-2", "unit-1"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("reorder: %d %s", rec.Code, rec.Body.String())
	}
	_, categories = adminMaterials(t, handler)
	if categories[0].ID != "unit-2" || categories[1].ID != "unit-1" {
		t.Fatalf("order not persisted: %+v", categories)
	}

	// Move the root file into unit-1, then rename it there.
	rec = adminJSON(t, handler, http.MethodPost, "/api/admin/materials/move?courseYearId=1&termId=1",
		map[string]string{"fromCategory": "", "toCategory": "unit-1", "file": "syllabus.pdf"})
	if rec.Code != http.StatusOK {
		t.Fatalf("move: %d %s", rec.Code, rec.Body.String())
	}
	files, categories = adminMaterials(t, handler)
	if len(files) != 0 || len(categories[1].Files) != 3 {
		t.Fatalf("move failed: files=%+v categories=%+v", files, categories)
	}
	rec = adminJSON(t, handler, http.MethodPost, "/api/admin/materials/rename?courseYearId=1&termId=1",
		map[string]string{"category": "unit-1", "file": "syllabus.pdf", "newName": "syllabus-v2.pdf"})
	if rec.Code != http.StatusOK {
		t.Fatalf("rename file: %d %s", rec.Code, rec.Body.String())
	}

	// Renaming to an existing name conflicts.
	rec = adminJSON(t, handler, http.MethodPost, "/api/admin/materials/rename?courseYearId=1&termId=1",
		map[string]string{"category": "unit-1", "file": "a.pdf", "newName": "b.pdf"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("rename onto existing: got %d, want 409", rec.Code)
	}

	// Download the renamed file from its category, as the student.
	req := httptest.NewRequest(http.MethodGet, "/api/materials/download/1/1/unit-1/syllabus-v2.pdf", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	dlRec := httptest.NewRecorder()
	handler.ServeHTTP(dlRec, req)
	if dlRec.Code != http.StatusOK || dlRec.Body.String() != "root" {
		t.Fatalf("categorized download: %d %q", dlRec.Code, dlRec.Body.String())
	}
	// The old uncategorized path no longer resolves it.
	req = httptest.NewRequest(http.MethodGet, "/api/materials/download/1/1/syllabus-v2.pdf", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	dlRec = httptest.NewRecorder()
	handler.ServeHTTP(dlRec, req)
	if dlRec.Code != http.StatusNotFound {
		t.Fatalf("moved file at old path: got %d, want 404", dlRec.Code)
	}

	// Non-empty category refuses to delete; empty one deletes fine.
	rec = adminJSON(t, handler, http.MethodDelete, "/api/admin/materials/categories?courseYearId=1&termId=1&id=unit-1", nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("delete non-empty: got %d, want 400", rec.Code)
	}
	rec = adminJSON(t, handler, http.MethodDelete, "/api/admin/materials/categories?courseYearId=1&termId=1&id=unit-2", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete empty: %d %s", rec.Code, rec.Body.String())
	}
	_, categories = adminMaterials(t, handler)
	if len(categories) != 1 || categories[0].ID != "unit-1" {
		t.Fatalf("after delete: %+v", categories)
	}
}
