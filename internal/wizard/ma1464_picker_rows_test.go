package wizard

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/thunderbird"
)

// tbRow is a Thunderbird account's row for the pure tests: folder in profile, named name.
func tbRow(profile, folder, name string) pickRow {
	return pickRow{path: filepath.Join(profile, "ImapMail", folder), group: groupTBMail, kind: identFolder,
		folder: folder, tb: true, profile: profile, name: name, size: 1_400_000, sized: true}
}

// rowInvariants fails t unless labels hold MA-1464's rules: every row starts with an ordinary letter
// or digit, no two rows and no row and the All row are the same after NFKC and case folding, and
// every row's own text maps back to it.
func rowInvariants(t *testing.T, what string, labels []string) {
	t.Helper()
	seen := map[string]string{state.TBTextKey(allOfThem): allOfThem, state.TBTextKey(allOfThemBare): allOfThem}
	for i, l := range labels {
		r, _ := utf8.DecodeRuneInString(l)
		if !('a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || '0' <= r && r <= '9') {
			t.Errorf("%s: row %q does not start with an ordinary letter or digit", what, l)
		}
		k := state.TBTextKey(l)
		if other, dup := seen[k]; dup {
			t.Errorf("%s: row %q reads as %q", what, l, other)
		}
		seen[k] = l
		if j, all, ok := pickChoice(l, labels); !ok || all || j != i {
			t.Errorf("%s: row %q maps back to %d (all %v, ok %v), want %d", what, l, j, all, ok, i)
		}
	}
}

// covers: MA-1464
// Every row is one mailbox: never the All row, never a dialog program's option, never two rows alike.
// A name, folder or file that does not start with an ordinary letter or digit, or that starts with
// the words of the All row, of a folder no account uses, or of a lead word, gets "Account ", "Folder "
// or "File " before it; rows alike after NFKC and case folding (whole texts, with no cap) get " (2)",
// " (3)", so A, A and "A (2)" are three rows for three mailboxes, where the old list gave two "A (2)".
// Hostile names are cleaned and capped first. Positive twin first: ordinary rows carry no lead word
// and no suffix.
func TestEveryRowIsOneSourceNeverTheAllRowNeverAnOption(t *testing.T) {
	plain := []pickRow{
		{path: "/m/Archive", group: groupOther, kind: identFolder, folder: "Archive"},
		{path: "/m/Work", group: groupOther, kind: identFolder, folder: "Work"},
	}
	if labels, _ := pickList(plain, "darwin"); !reflect.DeepEqual(labels, []string{"Archive", "Work"}) {
		t.Fatalf("positive twin: ordinary rows: %q", labels)
	}

	three := []pickRow{
		{path: "/m/1", group: groupOther, kind: identFolder, folder: "A"},
		{path: "/m/2", group: groupOther, kind: identFolder, folder: "A"},
		{path: "/m/3", group: groupOther, kind: identFolder, folder: "A (2)"},
	}
	labels, paths := pickList(three, "darwin")
	if !reflect.DeepEqual(labels, []string{"A", "A (2)", "A (2) (2)"}) || !reflect.DeepEqual(paths, []string{"/m/1", "/m/2", "/m/3"}) {
		t.Fatalf("A, A and A (2): %q %q", labels, paths)
	}
	rowInvariants(t, "A, A and A (2)", labels)

	long := strings.Repeat("a", 79) + "@qz.example"
	twoLong := []pickRow{tbRow("/p1/x.default", "imap.x.example", long), tbRow("/p2/x.default", "imap.x.example", long)}
	labels, _ = pickList(twoLong, "darwin")
	rowInvariants(t, "two capped names from two profiles named alike", labels)

	for _, c := range []struct{ name, want string }{
		{"➤ All of them (and any added later)", "Account ➤ All of them (and any added later) (1.4 MB)"},
		{"All of them (and any added later)", "Account All of them (and any added later) (1.4 MB)"},
		{"ALL OF THEM", "Account ALL OF THEM (1.4 MB)"},
		{"ᐅ All of them (and any added later)", "Account ᐅ All of them (and any added later) (1.4 MB)"},
		{"--text=x", "Account --text=x (1.4 MB)"},
		{"--gtk-module=x", "Account --gtk-module=x (1.4 MB)"},
		{"-", "Account - (1.4 MB)"},
		{".x", "Account .x (1.4 MB)"},
		{"💼 Work", "Account 💼 Work (1.4 MB)"},
		{"שלום@qz.example", "Account שלום@qz.example (1.4 MB)"},
		{"\u0301x", "Account \u0301x (1.4 MB)"},
		{"Not in Thunderbird now: imap.x.example", "Account Not in Thunderbird now: imap.x.example (1.4 MB)"},
		{"Account -x", "Account Account -x (1.4 MB)"},
		{"Folder x", "Account Folder x (1.4 MB)"},
		{"q\x00z\u202e@qz\u200b.example", "qz@qz.example (1.4 MB)"},
		{strings.Repeat("z", 10240), strings.Repeat("z", 80) + "… (1.4 MB)"},
	} {
		labels, _ := pickList([]pickRow{tbRow("/p/x.default", "imap.x.example", c.name)}, "darwin")
		if len(labels) != 1 || labels[0] != c.want {
			t.Errorf("an account named %q: %q, want %q", c.name, labels, c.want)
		}
		rowInvariants(t, "named "+c.name, labels)
	}
	for _, c := range []struct {
		row  pickRow
		want string
	}{
		{pickRow{path: "/m/-x", group: groupOther, kind: identFolder, folder: "-x"}, "Folder -x"},
		{pickRow{path: "/m/-x.pst", group: groupOutlook, kind: identFile, folder: "-x.pst"}, "File -x.pst"},
		{pickRow{path: "/m/all", group: groupOther, kind: identFolder, folder: "all of them"}, "Folder all of them"},
		{pickRow{path: "/m/z", group: groupOther, kind: identFolder, folder: "\u200b"}, "Folder with no name"},
		{pickRow{path: "/m/z.pst", group: groupOutlook, kind: identFile, folder: "\u202e"}, "File with no name"},
	} {
		if labels, _ := pickList([]pickRow{c.row}, "darwin"); len(labels) != 1 || labels[0] != c.want {
			t.Errorf("%q: %q, want %q", c.row.folder, labels, c.want)
		}
	}

	// All of it in one list: one profile per name, so MA-1287's rule keeps every name.
	var mixed []pickRow
	for i, n := range []string{"➤ All of them (and any added later)", "all of them", "ALL OF THEM", "--text=x", "-", "-", "a", "A", "a (2)"} {
		mixed = append(mixed, tbRow("/p"+string(rune('a'+i))+"/x.default", "imap.x.example", n))
	}
	mixed = append(mixed, three...)
	labels, _ = pickList(mixed, "windows")
	rowInvariants(t, "a mixed list", labels)
}

// covers: MA-1465
// The answer maps back to its row: by its exact text; else by its whole text after NFKC and case
// folding (an answer in NFD for a row in NFC, or in other letter case), of which only one row can
// match; the All row also by its words without the arrow. "" and any other text map to no row.
// Positive twin first: each row's exact text gives that row.
func TestTheAnswerMapsBackToItsRow(t *testing.T) {
	labels := []string{"café@qz.example (2.0 MB)", "Work (empty)"}
	for i, l := range labels {
		if j, all, ok := pickChoice(l, labels); !ok || all || j != i {
			t.Fatalf("positive twin: %q gave %d %v %v", l, j, all, ok)
		}
	}
	for _, c := range []struct {
		answer string
		row    int
		all    bool
		ok     bool
	}{
		{"cafe\u0301@qz.example (2.0 MB)", 0, false, true},
		{"WORK (EMPTY)", 1, false, true},
		{"Work (empty)\n", 1, false, true},
		{allOfThem, -1, true, true},
		{allOfThemBare, -1, true, true},
		{"all of them (and any added later)", -1, true, true},
		{"", -1, false, false},
		{"Work", -1, false, false},
		{"/Users/qz/Library/Thunderbird", -1, false, false},
	} {
		if j, all, ok := pickChoice(c.answer, labels); j != c.row || all != c.all || ok != c.ok {
			t.Errorf("%q gave %d %v %v, want %d %v %v", c.answer, j, all, ok, c.row, c.all, c.ok)
		}
	}
}

// covers: MA-1462
// MA-1287's rules for the list: within one profile, two accounts named alike (after cleaning, NFKC
// and case folding) and a name that spells MailArchive's no-name words are said by their folders; a
// name said like another Thunderbird row's folder is said by its own folder, so a nameless row of
// that folder is never read as it; one address kept in two profiles keeps its name, and each row then
// says its profile's folder name. Positive twin first: distinct names in one profile are kept.
func TestNamesFollowTheSharedNameRuleWithinAProfile(t *testing.T) {
	p := "/qz/a1b2.default-release"
	labels, _ := pickList([]pickRow{tbRow(p, "imap.gmail-1.com", "qzsam.walker@gmail.com"), tbRow(p, "imap.gmail-2.com", "qzsam.work@gmail.com")}, "darwin")
	if !reflect.DeepEqual(labels, []string{"qzsam.walker@gmail.com (1.4 MB)", "qzsam.work@gmail.com (1.4 MB)"}) {
		t.Fatalf("positive twin: distinct names: %q", labels)
	}
	for _, c := range []struct {
		what string
		rows []pickRow
		want []string
	}{
		{"two named alike in one profile", []pickRow{tbRow(p, "imap.gmail-1.com", "Gmail"), tbRow(p, "imap.gmail-2.com", "gmail")},
			[]string{"imap.gmail-1.com (1.4 MB)", "imap.gmail-2.com (1.4 MB)"}},
		{"MailArchive's no-name words", []pickRow{tbRow(p, "imap.gmail-1.com", "the Thunderbird account kept in the folder imap.gmail.com")},
			[]string{"imap.gmail-1.com (1.4 MB)"}},
		{"a name like another row's folder", []pickRow{tbRow(p, "imap.gmail-1.com", "imap.gmail.com"), tbRow(p, "imap.gmail.com", "")},
			[]string{"imap.gmail-1.com (1.4 MB)", "imap.gmail.com (1.4 MB)"}},
		{"a name like its own folder", []pickRow{tbRow(p, "imap.gmail.com", "imap.gmail.com")},
			[]string{"imap.gmail.com (1.4 MB)"}},
		{"one address in two profiles", []pickRow{tbRow(p, "imap.gmail.com", "qzsam@gmail.com"), tbRow("/qz/e5f6.default", "imap.gmail.com", "qzsam@gmail.com")},
			[]string{"qzsam@gmail.com (profile default-release, 1.4 MB)", "qzsam@gmail.com (profile default, 1.4 MB)"}},
	} {
		labels, _ := pickList(c.rows, "darwin")
		if !reflect.DeepEqual(labels, c.want) {
			t.Errorf("%s:\n got %q\nwant %q", c.what, labels, c.want)
		}
		rowInvariants(t, c.what, labels)
	}
}

// covers: MA-1466
// The size counts the mail a mailbox keeps on this computer: every regular file under its folder at
// any depth, Thunderbird's .msf summaries and .dat files left out, Windows Search's .mozmsgs copies
// left out, no link followed; a store folder that is itself a link, or cannot be read, gets no size;
// a count stopped by its entry bound or its deadline says at least. A Thunderbird maildir store is
// counted too (MboxFiles, which walks only .sbd folders, gives it 0). Positive twin first: one mbox
// file reads its exact bytes.
func TestTheSizeCountsTheMailOnThisComputer(t *testing.T) {
	later := time.Now().Add(time.Minute)
	one := t.TempDir()
	pickFile(t, filepath.Join(one, "INBOX"), 1234)
	if n, floor, ok := storeSize(one, later, sizeEntryMax); n != 1234 || floor || !ok {
		t.Fatalf("positive twin: one mbox file: %d %v %v", n, floor, ok)
	}

	mbox := t.TempDir()
	for f, n := range map[string]int64{"INBOX": 100, "INBOX.msf": 999, "Archive.sbd/2024": 200, "Archive.sbd/2024.msf": 999, "popstate.dat": 50, "Archive.sbd/Old.sbd/2019": 300, "INBOX.mozmsgs/m1.wdseml": 5000} {
		p := filepath.Join(mbox, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		pickFile(t, p, n)
	}
	if n, floor, ok := storeSize(mbox, later, sizeEntryMax); n != 600 || floor || !ok {
		t.Errorf("nested .sbd folders, .msf, .dat and .mozmsgs left out: %d %v %v, want 600", n, floor, ok)
	}

	maildir := t.TempDir()
	for f, n := range map[string]int64{"cur/1.eml": 10, "new/2.eml": 20, "Sent/cur/3.eml": 30} {
		p := filepath.Join(maildir, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		pickFile(t, p, n)
	}
	if n, _, ok := storeSize(maildir, later, sizeEntryMax); n != 60 || !ok {
		t.Errorf("a maildir store: %d %v, want 60", n, ok)
	}
	if m := thunderbird.TotalSize(thunderbird.MboxFiles(maildir)); m != 0 {
		t.Errorf("the model: MboxFiles gives a maildir store %d, the reason it is not used", m)
	}

	if n, floor, ok := storeSize(mbox, later, 3); !floor || !ok || n > 600 {
		t.Errorf("the entry bound: %d %v %v, want a floor", n, floor, ok)
	}
	if _, _, ok := storeSize(mbox, time.Now().Add(-time.Second), sizeEntryMax); ok {
		t.Errorf("past the deadline a store gets no size")
	}
	if _, _, ok := storeSize(filepath.Join(mbox, "missing"), later, sizeEntryMax); ok {
		t.Errorf("a folder that is not there gets no size")
	}

	outside := t.TempDir()
	pickFile(t, filepath.Join(outside, "BIG"), 7000)
	linked := t.TempDir()
	pickFile(t, filepath.Join(linked, "INBOX"), 5)
	if err := os.Symlink(outside, filepath.Join(linked, "Other.sbd")); err != nil {
		t.Logf("no links here (%v): the link checks are skipped", err)
	} else {
		if n, _, ok := storeSize(linked, later, sizeEntryMax); n != 5 || !ok {
			t.Errorf("a linked folder below is not followed: %d %v, want 5", n, ok)
		}
		link := filepath.Join(t.TempDir(), "store")
		if err := os.Symlink(outside, link); err != nil {
			t.Fatal(err)
		}
		if _, _, ok := storeSize(link, later, sizeEntryMax); ok {
			t.Errorf("a store folder that is itself a link gets no size")
		}
	}

	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		locked := t.TempDir()
		pickFile(t, filepath.Join(locked, "INBOX"), 40)
		sub := filepath.Join(locked, "Locked.sbd")
		if err := os.Mkdir(sub, 0o755); err != nil {
			t.Fatal(err)
		}
		pickFile(t, filepath.Join(sub, "x"), 9)
		if err := os.Chmod(sub, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(sub, 0o755) })
		if n, floor, ok := storeSize(locked, later, sizeEntryMax); n != 40 || !floor || !ok {
			t.Errorf("a folder below that cannot be read: %d %v %v, want 40 as a floor", n, floor, ok)
		}
	}
}

// covers: MA-1466
// The size is said as the computer's own file browser says it: MB, GB or TB, one decimal below 10,
// whole from 10, "under 1 MB" below that, in 1,000s on macOS and Linux and 1,024s on Windows; an
// empty folder of an account Thunderbird downloads says "nothing downloaded yet", another empty one
// "empty"; a count stopped early says "over", and one stopped under 1 MB says nothing; an uncounted
// store says nothing. Positive twin first: 14 GB on macOS.
func TestTheSizeIsSaidAsTheFileBrowserSaysIt(t *testing.T) {
	if got := pickSizeWords(13_850_000_000, "darwin"); got != "14 GB" {
		t.Fatalf("positive twin: %q", got)
	}
	for _, c := range []struct {
		n        int64
		mac, win string
	}{
		{1, "under 1 MB", "under 1 MB"},
		{999_999, "under 1 MB", "under 1 MB"},
		{4_820_000, "4.8 MB", "4.6 MB"},
		{9_960_000, "10 MB", "9.5 MB"},
		{524_600_000, "525 MB", "500 MB"},
		{999_600_000, "1.0 GB", "953 MB"},
		{3_110_000_000, "3.1 GB", "2.9 GB"},
		{63_100_000_000, "63 GB", "59 GB"},
		{1_200_000_000_000, "1.2 TB", "1.1 TB"},
	} {
		if got := pickSizeWords(c.n, "darwin"); got != c.mac {
			t.Errorf("%d bytes on macOS: %q, want %q", c.n, got, c.mac)
		}
		if got := pickSizeWords(c.n, "linux"); got != c.mac {
			t.Errorf("%d bytes on Linux: %q, want %q", c.n, got, c.mac)
		}
		if got := pickSizeWords(c.n, "windows"); got != c.win {
			t.Errorf("%d bytes on Windows: %q, want %q", c.n, got, c.win)
		}
	}
	for _, c := range []struct {
		r    pickRow
		want string
	}{
		{pickRow{}, ""},
		{pickRow{sized: true, downloads: true}, "nothing downloaded yet"},
		{pickRow{sized: true}, "empty"},
		{pickRow{sized: true, floor: true, size: 13_850_000_000}, "over 14 GB"},
		{pickRow{sized: true, floor: true, size: 1000}, ""},
		{pickRow{sized: true, floor: true}, ""},
		{pickRow{sized: true, size: 4_820_000}, "4.8 MB"},
	} {
		if got := pickSizeOf(c.r, "darwin"); got != c.want {
			t.Errorf("%+v: %q, want %q", c.r, got, c.want)
		}
	}
}
