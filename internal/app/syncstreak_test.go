package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"mail-archive-tool/internal/state"
)

// How a run was started, for the sync streak (design-presync-attended §5.3, C-33).
type runKind int

const (
	kindScheduled runKind = iota // the scheduled run-job: Unattended, not attended
	kindRunNow                   // the plaintext Run now: Unattended (for the source streak) but attended
	kindManual                   // a manual command line run: neither
)

func (k runKind) opts(out, input string, wanted bool) Options {
	o := Options{Inputs: []string{input}, Out: out, PresyncWanted: wanted}
	switch k {
	case kindScheduled:
		o.Unattended = true
	case kindRunNow:
		o.Unattended, o.PresyncAttended = true, true
	}
	return o
}

// streakOnDisk is the archive's sync streak as the next run reads it.
func streakOnDisk(t *testing.T, out string) (state.SyncStreak, bool) {
	t.Helper()
	if _, err := os.Lstat(filepath.Join(out, state.SyncStreakName)); errors.Is(err, os.ErrNotExist) {
		return state.SyncStreak{}, false
	}
	s, err := state.ReadSyncStreak(out)
	if err != nil {
		t.Fatalf("the sync streak cannot be read: %v", err)
	}
	return s, true
}

// syncRun runs a real app.Run of kind with a scripted sync ending as o (nil: no hook).
func syncRun(t *testing.T, out, input string, kind runKind, wanted bool, o *PresyncOutcome) {
	t.Helper()
	opts := kind.opts(out, input, wanted)
	if o != nil {
		opts = scriptedSync(opts, *o, "", nil)
	}
	if _, err := Run(context.Background(), opts, discard(), nil); err != nil {
		t.Fatalf("the run failed: %v", err)
	}
}

func outcome(name string, synced bool, verdict string) *PresyncOutcome {
	return &PresyncOutcome{Name: name, Synced: synced, Verdict: verdict, Left: "none"}
}

// covers: MA-908, R32, S65
// The sync streak through a real app.Run (design-presync-attended §5.2, §5.3; C-4,
// C-33, C-34, C-72, C-86d): positive twin first, a scheduled run of a sync job whose
// session missed starts the streak. Only a scheduled run (Unattended and not attended)
// of a job that asks for the sync extends it; a plaintext Run now (Unattended true, but
// attended) that missed and a manual run do not; leftover-found counts missed; a
// cancelled session, whatever it left open, and a stopped wait move nothing; two
// profile-busy move only the busy count, any other scheduled outcome resets it, and the
// third in a row counts one miss; a synced session from any run resets the streak and
// keeps Serial, and the next miss starts a new one; a hook that could not be prepared
// (not-set-up) extends it and never deletes it; a scheduled run of a sync job with no
// session moves nothing; a manual run without the sync keeps the file, and a scheduled
// run whose job has no sync removes it. The file is owner-only.
func TestTheSyncStreakFollowsTheRules(t *testing.T) {
	out, in := tmpDir(t), completeMaildir(t)
	missed := func(label string, n, serial int, last string) state.SyncStreak {
		t.Helper()
		s, ok := streakOnDisk(t, out)
		if !ok || s.Missed != n || s.Serial != serial || (last != "" && s.LastName != last) {
			t.Fatalf("%s: streak %+v (file %v), want missed %d, serial %d, last %q", label, s, ok, n, serial, last)
		}
		return s
	}

	syncRun(t, out, in, kindScheduled, true, outcome("owner-active-skipped", false, ""))
	first := missed("positive twin: a scheduled miss", 1, 1, "owner-active-skipped")
	if first.FirstMissed.IsZero() || first.LastMissed.IsZero() {
		t.Errorf("the first miss kept no time: %+v", first)
	}
	if fi, err := os.Stat(filepath.Join(out, state.SyncStreakName)); err != nil || (runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600) {
		t.Errorf("the streak file: %v (%v), want owner-only", fi.Mode().Perm(), err)
	}

	syncRun(t, out, in, kindRunNow, true, outcome("owner-active-skipped", false, ""))
	missed("a plaintext Run now that missed (C-33)", 1, 1, "")
	syncRun(t, out, in, kindManual, true, outcome("cannot-start", false, ""))
	missed("a manual run that missed", 1, 1, "")

	syncRun(t, out, in, kindScheduled, true, outcome("leftover-found", false, ""))
	missed("a scheduled leftover-found", 2, 1, "leftover-found")

	// A cancelled session, whatever it left open, moves nothing (C-72); nor does a
	// stopped wait (OQ-6).
	for _, left := range []string{"left-open-by-rule", "left-open-by-choice", "left-open", "may-be-starting"} {
		ctx, cancel := context.WithCancel(context.Background())
		o := PresyncOutcome{Name: "cancelled", Left: left}
		_, err := Run(ctx, scriptedSync(kindScheduled.opts(out, in, true), o, "", cancel), discard(), nil)
		cancel()
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("the cancelled run: %v", err)
		}
		missed("a cancelled session, left "+left, 2, 1, "leftover-found")
	}
	syncRun(t, out, in, kindRunNow, true, &PresyncOutcome{Name: "wait-ended", Left: "left-open-by-rule"})
	missed("a stopped wait", 2, 1, "")

	// profile-busy (C-4, C-86d).
	syncRun(t, out, in, kindScheduled, true, outcome("profile-busy", false, ""))
	syncRun(t, out, in, kindScheduled, true, outcome("profile-busy", false, ""))
	if s := missed("two profile-busy", 2, 1, "leftover-found"); s.Busy != 2 {
		t.Fatalf("two profile-busy: busy %d, want 2", s.Busy)
	}
	syncRun(t, out, in, kindScheduled, true, outcome("app-not-found", false, ""))
	if s := missed("another scheduled outcome after two busy", 3, 1, "app-not-found"); s.Busy != 0 {
		t.Fatalf("busy %d after another scheduled outcome, want 0", s.Busy)
	}
	for i := 0; i < 3; i++ {
		syncRun(t, out, in, kindScheduled, true, outcome("profile-busy", false, ""))
	}
	if s := missed("three profile-busy in a row", 4, 1, "profile-busy"); s.Busy != 0 {
		t.Fatalf("busy %d after the third, want 0", s.Busy)
	}

	// A synced session from any run resets it — a Run now here — and keeps Serial.
	syncRun(t, out, in, kindRunNow, true, outcome("ok", true, "ok"))
	if s := missed("a synced Run now", 0, 1, ""); s.LastSynced.IsZero() || !s.FirstMissed.IsZero() {
		t.Fatalf("after a sync: %+v", s)
	}
	syncRun(t, out, in, kindScheduled, true, outcome("owner-active-skipped", false, ""))
	missed("a miss after the reset starts a new streak", 1, 2, "owner-active-skipped")

	// not-set-up (C-34): extends it, never deletes it.
	syncRun(t, out, in, kindScheduled, true, &PresyncOutcome{Code: "presync_not_set_up", Name: "not-set-up", Class: "warn", Left: "none"})
	missed("a hook that could not be prepared", 2, 2, "not-set-up")

	// A sync job with no session (no hook here) moves nothing; nor does a run already
	// cancelled before its hook.
	syncRun(t, out, in, kindScheduled, true, nil)
	missed("a scheduled sync job with no session", 2, 2, "not-set-up")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Run(ctx, scriptedSync(kindScheduled.opts(out, in, true), *outcome("owner-active-skipped", false, ""), "", nil), discard(), nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("the run cancelled before its hook: %v", err)
	}
	missed("a run cancelled before its hook", 2, 2, "not-set-up")

	// Removal (C-34): a manual run without the sync keeps the file; a scheduled run whose
	// job has no sync removes it.
	syncRun(t, out, in, kindManual, false, nil)
	missed("a manual run without -thunderbird-sync", 2, 2, "")
	syncRun(t, out, in, kindRunNow, false, nil)
	missed("a Run now of a job without the sync", 2, 2, "")
	syncRun(t, out, in, kindScheduled, false, nil)
	if _, ok := streakOnDisk(t, out); ok {
		t.Fatal("a scheduled run whose job has no sync left the sync streak")
	}
	// And one with no file to remove is fine.
	syncRun(t, out, in, kindScheduled, false, nil)
}

// covers: MA-926, R32, S65
// The no-change count reads the session's Verdict (owner ruling OQ-5, §5.2; C-68):
// positive twin first, a scheduled no-change session starts it; it extends also when the
// session ends owner-active or quit-* after a no-change verdict (and those still reset
// the missed streak, as synced); an attended no-change session never moves it; a
// session with no verdict (already-running, a skip, a cancel, a stopped wait) leaves it;
// a verdict that mail arrived resets it, from a Run now as from a scheduled run. The
// verdict is never written to the record or the history (only the outcome's name is).
func TestTheNoChangeCountReadsTheVerdict(t *testing.T) {
	out, in := tmpDir(t), completeMaildir(t)
	noChange := func(label string, n int) {
		t.Helper()
		s, _ := streakOnDisk(t, out)
		if s.NoChange != n {
			t.Fatalf("%s: no-change %d, want %d (%+v)", label, s.NoChange, n, s)
		}
	}
	syncRun(t, out, in, kindScheduled, true, outcome("ok-noactivity", true, "ok-noactivity"))
	noChange("positive twin: a scheduled no-change session", 1)
	if s, _ := streakOnDisk(t, out); s.FirstNoChange.IsZero() || s.Missed != 0 {
		t.Errorf("the first no-change session: %+v", s)
	}

	// A scheduled miss first, so the synced endings below are seen to reset it.
	syncRun(t, out, in, kindScheduled, true, outcome("owner-active-skipped", false, ""))
	syncRun(t, out, in, kindScheduled, true, outcome("owner-active", true, "watch-truncated-noactivity"))
	noChange("a scheduled owner-active after a no-change verdict", 2)
	if s, _ := streakOnDisk(t, out); s.Missed != 0 {
		t.Errorf("owner-active is synced, yet the missed streak is %d", s.Missed)
	}
	syncRun(t, out, in, kindScheduled, true, outcome("quit-timeout", true, "ok-noactivity"))
	noChange("a scheduled quit-timeout after a no-change verdict", 3)

	syncRun(t, out, in, kindRunNow, true, outcome("ok-noactivity", true, "ok-noactivity"))
	noChange("an attended no-change session", 3)

	for _, o := range []*PresyncOutcome{
		outcome("already-running", true, ""), outcome("owner-active-skipped", false, ""),
		outcome("profile-busy", false, ""), {Name: "wait-ended", Left: "left-open-by-rule"},
	} {
		syncRun(t, out, in, kindScheduled, true, o)
		noChange("a session with no verdict ("+o.Name+")", 3)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if _, err := Run(ctx, scriptedSync(kindScheduled.opts(out, in, true), PresyncOutcome{Name: "cancelled", Left: "left-open"}, "", cancel), discard(), nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("the cancelled run: %v", err)
	}
	cancel()
	noChange("a cancelled session", 3)

	// Mail arrived: reset, from a Run now as from a scheduled run.
	syncRun(t, out, in, kindRunNow, true, outcome("ok", true, "ok"))
	noChange("a Run now in which mail arrived", 0)
	if s, _ := streakOnDisk(t, out); !s.FirstNoChange.IsZero() || !s.LastNoChange.IsZero() {
		t.Errorf("the reset kept the no-change times: %+v", s)
	}
	syncRun(t, out, in, kindScheduled, true, outcome("ok-noactivity", true, "ok-noactivity"))
	noChange("a scheduled no-change session again", 1)
	syncRun(t, out, in, kindScheduled, true, outcome("not-quiet", true, "not-quiet"))
	noChange("a scheduled session still fetching at 20 minutes", 0)
	syncRun(t, out, in, kindScheduled, true, outcome("ok-noactivity", true, "ok-noactivity"))
	syncRun(t, out, in, kindScheduled, true, outcome("quit-no-window", true, "watch-truncated"))
	noChange("a scheduled truncated walk in which mail arrived", 0)

	// The verdict is never written beside the archive but in the streak's count.
	if p, ok := rawRecord(t, out)["presync"].(map[string]any); !ok {
		t.Fatal("the record has no presync")
	} else if _, has := p["verdict"]; has {
		t.Errorf("the record carries the verdict: %v", p)
	}
	b, err := os.ReadFile(filepath.Join(out, state.SyncStreakName))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	if _, has := raw["verdict"]; has {
		t.Errorf("the streak file carries a verdict: %s", b)
	}
}

// covers: MA-904, R37
// The sync streak is plaintext only, by code (§4.4, §5.1, C-16; slice AS5's part of
// MA-904): positive twin first, a plaintext Run now that synced writes the streak; a
// keyed run that synced writes none, and a keyed run removes a stray one planted beside
// the archive — and so does a run whose record is marked encrypted.
func TestTheSyncStreakIsPlaintextOnly(t *testing.T) {
	synced := outcome("ok", true, "ok")
	plain := tmpDir(t)
	syncRun(t, plain, completeMaildir(t), kindRunNow, true, synced)
	if _, ok := streakOnDisk(t, plain); !ok {
		t.Fatal("positive twin: a plaintext sync wrote no streak")
	}

	out, key := encryptedOut(t)
	if _, err := Run(context.Background(), scriptedSync(Options{Inputs: []string{completeMaildir(t)}, Out: out, Key: key}, *synced, "", nil), discard(), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(out, state.SyncStreakName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a keyed run that synced wrote a sync streak (%v)", err)
	}

	// A stray one, planted between runs, is removed by the next keyed run, and by the
	// record writer for a run marked encrypted.
	stray := state.SyncStreak{}.Miss("owner-active-skipped", time.Date(2026, 10, 20, 2, 0, 0, 0, time.UTC))
	if err := state.WriteSyncStreak(out, stray); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), scriptedSync(Options{Inputs: []string{completeMaildir(t)}, Out: out, Key: key}, *outcome("owner-active-skipped", false, ""), "", nil), discard(), nil); err != nil {
		t.Fatalf("the keyed run with a stray streak: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(out, state.SyncStreakName)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a keyed run left the stray sync streak (%v)", err)
	}
	if err := state.WriteSyncStreak(out, stray); err != nil {
		t.Fatal(err)
	}
	updateSyncStreak(kindScheduled.opts(out, "", true), synced, true, time.Now(), nil)
	if _, err := os.Lstat(filepath.Join(out, state.SyncStreakName)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a run marked encrypted left the stray sync streak (%v)", err)
	}
}

// covers: MA-908, MA-926, R32
// The fold's own times (§5.3): a scheduled miss and a scheduled no-change session carry
// the run's finish time, so two of them 3 days apart are stale, and two an hour apart,
// however long ago, are not — the fold never compares with now.
func TestTheSyncFoldUsesTheRunsTime(t *testing.T) {
	t0 := time.Date(2025, 3, 1, 2, 0, 0, 0, time.UTC) // long ago
	s, moved := foldSyncStreak(state.SyncStreak{}, *outcome("owner-active-skipped", false, ""), true, t0)
	s, _ = foldSyncStreak(s, *outcome("owner-active-skipped", false, ""), true, t0.Add(time.Hour))
	if !moved || s.Stale() || s.Missed != 2 {
		t.Fatalf("two misses an hour apart, long ago: %+v stale %v", s, s.Stale())
	}
	s, _ = foldSyncStreak(s, *outcome("owner-active-skipped", false, ""), true, t0.Add(3*24*time.Hour))
	if !s.Stale() || !s.LastMissed.Equal(t0.Add(3*24*time.Hour)) {
		t.Errorf("misses 3 days apart: %+v stale %v", s, s.Stale())
	}
	n, _ := foldSyncStreak(state.SyncStreak{}, *outcome("ok-noactivity", true, "ok-noactivity"), true, t0)
	n, _ = foldSyncStreak(n, *outcome("ok-noactivity", true, "ok-noactivity"), true, t0.Add(time.Hour))
	if n.NothingArrived() {
		t.Error("two no-change sessions an hour apart, long ago, read as 3 days")
	}
	n, _ = foldSyncStreak(n, *outcome("ok-noactivity", true, "ok-noactivity"), true, t0.Add(3*24*time.Hour))
	if !n.NothingArrived() {
		t.Errorf("no-change sessions 3 days apart: %+v", n)
	}
	if _, moved := foldSyncStreak(state.SyncStreak{}, *outcome("owner-active-skipped", false, ""), false, t0); moved {
		t.Error("a miss that is not a scheduled sync job's moved the streak")
	}
}
