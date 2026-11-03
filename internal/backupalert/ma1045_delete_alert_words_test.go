package backupalert

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/alert"
	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/archdelete" // installs the engine's unfinished-delete check
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/vault"
)

// covers: MA-1045, R36, R37
// A scheduled run refused because an ENCRYPTED archive's delete did not finish sends an alert that
// says what to do — finish the delete — not "the archive's drive or folder is not there — connect
// it", archive_absent's fixed sentence, which an encrypted archive's alert says in place of the
// error text (the G2 settle pass's MED-3). The refusal is kind delete_unfinished, with its own
// sentence; a deletion log that cannot be read is deletion_log_unreadable, with its own. Positive
// twin first: a plain archive's alert carries the refusal's whole words.
func TestAnEncryptedArchivesUnfinishedDeleteAlertSaysToFinishTheDelete(t *testing.T) {
	settingsPath := filepath.Join(t.TempDir(), "s.json")
	if err := os.WriteFile(settingsPath, []byte(`{"alertEnabled":true,"alertOnFailure":true,"alertHost":"smtp.example.org","alertPort":587,"alertFrom":"b@example.org","alertTo":"t@example.org","alertUsername":"b@example.org"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var sent []alert.Event
	restore := SetSenderForTest(func(_ alert.Config, _ string, ev alert.Event) error { sent = append(sent, ev); return nil })
	defer restore()
	logPath := filepath.Join(t.TempDir(), archdelete.LogName)
	started := func(folder string) {
		b, err := json.Marshal(map[string]any{"at": "2026-11-02T10:00:00Z", "event": "started", "run": "a1045-" + filepath.Base(folder), "folder": folder, "row": folder, "kind": "plain"})
		if err != nil {
			t.Fatal(err)
		}
		f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		_, werr := f.Write(append(b, '\n'))
		if cerr := f.Close(); werr != nil || cerr != nil {
			t.Fatal(werr, cerr)
		}
	}
	alertFor := func(folder string, err error) string {
		t.Helper()
		sent = nil
		FireForRun(settingsPath, nil, folder, ResultFailed, err, 0, nil)
		if len(sent) != 1 {
			t.Fatalf("one alert for %s: %+v", folder, sent)
		}
		return sent[0].Error
	}

	// Positive twin: a plain archive.
	plain := filepath.Join(t.TempDir(), "plain")
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	started(plain)
	perr := app.RefuseUnfinishedDelete(plain, logPath)
	assure.Reached(t, perr != nil, "precondition: the plain archive is refused")
	got := alertFor(plain, perr)
	assure.Reached(t, strings.Contains(got, "Delete not finished: click Delete again.") && strings.Contains(got, plain), "the plain archive's alert words: "+got)

	sealed := filepath.Join(t.TempDir(), "sealed")
	if err := os.MkdirAll(sealed, 0o755); err != nil {
		t.Fatal(err)
	}
	p, err := vault.PrepareCreate(sealed, "settle passphrase")
	if err != nil {
		t.Fatal(err)
	}
	k, err := p.Commit()
	if err != nil {
		t.Fatal(err)
	}
	k.Destroy()
	sealedNames := len(ma1045Names(sealed))
	started(sealed)
	serr := app.RefuseUnfinishedDelete(sealed, logPath)
	got = alertFor(sealed, serr)
	assure.Refused(t, 1, got, assure.Code(1),
		assure.Names("a delete of this archive did not finish, so it was not backed up — finish it with Delete… on its row in MailArchive Desktop"),
		assure.Forbid("connect it", sealed),
		assure.NoSideEffect(func() bool { return len(ma1045Names(sealed)) == sealedNames }))
	if k := app.FailureKind(serr); k != state.FailDeleteUnfinished {
		t.Errorf("the refusal's kind: %q, want %q", k, state.FailDeleteUnfinished)
	}

	// A deletion log that cannot be read.
	bad := filepath.Join(t.TempDir(), archdelete.LogName)
	if err := os.Mkdir(bad, 0o700); err != nil { // a folder where the file goes
		t.Fatal(err)
	}
	lerr := app.RefuseUnfinishedDelete(sealed, bad)
	got = alertFor(sealed, lerr)
	assure.Refused(t, 1, got, assure.Code(1),
		assure.Names("MailArchive could not read its deletion log, so it could not tell whether a delete of this archive did not finish, and it was not backed up — check that file in MailArchive's settings folder, then try again"),
		assure.Forbid("connect it", bad),
		assure.NoSideEffect(func() bool { return len(ma1045Names(sealed)) == sealedNames }))
	if k := app.FailureKind(lerr); k != state.FailDeletionLogUnreadable {
		t.Errorf("the refusal's kind: %q, want %q", k, state.FailDeletionLogUnreadable)
	}
}

// ma1045Names is dir's names, or nil when it cannot be read.
func ma1045Names(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var n []string
	for _, e := range entries {
		n = append(n, e.Name())
	}
	return n
}
