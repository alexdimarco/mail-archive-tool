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

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
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
