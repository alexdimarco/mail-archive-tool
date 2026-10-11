//go:build !windows

package app

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
)

// within runs fn and fails the test when it has not returned after d: a blocked
// open(2) on a planted FIFO never returns, and a hang must not hang the suite.
func within(t *testing.T, d time.Duration, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { defer close(done); fn() }()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("%s was still blocked after %v", what, d)
	}
}

// firstEml is the path of one message file under root.
func firstEml(t *testing.T, root string) string {
	t.Helper()
	var found string
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".eml") && found == "" {
			found = p
		}
		return nil
	})
	if found == "" {
		t.Fatal("no .eml in the export")
	}
	return found
}

// plantFifo replaces path with a named pipe that nothing writes to.
func plantFifo(t *testing.T, path string) {
	t.Helper()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Skipf("no FIFOs here: %v", err)
	}
}

// swapAfterProbe makes the first path probe of a folder swap the folder for a symlink to
// victim as soon as the real probe has answered: the gap between the identity probe and
// the open of the folder that EW-AI1 found. It returns a restore function.
func swapAfterProbe(t *testing.T, folder, victim string) func() {
	t.Helper()
	old := probeFolderFn
	swapped := false
	probeFolderFn = func(p string) (ceFolderSeen, error) {
		s, err := old(p)
		if !swapped && p == folder {
			swapped = true
			if rerr := os.Rename(folder, folder+".away"); rerr != nil {
				t.Error(rerr)
			}
			if lerr := os.Symlink(victim, folder); lerr != nil {
				t.Error(lerr)
			}
		}
		return s, err
	}
	return func() { probeFolderFn = old }
}

// victimDir is a directory the app has no business writing in, holding one file.
func victimDir(t *testing.T) (dir string, untouched func() bool) {
	t.Helper()
	dir = t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.txt"), []byte("the victim's own README\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, func() bool {
		ents, _ := os.ReadDir(dir)
		b, _ := os.ReadFile(filepath.Join(dir, "README.txt"))
		return len(ents) == 1 && string(b) == "the victim's own README\n"
	}
}

// covers: MA-429, MA-448, R29, R5
// EW-AI1 tombstone. Finish as partial probed the export folder by its path and then
// opened that path again with no check on the opened folder, so a co-writer who swapped
// the folder for a symlink in between made the finish write README, manifest and sums
// into the victim directory with the app's own replacing writes. The folder is now
// judged on the handle that is opened, once: a swap after the probe is R-r or R-r2 and
// nothing is written anywhere. The unswapped finish just before it is the positive twin.
func TestCopyExportFinishJudgesTheFolderItOpened(t *testing.T) {
	e, src := newCEEnv(t), newFakeSrc()

	// Positive twin: no swap, the same kill, Finish as partial works.
	ok := killedIn(t, e, src, "W4", ceMsgs(2))
	if fin, err := e.FinishAsPartial(ok.RunID); err != nil || fin.Status != CopyStatusFinishedAsPartial {
		t.Fatalf("the unswapped finish: %+v (%v)", fin, err)
	}

	res := killedIn(t, e, src, "W4", ceMsgs(2))
	victim, untouched := victimDir(t)
	defer swapAfterProbe(t, res.Root, victim)()
	_, err := e.FinishAsPartial(res.RunID)
	assure.Refused(t, 1, errString(err), assure.Code(1), assure.Names("export's folder"), assure.NoSideEffect(untouched))
	var ref *CopyRefusal
	if !errors.As(err, &ref) || (ref.ID != "R-r" && ref.ID != "R-r2") {
		t.Errorf("the swapped folder was refused as %v, want R-r or R-r2", err)
	}
}

// covers: MA-429, MA-448, R29, R5
// EW-AI1 tombstone, the reading side: Check this export probed by path and then opened
// the path again, so a swapped folder was hashed and reported on as the export.
func TestCopyExportCheckJudgesTheFolderItOpened(t *testing.T) {
	e, src := newCEEnv(t), newFakeSrc()
	res := mustRun(t, e, cePlan(t, e, src, ceMsgs(2)), src)
	if c, err := e.CheckExport(res.RunID); err != nil || !c.OK {
		t.Fatalf("the unswapped check: %+v (%v)", c, err)
	}
	victim, _ := victimDir(t)
	defer swapAfterProbe(t, res.Root, victim)()
	c, err := e.CheckExport(res.RunID)
	if err == nil {
		t.Fatalf("a swapped folder was checked and reported on: %+v", c)
	}
	var ref *CopyRefusal
	if !errors.As(err, &ref) || (ref.ID != "R-r" && ref.ID != "R-r2") {
		t.Errorf("the swapped folder was refused as %v, want R-r or R-r2", err)
	}
}

// covers: MA-421, MA-422, R5
// EW-AI2 tombstone. A co-writer who replaced the marker with a named pipe made the
// seal's marker read block in open(2) for ever with the ledger mutex held: the run
// never returned, and every later seal and Start waited on that mutex. The marker is now
// opened without blocking and refused unless it is a regular file, so the run ends, and
// a second export in the same process runs after it.
func TestCopyExportFifoMarkerDoesNotWedgeTheSeal(t *testing.T) {
	e, src := newCEEnv(t), newFakeSrc()
	p := cePlan(t, e, src, ceMsgs(2))
	root := filepath.Join(p.Dest, p.ExportName)
	e.SetStepHook(func(step string) {
		if step == "W9" {
			plantFifo(t, filepath.Join(root, copyMarkerName))
		}
	})
	var res *CopyResult
	var err error
	within(t, 5*time.Second, "the seal with a FIFO for a marker", func() {
		res, err = e.NewCopyRun(p, src).Run(context.Background())
	})
	if err != nil || res == nil || res.Status != CopyStatusComplete {
		t.Fatalf("the run with a FIFO marker: %+v (%v)", res, err)
	}
	e.SetStepHook(nil)
	// The mutex is free: another export in this process starts and finishes.
	within(t, 5*time.Second, "the next export", func() {
		next := mustRun(t, e, cePlan(t, e, src, ceMsgs(1)), src)
		if next.Status != CopyStatusComplete {
			t.Errorf("the next export: %s", next.Label)
		}
	})
}

// covers: MA-421, MA-422, R5
// EW-AI3 tombstone. A FIFO at a path SHA256SUMS.txt lists (or in place of
// SHA256SUMS.txt) blocked Check this export and Check and finish for ever: the file was
// opened, blocking, before anyone looked at what it was. Files are now opened without
// blocking and refused unless regular, so the check answers: the pipe is a file that
// changed or is missing, never a GREEN.
func TestCopyExportCheckDoesNotBlockOnSpecialFiles(t *testing.T) {
	e, src := newCEEnv(t), newFakeSrc()

	// A listed message file replaced by a pipe.
	res := mustRun(t, e, cePlan(t, e, src, ceMsgs(3)), src)
	eml := firstEml(t, res.Root)
	plantFifo(t, eml)
	var c *CopyCheck
	var err error
	within(t, 5*time.Second, "Check this export with a FIFO for a listed file", func() { c, err = e.CheckExport(res.RunID) })
	if err != nil || c.OK || len(c.Missing)+len(c.Changed) != 1 {
		t.Errorf("a pipe for a listed file: %+v (%v)", c, err)
	}

	// SHA256SUMS.txt itself replaced by a pipe.
	res2 := mustRun(t, e, cePlan(t, e, src, ceMsgs(2)), src)
	plantFifo(t, filepath.Join(res2.Root, copySumsName))
	within(t, 5*time.Second, "Check this export with a FIFO for the sums", func() { c, err = e.CheckExport(res2.RunID) })
	if err != nil || c.OK || len(c.Problems) == 0 {
		t.Errorf("a pipe for the sums: %+v (%v)", c, err)
	}

	// Check and finish on a sealed run whose marker is still there, a listed file a pipe.
	res3 := killedIn(t, e, src, "W9b", ceMsgs(2))
	plantFifo(t, firstEml(t, res3.Root))
	within(t, 5*time.Second, "Check and finish with a FIFO for a listed file", func() { _, err = e.CheckAndFinish(res3.RunID) })
	if !IsCheckFailed(err) {
		t.Errorf("Check and finish over a pipe: %v, want its checksum refusal", err)
	}
	if !exists(res3.Root, copyMarkerName) {
		t.Error("the marker was removed over a pipe")
	}

	// The folder probe reads the marker and README bounded too: a pipe there is no run id.
	res4 := killedIn(t, e, src, "W4", ceMsgs(1))
	plantFifo(t, filepath.Join(res4.Root, copyMarkerName))
	var seen ceFolderSeen
	within(t, 5*time.Second, "the folder probe with a FIFO for the marker", func() { seen, err = probeFolder(res4.Root) })
	if err != nil || seen.Nonce != "" {
		t.Errorf("the probe read a run id from a pipe: %+v (%v)", seen, err)
	}
}
