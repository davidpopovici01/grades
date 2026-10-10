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

func TestReadDiffLines(t *testing.T) {
	dir := t.TempDir()

	text := filepath.Join(dir, "main.py")
	if err := os.WriteFile(text, []byte("a\r\nb\rc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, ok, err := readDiffLines(text)
	if err != nil || !ok {
		t.Fatalf("read text: ok=%v err=%v", ok, err)
	}
	if len(lines) != 3 || lines[0] != "a" || lines[1] != "b" || lines[2] != "c" {
		t.Fatalf("lines: %q", lines)
	}

	binary := filepath.Join(dir, "data.bin")
	if err := os.WriteFile(binary, []byte{'a', 0, 'b'}, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := readDiffLines(binary); err != nil || ok {
		t.Fatalf("read binary: ok=%v err=%v", ok, err)
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
	a := map[string][]string{
		"main.py": {"print(1)"},
		"util.py": {"def helper():", "    return 42"},
	}
	b := map[string][]string{
		"main.py":  {"print(2)"},
		"extra.py": {"only here"},
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
