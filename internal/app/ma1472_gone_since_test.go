package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/autoplace"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/state"
)

// goneWarningFor is out's last-run record's mailbox_gone warning for path, failing the test when
// there is not exactly one.
func goneWarningFor(t *testing.T, lr state.LastRun, path string) state.RunWarning {
	t.Helper()
	var found []state.RunWarning
	for _, w := range lr.Warnings {
		if w.Code == state.MailboxGoneCode && w.Path == path {
			found = append(found, w)
		}
	}
	if len(found) != 1 {
		t.Fatalf("the record holds %d mailbox_gone warnings for %s, want 1: %+v", len(found), path, lr.Warnings)
	}
	return found[0]
}

// plantGoneSince rewrites out's last-run record with since as its mailbox_gone warning's time.
func plantGoneSince(t *testing.T, out string, since time.Time) {
	t.Helper()
	lr, st, err := state.ReadLastRun(out)
	if err != nil || st != state.LastRunPresent {
		t.Fatalf("no record to plant in: %v", err)
	}
	for i := range lr.Warnings {
		if lr.Warnings[i].Code == state.MailboxGoneCode {
			lr.Warnings[i].Since = since
		}
	}
	if err := state.WriteLastRun(out, lr); err != nil {
		t.Fatal(err)
	}
}

// between reports whether t lies in [from, to].
func between(t, from, to time.Time) bool { return !t.Before(from) && !t.After(to) }

// covers: MA-1472, R18
// The plaintext record keeps, for each skipped input, one mailbox_gone warning with its Path, Name
// and Since (docs/design-1.7.5-fixes.md §1.3), and Result.Skipped carries the same Since. Since is
// resolved once, straight after discovery: rule 1, the previous record's warning for the same
// input, carried; rule 3, this run's start. A time later than this run's start (a clock that was
// ahead) and one before MinPlausibleTime (a clock not set) become this run's start and are never
// carried. Positive twin first: a run that skipped nothing records no such warning. (Amended
// 2026-11-20, CON-02, the owner's Q8: a time later than this run's start is kept as recorded.)
func TestTheRecordKeepsWhenAMailboxWasFirstFoundGone(t *testing.T) {
	f := goneFixture(t, false, "imap.qza.example", "imap.qzb.example")
	a, b := f.stores["imap.qza.example"], f.stores["imap.qzb.example"]
	out := tmpDir(t)
	opts := Options{Inputs: []string{a, b}, Auto: true, Out: out, Unattended: true}
	if _, err, _ := goneRun(t, Options{Inputs: []string{a, b}, Auto: true, Out: out}); err != nil {
		t.Fatal(err)
	}
	if lr, _, _ := state.ReadLastRun(out); len(lr.Warnings) != 0 {
		t.Fatalf("positive twin: a run that skipped nothing records no warning: %+v", lr.Warnings)
	}
	os.RemoveAll(b)

	from := time.Now().UTC()
	res, err, _ := goneRun(t, opts)
	to := time.Now().UTC()
	if err != nil {
		t.Fatal(err)
	}
	lr, _, _ := state.ReadLastRun(out)
	w := goneWarningFor(t, lr, b)
	if w.Name != goneAddress("imap.qzb.example") || w.Message == "" || !between(w.Since, from, to) || w.Since.After(lr.Started) {
		t.Fatalf("the first run that skips: the warning's name, message and since (this run's start, %s to %s): %+v (started %s)", from, to, w, lr.Started)
	}
	if len(res.Skipped) != 1 || !res.Skipped[0].Since.Equal(w.Since) {
		t.Fatalf("Result.Skipped carries the same since: %+v", res.Skipped)
	}
	first := w.Since

	// Rule 1: the next run carries it.
	if _, err, _ := goneRun(t, opts); err != nil {
		t.Fatal(err)
	}
	if lr, _, _ = state.ReadLastRun(out); !goneWarningFor(t, lr, b).Since.Equal(first) {
		t.Fatalf("rule 1: the next run must carry %s, got %+v", first, lr.Warnings)
	}

	// A clock that was ahead, and one not set: this run's start, never carried.
	// Amended 2026-11-20 by the 1.7.5 adversarial pass (CON-02; the owner, 2026-11-20, Q8: "Yes,
	// change it (Recommended)"): a time ahead of this run's start is now kept as recorded, never
	// moved to the start, since moving it cut the WARN short when it was this run's clock that was
	// behind. The 48-hours-ahead case moves below, at the same strength (the exact time carried, by
	// the next run too); the 2020 and zero cases stay here unchanged.
	ahead := time.Now().Add(48 * time.Hour).Truncate(time.Second)
	plantGoneSince(t, out, ahead)
	for i := 0; i < 2; i++ {
		if _, err, _ := goneRun(t, opts); err != nil {
			t.Fatal(err)
		}
		lr, _, _ = state.ReadLastRun(out)
		if got := goneWarningFor(t, lr, b).Since; !got.Equal(ahead) {
			t.Fatalf("run %d: a planted since %s ahead of this run must be kept as recorded (Q8), got %s", i+1, ahead, got)
		}
	}
	for _, planted := range []time.Time{time.Date(2020, 3, 1, 0, 0, 0, 0, time.UTC), {}} {
		plantGoneSince(t, out, planted)
		from = time.Now().UTC()
		if _, err, _ := goneRun(t, opts); err != nil {
			t.Fatal(err)
		}
		to = time.Now().UTC()
		lr, _, _ = state.ReadLastRun(out)
		if got := goneWarningFor(t, lr, b).Since; !between(got, from, to) {
			t.Fatalf("a planted since %s must become this run's start (%s to %s), got %s", planted, from, to, got)
		}
	}
}

// covers: MA-1472, R18
// Rule 2: an archive 1.7.4 stopped because this input was gone (its record a failed run whose
// error is this input's own refusal, its streak's last kind local-source-unreachable) takes its
// first date from the streak's FirstUnreachable, not from the day of the upgrade. Positive twins
// first: another streak kind, or an error naming another input, give this run's start.
func TestAnArchive174StoppedTakesItsFirstDateFromTheStreak(t *testing.T) {
	f := goneFixture(t, false, "imap.qza.example", "imap.qzb.example")
	a, b := f.stores["imap.qza.example"], f.stores["imap.qzb.example"]
	t0 := time.Now().UTC().Add(-40 * 24 * time.Hour).Truncate(time.Second)
	stopped := func(t *testing.T, kind, refused string) string {
		t.Helper()
		out := tmpDir(t)
		if _, err, _ := goneRun(t, Options{Inputs: []string{a, b}, Auto: true, Out: out}); err != nil {
			t.Fatal(err)
		}
		if err := state.WriteLastRun(out, state.LastRun{Status: state.RunFailed, Started: t0.Add(30 * 24 * time.Hour), Finished: t0.Add(30 * 24 * time.Hour),
			Error: autoplace.GoneText(refused), Job: []string{"-out", out, "-mode", "incremental", "-auto", "-input", a, "-input", b}}); err != nil {
			t.Fatal(err)
		}
		if err := state.WriteSourceStreak(out, state.SourceStreak{ConsecutiveFails: 30, FirstUnreachable: t0, LastUnreachable: t0.Add(30 * 24 * time.Hour), LastKind: kind}); err != nil {
			t.Fatal(err)
		}
		return out
	}
	os.RemoveAll(b)
	sinceOf := func(t *testing.T, out string) (time.Time, time.Time, time.Time) {
		t.Helper()
		from := time.Now().UTC()
		if _, err, _ := goneRun(t, Options{Inputs: []string{a, b}, Auto: true, Out: out, Unattended: true}); err != nil {
			t.Fatal(err)
		}
		lr, _, _ := state.ReadLastRun(out)
		return goneWarningFor(t, lr, b).Since, from, time.Now().UTC()
	}
	for _, twin := range []struct{ kind, refused string }{{"m365-unreachable", b}, {"local-source-unreachable", a}} {
		if got, from, to := sinceOf(t, stopped(t, twin.kind, twin.refused)); !between(got, from, to) {
			t.Fatalf("positive twin %+v: this run's start (%s to %s), got %s", twin, from, to, got)
		}
	}
	if got, _, _ := sinceOf(t, stopped(t, "local-source-unreachable", b)); !got.Equal(t0) {
		t.Fatalf("rule 2: the streak's first unreachable %s, got %s", t0, got)
	}
}

// covers: MA-1472, R18
// The warning goes into the run's own warnings straight after discovery, so every record the run
// writes keeps it with its Since: the "running" record a killed run leaves, a cancelled run's and a
// failed run's. Positive twin first: the finished ok record (TestTheRecordKeeps…).
func TestEveryRecordTheRunWritesKeepsTheGoneMailbox(t *testing.T) {
	f := goneFixture(t, false, "imap.qza.example", "imap.qzb.example")
	a, b := f.stores["imap.qza.example"], f.stores["imap.qzb.example"]
	out := tmpDir(t)
	if _, err, _ := goneRun(t, Options{Inputs: []string{a, b}, Auto: true, Out: out}); err != nil {
		t.Fatal(err)
	}
	os.RemoveAll(b)
	if _, err, _ := goneRun(t, Options{Inputs: []string{a, b}, Auto: true, Out: out}); err != nil {
		t.Fatal(err)
	}
	lr, _, _ := state.ReadLastRun(out)
	first := goneWarningFor(t, lr, b).Since

	// The running record (what a killed run leaves), read while the run is between its start and
	// its reading.
	var running state.LastRun
	_, err, _ := goneRun(t, Options{Inputs: []string{a, b}, Auto: true, Out: out, BeforeRead: func(context.Context, []string) BeforeReadResult {
		running, _, _ = state.ReadLastRun(out)
		return BeforeReadResult{}
	}})
	if err != nil || running.Status != state.RunRunning || !goneWarningFor(t, running, b).Since.Equal(first) {
		t.Fatalf("the running record must keep the warning and its since %s: %+v (%v)", first, running, err)
	}

	// A cancelled run.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Run(ctx, Options{Inputs: []string{a, b}, Auto: true, Out: out, Mode: export.Incremental}, discard(), nil); err == nil {
		t.Fatal("a cancelled run must end cancelled")
	}
	if lr, _, _ = state.ReadLastRun(out); lr.Status != state.RunCancelled || !goneWarningFor(t, lr, b).Since.Equal(first) {
		t.Fatalf("the cancelled record must keep the warning and its since %s: %+v", first, lr)
	}

	// A failed run (its manifest cannot be read).
	if err := os.WriteFile(filepath.Join(out, ".mailarchive-manifest.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err, _ = goneRun(t, Options{Inputs: []string{a, b}, Auto: true, Out: out})
	assure.Reached(t, err, "the failed run's error")
	if lr, _, _ = state.ReadLastRun(out); lr.Status != state.RunFailed || !goneWarningFor(t, lr, b).Since.Equal(first) {
		t.Fatalf("the failed record must keep the warning and its since %s: %+v", first, lr)
	}
}
