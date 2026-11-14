package instance

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// quitRecorder is an instance's Quit: it records each request and answers as told.
type quitRecorder struct {
	mu     sync.Mutex
	stops  []bool
	ok     bool
	reason string
}

func (q *quitRecorder) quit(stop bool) (bool, string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.stops = append(q.stops, stop)
	return q.ok, q.reason
}

func (q *quitRecorder) calls() []bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]bool(nil), q.stops...)
}

// covers: MA-1427, MA-1401, MA-1424, R52, R23
// The quit request (the S1 fix round 2, B1, B2, B7: what `mailarchive quit` and the
// upgrade script ask, over the hand-off instead of window messages). Positive twin first:
// a proven can-quit reaches the instance's Quit as a question (stop false) and a proven
// quit as a stop (stop true), each answered yes under a MAC that covers the action. A busy
// instance answers "busy" and a closing one "closing", MAC-covered. The request shows
// nothing on any screen, so the sign-in gate is not asked and the one-a-second launch limit
// does not apply. A request without the key's proof is 403 and Quit is never asked; the
// same request sent again is 403; an instance with no Quit answers that it cannot.
func TestMA1427_TheQuitRequestIsProvenAndShowsNothing(t *testing.T) {
	ctx := context.Background()
	g := newRig(t)
	q := &quitRecorder{ok: true}
	g.h.Quit = q.quit
	gated := 0
	g.h.Gate = func(ActRequest) string { gated++; return ReasonOtherSession }
	rec := &actRecorder{shown: true}
	g.h.Act = rec.act

	rep, err := CallAct(ctx, g.addr, g.key, false, ActRequest{Act: ActCanQuit})
	if err != nil || !rep.Shown || rep.Reason != "" {
		t.Fatalf("twin: a proven can-quit: %+v, %v", rep, err)
	}
	// At the same moment (no clock advance): never answered "busy" by the launch limit.
	rep, err = CallAct(ctx, g.addr, g.key, false, ActRequest{Act: ActQuit})
	if err != nil || !rep.Shown || rep.Busy {
		t.Fatalf("twin: a proven quit: %+v, %v", rep, err)
	}
	if c := q.calls(); len(c) != 2 || c[0] || !c[1] {
		t.Fatalf("Quit was asked %v, want [question, stop]", c)
	}
	if gated != 0 || len(rec.calls()) != 0 {
		t.Errorf("the quit request went through the sign-in gate (%d) or showed something (%d)", gated, len(rec.calls()))
	}

	for _, reason := range []string{ReasonBusy, ReasonClosing} {
		q.mu.Lock()
		q.ok, q.reason = false, reason
		q.mu.Unlock()
		for _, act := range []string{ActCanQuit, ActQuit} {
			rep, err := CallAct(ctx, g.addr, g.key, false, ActRequest{Act: act})
			if err != nil || rep.Shown || rep.Reason != reason {
				t.Errorf("%s answered %s: %+v, %v", act, reason, rep, err)
			}
		}
	}

	before := len(q.calls())
	n := strings.Repeat("ef", 32)
	forged, _ := json.Marshal(map[string]any{"n": n, "p": reqMAC2([]byte("not the key, not the key, not the key"), n, ActQuit, "", "", "", false), "act": ActQuit})
	if c := rawPost(t, g, string(forged)); c != http.StatusForbidden {
		t.Errorf("a quit without the key's proof: %d, want 403", c)
	}
	proven, _ := json.Marshal(map[string]any{"n": n, "p": reqMAC2(g.key, n, ActQuit, "", "", "", false), "act": ActQuit})
	if c := rawPost(t, g, string(proven)); c != http.StatusOK {
		t.Fatalf("a proven quit: %d", c)
	}
	if c := rawPost(t, g, string(proven)); c != http.StatusForbidden {
		t.Errorf("the same quit sent again: %d, want 403", c)
	}
	if got := len(q.calls()) - before; got != 1 {
		t.Errorf("Quit was asked %d times by a forged, a proven and a replayed request, want 1", got)
	}

	g.h.Quit = nil
	if rep, err := CallAct(ctx, g.addr, g.key, false, ActRequest{Act: ActQuit}); err != nil || rep.Shown || rep.Reason != ReasonCantShow {
		t.Errorf("an instance with no Quit: %+v, %v; want cant-show", rep, err)
	}
	for _, a := range []string{ActCanQuit, ActQuit} {
		if !(Info{Acts: KnownActs}).HasAct(a) {
			t.Errorf("this version does not advertise %q", a)
		}
	}
}

// covers: MA-1427, MA-1402, R50
// The first request form carries no sign-in (the S1 fix round 2, B9). Positive twin
// first: an instance whose sign-ins are separate (Windows' lock is per session) opens its
// browser for a first-form open, as before. Where one instance serves every sign-in of
// the account (FirstFormQuiet), a first-form open from an older launcher opens nothing on
// the instance's screen and is answered with a link, which that launcher shows in its own
// session (a clear answer, never a refusal it would wait out); the second form's open still
// opens the browser.
func TestMA1427_AFirstFormOpenIsALinkWhereSignInsShareOneCopy(t *testing.T) {
	ctx := context.Background()
	g := newRig(t)
	rep, err := Call(ctx, g.addr, g.key, false)
	if err != nil || !rep.Opened || g.opened.Load() != 1 {
		t.Fatalf("twin: a first-form open where sign-ins are separate: %+v, %v, opened %d", rep, err, g.opened.Load())
	}

	g.h.FirstFormQuiet = true
	g.clock.Advance(2 * time.Second)
	rep, err = Call(ctx, g.addr, g.key, false)
	if err != nil {
		t.Fatalf("a first-form open where one copy serves every sign-in: %v", err)
	}
	if rep.Opened || g.opened.Load() != 1 {
		t.Errorf("a first-form open opened the instance's browser (%d opens)", g.opened.Load())
	}
	if !strings.Contains(rep.Link, "/_open?t=") {
		t.Errorf("a first-form open was answered %+v, want a link for the launch's own session", rep)
	}
	g.clock.Advance(2 * time.Second)
	if rep, err := CallAct(ctx, g.addr, g.key, false, ActRequest{Act: ActOpen, Session: "100"}); err != nil || !rep.Opened || g.opened.Load() != 2 {
		t.Errorf("the second form's open: %+v, %v, opens %d; want opened", rep, err, g.opened.Load())
	}
}
