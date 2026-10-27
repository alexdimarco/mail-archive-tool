package desktop

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/keyring"
)

// The P3-4 adversarial pass's fixes on the Encryption card's copy (MA-830).

// covers: MA-830, R37
// A copy that does not finish is told as the copy's own failure (LEAK-03, FAIL-01):
// never "the backup failed", but that the copy did not finish and which folder now
// holds part of it, readable — to delete. A plan gone stale says to plan again, with
// nothing made.
func TestTheCardNamesAFailedCopysFolder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("an unreadable unit is made with a file mode")
	}
	out := backedUpEncrypted(t)
	parent := t.TempDir()
	pickingParent(t, parent)
	e := newKeysEnv(t, out)
	e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword})

	_, p := e.post(t, "api/encryption/copy/plan", nil)
	mirror := filepath.Join(out, container.MirrorName)
	raw, _ := os.ReadFile(mirror)
	os.WriteFile(mirror, append(raw, ' '), 0o600) // a backup since the plan
	e.post(t, "api/encryption/copy/start", map[string]any{"token": p["token"], "confirm": true})
	_, errText := waitIdle(t, e)
	entries, _ := os.ReadDir(parent)
	assure.Refused(t, 1, errText, assure.Code(1), assure.Names("plan the copy again"),
		assure.NoSideEffect(func() bool { return len(entries) == 0 && !strings.Contains(errText, "backup failed") }))
	os.WriteFile(mirror, raw, 0o600)

	filepath.WalkDir(filepath.Join(out, export.UnitsDir), func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			os.Chmod(p, 0)
		}
		return nil
	})
	_, p = e.post(t, "api/encryption/copy/plan", nil)
	e.post(t, "api/encryption/copy/start", map[string]any{"token": p["token"], "confirm": true})
	_, errText = waitIdle(t, e)
	entries, _ = os.ReadDir(parent)
	if len(entries) != 1 {
		t.Fatalf("the parent holds %d entries", len(entries))
	}
	dest := filepath.Join(parent, entries[0].Name())
	_, merr := os.Stat(filepath.Join(dest, app.CopyMarkerName))
	assure.Reached(t, strings.Contains(errText, "did not finish") && strings.Contains(errText, dest) && strings.Contains(errText, "READABLE") &&
		!strings.Contains(errText, "backup failed") && merr == nil, "the failure names the unfinished folder: "+errText)
}

// covers: MA-830, R37
// Lock and Shut down while a copy runs are told where the person acts (KEY-02): the
// plan says the copy runs to its end even if the archive is locked, that Shut down
// stops it, and that a stopped copy's folder is not remembered; a Lock while it runs
// answers that the copy goes on; Shut down stops it — the copy is handed the
// Desktop's own lifetime — and the task says it was stopped and names the folder.
func TestLockAndShutDownWhileACopyRuns(t *testing.T) {
	out := backedUpEncrypted(t)
	parent := t.TempDir()
	pickingParent(t, parent)
	started := make(chan struct{})
	orig := decryptCopy
	decryptCopy = func(ctx context.Context, u *keyring.Unlocked, e *app.CopyExportEnv, p *app.DecryptPlan, archives, settings []string, logger *log.Logger, onProgress func(app.DecryptReport)) (app.DecryptReport, error) {
		os.Mkdir(p.Dest, 0o700)
		if onProgress != nil {
			onProgress(app.DecryptReport{Copied: 1, Skipped: 1, Messages: 5})
		}
		close(started)
		<-ctx.Done()
		return app.DecryptReport{Dest: p.Dest, Unfinished: true}, fmt.Errorf("%w: %s is marked unfinished", app.ErrCopyStopped, p.Dest)
	}
	t.Cleanup(func() { decryptCopy = orig })

	life := NewLifecycle(context.Background())
	store := graphconfig.NewFileSecretStore(t.TempDir())
	keys := newArchiveKeys(store, readerLockedNote)
	t.Cleanup(func() { keys.lockAll(true) })
	settings := filepath.Join(t.TempDir(), "s.json")
	SaveSettings(settings, Settings{Out: out})
	d := newDashboard(Config{SettingsPath: settings, Store: store, Keys: keys, Lifecycle: life})
	if _, err := keys.unlock(out, keysPassword, keyring.OpenOptions{}); err != nil {
		t.Fatal(err)
	}
	p := callAction(t, d, d.copyPlanAction, nil)
	plan := strings.Join(toLines(p["lines"]), "\n")
	if !strings.Contains(plan, "even if you lock the archive") || !strings.Contains(plan, "does not remember a stopped copy") {
		t.Fatalf("the plan does not say what Lock and Shut down do: %q", plan)
	}
	if s := callAction(t, d, d.copyStartAction, map[string]any{"token": p["token"], "confirm": true}); s["ok"] != true {
		t.Fatalf("start: %v", s)
	}
	<-started
	d.cap.mu.Lock()
	progress := strings.Join(d.cap.lines, "\n")
	d.cap.mu.Unlock()
	if !strings.Contains(progress, "2 of 5 messages so far") {
		t.Fatalf("Activity shows no progress while the copy runs (CRIT-02): %q", progress)
	}
	l := callAction(t, d, d.lockAction, nil)
	assure.Reached(t, l["ok"] == true && str(l["note"]) == copyGoesOnText, "the Lock reply says the copy goes on")

	life.Stop()
	var errText string
	for i := 0; i < 400; i++ {
		d.cap.mu.Lock()
		running, msg := d.cap.running, d.cap.errMsg
		d.cap.mu.Unlock()
		if !running {
			errText = msg
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	entries, _ := os.ReadDir(parent)
	assure.Reached(t, len(entries) == 1 && strings.Contains(errText, "was stopped") && strings.Contains(errText, filepath.Join(parent, entries[0].Name())) && strings.Contains(errText, "READABLE"),
		"Shut down stopped the copy and the task names its folder: "+errText)
	if l := callAction(t, d, d.lockAction, nil); l["note"] != nil {
		t.Fatalf("a Lock with no copy running speaks of one: %v", l)
	}
}

// covers: MA-830, R37
// The copy's progress shows in Activity as it goes (CRIT-02): counts only, the first
// at once, then at most every few seconds.
func TestTheCopysProgressShowsAsCountsOnly(t *testing.T) {
	var buf strings.Builder
	at := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	p := copyProgress(log.New(&buf, "", 0), func() time.Time { return at })
	p(app.DecryptReport{Copied: 1, Messages: 9})
	p(app.DecryptReport{Copied: 2, Messages: 9}) // too soon
	at = at.Add(copyProgressEvery)
	p(app.DecryptReport{Copied: 3, Skipped: 1, Messages: 9})
	assure.Reached(t, buf.String() == "Unencrypted copy: 1 of 9 messages so far\nUnencrypted copy: 4 of 9 messages so far\n", "progress: "+buf.String())
}

// covers: MA-846, R37
// A copy's failure is worded by how far it got, not by a look at the path (the PG pass,
// FIX-01): a copy whose folder was made and then moved away says its folder holds
// readable mail, to find and delete; one that never made its folder says nothing was
// made.
func TestACopysFailureIsWordedByHowFarItGot(t *testing.T) {
	gone := &app.DecryptPlan{Name: "Enc-unencrypted-2026-10-27", Dest: filepath.Join(t.TempDir(), "moved-away")}
	failed := fmt.Errorf("%w: its folder was moved or replaced", app.ErrCopyFailed)
	made := copyFailureText(gone, app.DecryptReport{Unfinished: true}, failed)
	never := copyFailureText(gone, app.DecryptReport{}, failed)
	assure.Reached(t, strings.Contains(made, "moved or replaced") && strings.Contains(made, "READABLE") && strings.Contains(made, gone.Name) &&
		strings.Contains(never, "nothing was made"), made+" | "+never)
}

// covers: MA-830, R37
// The positive twin of asking twice (FID-08): a folder that may leave this computer,
// confirmed twice, is copied to.
func TestTheCardCopiesToAFolderConfirmedTwice(t *testing.T) {
	t.Setenv("OneDrive", "")
	t.Setenv("OneDriveCommercial", "")
	t.Setenv("OneDriveConsumer", "")
	out := backedUpEncrypted(t)
	synced := filepath.Join(t.TempDir(), "Dropbox")
	os.Mkdir(synced, 0o700)
	pickingParent(t, synced)
	e := newKeysEnv(t, out)
	e.post(t, "api/encryption/unlock", map[string]any{"secret": keysPassword})
	_, p := e.post(t, "api/encryption/copy/plan", nil)
	_, s := e.post(t, "api/encryption/copy/start", map[string]any{"token": p["token"], "confirm": true, "confirmAgain": true})
	res, errText := waitIdle(t, e)
	entries, _ := os.ReadDir(synced)
	assure.Reached(t, p["askTwice"] == true && s["ok"] == true && errText == "" && strings.Contains(res, "Unencrypted copy made at") && len(entries) == 1,
		"confirmed twice, the copy is made: "+res+errText)
}
