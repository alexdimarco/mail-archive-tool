package app

import (
	"bytes"
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/vault"
)

// cancelProbe counts what a run did around its pre-sync hook: the hook's calls, its
// Release's calls, the read-start calls, and the steps the run paused at.
type cancelProbe struct {
	hook, release, readStart int
	points                   []string
}

// probedOptions is opts with a hook that returns outcome (and calls during, when set,
// from inside the hook) and an OnReadStart, all counted in p.
func probedOptions(opts Options, p *cancelProbe, outcome *PresyncOutcome, during func()) Options {
	opts.BeforeRead = func(ctx context.Context, files []string) BeforeReadResult {
		p.hook++
		if during != nil {
			during()
		}
		return BeforeReadResult{Release: func() { p.release++ }, Outcome: outcome}
	}
	opts.OnReadStart = func() { p.readStart++ }
	return opts
}

// pauseRunAt sets the run's pause seam for the test: every step is recorded in p, and
// at the step named at, cancel is called (the cancel arrives during that step).
func pauseRunAt(t *testing.T, p *cancelProbe, at string, cancel func()) {
	t.Helper()
	orig := runPause
	runPause = func(point string) {
		p.points = append(p.points, point)
		if point == at && cancel != nil {
			cancel()
		}
	}
	t.Cleanup(func() { runPause = orig })
}

// completeMaildir is a maildir folder holding one complete message: a run that reads
// it exports one message and ends ok.
func completeMaildir(t *testing.T) string {
	t.Helper()
	src := filepath.Join(tmpDir(t), "Inbox")
	maildirMessage(t, src, "aGVsbG8=")
	return src
}

// readLog is a run logger whose lines the test reads back: runFile logs "Reading
// <source>" as it opens a source, so its absence proves no source was opened.
type readLog struct{ bytes.Buffer }

func (l *readLog) logger() *log.Logger { return log.New(&l.Buffer, "", 0) }
func (l *readLog) read() bool          { return strings.Contains(l.String(), "Reading ") }

// recordOnDisk is the last-run record as the file on disk says it, not the Result.
func recordOnDisk(t *testing.T, out string) state.LastRun {
	t.Helper()
	lr, st, err := state.ReadLastRun(out)
	if st != state.LastRunPresent {
		t.Fatalf("no readable last-run record in %s (%v, %v)", out, st, err)
	}
	return lr
}

// covers: MA-902, R32, R18
// A cancel that comes before mail reading starts stops the run before it reads
// (design-presync-attended §4.1, slice AS2, C-10, C-11, C-12, C-76): a cancel before
// the hook never calls it; a cancel during the hook stops before any source read and
// before the index is opened, with the hook's Release still run; a cancel during the
// index open and during an encrypted archive's sealed catch-up stops at the second
// check, after OnReadStart, keeping the catch-up's work as the source loop's own cancel
// path keeps it. In every case the last-run FILE on disk reads cancelled, never
// running. The hook's outcome reaches Result.Presync on every return, in memory only
// (AS2 writes nothing of it, C-53). Positive twin first: nothing cancelled reads the
// source, calls the hook and OnReadStart once each, and records ok.
func TestACancelBeforeReadingStopsTheRun(t *testing.T) {
	outcome := &PresyncOutcome{Code: "presync_ok", Name: "ok", Class: "none", Synced: true}

	t.Run("positive twin: nothing cancelled reads the source", func(t *testing.T) {
		var p cancelProbe
		pauseRunAt(t, &p, "", nil)
		out := tmpDir(t)
		var rl readLog
		opts := probedOptions(Options{Inputs: []string{completeMaildir(t)}, Out: out, Index: true}, &p, outcome, nil)
		res, err := Run(context.Background(), opts, rl.logger(), nil)
		if err != nil {
			t.Fatalf("the healthy run failed: %v", err)
		}
		assure.Reached(t, res.Stats.Exported, "messages exported by the healthy run")
		if !rl.read() {
			t.Fatalf("the healthy run logged no source read; the probe cannot see a read:\n%s", rl.String())
		}
		assure.Reached(t, p.points, "the run's pause steps")
		if p.hook != 1 || p.release != 1 || p.readStart != 1 {
			t.Errorf("hook %d, release %d, read start %d; want 1 each", p.hook, p.release, p.readStart)
		}
		if lr := recordOnDisk(t, out); lr.Status != state.RunOK {
			t.Errorf("the record reads %q, want ok", lr.Status)
		}
		if res.Presync != outcome {
			t.Errorf("Result.Presync = %+v, want the hook's outcome", res.Presync)
		}
	})

	t.Run("a cancel before the hook never calls it", func(t *testing.T) {
		var p cancelProbe
		pauseRunAt(t, &p, "", nil)
		out := tmpDir(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		var rl readLog
		opts := probedOptions(Options{Inputs: []string{completeMaildir(t)}, Out: out, Index: true}, &p, outcome, nil)
		res, err := Run(ctx, opts, rl.logger(), nil)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
		if p.hook != 0 || p.readStart != 0 || len(p.points) != 0 {
			t.Errorf("hook %d, read start %d, steps %v; want none of them", p.hook, p.readStart, p.points)
		}
		if res.Stats.Exported != 0 || rl.read() {
			t.Errorf("a cancelled run read the source: %d exported, log:\n%s", res.Stats.Exported, rl.String())
		}
		if lr := recordOnDisk(t, out); lr.Status != state.RunCancelled {
			t.Errorf("the record on disk reads %q, want cancelled", lr.Status)
		}
	})

	t.Run("a cancel during the hook stops before the index and the read", func(t *testing.T) {
		var p cancelProbe
		pauseRunAt(t, &p, "", nil)
		out := tmpDir(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var rl readLog
		opts := probedOptions(Options{Inputs: []string{completeMaildir(t)}, Out: out, Index: true}, &p, outcome, cancel)
		res, err := Run(ctx, opts, rl.logger(), nil)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
		if p.hook != 1 || p.release != 1 {
			t.Errorf("hook %d, release %d; want the hook called and its Release run once", p.hook, p.release)
		}
		if p.readStart != 0 || len(p.points) != 0 {
			t.Errorf("read start %d, steps %v; the run went past the T16 check", p.readStart, p.points)
		}
		if res.Stats.Exported != 0 || rl.read() {
			t.Errorf("a cancelled run read the source: %d exported, log:\n%s", res.Stats.Exported, rl.String())
		}
		if _, err := os.Stat(filepath.Join(out, "search.db")); !os.IsNotExist(err) {
			t.Errorf("the search index was opened after the cancel (%v)", err)
		}
		if lr := recordOnDisk(t, out); lr.Status != state.RunCancelled {
			t.Errorf("the record on disk reads %q, want cancelled (never running)", lr.Status)
		}
		if res.Presync != outcome {
			t.Errorf("Result.Presync = %+v, want the hook's outcome on the cancel's return", res.Presync)
		}
	})

	t.Run("a cancel during the index open stops at read start", func(t *testing.T) {
		var p cancelProbe
		out := tmpDir(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		pauseRunAt(t, &p, "index-open", cancel)
		var rl readLog
		opts := probedOptions(Options{Inputs: []string{completeMaildir(t)}, Out: out, Index: true}, &p, outcome, nil)
		res, err := Run(ctx, opts, rl.logger(), nil)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
		if len(p.points) == 0 || p.points[0] != "index-open" {
			t.Fatalf("the run never paused at the index open: %v", p.points)
		}
		if p.readStart != 1 || p.release != 1 {
			t.Errorf("read start %d, release %d; want 1 each", p.readStart, p.release)
		}
		if res.Stats.Exported != 0 || rl.read() {
			t.Errorf("a cancelled run read the source: %d exported, log:\n%s", res.Stats.Exported, rl.String())
		}
		if lr := recordOnDisk(t, out); lr.Status != state.RunCancelled {
			t.Errorf("the record on disk reads %q, want cancelled", lr.Status)
		}
		if res.Presync != outcome {
			t.Errorf("Result.Presync = %+v, want the hook's outcome", res.Presync)
		}
	})

	// The sealed catch-up: a first keyed run with indexing off leaves records and no
	// index; the next indexed run catches the index up from the sealed units before its
	// source loop. It is given a second source, a maildir, which a run that reads exports.
	sealedFixture := func(t *testing.T) (out string, key *vault.Key, inputs []string, before int) {
		t.Helper()
		pst := encryptedSourcePST(t)
		out, key = encryptedOut(t)
		if _, err := Run(context.Background(), Options{Inputs: []string{pst}, Out: out, Key: key}, discard(), nil); err != nil {
			t.Fatal(err)
		}
		return out, key, []string{pst, completeMaildir(t)}, loadSealed(t, out, key).Len()
	}

	t.Run("positive twin: the sealed catch-up then the read", func(t *testing.T) {
		var p cancelProbe
		pauseRunAt(t, &p, "", nil)
		out, key, inputs, before := sealedFixture(t)
		var rl readLog
		opts := probedOptions(Options{Inputs: inputs, Out: out, Key: key, Index: true}, &p, outcome, nil)
		res, err := Run(context.Background(), opts, rl.logger(), nil)
		if err != nil {
			t.Fatalf("the healthy keyed run failed: %v", err)
		}
		if !reflect.DeepEqual(p.points, []string{"index-open", "sealed-catch-up"}) {
			t.Fatalf("steps %v, want the index open then the sealed catch-up", p.points)
		}
		assure.Reached(t, res.Stats.Exported, "messages the keyed run exported")
		if !rl.read() {
			t.Fatalf("the healthy keyed run logged no source read:\n%s", rl.String())
		}
		if after := loadSealed(t, out, key).Len(); after <= before {
			t.Errorf("the keyed run kept no new record (%d before, %d after)", before, after)
		}
		requireWholeIndex(t, out, key)
	})

	t.Run("a cancel during the sealed catch-up keeps its work and reads nothing", func(t *testing.T) {
		var p cancelProbe
		out, key, inputs, before := sealedFixture(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		pauseRunAt(t, &p, "sealed-catch-up", cancel)
		var rl readLog
		opts := probedOptions(Options{Inputs: inputs, Out: out, Key: key, Index: true}, &p, outcome, nil)
		res, err := Run(ctx, opts, rl.logger(), nil)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
		if !reflect.DeepEqual(p.points, []string{"index-open", "sealed-catch-up"}) {
			t.Fatalf("steps %v, want the index open then the sealed catch-up", p.points)
		}
		if p.readStart != 1 || p.release != 1 {
			t.Errorf("read start %d, release %d; want 1 each", p.readStart, p.release)
		}
		if res.Stats.Exported != 0 || rl.read() {
			t.Errorf("a cancelled run read the source: %d exported, log:\n%s", res.Stats.Exported, rl.String())
		}
		if after := loadSealed(t, out, key).Len(); after != before {
			t.Errorf("a cancelled run changed the records (%d before, %d after)", before, after)
		}
		// The catch-up's work is kept, as a cancel during the first source keeps it:
		// the index is whole and sealed.
		requireWholeIndex(t, out, key)
		if lr := recordOnDisk(t, out); lr.Status != state.RunCancelled {
			t.Errorf("the record on disk reads %q, want cancelled", lr.Status)
		}
		if res.Presync != outcome {
			t.Errorf("Result.Presync = %+v, want the hook's outcome", res.Presync)
		}
	})

	t.Run("the outcome survives an early return", func(t *testing.T) {
		var p cancelProbe
		pauseRunAt(t, &p, "", nil)
		out := tmpDir(t)
		// A folder where the search index file goes: the index open fails, an early
		// return after the hook (app/run.go's FailIndex return).
		if err := os.MkdirAll(filepath.Join(out, "search.db", "x"), 0o755); err != nil {
			t.Fatal(err)
		}
		opts := probedOptions(Options{Inputs: []string{completeMaildir(t)}, Out: out, Index: true}, &p, outcome, nil)
		res, err := Run(context.Background(), opts, discard(), nil)
		if err == nil || errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want the index open's failure", err)
		}
		if p.hook != 1 || p.readStart != 0 {
			t.Errorf("hook %d, read start %d; want the hook once and no read start", p.hook, p.readStart)
		}
		if res.Presync != outcome {
			t.Errorf("Result.Presync = %+v, want the hook's outcome on an early return", res.Presync)
		}
	})

	t.Run("a panicking hook's outcome is internal-error", func(t *testing.T) {
		var p cancelProbe
		pauseRunAt(t, &p, "", nil)
		out := tmpDir(t)
		opts := probedOptions(Options{Inputs: []string{completeMaildir(t)}, Out: out}, &p, outcome, func() { panic("pre-sync boom") })
		res, err := Run(context.Background(), opts, discard(), nil)
		if err != nil {
			t.Fatalf("a panicking hook must not fail the run: %v", err)
		}
		got := assure.Reached(t, res.Presync, "the panicking hook's outcome")
		if got.Name != "internal-error" || got.Class != "warn" {
			t.Errorf("Result.Presync = %+v, want internal-error, warn", got)
		}
		if p.readStart != 1 {
			t.Errorf("read start %d, want 1: the run reads after a recovered panic", p.readStart)
		}
	})
}

// covers: MA-902, R32
// The pre-sync outcome is in memory only (design-presync-attended §4.1, C-36, C-53):
// PresyncOutcome carries no JSON tag and has no Attended field (C-80), so nothing
// that could reach a persisted type carries it; AS7 converts it to the record's own
// type.
func TestThePresyncOutcomeIsInMemoryOnly(t *testing.T) {
	ty := reflect.TypeOf(PresyncOutcome{})
	assure.Reached(t, ty.NumField(), "PresyncOutcome's fields")
	for i := 0; i < ty.NumField(); i++ {
		f := ty.Field(i)
		if tag, ok := f.Tag.Lookup("json"); ok {
			t.Errorf("PresyncOutcome.%s has a json tag %q; the outcome is in memory only", f.Name, tag)
		}
		if f.Name == "Attended" {
			t.Errorf("PresyncOutcome has an Attended field (C-80 dropped it)")
		}
	}
	for _, name := range []string{"PresyncWanted", "PresyncAttended", "OnReadStart"} {
		f, ok := reflect.TypeOf(Options{}).FieldByName(name)
		if !ok {
			t.Errorf("Options has no %s", name)
			continue
		}
		if _, tagged := f.Tag.Lookup("json"); tagged {
			t.Errorf("Options.%s has a json tag; it is set by the caller and never written", name)
		}
	}
}
