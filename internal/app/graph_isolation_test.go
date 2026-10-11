package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/graph"
	"mail-archive-tool/internal/graph/graphtest"
	"mail-archive-tool/internal/health"
	"mail-archive-tool/internal/state"
)

// The rows FI-S3 lands (fault-isolation §9, §10): Graph isolation, on the shared
// fake (graphtest) through RunGraph.

// isoServer is MA-310's mailbox u1: an Inbox of [A1,A2,POISON,A3,A4] (the F9 PoC)
// and a Sent of [B1,B2], each listed in one page. POISON's $value answers 500
// ErrorMimeContentConversionFailed when poison is true; without it POISON is an
// ordinary message (the twin).
func isoServer(poison bool) *graphtest.Server {
	s := graphtest.NewServer()
	s.AddFolder("u1", "F_IN", "Inbox", 10)
	s.AddFolder("u1", "F_SE", "Sent", 10)
	for _, id := range []string{"A1", "A2", "POISON", "A3", "A4"} {
		s.AddMessage("u1", "F_IN", id)
	}
	s.AddMessage("u1", "F_SE", "B1")
	s.AddMessage("u1", "F_SE", "B2")
	if poison {
		s.SetValue("POISON", graphtest.Fail(500, "ErrorMimeContentConversionFailed"))
	}
	return s
}

// isoOptions are RunGraph's options for mailbox u1 of s.
func isoOptions(s *graphtest.Server) GraphOptions {
	return GraphOptions{Tenant: "t", ClientID: "c", ClientSecret: "s", Mailboxes: []string{"u1"},
		BaseURL: s.URL, TokenURL: s.URL + "/token"}
}

// isoRun is one incremental RunGraph of g into out.
func isoRun(g GraphOptions, out string) (Result, error) {
	return RunGraph(context.Background(), g, Options{Out: out, Mode: export.Incremental}, log.New(io.Discard, "", 0))
}

// graphGaps are the ledger's entries of out, in the ledger's order.
func graphGaps(t *testing.T, out string) []state.CaptureGap { return ledgerOf(t, out) }

// covers: MA-310, R1, R10, R17, S46
// F9's PoC through RunGraph: one message whose $value answers 500
// ErrorMimeContentConversionFailed costs exactly that message. A1..A4 and B1, B2
// are all archived; the run is partial (ErrPartial); the poison has no manifest
// record and no identity-index hit (H3: a gap is never a record); one fetch-failed
// fillable gap carries its subject and received date. Twin first: the same mailbox
// with a healthy POISON archives everything, returns nil and holds no gap.
func TestGraphPoisonItemCostsOnlyThatItem(t *testing.T) {
	twin := isoServer(false)
	defer twin.Close()
	twinOut := tmpDir(t)
	if res, err := isoRun(isoOptions(twin), twinOut); err != nil || res.Stats.Exported != 7 || len(graphGaps(t, twinOut)) != 0 {
		t.Fatalf("the twin (no poison): exported %d, err %v, gaps %d; want 7, nil, 0", res.Stats.Exported, err, len(graphGaps(t, twinOut)))
	}

	s := isoServer(true)
	defer s.Close()
	out := tmpDir(t)
	res, err := isoRun(isoOptions(s), out)
	if !errors.Is(err, ErrPartial) {
		t.Fatalf("the run with a poison message returned %v, want ErrPartial", err)
	}
	if res.Stats.Exported != 6 {
		t.Errorf("exported %d messages, want the 6 healthy ones (A1..A4, B1, B2)", res.Stats.Exported)
	}
	for _, id := range []string{"a1", "a2", "a3", "a4", "b1", "b2"} {
		if !archived(t, out, id+"@x") {
			t.Errorf("%s was not archived: one poison message stopped the walk", id)
		}
	}
	if archived(t, out, "poison@x") {
		t.Error("the poison has a manifest record: a gap must never be a record (H3)")
	}
	if refs := loadArchive(t, out).KeysForIdentity("mid:poison@x"); len(refs) != 0 {
		t.Errorf("the poison has identity-index entries %v", refs)
	}
	gaps := graphGaps(t, out)
	if len(gaps) != 1 {
		t.Fatalf("the ledger holds %d gaps %+v, want exactly the poison's", len(gaps), gaps)
	}
	g := gaps[0]
	if g.Reason != "fetch-failed" || g.Class != state.ClassFillable || g.Scope != state.GapScopeItem || g.Locator != "graph:POISON" ||
		g.Subject != "subj-POISON" || g.Received != "2025-03-01T09:00:00Z" || g.Folder != "Inbox" ||
		!strings.Contains(g.Detail, "ErrorMimeContentConversionFailed") {
		t.Errorf("the poison's gap = %+v; want fetch-failed, fillable, item, graph:POISON, its subject and received date, Inbox, naming the Graph code", g)
	}
	if time.Since(g.LastRun) > time.Hour {
		t.Errorf("the gap was not stamped by this run: %+v", g)
	}
	assure.Reached(t, res.Gaps.Observed, "the gaps the run observed")
}

// mailboxToken is the store token a Graph run of mailbox gives its store: the
// token depends on the mailbox address, so a test can seed the archive under it
// before the run's own first.
func mailboxToken(t *testing.T, out, mailbox string) string {
	t.Helper()
	m := loadArchive(t, out)
	tok := m.Token(state.MailboxSourceID(mailbox), mailbox)
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	return tok
}

// covers: MA-311, R1, R17, R2, S46
// The ledger is inert on Graph (P5): a gap is attempted again by every run that
// reaches its item, so run 2 over a poison that is still poison fetches it again,
// and run 3 over a healed one downloads it once, archives it, closes the gap as
// recovered (Recovered 1) and returns nil. And a record seeded for the poison as
// Terminal (the H3 anti-pattern, built in the test) is never re-fetched: the
// message-ID membership skip would pass it over for ever, which is why a gap is
// never a record.
func TestGraphLedgerIsInertAndHeals(t *testing.T) {
	s := isoServer(true)
	defer s.Close()
	out := tmpDir(t)
	g := isoOptions(s)
	if _, err := isoRun(g, out); !errors.Is(err, ErrPartial) {
		t.Fatalf("run 1 returned %v, want ErrPartial", err)
	}
	if n := s.Requests("/users/u1/messages/POISON/"); n != 1 {
		t.Fatalf("run 1 fetched POISON %d times, want 1", n)
	}
	s.ResetRequests()
	if _, err := isoRun(g, out); !errors.Is(err, ErrPartial) {
		t.Fatalf("run 2 returned %v, want ErrPartial", err)
	}
	if n := s.Requests("/users/u1/messages/POISON/"); n != 1 {
		t.Errorf("run 2 fetched POISON %d times, want 1: a gap is attempted by every run that reaches its item", n)
	}
	if n := s.Requests("/users/u1/messages/A1/"); n != 0 {
		t.Errorf("run 2 fetched the archived A1 %d times, want 0", n)
	}

	s.SetValue("POISON", nil) // Microsoft serves it now
	s.ResetRequests()
	res, err := isoRun(g, out)
	if err != nil {
		t.Fatalf("run 3 over the healed poison returned %v, want nil", err)
	}
	if n := s.Requests("/users/u1/messages/POISON/"); n != 1 {
		t.Errorf("run 3 fetched the healed POISON %d times, want once", n)
	}
	if !archived(t, out, "poison@x") {
		t.Error("the healed POISON was not archived")
	}
	if gaps := graphGaps(t, out); len(gaps) != 0 {
		t.Errorf("the gap did not close: %+v", gaps)
	}
	if res.Gaps.Recovered != 1 || res.Gaps.ClosedGone != 0 {
		t.Errorf("Recovered %d, ClosedGone %d; want 1 and 0 (the item was captured)", res.Gaps.Recovered, res.Gaps.ClosedGone)
	}

	// The H3 anti-pattern: a Terminal record for the poison, constructed here.
	s2 := isoServer(true)
	defer s2.Close()
	out2 := tmpDir(t)
	tok := mailboxToken(t, out2, "u1")
	m := loadArchive(t, out2)
	m.Add(state.LiveKey(tok, "mid:poison@x"), state.Record{Path: tok + "/Inbox/poison.html", Folder: "Inbox", FirstFolder: "Inbox",
		Terminal: []string{state.MissingBody}, Present: true, Subject: "subj-POISON"})
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := isoRun(isoOptions(s2), out2); err != nil {
		t.Fatalf("the run over a Terminal record for the poison returned %v", err)
	}
	if n := s2.Requests("/users/u1/messages/POISON/"); n != 0 {
		t.Errorf("a Terminal record for the poison was fetched %d times: it must be skipped for ever, which is why a gap is never a record", n)
	}
}

// covers: MA-313, MA-310, R5, R12, R18, R1, R10, R17, S46
// classifyFetch sorts every fault of a Graph request for one message by the table
// of §4.8.1: a cancelled walk, a refused sign-in (401, a failed token refresh) and
// a 403 are fatal and make no gap; a 404 (or ErrorItemNotFound) is
// vanished-at-fetch; a throttle or gateway error, no response and a dropped
// connection are fetch-transport, a timer of MIMETo's fetch-timeout, both counted
// by the breaker; an over-cap body is a terminal fetch-too-large; anything else,
// 500 with any Graph code included, is fetch-failed, which the breaker never
// counts. A refused redirect is an answer, not the network failing.
func TestClassifyFetchSortsEveryFault(t *testing.T) {
	live := context.Background()
	dead, cancel := context.WithCancel(context.Background())
	cancel()
	netErr := &url.Error{Op: "Get", URL: "https://graph.example/x", Err: io.ErrUnexpectedEOF}
	refresh := &url.Error{Op: "Get", URL: "https://graph.example/x", Err: &oauth2.RetrieveError{Response: &http.Response{StatusCode: 401}, ErrorCode: "invalid_client"}}
	redirect := &url.Error{Op: "Get", URL: "https://graph.example/x", Err: &graph.RedirectError{Host: "evil.example", Hops: 1}}
	status := func(code int, graphCode string) error {
		return fmt.Errorf("fetch: %w", &graph.StatusError{Code: code, GraphCode: graphCode, Text: "body"})
	}
	for _, tc := range []struct {
		name            string
		err             error
		walk            context.Context
		reason, class   string
		fatal, transprt bool
	}{
		{"cancelled walk", netErr, dead, "", "", true, false},
		{"401", status(401, "InvalidAuthenticationToken"), live, "", "", true, false},
		{"403 (owner ruling OR7)", status(403, "ErrorAccessDenied"), live, "", "", true, false},
		{"a failed token refresh", refresh, live, "", "", true, false},
		{"404", status(404, ""), live, "vanished-at-fetch", state.ClassFillable, false, false},
		{"ErrorItemNotFound on a 500", status(500, "ErrorItemNotFound"), live, "vanished-at-fetch", state.ClassFillable, false, false},
		{"429", status(429, ""), live, "fetch-transport", state.ClassFillable, false, true},
		{"502", status(502, ""), live, "fetch-transport", state.ClassFillable, false, true},
		{"503", status(503, ""), live, "fetch-transport", state.ClassFillable, false, true},
		{"504", status(504, ""), live, "fetch-transport", state.ClassFillable, false, true},
		{"no response", netErr, live, "fetch-transport", state.ClassFillable, false, true},
		{"idle timeout", &graph.TimeoutError{Kind: graph.TimeoutIdle, Limit: time.Second, MessageID: "M"}, live, "fetch-timeout", state.ClassFillable, false, true},
		{"header timeout", &graph.TimeoutError{Kind: graph.TimeoutHeader, Limit: time.Second, MessageID: "M"}, live, "fetch-timeout", state.ClassFillable, false, true},
		{"overall timeout", &graph.TimeoutError{Kind: graph.TimeoutOverall, Limit: time.Second, MessageID: "M"}, live, "fetch-timeout", state.ClassFillable, false, true},
		{"over the size cap", &graph.TooLargeError{MessageID: "M", Limit: 1, Declared: 2}, live, "fetch-too-large", state.ClassTerminal, false, false},
		{"500 with a Graph code", status(500, "ErrorMimeContentConversionFailed"), live, "fetch-failed", state.ClassFillable, false, false},
		{"another 4xx", status(400, "ErrorInvalidIdMalformed"), live, "fetch-failed", state.ClassFillable, false, false},
		{"a refused redirect", redirect, live, "fetch-failed", state.ClassFillable, false, false},
	} {
		reason, class, fatal, transport := classifyFetch(tc.err, tc.walk)
		if reason != tc.reason || class != tc.class || fatal != tc.fatal || transport != tc.transprt {
			t.Errorf("%s: classifyFetch = (%q, %q, fatal %v, transport %v); want (%q, %q, %v, %v)",
				tc.name, reason, class, fatal, transport, tc.reason, tc.class, tc.fatal, tc.transprt)
		}
	}
}

// refused401 is a token endpoint that has stopped accepting the client secret.
func refused401(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	io.WriteString(w, `{"error":"invalid_client","error_description":"AADSTS7000215: Invalid client secret provided."}`)
}

// covers: MA-313, R5, R12, R18, S46
// A fault of the mailbox itself ends the mailbox and makes no gap (§4.8.1, P8):
// a cancel of the run's own context mid-download (context.Canceled, last-run
// cancelled, nothing for that item); a $value 401, and a 403 (owner ruling OR7),
// each a failed mailbox whose error names the status and which leaves no more than
// one gap; and a token endpoint that starts refusing the client secret mid-walk
// (401 invalid_client): the mailbox fails with the auth wording `status` reads for
// its auth_remedy note, and no gap at all (FC11). The messages before the fault
// stay archived.
func TestGraphAuthAndCancelEndTheMailboxWithoutAGap(t *testing.T) {
	t.Run("cancel mid-download", func(t *testing.T) {
		s := isoServer(false)
		defer s.Close()
		started := make(chan struct{})
		s.SetValue("POISON", graphtest.Block(started))
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() {
			select {
			case <-started:
				cancel()
			case <-ctx.Done():
			}
		}()
		out := tmpDir(t)
		_, err := RunGraph(ctx, isoOptions(s), Options{Out: out, Mode: export.Incremental}, log.New(io.Discard, "", 0))
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("a cancelled run returned %v, want context.Canceled", err)
		}
		if gaps := graphGaps(t, out); len(gaps) != 0 {
			t.Errorf("the cancel left gaps %+v: an interrupt is no item's fault", gaps)
		}
		if lr, _, _ := state.ReadLastRun(out); lr.Status != state.RunCancelled {
			t.Errorf("the run is recorded %q, want cancelled", lr.Status)
		}
	})
	for _, tc := range []struct {
		name   string
		status int
		code   string
	}{{"$value 401", 401, "InvalidAuthenticationToken"}, {"$value 403", 403, "ErrorAccessDenied"}} {
		t.Run(tc.name, func(t *testing.T) {
			s := isoServer(false)
			defer s.Close()
			s.SetValue("POISON", graphtest.Fail(tc.status, tc.code))
			out := tmpDir(t)
			_, err := isoRun(isoOptions(s), out)
			if err == nil || errors.Is(err, ErrPartial) || !strings.Contains(err.Error(), strconv.Itoa(tc.status)) || !strings.Contains(err.Error(), "POISON") {
				t.Fatalf("the run returned %v; want the mailbox's failure naming the status %d and the message", err, tc.status)
			}
			if gaps := graphGaps(t, out); len(gaps) > 1 {
				t.Errorf("the refusal left %d gaps %+v, want at most one", len(gaps), gaps)
			}
			if !archived(t, out, "a2@x") || archived(t, out, "a3@x") || archived(t, out, "b1@x") {
				t.Errorf("A2 %v, A3 %v, B1 %v; want what came before the refusal archived and the rest of the mailbox not read", archived(t, out, "a2@x"), archived(t, out, "a3@x"), archived(t, out, "b1@x"))
			}
		})
	}
	t.Run("a token refresh refused mid-walk", func(t *testing.T) {
		s := isoServer(false)
		defer s.Close()
		s.ShortTokens(true)
		// A2's body is served, and from then on the endpoint refuses the secret.
		s.SetValue("A2", func(w http.ResponseWriter, r *http.Request) {
			s.SetToken(refused401)
			io.WriteString(w, graphtest.Body("A2"))
		})
		out := tmpDir(t)
		_, err := isoRun(isoOptions(s), out)
		if err == nil || errors.Is(err, ErrPartial) || !strings.Contains(err.Error(), "invalid_client") {
			t.Fatalf("the run returned %v; want the mailbox's auth failure naming invalid_client", err)
		}
		if gaps := graphGaps(t, out); len(gaps) != 0 {
			t.Errorf("a refused sign-in left gaps %+v, want none (FC11)", gaps)
		}
		if !archived(t, out, "a2@x") || archived(t, out, "poison@x") {
			t.Errorf("A2 %v, POISON %v; want A2 archived and the walk stopped at the next download", archived(t, out, "a2@x"), archived(t, out, "poison@x"))
		}
		rep := health.Assess(health.Gather(out, ""), time.Now())
		if !slices.Contains(rep.Codes, "auth_remedy") {
			t.Errorf("status's codes %v lack auth_remedy for a refused sign-in", rep.Codes)
		}
	})
}

// covers: MA-313, R5, R12, R18, S46
func TestGraphBreakerAndRetryAfterClamp(t *testing.T) {
	t.Skip("MA-313: the rate and time breaker, its control probe, the known-poison exemption, the run-level breaker and the Retry-After clamp (FC10); lands in FI-S3")
}

// unwritableServer is MA-501's mailbox u1: folders A, B and C of two, three and
// two messages, each listed in one page.
func unwritableServer() *graphtest.Server {
	s := graphtest.NewServer()
	for _, f := range []struct {
		id   string
		msgs []string
	}{{"A", []string{"A1", "A2"}}, {"B", []string{"B1", "B2", "B3"}}, {"C", []string{"C1", "C2"}}} {
		s.AddFolder("u1", f.id, f.id, 10)
		for _, id := range f.msgs {
			s.AddMessage("u1", f.id, id)
		}
	}
	return s
}

// covers: MA-501, R1, R6, R10, R21, S46
// N1 on Graph (the fixture of MA-320 through the fake server): a regular file at
// out/<token>/B makes B's output directory impossible to create. A and C are
// archived, B is one folder-unwritable fillable gap whose Skipped counts every
// message it could not write (the first, which was tried, and the two after it,
// which were NOT downloaded), the run is partial, and removing the file lets run 2
// archive B and close the gap as recovered. Twin first: the same mailbox without
// the file archives everything and returns nil.
func TestGraphUnwritableFolderCostsOneFolder(t *testing.T) {
	twin := unwritableServer()
	defer twin.Close()
	if res, err := isoRun(isoOptions(twin), tmpDir(t)); err != nil || res.Stats.Exported != 7 {
		t.Fatalf("the twin: exported %d, err %v; want 7, nil", res.Stats.Exported, err)
	}

	s := unwritableServer()
	defer s.Close()
	out := tmpDir(t)
	tok := mailboxToken(t, out, "u1")
	blocker := writeData(t, filepath.Join(out, tok), "B", []byte("not a directory"))
	res, err := isoRun(isoOptions(s), out)
	if !errors.Is(err, ErrPartial) {
		t.Fatalf("the run returned %v, want ErrPartial", err)
	}
	if res.Stats.Exported != 4 {
		t.Errorf("exported %d, want A's two and C's two", res.Stats.Exported)
	}
	for _, id := range []string{"b1", "b2", "b3"} {
		if archived(t, out, id+"@x") {
			t.Errorf("%s is archived under a folder that cannot be written", id)
		}
	}
	for _, id := range []string{"B2", "B3"} {
		if n := s.Requests("/users/u1/messages/" + id + "/"); n != 0 {
			t.Errorf("%s was downloaded %d times though its folder could not be written: it is counted, not fetched", id, n)
		}
	}
	gaps := graphGaps(t, out)
	if len(gaps) != 1 {
		t.Fatalf("the ledger holds %+v, want B's one folder gap", gaps)
	}
	if g := gaps[0]; g.Reason != "folder-unwritable" || g.Class != state.ClassFillable || g.Scope != state.GapScopeFolder || g.Folder != "B" || g.Skipped != 3 {
		t.Errorf("B's gap = %+v; want folder-unwritable, fillable, folder scope, Folder B, Skipped 3", g)
	}

	if err := os.Remove(blocker); err != nil {
		t.Fatal(err)
	}
	res, err = isoRun(isoOptions(s), out)
	if err != nil {
		t.Fatalf("run 2 with the file removed returned %v, want nil", err)
	}
	if res.Stats.Exported != 3 || res.Gaps.Recovered != 1 || len(graphGaps(t, out)) != 0 {
		t.Errorf("run 2: exported %d, Recovered %d, gaps %+v; want B's three archived and the gap closed as recovered", res.Stats.Exported, res.Gaps.Recovered, graphGaps(t, out))
	}
}

// covers: MA-501, R1, R6, R10, R21, S46
// Fast-path skips in an unwritable folder still stamp: B is archived whole, then
// its directory is replaced by a file and two new messages arrive in it. The
// archived B1..B3 are skipped by Message-ID membership as ever and stamped seen by
// this run, the new B4 is the folder's gap, and the new B5 is counted under it
// without a download.
func TestGraphUnwritableFolderStillStampsArchivedMessages(t *testing.T) {
	s := unwritableServer()
	defer s.Close()
	out := tmpDir(t)
	if _, err := isoRun(isoOptions(s), out); err != nil {
		t.Fatalf("run 1: %v", err)
	}
	tok := mailboxToken(t, out, "u1")
	if err := os.RemoveAll(filepath.Join(out, tok, "B")); err != nil {
		t.Fatal(err)
	}
	writeData(t, filepath.Join(out, tok), "B", []byte("not a directory"))
	s.AddMessage("u1", "B", "B4")
	s.AddMessage("u1", "B", "B5")
	s.ResetRequests()
	if _, err := isoRun(isoOptions(s), out); !errors.Is(err, ErrPartial) {
		t.Fatalf("run 2 returned %v, want ErrPartial", err)
	}
	if n := s.Requests("/users/u1/messages/B5/"); n != 0 {
		t.Errorf("B5 was downloaded %d times though B could not be written", n)
	}
	last, _, _ := state.ReadLastRun(out)
	for _, id := range []string{"b1@x", "b2@x", "b3@x"} {
		_, rec := recordForID(t, out, id)
		if !rec.Present || !rec.LastSeen.Equal(last.Started) {
			t.Errorf("%s: Present %v, LastSeen %v; want it present and stamped by this run (%v)", id, rec.Present, rec.LastSeen, last.Started)
		}
	}
}

// covers: MA-502, R1, R17, S46
// Inertness on Graph (P5): a manifest seeded with a gap whose graph: locator
// matches a healthy item does not stop its capture, whatever the gap says (here a
// terminal fetch-too-large): the item is downloaded and archived, and the gap
// closes as recovered. Twin first: the same run without the seed.
func TestGraphLedgerNeverStopsACapture(t *testing.T) {
	s := isoServer(false)
	defer s.Close()
	twinOut := tmpDir(t)
	if res, err := isoRun(isoOptions(s), twinOut); err != nil || res.Stats.Exported != 7 {
		t.Fatalf("the twin: exported %d, err %v; want 7, nil", res.Stats.Exported, err)
	}

	out := tmpDir(t)
	tok := mailboxToken(t, out, "u1")
	m := loadArchive(t, out)
	seededAt := time.Now().UTC().Add(-48 * time.Hour)
	m.ObserveGap(state.CaptureGap{Token: tok, Folder: "Inbox", Scope: state.GapScopeItem, Locator: "graph:A2", Reason: "fetch-too-large",
		Class: state.ClassTerminal, Subject: "subj-A2", Detail: "seeded"}, seededAt)
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	s.ResetRequests()
	res, err := isoRun(isoOptions(s), out)
	if err != nil {
		t.Fatalf("the run over a seeded gap returned %v, want nil", err)
	}
	if n := s.Requests("/users/u1/messages/A2/"); n != 1 {
		t.Errorf("A2 was fetched %d times, want 1: a seeded gap must not stop its capture", n)
	}
	if !archived(t, out, "a2@x") || res.Stats.Exported != 7 {
		t.Errorf("A2 archived: %v, exported %d; want it archived with the other six", archived(t, out, "a2@x"), res.Stats.Exported)
	}
	if gaps := graphGaps(t, out); len(gaps) != 0 || res.Gaps.Recovered != 1 {
		t.Errorf("gaps %+v, Recovered %d; want the gap closed as recovered", gaps, res.Gaps.Recovered)
	}
}

// covers: MA-318, R1, R3, R7, R10, S46
// An Export panic on the Graph path is never log-only either (F33): it increments
// Stats.ExportPanics and records a terminal export-panic gap at the message's
// graph: locator, the mailbox goes on with the next message and the run ends
// partial, where it used to log and return nil (exit 0).
func TestGraphExportPanicIsALedgerGapNeverLogOnly(t *testing.T) {
	s := isoServer(false)
	defer s.Close()
	seen := 0
	exporterSetup = func(e *export.Exporter) {
		e.BeforeWrite = func(path string) error {
			if strings.HasSuffix(path, ".html") {
				if seen++; seen == 3 {
					panic("renderer fault")
				}
			}
			return nil
		}
	}
	t.Cleanup(func() { exporterSetup = nil })
	out := tmpDir(t)
	res, err := isoRun(isoOptions(s), out)
	if !errors.Is(err, ErrPartial) {
		t.Fatalf("the run returned %v, want ErrPartial", err)
	}
	if res.Stats.ExportPanics != 1 || res.Stats.Exported != 6 {
		t.Errorf("panics %d, exported %d; want 1 and 6: the walk goes on", res.Stats.ExportPanics, res.Stats.Exported)
	}
	g := graphGaps(t, out)
	if len(g) != 1 || g[0].Reason != "export-panic" || g[0].Class != state.ClassTerminal || g[0].Locator != "graph:POISON" || !strings.Contains(g[0].Detail, "renderer fault") {
		t.Errorf("ledger %+v; want one terminal export-panic gap at graph:POISON (the third message written)", g)
	}
}

// covers: MA-501, R1, R6, R10, R21, S46
// A write that fails for one message (an access error: the archive is still
// there) is that message's export-failed fillable gap at its graph: locator, and
// the mailbox goes on; the next run writes it and the gap closes as recovered. A
// full drive is the run's: the mailbox ends with the drive-full refusal (the
// words of §7.5), no gap, and the next mailbox is not read.
func TestGraphWriteFaultsCostOneMessageOrTheRun(t *testing.T) {
	t.Run("one message's write", func(t *testing.T) {
		s := isoServer(false)
		defer s.Close()
		failWrites(t, func(n int, path string) error {
			if n == 2 {
				return pathErr(path, syscall.EACCES)
			}
			return nil
		})
		out := tmpDir(t)
		res, err := isoRun(isoOptions(s), out)
		if !errors.Is(err, ErrPartial) || res.Stats.Exported != 6 {
			t.Fatalf("run 1: err %v, exported %d; want ErrPartial and 6", err, res.Stats.Exported)
		}
		g := graphGaps(t, out)
		if len(g) != 1 || g[0].Reason != "export-failed" || g[0].Class != state.ClassFillable || g[0].Locator != "graph:A2" {
			t.Fatalf("ledger %+v; want one fillable export-failed gap at graph:A2", g)
		}
		exporterSetup = nil
		res, err = isoRun(isoOptions(s), out)
		if err != nil || res.Gaps.Recovered != 1 || len(graphGaps(t, out)) != 0 || !archived(t, out, "a2@x") {
			t.Errorf("run 2: err %v, Recovered %d, gaps %+v, A2 archived %v; want nil, 1, none, true", err, res.Gaps.Recovered, graphGaps(t, out), archived(t, out, "a2@x"))
		}
	})
	t.Run("a full drive", func(t *testing.T) {
		s := graphtest.NewServer()
		defer s.Close()
		s.Fixture()
		failWrites(t, func(n int, path string) error {
			if n == 2 {
				return pathErr(path, syscall.ENOSPC)
			}
			return nil
		})
		g := sgOptions(s)
		out := tmpDir(t)
		_, err := RunGraph(context.Background(), g, Options{Out: out, Mode: export.Incremental}, log.New(io.Discard, "", 0))
		var fatal *RunFatalError
		if !errors.As(err, &fatal) || !strings.Contains(err.Error(), "the archive drive is full") {
			t.Fatalf("the run returned %v, want the drive-full refusal", err)
		}
		if gaps := graphGaps(t, out); len(gaps) != 0 {
			t.Errorf("a full drive left gaps %+v: it is the run's fault, not an item's", gaps)
		}
		if n := s.Requests("/users/u2/"); n != 0 {
			t.Errorf("mailbox u2 was read (%d requests) after the drive filled", n)
		}
	})
}

// goneEventFor reports whether the run history holds a {k,gone} event for a
// record whose identity contains id.
func goneEventFor(t *testing.T, out, id string) bool {
	t.Helper()
	for _, ev := range mustHistory(t, out) {
		if ev.Gone && strings.Contains(ev.K, id) {
			return true
		}
	}
	return false
}

// covers: MA-316, R10, R6, R1, S46
// A Graph listing page error in one folder costs that folder only: Inbox's
// listing answers 500, so Inbox is one folder-list-failed fillable gap (folder
// scope, locator folder:<id>), Sent still walks (its new message B3 is archived),
// and the mailbox is NOT swept (B1, deleted from Sent at the source, stays
// Present: a message that moved into the failing folder would be swept from its
// old one). Run 3 lists Inbox again: the gap closes as recovered and the sweep
// finds B1 gone. Twin first: the healthy run 1.
func TestGraphListingPageErrorCostsOneFolder(t *testing.T) {
	s := isoServer(false)
	defer s.Close()
	out := tmpDir(t)
	if res, err := isoRun(isoOptions(s), out); err != nil || res.Stats.Exported != 7 || len(graphGaps(t, out)) != 0 {
		t.Fatalf("the twin: exported %d, err %v, gaps %d; want 7, nil, 0", res.Stats.Exported, err, len(graphGaps(t, out)))
	}
	s.SetListHandler("u1", "F_IN", graphtest.Fail(500, "ErrorInternalServerError"))
	s.RemoveMessage("u1", "F_SE", "B1")
	s.AddMessage("u1", "F_SE", "B3")
	res, err := isoRun(isoOptions(s), out)
	if !errors.Is(err, ErrPartial) {
		t.Fatalf("run 2 returned %v, want ErrPartial", err)
	}
	if !archived(t, out, "b3@x") || res.Stats.Exported != 1 {
		t.Errorf("Sent's new B3 archived: %v, exported %d; want it archived: the next folder walks", archived(t, out, "b3@x"), res.Stats.Exported)
	}
	gaps := graphGaps(t, out)
	if len(gaps) != 1 {
		t.Fatalf("the ledger holds %+v, want Inbox's one folder gap", gaps)
	}
	if g := gaps[0]; g.Reason != "folder-list-failed" || g.Class != state.ClassFillable || g.Scope != state.GapScopeFolder || g.Folder != "Inbox" || g.Locator != "folder:F_IN" {
		t.Errorf("Inbox's gap = %+v; want folder-list-failed, fillable, folder scope, Inbox, folder:F_IN", g)
	}
	if _, rec := recordForID(t, out, "b1@x"); !rec.Present || goneEventFor(t, out, "b1@x") {
		t.Errorf("B1 (deleted from Sent) is Present %v with a gone event %v: the mailbox must not be swept while a folder could not be listed", rec.Present, goneEventFor(t, out, "b1@x"))
	}

	s.SetListHandler("u1", "F_IN", nil)
	res, err = isoRun(isoOptions(s), out)
	if err != nil {
		t.Fatalf("run 3 with Inbox listing again returned %v, want nil", err)
	}
	if res.Gaps.Recovered != 1 || len(graphGaps(t, out)) != 0 {
		t.Errorf("run 3: Recovered %d, gaps %+v; want Inbox's gap closed as recovered", res.Gaps.Recovered, graphGaps(t, out))
	}
	if _, rec := recordForID(t, out, "b1@x"); rec.Present || !goneEventFor(t, out, "b1@x") {
		t.Errorf("after a clean walk B1 is Present %v, gone event %v; want it swept gone", rec.Present, goneEventFor(t, out, "b1@x"))
	}
}

// covers: MA-314, R10, R17, S46
// MA-314's folder-list-failed gap case: a repeating nextLink in Messages ends that
// folder as a folder-list-failed gap naming the stop, and the walk goes on: the
// two messages the folder listed before the repeat are archived, and so is the
// next folder's.
func TestRepeatingNextLinkIsAFolderListFailedGap(t *testing.T) {
	s := graphtest.NewServer()
	defer s.Close()
	s.AddFolder("u1", "F_IN", "Inbox", 1)
	s.AddFolder("u1", "F_SE", "Sent", 1)
	for _, id := range []string{"A1", "A2", "A3"} {
		s.AddMessage("u1", "F_IN", id)
	}
	s.AddMessage("u1", "F_SE", "B1")
	s.SetListing("u1", "F_IN", graphtest.Repeat)
	out := tmpDir(t)
	res, err := isoRun(isoOptions(s), out)
	if !errors.Is(err, ErrPartial) {
		t.Fatalf("the run returned %v, want ErrPartial", err)
	}
	gaps := graphGaps(t, out)
	if len(gaps) != 1 {
		t.Fatalf("the ledger holds %+v, want Inbox's one folder gap", gaps)
	}
	if g := gaps[0]; g.Reason != "folder-list-failed" || g.Scope != state.GapScopeFolder || g.Folder != "Inbox" || !strings.Contains(g.Detail, "did not terminate") {
		t.Errorf("Inbox's gap = %+v; want folder-list-failed naming the stop (\"did not terminate\")", g)
	}
	for _, id := range []string{"a1@x", "a2@x", "b1@x"} {
		if !archived(t, out, id) {
			t.Errorf("%s was not archived: the stop must cost the rest of Inbox's listing only", id)
		}
	}
	if res.Stats.Exported != 3 {
		t.Errorf("exported %d, want A1, A2 and B1", res.Stats.Exported)
	}
}

// covers: MA-314, R10, R17, S46
// The gap half of MA-314's stall case: a $value that stalls mid-body past the
// idle window (scaled to 200 ms) is a fillable fetch-timeout gap naming the
// message and the limit, the walk's own context is still live, and the next item
// is fetched and archived. The stalled message has no record.
func TestMIMEStallIsAFetchTimeoutGap(t *testing.T) {
	s := graphtest.NewServer()
	defer s.Close()
	s.AddFolder("u1", "F_IN", "Inbox", 10)
	for _, id := range []string{"A1", "STALL", "A2"} {
		s.AddMessage("u1", "F_IN", id)
	}
	s.SetValue("STALL", graphtest.StallMidBody("From: a@example.com\r\n\r\npartial"))
	g := isoOptions(s)
	g.MIMEIdleTimeout, g.MIMEHeaderTimeout = graphtest.IdleTimeout, graphtest.HeaderTimeout
	out := tmpDir(t)
	res, err := isoRun(g, out)
	if !errors.Is(err, ErrPartial) || errors.Is(err, context.Canceled) {
		t.Fatalf("the run returned %v, want ErrPartial and no cancel", err)
	}
	if !archived(t, out, "a1@x") || !archived(t, out, "a2@x") || archived(t, out, "stall@x") || res.Stats.Exported != 2 {
		t.Errorf("A1 %v, A2 %v, STALL %v, exported %d; want the two healthy messages archived and the stalled one not", archived(t, out, "a1@x"), archived(t, out, "a2@x"), archived(t, out, "stall@x"), res.Stats.Exported)
	}
	gaps := graphGaps(t, out)
	if len(gaps) != 1 {
		t.Fatalf("the ledger holds %+v, want STALL's one gap", gaps)
	}
	if g := gaps[0]; g.Reason != "fetch-timeout" || g.Class != state.ClassFillable || g.Locator != "graph:STALL" ||
		!strings.Contains(g.Detail, "STALL") || !strings.Contains(g.Detail, graphtest.IdleTimeout.String()) {
		t.Errorf("STALL's gap = %+v; want fetch-timeout, fillable, graph:STALL, naming the message and the 200ms idle limit", g)
	}
}
