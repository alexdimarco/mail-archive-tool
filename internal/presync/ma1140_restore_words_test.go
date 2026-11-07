package presync

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/tbrestore"
)

// covers: MA-1140, R40, R31, S71
// A pre-sync that finds a MailArchive restore writing into its profile starts nothing
// and says so as the restore (the restore-thunderbird R8 lab's wording note L3, settled
// 2026-11-07): its outcome is restore-running — even when a Thunderbird process is also
// seen — never "Thunderbird was already open"; its tag, status line and summary name the
// restore; it counts neither synced nor missed. Positive twin first: with no restore and
// a Thunderbird open, already-running as before.
func TestAPresyncStoppedByARestoreSaysTheRestore(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("HOME", cfg)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(cfg, ".config"))
	t.Setenv("APPDATA", filepath.Join(cfg, "AppData"))
	profile := t.TempDir()
	mk := func(f *fakeEnv) deps {
		return deps{
			env: f, app: "/a/thunderbird", profile: profile, watch: []string{"/w"},
			statePath: filepath.Join(t.TempDir(), "s.json"), lockPath: "/l",
			base:      record{Version: recordVersion},
			cfg:       quietConfig{settle: time.Second, quiet: time.Second, maxWait: time.Minute},
			awaitPoll: time.Second, awaitUp: time.Minute, pollEvery: time.Second, quitGrace: time.Minute, idleAway: 120 * time.Second,
		}
	}
	fake := func(tbOpen bool) *fakeEnv {
		f := newFakeEnv()
		f.realRestore = true
		f.lockOK = true
		f.idle, f.idleOK = 300, true
		if tbOpen {
			f.procs = []proc{{pid: 9, exe: "/x/thunderbird"}}
		}
		return f
	}

	f := fake(true)
	if res := runSession(context.Background(), mk(f)); res.outcome != OutcomeAlreadyRunning || f.started {
		t.Fatalf("positive twin, no restore: want already-running; got %q started=%v", res.outcome.Name, f.started)
	}

	rl, err := tbrestore.AcquireRestoreLock(profile)
	if err != nil {
		t.Fatal(err)
	}
	defer rl.Release()
	for _, tbOpen := range []bool{false, true} {
		f := fake(tbOpen)
		res := runSession(context.Background(), mk(f))
		if res.outcome != OutcomeRestoreRunning || f.started || f.quitted {
			t.Errorf("a restore holds the profile (Thunderbird process %v): want restore-running, nothing started; got %q started=%v", tbOpen, res.outcome.Name, f.started)
		}
	}

	o := OutcomeRestoreRunning
	for what, s := range map[string]string{
		"tag":     TagPhrase(o.Name),
		"status":  OutcomeLine(o, SurfaceCLI, "windows", LeftNone, 0, false),
		"desktop": OutcomeLine(o, SurfaceDesktop, "darwin", LeftNone, 0, false),
		"summary": SummaryLine(o),
	} {
		if !strings.Contains(s, "restor") || strings.Contains(s, "already open") || !strings.Contains(s, "Thunderbird") {
			t.Errorf("%s: %q names the restore, not an open Thunderbird", what, s)
		}
	}
	if o.Synced() || o.Class != ClassNote {
		t.Errorf("restore-running is a note, never synced: %+v", o)
	}
}
