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
