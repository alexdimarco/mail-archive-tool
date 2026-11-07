package tbrestore

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/util"
)

// covers: MA-1088, R40, S69
// SafeName and the name step (design §4.4 as folded, RT-6, N6): each hostile name
// becomes a name that is not empty, not ".", "..", hidden or a device, holds no
// separator, control, DEL, C1, U+200E/F or bidirectional control, and is not one
// Thunderbird skips (its source's rules: a leading "#", a trailing "~", its own file
// names, the .msf, .sbd, .snm, .toc and .mozmsgs endings); and, LAST, siblings that
// now spell the same — ".Inbox"/"Inbox."/"Inbox", "CON"/"_CON", "."/".."/"untitled",
// two names equal but for case — become distinct, in archive order.
func TestRestoreSafeNames(t *testing.T) {
	hostile := []string{"..", ".", "CON", "con.txt", "CON .txt", "COM0", "LPT¹", "CONIN$", "clock$", "Inbox.",
		".Inbox", "a\x00b", "a\x7fb", "a\u0085b", "a‎b‏", "‮evil.exe", "x.msf", "Inbox.sbd",
		"y.MOZMSGS", "z.toc", "w.snm", "#draft", "backup~", "msgFilterRules.dat", "popstate.dat", "filters.js",
		"feeds.json", "feeditems_error.log", "a/b", `a\b`, "q:r", "tab\there", "ok name"}
	for _, h := range hostile {
		n, _ := SafeName(h)
		switch {
		case n == "" || n == "." || n == ".." || strings.HasPrefix(n, "."):
			t.Errorf("%q → %q: empty, dot or hidden", h, n)
		case strings.ContainsAny(n, "/\\:\x00\x7f\u0085‎‏‮\t"):
			t.Errorf("%q → %q: a separator or control left", h, n)
		case isDeviceName(n):
			t.Errorf("%q → %q: a device name", h, n)
		case skipped(n):
			t.Errorf("%q → %q: Thunderbird would skip it", h, n)
		}
	}
	// "#draft" was "_#draft" until the MA-1095 lab (settled 2026-11-07): Thunderbird hashes
	// any name holding "#" (it showed "_#hash" empty), so "#" now becomes "_" anywhere.
	for h, want := range map[string]string{
		"#draft": "_draft", "backup~": "backup~_", "x.msf": "x.msf_", "Inbox.sbd": "Inbox.sbd_", "z.TOC": "z.TOC_",
		"msgFilterRules.dat": "msgFilterRules.dat_", "feeditems_error.log": "_feeditems_error.log",
		"COM0": "_COM0", "LPT¹": "_LPT¹", "CONIN$": "_CONIN$", "CON .txt": "_CON .txt", "con": "_con",
		"a‎b": "ab", "‮.x": "x",
	} {
		if got, _ := SafeName(h); got != want {
			t.Errorf("SafeName(%q) = %q, want %q", h, got, want)
		}
	}
	if n, ch := SafeName("ok name"); n != "ok name" || ch {
		t.Errorf("a plain name is kept as it is: %q %v", n, ch)
	}
	for _, group := range [][]string{
		{".Inbox", "Inbox.", "Inbox"},
		{"CON", "_CON"},
		{".", "..", "untitled"},
		{"Work", "WORK", "work"},
		{"café", "café"}, // NFC and NFD spellings of one name
	} {
		names, changed := FinalNames(group)
		seen := map[string]bool{}
		for i, n := range names {
			k := util.FoldKey(n)
			if seen[k] {
				t.Errorf("%v → %v: two names spell the same", group, names)
			}
			seen[k] = true
			if i > 0 && !changed[i] {
				t.Errorf("%v: a renamed sibling is counted as changed", group)
			}
		}
	}
	if names, _ := FinalNames([]string{"Work", "WORK"}); names[0] != "Work" || names[1] != "WORK (2)" {
		t.Errorf("the second gets \" (2)\" in archive order: %v", names)
	}
}

// covers: MA-1088, R40, S69
// The budgets (§4.4 note step 5, N6): at most 246 UTF-8 bytes and 246 UTF-16 units per
// name — 120 CJK characters (360 bytes) and 120 emoji (480 bytes, 240 units) are cut at a
// character boundary with the hash of the original kept, so two long names that differ
// only at the end stay distinct; the top folder counts "Restored — " in its budget; a
// name past " (99)" gets the hash suffix and is cut again to 251.
func TestRestoreNameBudgets(t *testing.T) {
	cjk := strings.Repeat("漢", 120)
	emoji := strings.Repeat("📨", 120)
	for _, s := range []string{cjk, emoji, strings.Repeat("a", 300)} {
		names, _ := FinalNames([]string{s + "1", s + "2"})
		for _, n := range names {
			if len(n) > 246 || utf16Len(n) > 246 || !strings.Contains(n, "~") {
				t.Errorf("%d bytes, %d units: %q", len(n), utf16Len(n), n)
			}
			if !isValidUTF8(n) {
				t.Errorf("cut inside a character: %q", n)
			}
		}
		if names[0] == names[1] {
			t.Error("two long names cut to one")
		}
	}
	if fits(emoji, 246) {
		t.Fatal("the emoji name is over the byte budget")
	}
	top := TopName(strings.Repeat("x", 400))
	if !strings.HasPrefix(top, TopPrefix) || len(top) > 246 || utf16Len(top) > 246 {
		t.Errorf("the top name counts its prefix: %d bytes", len(top))
	}
	// 101 siblings that all spell "n" once made safe (trailing dots trimmed): " (2)" …
	// " (99)", then the hash suffix.
	group := make([]string, 101)
	for i := range group {
		group[i] = "n" + strings.Repeat(".", i)
	}
	names, _ := FinalNames(group)
	seen := map[string]bool{}
	for i, n := range names {
		if seen[util.FoldKey(n)] {
			t.Fatalf("name %d repeats: %q", i, n)
		}
		seen[util.FoldKey(n)] = true
	}
	if names[0] != "n" || names[1] != "n (2)" || names[98] != "n (99)" || !strings.HasPrefix(names[99], "n~") || !strings.HasPrefix(names[100], "n~") {
		t.Errorf("(2)…(99), then the hash suffix: %q %q %q %q", names[1], names[98], names[99], names[100])
	}
	// N6: past " (99)" a name at the budget is cut again so it fits 251 with its suffix.
	long := strings.Repeat("ü", 123) // 246 bytes, 123 units
	same := make([]string, 101)
	orig := make([]string, 101)
	for i := range same {
		same[i], orig[i] = long, long+itoa(i)
	}
	dedupe(same, orig, make([]bool, 101))
	for i, n := range same {
		if len(n) > 251 || utf16Len(n) > 251 || !isValidUTF8(n) {
			t.Errorf("name %d: %d bytes, %d units", i, len(n), utf16Len(n))
		}
	}
	if !strings.Contains(same[99], "~") || same[99] == same[100] {
		t.Errorf("past (99): the hash suffix, distinct: %q", same[99][240:])
	}
}

// covers: MA-1088, R40, S69
// Every created path stays under Local Folders: an archive whose folder names are
// hostile restores into the one new folder and nowhere else; Local Folders holds,
// besides what was there, only "Restored — X" and its .sbd.
func TestRestoreHostileNamesStayUnderTheRoot(t *testing.T) {
	in := []FolderIn{
		{Names: []string{".."}, Messages: 1},
		{Names: []string{"..", ".."}, Messages: 1},
		{Names: []string{"../../escape"}, Messages: 1},
		{Names: []string{"CON"}, Messages: 1},
		{Names: []string{"x.msf"}, Messages: 1},
		{Names: []string{".hidden"}, Messages: 1},
	}
	m := []byte("Subject: h\n\nx\n")
	fx := newFixture(t, in, [][][]byte{{m}, {m}, {m}, {m}, {m}, {m}})
	parent := filepath.Dir(fx.lf.Dir)
	before, _ := os.ReadDir(parent)
	beforeLF, _ := os.ReadDir(fx.lf.Dir)
	res, _ := fx.write(context.Background(), nil)
	if res.Outcome != OutDone || res.Restored != 6 {
		t.Fatalf("%s %v %d", res.Outcome, res.Err, res.Restored)
	}
	after, _ := os.ReadDir(parent)
	afterLF, _ := os.ReadDir(fx.lf.Dir)
	if len(after) != len(before) {
		t.Fatal("something was made beside Local Folders")
	}
	if len(afterLF) != len(beforeLF)+2 {
		t.Fatalf("Local Folders gained more than the folder and its .sbd: %v", afterLF)
	}
	for _, e := range madeOnDisk(t, filepath.Join(fx.lf.Dir, fx.top+".sbd")) {
		for _, part := range strings.Split(e, "/") {
			if strings.HasPrefix(part, ".") || part == "CON" {
				t.Errorf("an unsafe name was written: %s", e)
			}
		}
	}
}

func isValidUTF8(s string) bool { return strings.ToValidUTF8(s, "�") == s }
