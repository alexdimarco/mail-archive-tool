package thunderbird

import (
	"strings"
	"testing"
)

// covers: MA-1271, R12
// The C0c pass round 6's N3, settled in slice C3: a user.js refusal's "what" never says that
// Thunderbird writes the file. Each fragment speaks of Thunderbird's settings format, or of what
// Thunderbird uses, never of what it "never writes" or "writes", so a person told "user.js is a file
// people or add-ons write by hand" does not then read that Thunderbird wrote it. prefs.js keeps
// "Thunderbird never writes such a line" (it does write prefs.js). Positive twin first.
func TestUserJSWordsNeverSayThunderbirdWritesIt(t *testing.T) {
	line := "/* set by IT */\n"
	p, store := uncertainProfile(t, "prefs.js", line, false)
	if _, err := AccountToEnable(p, store); err == nil || !strings.Contains(err.Error(), ". Thunderbird never writes such a line, so the file was changed outside Thunderbird") {
		t.Fatalf("positive twin, prefs.js: %v", err)
	}
	p, store = uncertainProfile(t, "user.js", "// mine\n"+line, false)
	_, err := AccountToEnable(p, store)
	if err == nil {
		t.Fatal("user.js with a block comment was not refused")
	}
	plain, _ := PlainWords(err)
	for _, s := range []string{err.Error(), plain} {
		if strings.Contains(s, "Thunderbird never writes") || strings.Contains(s, "Thunderbird writes") {
			t.Errorf("a user.js refusal says Thunderbird writes it: %s", s)
		}
	}
	for name, what := range map[string]string{"whatNameBS": whatNameBS, "whatEscape": whatEscape, "whatComment": whatComment, "whatNotBool": whatNotBool, "whatNotText": whatNotText} {
		if strings.Contains(what, "never writes") || strings.Contains(what, "Thunderbird writes") {
			t.Errorf("%s: %q", name, what)
		}
	}
}

// covers: MA-1205, R12
// The C0c pass round 6's N2 and its pre-existing note, settled in slice C3: the user.js refusal
// tells the person to delete "the line that contains" the setting as it is written in user.js,
// in Thunderbird's escaped form (a quote as \" and a backslash as \\), so a search for it finds
// the line; and no control character in a server key or host reaches either refusal's words.
// Positive twin first: the name Thunderbird makes (serverN) is said as it is.
func TestTheUserJSLineIsNamedAsItIsWritten(t *testing.T) {
	plain := (&UserJSError{Path: "/p/user.js", Key: "mail.server.server1.offline_download", Host: "imap.example.com"}).Plain()
	if !strings.Contains(plain, "Delete the line that contains mail.server.server1.offline_download (or change") {
		t.Fatalf("positive twin: %s", plain)
	}
	e := &UserJSError{Path: "/p/user.js", Key: `mail.server.a"b\c` + "\x1b[2J.offline_download", Host: "imap.\x1bexample.com"}
	for _, s := range []string{e.Plain(), e.Error()} {
		if !strings.Contains(s, `mail.server.a\"b\\c[2J.offline_download`) {
			t.Errorf("the setting is not named as user.js holds it: %s", s)
		}
		if strings.Contains(s, "\x1b") {
			t.Errorf("a control character reaches the words: %q", s)
		}
	}
	h := &HandEditedError{File: "prefs.js", Line: 3, What: whatComment, Key: "mail.server.a\x1b]0;x\x07.offline_download", Host: "imap.\x07x"}
	for _, s := range []string{h.Error(), h.Plain()} {
		if strings.ContainsAny(s, "\x1b\x07") {
			t.Errorf("a control character reaches the hand-edited words: %q", s)
		}
	}
}
