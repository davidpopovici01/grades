package app

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/term"
)

// RubricCriterion is one graded row of a rubric.
type RubricCriterion struct {
	Name      string `json:"name"`
	MaxPoints int    `json:"max_points"`
}

// Rubric is the parsed structure of a rubric DOCX template.
type Rubric struct {
	Title    string
	Criteria []RubricCriterion
}

// Total returns the maximum score across all criteria.
func (r Rubric) Total() int {
	total := 0
	for _, criterion := range r.Criteria {
		total += criterion.MaxPoints
	}
	return total
}

type linkedRubric struct {
	Rubric
	Template     []byte
	TemplateName string
}

var (
	rubricTextRunRE      = regexp.MustCompile(`(?s)<w:t(?:\s[^>]*)?>(.*?)</w:t>`)
	rubricPointsBlankRE  = regexp.MustCompile(`^_+\s*/\s*(\d+)$`)
	rubricNameBlankRE    = regexp.MustCompile(`Name:\s*_+`)
	rubricTotalBlankRE   = regexp.MustCompile(`Total:\s*_+`)
	rubricUnderscoreRE   = regexp.MustCompile(`_+`)
	errRubricCancelled   = errors.New("rubric entry cancelled")
	errRubricNotTerminal = errors.New("stdin is not a terminal")
)

// ParseRubricDocx extracts the title and criteria (with per-criterion max
// points) from a rubric DOCX. Criteria rows are detected by a `____/N` blank
// in the table column headed "Points".
func ParseRubricDocx(docxBytes []byte) (Rubric, error) {
	doc, err := readDocxDocumentXML(docxBytes)
	if err != nil {
		return Rubric{}, err
	}
	rubric := Rubric{Title: parseRubricTitle(doc)}
	rows := rowPattern.FindAllString(doc, -1)
	headerRow, pointsCol := rubricPointsColumn(rows)
	if headerRow < 0 {
		return Rubric{}, errors.New("rubric table header with a Points column not found")
	}
	for _, row := range rows[headerRow+1:] {
		cells := cellPattern.FindAllString(row, -1)
		if pointsCol >= len(cells) || len(cells) == 0 {
			continue
		}
		match := rubricPointsBlankRE.FindStringSubmatch(normalizeSpaces(docxCellText(cells[pointsCol])))
		if match == nil {
			continue
		}
		maxPoints, err := strconv.Atoi(match[1])
		if err != nil || maxPoints <= 0 {
			continue
		}
		name := normalizeSpaces(docxCellText(cells[0]))
		if name == "" {
			continue
		}
		rubric.Criteria = append(rubric.Criteria, RubricCriterion{Name: name, MaxPoints: maxPoints})
	}
	if len(rubric.Criteria) == 0 {
		return Rubric{}, errors.New("no rubric criteria found (rows need a ____/N blank in the Points column)")
	}
	return rubric, nil
}

func readDocxDocumentXML(docxBytes []byte) (string, error) {
	reader, err := zip.NewReader(bytes.NewReader(docxBytes), int64(len(docxBytes)))
	if err != nil {
		return "", fmt.Errorf("not a valid docx file: %w", err)
	}
	for _, file := range reader.File {
		if file.Name != "word/document.xml" {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			return "", err
		}
		body, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			return "", err
		}
		return string(body), nil
	}
	return "", errors.New("word/document.xml not found in docx file")
}

func rubricPointsColumn(rows []string) (headerRow, pointsCol int) {
	for i, row := range rows {
		cells := cellPattern.FindAllString(row, -1)
		for j, cell := range cells {
			if strings.EqualFold(normalizeSpaces(docxCellText(cell)), "Points") {
				return i, j
			}
		}
	}
	return -1, -1
}

func docxCellText(chunk string) string {
	matches := rubricTextRunRE.FindAllStringSubmatch(chunk, -1)
	var text strings.Builder
	for _, match := range matches {
		text.WriteString(match[1])
	}
	return xmlUnescape(text.String())
}

func xmlUnescape(s string) string {
	return strings.NewReplacer(
		"&lt;", "<",
		"&gt;", ">",
		"&quot;", `"`,
		"&apos;", "'",
		"&amp;", "&",
	).Replace(s)
}

func xmlEscape(s string) string {
	var escaped bytes.Buffer
	_ = xml.EscapeText(&escaped, []byte(s))
	return escaped.String()
}

func parseRubricTitle(doc string) string {
	end := strings.Index(doc, "</w:p>")
	if end < 0 {
		return ""
	}
	text := normalizeSpaces(docxCellText(doc[:end]))
	if idx := strings.Index(text, "Name:"); idx >= 0 {
		text = strings.TrimSpace(text[:idx])
	}
	return text
}

// FillRubricDocx fills the student name, per-criterion points, and total into
// a rubric DOCX template and returns the finished document.
func FillRubricDocx(template []byte, studentName string, scores []int) ([]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(template), int64(len(template)))
	if err != nil {
		return nil, fmt.Errorf("not a valid docx file: %w", err)
	}
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for _, file := range reader.File {
		header := file.FileHeader
		out, err := writer.CreateHeader(&header)
		if err != nil {
			return nil, err
		}
		rc, err := file.Open()
		if err != nil {
			return nil, err
		}
		if file.Name == "word/document.xml" {
			body, err := io.ReadAll(rc)
			_ = rc.Close()
			if err != nil {
				return nil, err
			}
			updated, err := fillRubricDocument(string(body), studentName, scores)
			if err != nil {
				return nil, err
			}
			if _, err := io.Copy(out, strings.NewReader(updated)); err != nil {
				return nil, err
			}
			continue
		}
		if _, err := io.Copy(out, rc); err != nil {
			_ = rc.Close()
			return nil, err
		}
		_ = rc.Close()
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func fillRubricDocument(doc, studentName string, scores []int) (string, error) {
	total := 0
	for _, score := range scores {
		total += score
	}
	doc = rubricNameBlankRE.ReplaceAllStringFunc(doc, func(match string) string {
		return blankFillPrefix(match) + xmlEscape(studentName)
	})
	doc = rubricTotalBlankRE.ReplaceAllStringFunc(doc, func(match string) string {
		return blankFillPrefix(match) + strconv.Itoa(total)
	})

	rows := rowPattern.FindAllString(doc, -1)
	headerRow, pointsCol := rubricPointsColumn(rows)
	if headerRow < 0 {
		return "", errors.New("rubric table header with a Points column not found")
	}
	criterion := 0
	for i := headerRow + 1; i < len(rows) && criterion < len(scores); i++ {
		cells := cellPattern.FindAllString(rows[i], -1)
		if pointsCol >= len(cells) {
			continue
		}
		if !rubricPointsBlankRE.MatchString(normalizeSpaces(docxCellText(cells[pointsCol]))) {
			continue
		}
		filled := rubricUnderscoreRE.ReplaceAllStringFunc(cells[pointsCol], func(string) string {
			return strconv.Itoa(scores[criterion])
		})
		rows[i] = strings.Replace(rows[i], cells[pointsCol], filled, 1)
		criterion++
	}
	if criterion < len(scores) {
		return "", fmt.Errorf("rubric template has %d score blanks but %d scores were provided", criterion, len(scores))
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

// blankFillPrefix keeps the label ("Name:"/"Total:") of a matched blank and
// guarantees a space before the filled value.
func blankFillPrefix(match string) string {
	prefix := strings.TrimRight(match, "_")
	if !strings.HasSuffix(prefix, " ") {
		prefix += " "
	}
	return prefix
}

// LinkRubric parses a rubric DOCX and links it to the current assignment.
func (a *App) LinkRubric(path string) error {
	ctx := a.context()
	if ctx.AssignmentID == 0 || ctx.TermID == 0 || ctx.CourseYearID == 0 {
		return errors.New("set year, term, course, and assignment first")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	rubric, err := ParseRubricDocx(data)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	meta, err := a.assignmentScoreMeta(ctx.AssignmentID)
	if err != nil {
		return err
	}

	fmt.Fprintf(a.out, "Rubric:\t%s\n", rubric.Title)
	for i, criterion := range rubric.Criteria {
		fmt.Fprintf(a.out, "%d\t%s\t/%d\n", i+1, criterion.Name, criterion.MaxPoints)
	}
	fmt.Fprintf(a.out, "Total:\t/%d\n", rubric.Total())
	ok, err := a.promptYesNo(fmt.Sprintf(`Link this rubric to "%s"?`, meta.Title), true)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Fprintln(a.out, "Rubric not linked.")
		return nil
	}

	criteriaJSON, err := json.Marshal(rubric.Criteria)
	if err != nil {
		return err
	}
	title := rubric.Title
	if title == "" {
		title = meta.Title
	}
	_, err = a.db.Exec(`
		INSERT INTO assignment_rubrics(assignment_id, title, criteria_json, template_docx, template_name, updated_at)
		VALUES (?, ?, ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%fZ','now'))
		ON CONFLICT(assignment_id) DO UPDATE SET
			title = excluded.title,
			criteria_json = excluded.criteria_json,
			template_docx = excluded.template_docx,
			template_name = excluded.template_name,
			updated_at = excluded.updated_at`,
		ctx.AssignmentID, title, string(criteriaJSON), data, filepath.Base(path))
	if err != nil {
		return err
	}
	fmt.Fprintf(a.out, "Linked rubric to %s.\n", meta.Title)

	if meta.MaxPoints != rubric.Total() {
		update, err := a.promptYesNo(fmt.Sprintf("Assignment max is %d but the rubric total is %d. Set assignment max to %d?", meta.MaxPoints, rubric.Total(), rubric.Total()), true)
		if err != nil {
			return err
		}
		if update {
			if _, err := a.db.Exec(`UPDATE assignments SET max_points = ? WHERE assignment_id = ?`, rubric.Total(), ctx.AssignmentID); err != nil {
				return err
			}
			fmt.Fprintf(a.out, "Set assignment max to %d.\n", rubric.Total())
		}
	}
	return nil
}

func (a *App) loadLinkedRubric(assignmentID int) (*linkedRubric, error) {
	var title, criteriaJSON, templateName string
	var template []byte
	err := a.db.QueryRow(`
		SELECT title, criteria_json, template_docx, template_name
		FROM assignment_rubrics
		WHERE assignment_id = ?`, assignmentID).Scan(&title, &criteriaJSON, &template, &templateName)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var criteria []RubricCriterion
	if err := json.Unmarshal([]byte(criteriaJSON), &criteria); err != nil {
		return nil, err
	}
	return &linkedRubric{
		Rubric:       Rubric{Title: title, Criteria: criteria},
		Template:     template,
		TemplateName: templateName,
	}, nil
}

func (a *App) rubricScoresFor(assignmentID, studentID int) ([]int, string, error) {
	var scoresJSON string
	var filePath sql.NullString
	err := a.db.QueryRow(`
		SELECT scores_json, file_path
		FROM rubric_scores
		WHERE assignment_id = ? AND student_pk = ?`, assignmentID, studentID).Scan(&scoresJSON, &filePath)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	var scores []int
	if err := json.Unmarshal([]byte(scoresJSON), &scores); err != nil {
		return nil, "", err
	}
	return scores, filePath.String, nil
}

func (a *App) saveRubricScores(assignmentID, studentID int, scores []int, total float64, filePath string) error {
	scoresJSON, err := json.Marshal(scores)
	if err != nil {
		return err
	}
	_, err = a.db.Exec(`
		INSERT INTO rubric_scores(assignment_id, student_pk, scores_json, total, file_path, updated_at)
		VALUES (?, ?, ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%fZ','now'))
		ON CONFLICT(assignment_id, student_pk) DO UPDATE SET
			scores_json = excluded.scores_json,
			total = excluded.total,
			file_path = excluded.file_path,
			updated_at = excluded.updated_at`,
		assignmentID, studentID, string(scoresJSON), total, filePath)
	return err
}

// RubricEnter grades students on the rubric linked to the current assignment,
// writes a filled DOCX per student, and records the rubric total as the
// student's grade.
func (a *App) RubricEnter() error {
	ctx := a.context()
	if ctx.AssignmentID == 0 || ctx.TermID == 0 || ctx.CourseYearID == 0 {
		return errors.New("set year, term, course, and assignment first")
	}
	rubric, err := a.loadLinkedRubric(ctx.AssignmentID)
	if err != nil {
		return err
	}
	if rubric == nil {
		return errors.New("no rubric linked to the current assignment; run: grades rubric link <file.docx>")
	}
	meta, err := a.assignmentScoreMeta(ctx.AssignmentID)
	if err != nil {
		return err
	}
	students, scope, err := a.studentsForList()
	if err != nil {
		return err
	}
	if len(students) == 0 {
		fmt.Fprintln(a.out, "No students enrolled.")
		return nil
	}
	if scope != "" {
		fmt.Fprintln(a.out, scope)
	}
	fmt.Fprintf(a.out, "Rubric: %s (%d criteria, /%d)\n", rubric.Title, len(rubric.Criteria), rubric.Total())

	for {
		query, err := a.prompt("Search")
		if err != nil {
			if errors.Is(err, io.EOF) {
				fmt.Fprintln(a.out, "Finished rubric entry.")
				return nil
			}
			return err
		}
		switch strings.ToLower(strings.TrimSpace(query)) {
		case "", "done", "quit", "q", "exit":
			fmt.Fprintln(a.out, "Finished rubric entry.")
			return nil
		}

		student, err := matchStudent(students, map[int]bool{}, query)
		if err != nil {
			fmt.Fprintln(a.out, err.Error())
			continue
		}
		fmt.Fprintf(a.out, "Matched: %s %s\n", student.FirstName, student.LastName)

		existing, existingPath, err := a.rubricScoresFor(ctx.AssignmentID, student.ID)
		if err != nil {
			return err
		}
		if existing != nil {
			existingTotal := 0
			for _, score := range existing {
				existingTotal += score
			}
			fmt.Fprintf(a.out, "%s already has a rubric (%d/%d", colorOrange(student.FirstName+" "+student.LastName), existingTotal, rubric.Total())
			if existingPath != "" {
				fmt.Fprintf(a.out, ", %s", existingPath)
			}
			fmt.Fprintln(a.out, "). Re-enter scores to update it.")
		}

		prev, err := a.currentGrade(ctx.AssignmentID, student.ID)
		if err != nil {
			return err
		}
		flags := 0
		if prev != nil {
			flags = prev.Flags & (flagLate | flagRedo)
		}

		scores, flags, err := a.rubricScoresPrompt(rubric.Rubric, existing, flags, student.FirstName+" "+student.LastName)
		if errors.Is(err, errRubricCancelled) {
			fmt.Fprintf(a.out, "Skipped %s %s.\n", student.FirstName, student.LastName)
			continue
		}
		if err != nil {
			return err
		}

		total := 0
		for _, score := range scores {
			total += score
		}
		totalValue := float64(total)
		if err := a.saveGrade(ctx.AssignmentID, student.ID, gradeEntry{Score: &totalValue, Flags: flags}, prev); err != nil {
			return err
		}

		filled, err := FillRubricDocx(rubric.Template, student.FirstName+" "+student.LastName, scores)
		if err != nil {
			return err
		}
		outputPath := filepath.Join(a.homeDir, "rubrics", sanitizeFilename(meta.Title),
			sanitizeFilename(student.LastName+"_"+student.FirstName)+".docx")
		if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(outputPath, filled, 0o644); err != nil {
			return err
		}
		if err := a.saveRubricScores(ctx.AssignmentID, student.ID, scores, totalValue, outputPath); err != nil {
			return err
		}
		fmt.Fprintf(a.out, "Saved %s (total %d/%d)\n", outputPath, total, rubric.Total())
	}
}

// RubricList shows which roster students already have a rubric for the
// current assignment.
func (a *App) RubricList() error {
	ctx := a.context()
	if ctx.AssignmentID == 0 || ctx.TermID == 0 || ctx.CourseYearID == 0 {
		return errors.New("set year, term, course, and assignment first")
	}
	rubric, err := a.loadLinkedRubric(ctx.AssignmentID)
	if err != nil {
		return err
	}
	if rubric == nil {
		return errors.New("no rubric linked to the current assignment; run: grades rubric link <file.docx>")
	}
	students, scope, err := a.studentsForList()
	if err != nil {
		return err
	}
	if scope != "" {
		fmt.Fprintln(a.out, scope)
	}
	nameWidth := 0
	for _, student := range students {
		nameWidth = max(nameWidth, visibleWidth(student.FirstName+" "+student.LastName))
	}
	for _, student := range students {
		name := student.FirstName + " " + student.LastName
		scores, _, err := a.rubricScoresFor(ctx.AssignmentID, student.ID)
		if err != nil {
			return err
		}
		if scores == nil {
			fmt.Fprintf(a.out, "%s  %s\n", padVisibleRight(name, nameWidth), "—")
			continue
		}
		total := 0
		for _, score := range scores {
			total += score
		}
		fmt.Fprintf(a.out, "%s  %s\n", padVisibleRight(name, nameWidth), colorGreen(fmt.Sprintf("%d/%d", total, rubric.Total())))
	}
	return nil
}

// rubricScoresPrompt collects per-criterion scores, using the interactive
// arrow-key sheet on a terminal and plain line prompts otherwise.
func (a *App) rubricScoresPrompt(rubric Rubric, existing []int, flags int, studentName string) ([]int, int, error) {
	if a.stdinFile != nil && term.IsTerminal(int(a.stdinFile.Fd())) {
		scores, flags, err := a.rubricScoreSheet(rubric, existing, flags, studentName)
		if !errors.Is(err, errRubricNotTerminal) {
			return scores, flags, err
		}
	}
	return a.rubricScoreLines(rubric, existing, flags)
}

func (a *App) rubricScoreLines(rubric Rubric, existing []int, flags int) ([]int, int, error) {
	scores := make([]int, len(rubric.Criteria))
	for i, criterion := range rubric.Criteria {
		label := fmt.Sprintf("%s [0-%d]", criterion.Name, criterion.MaxPoints)
		if existing != nil && i < len(existing) {
			label += fmt.Sprintf(" (current: %d)", existing[i])
		}
		for {
			raw, err := a.prompt(label)
			if err != nil {
				return nil, 0, err
			}
			raw = strings.TrimSpace(raw)
			if raw == "" && existing != nil && i < len(existing) {
				scores[i] = existing[i]
				break
			}
			score, err := strconv.Atoi(raw)
			if err != nil || score < 0 || score > criterion.MaxPoints {
				fmt.Fprintln(a.out, retryMessage(fmt.Sprintf("enter a number between 0 and %d", criterion.MaxPoints)))
				continue
			}
			scores[i] = score
			break
		}
	}
	for {
		raw, err := a.prompt(fmt.Sprintf("Flags ([l]ate [r]edo toggle, Enter to accept: %s)", rubricFlagLabel(flags)))
		if err != nil {
			return nil, 0, err
		}
		raw = strings.ToLower(strings.TrimSpace(raw))
		if raw == "" {
			break
		}
		for _, letter := range raw {
			switch letter {
			case 'l':
				flags ^= flagLate
			case 'r':
				flags ^= flagRedo
			}
		}
		fmt.Fprintf(a.out, "Flags: %s\n", rubricFlagLabel(flags))
	}
	return scores, flags, nil
}

func rubricFlagLabel(flags int) string {
	var parts []string
	if flags&flagLate != 0 {
		parts = append(parts, "late")
	}
	if flags&flagRedo != 0 {
		parts = append(parts, "redo")
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, " ")
}

type rubricKeyAction int

const (
	rubricActionNone rubricKeyAction = iota
	rubricActionUp
	rubricActionDown
	rubricActionDigit
	rubricActionSave
	rubricActionCancel
	rubricActionToggleLate
	rubricActionToggleRedo
)

// parseRubricKey maps one read from a raw terminal to a sheet action.
func parseRubricKey(b []byte) (rubricKeyAction, int) {
	if len(b) == 0 {
		return rubricActionNone, -1
	}
	if b[0] == 0x1b {
		if len(b) >= 3 && b[1] == '[' {
			switch b[2] {
			case 'A':
				return rubricActionUp, -1
			case 'B':
				return rubricActionDown, -1
			}
		}
		return rubricActionCancel, -1
	}
	switch {
	case b[0] >= '0' && b[0] <= '9':
		return rubricActionDigit, int(b[0] - '0')
	}
	switch b[0] {
	case '\r', '\n':
		return rubricActionDown, -1
	case 's', 'S':
		return rubricActionSave, -1
	case 'q', 'Q', 0x03:
		return rubricActionCancel, -1
	case 'l', 'L':
		return rubricActionToggleLate, -1
	case 'r', 'R':
		return rubricActionToggleRedo, -1
	}
	return rubricActionNone, -1
}

// rubricScoreSheet is the interactive terminal score sheet: arrows move
// between criteria, digits set scores, l/r toggle late/redo, s saves.
func (a *App) rubricScoreSheet(rubric Rubric, existing []int, flags int, studentName string) ([]int, int, error) {
	fd := int(a.stdinFile.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return nil, 0, errRubricNotTerminal
	}
	defer func() { _ = term.Restore(fd, oldState) }()

	scores := make([]int, len(rubric.Criteria))
	for i := range scores {
		scores[i] = -1
		if existing != nil && i < len(existing) {
			scores[i] = existing[i]
		}
	}
	cursor := 0
	lineCount := len(rubric.Criteria) + 2
	render := func() {
		for i, line := range rubricSheetLines(rubric, scores, flags, cursor, studentName) {
			if i > 0 {
				fmt.Fprint(a.out, "\r\n")
			}
			fmt.Fprint(a.out, "\x1b[2K\r"+line)
		}
		fmt.Fprint(a.out, "\r\n")
	}
	render()
	for {
		buf := make([]byte, 16)
		n, err := a.stdinFile.Read(buf)
		if err != nil {
			return nil, 0, err
		}
		action, digit := parseRubricKey(buf[:n])
		switch action {
		case rubricActionUp:
			if cursor > 0 {
				cursor--
			}
		case rubricActionDown:
			if cursor < len(rubric.Criteria)-1 {
				cursor++
			} else if rubricScoresComplete(scores) {
				fmt.Fprintf(a.out, "\x1b[%dA", lineCount)
				render()
				return scores, flags, nil
			}
		case rubricActionDigit:
			if digit <= rubric.Criteria[cursor].MaxPoints {
				scores[cursor] = digit
				if cursor < len(rubric.Criteria)-1 {
					cursor++
				}
			}
		case rubricActionToggleLate:
			flags ^= flagLate
		case rubricActionToggleRedo:
			flags ^= flagRedo
		case rubricActionSave:
			if !rubricScoresComplete(scores) {
				continue
			}
			fmt.Fprintf(a.out, "\x1b[%dA", lineCount)
			render()
			return scores, flags, nil
		case rubricActionCancel:
			fmt.Fprintf(a.out, "\x1b[%dA", lineCount)
			render()
			return nil, 0, errRubricCancelled
		case rubricActionNone:
			continue
		}
		fmt.Fprintf(a.out, "\x1b[%dA", lineCount)
		render()
	}
}

func rubricScoresComplete(scores []int) bool {
	for _, score := range scores {
		if score < 0 {
			return false
		}
	}
	return true
}

func rubricSheetLines(rubric Rubric, scores []int, flags int, cursor int, studentName string) []string {
	header := studentName + " — " + rubric.Title
	if flags != 0 {
		header += "   [" + rubricFlagLabel(flags) + "]"
	}
	lines := []string{header}
	for i, criterion := range rubric.Criteria {
		pointer := "  "
		if i == cursor {
			pointer = "> "
		}
		scoreLabel := "_"
		if scores[i] >= 0 {
			scoreLabel = strconv.Itoa(scores[i])
		}
		lines = append(lines, fmt.Sprintf("%s%-36s %s/%d", pointer, criterion.Name, scoreLabel, criterion.MaxPoints))
	}
	total := 0
	for _, score := range scores {
		if score > 0 {
			total += score
		}
	}
	lines = append(lines, fmt.Sprintf("Total: %d/%d · ↑/↓ move · 0-%d set · l late · r redo · s save · q cancel",
		total, rubric.Total(), rubric.Criteria[0].MaxPoints))
	return lines
}
