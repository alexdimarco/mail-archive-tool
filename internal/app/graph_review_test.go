package app

import (
	"context"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/graph/graphtest"
	"mail-archive-tool/internal/state"
)

// eofHook calls hook when the body it wraps reports its end.
type eofHook struct {
	io.ReadCloser
	hook func()
}

func (b eofHook) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err == io.EOF {
		b.hook()
	}
	return n, err
}

// cancelAtEOF is a transport fault: when the $value body of message id has been
// read to its end, the run's context is cancelled, though the walk has nothing
// left to do but finish.
func cancelAtEOF(id string, cancel context.CancelFunc) func(http.RoundTripper) http.RoundTripper {
	return func(rt http.RoundTripper) http.RoundTripper {
		return roundTripFunc(func(r *http.Request) (*http.Response, error) {
			resp, err := rt.RoundTrip(r)
			if err == nil && strings.HasSuffix(r.URL.Path, "/messages/"+id+"/$value") {
				resp.Body = eofHook{ReadCloser: resp.Body, hook: cancel}
			}
			return resp, err
		})
	}
}

// lastItem is the new message of mailbox 2's Sent folder: the last request of the
// whole walk is its download.
const lastItem = "U2SENEW"

// covers: MA-540, R10, R17, R18, R21, S37, S46
// AI5: RunGraph's "cancelled" decision is about a walk that did not finish. An
// interrupt that lands after the last mailbox's walk completed cleanly leaves a
// complete run (nil, recorded as such, never cancelled); and a mailbox that failed
// with a transport's own context.Canceled stays a failure whose error matches no
// context error when the interrupt comes later, during the run's finish.
func TestRunGraphCancelAfterTheWalkDoesNotRewriteTheOutcome(t *testing.T) {
	addLast := func(s *graphtest.Server) { s.AddMessage("u2", "F_SE", lastItem) }
	t.Run("a clean walk then an interrupt", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		c := runSGCase(t, ctx, addLast, func(g *GraphOptions) { g.WrapTransport = cancelAtEOF(lastItem, cancel) })
		if ctx.Err() == nil {
			t.Fatal("the fault never cancelled the run's context: the case tests nothing")
		}
		if c.err != nil {
			t.Fatalf("a walk that finished cleanly before the interrupt returned %v", c.err)
		}
		if !archived(t, c.out, "u2new@x") || !archived(t, c.out, strings.ToLower(lastItem)+"@x") {
			t.Error("the walk's messages are not archived")
		}
		lr, _, _ := state.ReadLastRun(c.out)
		if lr.Status != state.RunOK {
			t.Errorf("a complete run is recorded %q, want %q", lr.Status, state.RunOK)
		}
		// AN2: the clean-run footer is written too: a walk the interrupt did not
		// cut short is a complete timeline a later gone-detection can trust.
		ev, herr := state.ReadHistory(filepath.Join(c.out, state.HistoryName))
		if herr != nil {
			t.Fatalf("read history log: %v", herr)
		}
		// runSGCase walks twice; the interrupted walk is the last run's.
		var lastRun int64
		for _, e := range ev {
			if e.Run > 0 && e.At != "" {
				lastRun = e.Run
			}
		}
		footers := 0
		for _, e := range ev {
			if lastRun > 0 && e.Run == lastRun && e.Completed != "" {
				footers++
			}
		}
		if footers != 1 {
			t.Errorf("the interrupted walk finished cleanly and wrote %d run footers, want 1", footers)
		}
	})
	t.Run("a failed mailbox then an interrupt", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		// From FI-S3 a transport's own context.Canceled on a $value is that item's
		// gap and no longer fails the mailbox (MA-540 case (6)); the mailbox that
		// still fails with one in its chain is one whose folder list the transport
		// cancelled.
		c := runSGCase(t, ctx,
			addLast,
			func(g *GraphOptions) {
				fail, stop := cancelOn("/users/u1/mailFolders"), cancelAtEOF(lastItem, cancel)
				g.WrapTransport = func(rt http.RoundTripper) http.RoundTripper { return stop(fail(rt)) }
			})
		if ctx.Err() == nil {
			t.Fatal("the fault never cancelled the run's context: the case tests nothing")
		}
		if c.err == nil || !strings.Contains(c.err.Error(), "list folders") {
			t.Errorf("the failed mailbox's error does not reach the run's: %v", c.err)
		}
		if errors.Is(c.err, context.Canceled) {
			t.Errorf("the failure matches a context error: %v", c.err)
		}
		lr, _, _ := state.ReadLastRun(c.out)
		if lr.Status != state.RunFailed {
			t.Errorf("the run is recorded %q, want failed", lr.Status)
		}
	})
}
