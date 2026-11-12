package app

import (
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/state"
)

// headersOnly writes one maildir message with headers and no body: a message whose content the
// source does not hold, so a run records it as not downloaded in full (a fillable record).
func headersOnly(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "cur"), 0o755); err != nil {
		t.Fatal(err)
	}
	msg := "From: a@example.com\r\nSubject: no body\r\nDate: Mon, 03 Mar 2025 09:00:00 +0000\r\nMessage-ID: <nobody@x>\r\n\r\n"
	if err := os.WriteFile(filepath.Join(dir, "cur", "1.eml"), []byte(msg), 0o644); err != nil {
		t.Fatal(err)
	}
}

// covers: MA-1157, R18, S29
// A real run records where it read its mail beside its messages not downloaded in full (the
// 1.6.1 friction review's P7): a plain maildir no mail app keeps is a mail file, and a folder
// under a Thunderbird profile (its prefs.js above it) is Thunderbird's; the last-run record and
// the history entry carry it. The classification: an IMAP account of Thunderbird's, an Outlook
// .ost and the Outlook export are a mail app's store, a mix of a mail app's store and a plain file
// is not known, Thunderbird's with another mail app's is a mail app's. Positive twin first: the
// plain maildir's run says mail file.
func TestARunRecordsWhereItReadItsMail(t *testing.T) {
	plain := filepath.Join(tmpDir(t), "Inbox")
	headersOnly(t, plain)
	out := tmpDir(t)
	r, err := runLocal(t, out, plain)
	if err != nil || r.Fillable != 1 || r.FillableFrom != state.FromMailFile {
		t.Fatalf("the positive twin: err %v fillable %d from %q", err, r.Fillable, r.FillableFrom)
	}
	if lr, _, _ := state.ReadLastRun(out); lr.FillableFrom != state.FromMailFile || lr.Fillable != 1 {
		t.Errorf("the record: fillable %d from %q", lr.Fillable, lr.FillableFrom)
	}

	profile := filepath.Join(tmpDir(t), "abcd.default-release")
	if err := os.MkdirAll(profile, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profile, "prefs.js"), []byte("// prefs\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Amended 2026-11-08 (the merge of main into tb-completeness): on this branch a profile is known
	// by its own files, compatibility.ini beside prefs.js (MA-1206), as Thunderbird writes them at
	// every start; a prefs.js alone no longer makes a folder Thunderbird's.
	if err := os.WriteFile(filepath.Join(profile, "compatibility.ini"), []byte("[Compatibility]\nLastVersion=153.6.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := filepath.Join(profile, "Mail", "Local Folders", "Inbox")
	headersOnly(t, store)
	tbOut := tmpDir(t)
	if r, err := runLocal(t, tbOut, store); err != nil || r.FillableFrom != state.FromThunderbird {
		t.Fatalf("a Thunderbird profile's folder: err %v from %q", err, r.FillableFrom)
	}
	h, _ := state.ReadRunHistory(tbOut)
	assure.Reached(t, len(h) == 1 && h[0].FillableFrom == state.FromThunderbird && h[0].Fillable == 1, "the history entry names Thunderbird")

	for name, c := range map[string]struct {
		sources []string
		want    string
	}{
		"an IMAP account":       {[]string{"/home/a/.thunderbird/x.default/ImapMail/imap.example.com"}, state.FromThunderbird},
		"an Outlook .ost":       {[]string{`C:\Users\a\AppData\Local\Microsoft\Outlook\a@example.com.ost`}, state.FromMailApp},
		"the Outlook export":    {[]string{`C:\Archive\_outlook-pst\Mailbox.pst`}, state.FromMailApp},
		"an Evolution cache":    {[]string{"/home/a/.cache/evolution/mail/1234abcd"}, state.FromMailApp},
		"a .pst":                {[]string{`D:\Old\backup.pst`}, state.FromMailFile},
		"a mail app and a file": {[]string{"/home/a/.thunderbird/x/ImapMail/imap.example.com", "/home/a/old.mbox"}, ""},
		"two mail apps":         {[]string{"/home/a/.thunderbird/x/ImapMail/imap.example.com", "/home/a/.cache/evolution/mail/1"}, state.FromMailApp},
		"nothing read":          {nil, ""},
	} {
		if got := fillableFrom(c.sources); got != c.want {
			t.Errorf("%s: %q, want %q", name, got, c.want)
		}
	}
}
