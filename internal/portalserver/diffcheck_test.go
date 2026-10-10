package portalserver

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func diffSimilarity(a, b []string) float64 {
	res := diffAlign(a, b, nil)
	if res.weight() == 0 {
		return 0
	}
	return res.credit / res.weight()
}

func TestDiffAlignIdentical(t *testing.T) {
	lines := []string{"package main", "", "func main() {", "}"}
	if sim := diffSimilarity(lines, lines); sim != 1.0 {
		t.Fatalf("identical files: similarity %v, want 1", sim)
	}
	res := diffAlign(lines, lines, nil)
	if len(res.rows) != len(lines) {
		t.Fatalf("rows = %d, want %d", len(res.rows), len(lines))
	}
	for i, row := range res.rows {
		if row.Tag != "same" || row.LeftNo != i+1 || row.RightNo != i+1 {
			t.Fatalf("row %d: %+v", i, row)
		}
	}
}

func TestDiffAlignDisjoint(t *testing.T) {
	a := []string{"aaaa", "bbbb", "cccc"}
	b := []string{"xxxx", "yyyy", "zzzz"}
	if sim := diffSimilarity(a, b); sim != 0 {
		t.Fatalf("disjoint files: similarity %v, want 0", sim)
	}
}

func TestDiffAlignHalfOverlap(t *testing.T) {
	a := []string{"one", "two", "three", "four"}
	b := []string{"one", "two", "x", "y"}
	if sim := diffSimilarity(a, b); sim != 0.5 {
		t.Fatalf("half overlap: similarity %v, want 0.5", sim)
	}
}

func TestDiffAlignEmpty(t *testing.T) {
	if sim := diffSimilarity(nil, nil); sim != 0 {
		t.Fatalf("empty inputs: similarity %v, want 0", sim)
	}
	if sim := diffSimilarity([]string{"a"}, nil); sim != 0 {
		t.Fatalf("one side empty: similarity %v, want 0", sim)
	}
	res := diffAlign(nil, []string{"a", "b"}, nil)
	if len(res.rows) != 2 || res.rows[0].Tag != "added" {
		t.Fatalf("rows for one-sided file: %+v", res.rows)
	}
}

func TestDiffAlignPartialCredit(t *testing.T) {
	// A verbatim copy with one identifier changed per line still scores high.
	a := []string{
		"int total = 0;",
		"for (int i = 0; i < n; i++) {",
		"    total += values[i];",
		"}",
		"return total;",
	}
	b := []string{
		"int sum = 0;",
		"for (int i = 0; i < n; i++) {",
		"    sum += values[i];",
		"}",
		"return sum;",
	}
	sim := diffSimilarity(a, b)
	if sim < 0.8 || sim >= 1.0 {
		t.Fatalf("partial credit: similarity %v, want in [0.8, 1)", sim)
	}
	res := diffAlign(a, b, nil)
	changed := 0
	for _, row := range res.rows {
		if row.Tag == "changed" {
			changed++
		}
	}
	if changed != 3 {
		t.Fatalf("changed rows = %d, want 3", changed)
	}
}

func TestDiffAlignSingleTweak(t *testing.T) {
	// One tweaked line in an otherwise identical file scores close to 1.
	a := []string{"aaa", "bbb", "ccc", "ddd", "eee", "fff", "ggg", "hhh"}
	b := []string{"aaa", "bbb", "ccc", "ddd", "eee", "fff", "ggg", "hhH"}
	sim := diffSimilarity(a, b)
	if sim <= 0.95 || sim >= 1.0 {
		t.Fatalf("single tweak: similarity %v, want in (0.95, 1)", sim)
	}
}

func TestDiffAlignIgnoresBlankAndBasecode(t *testing.T) {
	a := []string{"template();", "", "unique_a();"}
	b := []string{"template();", "", "", "unique_b();"}
	skip := map[string]bool{"template();": true}
	res := diffAlign(a, b, skip)
	// Only unique_a/unique_b carry weight: one changed pair.
	if res.weight() != 1 {
		t.Fatalf("weight = %v, want 1", res.weight())
	}
	if res.credit <= 0 || res.credit >= 1 {
		t.Fatalf("credit = %v, want partial", res.credit)
	}
}

func TestDiffAlignBasecodeEditNoCredit(t *testing.T) {
	// One side has an exact base-code line, the other a small edit of it:
	// the common source is starter code, so the pair earns no credit even
	// though the edited side carries weight.
	a := []string{"template();"}
	b := []string{"templateX();"}
	skip := map[string]bool{"template();": true}
	res := diffAlign(a, b, skip)
	if res.credit != 0 {
		t.Fatalf("credit = %v, want 0 for a base-code edit", res.credit)
	}
	if res.weightB != 1 || res.weightA != 0 {
		t.Fatalf("weights = %v/%v, want 0/1", res.weightA, res.weightB)
	}
}

func TestStreamDiffFileCountBudget(t *testing.T) {
	// Unique line keys are retained only up to the byte budget; further
	// unique lines count as overflow (weight, no credit), keeping memory
	// bounded for huge all-unique uploads. Repeated lines are always counted.
	dir := t.TempDir()
	path := filepath.Join(dir, "big.txt")
	var sb strings.Builder
	sb.WriteString("repeat\nrepeat\n") // 6-byte key, twice
	for i := 0; i < 10; i++ {
		fmt.Fprintf(&sb, "unique-line-%08d\n", i) // 15-byte keys
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	df, ok, err := streamDiffFile(path, nil, 66) // room for "repeat" + 3 unique keys
	if err != nil || !ok {
		t.Fatalf("stream: ok=%v err=%v", ok, err)
	}
	if df.counts["repeat"] != 2 {
		t.Fatalf("repeat count = %d, want 2", df.counts["repeat"])
	}
	if got := len(df.counts); got != 4 {
		t.Fatalf("retained unique keys = %d, want 4 (repeat + 3)", got)
	}
	if df.overflow != 7 {
		t.Fatalf("overflow = %d, want 7", df.overflow)
	}

	// Overflow adds weight but no credit, so an overflow-heavy pair scores
	// low even against itself.
	res := compareDiffFiles(df, df, nil)
	wantWeight := float64(2 + 3 + 7)
	if res.weight() != wantWeight {
		t.Fatalf("weight = %v, want %v", res.weight(), wantWeight)
	}
	if res.credit != 5 {
		t.Fatalf("credit = %v, want 5 (retained lines only)", res.credit)
	}
}

func TestPairHunkLinesCellBudget(t *testing.T) {
	// Long lines that pass the histogram prefilter each spend
	// diffMaxLineRunes^2 cells; the per-hunk budget must stop pairing once
	// spent, so adversarial long-line uploads cannot stall the worker.
	pad := strings.Repeat("a", diffMaxLineRunes)
	maxPairs := diffMaxHunkCells / (diffMaxLineRunes * diffMaxLineRunes)
	n := maxPairs + 4
	a, b := make([]string, n), make([]string, n)
	rm, ad := make([]int, n), make([]int, n)
	for i := 0; i < n; i++ {
		a[i], b[i] = pad, pad
		rm[i], ad[i] = i, i
	}
	if pairs := pairHunkLines(a, b, rm, ad); len(pairs) > maxPairs {
		t.Fatalf("pairs = %d, want <= %d (cell budget)", len(pairs), maxPairs)
	}

	// Disjoint long lines never reach Levenshtein: the histogram upper bound
	// already shows they cannot pass the threshold.
	for i := 0; i < n; i++ {
		b[i] = strings.Repeat("b", diffMaxLineRunes)
	}
	if pairs := pairHunkLines(a, b, rm, ad); len(pairs) != 0 {
		t.Fatalf("disjoint long lines: pairs = %d, want 0", len(pairs))
	}
}

func TestDiffAlignTooLarge(t *testing.T) {
	// Beyond the cell cap the engine falls back to multiset overlap and
	// reports no row alignment.
	n := 2100 // 2100*2100 > diffAlignMaxCells
	a := make([]string, n)
	b := make([]string, n)
	for i := range a {
		a[i] = fmt.Sprintf("line %d", i)
		b[i] = fmt.Sprintf("line %d", i)
	}
	b[0] = "different"
	res := diffAlign(a, b, nil)
	if !res.tooLarge || res.rows != nil {
		t.Fatalf("tooLarge result: %+v", res)
	}
	if res.weight() == 0 || res.credit/res.weight() < 0.99 {
		t.Fatalf("overlap score: credit %v weight %v", res.credit, res.weight())
	}
}

func TestReadDiffFile(t *testing.T) {
	dir := t.TempDir()

	text := filepath.Join(dir, "main.py")
	if err := os.WriteFile(text, []byte("a\r\nb\rc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	df, ok, err := readDiffFile(text, nil, diffMaxStreamCountBytes)
	if err != nil || !ok {
		t.Fatalf("read text: ok=%v err=%v", ok, err)
	}
	if df.tooLarge || len(df.lines) != 3 || df.lines[0] != "a" || df.lines[1] != "b" || df.lines[2] != "c" {
		t.Fatalf("lines: %+v", df)
	}

	binary := filepath.Join(dir, "data.bin")
	if err := os.WriteFile(binary, []byte{'a', 0, 'b'}, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := readDiffFile(binary, nil, diffMaxStreamCountBytes); err != nil || ok {
		t.Fatalf("read binary: ok=%v err=%v", ok, err)
	}
}

func TestReadDiffFileOversized(t *testing.T) {
	// A file beyond diffMaxKeepBytes is streamed into line counts: it stays in
	// scoring (tooLarge, multiset overlap) instead of being dropped.
	dir := t.TempDir()
	big := filepath.Join(dir, "big.txt")
	var sb strings.Builder
	for sb.Len() <= diffMaxKeepBytes {
		sb.WriteString("some fairly long line of text to fill the file\n")
	}
	sb.WriteString("unique marker line\n")
	if err := os.WriteFile(big, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	df, ok, err := readDiffFile(big, nil, diffMaxStreamCountBytes)
	if err != nil || !ok {
		t.Fatalf("read big: ok=%v err=%v", ok, err)
	}
	if !df.tooLarge || df.lines != nil {
		t.Fatalf("big file should be counts-only: %+v", df)
	}
	if df.counts["unique marker line"] != 1 || df.counts["some fairly long line of text to fill the file"] == 0 {
		t.Fatalf("counts: %v", df.counts)
	}

	// An oversized pair still scores via the multiset fallback.
	res := compareDiffFiles(df, df, nil)
	if !res.tooLarge || res.weight() == 0 || res.credit/res.weight() != 1.0 {
		t.Fatalf("oversized self-compare: %+v", res)
	}
}

func TestLineCharSimilarity(t *testing.T) {
	if got := lineCharSimilarity("abc", "abc"); got != 1 {
		t.Fatalf("identical: %v", got)
	}
	if got := lineCharSimilarity("abc", "xyz"); got != 0 {
		t.Fatalf("disjoint: %v", got)
	}
	if got := lineCharSimilarity("kittens", "kittens"); got != 1 {
		t.Fatalf("identical words: %v", got)
	}
	// One substitution in a long line: high similarity.
	line := strings.Repeat("x", 100)
	other := strings.Repeat("x", 50) + "y" + strings.Repeat("x", 49)
	if got := lineCharSimilarity(line, other); got != 0.99 {
		t.Fatalf("one substitution: %v", got)
	}
	if got := lineCharSimilarity("", ""); got != 1 {
		t.Fatalf("empty: %v", got)
	}
}

func TestBasecodeLineSet(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Template.java"), []byte("public class T {\n}\n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	set := basecodeLineSet(dir)
	if !set["public class T {"] || !set["}"] || set[""] {
		t.Fatalf("set: %v", set)
	}
	if got := basecodeLineSet(filepath.Join(dir, "missing")); got != nil {
		t.Fatalf("missing dir: %v", got)
	}
}

func TestDiffPairSimilarity(t *testing.T) {
	a := map[string]*diffFile{
		"main.py": {lines: []string{"print(1)"}},
		"util.py": {lines: []string{"def helper():", "    return 42"}},
	}
	b := map[string]*diffFile{
		"main.py":  {lines: []string{"print(2)"}},
		"extra.py": {lines: []string{"only here"}},
	}
	sim := diffPairSimilarity(a, b, nil)
	// main.py: 0.875 credit / 1 weight; util.py (only A): 0/2; extra.py: 0/1.
	want := 0.875 / 4
	if sim != want {
		t.Fatalf("pair similarity %v, want %v", sim, want)
	}
	if sim := diffPairSimilarity(a, a, nil); sim != 1.0 {
		t.Fatalf("self similarity %v, want 1", sim)
	}
}
