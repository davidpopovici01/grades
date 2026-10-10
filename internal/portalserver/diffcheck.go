package portalserver

import (
	"bufio"
	"bytes"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Caps for the built-in diff check.
const (
	// diffBinarySniffBytes is how much of a file is scanned for a NUL byte
	// before the file is treated as binary and skipped.
	diffBinarySniffBytes = 8 << 10
	// diffAlignMaxCells bounds the LCS matrix (lenA*lenB). Larger file pairs
	// fall back to line-multiset overlap scoring without a row alignment.
	diffAlignMaxCells = 4 << 20
	// diffMaxLineRunes truncates longer lines for the character comparison.
	diffMaxLineRunes = 2000
	// diffMaxHunkPairs bounds greedy best-match pairing inside one changed
	// hunk; larger hunks pair removed/added lines in order.
	diffMaxHunkPairs = 10000
	// diffMinPairSimilarity is the character similarity a removed/added line
	// pair needs to be treated as an edited copy of the same line; below it
	// the lines count as unrelated (removed + added, no credit).
	diffMinPairSimilarity = 0.5
	// diffMaxKeepBytes caps how much of one file is held in memory for
	// alignment; larger text files are streamed into line counts only.
	diffMaxKeepBytes = 1 << 20
	// diffMaxKeepTotalBytes caps the kept file content per submission, so a
	// diff run over many large submissions stays within the service memory
	// budget; content beyond the budget is reduced to line counts.
	diffMaxKeepTotalBytes = 4 << 20
	// diffMaxKeepLines caps the lines kept for alignment per file.
	diffMaxKeepLines = 20000
	// diffMaxStreamLineBytes caps how much of one line is read when streaming
	// an oversized file; longer lines are truncated for counting purposes.
	diffMaxStreamLineBytes = 64 << 10
)

// diffFile is one submission file prepared for diffing: either the kept lines
// (alignment possible) or, for oversized content, only the effective line
// multiset with tooLarge set (multiset scoring, no rendered rows).
type diffFile struct {
	lines    []string
	counts   map[string]int
	tooLarge bool
	bytes    int64
}

// toCounts drops the kept lines in favor of their effective line multiset.
func (f *diffFile) toCounts(skip map[string]bool) {
	if f.lines == nil {
		return
	}
	f.counts = effLineCounts(f.lines, skip)
	f.lines = nil
	f.tooLarge = true
}

// effCounts returns the file's effective line multiset.
func (f *diffFile) effCounts(skip map[string]bool) map[string]int {
	if f.counts != nil {
		return f.counts
	}
	return effLineCounts(f.lines, skip)
}

// diffRow is one aligned row of the side-by-side diff view. Tag is "same",
// "changed" (paired lines with small edits), "removed", or "added". Line
// numbers are 1-based; 0 means the side has no line in this row.
type diffRow struct {
	Tag     string `json:"tag"`
	Left    string `json:"left"`
	Right   string `json:"right"`
	LeftNo  int    `json:"leftNo"`
	RightNo int    `json:"rightNo"`
}

// diffResult is the outcome of aligning two files: the rows for the viewer
// and the aggregate credit/weight used for the similarity score. Similarity
// is credit/weight (0 when weight is 0), where weight is the larger of the
// two files' effective line counts.
type diffResult struct {
	rows     []diffRow
	credit   float64
	weightA  float64
	weightB  float64
	tooLarge bool
}

// weight is the larger side's effective line count.
func (r diffResult) weight() float64 {
	return max(r.weightA, r.weightB)
}

// readDiffFile loads one submission file for diffing. Files with a NUL byte
// in the first sniff bytes are binary and report ok=false. Files beyond
// diffMaxKeepBytes or diffMaxKeepLines are streamed/reduced to their
// effective line multiset (tooLarge) instead of being kept in full.
func readDiffFile(path string, skip map[string]bool) (df *diffFile, ok bool, err error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, false, err
	}
	if info.Size() > diffMaxKeepBytes {
		return streamDiffFile(path, skip)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false, err
	}
	sniff := data
	if len(sniff) > diffBinarySniffBytes {
		sniff = sniff[:diffBinarySniffBytes]
	}
	if bytes.IndexByte(sniff, 0) >= 0 {
		return nil, false, nil
	}
	lines := splitDiffLines(data)
	if len(lines) > diffMaxKeepLines {
		return &diffFile{counts: effLineCounts(lines, skip), tooLarge: true, bytes: info.Size()}, true, nil
	}
	return &diffFile{lines: lines, bytes: info.Size()}, true, nil
}

// splitDiffLines normalizes line endings (\r\n and lone \r) and drops the
// trailing empty element of a newline-terminated file.
func splitDiffLines(data []byte) []string {
	text := strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(string(data))
	lines := strings.Split(text, "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	return lines
}

// streamDiffFile counts the effective lines of an oversized text file without
// holding it in memory. Lines longer than diffMaxStreamLineBytes are
// truncated for counting. Binary files report ok=false.
func streamDiffFile(path string, skip map[string]bool) (df *diffFile, ok bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()

	br := bufio.NewReaderSize(f, 64<<10)
	head, _ := br.Peek(diffBinarySniffBytes) // short read at EOF is fine
	if bytes.IndexByte(head, 0) >= 0 {
		return nil, false, nil
	}
	counts := map[string]int{}
	line := []byte{}
	for {
		frag, isPrefix, readErr := br.ReadLine()
		if room := diffMaxStreamLineBytes - len(line); room > 0 {
			if len(frag) > room {
				frag = frag[:room]
			}
			line = append(line, frag...)
		}
		if !isPrefix {
			if key := string(bytes.TrimRight(line, " \t")); key != "" && !skip[key] {
				counts[key]++
			}
			line = line[:0]
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return nil, false, readErr
		}
	}
	info, _ := f.Stat()
	var size int64
	if info != nil {
		size = info.Size()
	}
	return &diffFile{counts: counts, tooLarge: true, bytes: size}, true, nil
}

// loadDiffWork reads one submission's text files from dir, keeping at most
// diffMaxKeepTotalBytes of file content in memory; content beyond the budget
// is reduced to effective line counts. Binary files are skipped.
func loadDiffWork(dir string, names []string, skip map[string]bool) (map[string]*diffFile, error) {
	out := map[string]*diffFile{}
	var kept int64
	for _, name := range names {
		df, ok, err := readDiffFile(filepath.Join(dir, name), skip)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		if df.lines != nil {
			if kept+df.bytes > diffMaxKeepTotalBytes {
				df.toCounts(skip)
			} else {
				kept += df.bytes
			}
		}
		out[name] = df
	}
	return out, nil
}

// effKey normalizes a line for scoring decisions: trailing whitespace is
// removed. Blank lines and lines contained in skip carry no scoring weight.
func effKey(line string) string {
	return strings.TrimRight(line, " \t")
}

func lineEffective(line string, skip map[string]bool) bool {
	key := effKey(line)
	return key != "" && !skip[key]
}

// basecodeLineSet collects the scoring-excluded lines of an assignment's
// base code: every non-blank line of every base-code file, so starter code
// shared by all students does not inflate similarity.
func basecodeLineSet(dir string) map[string]bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	set := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		df, ok, err := readDiffFile(filepath.Join(dir, e.Name()), nil)
		if err != nil || !ok {
			continue
		}
		if df.lines != nil {
			for _, line := range df.lines {
				if key := effKey(line); key != "" {
					set[key] = true
				}
			}
		} else {
			for key := range df.counts {
				set[key] = true
			}
		}
	}
	if len(set) == 0 {
		return nil
	}
	return set
}

// compareDiffFiles aligns and scores two files. When either side is oversized
// (or the alignment matrix would exceed diffAlignMaxCells), the files are
// scored by effective-line multiset overlap with no row alignment.
func compareDiffFiles(a, b *diffFile, skip map[string]bool) diffResult {
	if a == nil {
		a = &diffFile{}
	}
	if b == nil {
		b = &diffFile{}
	}
	if a.tooLarge || b.tooLarge || len(a.lines)*len(b.lines) > diffAlignMaxCells {
		credit, weightA, weightB := overlapScoreCounts(a.effCounts(skip), b.effCounts(skip))
		return diffResult{credit: credit, weightA: weightA, weightB: weightB, tooLarge: true}
	}
	return diffAlign(a.lines, b.lines, skip)
}

// diffAlign aligns two files line by line and scores them. The backbone is
// an LCS over exact lines (credit 1 per unchanged line); inside each changed
// hunk, removed and added lines are paired by best character similarity and
// earn partial credit, so a copied line with a small in-line edit still
// counts proportionally. skip lists scoring-excluded lines (base code).
func diffAlign(a, b []string, skip map[string]bool) diffResult {
	if len(a)*len(b) > diffAlignMaxCells {
		credit, weightA, weightB := overlapScoreCounts(effLineCounts(a, skip), effLineCounts(b, skip))
		return diffResult{credit: credit, weightA: weightA, weightB: weightB, tooLarge: true}
	}

	ops := lcsOps(a, b)
	res := diffResult{rows: []diffRow{}}
	for p := 0; p < len(ops); {
		if ops[p].kind == '=' {
			line := a[ops[p].ai]
			res.rows = append(res.rows, diffRow{Tag: "same", Left: line, Right: line, LeftNo: ops[p].ai + 1, RightNo: ops[p].bi + 1})
			if lineEffective(line, skip) {
				res.credit++
				res.weightA++
				res.weightB++
			}
			p++
			continue
		}
		var rm, ad []int
		for p < len(ops) && ops[p].kind != '=' {
			if ops[p].kind == '-' {
				rm = append(rm, ops[p].ai)
			} else {
				ad = append(ad, ops[p].bi)
			}
			p++
		}
		res.appendHunk(a, b, rm, ad, skip)
	}
	return res
}

// appendHunk pairs the removed and added lines of one changed hunk and
// appends the resulting rows. Pairs are greedy best-match by character
// similarity (in order for very large hunks); leftovers become plain
// removed/added rows with zero credit.
func (r *diffResult) appendHunk(a, b []string, rm, ad []int, skip map[string]bool) {
	pairs := pairHunkLines(a, b, rm, ad)
	pairedR := map[int]bool{}
	pairedA := map[int]bool{}
	for _, p := range pairs {
		pairedR[p[0]] = true
		pairedA[p[1]] = true
		left, right := a[p[0]], b[p[1]]
		r.rows = append(r.rows, diffRow{Tag: "changed", Left: left, Right: right, LeftNo: p[0] + 1, RightNo: p[1] + 1})
		effA, effB := lineEffective(left, skip), lineEffective(right, skip)
		if effA || effB {
			r.credit += lineCharSimilarity(left, right)
		}
		if effA {
			r.weightA++
		}
		if effB {
			r.weightB++
		}
	}
	for _, i := range rm {
		if pairedR[i] {
			continue
		}
		r.rows = append(r.rows, diffRow{Tag: "removed", Left: a[i], LeftNo: i + 1})
		if lineEffective(a[i], skip) {
			r.weightA++
		}
	}
	for _, j := range ad {
		if pairedA[j] {
			continue
		}
		r.rows = append(r.rows, diffRow{Tag: "added", Right: b[j], RightNo: j + 1})
		if lineEffective(b[j], skip) {
			r.weightB++
		}
	}
}

// pairHunkLines matches removed line indexes (into a) with added line
// indexes (into b): greedy best character similarity first, or positional
// pairing when the hunk exceeds the pair cap. Pairs below
// diffMinPairSimilarity count as unrelated lines and are left unpaired.
func pairHunkLines(a, b []string, rm, ad []int) [][2]int {
	if len(rm) == 0 || len(ad) == 0 {
		return nil
	}
	if len(rm)*len(ad) > diffMaxHunkPairs {
		n := min(len(rm), len(ad))
		pairs := make([][2]int, 0, n)
		for k := 0; k < n; k++ {
			if lineCharSimilarity(a[rm[k]], b[ad[k]]) > diffMinPairSimilarity {
				pairs = append(pairs, [2]int{rm[k], ad[k]})
			}
		}
		return pairs
	}
	sims := make([][]float64, len(rm))
	for i, ai := range rm {
		sims[i] = make([]float64, len(ad))
		for j, bi := range ad {
			sims[i][j] = lineCharSimilarity(a[ai], b[bi])
		}
	}
	usedR := make([]bool, len(rm))
	usedA := make([]bool, len(ad))
	pairs := [][2]int{}
	for {
		best, bi, bj := diffMinPairSimilarity, -1, -1
		for i := range rm {
			if usedR[i] {
				continue
			}
			for j := range ad {
				if !usedA[j] && sims[i][j] > best {
					best, bi, bj = sims[i][j], i, j
				}
			}
		}
		if bi < 0 {
			return pairs
		}
		usedR[bi], usedA[bj] = true, true
		pairs = append(pairs, [2]int{rm[bi], ad[bj]})
	}
}

// lcsOp is one step of the line alignment: '=' keeps a[i]/b[j], '-' drops
// a[i], '+' adds b[j].
type lcsOp struct {
	kind   byte
	ai, bi int
}

// lcsOps computes an LCS alignment of two line slices via full-matrix DP.
// Callers must bound len(a)*len(b) (see diffAlignMaxCells).
func lcsOps(a, b []string) []lcsOp {
	n, m := len(a), len(b)
	width := m + 1
	dp := make([]uint32, (n+1)*width)
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			switch {
			case a[i] == b[j]:
				dp[i*width+j] = dp[(i+1)*width+j+1] + 1
			case dp[(i+1)*width+j] >= dp[i*width+j+1]:
				dp[i*width+j] = dp[(i+1)*width+j]
			default:
				dp[i*width+j] = dp[i*width+j+1]
			}
		}
	}
	ops := []lcsOp{}
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			ops = append(ops, lcsOp{kind: '=', ai: i, bi: j})
			i++
			j++
		case dp[(i+1)*width+j] >= dp[i*width+j+1]:
			ops = append(ops, lcsOp{kind: '-', ai: i})
			i++
		default:
			ops = append(ops, lcsOp{kind: '+', bi: j})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, lcsOp{kind: '-', ai: i})
	}
	for ; j < m; j++ {
		ops = append(ops, lcsOp{kind: '+', bi: j})
	}
	return ops
}

// lineCharSimilarity is the character-level similarity of two lines: 1 for
// identical, 0 for nothing in common. Levenshtein over runes, two rows;
// lines are truncated to diffMaxLineRunes first.
func lineCharSimilarity(a, b string) float64 {
	ra, rb := truncateRunes(a, diffMaxLineRunes), truncateRunes(b, diffMaxLineRunes)
	if len(ra) == 0 && len(rb) == 0 {
		return 1
	}
	dist := levenshtein(ra, rb)
	return 1 - float64(dist)/float64(max(len(ra), len(rb)))
}

// truncateRunes decodes at most n leading runes of s without materializing
// the whole string, so a single huge line cannot exhaust memory.
func truncateRunes(s string, n int) []rune {
	if len(s) <= n {
		return []rune(s)
	}
	out := make([]rune, 0, n)
	for _, r := range s {
		if len(out) == n {
			break
		}
		out = append(out, r)
	}
	return out
}

func levenshtein(a, b []rune) int {
	if len(a) == 0 {
		return len(b)
	}
	if len(b) == 0 {
		return len(a)
	}
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i, ca := range a {
		cur[0] = i + 1
		for j, cb := range b {
			cost := 1
			if ca == cb {
				cost = 0
			}
			cur[j+1] = min(min(cur[j]+1, prev[j+1]+1), prev[j]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

// overlapScoreCounts is the fallback for file pairs too large to align:
// credit counts lines shared as multisets, the weights are each side's
// effective line count.
func overlapScoreCounts(countA, countB map[string]int) (credit, weightA, weightB float64) {
	for _, c := range countA {
		weightA += float64(c)
	}
	for _, c := range countB {
		weightB += float64(c)
	}
	for line, ca := range countA {
		credit += float64(min(ca, countB[line]))
	}
	return credit, weightA, weightB
}

func effLineCounts(lines []string, skip map[string]bool) map[string]int {
	counts := map[string]int{}
	for _, line := range lines {
		if key := effKey(line); key != "" && !skip[key] {
			counts[key]++
		}
	}
	return counts
}

// executeDiffRun is the built-in counterpart of executePlagRun: it compares
// the latest submission of every student pair with the pure-Go line/character
// diff and stores the pairwise similarities. No external tools are needed, so
// it works for every assignment language, including generic file submissions.
func (g *Grader) executeDiffRun(runID int64) {
	run, err := g.store.GetSubPlagRun(runID)
	if err != nil || run == nil || run.Status != "queued" {
		return
	}
	asg, err := g.store.GetSubAssignment(run.AssignmentID)
	if err != nil || asg == nil {
		log.Printf("diffcheck: run %d: assignment missing", runID)
		return
	}

	setStatus := func(status, message string) {
		if err := g.store.SetSubPlagRunStatus(runID, status, "", message); err != nil {
			log.Printf("diffcheck: update run %d: %v", runID, err)
		}
	}

	latest, err := g.store.LatestSubmissionsByAssignment(asg.ID)
	if err != nil || len(latest) < 2 {
		setStatus("error", "need at least 2 students with submissions")
		return
	}
	skip := basecodeLineSet(basecodeDir(g.submissionsDir, asg.ID))

	type studentWork struct {
		pk    int
		files map[string]*diffFile
	}
	students := []studentWork{}
	for pk, sub := range latest {
		files, err := g.store.ListSubFiles(sub.ID)
		if err != nil {
			setStatus("error", "failed to list submission files")
			return
		}
		names := make([]string, 0, len(files))
		for _, f := range files {
			names = append(names, f.Filename)
		}
		srcDir := submissionDir(g.submissionsDir, asg.ID, sub.StudentPK, sub.Attempt)
		work, err := loadDiffWork(srcDir, names, skip)
		if err != nil {
			setStatus("error", "failed to read submission files")
			return
		}
		students = append(students, studentWork{pk: pk, files: work})
	}
	sort.Slice(students, func(i, j int) bool { return students[i].pk < students[j].pk })

	pairs := []SubPlagPair{}
	for i := 0; i < len(students); i++ {
		for j := i + 1; j < len(students); j++ {
			pairs = append(pairs, SubPlagPair{
				RunID:      runID,
				StudentA:   students[i].pk,
				StudentB:   students[j].pk,
				Similarity: diffPairSimilarity(students[i].files, students[j].files, skip),
			})
		}
	}
	if err := g.store.ReplaceSubPlagPairs(runID, pairs); err != nil {
		log.Printf("diffcheck: run %d store pairs: %v", runID, err)
		setStatus("error", "failed to store results")
		return
	}
	setStatus("done", "")
}

// diffPairSimilarity aggregates two students' file sets into one 0-1
// similarity: per-file credits and weights summed across the union of file
// names (a file only one side has counts toward the weight with no credit).
func diffPairSimilarity(filesA, filesB map[string]*diffFile, skip map[string]bool) float64 {
	var credit, weight float64
	for name, fileA := range filesA {
		r := compareDiffFiles(fileA, filesB[name], skip)
		credit += r.credit
		weight += r.weight()
	}
	for name, fileB := range filesB {
		if _, ok := filesA[name]; ok {
			continue
		}
		r := compareDiffFiles(nil, fileB, skip)
		credit += r.credit
		weight += r.weight()
	}
	if weight == 0 {
		return 0
	}
	return credit / weight
}
