package source

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/state"
)

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
