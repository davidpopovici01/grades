package app

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type categoryExportDocument struct {
	Rule     CategoryRule
	Document assignmentExportDocument
}

// ExportPendingCategories exports new or changed category totals and records only confirmed exports.
func (a *App) ExportPendingCategories() error {
	ctx := a.context()
	if ctx.TermID == 0 || ctx.CourseYearID == 0 {
		return errors.New("set year, term, and course first")
	}
	documents, err := a.buildCategoryExportDocuments(ctx.TermID, ctx.CourseYearID)
	if err != nil {
		return err
	}

	var pending []categoryExportDocument
	for _, item := range documents {
		needsExport, err := a.categoryNeedsExport(ctx.CourseYearID, ctx.TermID, item.Rule.CategoryID, item.Document.StableHash)
		if err != nil {
			return err
		}
		if needsExport {
			pending = append(pending, item)
		}
	}
	if len(pending) == 0 {
		fmt.Fprintln(a.out, "No unexported or modified categories.")
		return nil
	}

	for i, item := range pending {
		fmt.Fprintf(a.out, "[%d/%d] %s\n", i+1, len(pending), item.Rule.CategoryName)
		if err := a.exportCategory(ctx.CourseYearID, ctx.TermID, item); err != nil {
			if errors.Is(err, errExportNotConfirmed) {
				continue
			}
			return err
		}
	}
	return nil
}

// buildCategoryExportDocuments calculates category totals and renders stable export documents for the current context.
func (a *App) buildCategoryExportDocuments(termID, courseYearID int) ([]categoryExportDocument, error) {
	rules, err := a.categoryRulesForContext(courseYearID, termID)
	if err != nil {
		return nil, err
	}
	if len(rules) == 0 {
		return nil, nil
	}

	students, statuses, err := a.categoryExportRoster(termID, courseYearID)
	if err != nil {
		return nil, err
	}
	scores, err := a.categoryScoreValuesByStudent(students, rules)
	if err != nil {
		return nil, err
	}
	var courseName string
	if err := a.db.QueryRow(`
		SELECT courses.name
		FROM course_years
		JOIN courses ON courses.course_id = course_years.course_id
		WHERE course_years.course_year_id = ?`, courseYearID).Scan(&courseName); err != nil {
		return nil, err
	}

	documents := make([]categoryExportDocument, 0, len(rules))
	for _, rule := range rules {
		rows := make([]assignmentExportRow, 0, len(students))
		for _, student := range students {
			score := ""
			value := scores[student.ID][rule.CategoryID]
			if strings.EqualFold(statuses[student.ID], "active") && value.Included {
				score = strconv.FormatFloat(value.Percent, 'f', 2, 64)
			}
			rows = append(rows, assignmentExportRow{
				StudentID:      student.ID,
				PowerSchoolNum: student.PowerSchoolNum,
				StudentName:    formatPowerSchoolExportName(student.LastName, student.ChineseName, student.FirstName),
				Score:          score,
			})
		}
		meta := assignmentExportMeta{
			Title:      rule.CategoryName,
			Category:   rule.CategoryName,
			CourseName: courseName,
			DueDate:    time.Now().Format("2006-01-02"),
			MaxPoints:  100,
		}
		csvBytes, err := renderAssignmentExportCSV(meta, rows)
		if err != nil {
			return nil, err
		}
		documents = append(documents, categoryExportDocument{
			Rule: rule,
			Document: assignmentExportDocument{
				Meta:       meta,
				Rows:       rows,
				CSV:        csvBytes,
				StableHash: hashCategoryExport(rule, meta, rows),
			},
		})
	}
	return documents, nil
}

func (a *App) categoryExportRoster(termID, courseYearID int) ([]Student, map[int]string, error) {
	rows, err := a.db.Query(`
		SELECT DISTINCT students.student_pk,
		       students.first_name,
		       students.last_name,
		       COALESCE(students.chinese_name, ''),
		       COALESCE(students.school_student_id, ''),
		       COALESCE(students.powerschool_num, ''),
		       students.status
		FROM section_enrollments
		JOIN sections ON sections.section_id = section_enrollments.section_id
		JOIN students ON students.student_pk = section_enrollments.student_pk
		WHERE section_enrollments.term_id = ? AND sections.course_year_id = ?
		ORDER BY students.last_name, students.first_name`, termID, courseYearID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var students []Student
	statuses := map[int]string{}
	for rows.Next() {
		var student Student
		var status string
		if err := rows.Scan(&student.ID, &student.FirstName, &student.LastName, &student.ChineseName, &student.SchoolStudentID, &student.PowerSchoolNum, &status); err != nil {
			return nil, nil, err
		}
		students = append(students, student)
		statuses[student.ID] = status
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	return students, statuses, nil
}

func hashCategoryExport(rule CategoryRule, meta assignmentExportMeta, rows []assignmentExportRow) string {
	base := hashAssignmentExport(meta, rows)
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\n%t\n%.4f", base, rule.HasWeight, rule.WeightPercent)))
	return hex.EncodeToString(sum[:])
}

func (a *App) defaultCategoryExportPath(courseYearID, termID int, categoryName string) (string, error) {
	var courseYearName, termName string
	if err := a.db.QueryRow(`
		SELECT course_years.name, terms.name
		FROM course_years
		CROSS JOIN terms
		WHERE course_years.course_year_id = ? AND terms.term_id = ?`, courseYearID, termID).
		Scan(&courseYearName, &termName); err != nil {
		return "", err
	}
	exportDir := filepath.Clean(filepath.Join(
		a.homeDir, "..", "gradesCategoryExports",
		sanitizeFilename(courseYearName), sanitizeFilename(termName),
	))
	filename := sanitizeFilename(categoryName) + ".csv"
	return filepath.Join(exportDir, filename), nil
}

func (a *App) exportCategory(courseYearID, termID int, item categoryExportDocument) error {
	document := item.Document
	fmt.Fprintf(a.out, "Assignment Name:\t%s\n", document.Meta.Title)
	fmt.Fprintf(a.out, "Category:\t%s\n", document.Meta.Category)
	weight := "unweighted"
	if item.Rule.HasWeight {
		weight = fmt.Sprintf("%.1f%%", item.Rule.WeightPercent)
	}
	fmt.Fprintf(a.out, "Weight of Total Grade:\t%s\n", weight)
	fmt.Fprintln(a.out, "Max Points:\t100.0")

	file, err := a.defaultCategoryExportPath(courseYearID, termID, document.Meta.Title)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(file, document.CSV, 0o644); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Exported category totals to %s\n", file)

	previousRows, err := a.loadCategoryExportRows(courseYearID, termID, item.Rule.CategoryID)
	if err != nil {
		return err
	}
	a.printExportDiff(document.Rows, previousRows)

	ok, err := a.promptYesNo("Was the export successful?", false)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(a.out, colorOrange("Export not confirmed. This category still needs export."))
		return errExportNotConfirmed
	}
	if err := a.recordCategoryExport(courseYearID, termID, item.Rule.CategoryID, document.StableHash, file, document.Rows); err != nil {
		return err
	}
	fmt.Fprintln(a.out, colorGreen("Marked category as exported."))
	return nil
}

func (a *App) categoryNeedsExport(courseYearID, termID, categoryID int, currentHash string) (bool, error) {
	var storedHash string
	err := a.db.QueryRow(`
		SELECT export_hash FROM category_exports
		WHERE course_year_id = ? AND term_id = ? AND category_id = ?`, courseYearID, termID, categoryID).Scan(&storedHash)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return storedHash != currentHash, nil
}

func (a *App) recordCategoryExport(courseYearID, termID, categoryID int, exportHash, exportPath string, rows []assignmentExportRow) error {
	rowsJSON, err := json.Marshal(assignmentExportRowsMap(rows))
	if err != nil {
		return err
	}
	_, err = a.db.Exec(`
		INSERT INTO category_exports(course_year_id, term_id, category_id, export_hash, export_path, export_rows_json, exported_at)
		VALUES (?, ?, ?, ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%fZ','now'))
		ON CONFLICT(course_year_id, term_id, category_id) DO UPDATE SET
			export_hash = excluded.export_hash,
			export_path = excluded.export_path,
			export_rows_json = excluded.export_rows_json,
			exported_at = excluded.exported_at`,
		courseYearID, termID, categoryID, exportHash, exportPath, string(rowsJSON))
	return err
}

func (a *App) loadCategoryExportRows(courseYearID, termID, categoryID int) (map[string]string, error) {
	var rowsJSON sql.NullString
	err := a.db.QueryRow(`
		SELECT export_rows_json FROM category_exports
		WHERE course_year_id = ? AND term_id = ? AND category_id = ?`, courseYearID, termID, categoryID).Scan(&rowsJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	if !rowsJSON.Valid || strings.TrimSpace(rowsJSON.String) == "" {
		return map[string]string{}, nil
	}
	var previous map[string]string
	if err := json.Unmarshal([]byte(rowsJSON.String), &previous); err != nil {
		return nil, err
	}
	return previous, nil
}
