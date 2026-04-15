package app

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

type studyReportCandidate struct {
	Student        Student
	CurrentPercent float64
	LetterGrade    string
	Reasons        []string
	Comments       string
	Improvements   studyReportImprovements
}

type studyReportImprovements struct {
	MissingAssignments bool
	LowTestScores      bool
	ExcessiveTardies   bool
	LackOfMotivation   bool
	Other              string
}

type studyReportMetrics struct {
	currentPercent float64
	letterGrade    string
	testAverage    float64
	testCount      int
	lowTestCount   int
	redoCount      int
	lateCount      int
	missingCount   int
	redoDoneCount  int
	lateDoneCount  int
}

func (a *App) SuggestStudyReports() error {
	ctx := a.context()
	if ctx.TermID == 0 || ctx.CourseYearID == 0 {
		return errors.New("set year, term, and course first")
	}
	students, scope, err := a.studentsForList()
	if err != nil {
		return err
	}
	if scope != "" {
		fmt.Fprintln(a.out, scope)
	}
	candidates, err := a.studyReportCandidates(students)
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		fmt.Fprintln(a.out, "No students currently match the study report suggestion thresholds.")
		return nil
	}
	for _, candidate := range candidates {
		fmt.Fprintf(a.out, "%s %s\t%.1f%%\t%s\n",
			candidate.Student.FirstName,
			candidate.Student.LastName,
			candidate.CurrentPercent,
			candidate.LetterGrade)
		fmt.Fprintf(a.out, "Reasons:\t%s\n", strings.Join(candidate.Reasons, "; "))
	}
	return nil
}

func (a *App) CreateStudyReport(studentRef, output string) error {
	ctx := a.context()
	if ctx.TermID == 0 || ctx.CourseYearID == 0 {
		return errors.New("set year, term, and course first")
	}
	student, err := a.resolveStudentReference(studentRef)
	if err != nil {
		return err
	}
	candidate, err := a.studyReportCandidateForStudent(student)
	if err != nil {
		return err
	}
	templatePath, err := findStudyReportTemplate(".")
	if err != nil {
		return err
	}
	if output == "" {
		if err := os.MkdirAll("study-reports", 0o755); err != nil {
			return err
		}
		output = filepath.Join("study-reports", fmt.Sprintf("%s - %s %s - Study Report.docx",
			time.Now().Format("2006-01-02"), student.FirstName, student.LastName))
	}
	courseName, err := a.currentCourseName()
	if err != nil {
		return err
	}
	if err := writeFilledStudyReport(templatePath, output, studyReportData{
		StudentName:   student.FirstName + " " + student.LastName,
		Date:          time.Now().Format("2006-01-02"),
		Subject:       courseName,
		TeacherName:   "Mr Popovici",
		CurrentGrade:  fmt.Sprintf("%.1f%% (%s)", candidate.CurrentPercent, candidate.LetterGrade),
		Improvements:  candidate.Improvements,
		TeacherNotes:  candidate.Comments,
	}); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Created study report for %s %s\n", student.FirstName, student.LastName)
	fmt.Fprintf(a.out, "File:\t%s\n", output)
	fmt.Fprintf(a.out, "Reasons:\t%s\n", strings.Join(candidate.Reasons, "; "))
	return nil
}

func (a *App) studyReportCandidates(students []Student) ([]studyReportCandidate, error) {
	var candidates []studyReportCandidate
	for _, student := range students {
		candidate, err := a.studyReportCandidateForStudent(student)
		if err != nil {
			return nil, err
		}
		if len(candidate.Reasons) == 0 {
			continue
		}
		candidates = append(candidates, candidate)
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].CurrentPercent == candidates[j].CurrentPercent {
			if candidates[i].Student.LastName == candidates[j].Student.LastName {
				return candidates[i].Student.FirstName < candidates[j].Student.FirstName
			}
			return candidates[i].Student.LastName < candidates[j].Student.LastName
		}
		return candidates[i].CurrentPercent < candidates[j].CurrentPercent
	})
	return candidates, nil
}

func (a *App) studyReportCandidateForStudent(student Student) (studyReportCandidate, error) {
	metrics, err := a.studyReportMetrics(student)
	if err != nil {
		return studyReportCandidate{}, err
	}
	reasons := buildStudyReportReasons(metrics)
	return studyReportCandidate{
		Student:        student,
		CurrentPercent: metrics.currentPercent,
		LetterGrade:    metrics.letterGrade,
		Reasons:        reasons,
		Comments:       buildStudyReportComments(metrics, reasons),
		Improvements:   buildStudyReportImprovements(metrics),
	}, nil
}

func (a *App) studyReportMetrics(student Student) (studyReportMetrics, error) {
	ctx := a.context()
	rules, err := a.categoryRulesForContext(ctx.CourseYearID, ctx.TermID)
	if err != nil {
		return studyReportMetrics{}, err
	}
	categoryScores, weighted, err := a.categoryScoresByStudent([]Student{student}, rules)
	if err != nil {
		return studyReportMetrics{}, err
	}
	currentPercent := parsePercentLabel(weighted[student.ID])
	if currentPercent == 0 {
		currentPercent = averagePercentStrings(categoryScores[student.ID])
	}

	details, err := a.studentAssignmentDetails(student.ID)
	if err != nil {
		return studyReportMetrics{}, err
	}
	metrics := studyReportMetrics{
		currentPercent: currentPercent,
		letterGrade:    americanLetterGrade(currentPercent),
	}
	for _, detail := range details {
		if hasOutstandingStudyReportRedo(detail.Grade) {
			metrics.redoCount++
		}
		if hasOutstandingStudyReportLate(detail.Grade) {
			metrics.lateCount++
		}
		if hasCompletedStudyReportRedo(detail.Grade) {
			metrics.redoDoneCount++
		}
		if hasCompletedStudyReportLate(detail.Grade) {
			metrics.lateDoneCount++
		}
		if detail.Grade.Flags&flagMissing != 0 {
			metrics.missingCount++
		}
		if !isTestLike(detail.Title, detail.Category) || !countsTowardAssignmentAverage(detail.Grade) {
			continue
		}
		score := parsePercentLabel(detail.CountsAs)
		metrics.testAverage += score
		metrics.testCount++
		if score < 70 {
			metrics.lowTestCount++
		}
	}
	if metrics.testCount > 0 {
		metrics.testAverage /= float64(metrics.testCount)
	}
	return metrics, nil
}

func hasOutstandingStudyReportLate(record GradeRecord) bool {
	return record.Flags&flagLate != 0 && !hasCompletedStudyReportLate(record)
}

func hasOutstandingStudyReportRedo(record GradeRecord) bool {
	if record.Flags&flagRedo == 0 {
		return false
	}
	if hasCompletedStudyReportRedo(record) {
		return false
	}
	if !record.Score.Valid || record.MaxPoints <= 0 {
		return true
	}
	return (record.Score.Float64/float64(record.MaxPoints))*100 < 80
}

func hasCompletedStudyReportLate(record GradeRecord) bool {
	if record.Flags&flagLate == 0 {
		return false
	}
	return record.Flags&flagPass != 0 || record.Score.Valid
}

func hasCompletedStudyReportRedo(record GradeRecord) bool {
	if record.Flags&flagRedo == 0 {
		return false
	}
	if record.Flags&flagPass != 0 {
		return true
	}
	if !record.Score.Valid || record.MaxPoints <= 0 {
		return false
	}
	return (record.Score.Float64/float64(record.MaxPoints))*100 >= 80
}

func buildStudyReportReasons(metrics studyReportMetrics) []string {
	var reasons []string
	if metrics.currentPercent > 0 && metrics.currentPercent < 70 {
		reasons = append(reasons, fmt.Sprintf("current course grade is %.1f%% (%s)", metrics.currentPercent, metrics.letterGrade))
	}
	if metrics.testCount > 0 && (metrics.testAverage < 75 || metrics.lowTestCount >= 2) {
		reasons = append(reasons, fmt.Sprintf("test average is %.1f%% across %d test(s)", metrics.testAverage, metrics.testCount))
	}
	if metrics.redoCount >= 2 {
		reasons = append(reasons, fmt.Sprintf("%d assignment(s) currently marked redo", metrics.redoCount))
	}
	if metrics.missingCount >= 2 || metrics.lateCount+metrics.missingCount >= 3 {
		reasons = append(reasons, fmt.Sprintf("%d missing and %d late assignment(s)", metrics.missingCount, metrics.lateCount))
	}
	return reasons
}

func buildStudyReportImprovements(metrics studyReportMetrics) studyReportImprovements {
	improvements := studyReportImprovements{}
	if metrics.missingCount > 0 {
		improvements.MissingAssignments = true
	}
	if metrics.testCount > 0 && (metrics.testAverage < 75 || metrics.lowTestCount >= 2) {
		improvements.LowTestScores = true
	}
	if metrics.lateCount >= 2 {
		improvements.ExcessiveTardies = true
	}
	if metrics.redoCount >= 2 || metrics.currentPercent < 70 || metrics.lateCount+metrics.missingCount >= 3 {
		improvements.LackOfMotivation = true
	}
	if metrics.redoCount > 0 {
		improvements.Other = "Redo assignments"
	}
	return improvements
}

func buildStudyReportComments(metrics studyReportMetrics, reasons []string) string {
	history := studyReportHistoryComment(metrics)
	if len(reasons) == 0 {
		base := fmt.Sprintf("Current grade is %.1f%% (%s). Please continue monitoring PowerSchool and class progress.", metrics.currentPercent, metrics.letterGrade)
		if history == "" {
			return base
		}
		return base + " " + history
	}
	var recommendations []string
	if metrics.missingCount > 0 {
		recommendations = append(recommendations, "check PowerSchool daily and complete missing work")
	}
	if metrics.redoCount > 0 {
		recommendations = append(recommendations, "finish redo assignments promptly")
	}
	if metrics.testCount > 0 && (metrics.testAverage < 75 || metrics.lowTestCount >= 2) {
		recommendations = append(recommendations, "review notes and prepare more consistently for tests")
	}
	if metrics.lateCount > 0 {
		recommendations = append(recommendations, "submit work on time")
	}
	if len(recommendations) == 0 {
		recommendations = append(recommendations, "maintain consistent study habits")
	}
	return fmt.Sprintf("Current grade is %.1f%% (%s). Concerns: %s. Recommendation: %s.",
		metrics.currentPercent,
		metrics.letterGrade,
		strings.Join(reasons, "; "),
		strings.Join(recommendations, "; ")) + historySuffix(history)
}

func studyReportHistoryComment(metrics studyReportMetrics) string {
	var parts []string
	if metrics.lateDoneCount > 0 {
		parts = append(parts, fmt.Sprintf("%d late assignment(s) completed", metrics.lateDoneCount))
	}
	if metrics.redoDoneCount > 0 {
		parts = append(parts, fmt.Sprintf("%d redo assignment(s) completed", metrics.redoDoneCount))
	}
	if len(parts) == 0 {
		return ""
	}
	return "Work habits history: " + strings.Join(parts, "; ") + "."
}

func historySuffix(history string) string {
	if history == "" {
		return ""
	}
	return " " + history
}

func averagePercentStrings(values map[int]string) float64 {
	total := 0.0
	count := 0
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			continue
		}
		total += parsePercentLabel(value)
		count++
	}
	if count == 0 {
		return 0
	}
	return total / float64(count)
}

func americanLetterGrade(percent float64) string {
	switch {
	case percent >= 93:
		return "A"
	case percent >= 90:
		return "A-"
	case percent >= 87:
		return "B+"
	case percent >= 83:
		return "B"
	case percent >= 80:
		return "B-"
	case percent >= 77:
		return "C+"
	case percent >= 73:
		return "C"
	case percent >= 70:
		return "C-"
	case percent >= 67:
		return "D+"
	case percent >= 63:
		return "D"
	case percent >= 60:
		return "D-"
	default:
		return "F"
	}
}

func isTestLike(title, category string) bool {
	for _, token := range reportAssessmentTokens(title) {
		switch token {
		case "test", "tests", "exam", "exams", "midterm", "midterms", "final", "finals", "retest", "retests":
			return true
		}
	}
	return false
}

func reportAssessmentTokens(values ...string) []string {
	var tokens []string
	for _, value := range values {
		for _, token := range strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsNumber(r)
		}) {
			if token != "" {
				tokens = append(tokens, token)
			}
		}
	}
	return tokens
}

func (a *App) currentCourseName() (string, error) {
	var courseName string
	if err := a.db.QueryRow(`SELECT name FROM course_years WHERE course_year_id = ?`, a.context().CourseYearID).Scan(&courseName); err != nil {
		return "", err
	}
	return baseCourseName(courseName), nil
}

func findStudyReportTemplate(root string) (string, error) {
	searchRoots := []string{root}
	if wd, err := os.Getwd(); err == nil {
		current := wd
		for i := 0; i < 4; i++ {
			searchRoots = append(searchRoots, current)
			parent := filepath.Dir(current)
			if parent == current {
				break
			}
			current = parent
		}
	}
	seen := map[string]bool{}
	for _, searchRoot := range searchRoots {
		if seen[searchRoot] {
			continue
		}
		seen[searchRoot] = true
		matches, err := filepath.Glob(filepath.Join(searchRoot, "Study Report*blank*.docx"))
		if err != nil {
			return "", err
		}
		if len(matches) == 0 {
			continue
		}
		sort.Strings(matches)
		return matches[0], nil
	}
	return "", errors.New("blank study report template not found")
}

type studyReportData struct {
	StudentName  string
	Date         string
	Subject      string
	TeacherName  string
	CurrentGrade string
	Improvements studyReportImprovements
	TeacherNotes string
}

func writeFilledStudyReport(templatePath, outputPath string, data studyReportData) error {
	source, err := os.Open(templatePath)
	if err != nil {
		return err
	}
	defer source.Close()
	reader, err := zip.NewReader(source, fileSize(source))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil && filepath.Dir(outputPath) != "." {
		return err
	}
	dest, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer dest.Close()
	writer := zip.NewWriter(dest)
	defer writer.Close()

	for _, file := range reader.File {
		header := file.FileHeader
		out, err := writer.CreateHeader(&header)
		if err != nil {
			return err
		}
		rc, err := file.Open()
		if err != nil {
			return err
		}
		if file.Name == "word/document.xml" {
			body, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return err
			}
			updated, err := fillStudyReportDocument(string(body), data)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, strings.NewReader(updated)); err != nil {
				return err
			}
			continue
		}
		if _, err := io.Copy(out, rc); err != nil {
			rc.Close()
			return err
		}
		rc.Close()
	}
	return writer.Close()
}

func fileSize(f *os.File) int64 {
	info, _ := f.Stat()
	if info == nil {
		return 0
	}
	return info.Size()
}

var (
	rowPattern  = regexp.MustCompile(`(?s)<w:tr\b.*?</w:tr>`)
	cellPattern = regexp.MustCompile(`(?s)<w:tc\b.*?</w:tc>`)
	cellBodyRE  = regexp.MustCompile(`(?s)(<w:tc\b.*?<w:tcPr>.*?</w:tcPr>)(.*)(</w:tc>)`)
)

func fillStudyReportDocument(doc string, data studyReportData) (string, error) {
	rows := rowPattern.FindAllString(doc, -1)
	if len(rows) < 10 {
		return "", errors.New("study report template format not recognized")
	}
	var err error
	rows[1], err = setRowCellText(rows[1], 1, data.StudentName)
	if err != nil {
		return "", err
	}
	rows[1], err = setRowCellText(rows[1], 3, data.Date)
	if err != nil {
		return "", err
	}
	rows[2], err = setRowCellText(rows[2], 1, data.Subject)
	if err != nil {
		return "", err
	}
	rows[3], err = setRowCellText(rows[3], 2, data.TeacherName)
	if err != nil {
		return "", err
	}
	rows[4], err = setRowCellText(rows[4], 1, data.CurrentGrade)
	if err != nil {
		return "", err
	}
	rows[7], err = setRowCellText(rows[7], 0, improvementLine(data.Improvements))
	if err != nil {
		return "", err
	}
	rows[9], err = setRowCellText(rows[9], 0, data.TeacherNotes)
	if err != nil {
		return "", err
	}
	matches := rowPattern.FindAllStringIndex(doc, -1)
	var out strings.Builder
	last := 0
	for i, match := range matches {
		out.WriteString(doc[last:match[0]])
		out.WriteString(rows[i])
		last = match[1]
	}
	out.WriteString(doc[last:])
	return out.String(), nil
}

func setRowCellText(row string, cellIndex int, text string) (string, error) {
	cells := cellPattern.FindAllString(row, -1)
	if cellIndex >= len(cells) {
		return "", fmt.Errorf("study report template cell %d not found", cellIndex)
	}
	replaced := setCellText(cells[cellIndex], text)
	result := row
	for i, cell := range cells {
		if i == cellIndex {
			result = strings.Replace(result, cell, replaced, 1)
			break
		}
	}
	return result, nil
}

func setCellText(cell, text string) string {
	body := makeWordParagraph(text)
	return cellBodyRE.ReplaceAllString(cell, "${1}"+body+"${3}")
}

func makeWordParagraph(text string) string {
	var escaped bytes.Buffer
	_ = xml.EscapeText(&escaped, []byte(text))
	return "<w:p><w:pPr><w:spacing w:after=\"0\" w:line=\"240\" w:lineRule=\"auto\"/></w:pPr><w:r><w:rPr><w:rFonts w:ascii=\"Times New Roman\" w:hAnsi=\"Times New Roman\" w:eastAsia=\"Times New Roman\" w:cs=\"Times New Roman\"/><w:color w:val=\"000000\"/></w:rPr><w:t xml:space=\"preserve\">" + escaped.String() + "</w:t></w:r></w:p>"
}

func improvementLine(improvements studyReportImprovements) string {
	check := func(on bool) string {
		if on {
			return "☒"
		}
		return "☐"
	}
	otherText := "_______________________"
	if strings.TrimSpace(improvements.Other) != "" {
		otherText = improvements.Other
	}
	return fmt.Sprintf("Areas for improvement: %s Missing assignments  %s Low test scores  %s Excessive absences  %s Excessive tardies  %s Inappropriate behavior  %s Lack of motivation  %s Other: %s",
		check(improvements.MissingAssignments),
		check(improvements.LowTestScores),
		check(false),
		check(improvements.ExcessiveTardies),
		check(false),
		check(improvements.LackOfMotivation),
		check(strings.TrimSpace(improvements.Other) != ""),
		otherText)
}
