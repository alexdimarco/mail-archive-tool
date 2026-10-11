package app

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/source"
	"mail-archive-tool/internal/state"
)

// The Graph walk's handling of a message whose parse gives up, and of an item it was
// not asked for (§4.8.2): the stub rule, FC27's adoption and the -since rule.

// parseGivesUp makes every parse of a message fail the way a crashed MIME stack
// does: safeParseMessage's constant "(unreadable message)" stub.
func parseGivesUp(t *testing.T) {
	t.Helper()
	parseRFC822 = func([]byte) *model.Message { return &model.Message{Subject: state.LegacyPlaceholderSubject} }
	t.Cleanup(func() { parseRFC822 = source.ParseRFC822 })
}

// covers: MA-318, R1, R3, R10, S46
// A message whose parse gives up, with a Message-ID in the listing, becomes a
// record under mid:<id> (the stub's identity is no content hash that folds its
// received time), so the fast path skips it on every later run: no download storm.
// Without a listing Message-ID its identity stays the content hash, as today, and
// it is downloaded again each run (the acknowledged price of a no-Message-ID item).
func TestGraphParseStubTakesTheListingMessageID(t *testing.T) {
	parseGivesUp(t)
	s := sweepServer(false, []string{"S", "T"}, nil, nil)
	defer s.Close()
	s.SetNoMid("T")
	out := tmpDir(t)
	g := isoOptions(s)
	if res, err := sweepRun(g, out, export.Incremental); err != nil || res.Stats.Exported != 2 {
		t.Fatalf("run 1: exported %d, err %v; want 2, nil: a stub is info, not a partial run", res.Stats.Exported, err)
	}
	if _, rec := recordForID(t, out, "mid:s@x"); !rec.Present {
		t.Errorf("S's stub record: %+v", rec)
	}
	s.ResetRequests()
	if res, err := sweepRun(g, out, export.Incremental); err != nil || res.Stats.Exported != 0 {
		t.Fatalf("run 2: exported %d, err %v; want 0, nil", res.Stats.Exported, err)
	}
	if n := s.Requests("/users/u1/messages/S/"); n != 0 {
		t.Errorf("run 2 downloaded S %d times: its stub has no listing Message-ID in its identity, so the fast path cannot skip it", n)
	}
	if n := s.Requests("/users/u1/messages/T/"); n != 1 {
		t.Errorf("run 2 downloaded T %d times; want 1: with no Message-ID in the listing its identity stays a content hash", n)
	}
	if got := loadArchive(t, out).Len(); got != 2 {
		t.Errorf("the archive holds %d records after two runs, want 2", got)
	}
}

// covers: MA-323, R1, R5, S46
// FC27 through the walk: an older build's Graph stub (the placeholder subject,
// Terminal ["body"], an identity that is a hash of its received time) for a message
// that still cannot be parsed is adopted on the first run that gives its stub the
// listing's Message-ID: matched by its Received time here, stamped seen, re-classed
// to the info label legacy-placeholder and logged once, and never marked gone, not
// by that run's sweep and not by the next run's, when nothing stamps it. A legacy
// stub whose message is no longer listed is not adopted, and the sweep marks it gone
// as it would any record.
func TestGraphLegacyStubIsAdoptedByTheWalk(t *testing.T) {
	parseGivesUp(t)
	received := time.Date(2025, 2, 1, 10, 0, 0, 0, time.UTC)
	s := sweepServer(false, []string{"G"}, nil, nil)
	defer s.Close()
	s.SetReceived("G", received)
	out := tmpDir(t)
	tok := mailboxToken(t, out, "u1")
	m := loadArchive(t, out)
	old := time.Now().UTC().Add(-72 * time.Hour)
	legacy := func(r time.Time, name string) (string, state.Record) {
		id := (&model.Message{Subject: state.LegacyPlaceholderSubject, Received: r}).Identity()
		return state.LiveKey(tok, id), state.Record{Path: tok + "/Inbox/" + name + ".html", Folder: "Inbox", FirstFolder: "Inbox",
			ExportedAt: old, FirstSeen: old, LastSeen: old, Present: true, Subject: state.LegacyPlaceholderSubject,
			Date: r.UTC().Format(time.RFC3339), Terminal: []string{state.MissingBody}}
	}
	kLegacy, rLegacy := legacy(received, "legacy")
	kGone, rGone := legacy(received.AddDate(-1, 0, 0), "departed")
	m.Add(kLegacy, rLegacy)
	m.Add(kGone, rGone)
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}

	var logs bytes.Buffer
	run := func() error {
		_, err := RunGraph(context.Background(), isoOptions(s), Options{Out: out, Mode: export.Incremental}, log.New(&logs, "", 0))
		return err
	}
	if err := run(); err != nil {
		t.Fatalf("run 1: %v", err)
	}
	if strings.Count(logs.String(), "re-classed 1 legacy placeholder(s)") != 1 {
		t.Errorf("the run did not log the adoption once:\n%s", logs.String())
	}
	runAt := runStart(t, out)
	rec, _ := loadArchive(t, out).Get(kLegacy)
	if !rec.Present || !rec.LastSeen.Equal(runAt) || len(rec.Terminal) != 1 || rec.Terminal[0] != state.LabelLegacyPlaceholder || len(rec.Missing) != 0 || rec.Path != rLegacy.Path {
		t.Errorf("the adopted stub = %+v; want it present, stamped by the run, re-classed legacy-placeholder and kept whole", rec)
	}
	if goneKey(t, out, kLegacy) {
		t.Error("the adopted stub got a gone event")
	}
	if rec, _ := loadArchive(t, out).Get(kGone); rec.Present || !goneKey(t, out, kGone) {
		t.Errorf("the unmatched legacy stub = %+v, gone event %v; want it swept gone like any record whose message left", rec, goneKey(t, out, kGone))
	}
	if _, rec := recordForID(t, out, "mid:g@x"); !rec.Present {
		t.Errorf("G has no record of its own under its Message-ID: %+v", rec)
	}

	// Run 2: G is skipped by the fast path, so nothing stamps the legacy stub; the sweep
	// spares it.
	logs.Reset()
	s.ResetRequests()
	if err := run(); err != nil {
		t.Fatalf("run 2: %v", err)
	}
	if n := s.Requests("/users/u1/messages/G/"); n != 0 {
		t.Errorf("run 2 downloaded G %d times", n)
	}
	if rec, _ := loadArchive(t, out).Get(kLegacy); !rec.Present || goneKey(t, out, kLegacy) {
		t.Errorf("the adopted stub is Present %v, gone event %v after the next run: it is never marked gone", rec.Present, goneKey(t, out, kLegacy))
	}
	if strings.Contains(logs.String(), "re-classed") {
		t.Errorf("run 2 re-classed again:\n%s", logs.String())
	}
}

// covers: MA-323, R1, R5, S46, R11
// The Graph walk's -since rule (§4.8.2 step 1): under -since a message received
// before the window that has no record and whose download fails was not asked for,
// so it is counted as skipped by date and records no gap, and a gap an earlier run
// left for it is kept as it was (not observed again, not closed). A message inside
// the window that fails is a gap as ever.
func TestGraphSinceRuleRecordsNoGapForUnrequestedItem(t *testing.T) {
	s := sweepServer(false, []string{"A1", "OLD", "A2"}, nil, nil)
	defer s.Close()
	inside := time.Date(2025, 3, 5, 9, 0, 0, 0, time.UTC)
	before := time.Date(2025, 2, 1, 9, 0, 0, 0, time.UTC)
	s.SetReceived("A1", inside)
	s.SetReceived("A2", inside)
	s.SetReceived("OLD", before)
	s.SetValue("OLD", poison500)
	s.SetValue("A2", poison500)
	out := tmpDir(t)
	tok := mailboxToken(t, out, "u1")
	m := loadArchive(t, out)
	seededAt := time.Now().UTC().Add(-48 * time.Hour)
	m.ObserveGap(state.CaptureGap{Token: tok, Folder: "Inbox", Scope: state.GapScopeItem, Locator: "graph:OLD", Reason: "fetch-failed",
		Class: state.ClassFillable, Subject: "subj-OLD", Received: before.Format(time.RFC3339), Detail: "seeded"}, seededAt)
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}

	opts := Options{Out: out, Mode: export.Incremental, Since: time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)}
	res, err := RunGraph(context.Background(), isoOptions(s), opts, log.New(&bytes.Buffer{}, "", 0))
	if !errors.Is(err, ErrPartial) {
		t.Fatalf("the run returned %v, want ErrPartial for the failing message inside the window", err)
	}
	if res.Stats.SkippedDate < 1 {
		t.Errorf("SkippedDate = %d, want the unrequested message counted", res.Stats.SkippedDate)
	}
	byLoc := map[string]state.CaptureGap{}
	for _, g := range graphGaps(t, out) {
		byLoc[g.Locator] = g
	}
	if len(byLoc) != 2 {
		t.Fatalf("the ledger holds %+v, want the kept OLD gap and A2's new one", byLoc)
	}
	old := byLoc["graph:OLD"]
	if old.Detail != "seeded" || old.Runs != 1 || old.LastRun.After(seededAt.Add(time.Second)) {
		t.Errorf("OLD's gap = %+v; want it exactly as the earlier run left it (not observed, not closed)", old)
	}
	if g := byLoc["graph:A2"]; g.Reason != "fetch-failed" || g.LastRun.Before(seededAt.Add(24*time.Hour)) {
		t.Errorf("A2's gap = %+v; want a fetch-failed gap observed by this run", g)
	}
}
