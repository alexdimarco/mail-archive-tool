package desktop

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/seal"
	"mail-archive-tool/internal/vault"
)

// Run now on an encrypted archive (design-encrypted-archives-phase3 §3.2, §3.6, §6;
// the P3-3 plan, 3d; MA-806).

const laterMbox = "From - Tue Jan 02 00:00:00 2024\r\n" +
	"From: Carol <carol@example.com>\r\nSubject: Second note\r\nDate: Tue, 02 Jan 2024 09:00:00 +0000\r\n" +
	"Message-ID: <def@example.com>\r\n\r\njust plain text here\r\n"

// runJobFor writes the run-job file Run now repeats for out, archiving box.
func runJobFor(t *testing.T, out, box string) {
	t.Helper()
	p, err := job.PathFor(schedule.DefaultNameFor(out))
	if err != nil {
		t.Fatal(err)
	}
	if err := job.Write(p, job.Job{Name: schedule.DefaultNameFor(out), Inputs: []string{box}, Out: out, Mode: "incremental", KeepRaw: true}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(p) })
}

// waitIdle waits for the dashboard's job to finish and returns its result and error.
func waitIdle(t *testing.T, e *keysEnv) (string, string) {
	t.Helper()
	for i := 0; i < 600; i++ {
		_, m := sessionRequest(t, e.h, http.MethodGet, "api/activity", "")
		if m["running"] != true {
			r, _ := m["result"].(string)
			er, _ := m["error"].(string)
			if er != "" {
				t.Logf("the job's log: %v", m["log"])
			}
			return r, er
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("the job did not finish")
	return "", ""
}

// covers: MA-806, MA-801, R37
// The keyed pause (E6): pausing an encrypted archive open here seals why in its
// record — the card shows it while paused — and writes the pause file empty, the
// reason nowhere in plaintext; Resume clears the sealed note. A locked archive still
// pauses, with the empty marker only.
func TestTheKeyedPauseSealsItsNote(t *testing.T) {
	out := backedUpEncrypted(t)
	e := newKeysEnv(t, out)
	e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword})
	if code, m := e.post(t, "api/pause-archive", map[string]any{"path": out}); code != http.StatusOK || m["ok"] != true || m["note"] != "" {
		t.Fatalf("pause: %d %v", code, m)
	}
	marker, _ := os.ReadFile(filepath.Join(out, seal.Name))
	if !seal.IsSealed(out) || len(marker) != 0 || !strings.Contains(sessionPage(t, e.h), "Paused: paused from MailArchive Desktop") {
		t.Fatalf("paused with the note sealed: marker %q", marker)
	}
	if code, m := e.post(t, "api/resume-archive", map[string]any{"path": out}); code != http.StatusOK || m["ok"] != true {
		t.Fatalf("resume: %d %v", code, m)
	}
	if note, err := keyring.PauseNote(e.cfg.Keys.get(out).u); err != nil || note != "" || seal.IsSealed(out) {
		t.Fatalf("resumed, the note cleared: %q %v", note, err)
	}
	e.post(t, "api/encryption/lock", nil)
	code, m := e.post(t, "api/pause-archive", map[string]any{"path": out})
	marker, _ = os.ReadFile(filepath.Join(out, seal.Name))
	assure.Reached(t, code == http.StatusOK && m["ok"] == true && seal.IsSealed(out) && len(marker) == 0, "a locked archive pauses with the empty marker")
}

// covers: MA-806, R37
// Finish setting up (§5.1 step 7): an encrypted archive made here whose first backup
// never ran says so on its card, Remember ticked by default for it; unlocked, Run now
// finishes it and the card no longer asks.
func TestFinishSettingUpAnEncryptedArchive(t *testing.T) {
	t.Cleanup(encrecord.SetDirForTest(t.TempDir()))
	out := filepath.Join(t.TempDir(), "New")
	p, err := keyring.Create(out, keysPassword, keyring.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	u, err := p.Commit()
	if err != nil {
		t.Fatal(err)
	}
	u.Destroy()
	box := filepath.Join(t.TempDir(), "Inbox")
	os.WriteFile(box, []byte(keysMbox), 0o600)
	runJobFor(t, out, box)
	e := newKeysEnv(t, out)
	page := sessionPage(t, e.h)
	if !strings.Contains(page, `id="encNotSetUp"`) || !strings.Contains(page, `id="encRemember" checked`) {
		t.Fatal("an unfinished archive's card asks to finish it, Remember ticked")
	}
	e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword})
	e.post(t, "api/run-backup", nil)
	if _, errText := waitIdle(t, e); errText != "" {
		t.Fatalf("run now: %s", errText)
	}
	assure.Reached(t, !strings.Contains(sessionPage(t, e.h), `id="encNotSetUp"`), "finished")
}

// covers: MA-806, R37
// Run now backs up an unlocked encrypted archive on a write lease of its own: a
// missing copy of the key file is rewritten from the good one first, and the open it
// was leased from no longer owes it (§3.6, design-named); the new message is sealed
// in — the reader finds it — and the archive gains no file a keyless reader could
// read. A Lock right after Run now starts does not stop it (§3.2). Locked, Run now is
// refused naming the Encryption card, nothing written.
func TestRunNowBacksUpAnUnlockedEncryptedArchive(t *testing.T) {
	out := backedUpEncrypted(t)
	box := filepath.Join(t.TempDir(), "Later")
	os.WriteFile(box, []byte(laterMbox), 0o600)
	runJobFor(t, out, box)
	e := newKeysEnv(t, out)

	ptr := filepath.Join(out, container.PointerName)
	before, _ := os.ReadFile(ptr)
	code, m := e.post(t, "api/run-backup", nil)
	after, _ := os.ReadFile(ptr)
	assure.Refused(t, code, toText(m), assure.Code(http.StatusOK), assure.Names("locked", "Encryption card"),
		assure.NoSideEffect(func() bool { return string(before) == string(after) }))

	mirror := filepath.Join(out, vault.BackupName)
	os.Remove(mirror)
	e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword})
	if page := sessionPage(t, e.h); !strings.Contains(page, `id="runBackupBtn"`) {
		t.Fatal("an unlocked encrypted archive with a run job is offered Run now")
	}
	if code, m := e.post(t, "api/run-backup", nil); code != http.StatusOK || m["ok"] != true {
		t.Fatalf("run now: %d %v", code, m)
	}
	e.cfg.Keys.lock(out) // the run's lease is its own
	result, errText := waitIdle(t, e)
	if errText != "" || !strings.Contains(result, "Backup complete") || strings.Contains(result, "index.html") {
		t.Fatalf("the run: %q %q", result, errText)
	}
	if _, err := os.Stat(mirror); err != nil {
		t.Fatal("the missing copy of the key file was not rewritten")
	}
	e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword})
	if code, body := e.read("/api/search?q=plain"); code != http.StatusOK || !strings.Contains(body, "Second note") {
		t.Fatalf("the new message is sealed in: %d %s", code, body)
	}
	var plain []string
	filepath.Walk(out, func(p string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() && (strings.HasSuffix(p, ".html") || strings.HasSuffix(p, ".log") || strings.HasSuffix(p, ".eml")) {
			plain = append(plain, p)
		}
		return nil
	})
	assure.Reached(t, len(plain) == 0 && e.cfg.Keys.get(out) != nil && !e.cfg.Keys.get(out).u.NeedsHeal(), "nothing readable without the key; nothing owed a heal")
}
