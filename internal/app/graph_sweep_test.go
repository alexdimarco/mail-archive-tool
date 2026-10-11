package app

import (
	"context"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/graph/graphtest"
	"mail-archive-tool/internal/state"
)

// MA-312: a Graph fault defers gone-detection for exactly the records it could hide
// (§4.8.3, FC4, owner ruling OR10), and never marks a present message gone.

// sweepServer is mailbox u1 with an Inbox (F_IN), a Sent folder (F_SE) and a
// Projects folder (F_PR), each listed in one page.
func sweepServer(honor bool, inbox, sent, projects []string) *graphtest.Server {
	s := graphtest.NewServer()
	s.HonorImmutableIDs(honor)
	for _, f := range []struct {
		id, name string
		msgs     []string
	}{{"F_IN", "Inbox", inbox}, {"F_SE", "Sent", sent}, {"F_PR", "Projects", projects}} {
		s.AddFolder("u1", f.id, f.name, 50)
		for _, id := range f.msgs {
			s.AddMessage("u1", f.id, id)
		}
	}
	return s
}

// sweepRun is one run of mode of g into out.
func sweepRun(g GraphOptions, out string, mode export.Mode) (Result, error) {
	return RunGraph(context.Background(), g, Options{Out: out, Mode: mode}, log.New(io.Discard, "", 0))
}

// recordWith is the one record whose path holds needle (case-insensitively): a
// message with no Message-ID has no id in its key to look for.
func recordWith(t *testing.T, out, needle string) (string, state.Record) {
	t.Helper()
	var key string
	var rec state.Record
	n := 0
	for k, r := range loadArchive(t, out).All() {
		if strings.Contains(strings.ToLower(r.Path), strings.ToLower(needle)) {
			key, rec = k, r
			n++
		}
	}
	if n != 1 {
		t.Fatalf("want exactly one record whose path holds %q, found %d", needle, n)
	}
	return key, rec
}

// goneKey reports whether the run history holds a {k,gone} event for key.
func goneKey(t *testing.T, out, key string) bool {
	t.Helper()
	for _, ev := range mustHistory(t, out) {
		if ev.Gone && ev.K == key {
			return true
		}
	}
	return false
}

// wantSeenNotGone asserts the record at key is Present, was stamped by the run
// that began at runAt, and has no gone event.
func wantSeenNotGone(t *testing.T, out, what, key string, rec state.Record, runAt time.Time) {
	t.Helper()
	if !rec.Present || !rec.LastSeen.Equal(runAt) || goneKey(t, out, key) {
		t.Errorf("%s: Present %v, LastSeen %v (run %v), gone event %v; want it present and stamped by this run", what, rec.Present, rec.LastSeen, runAt, goneKey(t, out, key))
	}
}

// wantDeferred asserts the record is Present with no gone event: the run left it
// alone (its sweep was deferred); it need not be stamped.
func wantDeferred(t *testing.T, out, what, key string, rec state.Record) {
	t.Helper()
	if !rec.Present || goneKey(t, out, key) {
		t.Errorf("%s: Present %v, gone event %v; want it left alone by a sweep that was deferred", what, rec.Present, goneKey(t, out, key))
	}
}

// wantGone asserts the record was swept: not Present, with a gone event. A walk
// that carried a fault still sweeps what the fault could not hide, and this is
// how a test shows its sweep ran.
func wantGone(t *testing.T, out, what, key string, rec state.Record) {
	t.Helper()
	if rec.Present || !goneKey(t, out, key) {
		t.Errorf("%s: Present %v, gone event %v; want it swept gone", what, rec.Present, goneKey(t, out, key))
	}
}

func runStart(t *testing.T, out string) time.Time {
	t.Helper()
	lr, _, err := state.ReadLastRun(out)
	if err != nil {
		t.Fatal(err)
	}
	return lr.Started
}

// poison500 is a $value the service cannot convert: an item-class fault.
var poison500 = graphtest.Fail(500, "ErrorMimeContentConversionFailed")

// covers: MA-312, R21, R1, S37, S46
// Sweep safety (H2, FC4, OR10): each case runs a walk that meets a fault, shows
// the record the fault could hide is neither marked gone nor left unstamped where
// the design stamps it, and shows the walk's sweep still ran for what the fault
// could not hide (a message deleted at the source is swept gone in the same run),
// since a rule that deferred every sweep would pass the first half alone. A clean
// walk sweeps (the twin, MA-207 unchanged).
func TestGraphFaultsNeverSweepAPresentMessageGone(t *testing.T) {
	t.Run("(a) Full mode: the re-fetch of an archived message fails", func(t *testing.T) {
		s := sweepServer(false, []string{"A1", "A2"}, []string{"B1", "B2"}, nil)
		defer s.Close()
		out := tmpDir(t)
		g := isoOptions(s)
		if _, err := sweepRun(g, out, export.Incremental); err != nil {
			t.Fatalf("run 1: %v", err)
		}
		s.SetValue("A2", poison500)
		s.RemoveMessage("u1", "F_SE", "B2")
		if _, err := sweepRun(g, out, export.Full); err != nil {
			t.Fatalf("run 2 (Full, a refresh that fails keeps the archived capture): %v", err)
		}
		runAt := runStart(t, out)
		k, rec := recordForID(t, out, "a2@x")
		wantSeenNotGone(t, out, "A2 (its refresh failed)", k, rec, runAt)
		k, rec = recordForID(t, out, "b2@x")
		wantGone(t, out, "B2 (deleted at the source)", k, rec)
		if g := graphGaps(t, out); len(g) != 1 || g[0].Reason != "refresh-failed" || g[0].Class != state.ClassInfo || g[0].Locator != "graph:A2" {
			t.Errorf("ledger %+v; want one info refresh-failed gap at graph:A2", g)
		}
	})

	t.Run("(b) the immutable-id comparable path: the download of a re-issued id fails", func(t *testing.T) {
		s := sweepServer(true, []string{"A1", "A2"}, []string{"B1", "B2"}, nil)
		defer s.Close()
		out := tmpDir(t)
		g := isoOptions(s)
		if _, err := sweepRun(g, out, export.Incremental); err != nil {
			t.Fatalf("run 1: %v", err)
		}
		// A2's id is re-issued (a restore): the Message-ID is the archived one, the
		// immutable id is new, so the run must download to tell churn from a distinct
		// reuse, and the download fails.
		s.RemoveMessage("u1", "F_IN", "A2")
		s.AddMessage("u1", "F_IN", "A2R")
		s.SetMid("A2R", "a2@x")
		s.SetValue("A2R", poison500)
		s.RemoveMessage("u1", "F_SE", "B2")
		s.ResetRequests()
		if _, err := sweepRun(g, out, export.Incremental); !errors.Is(err, ErrPartial) {
			t.Fatalf("run 2 returned %v, want ErrPartial", err)
		}
		if n := s.Requests("/users/u1/messages/A2R/"); n != 1 {
			t.Fatalf("the re-issued id was fetched %d times: the case does not reach the comparable path", n)
		}
		runAt := runStart(t, out)
		k, rec := recordForID(t, out, "a2@x")
		wantSeenNotGone(t, out, "A2 (its re-issued id failed to download)", k, rec, runAt)
		k, rec = recordForID(t, out, "b2@x")
		wantGone(t, out, "B2 (deleted at the source)", k, rec)
	})

	t.Run("(c) a no-Message-ID fetch failure that no record matches", func(t *testing.T) {
		s := sweepServer(false, []string{"A1", "N1", "N2"}, nil, nil)
		defer s.Close()
		s.SetNoMid("N1")
		s.SetNoMid("N2")
		out := tmpDir(t)
		g := isoOptions(s)
		if _, err := sweepRun(g, out, export.Incremental); err != nil {
			t.Fatalf("run 1 (ids not honoured: PhysID-less records): %v", err)
		}
		// Run 2: the tenant honours immutable ids, and Sent's B1 and B2 are captured with
		// theirs.
		s.HonorImmutableIDs(true)
		s.AddMessage("u1", "F_SE", "B1")
		s.AddMessage("u1", "F_SE", "B2")
		if _, err := sweepRun(g, out, export.Incremental); err != nil {
			t.Fatalf("run 2: %v", err)
		}
		// Run 3: N2 and B2 are deleted at the source, and a new no-Message-ID message,
		// N3, cannot be fetched.
		s.RemoveMessage("u1", "F_IN", "N2")
		s.RemoveMessage("u1", "F_SE", "B2")
		s.AddMessage("u1", "F_IN", "N3")
		s.SetNoMid("N3")
		s.SetValue("N3", poison500)
		if _, err := sweepRun(g, out, export.Incremental); !errors.Is(err, ErrPartial) {
			t.Fatalf("run 3 returned %v, want ErrPartial", err)
		}
		k, rec := recordWith(t, out, "subj-n2")
		wantDeferred(t, out, "N2 (a genuinely deleted no-Message-ID message, PhysID-less)", k, rec)
		k, rec = recordForID(t, out, "b2@x")
		wantGone(t, out, "B2 (deleted in Sent; its record has an immutable id)", k, rec)
		// Run 4: nothing is wrong, and the deferred sweep is made.
		s.SetValue("N3", nil)
		if _, err := sweepRun(g, out, export.Incremental); err != nil {
			t.Fatalf("run 4: %v", err)
		}
		k, rec = recordWith(t, out, "subj-n2")
		wantGone(t, out, "N2, on the next clean run", k, rec)
	})

	t.Run("(c') the same fault when this run's ids are not immutable", func(t *testing.T) {
		s := sweepServer(true, []string{"A1", "N1"}, nil, nil)
		defer s.Close()
		s.SetNoMid("N1")
		out := tmpDir(t)
		g := isoOptions(s)
		if _, err := sweepRun(g, out, export.Incremental); err != nil {
			t.Fatalf("run 1: %v", err)
		}
		// Run 2: the listing's ids are not immutable now, N1 is deleted, and a new
		// no-Message-ID message cannot be fetched. N1's record has an id, but this run
		// cannot match ids, so every content-hash record is spared.
		s.HonorImmutableIDs(false)
		s.RemoveMessage("u1", "F_IN", "N1")
		s.AddMessage("u1", "F_IN", "N3")
		s.SetNoMid("N3")
		s.SetValue("N3", poison500)
		if _, err := sweepRun(g, out, export.Incremental); !errors.Is(err, ErrPartial) {
			t.Fatalf("run 2 returned %v, want ErrPartial", err)
		}
		k, rec := recordWith(t, out, "subj-n1")
		wantDeferred(t, out, "N1 (a content-hash record, ids unconfirmed this run)", k, rec)
		s.SetValue("N3", nil)
		if _, err := sweepRun(g, out, export.Incremental); err != nil {
			t.Fatalf("run 3: %v", err)
		}
		k, rec = recordWith(t, out, "subj-n1")
		wantGone(t, out, "N1, on the next clean run", k, rec)
	})

	t.Run("(d) a message moved into a folder whose listing then fails", func(t *testing.T) {
		s := sweepServer(false, []string{"A1", "A2"}, []string{"B1"}, nil)
		defer s.Close()
		out := tmpDir(t)
		g := isoOptions(s)
		if _, err := sweepRun(g, out, export.Incremental); err != nil {
			t.Fatalf("run 1: %v", err)
		}
		s.MoveMessage("u1", "F_IN", "F_PR", "A2")
		s.RemoveMessage("u1", "F_SE", "B1")
		s.SetListHandler("u1", "F_PR", poison500)
		if _, err := sweepRun(g, out, export.Incremental); !errors.Is(err, ErrPartial) {
			t.Fatalf("run 2 returned %v, want ErrPartial", err)
		}
		k, rec := recordForID(t, out, "a2@x")
		wantDeferred(t, out, "A2 (moved into the folder whose listing failed)", k, rec)
		k, rec = recordForID(t, out, "b1@x")
		wantDeferred(t, out, "B1 (deleted: a folder-scope fault defers the whole mailbox's sweep)", k, rec)
		s.SetListHandler("u1", "F_PR", nil)
		if _, err := sweepRun(g, out, export.Incremental); err != nil {
			t.Fatalf("run 3: %v", err)
		}
		k, rec = recordForID(t, out, "a2@x")
		wantSeenNotGone(t, out, "A2 once Projects lists", k, rec, runStart(t, out))
		if rec.Folder != "Projects" {
			t.Errorf("A2 is recorded in %q, want Projects", rec.Folder)
		}
		k, rec = recordForID(t, out, "b1@x")
		wantGone(t, out, "B1, on the next clean run", k, rec)
	})

	t.Run("(e) a moved no-Message-ID message whose fetch fails, ids confirmed", func(t *testing.T) {
		s := sweepServer(true, []string{"A1", "N1"}, nil, nil)
		defer s.Close()
		s.SetNoMid("N1")
		out := tmpDir(t)
		g := isoOptions(s)
		if _, err := sweepRun(g, out, export.Incremental); err != nil {
			t.Fatalf("run 1: %v", err)
		}
		s.MoveMessage("u1", "F_IN", "F_PR", "N1")
		s.SetValue("N1", poison500)
		s.RemoveMessage("u1", "F_IN", "A1")
		if _, err := sweepRun(g, out, export.Incremental); !errors.Is(err, ErrPartial) {
			t.Fatalf("run 2 returned %v, want ErrPartial", err)
		}
		k, rec := recordWith(t, out, "subj-n1")
		wantSeenNotGone(t, out, "N1 (moved, its fetch failed: stamped through its immutable id)", k, rec, runStart(t, out))
		k, rec = recordForID(t, out, "a1@x")
		wantGone(t, out, "A1 (deleted at the source)", k, rec)
	})

	t.Run("(f) Full mode: refs not attempted because their folder could not be written", func(t *testing.T) {
		s := unwritableServer()
		defer s.Close()
		out := tmpDir(t)
		g := isoOptions(s)
		if _, err := sweepRun(g, out, export.Incremental); err != nil {
			t.Fatalf("run 1: %v", err)
		}
		tok := mailboxToken(t, out, "u1")
		if err := os.RemoveAll(filepath.Join(out, tok, "B")); err != nil {
			t.Fatal(err)
		}
		writeData(t, filepath.Join(out, tok), "B", []byte("not a directory"))
		s.RemoveMessage("u1", "A", "A2")
		s.ResetRequests()
		if _, err := sweepRun(g, out, export.Full); !errors.Is(err, ErrPartial) {
			t.Fatalf("run 2 returned %v, want ErrPartial", err)
		}
		if n := s.Requests("/users/u1/messages/B3/"); n != 0 {
			t.Fatalf("B3 was fetched %d times: the case does not reach a ref skipped by the unwritable set", n)
		}
		runAt := runStart(t, out)
		for _, id := range []string{"b1@x", "b2@x", "b3@x"} {
			k, rec := recordForID(t, out, id)
			wantSeenNotGone(t, out, id+" (in the folder that could not be written)", k, rec, runAt)
		}
		k, rec := recordForID(t, out, "a2@x")
		wantGone(t, out, "A2 (deleted at the source)", k, rec)
	})

	t.Run("(g) a pre-v6 record whose listing and parsed Message-IDs differ (OR10)", func(t *testing.T) {
		s := sweepServer(false, []string{"G1"}, nil, nil)
		defer s.Close()
		s.SetMid("G1", "listed@x")
		s.SetBodyMid("G1", "parsed@x")
		s.SetValue("G1", poison500)
		out := tmpDir(t)
		tok := mailboxToken(t, out, "u1")
		m := loadArchive(t, out)
		old := time.Now().UTC().Add(-48 * time.Hour)
		for _, id := range []string{"parsed@x", "deleted@x"} {
			m.Add(state.LiveKey(tok, "mid:"+id), state.Record{Path: tok + "/Inbox/" + id + ".html", Folder: "Inbox", FirstFolder: "Inbox",
				ExportedAt: old, FirstSeen: old, LastSeen: old, Present: true})
		}
		if err := m.Save(); err != nil {
			t.Fatal(err)
		}
		g := isoOptions(s)
		if _, err := sweepRun(g, out, export.Incremental); !errors.Is(err, ErrPartial) {
			t.Fatalf("run 1 returned %v, want ErrPartial", err)
		}
		for _, id := range []string{"parsed@x", "deleted@x"} {
			k, rec := recordForID(t, out, id)
			wantDeferred(t, out, id+" (PhysID-less, its sweep deferred while a fault matched no record)", k, rec)
		}
		// The service converts G1 now: its parsed Message-ID is the archived one.
		s.SetValue("G1", nil)
		if _, err := sweepRun(g, out, export.Incremental); err != nil {
			t.Fatalf("run 2: %v", err)
		}
		k, rec := recordForID(t, out, "parsed@x")
		wantSeenNotGone(t, out, "parsed@x once G1 is read", k, rec, runStart(t, out))
		k, rec = recordForID(t, out, "deleted@x")
		wantGone(t, out, "deleted@x, on the next clean run (the twin: a clean walk sweeps a deleted message)", k, rec)
	})
}

// covers: MA-312, R21, S37, S46
// The run footer says the history is complete to here, so a later fold can trust
// it (§4.8.3): it is written only for a run that read every mailbox and missed
// nothing. A run that met a poison message leaves none; the run that heals it
// writes one.
func TestGraphHistoryFooterOnlyForACompleteTimeline(t *testing.T) {
	footers := func(out string) int {
		n := 0
		for _, ev := range mustHistory(t, out) {
			if ev.Completed != "" {
				n++
			}
		}
		return n
	}
	s := isoServer(true)
	defer s.Close()
	out := tmpDir(t)
	g := isoOptions(s)
	if _, err := isoRun(g, out); !errors.Is(err, ErrPartial) {
		t.Fatalf("run 1 returned %v, want ErrPartial", err)
	}
	if n := footers(out); n != 0 {
		t.Errorf("a run that could not capture POISON wrote %d footers, want none", n)
	}
	s.SetValue("POISON", nil)
	if _, err := isoRun(g, out); err != nil {
		t.Fatalf("run 2: %v", err)
	}
	if n := footers(out); n != 1 {
		t.Errorf("the run that captured everything wrote %d footers in all, want 1", n)
	}
}
