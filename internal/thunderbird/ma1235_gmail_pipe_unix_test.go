//go:build unix

package thunderbird

import (
	"os"
	"path/filepath"
	"testing"
)

// covers: MA-1235, R25
// A pipe at prefs.js or user.js is never waited on: a scheduled backup asks the Gmail question
// unattended, so a planted pipe would hang the run (TC-15's hang, C0c's opener). The answer is
// "can't tell" at once, naming the file and "a pipe", and the pipe is left as it was. Positive twin
// first: the profile with a plain user.js is Gmail.
func TestGmailNeverWaitsOnAPipe(t *testing.T) {
	p, store := tbProfile(t, onePrefs+gmailLine("server1", "true"), "// mine\n")
	if !isGmail(t, p, store) {
		t.Fatal("the positive twin: a healthy Gmail profile is not Gmail")
	}
	for _, name := range []string{"prefs.js", "user.js"} {
		t.Run(name, func(t *testing.T) {
			p := t.TempDir()
			store := filepath.Join(p, "ImapMail", "imap.gmail.com")
			if err := os.MkdirAll(store, 0o755); err != nil {
				t.Fatal(err)
			}
			if name == "user.js" {
				writeAt(t, filepath.Join(p, "prefs.js"), onePrefs+gmailLine("server1", "true"))
			}
			pipe := filepath.Join(p, name)
			mkPipe(t, pipe)
			g := Gmail(-1)
			err := noHang(t, "GmailForStore", func() error {
				var err error
				g, err = GmailForStore(p, store)
				return err
			})
			if g != GmailCantTell {
				t.Fatalf("GmailForStore = %v, want can't tell", g)
			}
			refusedNaming(t, err, func() bool { return stillPipe(pipe) }, name, "a pipe", "imap.gmail.com")
		})
	}
}
