package tbrestore

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/util"
)

// The PCT-1 oracle: Thunderbird's own sibling compare, ported (not the product's key).
// nsMsgDBFolder::GetChildNamed compares with nsCaseInsensitiveUTF8StringComparator, which
// lowers each code point on its own (firefox nsUnicharUtils.cpp GetLowerUTF8Codepoint_inline:
// ASCII by a table, else ICU 78's u_tolower, the simple lowercase mapping of Unicode 17) and
// calls two names equal when they have as many code points and each pair lowers alike.
// Go's unicode.ToLower is the same mapping as of Unicode 15; unicode17Lower is the rest,
// typed here from Unicode 17.0.0's UnicodeData.txt (field 13) apart from the product's
// table, so a pair dropped there is caught here.
const unicode17Lower = `1C89 1C8A
A7CB 0264
A7CC A7CD
A7CE A7CF
A7D2 A7D3
A7D4 A7D5
A7DA A7DB
A7DC 019B
10D50 10D70
10D51 10D71
10D52 10D72
10D53 10D73
10D54 10D74
10D55 10D75
10D56 10D76
10D57 10D77
10D58 10D78
10D59 10D79
10D5A 10D7A
10D5B 10D7B
10D5C 10D7C
10D5D 10D7D
10D5E 10D7E
10D5F 10D7F
10D60 10D80
10D61 10D81
10D62 10D82
10D63 10D83
10D64 10D84
10D65 10D85
16EA0 16EBB
16EA1 16EBC
16EA2 16EBD
16EA3 16EBE
16EA4 16EBF
16EA5 16EC0
16EA6 16EC1
16EA7 16EC2
16EA8 16EC3
16EA9 16EC4
16EAA 16EC5
16EAB 16EC6
16EAC 16EC7
16EAD 16EC8
16EAE 16EC9
16EAF 16ECA
16EB0 16ECB
16EB1 16ECC
16EB2 16ECD
16EB3 16ECE
16EB4 16ECF
16EB5 16ED0
16EB6 16ED1
16EB7 16ED2
16EB8 16ED3`

var tbLowerExtra = func() map[rune]rune {
	m := map[rune]rune{}
	for _, line := range strings.Split(unicode17Lower, "\n") {
		f := strings.Fields(line)
		a, _ := strconv.ParseUint(f[0], 16, 32)
		b, _ := strconv.ParseUint(f[1], 16, 32)
		m[rune(a)] = rune(b)
	}
	return m
}()

// tbLower is Thunderbird's lowering of one code point.
func tbLower(r rune) rune {
	if r < 0x80 {
		if r >= 'A' && r <= 'Z' {
			return r + 0x20
		}
		return r
	}
	if l, ok := tbLowerExtra[r]; ok {
		return l
	}
	return unicode.ToLower(r)
}

// tbSameName is Thunderbird's GetChildNamed equality of two folder names.
func tbSameName(a, b string) bool {
	ra, rb := []rune(a), []rune(b)
	if len(ra) != len(rb) {
		return false
	}
	for i := range ra {
		if tbLower(ra[i]) != tbLower(rb[i]) {
			return false
		}
	}
	return true
}

// covers: MA-1088, R40, S69
// PCT-1 (settled 2026-11-07, the lead's call): every pair of sibling names Thunderbird calls
// one folder gets one restore key, over the whole code space: each code point against its
// Thunderbird lowercase, in a name as written, and as a Mac OS Extended volume hands it back
// decomposed (NFD; two names whose decomposed spellings Thunderbird calls equal). Cherokee
// (U+13A0 against U+AB70, the review's case), the dotted capital I against "i", and the
// Unicode 16 and 17 letters Go's tables do not know (Garay, Beria Erfe, the new Latin) are
// among them. Positive twin: names that differ are kept apart.
func TestTheSiblingKeyJoinsWhatThunderbirdJoins(t *testing.T) {
	var bad []string
	checked := 0
	for r := rune(0x20); r <= unicode.MaxRune; r++ {
		if !utf8.ValidRune(r) {
			continue
		}
		a := "a" + string(r) + "a"
		if l := tbLower(r); l != r {
			checked++
			if b := "a" + string(l) + "a"; siblingKey(a) != siblingKey(b) {
				bad = append(bad, fmt.Sprintf("U+%04X/U+%04X", r, l))
			}
		}
		d := []rune(norm.NFD.String(a))
		for i := range d {
			d[i] = tbLower(d[i])
		}
		b := norm.NFC.String(string(d))
		if b != a && tbSameName(norm.NFD.String(a), norm.NFD.String(b)) {
			checked++
			if siblingKey(a) != siblingKey(b) {
				bad = append(bad, fmt.Sprintf("NFD U+%04X", r))
			}
		}
	}
	assure.Reached(t, checked > 1400 && len(bad) == 0, fmt.Sprintf("%d pairs Thunderbird calls one folder; the restore's key keeps %d apart: %v (if Thunderbird's ICU moved past Unicode 17, re-read its UnicodeData.txt into newerLower)", checked, len(bad), bad))

	for _, p := range [][2]string{{"ᎠᎡ", "ꭰꭱ"}, {"Abc", "aBC"}, {"İş", "iş"}, {"\U00010D50", "\U00010D70"}, {"Ɤ", "ɤ"}} {
		assure.Reached(t, tbSameName(p[0], p[1]) && siblingKey(p[0]) == siblingKey(p[1]), fmt.Sprintf("%q and %q: one folder to Thunderbird, one key", p[0], p[1]))
	}
	for _, p := range [][2]string{{"Work", "Report"}, {"ᎠᎡ", "ᎠᎢ"}, {"a", "á"}, {"Inbox", "Inbox (2)"}} {
		assure.Reached(t, siblingKey(p[0]) != siblingKey(p[1]), fmt.Sprintf("positive twin: %q and %q are kept apart", p[0], p[1]))
	}
}

// covers: MA-1088, R40, S69
// The restore's key is a superset of util.FoldKey (the lead's call): every pair FoldKey
// joins it joins too, so nothing that clashed before stops clashing. Over the whole code
// space: code points that share a FoldKey in a name share the restore's key, and each
// code point's name shares the restore's key with its own FoldKey spelled out (so the
// full folds that grow a name, "ß" to "ss", "ﬁ" to "fi", are covered), and with its NFD.
func TestTheSiblingKeyJoinsEverythingFoldKeyJoins(t *testing.T) {
	byFold := map[string]string{}
	var bad []string
	n := 0
	for r := rune(0x20); r <= unicode.MaxRune; r++ {
		if !utf8.ValidRune(r) {
			continue
		}
		a := "a" + string(r) + "a"
		fk := util.FoldKey(a)
		if k, ok := byFold[fk]; ok && k != siblingKey(a) {
			bad = append(bad, fmt.Sprintf("U+%04X", r))
		} else if !ok {
			byFold[fk] = siblingKey(a)
		}
		for _, b := range []string{fk, norm.NFD.String(a)} {
			if b != a && util.FoldKey(b) == fk {
				n++
				if siblingKey(b) != siblingKey(a) {
					bad = append(bad, fmt.Sprintf("U+%04X/%q", r, b))
				}
			}
		}
	}
	for _, p := range [][2]string{{"Work", "work"}, {"I", "ı"}, {"Kelvin", "Kelvin"}, {"straße", "STRASSE"}, {"café", "café"}} {
		n++
		if util.FoldKey(p[0]) != util.FoldKey(p[1]) || siblingKey(p[0]) != siblingKey(p[1]) {
			bad = append(bad, fmt.Sprintf("%q/%q", p[0], p[1]))
		}
	}
	assure.Reached(t, n > 1000 && len(bad) == 0, fmt.Sprintf("of %d pairs FoldKey joins, the restore's key keeps %d apart: %v", n, len(bad), bad))
}

// covers: MA-1088, R40, S69, S74
// End to end (the review's PCT-1 scenario): an archive folder holding siblings "ᎠᎡ" and
// "ꭰꭱ" (Cherokee, one word in upper and lower case) and "Abc" and "aBC". The plan names the
// second of each pair with " (2)" (and lists it as changed), the real write restores all
// four and says so, and every written file is a folder of its own to Thunderbird's compare.
// On a case-blind Mac volume (APFS) the second create no longer meets the first, so the
// restore never stops part way with restored mail left behind; the encrypted twin finishes
// too. And the free top name: "Restored — ᎠᎡ" in Local Folders makes a new restore of an
// archive named "ꭰꭱ" take " (2)".
func TestCaseSiblingsThunderbirdJoinsAreEachRestored(t *testing.T) {
	names, changed := FinalNames([]string{"ᎠᎡ", "ꭰꭱ", "Abc", "aBC"})
	want := []string{"ᎠᎡ", "ꭰꭱ (2)", "Abc", "aBC (2)"}
	assure.Reached(t, fmt.Sprint(names) == fmt.Sprint(want) && !changed[0] && changed[1] && !changed[2] && changed[3],
		fmt.Sprintf("the second of each pair gets \" (2)\": %q %v", names, changed))

	in := []FolderIn{{Names: []string{"ᎠᎡ"}, Messages: 1}, {Names: []string{"ꭰꭱ"}, Messages: 1}, {Names: []string{"Abc"}, Messages: 1}, {Names: []string{"aBC"}, Messages: 1}}
	m := []byte("Subject: case\n\nbody\n")
	for _, enc := range []bool{false, true} {
		fx := newFixture(t, in, [][][]byte{{m}, {m}, {m}, {m}})
		fx.enc = enc
		res, _ := fx.write(context.Background(), nil)
		got := madeOnDisk(t, filepath.Join(fx.lf.Dir, fx.top+".sbd"))
		sort.Strings(got)
		wantFiles := append([]string(nil), want...)
		sort.Strings(wantFiles)
		assure.Reached(t, res.Outcome == OutDone && res.Restored == 4 && fileExists(filepath.Join(fx.lf.Dir, fx.top)),
			fmt.Sprintf("encrypted=%v: all four restored: %s %d %v %q", enc, res.Outcome, res.Restored, res.Err, res.Words))
		assure.Reached(t, fmt.Sprint(got) == fmt.Sprint(wantFiles), fmt.Sprintf("encrypted=%v: each folder's own file: %q", enc, got))
		for i := range got {
			for j := i + 1; j < len(got); j++ {
				assure.Reached(t, !tbSameName(tbDecodeFilename(got[i]), tbDecodeFilename(got[j])),
					fmt.Sprintf("%q and %q are one folder to Thunderbird", got[i], got[j]))
			}
		}
	}

	_, local := localProfile(t)
	writeFile(t, filepath.Join(local, TopPrefix+"ᎠᎡ"), "")
	if err := os.MkdirAll(filepath.Join(local, TopPrefix+"ᎠᎡ.sbd"), 0o700); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(local)
	if err != nil {
		t.Fatal(err)
	}
	top, n, err := FreeTopName(TopName("ꭰꭱ"), entries)
	assure.Reached(t, err == nil && n == 2 && top == TopPrefix+"ꭰꭱ (2)", fmt.Sprintf("the free top name steps past the one Thunderbird calls the same: %q %d %v", top, n, err))
}
