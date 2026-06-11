package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

type ExcelReportOptions struct {
	Workbook     string
	Sheet        string
	Teacher      string
	ExamCategory string
	Printable    string
	SkipCScores  bool
}

type excelReportPayload struct {
	Sheet     string                  `json:"sheet"`
	Teacher   string                  `json:"teacher"`
	Printable string                  `json:"printable"`
	Rows      []excelReportStudentRow `json:"rows"`
}

type excelReportStudentRow struct {
	StudentID     int     `json:"student_id"`
	FirstName     string  `json:"first_name"`
	LastName      string  `json:"last_name"`
	ChineseName   string  `json:"chinese_name"`
	ExamGrade     float64 `json:"exam_grade"`
	QuarterGrade  int     `json:"quarter_grade"`
	QuarterLetter string  `json:"quarter_letter"`
	CScore1       string  `json:"c_score_1,omitempty"`
	CScore2       string  `json:"c_score_2,omitempty"`
}

func (a *App) ExcelReport(opts ExcelReportOptions) error {
	ctx := a.context()
	if ctx.TermID == 0 || ctx.CourseYearID == 0 {
		return errors.New("set year, term, and course first")
	}
	if strings.TrimSpace(opts.Workbook) == "" {
		matches, err := filepath.Glob("25-26*.xls")
		if err != nil {
			return err
		}
		filtered := matches[:0]
		for _, match := range matches {
			name := strings.ToLower(filepath.Base(match))
			if strings.Contains(name, " - updated") || strings.Contains(name, ".bak") {
				continue
			}
			filtered = append(filtered, match)
		}
		if len(filtered) == 0 {
			return errors.New("workbook not provided and no 25-26*.xls file was found")
		}
		opts.Workbook = filtered[0]
	}
	if strings.TrimSpace(opts.Sheet) == "" {
		opts.Sheet = "Senior2"
	}
	if strings.TrimSpace(opts.Teacher) == "" {
		opts.Teacher = "David P."
	}
	if strings.TrimSpace(opts.ExamCategory) == "" {
		opts.ExamCategory = "Midterm"
	}

	workbook, err := filepath.Abs(opts.Workbook)
	if err != nil {
		return err
	}
	if _, err := os.Stat(workbook); err != nil {
		return err
	}
	printable := opts.Printable
	if strings.TrimSpace(printable) == "" {
		printable = filepath.Join("excel-reports", "Senior 2 APCSA - printable.xlsx")
	}
	printable, err = filepath.Abs(printable)
	if err != nil {
		return err
	}

	payload, err := a.buildExcelReportPayload(opts, printable)
	if err != nil {
		return err
	}
	if len(payload.Rows) == 0 {
		return errors.New("no students found for the current course and term")
	}
	if !opts.SkipCScores {
		if err := a.promptExcelReportCScores(payload.Rows); err != nil {
			return err
		}
	}

	if err := os.MkdirAll(filepath.Dir(printable), 0o755); err != nil {
		return err
	}
	backup := workbook + "." + time.Now().Format("20060102_150405") + ".bak"
	if err := copyFile(workbook, backup); err != nil {
		return err
	}

	payloadFile, err := os.CreateTemp("", "grades-excel-report-*.json")
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(payloadFile)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(payload); err != nil {
		_ = payloadFile.Close()
		_ = os.Remove(payloadFile.Name())
		return err
	}
	if err := payloadFile.Close(); err != nil {
		_ = os.Remove(payloadFile.Name())
		return err
	}
	defer os.Remove(payloadFile.Name())

	script := filepath.Join("internal", "excelreport", "excel_report.py")
	if _, err := os.Stat(script); err != nil {
		return err
	}
	run := exec.Command("python", script, workbook, payloadFile.Name())
	run.Stdout = a.out
	run.Stderr = a.errOut
	if err := run.Run(); err != nil {
		return err
	}

	fmt.Fprintf(a.out, "Excel report completed for: %s\n", workbook)
	fmt.Fprintf(a.out, "Backup created: %s\n", backup)
	fmt.Fprintf(a.out, "Printable report: %s\n", printable)
	return nil
}

func (a *App) buildExcelReportPayload(opts ExcelReportOptions, printable string) (excelReportPayload, error) {
	students, _, err := a.studentsForList()
	if err != nil {
		return excelReportPayload{}, err
	}
	sortStudentsForLastNameEntry(students)
	rules, err := a.categoryRulesForContext(a.context().CourseYearID, a.context().TermID)
	if err != nil {
		return excelReportPayload{}, err
	}
	examCategoryID := 0
	for _, rule := range rules {
		if strings.EqualFold(rule.CategoryName, opts.ExamCategory) {
			examCategoryID = rule.CategoryID
			break
		}
	}
	if examCategoryID == 0 {
		return excelReportPayload{}, fmt.Errorf("exam category not found: %s", opts.ExamCategory)
	}
	categoryScores, weighted, err := a.categoryScoresByStudent(students, rules)
	if err != nil {
		return excelReportPayload{}, err
	}

	payload := excelReportPayload{Sheet: opts.Sheet, Teacher: opts.Teacher, Printable: printable}
	for _, student := range students {
		exam, ok := percentFromLabel(categoryScores[student.ID][examCategoryID])
		if !ok {
			continue
		}
		quarter, ok := percentFromLabel(weighted[student.ID])
		if !ok {
			continue
		}
		roundedQuarter := int(math.Round(quarter))
		payload.Rows = append(payload.Rows, excelReportStudentRow{
			StudentID:     student.ID,
			FirstName:     student.FirstName,
			LastName:      student.LastName,
			ChineseName:   student.ChineseName,
			ExamGrade:     math.Round(exam*10) / 10,
			QuarterGrade:  roundedQuarter,
			QuarterLetter: americanLetterGrade(float64(roundedQuarter)),
		})
	}
	return payload, nil
}

func (a *App) promptExcelReportCScores(rows []excelReportStudentRow) error {
	sort.SliceStable(rows, func(i, j int) bool {
		leftLast := strings.ToLower(rows[i].LastName)
		rightLast := strings.ToLower(rows[j].LastName)
		if leftLast != rightLast {
			return leftLast < rightLast
		}
		leftFirst := strings.ToLower(rows[i].FirstName)
		rightFirst := strings.ToLower(rows[j].FirstName)
		if leftFirst != rightFirst {
			return leftFirst < rightFirst
		}
		return rows[i].StudentID < rows[j].StudentID
	})
	for idx := range rows {
		printExcelReportCCodeReference(a.out)
		fmt.Fprintf(a.out, "%s %s\n", rows[idx].FirstName, rows[idx].LastName)
		if err := a.printExcelReportStudentContext(rows[idx]); err != nil {
			return err
		}
		score1, err := a.promptNumericOrBlank("C Score#1")
		if err != nil {
			return err
		}
		score2, err := a.promptNumericOrBlank("C Score#2")
		if err != nil {
			return err
		}
		rows[idx].CScore1 = score1
		rows[idx].CScore2 = score2
	}
	return nil
}

func (a *App) printExcelReportStudentContext(row excelReportStudentRow) error {
	student, err := a.studentByID(row.StudentID)
	if err != nil {
		return err
	}
	rules, err := a.categoryRulesForContext(a.context().CourseYearID, a.context().TermID)
	if err != nil {
		return err
	}
	categoryScores, weighted, err := a.categoryScoresByStudent([]Student{student}, rules)
	if err != nil {
		return err
	}

	fmt.Fprintf(a.out, "Quarter:\t%d%% (%s)\n", row.QuarterGrade, row.QuarterLetter)
	fmt.Fprintf(a.out, "Exam:\t\t%.1f%%\n", row.ExamGrade)
	fmt.Fprintf(a.out, "Weighted:\t%s\n", fallback(weighted[student.ID]))
	if len(rules) > 0 {
		tw := tabwriter.NewWriter(a.out, 0, 0, 2, ' ', 0)
		fmt.Fprint(tw, "Categories:")
		for _, rule := range rules {
			value := categoryScores[student.ID][rule.CategoryID]
			if strings.TrimSpace(value) == "" {
				continue
			}
			fmt.Fprintf(tw, "\t%s %s", rule.CategoryName, value)
		}
		fmt.Fprintln(tw)
		if err := tw.Flush(); err != nil {
			return err
		}
	}

	details, err := a.studentAssignmentDetails(student.ID)
	if err != nil {
		return err
	}
	stats := summarizeExcelReportStudentDetails(details)
	fmt.Fprintf(a.out, "Work flags:\t%d missing, %d outstanding late, %d late handed in, %d outstanding redo, %d redo completed\n",
		stats.missingCount,
		stats.outstandingLateCount,
		stats.completedLateCount,
		stats.outstandingRedoCount,
		stats.completedRedoCount)
	if stats.testCount > 0 {
		fmt.Fprintf(a.out, "Tests:\t\t%.1f%% average, %d low test/quiz score(s)\n", stats.testAverage, stats.lowAssessmentCount)
	}
	printLimitedList(a.out, "Outstanding", stats.outstandingItems)
	printLimitedList(a.out, "Low scores", stats.lowScoreItems)
	fmt.Fprintf(a.out, "Suggested C codes:\t%s\n\n", strings.Join(recommendExcelReportCCodes(row, stats), ", "))
	return nil
}

type excelReportStudentStats struct {
	missingCount         int
	outstandingLateCount int
	completedLateCount   int
	outstandingRedoCount int
	completedRedoCount   int
	testAverage          float64
	testCount            int
	lowAssessmentCount   int
	outstandingItems     []string
	lowScoreItems        []string
}

func summarizeExcelReportStudentDetails(details []studentAssignmentDetail) excelReportStudentStats {
	var stats excelReportStudentStats
	testTotal := 0.0
	for _, detail := range details {
		switch {
		case detail.Grade.Flags&flagMissing != 0:
			stats.missingCount++
			stats.outstandingItems = append(stats.outstandingItems, detail.Title+" (missing)")
		case hasOutstandingStudyReportLate(detail.Grade):
			stats.outstandingLateCount++
			stats.outstandingItems = append(stats.outstandingItems, detail.Title+" (late)")
		case hasOutstandingStudyReportRedo(detail.Grade):
			stats.outstandingRedoCount++
			stats.outstandingItems = append(stats.outstandingItems, detail.Title+" (redo)")
		}
		if hasCompletedStudyReportLate(detail.Grade) {
			stats.completedLateCount++
		}
		if hasCompletedStudyReportRedo(detail.Grade) {
			stats.completedRedoCount++
		}
		score := parsePercentLabel(detail.CountsAs)
		if isTestLike(detail.Title, detail.Category) && countsTowardAssignmentAverage(detail.Grade) {
			testTotal += score
			stats.testCount++
			if score < 75 {
				stats.lowAssessmentCount++
			}
		}
		if countsTowardAssignmentAverage(detail.Grade) && score > 0 && score < 70 {
			stats.lowScoreItems = append(stats.lowScoreItems, fmt.Sprintf("%s %.1f%%", detail.Title, score))
		}
	}
	if stats.testCount > 0 {
		stats.testAverage = testTotal / float64(stats.testCount)
	}
	return stats
}

func recommendExcelReportCCodes(row excelReportStudentRow, stats excelReportStudentStats) []string {
	recommendations := map[string]bool{}
	add := func(code string) {
		recommendations[code] = true
	}
	switch {
	case row.QuarterGrade >= 93:
		add("4 Outstanding performance")
		add("9 Performing above average")
	case row.QuarterGrade >= 85:
		add("9 Performing above average")
	case row.QuarterGrade < 60:
		add("17 Minimum achievement/danger of failing")
	case row.QuarterGrade < 70:
		add("14 Achieving below ability")
	}
	if stats.completedLateCount > 0 || stats.completedRedoCount > 0 {
		add("8 Shows improvement & growth")
	}
	if stats.missingCount > 0 {
		add("12 Assignments not completed/submitted on time")
	}
	if stats.outstandingLateCount > 0 {
		add("18 Absence / tardies affect work")
	}
	if len(stats.lowScoreItems) > 0 || stats.lowAssessmentCount > 0 {
		add("16 Low quiz / test scores")
	}
	if len(recommendations) == 0 {
		add("6 Has a positive attitude")
		add("7 Good effort")
	}
	ordered := []string{
		"4 Outstanding performance",
		"9 Performing above average",
		"8 Shows improvement & growth",
		"6 Has a positive attitude",
		"7 Good effort",
		"12 Assignments not completed/submitted on time",
		"18 Absence / tardies affect work",
		"16 Low quiz / test scores",
		"14 Achieving below ability",
		"17 Minimum achievement/danger of failing",
	}
	var out []string
	for _, item := range ordered {
		if recommendations[item] {
			out = append(out, item)
		}
	}
	return out
}

func printLimitedList(out io.Writer, label string, values []string) {
	if len(values) == 0 {
		return
	}
	limit := len(values)
	if limit > 5 {
		limit = 5
	}
	suffix := ""
	if len(values) > limit {
		suffix = fmt.Sprintf(" (+%d more)", len(values)-limit)
	}
	fmt.Fprintf(out, "%s:\t%s%s\n", label, strings.Join(values[:limit], ", "), suffix)
}

func (a *App) promptNumericOrBlank(label string) (string, error) {
	for {
		raw, err := a.prompt(label)
		if err != nil {
			return "", err
		}
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return "", nil
		}
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			fmt.Fprintln(a.out, retryMessage("enter a number or leave blank"))
			continue
		}
		return strconv.FormatFloat(value, 'f', -1, 64), nil
	}
}

func printExcelReportCCodeReference(out io.Writer) {
	fmt.Fprintln(out)
	fmt.Fprintln(out, "C Code & Explanations")
	fmt.Fprintln(out, "1 Good leadership qualities 具有优秀的领导素质")
	fmt.Fprintln(out, "2 Shows respect 遵守纪律,尊重他人")
	fmt.Fprintln(out, "3 Consistently prepared for class 课前准备一贯充分")
	fmt.Fprintln(out, "4 Outstanding performance 学业表现出色")
	fmt.Fprintln(out, "5 Shows self control / responsibility 有良好的自我管理能力和责任心")
	fmt.Fprintln(out, "6 Has a positive attitude 学习态度积极")
	fmt.Fprintln(out, "7 Good effort 学习努力")
	fmt.Fprintln(out, "8 Shows improvement & growth 学习有进步")
	fmt.Fprintln(out, "9 Performing above average 学业表现高于平均水平")
	fmt.Fprintln(out, "10 Uncooperative attitude 学习态度不积极,不配合")
	fmt.Fprintln(out, "11 Material not brought to class 上课未携带指定材料")
	fmt.Fprintln(out, "12 Assignments not completed/submitted on time 未能按时完成/提交作业")
	fmt.Fprintln(out, "13 Frequently disturbs class 经常干扰课堂秩序")
	fmt.Fprintln(out, "14 Achieving below ability 学习未尽全力")
	fmt.Fprintln(out, "15 Incomplete Assignments 作业内容不完整")
	fmt.Fprintln(out, "16 Low quiz / test scores 测验/考试成绩低")
	fmt.Fprintln(out, "17 Minimum achievement/danger of failing 学习收效胜微/成绩濒临不及格")
	fmt.Fprintln(out, "18 Absence / tardies affect work 缺勤/迟到影响学习")
	fmt.Fprintln(out)
}

func percentFromLabel(label string) (float64, bool) {
	label = strings.TrimSpace(strings.TrimSuffix(label, "%"))
	if label == "" {
		return 0, false
	}
	value, err := strconv.ParseFloat(label, 64)
	return value, err == nil
}

func copyFile(source, dest string) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return os.WriteFile(dest, data, 0o644)
}
