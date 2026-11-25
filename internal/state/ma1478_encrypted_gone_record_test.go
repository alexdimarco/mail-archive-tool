package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// covers: MA-1478, MA-734, R37
// An encrypted archive's record carries, beside its frozen allow-list, only what the owner allowed
// for a gone mailbox (docs/design-1.7.5-fixes.md §1.3; owner, 2026-11-20, Q3, Q5 and Q6):
// mailbox_gone (written only when true), and only beside it mailbox_gone_since (a time) and
// mailbox_gone_count (a number). The run's skip warnings, which hold the paths and names, are
// never written. Positive twin first: a record that skipped nothing has none of the three.
func TestAnEncryptedRecordKeepsOnlyThatAMailboxIsGoneItsTimeAndCount(t *testing.T) {
	out := t.TempDir()
	at := time.Date(2026, 11, 20, 2, 0, 0, 0, time.UTC)
	since := at.Add(-3 * 24 * time.Hour)
	r := LastRun{Status: RunOK, Started: at, Finished: at.Add(time.Minute), Encrypted: true,
		MailboxGoneSince: since, MailboxGoneCount: 2, // set without MailboxGone: never written
		Warnings: []RunWarning{{Code: MailboxGoneCode, Message: "Not found, so skipped: qzsam@example.com (/Users/someone/ImapMail/imap.qz.example)", Path: "/Users/someone/ImapMail/imap.qz.example", Name: "qzsam@example.com", Since: since}}}
	if err := WriteLastRun(out, r); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(out, LastRunName))
	for _, k := range []string{"mailbox_gone", "qzsam", "someone", "imap.qz", "warnings"} {
		if strings.Contains(string(raw), k) {
			t.Fatalf("positive twin: a record whose run skipped nothing holds %q:\n%s", k, raw)
		}
	}

	r.MailboxGone = true
	if err := WriteLastRun(out, r); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(filepath.Join(out, LastRunName))
	var keys map[string]any
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatal(err)
	}
	for k := range keys {
		switch k {
		case "version", "status", "started", "finished", "error_kind", "encrypted", "program",
			"mailbox_gone", "mailbox_gone_since", "mailbox_gone_count":
		default:
			t.Fatalf("an encrypted archive's record carries %q, outside the allow-list:\n%s", k, raw)
		}
	}
	if keys["mailbox_gone"] != true || keys["mailbox_gone_since"] != since.Format(time.RFC3339) || keys["mailbox_gone_count"] != float64(2) {
		t.Fatalf("the gone mailbox's three fields: %v", keys)
	}
	for _, s := range []string{"qzsam", "someone", "imap.qz", "ImapMail", "Not found"} {
		if strings.Contains(string(raw), s) {
			t.Fatalf("the encrypted record names %q:\n%s", s, raw)
		}
	}
	back, st, err := ReadLastRun(out)
	if err != nil || st != LastRunPresent || !back.MailboxGone || !back.MailboxGoneSince.Equal(since) || back.MailboxGoneCount != 2 || len(back.Warnings) != 0 {
		t.Fatalf("read back: %+v %v", back, err)
	}

	// A plaintext record never sets them, so it is written as before.
	plain := t.TempDir()
	p := LastRun{Status: RunOK, Started: at, Finished: at.Add(time.Minute)}
	if err := WriteLastRun(plain, p); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(filepath.Join(plain, LastRunName)); strings.Contains(string(raw), "mailbox_gone") {
		t.Fatalf("a plaintext record must not carry the encrypted fields:\n%s", raw)
	}
}
