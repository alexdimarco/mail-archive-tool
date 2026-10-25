package wizard

import (
	"bytes"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/ncruces/zenity"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/seal"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/vault"
)

// covers: MA-741, MA-746, R37
// Pausing an encrypted archive seals it with an empty marker: the owner's note is not
// written beside the archive in plaintext. The positive twin: a plaintext archive's
// pause keeps the note.
func TestPausingAnEncryptedArchiveKeepsNoNoteInIt(t *testing.T) {
	plain := t.TempDir()
	if err := PauseArchive(plain, "parked by the owner"); err != nil || seal.Reason(plain) != "parked by the owner" {
		t.Fatalf("the positive twin: a plaintext pause keeps its note (%v)", err)
	}
	sealed := t.TempDir()
	p, err := vault.PrepareCreate(sealed, "pause test passphrase")
	if err != nil {
		t.Fatal(err)
	}
	k, err := p.Commit()
	if err != nil {
		t.Fatal(err)
	}
	k.Destroy()
	// A folder inside the encrypted archive is not paused at all (MA-746): its pause
	// file, and the folder itself, would be written into the archive.
	inside := filepath.Join(sealed, "sub")
	if err := PauseArchive(inside, "SECRET note"); !errors.Is(err, app.ErrEncryptedNeedsKey) {
		t.Fatalf("pausing a folder inside an encrypted archive: want ErrEncryptedNeedsKey, got %v", err)
	}
	if _, err := os.Stat(inside); !os.IsNotExist(err) {
		t.Fatal("a refused pause created the folder inside the encrypted archive")
	}
	if err := PauseArchive(sealed, "SECRET note about the mail"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(sealed, seal.Name))
	if err != nil || len(raw) != 0 || !seal.IsSealed(sealed) {
		t.Fatalf("an encrypted archive's pause file must be an empty marker that still seals it: %q (%v)", raw, err)
	}
}

// covers: MA-732, R37
// The scheduled run-job (RunHeadless) and Run now (RunJobInProcess) refuse a keyless
// run into an encrypted archive BEFORE the run log opens in it: exit 1, a stderr line
// saying it is encrypted, the refusal recorded as the failed last run, and no
// mailarchive.log in the archive. The positive twin: the same job into a plaintext
// archive does open its run log there.
func TestARunJobRefusesAnEncryptedArchiveBeforeItsRunLog(t *testing.T) {
	// A failed run-job leaves a config-dir breadcrumb and a desktop notification:
	// both stay inside the test.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AppData", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())
	oldNotify := notify
	notify = func(string, ...zenity.Option) error { return nil }
	t.Cleanup(func() { notify = oldNotify })

	jobFor := func(t *testing.T, out string) string {
		t.Helper()
		if err := os.WriteFile(filepath.Join(out, ".mailarchive-manifest.json"), []byte(`{"version":6,"entries":{}}`), 0o600); err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(t.TempDir(), "backup.json")
		if err := job.Write(p, job.Job{Name: "backup", Out: out, Mode: "incremental", Inputs: []string{t.TempDir()}}); err != nil {
			t.Fatal(err)
		}
		return p
	}
	plain := t.TempDir()
	RunHeadless(jobFor(t, plain))
	if _, err := os.Stat(filepath.Join(plain, "mailarchive.log")); err != nil {
		t.Fatalf("the positive twin: a plaintext archive's run-job opens its run log: %v", err)
	}

	sealed := t.TempDir()
	jobPath := jobFor(t, sealed)
	p, err := vault.PrepareCreate(sealed, "run-job test passphrase")
	if err != nil {
		t.Fatal(err)
	}
	k, err := p.Commit()
	if err != nil {
		t.Fatal(err)
	}
	k.Destroy()

	r, w, _ := os.Pipe()
	old := os.Stderr
	os.Stderr = w
	rc := RunHeadless(jobPath)
	_ = w.Close()
	os.Stderr = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	lr, st, rerr := state.ReadLastRun(sealed)
	if rerr != nil || st != state.LastRunPresent || lr.Status != state.RunFailed {
		t.Fatalf("the refused run-job is not recorded as a failed run: %+v (%v)", lr, rerr)
	}
	noLog := assure.NoSideEffect(func() bool {
		_, err := os.Stat(filepath.Join(sealed, "mailarchive.log"))
		return os.IsNotExist(err)
	})
	assure.Refused(t, rc, buf.String(), assure.Code(1), assure.Names(sealed, "encrypted archive"), noLog)

	// Run now refuses before it starts anything: nothing reaches its log.
	var runLog bytes.Buffer
	_, err = RunJobInProcess(jobPath, log.New(&runLog, "", 0))
	if !errors.Is(err, app.ErrEncryptedNeedsKey) {
		t.Fatalf("Run now: want ErrEncryptedNeedsKey, got %v", err)
	}
	assure.Refused(t, 1, err.Error(), assure.Code(1), assure.Names(sealed), assure.NoSideEffect(func() bool {
		_, serr := os.Stat(filepath.Join(sealed, "mailarchive.log"))
		return os.IsNotExist(serr) && runLog.Len() == 0
	}))
}
