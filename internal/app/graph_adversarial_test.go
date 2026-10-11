package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"testing"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/graph"
	"mail-archive-tool/internal/graph/graphtest"
	"mail-archive-tool/internal/state"
)

// The tombstones of the FI-S3 adversarial pass (docs/review-fault-isolation-s3-
// adversarial.md): each reproduces one confirmed finding through RunGraph on the
// shared fake, and was seen RED against the code before its fix.

// hostileText is what a hostile Graph endpoint puts in an error body: a terminal
// title-set escape, a clear-screen, and a right-to-left override.
const hostileText = "\x1b]0;pwned\x07\x1b[2J ‮evil"

// hostileAnswer answers status with hostileText as the body.
func hostileAnswer(status int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		io.WriteString(w, hostileText)
	}
}

// wantNoTerminalControl fails when s carries an ESC (or any C0 control) or a bidi
// override: text the run prints on the operator's terminal.
func wantNoTerminalControl(t *testing.T, what, s string) {
	t.Helper()
	if strings.ContainsAny(s, "\x1b\x07‮") {
		t.Errorf("%s carries a terminal escape or a bidi override: %q", what, s)
	}
}

// covers: MA-314, R10, R17, S46
// S3-AO3: a $value whose end the framing does not mark (200 with neither a
// Content-Length nor chunked encoding: the body ends where the connection closes)
// cannot be told from a dropped connection, so it is a transport fault and never a
// complete message: its record would be terminal (SourceComplete) and the
// Message-ID fast path would skip it on every later run, a silent permanent
// truncation. It is a fetch-transport gap (the same as the Content-Length and
// chunked truncations), the message is not archived, and the next healthy run
// archives it whole and closes the gap. Twin first: a gzip-compressed answer, whose
// length net/http does not report, is complete (the gzip trailer marks its end) and
// is archived.
func TestGraphCloseDelimitedBodyIsATransportFault(t *testing.T) {
	s := sweepServer(false, []string{"A1", "A2", "A3"}, nil, nil)
	defer s.Close()
	out := tmpDir(t)
	g := isoOptions(s)

	s.SetValue("A2", graphtest.Gzipped(graphtest.Body("A2")))
	if res, err := sweepRun(g, out, export.Incremental); err != nil || res.Stats.Exported != 3 || len(graphGaps(t, out)) != 0 {
		t.Fatalf("the twin (a gzip answer): exported %d, err %v, gaps %d; want 3, nil, 0", res.Stats.Exported, err, len(graphGaps(t, out)))
	}

	out = tmpDir(t)
	whole := graphtest.Body("A2")
	s.SetValue("A2", graphtest.CloseDelimited(whole[:30]))
	if _, err := sweepRun(g, out, export.Incremental); !errors.Is(err, ErrPartial) {
		t.Fatalf("a close-delimited $value: the run returned %v, want ErrPartial (a truncated body is not a capture)", err)
	}
	if archived(t, out, "a2@x") {
		t.Fatal("A2 was archived from a body its framing does not show to be whole")
	}
	if !archived(t, out, "a1@x") || !archived(t, out, "a3@x") {
		t.Error("the items around A2 were not archived")
	}
	gaps := graphGaps(t, out)
	if len(gaps) != 1 || gaps[0].Locator != "graph:A2" || gaps[0].Reason != "fetch-transport" || gaps[0].Class != state.ClassFillable {
		t.Fatalf("the ledger holds %+v; want one fillable fetch-transport gap at graph:A2", gaps)
	}

	s.SetValue("A2", nil)
	res, err := sweepRun(g, out, export.Incremental)
	if err != nil || !archived(t, out, "a2@x") || len(graphGaps(t, out)) != 0 || res.Gaps.Recovered != 1 {
		t.Errorf("the healed run: err %v, archived %v, gaps %+v, recovered %d; want A2 archived whole and its gap closed as recovered",
			err, archived(t, out, "a2@x"), graphGaps(t, out), res.Gaps.Recovered)
	}
}

// covers: MA-316, R10, R6, R1, S46
// S3-AO1: a listing answer that is not a Graph page is never an empty folder. A real
// page always carries a value array; a 2xx whose body is {}, null or an error object
// (a proxy, a captive portal, an impersonator) used to decode to zero items and no
// nextLink, so the folder counted as fully walked and the sweep marked every record
// of it gone. It is a folder-list-failed gap now, the next folder walks, and the
// mailbox's sweep is deferred. A folder list that is not a folder list (no value
// array, or no folder at all: a mailbox always has its Inbox) fails the mailbox
// before anything is walked. The twin documents the limit: an honest-looking
// {"value":[]} cannot be told from an empty folder and is swept like one.
func TestGraphMalformedListingIsNeverAnEmptyFolder(t *testing.T) {
	setup := func(t *testing.T) (*graphtest.Server, GraphOptions, string) {
		s := sweepServer(false, []string{"A1", "A2"}, []string{"B1"}, nil)
		t.Cleanup(s.Close)
		g, out := isoOptions(s), tmpDir(t)
		if _, err := sweepRun(g, out, export.Incremental); err != nil {
			t.Fatalf("run 1: %v", err)
		}
		return s, g, out
	}
	t.Run("the twin: an honest empty value array is an empty folder", func(t *testing.T) {
		s, g, out := setup(t)
		s.SetListHandler("u1", "F_IN", graphtest.Answer(200, `{"value":[]}`))
		if _, err := sweepRun(g, out, export.Incremental); err != nil {
			t.Fatalf("run 2: %v", err)
		}
		for _, id := range []string{"a1@x", "a2@x"} {
			k, rec := recordForID(t, out, id)
			wantGone(t, out, id+" (the folder listed empty)", k, rec)
		}
	})
	for _, body := range []string{`{}`, `null`, `{"error":{"code":"Oops"}}`, `[]`, `"<html>sign in to the guest wifi</html>"`} {
		t.Run("a folder's listing answers "+body, func(t *testing.T) {
			s, g, out := setup(t)
			s.SetListHandler("u1", "F_IN", graphtest.Answer(200, body))
			s.RemoveMessage("u1", "F_SE", "B1")
			if _, err := sweepRun(g, out, export.Incremental); !errors.Is(err, ErrPartial) {
				t.Fatalf("run 2 returned %v, want ErrPartial: the folder was not read", err)
			}
			for _, id := range []string{"a1@x", "a2@x", "b1@x"} {
				k, rec := recordForID(t, out, id)
				wantDeferred(t, out, id+" (the walk could not read the Inbox: its sweep is deferred)", k, rec)
			}
			gaps := graphGaps(t, out)
			if len(gaps) != 1 || gaps[0].Reason != "folder-list-failed" || gaps[0].Scope != state.GapScopeFolder || gaps[0].Folder != "Inbox" {
				t.Errorf("the ledger holds %+v; want one folder-list-failed gap for the Inbox", gaps)
			}
		})
	}
	for _, body := range []string{`{}`, `null`, `{"value":[]}`} {
		t.Run("the folder list answers "+body, func(t *testing.T) {
			s, g, out := setup(t)
			s.SetFolderListHandler("u1", graphtest.Answer(200, body))
			s.RemoveMessage("u1", "F_IN", "A2")
			if _, err := sweepRun(g, out, export.Incremental); err == nil || errors.Is(err, ErrPartial) {
				t.Fatalf("run 2 returned %v, want the mailbox to fail: a folder list with no folder in it is not a mailbox", err)
			}
			for _, id := range []string{"a1@x", "a2@x", "b1@x"} {
				k, rec := recordForID(t, out, id)
				wantDeferred(t, out, id+" (no folder was walked)", k, rec)
			}
		})
	}
}

// covers: MA-310, R1, R10, R17, S46
// S3-AO2: a $value that is not a message (a 200 HTML page, an empty body: a captive
// portal, an impersonating proxy) is a fetch-failed gap, never a record. It used to
// parse to a headerless "message", and three such items collapsed into ONE sha:
// record under no labels and no gap, with a clean exit, so a portal that persisted
// made every run a silent non-capture. The next healthy run archives each under its
// own Message-ID and closes the gaps as recovered. Twin first: the healthy mailbox.
func TestGraphValueThatIsNotAMessageIsAGapNotARecord(t *testing.T) {
	s := sweepServer(false, []string{"A1", "A2", "A3"}, nil, nil)
	defer s.Close()
	g := isoOptions(s)
	if res, err := sweepRun(g, tmpDir(t), export.Incremental); err != nil || res.Stats.Exported != 3 {
		t.Fatalf("the twin: exported %d, err %v; want 3, nil", res.Stats.Exported, err)
	}

	out := tmpDir(t)
	s.SetValue("A1", graphtest.Answer(200, "<html><body>Sign in to the guest wifi</body></html>"))
	s.SetValue("A2", graphtest.Answer(200, "<!DOCTYPE html>\n<title>Access denied</title>"))
	s.SetValue("A3", graphtest.Answer(200, ""))
	res, err := sweepRun(g, out, export.Incremental)
	if !errors.Is(err, ErrPartial) {
		t.Fatalf("three answers that are no message: the run returned %v, want ErrPartial", err)
	}
	if res.Stats.Exported != 0 || loadArchive(t, out).Len() != 0 {
		t.Errorf("exported %d, records %d; want none: a page that is not a message is not an archived message", res.Stats.Exported, loadArchive(t, out).Len())
	}
	gaps := graphGaps(t, out)
	if len(gaps) != 3 {
		t.Fatalf("the ledger holds %+v; want one gap per item", gaps)
	}
	for _, g := range gaps {
		if g.Reason != "fetch-failed" || g.Class != state.ClassFillable || g.Scope != state.GapScopeItem {
			t.Errorf("gap %+v; want a fillable fetch-failed item gap", g)
		}
	}

	for _, id := range []string{"A1", "A2", "A3"} {
		s.SetValue(id, nil)
	}
	res, err = sweepRun(g, out, export.Incremental)
	if err != nil || res.Stats.Exported != 3 || res.Gaps.Recovered != 3 || len(graphGaps(t, out)) != 0 {
		t.Errorf("the healed run: exported %d, err %v, recovered %d, gaps %+v; want 3, nil, 3, none", res.Stats.Exported, err, res.Gaps.Recovered, graphGaps(t, out))
	}
	for _, id := range []string{"a1@x", "a2@x", "a3@x"} {
		if !archived(t, out, id) {
			t.Errorf("%s is not archived under its own Message-ID", id)
		}
	}
}

// covers: MA-311, MA-323, R1, R10, S46
// S3-AO9 / S3-AI6: a hostile or very long item id keeps its own ledger entry. The
// ledger stores a locator control-stripped and cut at 512 bytes, and a key built from
// the raw id (what recovered and the known-poison lookup built) then found nothing,
// while distinct ids that shared the first 512 bytes collapsed into ONE entry: the
// run said "1 item(s) not captured" for three (R10), and healing them closed the
// entry as "gone from the source" and not as recovered. Three items whose 600-byte
// ids differ only at the end each keep an entry, are recovered by their own capture,
// and the entry's key is the one the lookups build.
func TestGraphLongItemIDsKeepTheirOwnGaps(t *testing.T) {
	s := sweepServer(false, nil, nil, nil)
	defer s.Close()
	long := strings.Repeat("a", 600)
	ids := []string{long + "1", long + "2", long + "3"}
	for _, id := range ids {
		s.AddMessage("u1", "F_IN", id)
		s.SetValue(id, poison500)
	}
	out := tmpDir(t)
	g := isoOptions(s)
	if _, err := sweepRun(g, out, export.Incremental); !errors.Is(err, ErrPartial) {
		t.Fatalf("run 1 returned %v, want ErrPartial", err)
	}
	m := loadArchive(t, out)
	if got := len(m.CaptureGapsSorted()); got != 3 || m.MissingGapCount() != 3 {
		t.Fatalf("the ledger holds %d entries and counts %d missing; want one entry per item, 3 and 3", got, m.MissingGapCount())
	}
	tok := mailboxToken(t, out, "u1")
	for _, id := range ids {
		if _, ok := m.GapFor(state.GapKey(tok, state.GapScopeItem, graphLocator(graph.MessageRef{ID: id}))); !ok {
			t.Errorf("no entry under the key the walk builds for the item ending %q: its recovery and its known-poison lookup would miss it", id[len(id)-3:])
		}
	}

	for _, id := range ids {
		s.SetValue(id, nil)
	}
	res, err := sweepRun(g, out, export.Incremental)
	if err != nil || res.Stats.Exported != 3 || res.Gaps.Recovered != 3 || res.Gaps.ClosedGone != 0 || len(graphGaps(t, out)) != 0 {
		t.Errorf("the healed run: exported %d, err %v, recovered %d, closed-gone %d, gaps %d; want 3, nil, 3, 0, 0",
			res.Stats.Exported, err, res.Gaps.Recovered, res.Gaps.ClosedGone, len(graphGaps(t, out)))
	}
}

// covers: MA-312, R21, R1, S37, S46
// S3-AI1 / S3-AO4 (OR10's hole): a run whose listing does not confirm immutable ids
// cannot match a fault's item to a record by id, and a record that holds an
// immutable id can still be that item (its listing and parsed Message-IDs differ,
// the L10-3 mismatch, so no Message-ID names it). The fault matched no record, so
// every record is spared this run's sweep: the message that is still in the mailbox
// gets no phantom {k,gone}, the gap's "defers gone-detection" says what happened,
// and the deferred sweep is made on the next run that meets no such fault. Twin:
// the same run without the fault sweeps the deleted message.
func TestGraphUnconfirmedIDsFaultSparesRecordsThatHoldAnImmutableID(t *testing.T) {
	s := sweepServer(true, []string{"N", "D"}, nil, nil)
	defer s.Close()
	s.SetMid("N", "n-list@x") // the listing's Message-ID is not the message's own
	s.SetBodyMid("N", "n-body@x")
	out := tmpDir(t)
	g := isoOptions(s)
	if _, err := sweepRun(g, out, export.Incremental); err != nil {
		t.Fatalf("run 1: %v", err)
	}
	if _, rec := recordForID(t, out, "n-body@x"); rec.PhysID != "N" {
		t.Fatalf("N's record holds the immutable id %q, want N: the case needs a record that carries one", rec.PhysID)
	}

	// Run 2: the tenant no longer honours immutable ids, D is deleted at the source and
	// N cannot be downloaded.
	s.HonorImmutableIDs(false)
	s.RemoveMessage("u1", "F_IN", "D")
	s.SetValue("N", poison500)
	if _, err := sweepRun(g, out, export.Incremental); !errors.Is(err, ErrPartial) {
		t.Fatalf("run 2 returned %v, want ErrPartial", err)
	}
	k, rec := recordForID(t, out, "n-body@x")
	wantDeferred(t, out, "N (it holds an immutable id this run cannot match)", k, rec)
	k, rec = recordForID(t, out, "d@x")
	wantDeferred(t, out, "D (deleted, but this run cannot tell it from N's record)", k, rec)
	if gaps := graphGaps(t, out); len(gaps) != 1 || gaps[0].Locator != "graph:N" || !gaps[0].DefersGone {
		t.Errorf("ledger %+v; want N's gap to record that it defers gone-detection", gaps)
	}

	// Run 3: the fault is gone; the deferred sweep is made (the twin: a clean walk sweeps).
	s.SetValue("N", nil)
	if _, err := sweepRun(g, out, export.Incremental); err != nil {
		t.Fatalf("run 3: %v", err)
	}
	k, rec = recordForID(t, out, "n-body@x")
	wantSeenNotGone(t, out, "N, once it downloads", k, rec, runStart(t, out))
	k, rec = recordForID(t, out, "d@x")
	wantGone(t, out, "D, on the next clean run", k, rec)
}

// covers: MA-312, R21, R1, S37, S46
// ESC-S3-1 (open, owner decision): the same hole with this run's ids CONFIRMED. A
// message restored under a new immutable id whose listing and parsed Message-IDs
// differ, and whose download fails, matches no record by Message-ID or by id; its
// archived record (the old id) is swept gone while the message is still there. The
// only rule that closes it spares every record on every fault that matched nothing,
// which with confirmed ids would make each NEW poison item (never archived, so
// always unmatched) defer the whole mailbox's gone-detection for as long as it
// persists, where OR10 as ruled spares only the records without an immutable id.
// That narrows deletion tracking and contradicts the ruling's text, so it is the
// owner's call; the phantom gone is reversible (the next run that lists the
// message records it present again).
func TestGraphReissuedIDFaultSparesTheArchivedRecord(t *testing.T) {
	t.Skip("ESC-S3-1: sparing every record on an unmatched fault in an id-confirmed run narrows gone-detection (OR10 ruled PhysID-less records only); owner decision. Invariant: R21 / MA-312 (a fault never sweeps a present message gone)")
	s := sweepServer(true, []string{"N"}, nil, nil)
	defer s.Close()
	s.SetMid("N", "n-list@x")
	s.SetBodyMid("N", "n-body@x")
	out := tmpDir(t)
	g := isoOptions(s)
	if _, err := sweepRun(g, out, export.Incremental); err != nil {
		t.Fatalf("run 1: %v", err)
	}
	s.RemoveMessage("u1", "F_IN", "N")
	s.AddMessage("u1", "F_IN", "N2") // N restored under a new immutable id
	s.SetMid("N2", "n-list@x")
	s.SetBodyMid("N2", "n-body@x")
	s.SetValue("N2", poison500)
	if _, err := sweepRun(g, out, export.Incremental); !errors.Is(err, ErrPartial) {
		t.Fatalf("run 2 returned %v, want ErrPartial", err)
	}
	k, rec := recordForID(t, out, "n-body@x")
	wantDeferred(t, out, "N (restored under a new id, its download fails)", k, rec)
}

// covers: MA-313, R5, R12, R18, S46
// S3-AO7: a mailbox-fatal fault (a 403 or 401 on a $value, a 401 on a listing) puts
// the server's own words in RunGraph's error and in the run log; those words are the
// endpoint's, so they reach the operator's terminal cleaned where they are built
// (S2A-AO5's convention), with the status and the ledger-free outcome unchanged.
func TestGraphFatalErrorTextIsCleanedWhereBuilt(t *testing.T) {
	cases := []struct {
		name  string
		apply func(*graphtest.Server)
		want  string
	}{
		{"$value 403", func(s *graphtest.Server) { s.SetValue("A2", hostileAnswer(403)) }, "403"},
		{"$value 401", func(s *graphtest.Server) { s.SetValue("A2", hostileAnswer(401)) }, "401"},
		{"listing 401", func(s *graphtest.Server) { s.SetListHandler("u1", "F_IN", hostileAnswer(401)) }, "401"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := sweepServer(false, []string{"A1", "A2", "A3"}, nil, nil)
			defer s.Close()
			c.apply(s)
			var logs bytes.Buffer
			_, err := RunGraph(context.Background(), isoOptions(s), Options{Out: tmpDir(t), Mode: export.Incremental}, log.New(&logs, "", 0))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("the run returned %v; want the mailbox's failure naming %s", err, c.want)
			}
			if !strings.Contains(logs.String(), "evil") || !strings.Contains(err.Error(), "evil") {
				t.Fatalf("the cleaned text lost the server's words (a stripped escape must leave the rest): err %q, log %q", err, logs.String())
			}
			wantNoTerminalControl(t, "RunGraph's error", err.Error())
			wantNoTerminalControl(t, "the run log", logs.String())
		})
	}
}
