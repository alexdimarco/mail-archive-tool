package tbrestore

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/util"
)

// covers: MA-1088, R40, S69
// ADV-05 (the R3–R5 adversarial pass, settled 2026-11-07): Windows gives a long name a
// short alias ("Long folder name one" → "LONGFO~1"), so an archive folder named like one
// can land on its long-named sibling's alias and fail its exclusive create at Start. The
// plan's names step reuses util.IsShortNameAlias (extract's XC13 rule): a name that looks
// like a short alias gets the hash suffix of its original, on every platform (a profile
// can be copied to Windows), and is counted as changed, so the plan's words name it as a
// changed name and no name can fail part way through. A name that only resembles one (a
// tilde with letters, a seven-character stem) is kept.
func TestRestoreNamesNeverLookLikeAShortName(t *testing.T) {
	originals := []string{"Long folder name one", "LONGFO~1", "report~2.pdf", "A~12", "Q~3x", "SEVENCH~1", "plain"}
	names, changed := FinalNames(originals)
	for i, n := range names {
		if util.IsShortNameAlias(n) {
			t.Errorf("%q became %q, still a short-name alias", originals[i], n)
		}
	}
	for i, o := range originals[1:4] {
		k := i + 1
		if names[k] == o || !changed[k] || !strings.HasPrefix(names[k], o+"~") {
			t.Errorf("%q: the hash suffix after its own name, counted as changed: %q %v", o, names[k], changed[k])
		}
	}
	for _, k := range []int{0, 4, 5, 6} {
		if names[k] != originals[k] || changed[k] {
			t.Errorf("%q is not alias-shaped and is kept: %q %v", originals[k], names[k], changed[k])
		}
	}
	// Through the layout: both folders restored, the alias-shaped one renamed and counted.
	l, err := PlanLayout("Work mail", []FolderIn{{Names: []string{"Long folder name one"}}, {Names: []string{"LONGFO~1"}}}, `C:\Users\a\Local Folders`, "windows")
	if err != nil {
		t.Fatal(err)
	}
	if l.Changed != 1 || util.IsShortNameAlias(l.Folders[1].Parts[0]) || l.Folders[0].Parts[0] != "Long folder name one" {
		t.Fatalf("the layout: %d changed, %q %q", l.Changed, l.Folders[0].Parts, l.Folders[1].Parts)
	}

	// The dedupe's own hash suffix never makes an alias either (a short name past " (99)").
	many := make([]string, 101)
	for i := range many {
		many[i] = "A"
	}
	got, _ := FinalNames(many)
	seen := map[string]bool{}
	for _, n := range got {
		if util.IsShortNameAlias(n) || seen[util.FoldKey(n)] {
			t.Fatalf("past (99): %q alias-shaped or repeated", n)
		}
		seen[util.FoldKey(n)] = true
	}
}
