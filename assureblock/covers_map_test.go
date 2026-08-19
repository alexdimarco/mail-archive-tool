// Covers-map meta-test: ties tests to the numbered invariant/scenario catalog
// (docs/scenario-catalog.md), the Go analog of vps-capture's test_covers_map.py.
// It scans test files TEXTUALLY (not the collection graph) for `// covers: <ID>`
// markers and enforces three directional rules:
//
//  1. every U/S test-spec ID in the catalog has >=1 test carrying its marker,
//  2. every marker ID exists in the catalog (renamed/typo'd IDs red loudly),
//  3. no product test lacks a marker (an unmarked test proves nothing traceable).
//
// Harness packages (assure/, assureblock/) are out of scope.
package assureblock

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var (
	specRowRe = regexp.MustCompile(`(?m)^\|\s*(MA-\d+)\s*\|\s*([USABL])\s*\|`)
	invIDRe   = regexp.MustCompile(`\bR\d+\b`)
	scenRowRe = regexp.MustCompile(`(?m)^\|\s*(S\d+)\b`)
	funcRe    = regexp.MustCompile(`^func (Test\w+)\(`)
	coversRe  = regexp.MustCompile(`//\s*covers:\s*(.+)$`)
	idRe      = regexp.MustCompile(`MA-\d+|R\d+|S\d+`)

	// Directories that are harness or fixtures, not product tests.
	skipDirs = map[string]bool{".git": true, "assure": true, "assureblock": true, "testdata": true}
)

func TestCoversMap(t *testing.T) {
	root := repoRoot(t)

	catPath := filepath.Join(root, "docs", "scenario-catalog.md")
	catBytes, err := os.ReadFile(catPath)
	if err != nil {
		t.Fatalf("MISSING: scenario catalog %s: %v", catPath, err)
	}
	cat := string(catBytes)

	// Defined IDs: MA- test-specs (with tier), R- invariants, S- scenarios.
	specTier := map[string]string{}
	for _, m := range specRowRe.FindAllStringSubmatch(cat, -1) {
		specTier[m[1]] = m[2]
	}
	if len(specTier) == 0 {
		t.Fatal("UNPARSED: no `| MA-NN | tier |` test-spec rows in the catalog; the table shape is the contract.")
	}
	valid := map[string]bool{}
	for id := range specTier {
		valid[id] = true
	}
	for _, id := range invIDRe.FindAllString(cat, -1) {
		valid[id] = true
	}
	for _, m := range scenRowRe.FindAllStringSubmatch(cat, -1) {
		valid[m[1]] = true
	}

	covered := map[string]bool{}
	var orphans, unknowns []string

	err = filepath.WalkDir(root, func(path string, d os.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		scanTestFile(t, root, path, covered, valid, &orphans, &unknowns)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	var uncovered []string
	for id, tier := range specTier {
		if (tier == "U" || tier == "S") && !covered[id] {
			uncovered = append(uncovered, id)
		}
	}
	sort.Strings(uncovered)
	sort.Strings(orphans)
	sort.Strings(unknowns)

	if len(uncovered) > 0 {
		t.Errorf("UNCOVERED: U/S invariant-spec IDs with no `// covers:` test: %s", strings.Join(uncovered, ", "))
	}
	if len(orphans) > 0 {
		t.Errorf("ORPHAN: product tests without a `// covers:` marker: %s", strings.Join(orphans, ", "))
	}
	if len(unknowns) > 0 {
		t.Errorf("UNKNOWN-ID: `// covers:` IDs absent from the catalog: %s", strings.Join(unknowns, ", "))
	}
}

func scanTestFile(t *testing.T, root, path string, covered, valid map[string]bool, orphans, unknowns *[]string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read %s: %v", path, err)
	}
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		fm := funcRe.FindStringSubmatch(strings.TrimLeft(line, " \t"))
		if fm == nil {
			continue
		}
		if fm[1] == "TestMain" { // the harness entry point, not a product test
			continue
		}
		var ids []string
		// The marker(s) live in the contiguous comment block directly above the
		// func (a blank line between them ends the block → treated as unmarked).
		for j := i - 1; j >= 0; j-- {
			l := strings.TrimSpace(lines[j])
			if !strings.HasPrefix(l, "//") {
				break
			}
			if cm := coversRe.FindStringSubmatch(l); cm != nil {
				ids = append(ids, idRe.FindAllString(cm[1], -1)...)
			}
		}
		label := relPath(root, path) + ":" + fm[1]
		if len(ids) == 0 {
			*orphans = append(*orphans, label)
			continue
		}
		for _, id := range ids {
			if !valid[id] {
				*unknowns = append(*unknowns, label+"→"+id)
			}
			if strings.HasPrefix(id, "MA-") {
				covered[id] = true
			}
		}
	}
}

func relPath(root, path string) string {
	if rel, err := filepath.Rel(root, path); err == nil {
		return rel
	}
	return path
}

var (
	assertRe   = regexp.MustCompile(`t\.(Error|Errorf|Fatal|Fatalf|Skip|Skipf)\b|assure\.(Reached|Refused)\b`)
	noAssertRe = regexp.MustCompile(`//\s*no-assert:`)
)

// TestNoVacuousTests: a product test that asserts nothing proves nothing. Each
// `func Test*` must contain an assertion (t.Error/Fatal/Skip or an assure helper)
// or declare itself with a `// no-assert: reason` marker.
func TestNoVacuousTests(t *testing.T) {
	root := repoRoot(t)
	var vacuous []string

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			fm := funcRe.FindStringSubmatch(strings.TrimLeft(line, " \t"))
			if fm == nil || fm[1] == "TestMain" {
				continue
			}
			j := i + 1
			for j < len(lines) && !strings.HasPrefix(lines[j], "func ") {
				j++
			}
			body := strings.Join(lines[i:j], "\n")
			if noAssertRe.MatchString(body) || assertRe.MatchString(body) {
				continue
			}
			vacuous = append(vacuous, relPath(root, path)+":"+fm[1])
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	sort.Strings(vacuous)
	if len(vacuous) > 0 {
		t.Errorf("VACUOUS: tests that assert nothing (add an assertion or a `// no-assert: reason`): %s",
			strings.Join(vacuous, ", "))
	}
}
