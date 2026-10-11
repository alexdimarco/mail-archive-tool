package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
)

// seqRand is a deterministic random source: the root's 4-hex suffix is fixed (so a
// test can plant the next name), every other value counts up.
func seqRand(suffix string) func(n int) string {
	var mu sync.Mutex
	i := 0
	return func(n int) string {
		if n == 4 {
			return suffix
		}
		mu.Lock()
		defer mu.Unlock()
		i++
		s := fmt.Sprintf("%0*x", n, i)
		return s[len(s)-n:]
	}
}

// fixedNow pins the clock, so two runs share a second.
func fixedNow(e *CopyExportEnv) time.Time {
	at := time.Date(2026, 10, 9, 14, 22, 33, 0, time.Local)
	e.now = func() time.Time { return at }
	return at
}

// covers: MA-416, R29, R5, S52
// Each run creates a new export directory with an exclusive mkdir, never MkdirAll: the
// name is MailExport-<local timestamp>-<4 hex>, and on EEXIST it moves to -2 … -99,
// then the run is refused with R-t. Two runs in the same second get distinct
// directories; a planted pre-existing directory of the would-be name is never written
// into, never recorded in the journal as this run and never reconciled as it (the
// journal start names the directory actually created); an earlier export's bytes are
// unchanged after a later run.
func TestCopyExportRootIsNew(t *testing.T) {
	e, src := newCEEnv(t), newFakeSrc()
	e.randHex = seqRand("7f3a")
	fixedNow(e)
	dest := filepath.Join(t.TempDir(), "dest")
	os.MkdirAll(dest, 0o755)
	const name = "MailExport-2026-10-09_142233-7f3a"

	// A planted directory of the would-be name, with a file in it.
	os.Mkdir(filepath.Join(dest, name), 0o755)
	os.WriteFile(filepath.Join(dest, name, "canary.txt"), []byte("mine"), 0o600)
	p := cePlanAt(t, e, src, ceMsgs(3), dest)
	if p.ExportName != name {
		t.Fatalf("the would-be name is %q, want %q", p.ExportName, name)
	}
	first := mustRun(t, e, p, src)
	if first.RootName != name+"-2" {
		t.Fatalf("the run took %q, want %q beside the planted directory", first.RootName, name+"-2")
	}
	if got := treeFiles(t, filepath.Join(dest, name)); len(got) != 1 || got["canary.txt"] == "" {
		t.Errorf("the planted directory was written into: %v", got)
	}
	// The journal names the directory actually created, and reconciles that one.
	lines, _ := e.readJournal()
	var start *ceJournalLine
	for i := range lines {
		if lines[i].Event == "start" {
			start = &lines[i]
		}
	}
	if start == nil || start.DestDir != filepath.Join(dest, name+"-2") {
		t.Errorf("the journal start = %+v, want the directory actually created", start)
	}
	if sum := mustSummary(t, e, first.RunID); sum.Header.Root != filepath.Join(dest, name+"-2") || sum.Header.RootName != name+"-2" {
		t.Errorf("the run record names %q, not the directory created", sum.Header.Root)
	}
	before := treeFiles(t, first.Root)

	// A second run in the same second gets its own directory, and the first is untouched.
	p2 := cePlanAt(t, e, src, ceMsgs(3), dest)
	second := mustRun(t, e, p2, src)
	if second.RootName != name+"-3" || second.Root == first.Root {
		t.Errorf("the second run took %q, want %q", second.RootName, name+"-3")
	}
	after := treeFiles(t, first.Root)
	if len(after) != len(before) {
		t.Errorf("the first export holds %d files after a second run, was %d", len(after), len(before))
	}
	for p, h := range before {
		if after[p] != h {
			t.Errorf("%s of the first export changed after a later run", p)
		}
	}

	// Every candidate taken: R-t, with nothing written.
	for i := 4; i <= 99; i++ {
		os.Mkdir(filepath.Join(dest, fmt.Sprintf("%s-%d", name, i)), 0o755)
	}
	p3 := cePlanAt(t, e, src, ceMsgs(1), dest)
	entries, _ := os.ReadDir(dest)
	_, err := ceRunPlan(t, e, p3, src)
	var ref *CopyRefusal
	if !errors.As(err, &ref) {
		t.Fatalf("a destination with every name taken did not refuse: %v", err)
	}
	assure.Refused(t, ref.ExitCode(), ref.Message, assure.Code(1), assure.Names("-99", "choose another destination"),
		assure.NoSideEffect(func() bool { now, _ := os.ReadDir(dest); return len(now) == len(entries) }))
	if ref.ID != "R-t" {
		t.Errorf("the refusal is %s, want R-t", ref.ID)
	}
}
