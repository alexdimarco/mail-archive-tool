package wizard

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
)

// hashEnding is util.SanitizeSegment's disambiguating ending ("~3fa2b1c0").
var hashEnding = regexp.MustCompile(`~[0-9a-f]{6,}$`)

// safeSegment fails the test unless name is one folder name this computer can use, at most max
// characters, that is not a device name, "untitled", or a name with SanitizeSegment's hash ending.
func safeSegment(t *testing.T, what, name string, max int) {
	t.Helper()
	switch {
	case name == "":
		t.Errorf("%s: an empty name", what)
	case !utf8.ValidString(name):
		t.Errorf("%s: %q is not whole characters", what, name)
	case utf8.RuneCountInString(name) > max:
		t.Errorf("%s: %q is %d characters, over %d", what, name, utf8.RuneCountInString(name), max)
	case app.FolderNameWhy(name) != "":
		t.Errorf("%s: %q fails the folder-name check: %s", what, name, app.FolderNameWhy(name))
	case strings.ContainsAny(name, "/\\:*?\"<>|"):
		t.Errorf("%s: %q holds a character a folder name cannot", what, name)
	case setReservedName(name):
		t.Errorf("%s: %q is a device name Windows reserves", what, name)
	case strings.EqualFold(name, "untitled"), strings.EqualFold(name, "New folder"):
		t.Errorf("%s: %q is a placeholder name", what, name)
	case hashEnding.MatchString(name):
		t.Errorf("%s: %q has SanitizeSegment's hash ending", what, name)
	case strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".") || strings.HasSuffix(name, " "):
		t.Errorf("%s: %q starts or ends with a dot or a space", what, name)
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) || (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) {
			t.Errorf("%s: %q holds a control or direction character %U", what, name, r)
		}
	}
}

// covers: MA-1492
// Folder names (§4): each archive folder is named for its mailbox as the list says it, without the
// row's details or lead word, with the suffix of its kind. Positive twins first: an account by its
// name, Local Folders as shown, an Outlook file without .pst and with " (Outlook)", an Evolution store
// with " (Evolution)", a folder no account uses with " (not in Thunderbird now)", an older copy with
// " (older copy)", a row with no name as the list says it, a row whose words need a lead word without
// it. Then hostile names (`..`, `/`, `\`, `CON`, `CONIN$`, `COM¹`, a trailing dot, control and
// right-to-left characters, 300 characters, "untitled") each come out as one safe segment of at most
// 60 characters, its suffix kept after the cut, passing app.FolderNameWhy, never "untitled" nor a `~`
// hash; and names alike after case folding and NFKC (and, where answers may be transcoded, in ASCII
// form) get " (2)", " (3)", at most 65 characters with the number. Adversarial.
func TestEachArchiveFolderIsNamedSafelyForItsMailbox(t *testing.T) {
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.Local)
	rows := []pickRow{
		{path: "/p1/ImapMail/imap.gmail-1.com", group: groupTBMail, kind: identFolder, folder: "imap.gmail-1.com", tb: true, profile: "/p1", name: "qzsam.walker@gmail.com", acct: "imap", host: "imap.gmail.com", sized: true, size: 10},
		{path: "/p1/Mail/Local Folders", group: groupTBLocal, kind: identFolder, folder: "Local Folders", tb: true, profile: "/p1", name: "Local Folders", acct: "none", sized: true},
		{path: "/files/Archive 2019.PST", group: groupOutlook, kind: identFile, folder: "Archive 2019.PST", sized: true, size: 10},
		{path: "/files/.ost", group: groupOutlook, kind: identFile, folder: ".ost", sized: true, size: 10},
		{path: "/home/e/.local/share/evolution/mail/local", group: groupOther, kind: identFolder, folder: "On This Computer", evolution: true, sized: true, size: 10},
		{path: "/p1/ImapMail/imap.gmail.com", group: groupUnused, kind: identFolder, folder: "imap.gmail.com", tb: true, profile: "/p1", unused: true, sized: true, size: 10},
		{path: "/p2/ImapMail/b", group: groupTBMail, kind: identFolder, folder: "b", tb: true, profile: "/p2", name: "pat@b.example", acct: "imap", host: "imap.b.example", sized: true, size: 10, changed: now.Add(-time.Hour)},
		{path: "/p3/ImapMail/b", group: groupTBMail, kind: identFolder, folder: "b", tb: true, profile: "/p3", name: "pat@b.example", acct: "imap", host: "imap.b.example", sized: true, size: 10, changed: now.Add(-40 * 24 * time.Hour)},
		{path: "/mail/\x01​", group: groupOther, kind: identFolder, folder: "\x01​", sized: true, size: 10},
		{path: "/mail/-x", group: groupOther, kind: identFolder, folder: "-x", sized: true, size: 10},
	}
	got := map[string]string{}
	for _, r := range setRowsAt(t, rows, now, "darwin") {
		got[r.path] = r.name
	}
	want := map[string]string{
		"/p1/ImapMail/imap.gmail-1.com":             "qzsam.walker@gmail.com",
		"/p1/Mail/Local Folders":                    "Local Folders",
		"/files/Archive 2019.PST":                   "Archive 2019 (Outlook)",
		"/files/.ost":                               "File with no name (Outlook)",
		"/home/e/.local/share/evolution/mail/local": "On This Computer (Evolution)",
		"/p1/ImapMail/imap.gmail.com":               "imap.gmail.com (not in Thunderbird now)",
		"/p2/ImapMail/b":                            "pat@b.example",
		"/p3/ImapMail/b":                            "pat@b.example (older copy)",
		"/mail/\x01​":                               "Folder with no name",
		"/mail/-x":                                  "-x",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("positive twins: the names of each kind:\n got %q\nwant %q", got, want)
	}
	for p, n := range got {
		safeSegment(t, p, n, setNameMax)
	}

	// Hostile names: each one safe segment of at most 60 characters, its suffix kept after the cut.
	long := strings.Repeat("ž", 300)
	for _, c := range []struct {
		base, suffix, want string
	}{
		{"..", "", setFallbackName},
		{"/", "", setFallbackName},
		{`\`, "", setFallbackName},
		{".", suffixUnused, "(not in Thunderbird now)"},
		{"CON", "", "_CON"},
		{"con.example", "", "_con.example"},
		{"CONIN$", "", setFallbackName},
		{"conout$.mail", "", setFallbackName},
		{"COM¹", "", setFallbackName},
		{"lpt³.x", "", setFallbackName},
		{"trailing.", "", "trailing"},
		{"a\x00b\x1fc\x7fd\u0085e", "", "abcde"},
		{"‮gnp.exe", "", "gnp.exe"},
		{"⁧sam⁩@x", "", "sam@x"},
		{"עברית@x", "", "עברית@x"},
		{"untitled", "", setFallbackName},
		{"New Folder", "", setFallbackName},
		{`a/b\c:d*e?f"g<h>i|j`, "", "a b c d e f g h i j"},
		{long, "", strings.Repeat("ž", 60)},
		{long, suffixUnused, strings.Repeat("ž", 60-len(suffixUnused)) + suffixUnused},
		{strings.Repeat("a", 59) + "क़", "", strings.Repeat("a", 59)},
	} {
		got := setSafeName(c.base, c.suffix)
		if got != c.want {
			t.Errorf("setSafeName(%q, %q) = %q, want %q", c.base, c.suffix, got, c.want)
		}
		safeSegment(t, c.base, got, setNameMax)
	}

	// Told apart, in order, after case folding and NFKC; in ASCII form only where answers may be
	// transcoded. With its number a name stays within 65 characters.
	sixty := strings.Repeat("m", 60)
	names := []string{"Sam@X.example", "sam@x.example", "ｓａｍ@x.example", "Søren", "Sören", sixty, strings.ToUpper(sixty)}
	if got, want := setToldApart(names, "darwin"), []string{"Sam@X.example", "sam@x.example (2)", "ｓａｍ@x.example (3)", "Søren", "Sören", sixty, strings.ToUpper(sixty) + " (2)"}; !reflect.DeepEqual(got, want) {
		t.Errorf("told apart on macOS:\n got %q\nwant %q", got, want)
	}
	linux := setToldApart(names, "linux")
	if linux[4] != "Sören (2)" {
		t.Errorf("on Linux, Sören reads as Søren in ASCII form: %q", linux)
	}
	for _, n := range assure.Reached(t, linux, "told-apart names") {
		safeSegment(t, "told apart", n, setNameMax+5)
	}
	// Every number to 99 taken: no name is left.
	many := make([]string, setNumberMax+1)
	for i := range many {
		many[i] = "same"
	}
	out := setToldApart(many, "darwin")
	if out[setNumberMax-1] != "same (99)" || out[setNumberMax] != "" {
		t.Errorf("the hundredth alike gets no name: %q, %q", out[setNumberMax-1], out[setNumberMax])
	}
}
