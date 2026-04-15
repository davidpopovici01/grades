package app

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type portalStudentSnapshot struct {
	StudentID           int                        `json:"studentId"`
	FirstName           string                     `json:"firstName"`
	LastName            string                     `json:"lastName"`
	ChineseName         string                     `json:"chineseName,omitempty"`
	SchoolStudentID     string                     `json:"schoolStudentId,omitempty"`
	CourseName          string                     `json:"courseName"`
	TermName            string                     `json:"termName"`
	Sections            []string                   `json:"sections"`
	Username            string                     `json:"username,omitempty"`
	WeightedTotal       float64                    `json:"weightedTotal"`
	WeightedTotalLabel  string                     `json:"weightedTotalLabel"`
	GPA                 float64                    `json:"gpa"`
	GPALabel            string                     `json:"gpaLabel"`
	ActiveCategoryCount int                        `json:"activeCategoryCount"`
	Categories          []portalCategorySnapshot   `json:"categories"`
	Assignments         []portalAssignmentSnapshot `json:"assignments"`
	ImprovementTips     []string                   `json:"improvementTips"`
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
	CurrentPercent      float64  `json:"currentPercent"`
	CurrentPercentLabel string   `json:"currentPercentLabel"`
}

type portalCourseSnapshot struct {
	CourseYearID int                     `json:"courseYearId"`
	TermID       int                     `json:"termId"`
	CourseName   string                  `json:"courseName"`
	TermName     string                  `json:"termName"`
	PublishedAt  string                  `json:"publishedAt"`
	Students     []portalStudentSnapshot `json:"students"`
}

type portalSession struct {
	StudentID int
	ExpiresAt time.Time
}

type portalServer struct {
	app        *App
	publishDir string
	now        func() time.Time
	mu         sync.Mutex
	sessions   map[string]portalSession
}

type accountInitResult struct {
	StudentID int
	Name      string
	Username  string
	Password  string
}

func (a *App) PublishStudentPortal(dir string) error {
	ctx := a.context()
	if ctx.TermID == 0 || ctx.CourseYearID == 0 {
		return errors.New("set year, term, and course first")
	}
	publishDir, err := a.portalPublishDir(dir)
	if err != nil {
		return err
	}
	snapshot, err := a.buildPortalCourseSnapshot(ctx.CourseYearID, ctx.TermID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(publishDir, "students"), 0o755); err != nil {
		return err
	}
	indexPayload := struct {
		CourseYearID int    `json:"courseYearId"`
		TermID       int    `json:"termId"`
		CourseName   string `json:"courseName"`
		TermName     string `json:"termName"`
		PublishedAt  string `json:"publishedAt"`
		StudentCount int    `json:"studentCount"`
	}{
		CourseYearID: snapshot.CourseYearID,
		TermID:       snapshot.TermID,
		CourseName:   snapshot.CourseName,
		TermName:     snapshot.TermName,
		PublishedAt:  snapshot.PublishedAt,
		StudentCount: len(snapshot.Students),
	}
	if err := writeJSONFile(filepath.Join(publishDir, "index.json"), indexPayload); err != nil {
		return err
	}
	for _, student := range snapshot.Students {
		if err := writeJSONFile(filepath.Join(publishDir, "students", strconv.Itoa(student.StudentID)+".json"), student); err != nil {
			return err
		}
	}
	fmt.Fprintf(a.out, "Published student portal to %s\n", publishDir)
	fmt.Fprintf(a.out, "Published %d student snapshot(s)\n", len(snapshot.Students))
	return nil
}

func (a *App) InitStudentPortalAccounts(defaultPassword string) error {
	ctx := a.context()
	if ctx.TermID == 0 || ctx.CourseYearID == 0 {
		return errors.New("set year, term, and course first")
	}
	results, err := a.ensurePortalAccounts(ctx.CourseYearID, ctx.TermID, defaultPassword)
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

func (a *App) ResetStudentPortalPassword(studentRef, password string) error {
	if err := a.ensureStudentCommandContext(); err != nil {
		return err
	}
	student, err := a.resolveStudentReference(studentRef)
	if err != nil {
		return err
	}
	if strings.TrimSpace(password) == "" {
		password, err = randomPassword(12)
		if err != nil {
			return err
		}
	}
	hash, salt, err := hashPortalPassword(password)
	if err != nil {
		return err
	}
	username, err := a.studentPortalUsername(student.ID)
	if err != nil {
		username = nextPortalUsername(student, map[string]bool{})
	}
	_, err = a.db.Exec(`
		INSERT INTO student_accounts(student_pk, username, password_salt, password_hash, must_change_password, updated_at)
		VALUES (?, ?, ?, ?, 1, strftime('%Y-%m-%dT%H:%M:%fZ','now'))
		ON CONFLICT(student_pk) DO UPDATE SET
			username = excluded.username,
			password_salt = excluded.password_salt,
			password_hash = excluded.password_hash,
			must_change_password = excluded.must_change_password,
			updated_at = excluded.updated_at`,
		student.ID, username, salt, hash)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Reset portal password for %s %s\n", student.FirstName, student.LastName)
	fmt.Fprintf(a.out, "Username:\t%s\n", username)
	fmt.Fprintf(a.out, "Temporary password:\t%s\n", password)
	return nil
}

func (a *App) ServeStudentPortal(addr, dir string) error {
	ctx := a.context()
	if ctx.TermID == 0 || ctx.CourseYearID == 0 {
		return errors.New("set year, term, and course first")
	}
	if strings.TrimSpace(addr) == "" {
		addr = "127.0.0.1:8080"
	}
	created, err := a.ensurePortalAccounts(ctx.CourseYearID, ctx.TermID, "")
	if err != nil {
		return err
	}
	if len(created) > 0 {
		fmt.Fprintln(a.out, "Created student portal accounts")
		for _, item := range created {
			fmt.Fprintf(a.out, "%s\t%s\t%s\n", item.Name, item.Username, item.Password)
		}
	}
	if err := a.PublishStudentPortal(dir); err != nil {
		return err
	}
	publishDir, err := a.portalPublishDir(dir)
	if err != nil {
		return err
	}
	server := &portalServer{app: a, publishDir: publishDir, now: time.Now, sessions: map[string]portalSession{}}
	fmt.Fprintf(a.out, "Student portal serving at http://%s\n", addr)
	return http.ListenAndServe(addr, server.routes())
}

func (a *App) portalPublishDir(dir string) (string, error) {
	if strings.TrimSpace(dir) != "" {
		return filepath.Clean(dir), nil
	}
	return filepath.Clean(filepath.Join(a.homeDir, "..", "gradesPublished")), nil
}

func (a *App) buildPortalCourseSnapshot(courseYearID, termID int) (portalCourseSnapshot, error) {
	var snapshot portalCourseSnapshot
	snapshot.CourseYearID = courseYearID
	snapshot.TermID = termID
	if err := a.db.QueryRow(`
		SELECT course_years.name, terms.name
		FROM course_years
		JOIN course_year_terms ON course_year_terms.course_year_id = course_years.course_year_id
		JOIN terms ON terms.term_id = course_year_terms.term_id
		WHERE course_years.course_year_id = ? AND terms.term_id = ?`,
		courseYearID, termID).Scan(&snapshot.CourseName, &snapshot.TermName); err != nil {
		return portalCourseSnapshot{}, err
	}
	snapshot.CourseName = baseCourseName(snapshot.CourseName)
	snapshot.PublishedAt = time.Now().UTC().Format(time.RFC3339)
	students, err := a.studentsForCourseTerm(courseYearID, termID, false)
	if err != nil {
		return portalCourseSnapshot{}, err
	}
	rules, err := a.categoryRulesForContext(courseYearID, termID)
	if err != nil {
		return portalCourseSnapshot{}, err
	}
	for _, student := range students {
		item, err := a.buildPortalStudentSnapshot(snapshot.CourseName, snapshot.TermName, courseYearID, termID, student, rules)
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

func (a *App) buildPortalStudentSnapshot(courseName, termName string, courseYearID, termID int, student Student, rules []CategoryRule) (portalStudentSnapshot, error) {
	ctx := a.context()
	defer func(previous Context) {
		a.v.Set("context.year", previous.Year)
		a.v.Set("context.term_id", previous.TermID)
		a.v.Set("context.course_year_id", previous.CourseYearID)
		a.v.Set("context.section_id", previous.SectionID)
		a.v.Set("context.assignment_id", previous.AssignmentID)
	}(ctx)
	a.v.Set("context.term_id", termID)
	a.v.Set("context.course_year_id", courseYearID)
	a.v.Set("context.section_id", 0)
	a.v.Set("context.assignment_id", 0)

	details, err := a.portalAssignmentDetails(student.ID)
	if err != nil {
		return portalStudentSnapshot{}, err
	}
	sections, err := a.studentSectionsForPortal(student.ID, courseYearID, termID)
	if err != nil {
		return portalStudentSnapshot{}, err
	}
	categorySnapshots, weightedTotal, weightedLabel, activeCategoryCount := portalCategorySnapshots(rules, details)
	username, _ := a.studentPortalUsername(student.ID)
	item := portalStudentSnapshot{
		StudentID:           student.ID,
		FirstName:           student.FirstName,
		LastName:            student.LastName,
		ChineseName:         student.ChineseName,
		SchoolStudentID:     student.SchoolStudentID,
		CourseName:          courseName,
		TermName:            termName,
		Sections:            sections,
		Username:            username,
		WeightedTotal:       weightedTotal,
		WeightedTotalLabel:  weightedLabel,
		GPA:                 weightedTotal,
		GPALabel:            weightedLabel,
		ActiveCategoryCount: activeCategoryCount,
		Categories:          categorySnapshots,
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
		currentPercent := recordPercent(detail.Grade, detail.Anchor, detail.Lift)
		if detail.SchemeKey == "completion" {
			currentPercent = completionPercent(detail.Grade, detail.Grade.PassPercent, detail.Anchor, detail.Lift)
		}
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
			CurrentPercent:      currentPercent,
			CurrentPercentLabel: fmt.Sprintf("%.1f%%", currentPercent),
		})
	}
	item.ImprovementTips = portalImprovementTips(item.Assignments, item.Categories)
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
		for _, item := range items {
			if portalAssignmentHasEntry(item.Grade) {
				hasEntry = true
			}
			total += completionPercent(item.Grade, item.Grade.PassPercent, item.Anchor, item.Lift)
		}
		if !hasEntry {
			return 0, false
		}
		return total / float64(len(items)), true
	case "total-points":
		sum := 0.0
		maxTotal := 0.0
		for _, item := range items {
			if portalAssignmentHasEntry(item.Grade) {
				hasEntry = true
			}
			maxTotal += float64(item.Grade.MaxPoints)
			sum += (recordPercent(item.Grade, item.Anchor, item.Lift) / 100) * float64(item.Grade.MaxPoints)
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
		for _, item := range items {
			if portalAssignmentHasEntry(item.Grade) {
				hasEntry = true
			}
			total += recordPercent(item.Grade, item.Anchor, item.Lift)
		}
		if !hasEntry {
			return 0, false
		}
		return total / float64(len(items)), true
	}
}

func portalAssignmentHasEntry(record GradeRecord) bool {
	return record.Score.Valid || record.Flags != 0
}

func portalImprovementTips(assignments []portalAssignmentSnapshot, categories []portalCategorySnapshot) []string {
	var tips []string
	for _, item := range assignments {
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

func (a *App) ensurePortalAccounts(courseYearID, termID int, defaultPassword string) ([]accountInitResult, error) {
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
			password, err = randomPassword(12)
			if err != nil {
				return nil, err
			}
		}
		hash, salt, err := hashPortalPassword(password)
		if err != nil {
			return nil, err
		}
		if _, err := a.db.Exec(`
			INSERT INTO student_accounts(student_pk, username, password_salt, password_hash, must_change_password)
			VALUES (?, ?, ?, ?, 1)`,
			student.ID, username, salt, hash); err != nil {
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
		normalizePortalUsername(student.SchoolStudentID),
		normalizePortalUsername(student.PowerSchoolNum),
		normalizePortalUsername(student.FirstName + "." + student.LastName),
		normalizePortalUsername(student.FirstName + student.LastName),
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

func randomPassword(length int) (string, error) {
	if length < 8 {
		length = 8
	}
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"
	buffer := make([]byte, length)
	random := make([]byte, length)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	for idx := range buffer {
		buffer[idx] = alphabet[int(random[idx])%len(alphabet)]
	}
	return string(buffer), nil
}

func hashPortalPassword(password string) (hash string, salt string, err error) {
	saltBytes := make([]byte, 16)
	if _, err := rand.Read(saltBytes); err != nil {
		return "", "", err
	}
	return derivePortalPassword(password, saltBytes), hex.EncodeToString(saltBytes), nil
}

func derivePortalPassword(password string, salt []byte) string {
	mac := hmac.New(sha256.New, salt)
	_, _ = mac.Write([]byte(password))
	derived := mac.Sum(nil)
	for idx := 0; idx < 120000; idx++ {
		next := hmac.New(sha256.New, salt)
		_, _ = next.Write(derived)
		derived = next.Sum(nil)
	}
	return hex.EncodeToString(derived)
}

func verifyPortalPassword(password, saltHex, expected string) bool {
	salt, err := hex.DecodeString(saltHex)
	if err != nil {
		return false
	}
	actual := derivePortalPassword(password, salt)
	return subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) == 1
}

func (a *App) studentPortalUsername(studentID int) (string, error) {
	var username string
	err := a.db.QueryRow(`SELECT username FROM student_accounts WHERE student_pk = ?`, studentID).Scan(&username)
	if err != nil {
		return "", err
	}
	return username, nil
}

func writeJSONFile(path string, data any) error {
	bytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	bytes = append(bytes, '\n')
	return os.WriteFile(path, bytes, 0o644)
}

func (s *portalServer) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/login", s.handleIndex)
	mux.HandleFunc("/what-if", s.handleIndex)
	mux.HandleFunc("/api/login", s.handleLogin)
	mux.HandleFunc("/api/logout", s.handleLogout)
	mux.HandleFunc("/api/me", s.handleMe)
	mux.HandleFunc("/api/change-password", s.handleChangePassword)
	mux.HandleFunc("/api/grades", s.handleGrades)
	return mux
}

func (s *portalServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = portalPageTemplate.Execute(w, map[string]any{"Title": "Student Grades Portal"})
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
	studentID, mustChange, err := s.authenticate(payload.Username, payload.Password)
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
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "mustChangePassword": mustChange})
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
	hash, salt, err := hashPortalPassword(payload.NewPassword)
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
	path := filepath.Join(s.publishDir, "students", strconv.Itoa(studentID)+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "published student snapshot not found")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(data)
}

func (s *portalServer) authenticate(username, password string) (int, bool, error) {
	var studentID int
	var salt, hash string
	var mustChange int
	err := s.app.db.QueryRow(`
		SELECT student_pk, password_salt, password_hash, must_change_password
		FROM student_accounts
		WHERE lower(username) = lower(?)`, strings.TrimSpace(username)).Scan(&studentID, &salt, &hash, &mustChange)
	if err != nil {
		return 0, false, err
	}
	if !verifyPortalPassword(password, salt, hash) {
		return 0, false, errors.New("invalid password")
	}
	return studentID, mustChange != 0, nil
}

func (s *portalServer) verifyStudentPassword(studentID int, password string) (bool, error) {
	var salt, hash string
	err := s.app.db.QueryRow(`SELECT password_salt, password_hash FROM student_accounts WHERE student_pk = ?`, studentID).Scan(&salt, &hash)
	if err != nil {
		return false, err
	}
	return verifyPortalPassword(password, salt, hash), nil
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
