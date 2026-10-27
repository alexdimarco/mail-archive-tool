package desktop

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/seal"
	"mail-archive-tool/internal/vault"
)

// covers: MA-818, R37
// Run now's write lease is destroyed when the run ends, and at once when the run
// cannot start because another task is running (FID-07): no key copy outlives it.
func TestRunNowsLeaseNeverOutlivesTheRun(t *testing.T) {
	out := backedUpEncrypted(t)
	box := filepath.Join(t.TempDir(), "Later")
	os.WriteFile(box, []byte(laterMbox), 0o600)
	runJobFor(t, out, box)
	e := newKeysEnv(t, out)
	e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword})
	var mu sync.Mutex
	var leases []*keyring.Unlocked
	orig := runLease
	runLease = func(en *keyEntry, o keyring.OpenOptions) (*keyring.Unlocked, error) {
		l, err := orig(en, o)
		mu.Lock()
		leases = append(leases, l)
		mu.Unlock()
		return l, err
	}
	defer func() { runLease = orig }()
	if _, m := e.post(t, "api/run-backup", nil); m["ok"] != true {
		t.Fatalf("Run now: %v", m)
	}
	_, refused := e.post(t, "api/run-backup", nil) // while the first runs, or after it
	mu.Lock()
	second := len(leases) == 2
	mu.Unlock()
	if refused["ok"] != true && second {
		if _, err := leases[1].Lease(); err == nil {
			t.Fatal("a Run now that could not start kept its key")
		}
	}
	if _, errText := waitIdle(t, e); errText != "" {
		t.Fatalf("the run: %s", errText)
	}
	for i, l := range leases {
		if _, err := l.Lease(); err == nil {
			t.Fatalf("lease %d outlived its run", i)
		}
	}
	assure.Reached(t, len(leases) > 0, "Run now took a lease")
}

// covers: MA-818, R37
// Reading mail is using the Desktop for the idle lock (K4; FID-08): a reader page at
// minute 20 keeps the archive open at minute 31, but the reader's own script fetches
// do not, so it locks at minute 51.
func TestReadingMailHoldsOffTheIdleLockButItsFetchesDoNot(t *testing.T) {
	out := backedUpEncrypted(t)
	e := newKeysEnv(t, out)
	start := time.Now()
	clock := start
	e.cfg.Keys.now = func() time.Time { return clock }
	e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword})
	clock = start.Add(20 * time.Minute)
	e.read("/")
	e.cfg.Keys.reap(start.Add(31 * time.Minute))
	if e.cfg.Keys.get(out) == nil {
		t.Fatal("a reader page at minute 20 did not hold off the idle lock at minute 31")
	}
	clock = start.Add(40 * time.Minute)
	e.read("/api/search?q=hello")
	e.cfg.Keys.reap(start.Add(51 * time.Minute))
	assure.Reached(t, e.cfg.Keys.get(out) == nil, "the reader's fetches are not a person's use")
}

// covers: MA-818, R37
// Pausing is a safety action (E6, FID-07): an encrypted archive open here whose
// sealed note cannot be saved is paused anyway, and the reply says the note was not
// saved.
func TestAPauseWhoseNoteCannotBeSavedStillPauses(t *testing.T) {
	out := backedUpEncrypted(t)
	e := newKeysEnv(t, out)
	e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword})
	orig := setPauseNote
	setPauseNote = func(context.Context, *keyring.Unlocked, string, *log.Logger) error {
		return errors.New("the drive is full")
	}
	defer func() { setPauseNote = orig }()
	_, m := e.post(t, "api/pause-archive", map[string]any{"path": out})
	assure.Reached(t, m["ok"] == true && seal.IsSealed(out) && strings.Contains(str(m["note"]), "could not be saved"), "paused, and told the note was not saved")
}

// covers: MA-818, R37
// "Open an archive folder…" says what a folder lacks in the No-archive card's words
// (REG-04): one with no manifest is told to pick the folder holding
// .mailarchive-manifest.json; one whose manifest cannot be read is told so and where
// its repair shows; a recorded encrypted folder whose key files are gone is refused
// (FID-08). The positive twin is an archive.
func TestOpenAnArchiveFolderSaysWhatTheFolderLacks(t *testing.T) {
	plain := archiveAt(t, t.TempDir(), "Plain", map[string]string{"path:/mail/a.mbox": "a"})
	if why := archiveFolderProblem(plain); why != "" {
		t.Fatalf("positive twin: %s", why)
	}
	empty := t.TempDir()
	if why := archiveFolderProblem(empty); !strings.Contains(why, ".mailarchive-manifest.json file") || strings.Contains(why, "README") {
		t.Fatalf("a folder with no archive: %s", why)
	}
	broken := t.TempDir()
	os.WriteFile(filepath.Join(broken, ".mailarchive-manifest.json"), []byte("{not json"), 0o600)
	os.WriteFile(filepath.Join(broken, "README.txt"), []byte("an archive"), 0o600)
	if why := archiveFolderProblem(broken); !strings.Contains(why, "cannot be read") || !strings.Contains(why, "Status & health") {
		t.Fatalf("an archive whose record cannot be read: %s", why)
	}
	t.Cleanup(encrecord.SetDirForTest(t.TempDir()))
	gone := recordedArchive(t, t.TempDir(), "Gone")
	for _, n := range []string{vault.FileName, vault.BackupName} {
		os.Remove(filepath.Join(gone, n))
	}
	assure.Reached(t, archiveFolderProblem(gone) != "", "a recorded folder whose key files are gone is refused")
}

// covers: MA-818, R37
// Repair is offered only for a record that is damaged (FID-10): one written by a newer
// MailArchive is not offered it.
func TestRepairIsOfferedOnlyForADamagedRecord(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(encrecord.SetDirForTest(dir))
	for _, n := range []string{encrecord.FileName, encrecord.MirrorName} {
		os.WriteFile(filepath.Join(dir, n), []byte("not a record\n"), 0o600)
	}
	if a := recordActions(""); len(a) != 1 || a[0] != actionRepairRecord {
		t.Fatalf("positive twin: a damaged record is offered Repair: %v", a)
	}
	newer := "MAILARCHIVE-RECORD 99 seq=7\n{}\n"
	for _, n := range []string{encrecord.FileName, encrecord.MirrorName} {
		os.WriteFile(filepath.Join(dir, n), []byte(newer), 0o600)
	}
	_, lerr := encrecord.Load()
	var ue *encrecord.UnusableError
	if !errors.As(lerr, &ue) || ue.Case == encrecord.CaseDamaged {
		t.Fatalf("fixture: a newer record: %v", lerr)
	}
	assure.Reached(t, len(recordActions("")) == 0, "a newer record is not offered Repair")
}
