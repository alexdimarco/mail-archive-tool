package tbrestore

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/text/unicode/norm"

	"mail-archive-tool/internal/assure"
)

// covers: MA-1088, R40, S69
// ADV-N1 (high, settled 2026-11-07, the lead's call): Thunderbird percent-decodes each
// file name it finds before it works out the folder's file from it (DecodeFilename, see
// tbDecodeFilename), so a restored "a%41b" was read from "aAb" and shown empty. The port
// first (the review's probe results; a "%" without two hex digits is kept); then every
// name with a "%" — "Budget%2026", "a%41b", "%", "100%", and "Mail%20backup" as the
// archive's own name — is written with "_" for each "%", is read by Thunderbird from its
// own file (decoded, then hashed, unchanged) on both platforms' builds, and is counted as
// changed; ThunderbirdReads refuses any "%" (the defence in depth); and end to end the
// real Write puts each message in the very file Thunderbird reads, while a hand-made
// layout that keeps a "%" is refused W-TBNAME before anything is written.
func TestAPercentNameIsReadByThunderbirdFromItsOwnFile(t *testing.T) {
	for in, want := range map[string]string{
		"a%41b": "aAb", "Budget%2026": "Budget 26", "Mail%20backup": "Mail backup", "%7Etilde": "~tilde",
		"x%2Fy": "x/y", "%e9t%E9": "\xe9t\xe9", "50% off": "50% off", "100%": "100%", "%": "%", "%4": "%4",
		"%zz": "%zz", "%2g": "%2g", "%%41": "%A", "plain": "plain",
	} {
		if got := tbDecodeFilename(in); got != want {
			t.Errorf("DecodeFilename(%q) = %q, Thunderbird's nsUnescapeCount gives %q", in, got, want)
		}
	}
	if tbFileFor("a%41b", true) == "a%41b" || tbFileFor("x%2Fy", false) == "x%2Fy" {
		t.Fatal("the oracle must see that Thunderbird reads a%41b and x%2Fy from other files")
	}

	cases := []struct{ in, want string }{
		{"Budget%2026", "Budget_2026"}, {"a%41b", "a_41b"}, {"%", "_"}, {"100%", "100_"},
		{"Mail%20backup", "Mail_20backup"}, {"%7Etilde", "_7Etilde"}, {"x%2Fy", "x_2Fy"}, {"50% off", "50_ off"},
	}
	var ins []string
	for _, c := range cases {
		ins = append(ins, c.in)
	}
	names, changed := FinalNames(append(ins, "Plain"))
	assure.Reached(t, names, "final names")
	for i, c := range cases {
		readByThunderbird(t, names[i], fmt.Sprintf("folder %q", c.in))
		if names[i] != c.want || !changed[i] || strings.Contains(names[i], "%") {
			t.Errorf("%q → %q (changed %v), want %q, changed", c.in, names[i], changed[i], c.want)
		}
	}
	if last := len(names) - 1; names[last] != "Plain" || changed[last] {
		t.Errorf("positive twin: a name with nothing to change is kept: %q %v", names[last], changed[last])
	}
	for _, n := range []string{"%", "100%", "a%41b", "50% off", "Budget%2026"} {
		if ThunderbirdReads(n) {
			t.Errorf("the product's check passes %q, which Thunderbird reads from another file or may (its newer store escapes every %%)", n)
		}
	}

	// The archive's own name: the top folder and each of its " (n)" forms.
	top := TopName("Mail%20backup")
	if top != TopPrefix+"Mail_20backup" {
		t.Errorf("the top for an archive named Mail%%20backup: %q", top)
	}
	for _, n := range []int{1, 2, 99} {
		readByThunderbird(t, withNumber(top, n), fmt.Sprintf("top (%d)", n))
	}
	in := []FolderIn{{Names: []string{"a%41b"}, Messages: 1}, {Names: []string{"Budget%2026"}, Messages: 1},
		{Names: []string{"100%"}, Messages: 1}, {Names: []string{"Plain"}, Messages: 1}}
	for _, goos := range []string{"windows", "linux"} {
		l, err := PlanLayout("Mail%20backup", in, `C:\Users\a\AppData\Roaming\Thunderbird\Profiles\x.default\Mail\Local Folders`, goos)
		if err != nil {
			t.Fatalf("%s: %v", goos, err)
		}
		if l.Changed != 4 {
			t.Errorf("%s: %d changed names, want 4 (the top and three folders)", goos, l.Changed)
		}
	}

	// End to end: the real Write on a fixture profile.
	m := []byte("Subject: pct\n\nbody\n")
	fx := newFixture(t, in, [][][]byte{{m}, {m}, {m}, {m}})
	l, err := PlanLayout("Mail%20backup", in, fx.lf.Dir, "")
	if err != nil {
		t.Fatal(err)
	}
	fx.layout = l
	fx.top = fx.freeTop()
	res, _ := fx.write(context.Background(), nil)
	if res.Outcome != OutDone || res.Restored != 4 {
		t.Fatalf("the restore: %s %d %v", res.Outcome, res.Restored, res.Err)
	}
	readByThunderbird(t, fx.top, "the written top")
	container := filepath.Join(fx.lf.Dir, fx.top+".sbd")
	for _, f := range l.Folders {
		leaf := f.Parts[len(f.Parts)-1]
		readByThunderbird(t, leaf, "written "+leaf)
		b, err := os.ReadFile(filepath.Join(container, filepath.FromSlash(tbFileFor(leaf, true))))
		if err != nil || !strings.Contains(string(b), "Subject: pct") {
			t.Errorf("%q: the file Thunderbird reads holds the message: %v %q", f.Names, err, b)
		}
	}

	// The defence: a layout holding a "%" (which the name steps never make) is refused at
	// the write before anything is made, and at the plan's check.
	for _, bad := range []string{"a%41b", "100%"} {
		fx := newFixture(t, []FolderIn{{Names: []string{"ok"}, Messages: 1}}, [][][]byte{{m}})
		fx.layout.Folders[0].Parts = []string{bad}
		before, beforeLog := fx.local(), fx.logBytes()
		res, _ := fx.write(context.Background(), nil)
		refusedAs(t, res.Err, "W-TBNAME", func() bool { return fx.local() == before && fx.logBytes() == beforeLog }, "nothing was restored")
		refusedAs(t, fx.layout.ThunderbirdReadsAll(withNumber(fx.layout.Top, 99)), "W-TBNAME", nil, "nothing was restored")
	}
}

// covers: MA-1088, R40, S69
// ADV-N5 (settled 2026-11-07, the lead's call): a Local Folders on a volume that stores
// names decomposed (Mac OS Extended, HFS+) hands Thunderbird the NFD spelling, where "é"
// is two UTF-16 units. Every name the restore writes is within Thunderbird's 55 units in
// BOTH forms, NFC and NFD, and is read as written in both. Combining-accent names: 27
// "é" (54 NFD units) kept, 28 cut; an NFD-spelt input; Hangul (three jamo each, NFD);
// "Déjà vu" kept; three equal siblings get " (n)" within both forms; the top NAME's room
// is counted the same way; and ThunderbirdReads refuses a name over 55 in NFD.
func TestANameIsWithinThunderbirdsLimitDecomposedToo(t *testing.T) {
	both := func(n, what string) {
		t.Helper()
		nfc, nfd := norm.NFC.String(n), norm.NFD.String(n)
		if utf16Len(nfc) > tbNameMax || utf16Len(nfd) > tbNameMax {
			t.Errorf("%s: %q is %d units NFC, %d NFD; both must be within %d", what, n, utf16Len(nfc), utf16Len(nfd), tbNameMax)
		}
		readByThunderbird(t, nfc, what+" (NFC)")
		readByThunderbird(t, nfd, what+" (NFD, as HFS+ hands it back)")
	}
	cases := []struct {
		in   string
		kept bool
	}{
		{strings.Repeat("é", 27), true},
		{strings.Repeat("é", 28), false},
		{strings.Repeat("é", 55), false},
		{norm.NFD.String(strings.Repeat("é", 30)), false},
		{strings.Repeat("한", 18), true},
		{strings.Repeat("한", 19), false},
		{"Déjà vu", true},
		{strings.Repeat("a", 54) + "é", false},
		{strings.Repeat("b", 53) + "é", true},
	}
	var ins []string
	for _, c := range cases {
		ins = append(ins, c.in)
	}
	names, changed := FinalNames(ins)
	for i, c := range cases {
		both(names[i], fmt.Sprintf("folder of %d NFD units", utf16Len(norm.NFD.String(c.in))))
		kept := names[i] == norm.NFC.String(c.in)
		if kept != c.kept {
			t.Errorf("%q (%d NFC, %d NFD units) → %q: kept %v, want %v", c.in, utf16Len(norm.NFC.String(c.in)), utf16Len(norm.NFD.String(c.in)), names[i], kept, c.kept)
		}
		if !c.kept && !changed[i] {
			t.Errorf("%q: a cut name is a changed name", c.in)
		}
	}
	if names[1] == names[2] {
		t.Errorf("two cut names keep their originals' hashes apart: %q", names[1])
	}

	same := []string{strings.Repeat("é", 27), strings.Repeat("é", 27), strings.Repeat("é", 27)}
	got, _ := FinalNames(same)
	for i, n := range got {
		both(n, fmt.Sprintf("sibling %d", i+1))
	}
	if got[0] == got[1] || got[1] == got[2] || !strings.HasSuffix(got[2], " (3)") {
		t.Errorf("equal siblings: %q", got)
	}

	for _, name := range []string{strings.Repeat("é", 19), strings.Repeat("é", 20), strings.Repeat("é", 39)} {
		top := TopName(name)
		for _, n := range []int{1, 2, 99} {
			both(withNumber(top, n), fmt.Sprintf("top for %d é, (%d)", utf16Len(name), n))
		}
		if kept := top == TopPrefix+name; kept != (len([]rune(name)) <= 19) {
			t.Errorf("the top NAME of %d é: %q (kept %v)", len([]rune(name)), top, kept)
		}
	}

	if ThunderbirdReads(strings.Repeat("é", 28)) {
		t.Error("the product's check passes a name of 56 NFD units")
	}
	if !ThunderbirdReads(strings.Repeat("é", 27)) {
		t.Error("positive twin: 54 NFD units are within the limit")
	}
}
