package app

import (
	"bytes"
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/health"
	"mail-archive-tool/internal/state"
)

// covers: MA-392, R5, R18, S29
// A run whose manifest cannot be saved has kept no progress, so it FAILS
// (F53): Run and RunGraph return an error naming the manifest file and the
// remedy, the last-run record is finalized failed with that reason (never
// "ok"), and the human attention sidecar names it — while the manifest on disk
// is left exactly as the last successful run wrote it. The cause is the
// persistent one from the finding: the manifest's folder becomes unwritable
// after a first, successful run (the positive twin), so every later run's save
// fails.
func TestManifestSaveFailureFailsTheRun(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX directory permissions model an unwritable manifest location; Windows ignores a directory's read-only bit")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permission bits")
	}
	src := filepath.Join(tmpDir(t), "Inbox")
	maildirMessages(t, src, 2)
	logger := log.New(io.Discard, "", 0)
	_, srv := newFakeGraphServer()
	defer srv.Close()
	g := GraphOptions{Tenant: "t", ClientID: "c", ClientSecret: "s", Mailboxes: []string{"u1"}, BaseURL: srv.URL, TokenURL: srv.URL + "/token"}

	for _, r := range []struct {
		name string
		run  func(opts Options) (Result, error)
	}{
		{"local", func(opts Options) (Result, error) {
			opts.Inputs = []string{src}
			return Run(context.Background(), opts, logger, nil)
		}},
		{"graph", func(opts Options) (Result, error) {
			return RunGraph(context.Background(), g, opts, logger)
		}},
	} {
		t.Run(r.name, func(t *testing.T) {
			out, mdir := tmpDir(t), tmpDir(t)
			mpath := filepath.Join(mdir, "manifest.json")
			opts := Options{Out: out, Mode: export.Incremental, Index: true, Manifest: mpath}

			// Positive twin: with the folder writable the run saves and records ok.
			res, err := r.run(opts)
			if err != nil {
				t.Fatalf("%s: healthy run failed: %v", r.name, err)
			}
			assure.Reached(t, res.Stats.Exported, r.name+": messages exported")
			if lr, _, _ := state.ReadLastRun(out); lr.Status != state.RunOK {
				t.Fatalf("%s: healthy run recorded %q", r.name, lr.Status)
			}
			saved, err := os.ReadFile(mpath)
			if err != nil {
				t.Fatalf("%s: healthy run wrote no manifest: %v", r.name, err)
			}

			if err := os.Chmod(mdir, 0o555); err != nil {
				t.Fatal(err)
			}
			_, err = r.run(opts)
			_ = os.Chmod(mdir, 0o755)
			code, msg := 0, ""
			if err != nil {
				code, msg = 1, err.Error()
			}
			assure.Refused(t, code, msg, assure.Code(1),
				assure.Names(mpath, "could not save the manifest", "writable"),
				assure.NoSideEffect(func() bool {
					now, rerr := os.ReadFile(mpath)
					return rerr == nil && bytes.Equal(now, saved) // nothing new was persisted
				}))
			lr, st, rerr := state.ReadLastRun(out)
			if rerr != nil || st != state.LastRunPresent {
				t.Fatalf("%s: no last-run record: %v %v", r.name, st, rerr)
			}
			if lr.Status != state.RunFailed || !strings.Contains(lr.Error, mpath) {
				t.Errorf("%s: last-run %q (%q), want failed naming %s", r.name, lr.Status, lr.Error, mpath)
			}
			sidecar, serr := os.ReadFile(filepath.Join(out, state.AttentionName))
			if serr != nil || !strings.Contains(string(sidecar), mpath) {
				t.Errorf("%s: attention sidecar missing or does not name the manifest: %v\n%s", r.name, serr, sidecar)
			}
		})
	}
}

// covers: MA-392, R5, R18, S29
// Only the run's LAST manifest save decides (F53): a failed checkpoint save is
// healed by a later save that succeeds. With a checkpoint after every message,
// the save at message 2 is made to fail and message 3's checkpoint heals it,
// and the save at the last message (4) is made to fail and the end-of-input
// commit heals it. The run returns nil and records ok — no attention sidecar,
// no run_failed on status — and the manifest on disk holds every record.
// Fault injection: for exactly one save each time, a directory stands where
// the manifest is renamed to (the run, its saves and the rename are real), so
// the test runs on every OS. Positive twin: the same run with no fault logs no
// failed save.
func TestFailedCheckpointIsHealedByALaterSave(t *testing.T) {
	const n = 4
	src := filepath.Join(tmpDir(t), "Inbox")
	maildirMessages(t, src, n)
	run := func(out string, progress ProgressFunc) (Result, string, error) {
		var logged bytes.Buffer
		res, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Mode: export.Incremental, Index: true, CheckpointEvery: 1}, log.New(&logged, "", 0), progress)
		return res, logged.String(), err
	}
	const failedSave = "could not save manifest"

	if _, logged, err := run(tmpDir(t), nil); err != nil || strings.Contains(logged, failedSave) {
		t.Fatalf("positive twin: err=%v, log:\n%s", err, logged)
	}

	out := tmpDir(t)
	mpath := filepath.Join(out, ".mailarchive-manifest.json")
	block := func() { // the next save's rename meets a directory
		if err := os.Rename(mpath, mpath+".aside"); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(mpath, "blocker"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	unblock := func() {
		if err := os.RemoveAll(mpath); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(mpath+".aside", mpath); err != nil {
			t.Fatal(err)
		}
	}
	// Run checkpoints a message before reporting it, so the fault set up at
	// message k fails the save at message k+1.
	seen := 0
	res, logged, err := run(out, func(export.Stats) {
		seen++
		switch seen {
		case 1, 3:
			block()
		case 2, 4:
			unblock()
		}
	})
	if failed := assure.Reached(t, strings.Count(logged, failedSave), "checkpoint saves made to fail"); failed != 2 {
		t.Fatalf("%d checkpoint saves failed, want the 2 injected; log:\n%s", failed, logged)
	}
	if err != nil {
		t.Fatalf("a run whose failed checkpoints were healed by later saves failed: %v", err)
	}
	if res.Stats.Exported != n {
		t.Fatalf("exported %d, want %d", res.Stats.Exported, n)
	}
	if lr, _, _ := state.ReadLastRun(out); lr.Status != state.RunOK {
		t.Fatalf("a healed run recorded %q (%s), want ok", lr.Status, lr.Error)
	}
	if _, serr := os.Stat(filepath.Join(out, state.AttentionName)); !os.IsNotExist(serr) {
		t.Errorf("a healed run left the attention sidecar: %v", serr)
	}
	if rep := health.Assess(health.Gather(out, ""), time.Now()); reasonFor(rep, "run_failed") != "" {
		t.Errorf("status reports a healed run failed: %v", rep.Reasons)
	}
	m, lerr := state.Load(mpath)
	if lerr != nil {
		t.Fatal(lerr)
	}
	if m.Len() != n {
		t.Fatalf("the manifest on disk holds %d records, want all %d", m.Len(), n)
	}
}
