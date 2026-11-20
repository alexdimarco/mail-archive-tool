package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// covers: MA-1472, R18
// The plaintext record's mailbox_gone warning (docs/design-1.7.5-fixes.md §1.3): RunWarning gains
// Path, Name and Since, each left out when empty, so a record without them is written byte for
// byte as before (Since is omitzero: omitempty would write a zero time into every other warning);
// ReadLastRun cleans Path and Name of control characters at its one cleaning point, beside Code and
// Message, so every reader gets them clean. Positive twin first: a warning of the old shape.
func TestTheMailboxGoneWarningIsAdditiveAndCleaned(t *testing.T) {
	old := RunWarning{Code: "credential_in_archive", Message: "a secret file is inside the archive"}
	got, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"code":"credential_in_archive","message":"a secret file is inside the archive"}`; string(got) != want {
		t.Fatalf("a warning without the new fields must be written as before:\n%s\nwant\n%s", got, want)
	}

	out := t.TempDir()
	since := time.Date(2026, 11, 2, 2, 0, 0, 0, time.UTC)
	planted := LastRun{Status: RunOK, Started: since.Add(24 * time.Hour), Warnings: []RunWarning{{
		Code: MailboxGoneCode, Message: "Not found, so skipped.",
		Path: "/home/u/.thunderbird/p/ImapMail/imap.example\x1b[31m\nRED: forged", Name: "sam@example.com\r\x07", Since: since,
	}}}
	if err := WriteLastRun(out, planted); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(out, LastRunName))
	if !strings.Contains(string(raw), `"since": "2026-11-02T02:00:00Z"`) || !strings.Contains(string(raw), `"path": `) || !strings.Contains(string(raw), `"name": `) {
		t.Fatalf("the warning's path, name and since are written:\n%s", raw)
	}
	back, st, err := ReadLastRun(out)
	if err != nil || st != LastRunPresent || len(back.Warnings) != 1 {
		t.Fatalf("read back: %+v %v %v", back, st, err)
	}
	w := back.Warnings[0]
	if strings.ContainsAny(w.Path, "\x1b\n\r\x07") || strings.ContainsAny(w.Name, "\x1b\n\r\x07") {
		t.Fatalf("Path and Name must be cleaned at ReadLastRun: %q %q", w.Path, w.Name)
	}
	if !strings.HasPrefix(w.Path, "/home/u/.thunderbird/p/ImapMail/imap.example") || w.Name != "sam@example.com" || !w.Since.Equal(since) {
		t.Fatalf("the cleaned warning keeps its words and time: %+v", w)
	}
	if MailboxGoneNoteAfter != 14*24*time.Hour {
		t.Fatalf("the owner's 14 days (2026-11-20, Q2): %v", MailboxGoneNoteAfter)
	}
}
