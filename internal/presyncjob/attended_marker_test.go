package presyncjob

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/presync"
)

// covers: MA-906
// The first-attended-sync marker (design-presync-attended §4.6, C-83): the Desktop's
// one-time "New: Run now starts Thunderbird…" line shows until the first attended
// Run now on this computer has started, which the marker records — written in the
// pre-sync state folder, owner-only, when an attended session reaches S5 (it
// reports `starting`). Positive twin first: an attended session reporting
// `starting` writes it, and the caller's own Phase still hears the phase. A
// scheduled session reporting `starting`, and an attended one that never reaches
// S5, write nothing.
func TestFirstAttendedSyncMarker(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "presync")
	reach := true
	session := func(_ context.Context, cfg presync.Config) presync.Result {
		if reach && cfg.Phase != nil {
			cfg.Phase(presync.PhaseStarting, time.Now())
		}
		return presync.Result{Outcome: presync.OutcomeOK}
	}
	restore := OverrideSessionForTest(session, dir)
	defer restore()
	r := presync.OverrideTablesForTest(nil, map[string]bool{runtime.GOOS: true})
	defer r()

	run := func(attended bool) []string {
		var heard []string
		var opts app.Options
		if err := SetHook(&opts, HookOptions{Attended: attended, Phase: func(p string, _ time.Time) { heard = append(heard, p) }}); err != nil || opts.BeforeRead == nil {
			t.Fatalf("SetHook: hook %v, err %v", opts.BeforeRead != nil, err)
		}
		opts.BeforeRead(context.Background(), nil)
		return heard
	}

	if AttendedSyncSeen() {
		t.Fatal("a fresh state folder already says an attended sync started")
	}
	if heard := run(false); len(heard) != 1 || AttendedSyncSeen() {
		t.Fatalf("a scheduled session reaching S5: heard %v, marker written %v; want the phase heard and no marker", heard, AttendedSyncSeen())
	}
	reach = false
	if run(true); AttendedSyncSeen() {
		t.Fatal("an attended session that never reached S5 wrote the marker")
	}
	reach = true
	heard := run(true)
	if len(heard) != 1 || heard[0] != presync.PhaseStarting {
		t.Errorf("the caller's Phase heard %v, want [starting]", heard)
	}
	if !AttendedSyncSeen() {
		t.Fatal("an attended session reaching S5 did not write the marker")
	}
	fi, err := os.Stat(filepath.Join(dir, attendedMarker))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm()&0o077 != 0 {
		t.Errorf("the marker is %v, want owner-only", fi.Mode().Perm())
	}

	// A state folder that cannot be found never claims the line was seen... and never
	// shows it either: with no folder there is no sync to announce.
	stateDirMu.Lock()
	old := stateDirFn
	stateDirFn = func() (string, error) { return "", os.ErrNotExist }
	stateDirMu.Unlock()
	defer func() { stateDirMu.Lock(); stateDirFn = old; stateDirMu.Unlock() }()
	if !AttendedSyncSeen() {
		t.Error("no state folder: the first-sync line would show for a sync that cannot run")
	}
}

// covers: MA-906
// A session that panics still ends the caller's phase (design-presync-attended §4.6,
// C-11): OnEnd is told internal-error (class warn), so the Desktop's note never
// stays on "waiting" for a session that is gone; the panic still reaches app.Run,
// which recovers it (R32). Positive twin first: a session that returns tells OnEnd
// its own outcome.
func TestHookPanicStillEnds(t *testing.T) {
	panics := false
	session := func(context.Context, presync.Config) presync.Result {
		if panics {
			panic("boom")
		}
		return presync.Result{Outcome: presync.OutcomeOK}
	}
	restore := OverrideSessionForTest(session, t.TempDir())
	defer restore()
	var ended []app.PresyncOutcome
	var opts app.Options
	if err := SetHook(&opts, HookOptions{OnEnd: func(o app.PresyncOutcome) { ended = append(ended, o) }}); err != nil {
		t.Fatal(err)
	}
	opts.BeforeRead(context.Background(), nil)
	if len(ended) != 1 || ended[0].Name != "ok" {
		t.Fatalf("twin: OnEnd got %+v, want ok", ended)
	}
	panics = true
	func() {
		defer func() {
			if recover() == nil {
				t.Error("the panic did not reach the run")
			}
		}()
		opts.BeforeRead(context.Background(), nil)
	}()
	if len(ended) != 2 || ended[1].Name != "internal-error" || ended[1].Class != "warn" || ended[1].Synced {
		t.Errorf("after a panic OnEnd got %+v, want internal-error, warn, not synced", ended)
	}
}
