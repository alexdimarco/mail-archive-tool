package app

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/state"
)

// covers: MA-149, R2, R3, R6, R8, S30
// One physical store archived under different -input SPELLINGS resolves to one
// store token, so a re-run over a relative, symlinked, or trailing-slash spelling
// exports zero and writes no duplicate tree — the identity is the canonical path,
// not the caller's spelling (INT-CC-1). Without this, the tool's own remedies
// (status absolutises -input; the GUI writes absolute job files) would double the
// archive on the first nightly run.
func TestReExportIdentityAcrossSpellings(t *testing.T) {
	src := filepath.Join(tmpDir(t), "Inbox")
	maildirMessages(t, src, 3)
	out := tmpDir(t)
	logger := log.New(io.Discard, "", 0)
	ctx := context.Background()

	run := func(input string) export.Stats {
		res, err := Run(ctx, Options{Inputs: []string{input}, Out: out, Mode: export.Incremental, Index: true, Pages: true}, logger, nil)
		if err != nil {
			t.Fatalf("run over %q failed: %v", input, err)
		}
		return res.Stats
	}

	first := run(src) // absolute
	if first.Exported != 3 {
		t.Fatalf("first run exported %d, want 3", first.Exported)
	}
	htmlAfterFirst := countTree(t, out, ".html")

	// Trailing separator: the same physical store, must export zero.
	if s := run(src + string(os.PathSeparator)); s.Exported != 0 {
		t.Errorf("trailing-slash spelling re-exported %d, want 0", s.Exported)
	}

	// A symlink to the store (its display name even differs): still one token.
	if runtime.GOOS != "windows" {
		link := filepath.Join(tmpDir(t), "InboxLink")
		if err := os.Symlink(src, link); err != nil {
			t.Fatal(err)
		}
		if s := run(link); s.Exported != 0 {
			t.Errorf("symlink spelling re-exported %d, want 0", s.Exported)
		}
	}

	// Relative spelling (from the store's parent): must export zero.
	func() {
		old, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Chdir(filepath.Dir(src)); err != nil {
			t.Fatal(err)
		}
		defer os.Chdir(old)
		if s := run(filepath.Base(src)); s.Exported != 0 {
			t.Errorf("relative spelling re-exported %d, want 0", s.Exported)
		}
	}()

	// Exactly one store tree: no duplicate "Inbox~hash" dir, html count unchanged.
	if got := countTree(t, out, ".html"); got != htmlAfterFirst {
		t.Errorf("html count changed across spellings: %d then %d (a duplicate tree was written)", htmlAfterFirst, got)
	}
	if dups, _ := filepath.Glob(filepath.Join(out, "Inbox~*")); len(dups) > 0 {
		t.Errorf("a duplicate store tree was created: %v", dups)
	}
	if _, err := os.Stat(filepath.Join(out, "Inbox")); err != nil {
		t.Errorf("the single store tree out/Inbox is missing: %v", err)
	}

	// A manifest keyed by the RAW spelling (what the old binary wrote) is migrated
	// on load, so the very next run still finds every message and exports zero.
	mpath := filepath.Join(out, ".mailarchive-manifest.json")
	m, err := state.Load(mpath)
	if err != nil {
		t.Fatal(err)
	}
	m.Stores = map[string]string{src: "Inbox"} // simulate the pre-fix raw-spelling key
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	if s := run(src); s.Exported != 0 {
		t.Errorf("post-migration run re-exported %d, want 0 (the raw-spelling stores key was not migrated)", s.Exported)
	}
	if dups, _ := filepath.Glob(filepath.Join(out, "Inbox~*")); len(dups) > 0 {
		t.Errorf("migration minted a duplicate tree: %v", dups)
	}
}

// covers: MA-151, R5, R12, S25
// When the run loses its lock mid-walk (the lock file is removed or replaced), it
// stops promptly and writes NO shared state into the archive another run may now
// own: no manifest rewrite after the loss, and no README scaffold (finish is
// skipped). It returns the typed lock-loss error naming the lock. Positive twin
// first: a healthy run with the lock intact completes, commits, and scaffolds.
func TestLockLossMidWalkStopsWithoutRewriting(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	ctx := context.Background()

	// Positive twin: lock intact for the whole run → completes and scaffolds.
	srcOK := filepath.Join(tmpDir(t), "Inbox")
	maildirMessages(t, srcOK, 3)
	okOut := tmpDir(t)
	if _, err := Run(ctx, Options{Inputs: []string{srcOK}, Out: okOut, Mode: export.Incremental, Index: true, Pages: true, CheckpointEvery: 1}, logger, nil); err != nil {
		t.Fatalf("healthy run failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(okOut, "README.txt")); err != nil {
		t.Fatalf("healthy run did not write the README scaffold: %v", err)
	}
	if m, _ := state.Load(filepath.Join(okOut, ".mailarchive-manifest.json")); m.Len() != 3 {
		t.Fatalf("healthy run manifest holds %d, want 3", m.Len())
	}

	// Lock loss: remove the lock inode after the first message; the run must stop
	// early, report the loss, and leave the manifest at its pre-loss checkpoint
	// with no README scaffold.
	src := filepath.Join(tmpDir(t), "Inbox")
	maildirMessages(t, src, 8)
	out := tmpDir(t)
	mpath := filepath.Join(out, ".mailarchive-manifest.json")
	lockPath := filepath.Join(out, lockfile.Name)

	seen := 0
	progress := func(export.Stats) {
		seen++
		if seen == 1 {
			os.Remove(lockPath) // a second run could now acquire a fresh lock
		}
	}
	_, err := Run(ctx, Options{Inputs: []string{src}, Out: out, Mode: export.Incremental, Index: true, Pages: true, CheckpointEvery: 1}, logger, progress)
	if err == nil {
		t.Fatal("run did not report the lock loss")
	}
	if !strings.Contains(err.Error(), lockfile.Name) || !strings.Contains(err.Error(), "removed during the run") {
		t.Errorf("lock-loss error does not name the lock and cause: %v", err)
	}
	// No scaffold: finish() was not called after the loss.
	if _, statErr := os.Stat(filepath.Join(out, "README.txt")); !os.IsNotExist(statErr) {
		t.Errorf("README scaffold was written after lock loss (finish ran): %v", statErr)
	}
	// The manifest on disk holds only the pre-loss checkpoint, never all 8.
	m, lerr := state.Load(mpath)
	if lerr != nil {
		t.Fatal(lerr)
	}
	if m.Len() == 0 || m.Len() >= 8 {
		t.Errorf("manifest on disk holds %d entries; want a pre-loss checkpoint (1..7), proving it was not rewritten after the loss", m.Len())
	}
}
