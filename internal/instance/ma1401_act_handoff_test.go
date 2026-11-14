package instance

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// actRecorder is the instance's Act seam: what it was asked, and what it answers.
type actRecorder struct {
	mu     sync.Mutex
	got    []ActRequest
	shown  bool
	reason string
}

func (a *actRecorder) act(r ActRequest) (bool, string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.got = append(a.got, r)
	return a.shown, a.reason
}

func (a *actRecorder) calls() []ActRequest {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]ActRequest(nil), a.got...)
}

// rawPost posts body to the rig's /_handoff and returns the status.
func rawPost(t *testing.T, g *rig, body string) int {
	t.Helper()
	resp, err := http.Post(g.srv.URL+"/_handoff", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

// covers: MA-1401, MA-1427, R50, R23
// The hand-off's second form (design-menubar §7.1 as MB-3 amends it). Positive twins
// first: the first form with no action still opens Desktop exactly as before, and a
// proven `choose` reaches the instance's Act with the launcher's version, session and
// token, and comes back shown under a MAC that covers the action. Then: the request's
// MAC covers the action, the version, the session and the token (each changed after
// signing is a 403, and Act is never called); an unknown action under the new label is
// a 400, never "open"; first-form requests that carry second-form fields are a 400; a
// token over 256 characters or with the separator is a 400, and the client drops one
// before sending; the reasons come back MAC-covered, and a reason the instance does
// not know is sent as "can't show here"; an instance with no Act answers "can't show
// here"; the busy answer of the second form is bound to the action.
func TestTheSecondFormCarriesItsActionUnderTheMAC(t *testing.T) {
	ctx := context.Background()
	g := newRig(t)
	rec := &actRecorder{shown: true}
	g.h.Act = rec.act

	// Positive twin 1: the first form is today's open.
	rep, err := Call(ctx, g.addr, g.key, false)
	if err != nil || !rep.Opened || len(rec.calls()) != 0 {
		t.Fatalf("the first form: %+v, %v, Act calls %d; want Desktop opened and no Act", rep, err, len(rec.calls()))
	}

	// Positive twin 2: a proven choose.
	g.clock.Advance(2 * time.Second)
	tok := strings.Repeat("t", 256)
	rep, err = CallAct(ctx, g.addr, g.key, false, ActRequest{Act: ActChoose, Version: "v1.8.0", Session: "42", Token: tok})
	if err != nil || !rep.Shown || rep.Reason != "" || rep.Opened || rep.Link != "" {
		t.Fatalf("a proven choose: %+v, %v; want shown", rep, err)
	}
	if c := rec.calls(); len(c) != 1 || c[0] != (ActRequest{Act: ActChoose, Version: "v1.8.0", Session: "42", Token: tok}) {
		t.Fatalf("Act was asked %+v, want one choose carrying the version, session and token", c)
	}
	if g.opened.Load() != 1 {
		t.Errorf("a choose opened the browser (%d opens, want only the first form's 1)", g.opened.Load())
	}

	// An open in the second form opens Desktop under its own reply label.
	g.clock.Advance(2 * time.Second)
	if rep, err := CallAct(ctx, g.addr, g.key, false, ActRequest{Act: ActOpen, Version: "v1.8.0"}); err != nil || !rep.Opened {
		t.Errorf("an open in the second form: %+v, %v; want opened", rep, err)
	}
	if len(rec.calls()) != 1 {
		t.Errorf("an open called Act")
	}

	// The MAC covers every field: each changed after signing is refused, Act not called.
	n := strings.Repeat("ab", 32)
	signed := reqMAC2(g.key, n, ActChoose, "v1.8.0", "42", "tok", false)
	for name, body := range map[string]map[string]any{
		"action swapped":  {"n": n, "p": signed, "act": ActWizard, "ver": "v1.8.0", "sess": "42", "tok": "tok"},
		"version changed": {"n": n, "p": signed, "act": ActChoose, "ver": "v9.9.9", "sess": "42", "tok": "tok"},
		"session changed": {"n": n, "p": signed, "act": ActChoose, "ver": "v1.8.0", "sess": "43", "tok": "tok"},
		"token changed":   {"n": n, "p": signed, "act": ActChoose, "ver": "v1.8.0", "sess": "42", "tok": "TOK"},
		"quiet changed":   {"n": n, "p": signed, "act": ActChoose, "ver": "v1.8.0", "sess": "42", "tok": "tok", "quiet": true},
		"old label":       {"n": n, "p": MAC(g.key, "handoff-req", n), "act": ActChoose},
	} {
		g.clock.Advance(2 * time.Second)
		b, _ := json.Marshal(body)
		if c := rawPost(t, g, string(b)); c != http.StatusForbidden {
			t.Errorf("%s: %d, want 403", name, c)
		}
	}
	if len(rec.calls()) != 1 {
		t.Errorf("a request whose proof failed reached Act: %+v", rec.calls())
	}

	// An unknown action, and fields out of bounds, are a 400 even with a good proof.
	// (Amended 2026-11-13, the S1 fix round 2: "quit" is now a known action, so the
	// unknown one is "reboot"; amended, not weakened.)
	for name, f := range map[string][4]string{
		"unknown action":      {"reboot", "", "", ""},
		"long token":          {ActChoose, "", "", strings.Repeat("t", 257)},
		"separator in token":  {ActChoose, "", "", "a|b"},
		"control in version":  {ActChoose, "v1\n", "", ""},
		"long session":        {ActChoose, "", strings.Repeat("9", 65), ""},
		"separator in action": {"choose|x", "", "", ""},
	} {
		g.clock.Advance(2 * time.Second)
		b, _ := json.Marshal(map[string]any{"n": n, "p": reqMAC2(g.key, n, f[0], f[1], f[2], f[3], false), "act": f[0], "ver": f[1], "sess": f[2], "tok": f[3]})
		if c := rawPost(t, g, string(b)); c != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", name, c)
		}
	}
	// The first form with a second-form field is a 400.
	b, _ := json.Marshal(map[string]any{"n": n, "p": MAC(g.key, "handoff-req", n), "ver": "v1"})
	if c := rawPost(t, g, string(b)); c != http.StatusBadRequest {
		t.Errorf("a first-form request carrying a version: %d, want 400", c)
	}
	if len(rec.calls()) != 1 {
		t.Errorf("a refused request reached Act: %+v", rec.calls())
	}
	// The client never sends a token out of bounds: it drops it.
	g.clock.Advance(2 * time.Second)
	if _, err := CallAct(ctx, g.addr, g.key, false, ActRequest{Act: ActChoose, Token: strings.Repeat("t", 300)}); err != nil {
		t.Fatalf("a choose with a token out of bounds: %v, want it sent without the token", err)
	}
	if c := rec.calls(); c[len(c)-1].Token != "" {
		t.Errorf("a token over 256 characters reached the instance")
	}

	// Reasons come back covered by the MAC.
	for _, want := range []string{ReasonAlreadyOpen, ReasonCantShow, ReasonOtherSession, ReasonUnknownSession, ReasonClosing} {
		g.clock.Advance(2 * time.Second)
		rec.shown, rec.reason = false, want
		rep, err := CallAct(ctx, g.addr, g.key, false, ActRequest{Act: ActWizard})
		if err != nil || rep.Shown || rep.Reason != want {
			t.Errorf("reason %q: %+v, %v", want, rep, err)
		}
	}
	g.clock.Advance(2 * time.Second)
	rec.shown, rec.reason = false, "made up"
	if rep, err := CallAct(ctx, g.addr, g.key, false, ActRequest{Act: ActChoose}); err != nil || rep.Reason != ReasonCantShow {
		t.Errorf("a reason the protocol does not know: %+v, %v; want can't show here", rep, err)
	}
	g.clock.Advance(2 * time.Second)
	g.h.Act = nil
	if rep, err := CallAct(ctx, g.addr, g.key, false, ActRequest{Act: ActChoose}); err != nil || rep.Shown || rep.Reason != ReasonCantShow {
		t.Errorf("an instance with no Act: %+v, %v; want can't show here", rep, err)
	}

	// Busy, in the second form: bound to the action.
	g.h.Act = rec.act
	rec.shown, rec.reason = true, ""
	g.clock.Advance(2 * time.Second)
	if _, err := CallAct(ctx, g.addr, g.key, false, ActRequest{Act: ActChoose}); err != nil {
		t.Fatal(err)
	}
	if rep, err := CallAct(ctx, g.addr, g.key, false, ActRequest{Act: ActChoose}); err != nil || !rep.Busy {
		t.Errorf("a second choose inside a second: %+v, %v; want busy", rep, err)
	}
}

// covers: MA-1401, MA-1427, R23
// A launcher is never fooled by a listener answering the second form: a reply whose
// MAC is for another action, another shown value, another reason or another nonce,
// a busy answer under the first form's label, and a reason outside the protocol are
// all ErrBadProof; an instance that knows only the first form answers the second with
// 403, which the client reports as ErrRefused (still ErrBadProof), so the launcher can
// fall back to the first form once (MB-3 item 2).
func TestTheSecondFormsReplyIsProven(t *testing.T) {
	ctx := context.Background()
	_, key := NewKey()
	squat := func(reply func(n, act string) any) string {
		sq := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req struct{ N, Act string }
			json.NewDecoder(r.Body).Decode(&req)
			json.NewEncoder(w).Encode(reply(req.N, req.Act))
		}))
		t.Cleanup(sq.Close)
		return strings.TrimPrefix(sq.URL, "http://")
	}
	other := strings.Repeat("cd", 32)
	// Positive twin: a squatter that holds the key and answers properly is believed.
	good := squat(func(n, act string) any {
		return map[string]any{"shown": true, "p": MAC(key, "handoff-shown2", n, act, "true", "")}
	})
	if rep, err := CallAct(ctx, good, key, false, ActRequest{Act: ActChoose}); err != nil || !rep.Shown {
		t.Fatalf("a proper answer: %+v, %v", rep, err)
	}
	for name, addr := range map[string]string{
		"another action": squat(func(n, act string) any {
			return map[string]any{"shown": true, "p": MAC(key, "handoff-shown2", n, ActWizard, "true", "")}
		}),
		"shown flipped": squat(func(n, act string) any {
			return map[string]any{"shown": true, "p": MAC(key, "handoff-shown2", n, act, "false", "")}
		}),
		"reason swapped": squat(func(n, act string) any {
			return map[string]any{"reason": ReasonCantShow, "p": MAC(key, "handoff-shown2", n, act, "false", ReasonAlreadyOpen)}
		}),
		"another nonce": squat(func(n, act string) any {
			return map[string]any{"shown": true, "p": MAC(key, "handoff-shown2", other, act, "true", "")}
		}),
		"first-form busy": squat(func(n, act string) any {
			return map[string]any{"busy": true, "p": MAC(key, "handoff-busy", n)}
		}),
		"unknown reason": squat(func(n, act string) any {
			return map[string]any{"reason": "<b>", "p": MAC(key, "handoff-shown2", n, act, "false", "<b>")}
		}),
		"first-form ok": squat(func(n, act string) any {
			return map[string]any{"opened": true, "p": MAC(key, "handoff-ok", n, "true", "")}
		}),
	} {
		if _, err := CallAct(ctx, addr, key, false, ActRequest{Act: ActChoose}); !errors.Is(err, ErrBadProof) {
			t.Errorf("%s: %v, want ErrBadProof", name, err)
		}
	}
	// An open in the second form needs the second label too.
	opener := squat(func(n, act string) any {
		return map[string]any{"opened": true, "p": MAC(key, "handoff-ok", n, "true", "")}
	})
	if _, err := CallAct(ctx, opener, key, false, ActRequest{Act: ActOpen}); !errors.Is(err, ErrBadProof) {
		t.Errorf("an open answered under the first label: %v, want ErrBadProof", err)
	}

	// An instance that knows only the first form: today's handler with no Act field
	// in its proof check is simulated by a server that verifies only the old label.
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req handoffRequest
		json.NewDecoder(r.Body).Decode(&req)
		if !SameMAC(req.P, MAC(key, "handoff-req", req.N)) {
			http.Error(w, "refused", http.StatusForbidden)
			return
		}
		json.NewEncoder(w).Encode(handoffReply{Opened: true, P: MAC(key, "handoff-ok", req.N, "true", "")})
	}))
	t.Cleanup(old.Close)
	oldAddr := strings.TrimPrefix(old.URL, "http://")
	_, err := CallAct(ctx, oldAddr, key, false, ActRequest{Act: ActChoose})
	if !errors.Is(err, ErrRefused) || !errors.Is(err, ErrBadProof) {
		t.Errorf("the second form to a first-form instance: %v, want ErrRefused", err)
	}
	if rep, err := Call(ctx, oldAddr, key, false); err != nil || !rep.Opened {
		t.Errorf("the first form to it still works: %+v, %v", rep, err)
	}
}
