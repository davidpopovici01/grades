package app

import (
	"bytes"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/davidpopovici01/grades/internal/portalauth"
)

type portalStudentSnapshot struct {
	StudentID                  int                        `json:"studentId"`
	FirstName                  string                     `json:"firstName"`
	LastName                   string                     `json:"lastName"`
	ChineseName                string                     `json:"chineseName,omitempty"`
	SchoolStudentID            string                     `json:"schoolStudentId,omitempty"`
	CourseName                 string                     `json:"courseName"`
	TermName                   string                     `json:"termName"`
	Sections                   []string                   `json:"sections"`
	Username                   string                     `json:"username,omitempty"`
	WeightedTotal              float64                    `json:"weightedTotal"`
	WeightedTotalLabel         string                     `json:"weightedTotalLabel"`
	LetterGrade                string                     `json:"letterGrade"`
	GPA                        float64                    `json:"gpa"`
	GPALabel                   string                     `json:"gpaLabel"`
	ActiveCategoryCount        int                        `json:"activeCategoryCount"`
	OverviewCutoffAssignmentID int                        `json:"overviewCutoffAssignmentId"`
	Categories                 []portalCategorySnapshot   `json:"categories"`
	Assignments                []portalAssignmentSnapshot `json:"assignments"`
	ImprovementTips            []string                   `json:"improvementTips"`
}

type portalCategorySnapshot struct {
	CategoryID         int     `json:"categoryId"`
	CategoryName       string  `json:"categoryName"`
	WeightPercent      float64 `json:"weightPercent"`
	HasWeight          bool    `json:"hasWeight"`
	WeightLabel        string  `json:"weightLabel"`
	Score              float64 `json:"score"`
	ScoreLabel         string  `json:"scoreLabel"`
	SchemeKey          string  `json:"schemeKey"`
	DefaultPassPercent float64 `json:"defaultPassPercent,omitempty"`
	Included           bool    `json:"included"`
	ShowInOverview     bool    `json:"showInOverview"`
}

type portalAssignmentSnapshot struct {
	AssignmentID        int      `json:"assignmentId"`
	Title               string   `json:"title"`
	CategoryID          int      `json:"categoryId"`
	CategoryName        string   `json:"categoryName"`
	MaxPoints           int      `json:"maxPoints"`
	SchemeKey           string   `json:"schemeKey"`
	PassPercent         *float64 `json:"passPercent,omitempty"`
	Anchor              float64  `json:"anchor"`
	Lift                float64  `json:"lift"`
	Score               *float64 `json:"score,omitempty"`
	Flags               []string `json:"flags"`
	IsBeforeCutoff      bool     `json:"isBeforeCutoff"`
	CurrentPercent      float64  `json:"currentPercent"`
	CurrentPercentLabel string   `json:"currentPercentLabel"`
	ShowInOverview      bool     `json:"showInOverview"`
}

type portalCourseSnapshot struct {
	CourseYearID   int                     `json:"courseYearId"`
	TermID         int                     `json:"termId"`
	CourseName     string                  `json:"courseName"`
	CourseYearName string                  `json:"courseYearName"`
	TermName       string                  `json:"termName"`
	PublishedAt    string                  `json:"publishedAt"`
	Students       []portalStudentSnapshot `json:"students"`
}

type portalPublishRequest struct {
	Accounts []portalauth.Account `json:"accounts"`
	Course   portalCourseInfo     `json:"course"`
	Students []struct {
		StudentID int             `json:"studentId"`
		Snapshot  json.RawMessage `json:"snapshot"`
	} `json:"students"`
}

type portalCourseInfo struct {
	CourseYearID   int    `json:"courseYearId"`
	TermID         int    `json:"termId"`
	CourseName     string `json:"courseName"`
	CourseYearName string `json:"courseYearName"`
	TermName       string `json:"termName"`
	PublishedAt    string `json:"publishedAt"`
}

// portalStudentCourse bundles one enrolled course's snapshot for the
// preview server's /api/grades response. It mirrors portalserver.StudentSnapshot.
type portalStudentCourse struct {
	CourseYearID   int                   `json:"courseYearId"`
	TermID         int                   `json:"termId"`
	CourseName     string                `json:"courseName"`
	CourseYearName string                `json:"courseYearName"`
	TermName       string                `json:"termName"`
	PublishedAt    string                `json:"publishedAt"`
	Snapshot       portalStudentSnapshot `json:"snapshot"`
}

type portalSession struct {
	StudentID int
	ExpiresAt time.Time
}

type portalServer struct {
	app       *App
	staticDir string
	now       func() time.Time
	mu        sync.Mutex
	sessions  map[string]portalSession
	// buildMu serializes snapshot builds, which temporarily mutate the app context.
	buildMu sync.Mutex
}

type accountInitResult struct {
	StudentID int
	Name      string
	Username  string
	Password  string
}

// PrintPortalTeacherToken prints the configured portal admin token so it can
// be pasted into the portal's /admin login page.
func (a *App) PrintPortalTeacherToken() error {
	token := strings.TrimSpace(a.v.GetString("portal.teacher_token"))
	if token == "" {
		return errors.New("portal.teacher_token is not set in ~/.grades/config.yaml")
	}
	fmt.Fprintln(a.out, token)
	return nil
}

func (a *App) PublishStudentPortal() error {
	ctx := a.context()
	if ctx.TermID == 0 || ctx.CourseYearID == 0 {
		return errors.New("set year, term, and course first")
	}

	// Publishing pushes the snapshot to the portal server over HTTP. There is
	// no local file consumer anymore: the preview server (web serve) builds
	// snapshots straight from the database.
	portalURL := strings.TrimSpace(a.v.GetString("portal.url"))
	if portalURL == "" {
		fmt.Fprintln(a.out, "portal.url is not configured; nothing to publish.")
		fmt.Fprintln(a.out, "Set portal.url (and portal.teacher_token) in ~/.grades/config.yaml to push to the portal server,")
		fmt.Fprintln(a.out, "or run 'grades web serve' for a local preview that reads the database directly.")
		return nil
	}
	return a.PushStudentPortal(portalURL)
}

// PushStudentPortal sends the current course snapshot to the VPS portal API.
func (a *App) PushStudentPortal(baseURL string) error {
	ctx := a.context()
	if ctx.TermID == 0 || ctx.CourseYearID == 0 {
		return errors.New("set year, term, and course first")
	}

	snapshot, err := a.buildPortalCourseSnapshot(ctx.CourseYearID, ctx.TermID)
	if err != nil {
		return err
	}
	accounts, err := a.portalAccountsForCourseTerm(ctx.CourseYearID, ctx.TermID)
	if err != nil {
		return err
	}

	payload := portalPublishRequest{
		Accounts: accounts,
		Course: portalCourseInfo{
			CourseYearID:   snapshot.CourseYearID,
			TermID:         snapshot.TermID,
			CourseName:     snapshot.CourseName,
			CourseYearName: snapshot.CourseYearName,
			TermName:       snapshot.TermName,
			PublishedAt:    snapshot.PublishedAt,
		},
	}
	for _, student := range snapshot.Students {
		raw, err := json.Marshal(student)
		if err != nil {
			return err
		}
		payload.Students = append(payload.Students, struct {
			StudentID int             `json:"studentId"`
			Snapshot  json.RawMessage `json:"snapshot"`
		}{StudentID: student.StudentID, Snapshot: raw})
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	endpoint := strings.TrimSuffix(baseURL, "/") + "/api/admin/publish"
	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if token := strings.TrimSpace(a.v.GetString("portal.teacher_token")); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	resBody, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("portal publish failed (%d): %s", res.StatusCode, strings.TrimSpace(string(resBody)))
	}

	fmt.Fprintf(a.out, "Published student portal to %s\n", endpoint)
	fmt.Fprintf(a.out, "Published %d student snapshot(s)\n", len(payload.Students))
	fmt.Fprintf(a.out, "Exported %d account(s)\n", len(payload.Accounts))
	return nil
}

func (a *App) InitStudentPortalAccounts(defaultPassword string, memorable bool) error {
	ctx := a.context()
	if ctx.TermID == 0 || ctx.CourseYearID == 0 {
		return errors.New("set year, term, and course first")
	}
	results, err := a.ensurePortalAccounts(ctx.CourseYearID, ctx.TermID, defaultPassword, memorable)
	if err != nil {
		return err
	}
	if len(results) == 0 {
		fmt.Fprintln(a.out, "No student accounts created.")
		return nil
	}
	fmt.Fprintln(a.out, "Student portal accounts")
	for _, item := range results {
		fmt.Fprintf(a.out, "%s\t%s\t%s\n", item.Name, item.Username, item.Password)
	}
	return nil
}

func (a *App) ResetStudentPortalPassword(studentRef, password string, memorable bool) error {
	if err := a.ensureStudentCommandContext(); err != nil {
		return err
	}
	student, err := a.resolveStudentReference(studentRef)
	if err != nil {
		return err
	}
	if strings.TrimSpace(password) == "" {
		password, err = portalauth.RandomOrMemorablePassword(memorable)
		if err != nil {
			return err
		}
	}
	hash, salt, err := portalauth.HashPassword(password)
	if err != nil {
		return err
	}
	username, err := a.studentPortalUsername(student.ID)
	if err != nil {
		username = nextPortalUsername(student, map[string]bool{})
	}
	changedAt := time.Now().UTC().Format(time.RFC3339)
	_, err = a.db.Exec(`
		INSERT INTO student_accounts(student_pk, username, password_salt, password_hash, must_change_password, password_changed_at, updated_at)
		VALUES (?, ?, ?, ?, 0, ?, strftime('%Y-%m-%dT%H:%M:%fZ','now'))
		ON CONFLICT(student_pk) DO UPDATE SET
			username = excluded.username,
			password_salt = excluded.password_salt,
			password_hash = excluded.password_hash,
			must_change_password = excluded.must_change_password,
			password_changed_at = excluded.password_changed_at,
			updated_at = excluded.updated_at`,
		student.ID, username, salt, hash, changedAt)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Reset portal password for %s %s\n", student.FirstName, student.LastName)
	fmt.Fprintf(a.out, "Username:\t%s\n", username)
	fmt.Fprintf(a.out, "Temporary password:\t%s\n", password)
	return nil
}

func (a *App) ListStudentPortalAccounts() error {
	ctx := a.context()
	if ctx.TermID == 0 || ctx.CourseYearID == 0 {
		return errors.New("set year, term, and course first")
	}

	rows, err := a.db.Query(`
		SELECT students.student_pk, students.first_name, students.last_name,
		       COALESCE(student_accounts.username, ''),
		       COALESCE(student_accounts.must_change_password, 0)
		FROM students
		JOIN section_enrollments ON section_enrollments.student_pk = students.student_pk
		JOIN sections ON sections.section_id = section_enrollments.section_id
		LEFT JOIN student_accounts ON student_accounts.student_pk = students.student_pk
		WHERE sections.course_year_id = ? AND section_enrollments.term_id = ?
		GROUP BY students.student_pk
		ORDER BY students.last_name, students.first_name`,
		ctx.CourseYearID, ctx.TermID)
	if err != nil {
		return err
	}
	defer rows.Close()

	fmt.Fprintln(a.out, "Student portal accounts")
	fmt.Fprintln(a.out, "Name\t\t\tUsername\t\tStatus")
	count := 0
	for rows.Next() {
		var studentID int
		var firstName, lastName, username string
		var mustChange int
		if err := rows.Scan(&studentID, &firstName, &lastName, &username, &mustChange); err != nil {
			return err
		}
		name := firstName + " " + lastName
		status := "not created"
		if username != "" {
			status = "active"
		}
		fmt.Fprintf(a.out, "%-23s\t%-20s\t%s\n", name, username, status)
		count++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "\n%d student(s)\n", count)
	fmt.Fprintln(a.out, "\nNote: Passwords are stored hashed and cannot be displayed.")
	fmt.Fprintln(a.out, "Use 'grades web accounts reset <student> --memorable' to generate a new password.")
	return nil
}

func (a *App) ServeStudentPortal(addr string) error {
	ctx := a.context()
	if ctx.TermID == 0 || ctx.CourseYearID == 0 {
		return errors.New("set year, term, and course first")
	}
	if strings.TrimSpace(addr) == "" {
		addr = "127.0.0.1:8080"
	}
	created, err := a.ensurePortalAccounts(ctx.CourseYearID, ctx.TermID, "", false)
	if err != nil {
		return err
	}
	if len(created) > 0 {
		fmt.Fprintln(a.out, "Created student portal accounts")
		for _, item := range created {
			fmt.Fprintf(a.out, "%s\t%s\t%s\n", item.Name, item.Username, item.Password)
		}
	}
	server := &portalServer{app: a, staticDir: locatePortalWebDist(), now: time.Now, sessions: map[string]portalSession{}}
	if server.staticDir != "" {
		fmt.Fprintf(a.out, "Serving portal frontend from %s\n", server.staticDir)
	} else {
		fmt.Fprintln(a.out, "portal-web/dist not found; falling back to the legacy preview page.")
		fmt.Fprintln(a.out, "Note: the legacy page differs from the production UI.")
		fmt.Fprintln(a.out, "Build the real frontend with: cd portal-web && npm run build")
	}
	fmt.Fprintf(a.out, "Student portal serving at http://%s\n", addr)
	return http.ListenAndServe(addr, server.routes())
}

// locatePortalWebDist finds the built React SPA (portal-web/dist), looking
// relative to the working directory first and then next to the executable.
// It returns "" when no built frontend is found.
func locatePortalWebDist() string {
	candidates := []string{filepath.Join("portal-web", "dist")}
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(exeDir, "portal-web", "dist"),
			filepath.Join(exeDir, "..", "portal-web", "dist"),
		)
	}
	for _, candidate := range candidates {
		info, err := os.Stat(filepath.Join(candidate, "index.html"))
		if err != nil || info.IsDir() {
			continue
		}
		if abs, err := filepath.Abs(candidate); err == nil {
			return abs
		}
		return candidate
	}
	return ""
}

func (a *App) buildPortalCourseSnapshot(courseYearID, termID int) (portalCourseSnapshot, error) {
	var snapshot portalCourseSnapshot
	snapshot.CourseYearID = courseYearID
	snapshot.TermID = termID
	courseName, courseYearName, termName, err := a.portalCourseTermNames(courseYearID, termID)
	if err != nil {
		return portalCourseSnapshot{}, err
	}
	snapshot.CourseName = courseName
	snapshot.CourseYearName = courseYearName
	snapshot.TermName = termName
	snapshot.PublishedAt = time.Now().UTC().Format(time.RFC3339)
	students, err := a.studentsForCourseTerm(courseYearID, termID, false)
	if err != nil {
		return portalCourseSnapshot{}, err
	}
	rules, err := a.categoryRulesForContext(courseYearID, termID)
	if err != nil {
		return portalCourseSnapshot{}, err
	}
	cutoff := a.overviewCutoffForCourseTerm(courseYearID, termID)
	for _, student := range students {
		item, err := a.buildPortalStudentSnapshot(snapshot.CourseName, snapshot.TermName, courseYearID, termID, student, rules, cutoff)
		if err != nil {
			return portalCourseSnapshot{}, err
		}
		snapshot.Students = append(snapshot.Students, item)
	}
	sort.Slice(snapshot.Students, func(i, j int) bool {
		if snapshot.Students[i].LastName == snapshot.Students[j].LastName {
			return snapshot.Students[i].FirstName < snapshot.Students[j].FirstName
		}
		return snapshot.Students[i].LastName < snapshot.Students[j].LastName
	})
	return snapshot, nil
}

// portalCourseTermNames returns the display names for a course year and term:
// the base course name, the year label (e.g. "2025-26"), and the term name.
func (a *App) portalCourseTermNames(courseYearID, termID int) (string, string, string, error) {
	var courseName, termName string
	if err := a.db.QueryRow(`
		SELECT course_years.name, terms.name
		FROM course_years
		JOIN course_year_terms ON course_year_terms.course_year_id = course_years.course_year_id
		JOIN terms ON terms.term_id = course_year_terms.term_id
		WHERE course_years.course_year_id = ? AND terms.term_id = ?`,
		courseYearID, termID).Scan(&courseName, &termName); err != nil {
		return "", "", "", err
	}
	return baseCourseName(courseName), courseYearLabel(courseName), termName, nil
}

// overviewCutoffForCourseTerm returns the overview cutoff assignment for a
// course and term, or 0 when none is set. Unlike OverviewCutoff it does not
// depend on the current context.
func (a *App) overviewCutoffForCourseTerm(courseYearID, termID int) int {
	var cutoff sql.NullInt64
	err := a.db.QueryRow(`
		SELECT overview_cutoff_assignment_id
		FROM course_year_terms
		WHERE course_year_id = ? AND term_id = ?`, courseYearID, termID).Scan(&cutoff)
	if err != nil || !cutoff.Valid {
		return 0
	}
	return int(cutoff.Int64)
}

// portalCoursesForStudent builds a grade snapshot for every course and term
// the student is enrolled in, ordered by course and term name. The local
// preview server uses this to serve /api/grades straight from the database.
func (a *App) portalCoursesForStudent(studentID int) ([]portalStudentCourse, error) {
	rows, err := a.db.Query(`
		SELECT DISTINCT sections.course_year_id, section_enrollments.term_id
		FROM section_enrollments
		JOIN sections ON sections.section_id = section_enrollments.section_id
		WHERE section_enrollments.student_pk = ?
		ORDER BY sections.course_year_id, section_enrollments.term_id`, studentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var pairs [][2]int
	for rows.Next() {
		var courseYearID, termID int
		if err := rows.Scan(&courseYearID, &termID); err != nil {
			return nil, err
		}
		pairs = append(pairs, [2]int{courseYearID, termID})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var courses []portalStudentCourse
	for _, pair := range pairs {
		entry, err := a.buildPortalStudentCourse(pair[0], pair[1], studentID)
		if err != nil {
			return nil, err
		}
		courses = append(courses, entry)
	}
	sort.Slice(courses, func(i, j int) bool {
		if courses[i].CourseName == courses[j].CourseName {
			return courses[i].TermName < courses[j].TermName
		}
		return courses[i].CourseName < courses[j].CourseName
	})
	return courses, nil
}

// buildPortalStudentCourse builds the /api/grades course entry for a single
// student in one course and term.
func (a *App) buildPortalStudentCourse(courseYearID, termID, studentID int) (portalStudentCourse, error) {
	courseName, courseYearName, termName, err := a.portalCourseTermNames(courseYearID, termID)
	if err != nil {
		return portalStudentCourse{}, err
	}
	students, err := a.studentsForCourseTerm(courseYearID, termID, false)
	if err != nil {
		return portalStudentCourse{}, err
	}
	var student *Student
	for i := range students {
		if students[i].ID == studentID {
			student = &students[i]
			break
		}
	}
	if student == nil {
		return portalStudentCourse{}, fmt.Errorf("student %d is not enrolled in course year %d term %d", studentID, courseYearID, termID)
	}
	rules, err := a.categoryRulesForContext(courseYearID, termID)
	if err != nil {
		return portalStudentCourse{}, err
	}
	cutoff := a.overviewCutoffForCourseTerm(courseYearID, termID)
	snapshot, err := a.buildPortalStudentSnapshot(courseName, termName, courseYearID, termID, *student, rules, cutoff)
	if err != nil {
		return portalStudentCourse{}, err
	}
	return portalStudentCourse{
		CourseYearID:   courseYearID,
		TermID:         termID,
		CourseName:     courseName,
		CourseYearName: courseYearName,
		TermName:       termName,
		PublishedAt:    time.Now().UTC().Format(time.RFC3339),
		Snapshot:       snapshot,
	}, nil
}

func (a *App) buildPortalStudentSnapshot(courseName, termName string, courseYearID, termID int, student Student, rules []CategoryRule, cutoff int) (portalStudentSnapshot, error) {
	ctx := a.context()
	defer func(previous Context) {
		a.setContext("context.year", previous.Year)
		a.setContext("context.term_id", previous.TermID)
		a.setContext("context.course_year_id", previous.CourseYearID)
		a.setContext("context.section_id", previous.SectionID)
		a.setContext("context.assignment_id", previous.AssignmentID)
	}(ctx)
	a.setContext("context.term_id", termID)
	a.setContext("context.course_year_id", courseYearID)
	a.setContext("context.section_id", 0)
	a.setContext("context.assignment_id", 0)

	details, err := a.portalAssignmentDetails(student.ID)
	if err != nil {
		return portalStudentSnapshot{}, err
	}
	sections, err := a.studentSectionsForPortal(student.ID, courseYearID, termID)
	if err != nil {
		return portalStudentSnapshot{}, err
	}
	categorySnapshots, weightedTotal, weightedLabel, activeCategoryCount := portalCategorySnapshots(rules, details)
	showInOverviewByCategory := map[int]bool{}
	for _, rule := range rules {
		showInOverviewByCategory[rule.CategoryID] = rule.IsVisibleInOverview()
	}
	username, _ := a.studentPortalUsername(student.ID)
	item := portalStudentSnapshot{
		StudentID:                  student.ID,
		FirstName:                  student.FirstName,
		LastName:                   student.LastName,
		ChineseName:                student.ChineseName,
		SchoolStudentID:            student.SchoolStudentID,
		CourseName:                 courseName,
		TermName:                   termName,
		Sections:                   sections,
		Username:                   username,
		WeightedTotal:              weightedTotal,
		WeightedTotalLabel:         weightedLabel,
		LetterGrade:                portalauth.AmericanLetterGrade(weightedTotal),
		GPA:                        weightedTotal,
		GPALabel:                   weightedLabel,
		ActiveCategoryCount:        activeCategoryCount,
		OverviewCutoffAssignmentID: cutoff,
		Categories:                 categorySnapshots,
	}
	for _, detail := range details {
		var score *float64
		if detail.Grade.Score.Valid {
			value := detail.Grade.Score.Float64
			score = &value
		}
		var passPercent *float64
		if detail.Grade.PassPercent.Valid {
			value := detail.Grade.PassPercent.Float64
			passPercent = &value
		}
		currentPercent := effectiveAssignmentPercent(detail.Grade, detail.Grade.PassPercent, detail.Anchor, detail.Lift)
		item.Assignments = append(item.Assignments, portalAssignmentSnapshot{
			AssignmentID:        detail.AssignmentID,
			Title:               detail.Title,
			CategoryID:          detail.CategoryID,
			CategoryName:        detail.Category,
			MaxPoints:           detail.Grade.MaxPoints,
			SchemeKey:           detail.SchemeKey,
			PassPercent:         passPercent,
			Anchor:              detail.Anchor,
			Lift:                detail.Lift,
			Score:               score,
			Flags:               detail.Flags,
			IsBeforeCutoff:      cutoff > 0 && detail.AssignmentID <= cutoff,
			CurrentPercent:      currentPercent,
			CurrentPercentLabel: fmt.Sprintf("%.1f%%", currentPercent),
			ShowInOverview:      showInOverviewByCategory[detail.CategoryID],
		})
	}
	item.ImprovementTips = portalImprovementTips(item.Assignments, item.Categories, cutoff)
	return item, nil
}

func parsePercentLabel(value string) float64 {
	value = strings.TrimSpace(strings.TrimSuffix(value, "%"))
	if value == "" {
		return 0
	}
	out, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0
	}
	return out
}

func (a *App) studentSectionsForPortal(studentID, courseYearID, termID int) ([]string, error) {
	rows, err := a.db.Query(`
		SELECT sections.name
		FROM section_enrollments
		JOIN sections ON sections.section_id = section_enrollments.section_id
		WHERE section_enrollments.student_pk = ? AND section_enrollments.term_id = ? AND sections.course_year_id = ?
		ORDER BY sections.name`, studentID, termID, courseYearID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

func portalCategorySnapshots(rules []CategoryRule, details []portalAssignmentDetail) ([]portalCategorySnapshot, float64, string, int) {
	assignmentsByCategory := map[int][]portalAssignmentDetail{}
	for _, detail := range details {
		assignmentsByCategory[detail.CategoryID] = append(assignmentsByCategory[detail.CategoryID], detail)
	}
	var snapshots []portalCategorySnapshot
	weightedTotal := 0.0
	totalWeight := 0.0
	activeCategories := 0
	for _, rule := range rules {
		items := assignmentsByCategory[rule.CategoryID]
		score, included := portalCategoryScore(rule, items)
		passPercent := 0.0
		if rule.DefaultPassPercent.Valid {
			passPercent = rule.DefaultPassPercent.Float64
		}
		weightLabel := ""
		if rule.HasWeight {
			weightLabel = fmt.Sprintf("%.1f%%", rule.WeightPercent)
		}
		scoreLabel := ""
		if included {
			scoreLabel = fmt.Sprintf("%.1f%%", score)
			activeCategories++
			if rule.HasWeight {
				weightedTotal += score * rule.WeightPercent
				totalWeight += rule.WeightPercent
			}
		}
		snapshots = append(snapshots, portalCategorySnapshot{
			CategoryID:         rule.CategoryID,
			CategoryName:       rule.CategoryName,
			WeightPercent:      rule.WeightPercent,
			HasWeight:          rule.HasWeight,
			WeightLabel:        weightLabel,
			Score:              score,
			ScoreLabel:         scoreLabel,
			SchemeKey:          rule.SchemeKey,
			DefaultPassPercent: passPercent,
			Included:           included,
			ShowInOverview:     rule.IsVisibleInOverview(),
		})
	}
	weightedLabel := ""
	if totalWeight > 0 {
		weightedTotal /= totalWeight
		weightedLabel = fmt.Sprintf("%.1f%%", weightedTotal)
	}
	return snapshots, weightedTotal, weightedLabel, activeCategories
}

func portalCategoryScore(rule CategoryRule, items []portalAssignmentDetail) (float64, bool) {
	hasEntry := false
	switch rule.SchemeKey {
	case "completion":
		if len(items) == 0 {
			return 0, false
		}
		total := 0.0
		count := 0
		for _, item := range items {
			if portalAssignmentHasEntry(item.Grade) {
				hasEntry = true
			}
			if !countsTowardAssignmentAverage(item.Grade) {
				continue
			}
			total += effectiveAssignmentPercent(item.Grade, item.Grade.PassPercent, item.Anchor, item.Lift)
			count++
		}
		if !hasEntry || count == 0 {
			return 0, false
		}
		return total / float64(count), true
	case "total-points":
		sum := 0.0
		maxTotal := 0.0
		for _, item := range items {
			if portalAssignmentHasEntry(item.Grade) {
				hasEntry = true
			}
			if !countsTowardAssignmentAverage(item.Grade) {
				continue
			}
			maxTotal += float64(item.Grade.MaxPoints)
			sum += (effectiveAssignmentPercent(item.Grade, item.Grade.PassPercent, item.Anchor, item.Lift) / 100) * float64(item.Grade.MaxPoints)
		}
		if !hasEntry || maxTotal == 0 {
			return 0, false
		}
		return (sum / maxTotal) * 100, true
	default:
		if len(items) == 0 {
			return 0, false
		}
		total := 0.0
		count := 0
		for _, item := range items {
			if portalAssignmentHasEntry(item.Grade) {
				hasEntry = true
			}
			if !countsTowardAssignmentAverage(item.Grade) {
				continue
			}
			total += effectiveAssignmentPercent(item.Grade, item.Grade.PassPercent, item.Anchor, item.Lift)
			count++
		}
		if !hasEntry || count == 0 {
			return 0, false
		}
		return total / float64(count), true
	}
}

func portalAssignmentHasEntry(record GradeRecord) bool {
	return record.Score.Valid || record.Flags != 0
}

func portalImprovementTips(assignments []portalAssignmentSnapshot, categories []portalCategorySnapshot, cutoff int) []string {
	var tips []string
	for _, item := range assignments {
		// Skip assignments before the overview cutoff.
		if cutoff > 0 && item.AssignmentID <= cutoff {
			continue
		}
		// Skip assignments in categories hidden from the overview.
		if !item.ShowInOverview {
			continue
		}
		if containsFlag(item.Flags, "missing") {
			tips = append(tips, "Finish missing work: "+item.Title)
		} else if containsFlag(item.Flags, "redo") {
			tips = append(tips, "Redo and resubmit: "+item.Title)
		}
	}
	var lowest *portalCategorySnapshot
	for idx := range categories {
		if !categories[idx].Included {
			continue
		}
		if !categories[idx].ShowInOverview {
			continue
		}
		if lowest == nil || categories[idx].Score < lowest.Score {
			lowest = &categories[idx]
		}
	}
	if lowest != nil {
		tips = append(tips, "Focus first on your lowest active category: "+lowest.CategoryName)
	}
	if len(tips) == 0 {
		tips = append(tips, "Keep entering work in each category so more of your grade becomes active.")
	}
	return tips
}

func containsFlag(flags []string, want string) bool {
	for _, flag := range flags {
		if strings.EqualFold(flag, want) {
			return true
		}
	}
	return false
}

type portalAssignmentDetail struct {
	AssignmentID int
	CategoryID   int
	studentAssignmentDetail
}

func (a *App) portalAssignmentDetails(studentID int) ([]portalAssignmentDetail, error) {
	ctx := a.context()
	rows, err := a.db.Query(`
		SELECT assignments.assignment_id,
		       assignments.category_id,
		       assignments.title,
		       categories.name,
		       COALESCE(category_grading_policies.scheme_key, 'average'),
		       COALESCE(assignment_curves.anchor_percent, 100),
		       COALESCE(assignment_curves.lift_percent, 1),
		       grades.score,
		       COALESCE(grades.flags_bitmask, 0),
		       assignments.max_points,
		       COALESCE(grades.redo_count, 0),
		       COALESCE(assignments.pass_percent, category_grading_policies.default_pass_percent)
		FROM assignments
		JOIN categories ON categories.category_id = assignments.category_id
		LEFT JOIN grades ON grades.assignment_id = assignments.assignment_id AND grades.student_pk = ?
		LEFT JOIN assignment_curves ON assignment_curves.assignment_id = assignments.assignment_id
		LEFT JOIN category_grading_policies
		  ON category_grading_policies.course_year_id = assignments.course_year_id
		 AND category_grading_policies.term_id = assignments.term_id
		 AND category_grading_policies.category_id = assignments.category_id
		WHERE assignments.course_year_id = ? AND assignments.term_id = ?
		ORDER BY assignments.assignment_id`, studentID, ctx.CourseYearID, ctx.TermID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []portalAssignmentDetail
	for rows.Next() {
		var item portalAssignmentDetail
		if err := rows.Scan(&item.AssignmentID, &item.CategoryID, &item.Title, &item.Category, &item.SchemeKey, &item.Anchor, &item.Lift, &item.Grade.Score, &item.Grade.Flags, &item.Grade.MaxPoints, &item.Grade.RedoCount, &item.Grade.PassPercent); err != nil {
			return nil, err
		}
		item.Flags = studentVisibleFlags(item.Grade)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (a *App) ensurePortalAccounts(courseYearID, termID int, defaultPassword string, memorable bool) ([]accountInitResult, error) {
	students, err := a.studentsForCourseTerm(courseYearID, termID, false)
	if err != nil {
		return nil, err
	}
	existingStudents := map[int]bool{}
	rows, err := a.db.Query(`SELECT student_pk FROM student_accounts`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var studentID int
		if err := rows.Scan(&studentID); err != nil {
			return nil, err
		}
		existingStudents[studentID] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	usedUsernames := map[string]bool{}
	userRows, err := a.db.Query(`SELECT username FROM student_accounts`)
	if err != nil {
		return nil, err
	}
	defer userRows.Close()
	for userRows.Next() {
		var username string
		if err := userRows.Scan(&username); err != nil {
			return nil, err
		}
		usedUsernames[strings.ToLower(username)] = true
	}
	if err := userRows.Err(); err != nil {
		return nil, err
	}
	var results []accountInitResult
	for _, student := range students {
		if existingStudents[student.ID] {
			continue
		}
		username := nextPortalUsername(student, usedUsernames)
		usedUsernames[strings.ToLower(username)] = true
		password := strings.TrimSpace(defaultPassword)
		if password == "" {
			password, err = portalauth.RandomOrMemorablePassword(memorable)
			if err != nil {
				return nil, err
			}
		}
		hash, salt, err := portalauth.HashPassword(password)
		if err != nil {
			return nil, err
		}
		changedAt := time.Now().UTC().Format(time.RFC3339)
		if _, err := a.db.Exec(`
			INSERT INTO student_accounts(student_pk, username, password_salt, password_hash, must_change_password, password_changed_at)
			VALUES (?, ?, ?, ?, 0, ?)`,
			student.ID, username, salt, hash, changedAt); err != nil {
			return nil, err
		}
		results = append(results, accountInitResult{
			StudentID: student.ID,
			Name:      strings.TrimSpace(student.FirstName + " " + student.LastName),
			Username:  username,
			Password:  password,
		})
	}
	return results, nil
}

func nextPortalUsername(student Student, used map[string]bool) string {
	candidates := []string{
		normalizePortalUsername(student.FirstName + "." + student.LastName),
		normalizePortalUsername(student.FirstName + student.LastName),
		normalizePortalUsername(student.SchoolStudentID),
		normalizePortalUsername(student.PowerSchoolNum),
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if !used[strings.ToLower(candidate)] {
			return candidate
		}
	}
	base := normalizePortalUsername(student.FirstName + "." + student.LastName)
	if base == "" {
		base = fmt.Sprintf("student%d", student.ID)
	}
	for idx := 2; ; idx++ {
		candidate := fmt.Sprintf("%s%d", base, idx)
		if !used[strings.ToLower(candidate)] {
			return candidate
		}
	}
}

func normalizePortalUsername(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return ""
	}
	var out strings.Builder
	lastDot := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			out.WriteRune(r)
			lastDot = false
		case r == '.', r == '-', r == '_':
			if out.Len() > 0 && !lastDot {
				out.WriteByte('.')
				lastDot = true
			}
		}
	}
	return strings.Trim(out.String(), ".")
}

func (a *App) studentPortalUsername(studentID int) (string, error) {
	var username string
	err := a.db.QueryRow(`SELECT username FROM student_accounts WHERE student_pk = ?`, studentID).Scan(&username)
	if err != nil {
		return "", err
	}
	return username, nil
}

func (a *App) portalAccountsForCourseTerm(courseYearID, termID int) ([]portalauth.Account, error) {
	rows, err := a.db.Query(`
		SELECT sa.student_pk, sa.username, sa.password_salt, sa.password_hash, sa.must_change_password, COALESCE(sa.password_changed_at, '1970-01-01T00:00:00Z')
		FROM student_accounts sa
		JOIN section_enrollments se ON se.student_pk = sa.student_pk
		JOIN sections s ON s.section_id = se.section_id
		WHERE s.course_year_id = ? AND se.term_id = ?
		GROUP BY sa.student_pk
		ORDER BY sa.student_pk`, courseYearID, termID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var accounts []portalauth.Account
	for rows.Next() {
		var acc portalauth.Account
		var mustChange int
		if err := rows.Scan(&acc.StudentID, &acc.Username, &acc.PasswordSalt, &acc.PasswordHash, &mustChange, &acc.PasswordChangedAt); err != nil {
			return nil, err
		}
		acc.MustChangePassword = mustChange != 0
		accounts = append(accounts, acc)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return accounts, nil
}

func (s *portalServer) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/login", s.handleLogin)
	mux.HandleFunc("/api/logout", s.handleLogout)
	mux.HandleFunc("/api/me", s.handleMe)
	mux.HandleFunc("/api/change-password", s.handleChangePassword)
	mux.HandleFunc("/api/grades", s.handleGrades)
	mux.HandleFunc("/", s.handleStatic)
	return mux
}

// handleStatic serves the built React SPA when portal-web/dist is available,
// falling back to index.html for client-side routes. Without a built frontend
// it serves the legacy template page instead.
func (s *portalServer) handleStatic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Unknown API paths get a JSON 404 so the frontend never parses the
	// index.html fallback as an API response.
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeJSONError(w, http.StatusNotFound, "not found")
		return
	}
	if s.staticDir == "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = portalPageTemplate.Execute(w, map[string]any{"Title": "Student Grades Portal"})
		return
	}
	path := filepath.Join(s.staticDir, r.URL.Path)
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		path = filepath.Join(s.staticDir, "index.html")
	}
	http.ServeFile(w, r, path)
}

func (s *portalServer) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var payload struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid login payload")
		return
	}
	studentID, username, mustChange, err := s.authenticate(payload.Username, payload.Password)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	token, err := s.startSession(studentID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "could not start session")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "grades_session", Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "studentId": studentID, "username": username, "mustChangePassword": mustChange})
}

func (s *portalServer) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.clearSession(r)
	http.SetCookie(w, &http.Cookie{Name: "grades_session", Value: "", Path: "/", HttpOnly: true, MaxAge: -1, SameSite: http.SameSiteLaxMode})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *portalServer) handleMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	studentID, err := s.requireSession(r)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "login required")
		return
	}
	username, mustChange, err := s.accountInfo(studentID)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "could not load account")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"studentId": studentID, "username": username, "mustChangePassword": mustChange})
}

func (s *portalServer) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	studentID, err := s.requireSession(r)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "login required")
		return
	}
	var payload struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid password payload")
		return
	}
	if len(strings.TrimSpace(payload.NewPassword)) < 8 {
		writeJSONError(w, http.StatusBadRequest, "new password must be at least 8 characters")
		return
	}
	ok, err := s.verifyStudentPassword(studentID, payload.CurrentPassword)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "could not verify password")
		return
	}
	if !ok {
		writeJSONError(w, http.StatusUnauthorized, "current password is incorrect")
		return
	}
	hash, salt, err := portalauth.HashPassword(payload.NewPassword)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "could not update password")
		return
	}
	if _, err := s.app.db.Exec(`
		UPDATE student_accounts
		SET password_salt = ?, password_hash = ?, must_change_password = 0,
		    password_changed_at = strftime('%Y-%m-%dT%H:%M:%fZ','now'),
		    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')
		WHERE student_pk = ?`, salt, hash, studentID); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "could not update password")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleGrades serves the student's grade snapshots for every course they are
// enrolled in, built live from the local database. The response shape matches
// the VPS portal server: {"studentId": N, "courses": [...]}.
func (s *portalServer) handleGrades(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	studentID, err := s.requireSession(r)
	if err != nil {
		writeJSONError(w, http.StatusUnauthorized, "login required")
		return
	}
	s.buildMu.Lock()
	courses, err := s.app.portalCoursesForStudent(studentID)
	s.buildMu.Unlock()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "could not load grades")
		return
	}
	if courses == nil {
		courses = []portalStudentCourse{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"studentId": studentID, "courses": courses})
}

func (s *portalServer) authenticate(username, password string) (int, string, bool, error) {
	var studentID int
	var dbUsername, salt, hash string
	var mustChange int
	err := s.app.db.QueryRow(`
		SELECT student_pk, username, password_salt, password_hash, must_change_password
		FROM student_accounts
		WHERE lower(username) = lower(?)`, strings.TrimSpace(username)).Scan(&studentID, &dbUsername, &salt, &hash, &mustChange)
	if err != nil {
		return 0, "", false, err
	}
	if !portalauth.VerifyPassword(password, salt, hash) {
		return 0, "", false, errors.New("invalid password")
	}
	return studentID, dbUsername, mustChange != 0, nil
}

func (s *portalServer) verifyStudentPassword(studentID int, password string) (bool, error) {
	var salt, hash string
	err := s.app.db.QueryRow(`SELECT password_salt, password_hash FROM student_accounts WHERE student_pk = ?`, studentID).Scan(&salt, &hash)
	if err != nil {
		return false, err
	}
	return portalauth.VerifyPassword(password, salt, hash), nil
}

func (s *portalServer) accountInfo(studentID int) (string, bool, error) {
	var username string
	var mustChange int
	err := s.app.db.QueryRow(`SELECT username, must_change_password FROM student_accounts WHERE student_pk = ?`, studentID).Scan(&username, &mustChange)
	if err != nil {
		return "", false, err
	}
	return username, mustChange != 0, nil
}

func (s *portalServer) startSession(studentID int) (string, error) {
	buffer := make([]byte, 24)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(buffer)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[token] = portalSession{StudentID: studentID, ExpiresAt: s.now().Add(24 * time.Hour)}
	return token, nil
}

func (s *portalServer) requireSession(r *http.Request) (int, error) {
	cookie, err := r.Cookie("grades_session")
	if err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[cookie.Value]
	if !ok {
		return 0, errors.New("session not found")
	}
	if session.ExpiresAt.Before(s.now()) {
		delete(s.sessions, cookie.Value)
		return 0, errors.New("session expired")
	}
	return session.StudentID, nil
}

func (s *portalServer) clearSession(r *http.Request) {
	cookie, err := r.Cookie("grades_session")
	if err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, cookie.Value)
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": message})
}
