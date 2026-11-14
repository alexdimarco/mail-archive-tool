package instance

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// covers: MA-1401, R23
// A proven request is used once (the S1 fix round, A6). Positive twin first: a proven
// `choose` and a proven first-form open each work once. The same bytes sent again (a
// request read off the wire) are refused with 403, show nothing a second time and open
// nothing a second time; a fresh request from the launcher (a new nonce) still works.
func TestMA1401_ACapturedRequestIsRefusedWhenSentAgain(t *testing.T) {
	g := newRig(t)
	rec := &actRecorder{shown: true}
	g.h.Act = rec.act
	n := strings.Repeat("ab", 32)
	choose, _ := json.Marshal(map[string]any{"n": n, "p": reqMAC2(g.key, n, ActChoose, "", "", "", false), "act": ActChoose})
	if c := rawPost(t, g, string(choose)); c != http.StatusOK || len(rec.calls()) != 1 {
		t.Fatalf("a proven choose: %d, Act calls %d; want 200 and one", c, len(rec.calls()))
	}
	g.clock.Advance(2 * time.Second)
	if c := rawPost(t, g, string(choose)); c != http.StatusForbidden || len(rec.calls()) != 1 {
		t.Errorf("the same choose again: %d, Act calls %d; want 403 and still one", c, len(rec.calls()))
	}

	m := strings.Repeat("cd", 32)
	open, _ := json.Marshal(map[string]any{"n": m, "p": MAC(g.key, "handoff-req", m), "quiet": true})
	g.clock.Advance(2 * time.Second)
	if c := rawPost(t, g, string(open)); c != http.StatusOK {
		t.Fatalf("a proven quiet open: %d, want 200", c)
	}
	g.clock.Advance(2 * time.Second)
	if c := rawPost(t, g, string(open)); c != http.StatusForbidden {
		t.Errorf("the same quiet open again: %d, want 403 (no second link minted)", c)
	}
	g.clock.Advance(2 * time.Second)
	if rep, err := CallAct(context.Background(), g.addr, g.key, false, ActRequest{Act: ActChoose}); err != nil || !rep.Shown {
		t.Errorf("a fresh choose after the replay: %+v, %v", rep, err)
	}
	// The record is bounded: it keeps at most maxSeen nonces.
	h := &Handoff{}
	h.mu.Lock()
	for i := 0; i < maxSeen+10; i++ {
		h.replayed(strings.Repeat("0", 60) + string(rune('a'+i%26)) + string(rune('a'+(i/26)%26)) + string(rune('a'+(i/676)%26)) + "x")
	}
	size := len(h.seen)
	h.mu.Unlock()
	if size > maxSeen {
		t.Errorf("the replay record holds %d nonces, want at most %d", size, maxSeen)
	}
}

// covers: MA-1427, MA-1401, R50
// The gate answers before anything is shown or opened (the S1 fix round, A3, A7).
// Positive twin first: a gate that lets the request through leaves `choose` shown and an
// `open` opened. A gate that answers another sign-in, an unknown sign-in or "closing"
// makes the instance show and open nothing: the reason comes back MAC-covered for every
// action, an `open` included (ReachOpen hands it to the launcher at once), and the
// refusal does not use the second up. The gate sees the launcher's sign-in and quiet
// flag. A first-form request refused by the gate is a 503 (the first form has no reason),
// and opens nothing.
func TestMA1427_TheGateAnswersBeforeAnythingShows(t *testing.T) {
	ctx := context.Background()
	g := newRig(t)
	rec := &actRecorder{shown: true}
	g.h.Act = rec.act
	var mu sync.Mutex
	verdict := ""
	var asked []ActRequest
	g.h.Gate = func(r ActRequest) string {
		mu.Lock()
		defer mu.Unlock()
		asked = append(asked, r)
		return verdict
	}
	if rep, err := CallAct(ctx, g.addr, g.key, false, ActRequest{Act: ActChoose, Session: "100"}); err != nil || !rep.Shown {
		t.Fatalf("an allowed choose: %+v, %v", rep, err)
	}
	g.clock.Advance(2 * time.Second)
	if rep, err := ReachOpen(ctx, g.addr, g.key, false, ActRequest{Session: "100"}, time.Second, nil); err != nil || !rep.Opened {
		t.Fatalf("an allowed open: %+v, %v", rep, err)
	}
	opensBefore, actsBefore := g.opened.Load(), len(rec.calls())

	for _, reason := range []string{ReasonOtherSession, ReasonUnknownSession, ReasonClosing} {
		mu.Lock()
		verdict = reason
		mu.Unlock()
		for _, act := range []string{ActChoose, ActWizard} {
			rep, err := CallAct(ctx, g.addr, g.key, false, ActRequest{Act: act, Session: "200"})
			if err != nil || rep.Shown || rep.Reason != reason {
				t.Errorf("%s refused as %s: %+v, %v", act, reason, rep, err)
			}
		}
		start := time.Now()
		rep, err := ReachOpen(ctx, g.addr, g.key, false, ActRequest{Session: "200"}, 3*time.Second, func() bool { return true })
		if err != nil || rep.Opened || rep.Link != "" || rep.Reason != reason {
			t.Errorf("open refused as %s: %+v, %v", reason, rep, err)
		}
		if time.Since(start) > time.Second {
			t.Errorf("an open refused as %s was retried for %v", reason, time.Since(start))
		}
	}
	if g.opened.Load() != opensBefore || len(rec.calls()) != actsBefore {
		t.Errorf("a gated request opened %d or showed %d", g.opened.Load()-opensBefore, len(rec.calls())-actsBefore)
	}
	mu.Lock()
	last := asked[len(asked)-1]
	mu.Unlock()
	if last.Act != ActOpen || last.Session != "200" || last.Quiet {
		t.Errorf("the gate was asked %+v, want the open with the launcher's sign-in", last)
	}
	// A first-form request refused by the gate: 503, nothing opened.
	_, err := Call(ctx, g.addr, g.key, false)
	if !errors.Is(err, ErrBadProof) || errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "503") {
		t.Errorf("a gated first-form request: %v, want a 503", err)
	}
	if g.opened.Load() != opensBefore {
		t.Errorf("a gated first-form request opened the browser")
	}
	// The refusals did not use the second up: once the gate lets go, a request at the
	// same moment is shown, not answered busy.
	mu.Lock()
	verdict = ""
	mu.Unlock()
	g.clock.Advance(2 * time.Second)
	if rep, err := CallAct(ctx, g.addr, g.key, false, ActRequest{Act: ActChoose, Session: "100"}); err != nil || !rep.Shown || rep.Busy {
		t.Errorf("a choose after the gated ones: %+v, %v; want shown", rep, err)
	}
}
