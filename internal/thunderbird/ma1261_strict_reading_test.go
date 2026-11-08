package thunderbird

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// offKey is server1's offline setting, the one these tests turn on.
const offKey = "mail.server.server1.offline_download"

// uncertainFiles are prefs.js and user.js shapes Thunderbird never writes, where MailArchive
// cannot be sure what Thunderbird applies (the C0c pass round 3's R3-01 to R3-04): each is
// added to prefs.js (after server1's off line) or is the whole user.js. A backslash is built
// from its code so no editor decodes the escape.
// what is the command line's naming of what is on the line.
func uncertainFiles() map[string]struct{ file, body, what string } {
	bs := string(rune(0x5c))
	const (
		nul      = "a NUL (zero) byte, where Thunderbird stops reading the file"
		notWhole = `text that is not a whole user_pref("name", value); statement`
		spanning = "a quoted value that goes on past the end of its line"
		slash    = "a setting name holding a backslash (an escape code)"
	)
	ignored := func(fn string) string {
		return "a " + fn + "( statement, which Thunderbird does not apply in prefs.js or user.js"
	}
	return map[string]struct{ file, body, what string }{
		"prefs.js: a NUL byte on a line of its own (R3-03a)":   {"prefs.js", "\x00\n", nul},
		"prefs.js: a NUL byte inside a comment":                {"prefs.js", "// a note \x00\n", nul},
		"prefs.js: a statement with no semicolon (R3-03b)":     {"prefs.js", `user_pref("mail.zz", 1)` + "\n", notWhole},
		"prefs.js: a line that is not a statement":             {"prefs.js", "hello\n", notWhole},
		"prefs.js: a string spanning lines (R3-04)":            {"prefs.js", `user_pref("mail.server.server1.name", "line one` + "\n" + off1 + "\n" + `end");` + "\n", spanning},
		"prefs.js: a pref( statement after the line (R3-01)":   {"prefs.js", `pref("` + offKey + `", true);` + "\n", ignored("pref")},
		"prefs.js: a sticky_pref( statement for another name":  {"prefs.js", `sticky_pref("mail.zz", 1);` + "\n", ignored("sticky_pref")},
		"prefs.js: a setting name with a backslash":            {"prefs.js", `user_pref("mail.z` + bs + `x7a", 1);` + "\n", slash},
		"user.js: pref( turning it on (R3-01)":                 {"user.js", `pref("` + offKey + `", true);` + "\n", ignored("pref")},
		"user.js: sticky_pref( turning it on (R3-01)":          {"user.js", `sticky_pref("` + offKey + `", true);` + "\n", ignored("sticky_pref")},
		"user.js: an escaped name keeping it off (R3-02)":      {"user.js", `user_pref("mail.server.server1.offline` + bs + `u005fdownload", false);` + "\n", slash},
		"user.js: a NUL byte":                                  {"user.js", "\x00", nul},
		"user.js: a statement it cannot parse, for another":    {"user.js", `user_pref("mail.zz", yes);` + "\n", notWhole},
		"user.js: a string spanning lines":                     {"user.js", `user_pref("mail.zz", "a` + "\n" + `b");` + "\n", spanning},
		"user.js: a missing semicolon on an unrelated setting": {"user.js", `user_pref("browser.zz", true)` + "\n", notWhole},
	}
}

// uncertainProfile is a one-account profile (server1 off, or on with on) with tc planted.
func uncertainProfile(t *testing.T, file, body string, on bool) (string, string) {
	t.Helper()
	own := off1
	if on {
		own = on1
	}
	if file == "user.js" {
		p, store := offProfile(t, own+"\n")
		writeAt(t, filepath.Join(p, "user.js"), body)
		return p, store
	}
	return offProfile(t, own+"\n"+body)
}

// covers: MA-1261, R9, S10
// MailArchive changes prefs.js only when it can read prefs.js and user.js with certainty (the C0c
// pass round 3's R3-01 to R3-04, the lead's strict rule). Either file holding a NUL byte anywhere,
// a statement it cannot parse (one missing its semicolon among them), a string value spanning
// lines, any pref( or sticky_pref( statement, or a setting name with a backslash is refused,
// whichever setting that line is about: Thunderbird never writes such a file, so it was changed
// by hand, and MailArchive does not model how Thunderbird reads it. The refusal is a
// HandEditedError naming the file and saying nothing was changed; prefs.js and user.js stay byte
// for byte, no backup is made, and "already on" is never reported either: AccountToEnable,
// CheckOffline and EnableOffline all refuse, with the account on or off. Positive twin first: a
// prefs.js and user.js as Thunderbird writes them, with comments and several statements on a
// line, are read and turned on.
func TestTheSettingsAreChangedOnlyWhenReadWithCertainty(t *testing.T) {
	p, store := offProfile(t, "// Mozilla User Preferences\n/* a\n block */\n"+off1+"\n"+
		`user_pref("mail.server.server1.name", "a \"quoted\" name // not a comment");`+" "+`user_pref("mail.zz", -1);`+"\n")
	writeAt(t, filepath.Join(p, "user.js"), "# mine\n"+`user_pref("browser.zz", true);`+"\n")
	if acct, err := AccountToEnable(p, store); err != nil || acct.OfflineDownload {
		t.Fatalf("the positive twin: %+v, %v", acct, err)
	}
	if c, err := EnableOffline(p, "server1"); err != nil || !c.Changed || !accountOn(t, p, store) {
		t.Fatalf("the positive twin: %+v, %v", c, err)
	}

	for name, tc := range uncertainFiles() {
		for _, on := range []bool{false, true} {
			what := name + map[bool]string{false: ", account off", true: ", account on"}[on]
			t.Run(what, func(t *testing.T) {
				p, store := uncertainProfile(t, tc.file, tc.body, on)
				prefsPath, userPath := filepath.Join(p, "prefs.js"), filepath.Join(p, "user.js")
				prefsBefore, _ := os.ReadFile(prefsPath)
				userBefore, userErr := os.ReadFile(userPath)
				untouched := func() bool {
					same := bytesStill(prefsPath, prefsBefore)() && absent(prefsPath+".mailarchive-bak")
					if userErr == nil {
						same = same && bytesStill(userPath, userBefore)()
					}
					return same
				}

				acct, err := AccountToEnable(p, store)
				var h *HandEditedError
				if !errors.As(err, &h) {
					t.Fatalf("AccountToEnable: %+v, %v; want a HandEditedError (never \"already on\")", acct, err)
				}
				refusedNaming(t, err, func() bool { return acct == nil && untouched() }, tc.file, "changed nothing", "Thunderbird never writes")

				err = CheckOffline(p, "server1")
				if !errors.As(err, &h) {
					t.Fatalf("CheckOffline: %v; want a HandEditedError", err)
				}

				c, err := EnableOffline(p, "server1")
				if !errors.As(err, &h) {
					t.Fatalf("EnableOffline: %+v, %v; want a HandEditedError (never \"already on\" or Changed)", c, err)
				}
				refusedNaming(t, err, func() bool { return !c.Changed && untouched() }, tc.file, offKey, "changed nothing", tc.what)
				if words, ok := PlainWords(err); !ok || !strings.Contains(words, "changed outside Thunderbird") || strings.Contains(words, tc.file) {
					t.Errorf("the wizard's words do not say the file was changed outside Thunderbird, or name the file:\n%s", words)
				}
			})
		}
	}
}

// covers: MA-1261, R9
// What the strict rule leaves readable: every shape Thunderbird itself writes, and the comment
// and spacing shapes the earlier passes held (a statement split by spaces, a tab, CRLF, a block
// comment, an escaped value) are still read, and a line for another setting holding a backslash
// in its value (not its name) is fine. Each turns the account on. The NUL and pref( checks look
// at the whole file, not only the setting's own line.
func TestCertainFilesAreStillChanged(t *testing.T) {
	bs := string(rune(0x5c))
	for name, body := range map[string]string{
		"a Windows path in another value": `user_pref("mail.server.server1.directory", "C:` + bs + bs + `Users` + bs + bs + `x");` + "\n" + off1 + "\n",
		"spaces, a tab and CRLF":          "\tuser_pref ( \"" + offKey + "\" , false ) ;  \r\n",
		"an escape in another value":      `user_pref("mail.identity.id1.fullName", "Zo` + bs + `u00eb");` + "\n" + off1 + "\n",
		"a pref( inside a comment":        "// pref(\"" + offKey + "\", true);\n/* sticky_pref(\"x\", 1); */\n" + off1 + "\n",
		"a NUL-free hash comment":         "# note\n" + off1 + "\n",
	} {
		t.Run(name, func(t *testing.T) {
			p, store := offProfile(t, body)
			if c, err := EnableOffline(p, "server1"); err != nil || !c.Changed || !accountOn(t, p, store) {
				t.Fatalf("%+v, %v", c, err)
			}
		})
	}
}
