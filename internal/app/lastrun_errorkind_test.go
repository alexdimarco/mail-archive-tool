package app

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"mail-archive-tool/internal/state"
)

// covers: MA-386, R12, R18, S33
// A run refused at the search index records which refusal it was (g7 friction
// re-verification of F3), so an owner surface's health view can say it in the
// owner's words instead of printing the recorded CLI command and SQLite text.
// Walked with real runs over a real archive: a damaged search.db, and a
// read-only one. Positive twin: a healthy run records no kind.
func TestFailedRunRecordsWhichIndexRefusalItWas(t *testing.T) {
	opts, _, _ := unwritableFixture(t, 3)
	idxPath := filepath.Join(opts.Out, "search.db")
	kindOf := func() (string, string) {
		lr, st, err := state.ReadLastRun(opts.Out)
		if err != nil || st != state.LastRunPresent {
			t.Fatalf("last-run record: state %v, err %v", st, err)
		}
		return lr.Status, lr.ErrorKind
	}
	if status, kind := kindOf(); status != state.RunOK || kind != "" {
		t.Fatalf("positive twin: a healthy run recorded %q with kind %q", status, kind)
	}

	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		if err := os.Chmod(idxPath, 0o444); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(idxPath, 0o644) })
		if _, err := Run(context.Background(), opts, discard(), nil); err == nil {
			t.Fatal("a read-only search.db did not refuse the run")
		}
		if status, kind := kindOf(); status != state.RunFailed || kind != state.ErrKindIndexUnwritable {
			t.Fatalf("read-only search.db: recorded %q with kind %q, want failed / %q", status, kind, state.ErrKindIndexUnwritable)
		}
		if err := os.Chmod(idxPath, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := os.WriteFile(idxPath, []byte("this is not a sqlite database, and it is long enough to be read as a header"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), opts, discard(), nil); err == nil {
		t.Fatal("a damaged search.db did not refuse the run")
	}
	if status, kind := kindOf(); status != state.RunFailed || kind != state.ErrKindIndexDamaged {
		t.Fatalf("damaged search.db: recorded %q with kind %q, want failed / %q", status, kind, state.ErrKindIndexDamaged)
	}
}
