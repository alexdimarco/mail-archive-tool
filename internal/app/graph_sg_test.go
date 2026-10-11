package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/graph"
	"mail-archive-tool/internal/graph/graphtest"
	"mail-archive-tool/internal/state"
)

// returnsWithin runs fn and fails the test when it has not returned within d:
// the time guard for a walk that, unguarded, would not end.
func returnsWithin(t *testing.T, d time.Duration, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("%s did not return within %v", what, d)
	}
}

// sgOptions are MA-540's options on the shared fake, with its scaled bounds:
// JSON request timeout 100 ms, $value idle 200 ms and header window 600 ms,
// page cap 5, and capture's overall cap (Config.MIMETimeout until S3) 2 s.
func sgOptions(s *graphtest.Server) GraphOptions {
	return GraphOptions{
		Tenant: "t", ClientID: "c", ClientSecret: "s", Mailboxes: []string{"u1", "u2"},
		BaseURL: s.URL, TokenURL: s.URL + "/token",
		RequestTimeout: graphtest.RequestTimeout, MIMETimeout: graphtest.OverallCap,
		MIMEIdleTimeout: graphtest.IdleTimeout, MIMEHeaderTimeout: graphtest.HeaderTimeout,
		PageCap: graphtest.PageCap,
	}
}

// sgCase is one MA-540 case run: its archive, the fake and the run's error.
type sgCase struct {
	out string
	srv *graphtest.Server
	err error
}

// runSGCase runs one MA-540 case: a healthy first run archives every message of
// the Fixture's two mailboxes; then apply makes mailbox u1 misbehave, u2 gains
// one new message (U2NEW), tune adjusts the options, and the case run goes
// under ctx and a time guard, with the fake's request log reset just before it.
func runSGCase(t *testing.T, ctx context.Context, apply func(*graphtest.Server), tune func(*GraphOptions)) sgCase {
	t.Helper()
	s := graphtest.NewServer()
	t.Cleanup(s.Close)
	s.Fixture()
	g := sgOptions(s)
	out := tmpDir(t)
	opts := Options{Out: out, Mode: export.Incremental}
	logger := log.New(io.Discard, "", 0)
	if res, err := RunGraph(context.Background(), g, opts, logger); err != nil || res.Stats.Exported != 12 {
		t.Fatalf("the healthy first run: exported %d, err %v; want 12, nil", res.Stats.Exported, err)
	}
	apply(s)
	s.AddMessage("u2", "F_IN", "U2NEW")
	if tune != nil {
		tune(&g)
	}
	s.ResetRequests()
	var err error
	returnsWithin(t, 20*time.Second, "the case run", func() {
		_, err = RunGraph(ctx, g, opts, logger)
	})
	return sgCase{out: out, srv: s, err: err}
}

// archived reports whether the archive holds a record for the identity.
func archived(t *testing.T, out, identity string) bool {
	t.Helper()
	m, err := state.Load(filepath.Join(out, ".mailarchive-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	for k := range m.All() {
		if strings.Contains(k, identity) {
			return true
		}
	}
	return false
}

// failedNotCancelled asserts what every MA-540 stop expects besides its typed
// error: no {k,gone} event and every record still Present (nothing was deleted
// at the source); mailbox 2 walked (its new message archived); the run recorded
// failed, never cancelled, with the attention sidecar written; and an error
// that matches neither context error, so the CLI exits 1, not 0.
func failedNotCancelled(t *testing.T, c sgCase) {
	t.Helper()
	if c.err == nil {
		t.Error("the case run neither failed mailbox 1 nor was partial")
	}
	if errors.Is(c.err, context.Canceled) || errors.Is(c.err, context.DeadlineExceeded) {
		t.Errorf("the run's error matches a context error, which recordRun and the CLI read as an interrupt: %v", c.err)
	}
	for _, ev := range mustHistory(t, c.out) {
		if ev.Gone {
			t.Errorf("a {k,gone} event was recorded for %s, which was never deleted at the source", ev.K)
		}
	}
	m, err := state.Load(filepath.Join(c.out, ".mailarchive-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	for k, r := range m.All() {
		if !r.Present {
			t.Errorf("record %q is no longer Present", k)
		}
	}
	if !archived(t, c.out, "u2new@x") {
		t.Error("mailbox 2 was not walked: its new message U2NEW is not archived")
	}
	lr, st, rerr := state.ReadLastRun(c.out)
	if rerr != nil || st != state.LastRunPresent || lr.Status != state.RunFailed {
		t.Errorf("the run is recorded %q, want failed (never cancelled): %+v", lr.Status, lr)
	}
	if _, err := os.Stat(filepath.Join(c.out, state.AttentionName)); err != nil {
		t.Errorf("the attention sidecar was not written: %v", err)
	}
}

// u1Gap is the one ledger entry mailbox 1's case run left: what a stop became.
func u1Gap(t *testing.T, c sgCase) state.CaptureGap {
	t.Helper()
	gaps := ledgerOf(t, c.out)
	if len(gaps) != 1 {
		t.Fatalf("the ledger holds %+v, want the one gap of the stop", gaps)
	}
	return gaps[0]
}

// covers: MA-540, R10, R17, R18, R21, S37, S46
// SG's stops on the shipped capture path, through RunGraph on the shared fake:
// two mailboxes, a healthy first run, then a case run in which mailbox 1 meets
// one stop (graphtest.Stops): (1) a repeating nextLink in Messages after its
// first page; (2) fresh nextLinks past the page cap in Messages, and in
// Folders; (3) a new item's $value that stalls mid-body past the idle window,
// sends no headers within the header window, or trickles past capture's
// overall cap. A stop in Folders fails mailbox 1 with the typed error naming it.
// From S3 on a stop in Messages or in a $value is MA-314's gap case instead: a
// folder-list-failed or a fetch-timeout gap whose detail is the typed error's
// text, naming the stop and, for (3), the message and the limit; the mailbox
// goes on, and the run is partial (before S3 each failed mailbox 1, today's
// error path). In every case the error matches no context error; no {k,gone} is
// recorded and every record stays Present; mailbox 2 is walked; the run is
// recorded failed with the sidecar written.
func TestRunGraphGuardStopsFailTheMailbox(t *testing.T) {
	wantListing := func(kind graph.ListingKind, limit int) func(*testing.T, error) {
		return func(t *testing.T, err error) {
			var le *graph.ListingError
			if !errors.As(err, &le) || le.Kind != kind || le.Limit != limit {
				t.Errorf("the stop is not the typed %s listing stop (limit %d): %v", kind, limit, err)
				return
			}
			if !strings.Contains(err.Error(), "did not terminate") {
				t.Errorf("the run's error does not name the stop: %v", err)
			}
		}
	}
	// A stop that is a gap: the entry's reason and scope, and what its detail names.
	type gapWant struct {
		reason, scope, folder string
		detail                []string
	}
	listingGap := func(detail ...string) gapWant {
		return gapWant{"folder-list-failed", state.GapScopeFolder, "Inbox", append([]string{"did not terminate"}, detail...)}
	}
	// A timeout's gap names the message, the limit, and which of the three limits
	// (the typed *graph.TimeoutError's Kind) fired.
	timeoutGap := func(phrase string, limit time.Duration) gapWant {
		return gapWant{"fetch-timeout", state.GapScopeItem, "Inbox", []string{graphtest.NewItem, limit.String(), phrase}}
	}
	failsMailbox := map[string]func(*testing.T, error){
		"page cap in Folders": wantListing(graph.ListingPageCap, graphtest.PageCap),
	}
	becomesGap := map[string]gapWant{
		"repeating nextLink in Messages": listingGap("already fetched"),
		"page cap in Messages":           listingGap(fmt.Sprintf("%d pages", graphtest.PageCap)),
		"$value idle":                    timeoutGap("the download's idle limit", graphtest.IdleTimeout),
		"$value header":                  timeoutGap("the download's response limit", graphtest.HeaderTimeout),
		"$value overall":                 timeoutGap("the download's overall limit", graphtest.OverallCap),
	}
	for _, stop := range graphtest.Stops {
		t.Run(stop.Name, func(t *testing.T) {
			c := runSGCase(t, context.Background(), stop.Apply, nil)
			if check, ok := failsMailbox[stop.Name]; ok {
				check(t, c.err)
			} else if want, ok := becomesGap[stop.Name]; ok {
				if !errors.Is(c.err, ErrPartial) {
					t.Errorf("the run returned %v, want ErrPartial: the stop is mailbox 1's gap, and the mailbox goes on", c.err)
				}
				g := u1Gap(t, c)
				if g.Reason != want.reason || g.Scope != want.scope || g.Class != state.ClassFillable || g.Folder != want.folder {
					t.Errorf("the stop's gap = %+v; want %s, %s scope, fillable, in %s", g, want.reason, want.scope, want.folder)
				}
				for _, s := range want.detail {
					if !strings.Contains(g.Detail, s) {
						t.Errorf("the gap's detail %q does not name %q", g.Detail, s)
					}
				}
			} else {
				t.Fatalf("no expectation for the stop %q", stop.Name)
			}
			failedNotCancelled(t, c)
		})
	}
}

// covers: MA-540, R10, R17, R18, R21, S37, S46
// MA-540's case (4): a new item's $value over the size cap fails mailbox 1 with
// the typed too-large error naming the item and the cap, with every other
// expectation of the stop cases.
func TestRunGraphOverCapValueFailsTheMailbox(t *testing.T) {
	t.Skip("MA-540: case (4) is open, not accepted: capture passes no size cap until Microsoft's documentation bounds a stored item (owner ruling RR6-OR1, ESC-8's safe interim; fault-isolation §4.8.6); it lands when a documented figure lets the cap reach capture")
}

// covers: MA-540, R10, R17, R18, R21, S37, S46
// MA-540's case (5): the page cap counts per loop. Both mailboxes, three folders
// each, make 14 listing pages in all, more than the scaled cap of 5, while no
// loop makes more than 3: the run completes with nil and archives every
// message.
func TestRunGraphPageCapCountsPerLoop(t *testing.T) {
	s := graphtest.NewServer()
	defer s.Close()
	s.Fixture()
	out := tmpDir(t)
	res, err := RunGraph(context.Background(), sgOptions(s), Options{Out: out, Mode: export.Incremental}, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatalf("a walk whose loops each stay under the page cap failed: %v", err)
	}
	if res.Stats.Exported != 12 {
		t.Errorf("exported %d, want all 12", res.Stats.Exported)
	}
	pages, longest := 0, 0
	for _, u := range []string{"u1", "u2"} {
		for _, f := range []string{"F_IN", "F_AR", "F_SE"} {
			n := s.Requests("/users/" + u + "/mailFolders/" + f + "/messages")
			pages += n
			longest = max(longest, n)
		}
		pages += s.Requests("/users/"+u+"/mailFolders") - s.Requests("/users/"+u+"/mailFolders/")
	}
	if pages <= 5 || longest >= 5 {
		t.Fatalf("the fixture does not test the scope: %d pages in all, %d in the longest loop (want > 5 and < 5)", pages, longest)
	}
}

// roundTripFunc is an http.RoundTripper made of a function (fault injection).
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// cancelOn is a transport fault: a request whose path ends in suffix fails with
// context.Canceled, though no context was cancelled.
func cancelOn(suffix string) func(http.RoundTripper) http.RoundTripper {
	return func(rt http.RoundTripper) http.RoundTripper {
		return roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if strings.HasSuffix(r.URL.Path, suffix) {
				return nil, context.Canceled
			}
			return rt.RoundTrip(r)
		})
	}
}

// cancelValue is cancelOn for the $value request of message id.
func cancelValue(id string) func(http.RoundTripper) http.RoundTripper {
	return cancelOn("/messages/" + id + "/$value")
}

// covers: MA-540, R10, R17, R18, R21, S37, S46
// MA-540's case (6), RunGraph's second guard: a transport that returns
// context.Canceled for mailbox 1's new $value while the run's own context is
// live is never read as an interrupt: the item is a fetch-transport gap naming
// it (§4.8.1 rule 4) and the mailbox goes on (before S3 it failed the mailbox);
// mailbox 2 is walked, the run is recorded failed, not cancelled, and RunGraph's
// error does not match context.Canceled, so the CLI exits 1.
func TestRunGraphTransportCancelIsNotAnInterrupt(t *testing.T) {
	c := runSGCase(t, context.Background(),
		func(s *graphtest.Server) { s.AddMessage("u1", "F_IN", graphtest.NewItem) },
		func(g *GraphOptions) { g.WrapTransport = cancelValue(graphtest.NewItem) })
	if !errors.Is(c.err, ErrPartial) {
		t.Errorf("the run returned %v, want ErrPartial", c.err)
	}
	if g := u1Gap(t, c); g.Reason != "fetch-transport" || g.Scope != state.GapScopeItem || !strings.Contains(g.Detail, graphtest.NewItem) {
		t.Errorf("the item's gap = %+v; want fetch-transport, item scope, naming the message whose download failed", g)
	}
	failedNotCancelled(t, c)
}

// valueAs adds the new item to mailbox 1's Inbox, its $value served by h.
func valueAs(h http.HandlerFunc) func(*graphtest.Server) {
	return func(s *graphtest.Server) {
		s.AddMessage("u1", "F_IN", graphtest.NewItem)
		s.SetValue(graphtest.NewItem, h)
	}
}

// covers: MA-540, R10, R17, R18, R21, S37, S46
// MA-540's twins. Headers that arrive after the scaled JSON request timeout but
// inside the $value header window: the item is archived (the $value request is
// not on the JSON requests' transport). A 503 whose Retry-After sleep is longer
// than the header window but ends inside capture's overall cap, then a prompt
// 200: the item is archived (the header timer times each attempt and does not
// run in the sleep). The parent context cancelled during mailbox 1's download:
// RunGraph returns context.Canceled, the run is recorded cancelled (X1's
// handled interrupt, exit 0), and mailbox 2 is not walked.
func TestRunGraphSGTwins(t *testing.T) {
	t.Run("headers after the JSON timeout, inside the header window", func(t *testing.T) {
		c := runSGCase(t, context.Background(), valueAs(graphtest.DelayHeaders(300*time.Millisecond, graphtest.Body(graphtest.NewItem))), nil)
		if c.err != nil {
			t.Fatalf("the run failed: %v", c.err)
		}
		if !archived(t, c.out, "u1new@x") {
			t.Error("the item whose headers came after the JSON timeout was not archived")
		}
	})
	t.Run("a Retry-After sleep longer than the header window", func(t *testing.T) {
		c := runSGCase(t, context.Background(), valueAs(graphtest.ThrottleOnce("1", graphtest.Body(graphtest.NewItem))), nil)
		if c.err != nil {
			t.Fatalf("the run failed: %v", c.err)
		}
		if !archived(t, c.out, "u1new@x") {
			t.Error("the throttled item was not archived after its Retry-After sleep")
		}
	})
	t.Run("the parent context cancelled during mailbox 1's download", func(t *testing.T) {
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
		c := runSGCase(t, ctx, valueAs(graphtest.Block(started)), nil)
		if !errors.Is(c.err, context.Canceled) {
			t.Fatalf("a cancelled run did not return context.Canceled: %v", c.err)
		}
		lr, _, _ := state.ReadLastRun(c.out)
		if lr.Status != state.RunCancelled {
			t.Errorf("the cancelled run is recorded %q, want cancelled", lr.Status)
		}
		if n := c.srv.Requests("/users/u2/"); n != 0 {
			t.Errorf("mailbox 2 was walked after the cancel (%d requests)", n)
		}
	})
}

// chainMailbox serves mailbox u1 as a single chain of folders levels deep (L1 at
// the top), with one message, TOP, in L1. (A message 300 folders down cannot be
// written: its path is longer than a file system allows, the path-length issue
// fault isolation's N1 leaves to the path-portability work.)
func chainMailbox(levels int) *httptest.Server {
	mux := http.NewServeMux()
	j := func(w http.ResponseWriter, s string) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, s)
	}
	folder := func(k int) string {
		n := 0
		if k < levels {
			n = 1
		}
		return `{"id":"L` + strconv.Itoa(k) + `","displayName":"L` + strconv.Itoa(k) + `","childFolderCount":` + strconv.Itoa(n) + `}`
	}
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		j(w, `{"access_token":"t","token_type":"Bearer","expires_in":3600}`)
	})
	mux.HandleFunc("/users/u1/mailFolders", func(w http.ResponseWriter, r *http.Request) {
		j(w, `{"value":[`+folder(1)+`]}`)
	})
	mux.HandleFunc("/users/u1/mailFolders/", func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/users/u1/mailFolders/")
		id, sub, _ := strings.Cut(rest, "/")
		k, err := strconv.Atoi(strings.TrimPrefix(id, "L"))
		switch {
		case err != nil:
			http.NotFound(w, r) // the well-known Deleted Items / Junk Email resolution
		case sub == "childFolders" && k < levels:
			j(w, `{"value":[`+folder(k+1)+`]}`)
		case sub == "messages" && k == 1:
			j(w, `{"value":[{"id":"TOP","internetMessageId":"<top@x>","subject":"subj-TOP","receivedDateTime":"2025-03-01T09:00:00Z"}]}`)
		default:
			j(w, `{"value":[]}`)
		}
	})
	mux.HandleFunc("/users/u1/messages/", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, graphtest.Body("TOP"))
	})
	return httptest.NewServer(mux)
}

// covers: MA-541, R10, R17
// The folder depth cap on the shipped capture path: a mailbox whose folders form
// a chain of fresh ids 301 levels deep (one past the cap) fails, under a time
// guard, with the typed depth error naming the cap; the run is recorded failed,
// never cancelled, and its error matches no context error. Twin first: a chain
// exactly at the cap (300 levels, /mailFolders counted as level 1) is walked to
// the bottom and its message archived, with no error.
func TestRunGraphFolderDepthFailsTheMailbox(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	run := func(levels int) (string, Result, error) {
		srv := chainMailbox(levels)
		defer srv.Close()
		out := tmpDir(t)
		g := GraphOptions{Tenant: "t", ClientID: "c", ClientSecret: "s", Mailboxes: []string{"u1"},
			BaseURL: srv.URL, TokenURL: srv.URL + "/token"}
		var res Result
		var err error
		returnsWithin(t, 30*time.Second, "RunGraph over a "+strconv.Itoa(levels)+"-level chain", func() {
			res, err = RunGraph(context.Background(), g, Options{Out: out, Mode: export.Incremental}, logger)
		})
		return out, res, err
	}

	_, res, err := run(300)
	if err != nil {
		t.Fatalf("a mailbox exactly at the 300-level cap failed: %v", err)
	}
	if res.Stats.Exported != 1 {
		t.Errorf("the at-cap chain archived %d messages, want its 1 (TOP)", res.Stats.Exported)
	}

	out, _, err := run(301)
	var de *graph.DepthError
	if !errors.As(err, &de) || de.Limit != 300 {
		t.Fatalf("a mailbox 301 levels deep did not fail with the typed depth error at 300: %v", err)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("the depth stop reached the run as a context error: %v", err)
	}
	if !strings.Contains(err.Error(), "300") {
		t.Errorf("the run's error does not name the cap: %v", err)
	}
	lr, st, rerr := state.ReadLastRun(out)
	if rerr != nil || st != state.LastRunPresent || lr.Status != state.RunFailed {
		t.Errorf("the over-depth run is not recorded failed: %+v (state %v, err %v)", lr, st, rerr)
	}
}
