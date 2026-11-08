package thunderbird

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// applied is what Thunderbird applies for server1's offline setting, read by FindAccountForStore,
// failing when there is no account.
func applied(t *testing.T, p, store string) bool {
	t.Helper()
	acct, err := FindAccountForStore(p, store)
	if err != nil || acct == nil {
		t.Fatalf("no account (%v)", err)
	}
	return acct.OfflineDownload
}

// covers: MA-1207, R9, S10
// EnableOffline rewrites only a line that holds exactly one user_pref statement for the setting
// and nothing else but spaces and the line ending (the C0c pass round 2's R2-01 and R2-02). When
// a line for the setting holds anything else (another statement, a comment, a commented copy),
// or a line MailArchive cannot read as settings names it, or a comment is left open at the end
// of prefs.js so an added line would not count, it changes nothing: prefs.js keeps every byte,
// no backup is written, and the refusal names prefs.js and says it changed nothing. Positive
// twin first: a clean line, indented and with CRLF, is turned on.
func TestAHandEditedLineIsNeverRewritten(t *testing.T) {
	crlf := strings.ReplaceAll(live("server1")+imapServer("server1", "imap.gmail.com", "[ProfD]ImapMail/imap.gmail.com", ""), "\n", "\r\n") + "  " + off1 + "  \r\n"
	p := profileWith(t, crlf, "imap.gmail.com")
	c, err := EnableOffline(p, "server1")
	if err != nil || !c.Changed || !applied(t, p, filepath.Join(p, "ImapMail", "imap.gmail.com")) {
		t.Fatalf("the positive twin: %+v, %v", c, err)
	}
	if b, _ := os.ReadFile(filepath.Join(p, "prefs.js")); !strings.HasSuffix(string(b), "  "+on1+"  \r\n") {
		t.Fatalf("the positive twin's line was not rewritten in place: %q", b)
	}

	other := `user_pref("mail.server.server1.name", "keep me");`
	for name, body := range map[string]string{
		"a commented copy before the setting (R2-01)": "/* " + off1 + " */ " + off1 + "\n",
		"another setting after it (R2-02)":            off1 + " " + other + "\n",
		"another setting before it":                   other + " " + off1 + "\n",
		"a comment after it":                          off1 + " // note\n",
		"a comment closing before it":                 "/* old\n*/ " + off1 + "\n",
		"a line it cannot read names it":              `user_pref("mail.server.server1.offline_download", false)` + "\n",
		"a pref( line, not user_pref(":                `pref("mail.server.server1.offline_download", false);` + "\n",
		"a comment left open at the end":              "/* unfinished\n",
	} {
		t.Run(name, func(t *testing.T) {
			p, _ := offProfile(t, body)
			prefsPath := filepath.Join(p, "prefs.js")
			before, _ := os.ReadFile(prefsPath)
			c, err := EnableOffline(p, "server1")
			refusedNaming(t, err, func() bool {
				return bytesStill(prefsPath, before)() && absent(prefsPath+".mailarchive-bak") && !c.Changed
			}, "prefs.js", "mail.server.server1.offline_download", "changed nothing")
		})
	}
}

// covers: MA-1207, R9
// Reading agrees with Thunderbird on a hand-edited line (R2-02): statements are read one by one,
// so on a line with two settings each counts, and for a setting named twice the last statement
// wins, as Thunderbird's parser applies them. A setting named on a line MailArchive cannot read as
// settings is one it cannot be sure of: no account is given, and the error names the file and the
// setting, never a guess. Positive twin first: one clean line.
func TestReadingAgreesWithThunderbirdOnAHandEditedLine(t *testing.T) {
	p, store := offProfile(t, on1+"\n")
	if !applied(t, p, store) {
		t.Fatal("the positive twin reads off")
	}
	other := `user_pref("mail.server.server1.name", "keep me");`
	for name, tc := range map[string]struct {
		body string
		want bool
	}{
		"on, then another setting":  {on1 + " " + other + "\n", true},
		"off, then another setting": {off1 + " " + other + "\n", false},
		"off, then on, on one line": {off1 + " " + on1 + "\n", true},
		"on, then off, on one line": {on1 + off1 + "\n", false},
		"a commented copy, then on": {"/* " + off1 + " */ " + on1 + "\n", true},
	} {
		t.Run(name, func(t *testing.T) {
			p, store := offProfile(t, tc.body)
			if got := applied(t, p, store); got != tc.want {
				t.Fatalf("read %v; Thunderbird applies %v", got, tc.want)
			}
		})
	}

	for name, tc := range map[string]struct{ file, body string }{
		"prefs.js: no semicolon":     {"prefs.js", `user_pref("mail.server.server1.offline_download", true)` + "\n"},
		"prefs.js: an unknown value": {"prefs.js", `user_pref("mail.server.server1.offline_download", yes);` + "\n"},
		"user.js: no closing quote":  {"user.js", `user_pref("mail.server.server1.offline_download, true);` + "\n"},
	} {
		t.Run(name, func(t *testing.T) {
			p, store := offProfile(t, "")
			if tc.file == "user.js" {
				writeAt(t, filepath.Join(p, "user.js"), tc.body)
			} else {
				writeAt(t, filepath.Join(p, "prefs.js"), onePrefs+tc.body)
			}
			acct, err := FindAccountForStore(p, store)
			rc, msg := 0, ""
			if err != nil {
				rc, msg = 1, err.Error()
			}
			assure.Refused(t, rc, msg, assure.Code(1),
				assure.Names(tc.file, "mail.server.server1.offline_download"),
				assure.NoSideEffect(func() bool { return acct == nil }))
		})
	}
}
