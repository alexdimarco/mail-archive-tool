package source

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/state"
)

// releasePipe lets a goroutine that blocked opening the pipe at path go, once the test is
// over (a test that finds the hang must not leave a goroutine in the process).
func releasePipe(t *testing.T, path string) {
	t.Helper()
	t.Cleanup(func() {
		if f, err := os.OpenFile(path, os.O_RDWR, 0); err == nil {
			f.Close()
		}
	})
}

// The tombstones of FI-S2b's adversarial pass (docs/review-fault-isolation-s2b-adversarial.md).

// deepChain is the plain-directory chain "d/d/.../d", n levels.
func deepChain(n int) string { return strings.TrimSuffix(strings.Repeat("d/", n), "/") }

// covers: MA-322, R10, S47
// S2B-AO1 (and S2B-AC6): a store of plain directories nested thousands deep is bounded
// like the PST and Graph hierarchies are (the design's hostile-store row: cycle and
// depth bounded). Every descent re-opens its whole relative path through the root, so an
// unbounded chain made the walk's cost grow faster than its depth (800 levels took 14 s,
// 2,000 did not finish). A chain within the cap is read in full; one past it is a terminal
// folder-too-deep gap on the folder where the walk stopped, the messages above it are
// delivered, and the walk is quick.
func TestAnOverDeepStoreIsBoundedByTheDepthCap(t *testing.T) {
	store := filepath.Join(t.TempDir(), "Inbox")
	writeLocal(t, filepath.Join(store, "cur", "top:2,S"), localMail("top"))

	shallow := filepath.Join(store, "cur", filepath.FromSlash(deepChain(100)), "low:2,S")
	writeLocal(t, shallow, localMail("low"))
	got := walkLocal(t, store, OpenOptions{})
	if len(got.subjects) != 2 || len(got.faults) != 0 {
		t.Fatalf("a chain 100 deep: delivered %v, faults %+v; want both messages and no fault", got.subjects, got.faults)
	}

	deep := filepath.Join(store, "new", filepath.FromSlash(deepChain(maxFolderDepth+60)), "bottom:2,S")
	writeLocal(t, deep, localMail("bottom"))
	start := time.Now()
	got = walkLocal(t, store, OpenOptions{})
	if took := time.Since(start); took > 3*time.Second {
		t.Errorf("the walk took %v; an over-deep chain must be cut, not walked", took)
	}
	for _, s := range got.subjects {
		if strings.Contains(s, "bottom") {
			t.Errorf("delivered %v: a message %d levels down was read, past the cap of %d", got.subjects, maxFolderDepth+61, maxFolderDepth)
		}
	}
	if len(got.subjects) != 2 {
		t.Errorf("delivered %v; want the message above the cap and the one 100 deep", got.subjects)
	}
	f, ok := got.fault("folder-too-deep")
	if !ok || got.count("folder-too-deep") != 1 || f.Class != state.ClassTerminal || f.Scope != state.GapScopeFolder || !strings.HasPrefix(f.Locator, "folder:new/d/d") {
		t.Fatalf("faults %+v; want ONE terminal folder-too-deep folder gap under new/d/d/...", got.faults)
	}
	if rem := state.GapRemedy(f.Reason, state.SourceLocal, ""); strings.Contains(rem, "scanpst") || strings.Contains(rem, ".pst") {
		t.Errorf("the remedy of a local store's deep folder names a .pst repair: %q", rem)
	}
}

// covers: MA-322, R25, R10, S47
// S2B-AO4 (and S2B-AC1): a pipe named like an Outlook data file is never opened. The PST
// open was a plain os.Open, which waits for a writer on a pipe, so a FIFO named *.pst or
// *.ost in an input folder held the archive lock until someone opened it (FC2 says a pipe
// is never opened). The positive twin first: a regular file of that name is opened (and
// refused by the parser, not by the open).
func TestAPipeNamedLikeADataFileIsNeverOpened(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("FIFOs need privileges on Windows")
	}
	dir := t.TempDir()
	writeLocal(t, filepath.Join(dir, "plain.pst"), "not a pst")
	if _, err := Open(filepath.Join(dir, "plain.pst")); err == nil || strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("a regular file named .pst: error %v; want the parser's refusal, not the open's", err)
	}
	for _, name := range []string{"evil.pst", "evil.ost", "Upper.PST"} {
		pipe := filepath.Join(dir, name)
		if err := mkfifo(pipe); err != nil {
			t.Skip("no FIFOs here:", err)
		}
		releasePipe(t, pipe)
		done := make(chan error, 1)
		go func() {
			s, err := Open(pipe)
			if err == nil {
				s.Close()
			}
			done <- err
		}()
		select {
		case err := <-done:
			if err == nil || !strings.Contains(err.Error(), "not a regular file") {
				t.Errorf("%s: error %v; want the refusal of a pipe", name, err)
			}
		case <-time.After(2 * time.Second):
			t.Errorf("%s: Open waited on a pipe", name)
		}
	}
}
