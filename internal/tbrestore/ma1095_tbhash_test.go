package tbrestore

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"unicode/utf16"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/util"
)

// Thunderbird's folder-name hashing, ported as the oracle (lab row MA-1095's BUG-1,
// settled 2026-11-07). Thunderbird finds a local folder's mbox at
// NS_MsgHashIfNecessary(name): a name it changes is kept under the changed (hashed) file
// name, so the file the restore wrote under the plain name is never read and the folder
// shows empty. A name the restore writes must therefore be one this function returns
// unchanged.
//
// Ported line for line from comm-central mailnews/base/src/nsMsgUtils.cpp at
// releases-comm-central 9ff8c8b16fba2d9f9668b416eb6f2d708494fe78 (2026-11-04):
// NS_MsgHashIfNecessary(const nsAString&), MAX_LEN = 55, ILLEGAL_FOLDER_CHARS ";#",
// ILLEGAL_FOLDER_CHARS_AS_FIRST_LETTER ".", ILLEGAL_FOLDER_CHARS_AS_LAST_LETTER ".~ ",
// and StringHash (XP_StringHash2) over the UTF-16 buffer's bytes; with FILE_PATH_SEPARATOR
// and FILE_ILLEGAL_CHARACTERS (CONTROL_CHARACTERS + OS_FILE_ILLEGAL_CHARACTERS) from
// xpcom/base/nsCRTGlue.h at mozilla-firefox/firefox 6149f492996791576ff4791f57bcbd47a4da27c0
// (2026-03-16). The buffer's bytes are taken little-endian, as on every platform
// Thunderbird ships for (x86-64, ARM64). This is the test's oracle only; the product's
// own check is ThunderbirdReads, held against it below.

const tbMaxLen = 55 // nsMsgUtils.cpp: const static uint32_t MAX_LEN = 55;

// tbControlChars is CONTROL_CHARACTERS: "\001".."\037", "\177", "\200".."\237". In
// NS_MsgHashIfNecessary they are part of a u"" literal, so each is one UTF-16 unit of
// that value (U+0080–U+009F for the octal 200–237).
var tbControlChars = func() string {
	var b strings.Builder
	for r := rune(1); r <= 0x1f; r++ {
		b.WriteRune(r)
	}
	for r := rune(0x7f); r <= 0x9f; r++ {
		b.WriteRune(r)
	}
	return b.String()
}()

// tbStringHash is nsMsgUtils.cpp's StringHash(const nsString&): phong's linear
// congruential hash over str.Length()*2 bytes of the char16_t buffer.
func tbStringHash(u []uint16) uint32 {
	h := uint32(1)
	for _, c := range u {
		for _, b := range [2]byte{byte(c), byte(c >> 8)} {
			h = 0x63c63cd9*h + 0x9c39c33d + uint32(b)
		}
	}
	return h
}

// tbHashIfNecessary is NS_MsgHashIfNecessary for a build whose FILE_PATH_SEPARATOR is
// "\\" (windows) or "/" (macOS, Linux); OS_FILE_ILLEGAL_CHARACTERS is `/:*?"<>|` on both
// (the Android set is not a Thunderbird platform).
func tbHashIfNecessary(name string, windows bool) string {
	u := utf16.Encode([]rune(name))
	if len(u) == 0 {
		return name // Nothing to do.
	}
	sep := "/"
	if windows {
		sep = `\`
	}
	set := sep + tbControlChars + `/:*?"<>|` + ";#"
	idx := -1
	for i, c := range u { // name.FindCharInSet(...)
		if strings.ContainsRune(set, rune(c)) {
			idx = i
			break
		}
	}
	if idx == -1 {
		last := len(u) - 1
		if u[0] == '.' {
			idx = 0
		} else if strings.ContainsRune(".~ ", rune(u[last])) {
			idx = last
		}
	}
	kept := -1
	if idx != -1 {
		kept = min(idx, tbMaxLen-8)
	} else if len(u) > tbMaxLen {
		kept = tbMaxLen - 8
	}
	if kept == tbMaxLen-8 && u[kept-1] >= 0xd800 && u[kept-1] <= 0xdbff {
		kept-- // To avoid keeping only the high surrogate of a surrogate pair
	}
	if kept >= 0 {
		hashed := fmt.Sprintf("%08x", tbStringHash(u)) // PR_snprintf(hashedname, 9, "%08lx", ...)
		u = append(u[:kept:kept], utf16.Encode([]rune(hashed))...)
	}
	return string(utf16.Decode(u))
}

// readByThunderbird fails the test unless Thunderbird keeps n under n itself, on both
// platforms' builds; what names the name's role.
func readByThunderbird(t *testing.T, n, what string) {
	t.Helper()
	for _, win := range []bool{true, false} {
		if got := tbHashIfNecessary(n, win); got != n {
			t.Errorf("%s: Thunderbird (windows=%v) keeps %q as %q, so it would show it empty", what, win, n, got)
		}
	}
	if !ThunderbirdReads(n) {
		t.Errorf("%s: the product's check refuses %q, which Thunderbird reads", what, n)
	}
}

// covers: MA-1088, R40, S69
// The oracle is Thunderbird's own: on the Windows VM (lab r8, Thunderbird 153.6.0) it made
// these hashed names for the restored folders it did not read, and the port gives exactly
// them; the 54- and 55-character names it read, the port leaves unchanged.
func TestTheOracleIsThunderbirdsHash(t *testing.T) {
	seen := map[string]string{
		"mid#dle":    "mid3085bdd1",
		"semi;colon": "semi04771469",
		"_#hash":     "_d4e62f63",
		"Restored — This is a long archive folder name for lab tests": "Restored — This is a long archive folder name f49c53148",
		strings.Repeat("c", 56): strings.Repeat("c", 47) + "ac643999",
		strings.Repeat("d", 60): strings.Repeat("d", 47) + "245b4b29",
	}
	for in, want := range seen {
		if got := tbHashIfNecessary(in, true); got != want {
			t.Errorf("tbHashIfNecessary(%q) = %q, Thunderbird made %q", in, got, want)
		}
	}
	for _, n := range []string{strings.Repeat("a", 54), strings.Repeat("b", 55), "plain control", "Входящие"} {
		if got := tbHashIfNecessary(n, true); got != n {
			t.Errorf("Thunderbird read %q as it is; the port changes it to %q", n, got)
		}
	}
	// The rest of the rule, each on its own: first ".", last ".", "~", " ", a control, a
	// separator per platform, and the surrogate pair kept whole at the cut.
	for _, n := range []string{".x", "x.", "x~", "x ", "a\x01b", "a\u0085b", "a:b", "a/b"} {
		if tbHashIfNecessary(n, true) == n || tbHashIfNecessary(n, false) == n {
			t.Errorf("%q is hashed on both platforms", n)
		}
	}
	if tbHashIfNecessary(`a\b`, true) == `a\b` || tbHashIfNecessary(`a\b`, false) != `a\b` {
		t.Error(`"\" is the separator on Windows only`)
	}
	// 46 "a" then emoji: unit 47 is a high surrogate, so 46 are kept, then the 8 hex.
	h := tbHashIfNecessary(strings.Repeat("a", 46)+strings.Repeat("📨", 5), true)
	if rest := strings.TrimPrefix(h, strings.Repeat("a", 46)); len(rest) != 8 || strings.Trim(rest, "0123456789abcdef") != "" {
		t.Errorf("the cut keeps no lone high surrogate: %q", h)
	}
}

// covers: MA-1088, R40, S69
// Every name the restore writes is one Thunderbird reads as written (the MA-1095 lab's
// BUG-1, settled 2026-11-07): for the lab's names — ";", "#" (leading and inside), 54, 55,
// 56, 60 and 120 characters, Unicode within and over the limit counted in UTF-16 units —
// the chosen name equals what NS_MsgHashIfNecessary returns for it (no hashing), and the
// changed ones are counted as changed; a name of 55 or fewer units with nothing to change
// is kept as it is.
func TestRestoredNamesAreNeverHashedByThunderbird(t *testing.T) {
	cases := []struct {
		in      string
		want    string // "" when only the rule is pinned (a cut name with its hash)
		changed bool
	}{
		{"semi;colon", "semi_colon", true},
		{"mid#dle", "mid_dle", true},
		{"#hash", "_hash", true},
		{"a;#b", "a__b", true},
		{strings.Repeat("a", 54), strings.Repeat("a", 54), false},
		{strings.Repeat("b", 55), strings.Repeat("b", 55), false},
		{strings.Repeat("c", 56), strings.Repeat("c", 46) + hashOf(strings.Repeat("c", 56)), true},
		{strings.Repeat("d", 60), "", true},
		{strings.Repeat("e", 120), "", true},
		{"Входящие", "Входящие", false},
		{"受信トレイ", "受信トレイ", false},
		{strings.Repeat("漢", 55), strings.Repeat("漢", 55), false}, // 55 units, 165 bytes
		{strings.Repeat("漢", 56), "", true},
		{strings.Repeat("📨", 27), strings.Repeat("📨", 27), false}, // 54 units
		{strings.Repeat("📨", 28), "", true},                       // 56 units
		{"ok~", "ok~_", true},
		{"Q1; Reports #2", "Q1_ Reports _2", true},
	}
	var ins []string
	for _, c := range cases {
		ins = append(ins, c.in)
	}
	names, changed := FinalNames(ins)
	for i, c := range cases {
		readByThunderbird(t, names[i], fmt.Sprintf("folder %q", c.in))
		if c.want != "" && names[i] != c.want {
			t.Errorf("%q → %q, want %q", c.in, names[i], c.want)
		}
		if changed[i] != c.changed {
			t.Errorf("%q → %q: changed %v, want %v", c.in, names[i], changed[i], c.changed)
		}
	}
	// Two cut names that differ only past the cut stay distinct (the hash of the original).
	if names[7] == names[8] || !strings.Contains(names[7], "~") {
		t.Errorf("cut names keep the original's hash: %q %q", names[7], names[8])
	}
	// Every hostile name of MA-1088, and a group that needs " (2)" … " (99)" and then the
	// hash suffix on a 55-unit name, all read by Thunderbird and all distinct.
	hostile := []string{"..", ".", "CON", "con.txt", "CON .txt", "COM0", "LPT¹", "CONIN$", "clock$", "Inbox.",
		".Inbox", "a\x00b", "a\x7fb", "a\u0085b", "a‎b‏", "‮evil.exe", "x.msf", "Inbox.sbd",
		"y.MOZMSGS", "z.toc", "w.snm", "#draft", "backup~", "msgFilterRules.dat", "popstate.dat", "filters.js",
		"feeds.json", "feeditems_error.log", "a/b", `a\b`, "q:r", "tab\there", "ok name", "LONGFO~1", " #x ~ "}
	same := make([]string, 102)
	for i := range same {
		same[i] = strings.Repeat("ü", 55)
	}
	for _, group := range [][]string{hostile, same} {
		got, _ := FinalNames(group)
		seen := map[string]bool{}
		for i, n := range got {
			readByThunderbird(t, n, fmt.Sprintf("%q", group[i]))
			if seen[util.FoldKey(n)] {
				t.Errorf("%q → %q repeats a sibling", group[i], n)
			}
			seen[util.FoldKey(n)] = true
		}
	}
}

// covers: MA-1088, R40, S69
// The top folder (the lab's whole-restore case): "Restored — NAME" and every " (n)" form
// up to " (99)" is read by Thunderbird, so NAME is kept when it is 39 or fewer units and is
// cut to 30 plus the hash of the archive's name past that; a changed NAME is a changed name
// in the plan's count. Through PlanLayout on both platforms' rules, for the lab's archive
// R8 Names2 under a 60-character archive name, every part restored is read too.
func TestTheRestoredTopNameIsNeverHashed(t *testing.T) {
	for _, name := range []string{"Work mail", strings.Repeat("n", 39), strings.Repeat("n", 40),
		"This is a long archive folder name for lab tests", strings.Repeat("Archive name ", 5),
		strings.Repeat("x", 400), "semi;colon #1", strings.Repeat("📨", 25), "Входящие"} {
		top := TopName(name)
		for _, n := range []int{1, 2, 99} {
			readByThunderbird(t, withNumber(top, n), fmt.Sprintf("top for %q, (%d)", name, n))
		}
		keep := utf16Len(name) <= 39 && !strings.ContainsAny(name, ";#")
		if (top == TopPrefix+name) != keep {
			t.Errorf("archive name %q (%d units) → %q: kept %v, want %v", name, utf16Len(name), top, top == TopPrefix+name, keep)
		}
	}
	if top := TopName(strings.Repeat("n", 40)); top != TopPrefix+strings.Repeat("n", 30)+hashOf(strings.Repeat("n", 40)) {
		t.Errorf("a 40-character NAME is cut to 30 plus its hash: %q", top)
	}

	long := "R8 Names2 — an archive whose own folder name is sixty-plus chars"
	in := []FolderIn{}
	for _, n := range []string{"semi;colon", "mid#dle", strings.Repeat("a", 54), strings.Repeat("b", 55),
		strings.Repeat("c", 56), strings.Repeat("d", 60), strings.Repeat("e", 120), "plain control", "Déjà vu", "受信トレイ"} {
		in = append(in, FolderIn{Names: []string{n}, Messages: 1})
	}
	in = append(in, FolderIn{Names: []string{"mid#dle", "#sub;child"}, Messages: 1})
	for _, goos := range []string{"windows", "darwin"} {
		l, err := PlanLayout(long, in, `C:\Users\a\AppData\Roaming\Thunderbird\Profiles\x.default\Mail\Local Folders`, goos)
		if err != nil {
			t.Fatalf("%s: %v", goos, err)
		}
		assure.Reached(t, l.Restored(), "restored folders")
		readByThunderbird(t, withNumber(l.Top, 99), goos+" top")
		for _, i := range l.Restored() {
			for _, p := range l.Folders[i].Parts {
				readByThunderbird(t, p, goos+" part of "+strings.Join(l.Folders[i].Names, "/"))
			}
		}
		// semi;colon, mid#dle, 56, 60, 120, #sub;child, and the top NAME: 7 changed names.
		if l.Changed != 7 {
			t.Errorf("%s: %d changed names, want 7 (the top's NAME among them)", goos, l.Changed)
		}
	}
	if l, _ := PlanLayout("Work mail", in[:1], `/home/a/Local Folders`, "linux"); l == nil || l.Changed != 1 {
		t.Error("a top NAME kept as it is is not counted as changed")
	}
}

// covers: MA-1088, R40, S69
// Defence in depth (MA-1095 lab, settled 2026-11-07): a layout holding a name Thunderbird
// would hash — which the name steps never make, so it is built by hand here — is refused
// at the write with W-TBNAME in words, before anything is made: Local Folders and the
// restore record unchanged, and no result saying "restored". The positive twin, the same
// layout with the name made safe, restores.
func TestAHashedNameIsRefusedBeforeAnythingIsWritten(t *testing.T) {
	m := []byte("Subject: h\n\nx\n")
	twin := newFixture(t, []FolderIn{{Names: []string{"semi;colon"}, Messages: 1}}, [][][]byte{{m}})
	if twin.layout.Folders[0].Parts[0] != "semi_colon" {
		t.Fatalf("the plan's name: %q", twin.layout.Folders[0].Parts[0])
	}
	if res, _ := twin.write(context.Background(), nil); res.Outcome != OutDone || res.Restored != 1 {
		t.Fatalf("positive twin: %s %v", res.Outcome, res.Err)
	}

	for _, bad := range []string{"semi;colon", "mid#dle", strings.Repeat("c", 56), "x."} {
		fx := newFixture(t, []FolderIn{{Names: []string{"ok"}, Messages: 1}}, [][][]byte{{m}})
		fx.layout.Folders[0].Parts = []string{bad}
		before, beforeLog := fx.local(), fx.logBytes()
		res, _ := fx.write(context.Background(), nil)
		if res.Outcome == OutDone || strings.Contains(res.Words, "Restored ") {
			t.Fatalf("%q: a hashed name was written and reported restored: %s %q", bad, res.Outcome, res.Words)
		}
		refusedAs(t, res.Err, "W-TBNAME", func() bool { return fx.local() == before && fx.logBytes() == beforeLog },
			"Thunderbird would show such a folder empty", "nothing was restored", "leave out the folders")
		if res.Words != wTBName {
			t.Errorf("%q: the result's words are the refusal's: %q", bad, res.Words)
		}
	}
	// The top name too, and the plan's own check.
	fx := newFixture(t, []FolderIn{{Names: []string{"ok"}, Messages: 1}}, [][][]byte{{m}})
	fx.top = "Restored — semi;colon"
	before := fx.local()
	res, _ := fx.write(context.Background(), nil)
	refusedAs(t, res.Err, "W-TBNAME", func() bool { return fx.local() == before }, "nothing was restored")
	l := &Layout{Top: TopPrefix + strings.Repeat("t", 40), Folders: []Folder{{Names: []string{"a"}, Parts: []string{"a"}, Parent: -1}}}
	refusedAs(t, l.ThunderbirdReadsAll(withNumber(l.Top, 99)), "W-TBNAME", nil, "nothing was restored")
}
