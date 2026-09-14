package portalserver

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// plagRunTimeout caps one JPlag execution.
const plagRunTimeout = 10 * time.Minute

// executePlagRun stages the latest submission per student and runs JPlag,
// parsing its CSV export into sub_plag_pairs. JPlag's --csv-export writes
// results.csv into a directory named after the -r result base name, with the
// header submissionName1,submissionName2,averageSimilarity,maxSimilarity.
func (g *Grader) executePlagRun(runID int64) {
	run, err := g.store.GetSubPlagRun(runID)
	if err != nil || run == nil || run.Status != "queued" {
		return
	}
	asg, err := g.store.GetSubAssignment(run.AssignmentID)
	if err != nil || asg == nil {
		log.Printf("plagiarism: run %d: assignment missing", runID)
		return
	}

	setStatus := func(status, reportPath, message string) {
		if err := g.store.SetSubPlagRunStatus(runID, status, reportPath, message); err != nil {
			log.Printf("plagiarism: update run %d: %v", runID, err)
		}
	}

	if asg.Language == "files" {
		setStatus("error", "", "plagiarism detection is not available for generic file submissions")
		return
	}

	javaBin, err := exec.LookPath("java")
	if err != nil {
		setStatus("unavailable", "", "java is not installed on the server")
		return
	}
	if _, err := os.Stat(g.jplagJar); err != nil {
		setStatus("unavailable", "", fmt.Sprintf("JPlag jar not found at %s", g.jplagJar))
		return
	}

	latest, err := g.store.LatestSubmissionsByAssignment(asg.ID)
	if err != nil || len(latest) < 2 {
		setStatus("error", "", "need at least 2 students with submissions")
		return
	}
	roster, err := g.store.ListStudentsForCourse(asg.CourseYearID, asg.TermID)
	if err != nil {
		setStatus("error", "", "failed to list students")
		return
	}
	dirNames, pkByDir := plagSubmissionNames(roster, latest)

	plagDir := filepath.Join(g.submissionsDir, "plag", strconv.FormatInt(runID, 10))
	subsDir := filepath.Join(plagDir, "subs")
	for _, sub := range latest {
		files, err := g.store.ListSubFiles(sub.ID)
		if err != nil {
			setStatus("error", "", "failed to list submission files")
			return
		}
		dstDir := filepath.Join(subsDir, dirNames[sub.StudentPK])
		if err := os.MkdirAll(dstDir, 0o755); err != nil {
			setStatus("error", "", "failed to create workspace")
			return
		}
		srcDir := submissionDir(g.submissionsDir, asg.ID, sub.StudentPK, sub.Attempt)
		for _, f := range files {
			name := f.Filename
			// JPlag's text language only parses .txt/.asc/.tex; markdown is
			// plain text, so stage .md under a .txt name.
			if asg.Language == "text" && strings.HasSuffix(strings.ToLower(name), ".md") {
				name = strings.TrimSuffix(name, filepath.Ext(name)) + ".txt"
				if _, err := os.Stat(filepath.Join(srcDir, name)); err == nil {
					name = strings.TrimSuffix(f.Filename, filepath.Ext(f.Filename)) + "_md.txt"
				}
			}
			if err := copyFile(filepath.Join(srcDir, f.Filename), filepath.Join(dstDir, name)); err != nil {
				setStatus("error", "", fmt.Sprintf("failed to stage %s", f.Filename))
				return
			}
		}
	}

	language := jplagLanguage(asg.Language)
	// JPlag 6 writes the report bundle as <result name>.jplag (a zip inside).
	reportPath := plagDir + ".jplag"

	baseDir := ""
	if entries, err := os.ReadDir(basecodeDir(g.submissionsDir, asg.ID)); err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				baseDir = basecodeDir(g.submissionsDir, asg.ID)
				break
			}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), plagRunTimeout)
	defer cancel()
	args := append([]string{"-Xmx384m", "-jar", g.jplagJar}, jplagArgs(language, plagDir, subsDir, baseDir)...)
	cmd := exec.CommandContext(ctx, javaBin, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		log.Printf("plagiarism: run %d failed: %v: %s", runID, err, truncate(string(out), 2000))
		setStatus("error", "", plagErrorMessage(string(out)))
		return
	}

	pairs, err := parsePlagCSV(filepath.Join(plagDir, "results.csv"), runID, pkByDir)
	if err != nil {
		log.Printf("plagiarism: run %d parse results: %v", runID, err)
		setStatus("error", "", "failed to parse JPlag results")
		return
	}
	if err := g.store.ReplaceSubPlagPairs(runID, pairs); err != nil {
		log.Printf("plagiarism: run %d store pairs: %v", runID, err)
		setStatus("error", "", "failed to store results")
		return
	}
	setStatus("done", reportPath, "")
}

// jplagLanguage maps an assignment language to JPlag's -l argument.
func jplagLanguage(language string) string {
	switch language {
	case "python":
		return "python3"
	case "text":
		return "text"
	default:
		return "java"
	}
}

// jplagArgs builds the JPlag CLI arguments for one plagiarism run. Frequency
// analysis (rare shared code stands out over common boilerplate) and
// subsequence match merging are always on; token normalization is Java-only
// (JPlag rejects it for other languages); base code is subtracted from every
// submission when present.
func jplagArgs(language, resultBase, subsDir, baseDir string) []string {
	args := []string{"-l", language, "-M", "RUN", "--csv-export", "--frequency", "--match-merging"}
	if language == "java" {
		args = append(args, "--normalize")
	}
	if baseDir != "" {
		args = append(args, "-bc", baseDir)
	}
	return append(args, "-r", resultBase, subsDir)
}

// plagErrorMessage condenses JPlag's combined output into a short admin-visible
// reason: [ERROR] lines (e.g. "submission too small") when present, otherwise
// the tail of the output (e.g. a JVM startup failure). Progress-bar rewrites
// and JVM warnings are filtered out.
func plagErrorMessage(output string) string {
	var errs, tail []string
	for _, line := range strings.Split(output, "\n") {
		// Progress bars rewrite the line with carriage returns; keep the last segment.
		if idx := strings.LastIndexByte(line, '\r'); idx >= 0 {
			line = line[idx+1:]
		}
		line = strings.TrimSpace(line)
		if line == "" || strings.Contains(line, "=====") || strings.HasPrefix(line, "WARNING:") {
			continue
		}
		if strings.Contains(line, "[ERROR]") {
			if idx := strings.Index(line, "] "); idx >= 0 {
				line = line[idx+2:]
			}
			// Strip the logger name ("CLI - ", "SubmissionSet - ").
			if idx := strings.Index(line, " - "); idx >= 0 {
				line = line[idx+3:]
			}
			errs = append(errs, line)
		}
		tail = append(tail, line)
	}
	msg := strings.Join(errs, "; ")
	if msg == "" && len(tail) > 0 {
		if len(tail) > 4 {
			tail = tail[len(tail)-4:]
		}
		msg = strings.Join(tail, " | ")
	}
	if msg == "" {
		msg = "JPlag exited with an error"
	}
	return truncate(msg, 500)
}

// parsePlagCSV reads JPlag's results.csv, mapping submission directory names
// (usernames) back to students via pkByName. Similarities are 0-1.
func parsePlagCSV(path string, runID int64, pkByName map[string]int) ([]SubPlagPair, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	reader := csv.NewReader(f)
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}
	columns := map[string]int{}
	for i, name := range header {
		columns[name] = i
	}
	first, ok1 := columns["submissionName1"]
	second, ok2 := columns["submissionName2"]
	avg, ok3 := columns["averageSimilarity"]
	if !ok1 || !ok2 || !ok3 {
		return nil, fmt.Errorf("unexpected header: %v", header)
	}

	pairs := []SubPlagPair{}
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		studentA, okA := pkByName[record[first]]
		studentB, okB := pkByName[record[second]]
		similarity, err := strconv.ParseFloat(record[avg], 64)
		if !okA || !okB || err != nil {
			continue
		}
		pairs = append(pairs, SubPlagPair{
			RunID:      runID,
			StudentA:   studentA,
			StudentB:   studentB,
			Similarity: similarity,
		})
	}
	return pairs, nil
}

// plagSubmissionNames picks the directory name JPlag sees for each student —
// the username, so the report shows usernames instead of numeric ids — and
// returns the reverse lookup for parsing results back into student ids.
func plagSubmissionNames(roster []AdminStudent, latest map[int]*SubSubmission) (dirByPK map[int]string, pkByDir map[string]int) {
	usernames := map[int]string{}
	for _, st := range roster {
		usernames[st.StudentID] = st.Username
	}
	dirByPK = map[int]string{}
	pkByDir = map[string]int{}
	used := map[string]bool{}
	pks := make([]int, 0, len(latest))
	for pk := range latest {
		pks = append(pks, pk)
	}
	sort.Ints(pks) // lowest id wins a duplicated username deterministically
	for _, pk := range pks {
		name := sanitizePlagName(usernames[pk])
		if name == "" || used[name] {
			name = strconv.Itoa(pk)
		}
		for used[name] {
			name += "_"
		}
		used[name] = true
		dirByPK[pk] = name
		pkByDir[name] = pk
	}
	return dirByPK, pkByDir
}

// sanitizePlagName keeps a username safe as a directory name.
func sanitizePlagName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

func truncate(s string, max int) string {
	if len(s) > max {
		return s[:max]
	}
	return s
}
