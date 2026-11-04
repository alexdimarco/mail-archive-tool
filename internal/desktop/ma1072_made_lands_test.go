package desktop

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/wizard"
)

// After "archive made", the page lands at Your archives on the new archive's row and follows its
// first backup there (MA-1072; the owner's pick Q5 of 2026-11-04, and the independent review's
// MED-3 of bc84ca5: on the Windows VM the page said "Its first backup is running now — it shows
// here as it goes", reloaded eight seconds later, and nothing showed that backup again, neither the
// real mailbox's 617 messages backed up nor a lab archive's failure).

// covers: MA-1072, R39, S41
// The page script: what "archive made" said is kept for the page drawn next, which lands at Your
// archives on the new archive's row, says it there with the keyboard on the row, and follows the
// first backup there — running, then finished or failed; another task's result is never said as
// its own; a browser that keeps nothing for the tab follows it in place. Positive twin first: a page
// drawn with nothing kept is not moved. Run under Node (testdata/pagescript/harness.js, madeLanding).
func TestANewArchiveLandsAtYourArchivesFollowingItsFirstBackup(t *testing.T) {
	if passed := runPageScenario(t, "madeLanding", true); len(passed) != 6 {
		t.Errorf("the madeLanding scenario passed %d of its 6 checks: %v", len(passed), passed)
	}
}

// covers: MA-1072, MA-1075, R37
// The server: "archive made" names the new archive's folder and the job its first backup runs as,
// and /api/activity names the same job while it runs and once it has ended, so the page follows
// that backup and no other; a later task in the slot is named as another job. The line says where
// the first backup shows (its row in Your archives), and the recovery file saved for it is spoken of
// in the Desktop's own words, not the command line's (MA-1075). Positive twin first: the archive is
// made and its first backup runs.
func TestArchiveMadeNamesTheFirstBackupThePageFollows(t *testing.T) {
	t.Cleanup(encrecord.SetDirForTest(t.TempDir()))
	box := filepath.Join(t.TempDir(), "Inbox")
	if err := os.WriteFile(box, []byte(keysMbox), 0o600); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "New")
	orig := addEncryptedFn
	t.Cleanup(func() { addEncryptedFn = orig })
	addEncryptedFn = func(check func(string) error) (*wizard.PendingAdd, error) {
		if err := check(out); err != nil {
			return nil, err
		}
		return wizard.PendingAddFor(out, []string{box}), nil
	}
	saveDir := t.TempDir()
	origPick := pickFolder
	pickFolder = func(context.Context, string) (string, error) { return saveDir, nil }
	t.Cleanup(func() { pickFolder = origPick })
	e := newKeysEnv(t, archiveAt(t, t.TempDir(), "Plain", map[string]string{"path:/mail/a.mbox": "a"}))

	_, m := e.post(t, "api/encryption/create", map[string]any{})
	token := m["token"]
	const pw = "the new archive's own passphrase"
	_, k := e.post(t, "api/encryption/create/key", map[string]any{"token": token, "newPassword": pw, "again": pw})
	if _, s := e.post(t, "api/encryption/new-recovery-key/save-file", map[string]any{"token": token}); s["ok"] != true {
		t.Fatalf("the recovery file was not saved: %v", s)
	}
	_, c := e.post(t, "api/encryption/new-recovery-key/confirm", map[string]any{"token": token, "typed": k["recoveryKey"]})
	if c["ok"] != true || c["running"] != true {
		t.Fatalf("the positive twin: the archive is made and its first backup runs: %v", c)
	}
	job, _ := c["job"].(string)
	_, a := sessionRequest(t, e.h, http.MethodGet, "api/activity", "")
	if c["path"] != out || job == "" || a["job"] != job {
		t.Errorf("archive made names %v (want %q) and job %q; /api/activity names job %v", c["path"], out, job, a["job"])
	}
	result, errText := waitIdle(t, e)
	_, a = sessionRequest(t, e.h, http.MethodGet, "api/activity", "")
	if errText != "" || result == "" || a["job"] != job {
		t.Errorf("the finished first backup is not the job the page follows: result %q, error %q, job %v (want %q)", result, errText, a["job"], job)
	}
	said := fmt.Sprint(c["lines"])
	if !strings.Contains(said, firstBackupRowLine) || strings.Contains(said, "it shows here as it goes") {
		t.Errorf("archive made does not say where the first backup shows: %s", said)
	}
	if !strings.Contains(said, recoveryFileLastNote) || strings.Contains(said, keyring.RecoveryFileNote) || strings.Contains(said, "`") {
		t.Errorf("archive made speaks of the recovery file in the command line's words: %s", said)
	}
	// Another task in the slot is another job: the page never says its result as the first backup's.
	if _, v := e.post(t, "api/verify", map[string]any{}); v["ok"] != true {
		t.Fatalf("a later task did not start: %v", v)
	}
	waitIdle(t, e)
	_, a = sessionRequest(t, e.h, http.MethodGet, "api/activity", "")
	assure.Reached(t, a["job"] != nil && a["job"] != "" && a["job"] != job, "a later task in the slot is named as another job")
}
