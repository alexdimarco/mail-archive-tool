package main

import (
	"context"
	"io"
	"log"
	"net/http"
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/graph/graphtest"
)

// roundTripFunc is an http.RoundTripper made of a function (fault injection).
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// covers: MA-540, R10, R17, R18, R21, S37
// graph's exit code for SG's stops on the shipped capture path. `graph` takes no
// base-URL setting, so it cannot reach the fake server; this test runs RunGraph
// on the same fake (graphtest, MA-540's Fixture and Stops, its scaled bounds)
// and passes the error through the verb's own mapping (graphOutcome, then
// exitCode). Every stop case exits 1, and so does case (6), a transport's own
// context.Canceled while the run's context is live; the parent-cancel twin is
// X1's handled interrupt and exits 0. Positive twin first: each healthy first
// run exits 0.
func TestGraphExitCodeForSGStops(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	exit := func(err error) int { return exitCode(graphOutcome(logger, err)) }
	run := func(t *testing.T, ctx context.Context, apply func(*graphtest.Server), wrap func(http.RoundTripper) http.RoundTripper) int {
		t.Helper()
		s := graphtest.NewServer()
		defer s.Close()
		s.Fixture()
		g := app.GraphOptions{
			Tenant: "t", ClientID: "c", ClientSecret: "s", Mailboxes: []string{"u1", "u2"},
			BaseURL: s.URL, TokenURL: s.URL + "/token",
			RequestTimeout: graphtest.RequestTimeout, MIMETimeout: graphtest.OverallCap,
			MIMEIdleTimeout: graphtest.IdleTimeout, MIMEHeaderTimeout: graphtest.HeaderTimeout,
			PageCap: graphtest.PageCap,
		}
		opts := app.Options{Out: t.TempDir(), Mode: export.Incremental}
		_, err := app.RunGraph(context.Background(), g, opts, logger)
		if code := exit(err); code != 0 {
			t.Fatalf("the healthy first run exits %d (%v), want 0", code, err)
		}
		apply(s)
		g.WrapTransport = wrap
		_, err = app.RunGraph(ctx, g, opts, logger)
		return exit(err)
	}

	for _, stop := range graphtest.Stops {
		t.Run(stop.Name, func(t *testing.T) {
			if code := run(t, context.Background(), stop.Apply, nil); code != 1 {
				t.Errorf("graph exits %d for the stop, want 1", code)
			}
		})
	}

	t.Run("a transport's own context.Canceled", func(t *testing.T) {
		cancelled := func(rt http.RoundTripper) http.RoundTripper {
			return roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if strings.HasSuffix(r.URL.Path, "/messages/"+graphtest.NewItem+"/$value") {
					return nil, context.Canceled
				}
				return rt.RoundTrip(r)
			})
		}
		add := func(s *graphtest.Server) { s.AddMessage("u1", "F_IN", graphtest.NewItem) }
		if code := run(t, context.Background(), add, cancelled); code != 1 {
			t.Errorf("graph exits %d when a transport returns context.Canceled, want 1 (it is no interrupt)", code)
		}
	})

	t.Run("the parent context cancelled during the download", func(t *testing.T) {
		started := make(chan struct{})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() {
			select {
			case <-started:
				cancel()
			case <-ctx.Done():
			}
		}()
		block := func(s *graphtest.Server) {
			s.AddMessage("u1", "F_IN", graphtest.NewItem)
			s.SetValue(graphtest.NewItem, graphtest.Block(started))
		}
		if code := run(t, ctx, block, nil); code != 0 {
			t.Errorf("graph exits %d for an operator's interrupt, want 0 (X1's handled interrupt)", code)
		}
	})
}
