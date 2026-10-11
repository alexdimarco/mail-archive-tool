package app

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/http"
	"strings"
	"testing"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/graph/graphtest"
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
