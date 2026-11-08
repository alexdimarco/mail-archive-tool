package thunderbird

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// covers: MA-1265, R9, S10
// "Directly in <profile>/ImapMail/" follows the path rules of the OS (the C0c pass round 4's
// R4-03): on Windows "\" separates folders, on macOS and Linux it is part of a name. So on macOS
// and Linux a single folder named `ImapMail\imap.gmail.com` in the profile folder itself is not in
// ImapMail/ and gets no account by hostname: a NoAccountError, prefs.js byte for byte. Positive
// twins first: the real ImapMail/imap.gmail.com is found by hostname on the running OS, and on
// Windows the same backslash path is ImapMail/imap.gmail.com and found.
func TestABackslashIsASeparatorOnlyOnWindows(t *testing.T) {
	prefs := live("server1") + imapServer("server1", "imap.gmail.com", "", "") + off1 + "\n"
	p := profileWith(t, prefs, "imap.gmail.com")
	if got := accountKey(t, p, "imap.gmail.com"); got != "server1" {
		t.Fatalf("the positive twin: %s", got)
	}

	vals := map[string]string{
		"mail.accountmanager.accounts":         `"account1"`,
		"mail.account.account1.server":         `"server1"`,
		"mail.server.server1.hostname":         `"imap.gmail.com"`,
		"mail.server.server1.offline_download": "false",
	}
	if acct, err := matchAccount(vals, `C:\p`, `C:\p\ImapMail\imap.gmail.com`, "windows"); err != nil || acct.ServerKey != "server1" {
		t.Fatalf("the Windows twin: %+v, %v", acct, err)
	}
	for _, goos := range []string{"darwin", "linux"} {
		acct, err := matchAccount(vals, "/p", `/p/ImapMail\imap.gmail.com`, goos)
		var m *matchError
		if !errors.As(err, &m) || m.kind != NoAccountNone || acct != nil {
			t.Fatalf("%s: a folder named ImapMail\\imap.gmail.com in the profile folder: %+v, %v; want no account", goos, acct, err)
		}
	}

	// A value's \\ is one backslash (unquote decodes the escapes Thunderbird writes), so on macOS
	// and Linux a folder setting naming a folder with a backslash in its name matches that folder.
	vals["mail.server.server1.directory"] = `"/q/odd\\name"`
	for _, goos := range []string{"darwin", "linux"} {
		if acct, err := matchAccount(vals, "/p", `/q/odd\name`, goos); err != nil || acct.ServerKey != "server1" {
			t.Fatalf("%s: a folder setting with an escaped backslash: %+v, %v", goos, acct, err)
		}
	}

	if runtime.GOOS == "windows" {
		return // a backslash cannot be in a name here; the Windows rule is checked above
	}
	odd := filepath.Join(p, `ImapMail\imap.gmail.com`)
	if err := os.MkdirAll(odd, 0o755); err != nil {
		t.Fatal(err)
	}
	prefsPath := filepath.Join(p, "prefs.js")
	before, _ := os.ReadFile(prefsPath)
	acct, err := AccountToEnable(p, odd)
	var n *NoAccountError
	if !errors.As(err, &n) {
		t.Fatalf("AccountToEnable on %q: %+v, %v; want a NoAccountError", filepath.Base(odd), acct, err)
	}
	refusedNaming(t, err, func() bool { return acct == nil && bytesStill(prefsPath, before)() }, "changed nothing", filepath.Base(odd))
}
