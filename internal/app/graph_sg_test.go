package app

import (
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
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

// covers: MA-540, R10, R17, R18, R21, S37
// SG's stops on the shipped capture path: a nextLink repeat, the page cap and a
// stalled $value each fail mailbox 1 with a typed error, record no {k,gone},
// walk mailbox 2 and record the run failed, never cancelled.
func TestRunGraphGuardStopsFailTheMailbox(t *testing.T) {
	t.Skip("MA-540: the nextLink guard, MIMETo's typed stops and RunGraph's own ctx.Err() test do not exist yet; lands in FI-SG")
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
