package instance

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"mail-archive-tool/internal/localweb"
)

// The handoff (design §4.C4 step 3): POST /_handoff on the instance's listener,
// the body {"n":"<32-byte hex nonce N>","p":"<hex HMAC-SHA256(K, "handoff-req|"+N)>"}
// plus "quiet":true when the caller wants a link rather than a browser. The
// instance verifies p (constant time; body at most 1 KiB; at most one handoff a
// second), mints a launch token, opens a browser in its own logon session unless
// quiet, waits about 15 s for that token to be redeemed, and answers
// {"opened":bool,"link":"…","p":"<hex HMAC(K, "handoff-ok|"+N+"|"+opened+"|"+link)>"}
// [CC20]; a handoff within a second of the last is answered with a MAC-covered
// {"busy":true,"p":"<hex HMAC(K, "handoff-busy|"+N)>"}, which a launching client
// counts as success [CC21]. Both directions are proven with K, so a squatter on
// the recorded port can make the client open nothing, and a caller without K gets
// nothing.

const (
	maxHandoffBody = 1 << 10
	maxReplyBody   = 4 << 10
	handoffRate    = time.Second
	// DefaultWait is how long the instance waits for the browser it opened to
	// redeem the link before answering that it did not open [CC20].
	DefaultWait = 15 * time.Second
)

// CallTimeout bounds one handoff call: the instance may wait DefaultWait for the
// browser to arrive before it answers, so it outlasts that. A var so a test of a
// listener that never answers need not wait it out.
var CallTimeout = DefaultWait + 10*time.Second

// Handoff is the server side: it makes one instance open a browser for, or hand
// a link to, a caller that proves it holds the instance's key.
type Handoff struct {
	// Key is K.
	Key []byte
	// Mint makes a one-time launch link valid for ttl and returns it with its token.
	Mint func(ttl time.Duration) (link, token string)
	// Open opens link in a browser of the instance's own session; nil means this
	// instance has none to open (a handoff is then answered not-opened with a link).
	Open func(link string) error
	// Redeemed reports whether token was redeemed within d.
	Redeemed func(token string, d time.Duration) bool
	// Wait is how long to wait for the redemption (DefaultWait when zero).
	Wait time.Duration
	// Now is the clock for the rate limit (time.Now when nil).
	Now func() time.Time

	// Act answers a request with an action that shows something in the instance
	// (ActChoose, ActWizard; design-menubar §7.1): it is called only after the
	// request's proof is verified, never for ActOpen, and it returns at once with
	// whether the window is shown and, when not, why (a Reason* value). nil means
	// this instance shows nothing of the kind: it answers ReasonCantShow.
	Act func(ActRequest) (shown bool, reason string)

	mu   sync.Mutex
	last time.Time
}

type handoffRequest struct {
	N     string `json:"n"`
	P     string `json:"p"`
	Quiet bool   `json:"quiet"`
	// The second form (label handoff-req2, design-menubar §7.1 and MB-3): the action,
	// the launcher's version (MB-14), its sign-in session (R50, the fold's third
	// contradiction) and, on Linux, its activation token (§7.4). Each is inside the
	// request's MAC.
	Act  string `json:"act,omitempty"`
	Ver  string `json:"ver,omitempty"`
	Sess string `json:"sess,omitempty"`
	Tok  string `json:"tok,omitempty"`
}

type handoffReply struct {
	Opened bool   `json:"opened"`
	Busy   bool   `json:"busy,omitempty"`
	Link   string `json:"link,omitempty"`
	// Shown and Reason answer ActChoose and ActWizard (MB-3 item 4).
	Shown  bool   `json:"shown,omitempty"`
	Reason string `json:"reason,omitempty"`
	P      string `json:"p"`
}

// The actions of the second request form (design-menubar §7.1, D4 as MB-3 amends it).
const (
	// ActOpen opens Desktop in the instance's browser, as the first form does.
	ActOpen = "open"
	// ActChoose shows the launcher choice (Run the wizard / Open MailArchive Desktop).
	ActChoose = "choose"
	// ActWizard runs the wizard in the instance.
	ActWizard = "wizard"
)

// KnownActs is what an instance of this version advertises in its file (Info.Acts):
// a launcher sends the second form only to an instance that lists the action (MB-3).
var KnownActs = []string{ActOpen, ActChoose, ActWizard}

// Why an action was not shown (MB-3 item 4, MB-9, the fold's third contradiction).
const (
	// ReasonAlreadyOpen: a choice or a wizard is already open in the instance.
	ReasonAlreadyOpen = "already-open"
	// ReasonCantShow: the instance cannot show dialogs (no display, no dialog tool);
	// the launcher opens Desktop or shows the link instead (MB-9).
	ReasonCantShow = "cant-show"
	// ReasonOtherSession: the instance belongs to another sign-in of this account; the
	// launcher says so in its own session and nothing shows on the other screen (R50).
	ReasonOtherSession = "other-session"
)

// ActRequest is a verified request with an action, as the instance's Act sees it.
type ActRequest struct {
	Act string
	// Version is the launcher's build version (MB-14), Session its sign-in session
	// ("" when it has none to tell), Token its activation token ("" for none).
	Version, Session, Token string
}

// Field bounds of the second form: each is printable ASCII with no "|" (the MAC's
// separator), the token at most 256 characters (MB-3 item 5).
const (
	maxVerLen  = 64
	maxSessLen = 64
	maxTokLen  = 256
)

// validField reports whether s is at most max printable ASCII characters with no "|".
func validField(s string, max int) bool {
	if len(s) > max {
		return false
	}
	for i := 0; i < len(s); i++ {
		if c := s[i]; c < 0x20 || c > 0x7e || c == '|' {
			return false
		}
	}
	return true
}

func validReason(r string) bool {
	switch r {
	case "", ReasonAlreadyOpen, ReasonCantShow, ReasonOtherSession:
		return true
	}
	return false
}

func knownAct(a string) bool {
	for _, k := range KnownActs {
		if a == k {
			return true
		}
	}
	return false
}

// reqMAC2 is the second form's request proof: every field it carries is inside it.
func reqMAC2(key []byte, n, act, ver, sess, tok string) string {
	return MAC(key, "handoff-req2", n, act, ver, sess, tok)
}

func validNonce(n string) bool {
	if len(n) != 64 {
		return false
	}
	_, err := hex.DecodeString(n)
	return err == nil
}

func (h *Handoff) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

// ServeHTTP answers a POST /_handoff.
func (h *Handoff) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req handoffRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxHandoffBody)).Decode(&req); err != nil || !validNonce(req.N) {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	// The second form names its action; an unknown one, or a field out of bounds, is
	// refused with 400 and never read as "open" (MB-3 item 3).
	v2 := req.Act != ""
	if v2 && (!knownAct(req.Act) || !validField(req.Ver, maxVerLen) || !validField(req.Sess, maxSessLen) || !validField(req.Tok, maxTokLen)) {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if !v2 && (req.Ver != "" || req.Sess != "" || req.Tok != "") {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	want := MAC(h.Key, "handoff-req", req.N)
	if v2 {
		want = reqMAC2(h.Key, req.N, req.Act, req.Ver, req.Sess, req.Tok)
	}
	if !SameMAC(req.P, want) {
		http.Error(w, "refused", http.StatusForbidden)
		return
	}

	h.mu.Lock()
	now := h.now()
	if !h.last.IsZero() && now.Sub(h.last) < handoffRate {
		h.mu.Unlock()
		if v2 {
			writeReply(w, handoffReply{Busy: true, P: MAC(h.Key, "handoff-busy2", req.N, req.Act)})
			return
		}
		writeReply(w, handoffReply{Busy: true, P: MAC(h.Key, "handoff-busy", req.N)})
		return
	}
	prev := h.last
	h.last = now
	h.mu.Unlock()

	if v2 && req.Act != ActOpen {
		// The proof is verified: only now may the instance show anything (MB-8).
		shown, reason := false, ReasonCantShow
		if h.Act != nil {
			shown, reason = h.Act(ActRequest{Act: req.Act, Version: req.Ver, Session: req.Sess, Token: req.Tok})
		}
		if shown || !validReason(reason) {
			reason = ""
			if !shown {
				reason = ReasonCantShow
			}
		}
		if !shown {
			// Nothing was shown, so the second does not count: the launcher's fallback
			// (today's open, MB-9) must not be answered "busy" and lost.
			h.mu.Lock()
			if h.last.Equal(now) {
				h.last = prev
			}
			h.mu.Unlock()
		}
		writeReply(w, handoffReply{Shown: shown, Reason: reason, P: MAC(h.Key, "handoff-shown2", req.N, req.Act, strconv.FormatBool(shown), reason)})
		return
	}

	opened, link := false, ""
	if !req.Quiet && h.Open != nil {
		l, tok := h.Mint(localweb.OpenerTTL)
		if h.Open(l) == nil {
			wait := h.Wait
			if wait <= 0 {
				wait = DefaultWait
			}
			opened = h.Redeemed(tok, wait)
		}
	}
	if !opened {
		link, _ = h.Mint(localweb.LinkTTL)
	}
	if v2 {
		writeReply(w, handoffReply{Opened: opened, Link: link, P: MAC(h.Key, "handoff-ok2", req.N, req.Act, strconv.FormatBool(opened), link)})
		return
	}
	writeReply(w, handoffReply{Opened: opened, Link: link, P: MAC(h.Key, "handoff-ok", req.N, strconv.FormatBool(opened), link)})
}

func writeReply(w http.ResponseWriter, r handoffReply) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(r)
}

// Reply is what a verified handoff answered.
type Reply struct {
	// Busy: another handoff was within the second; the instance is already opening
	// a browser for it.
	Busy bool
	// Opened: the instance opened its browser and the link was redeemed.
	Opened bool
	// Link is a fresh one-time link, when the instance did not open its browser
	// (quiet, or it could not).
	Link string
	// Shown and Reason answer an action that shows a window (ActChoose, ActWizard).
	Shown  bool
	Reason string
}

// ErrBadProof means the listener answered but did not prove it holds the key: a
// squatter, another account's instance, or an old program. The caller opens
// nothing at that address.
var ErrBadProof = errors.New("the listener did not prove it is this account's MailArchive")

// ErrRefused (which is also ErrBadProof) is a 403 to the request: the listener did not
// accept the request's proof. A launcher that sent the second form to an instance that
// advertises it falls back once to the first form on it (MB-3 item 2).
var ErrRefused = fmt.Errorf("%w (the request was refused)", ErrBadProof)

// Call is the client side: one handoff to the instance at addr (host:port) with
// key K, quiet to ask for a link and no browser. It returns ErrBadProof for an
// answer that is not covered by K.
func Call(ctx context.Context, addr string, key []byte, quiet bool) (Reply, error) {
	return call(ctx, addr, key, quiet, ActRequest{})
}

// CallAct is Call in the second form (design-menubar §7.1): the action ar.Act with the
// launcher's version, session and token, all inside the request's MAC, and the reply's
// MAC covering the action. A 403 is ErrRefused.
func CallAct(ctx context.Context, addr string, key []byte, quiet bool, ar ActRequest) (Reply, error) {
	if !knownAct(ar.Act) {
		return Reply{}, fmt.Errorf("unknown action %q", ar.Act)
	}
	if !validField(ar.Version, maxVerLen) {
		ar.Version = ""
	}
	if !validField(ar.Session, maxSessLen) {
		ar.Session = ""
	}
	if !validField(ar.Token, maxTokLen) {
		ar.Token = "" // a token out of bounds is not passed on (MB-3 item 5)
	}
	return call(ctx, addr, key, quiet, ar)
}

func call(ctx context.Context, addr string, key []byte, quiet bool, ar ActRequest) (Reply, error) {
	nb := make([]byte, 32)
	rand.Read(nb)
	n := hex.EncodeToString(nb)
	hr := handoffRequest{N: n, P: MAC(key, "handoff-req", n), Quiet: quiet}
	v2 := ar.Act != ""
	if v2 {
		hr = handoffRequest{N: n, P: reqMAC2(key, n, ar.Act, ar.Version, ar.Session, ar.Token), Quiet: quiet,
			Act: ar.Act, Ver: ar.Version, Sess: ar.Session, Tok: ar.Token}
	}
	body, _ := json.Marshal(hr)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+addr+"/_handoff", bytes.NewReader(body))
	if err != nil {
		return Reply{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	c := &http.Client{
		Transport: &http.Transport{DialContext: (&net.Dialer{Timeout: 3 * time.Second}).DialContext, DisableKeepAlives: true},
		// The instance waits up to DefaultWait for the browser to arrive before it
		// answers, so the whole call outlasts that.
		Timeout:       CallTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := c.Do(req)
	if err != nil {
		return Reply{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(resp.Body, maxReplyBody))
		if resp.StatusCode == http.StatusForbidden {
			return Reply{}, ErrRefused
		}
		return Reply{}, fmt.Errorf("%w (status %d)", ErrBadProof, resp.StatusCode)
	}
	var rep handoffReply
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxReplyBody)).Decode(&rep); err != nil {
		return Reply{}, fmt.Errorf("%w (unreadable answer)", ErrBadProof)
	}
	if v2 {
		switch {
		case rep.Busy:
			if !SameMAC(rep.P, MAC(key, "handoff-busy2", n, ar.Act)) {
				return Reply{}, ErrBadProof
			}
			return Reply{Busy: true}, nil
		case ar.Act == ActOpen:
			if !SameMAC(rep.P, MAC(key, "handoff-ok2", n, ar.Act, strconv.FormatBool(rep.Opened), rep.Link)) {
				return Reply{}, ErrBadProof
			}
			return Reply{Opened: rep.Opened, Link: rep.Link}, nil
		}
		if !validReason(rep.Reason) || !SameMAC(rep.P, MAC(key, "handoff-shown2", n, ar.Act, strconv.FormatBool(rep.Shown), rep.Reason)) {
			return Reply{}, ErrBadProof
		}
		return Reply{Shown: rep.Shown, Reason: rep.Reason}, nil
	}
	if rep.Busy {
		if !SameMAC(rep.P, MAC(key, "handoff-busy", n)) {
			return Reply{}, ErrBadProof
		}
		return Reply{Busy: true}, nil
	}
	if !SameMAC(rep.P, MAC(key, "handoff-ok", n, strconv.FormatBool(rep.Opened), rep.Link)) {
		return Reply{}, ErrBadProof
	}
	return Reply{Opened: rep.Opened, Link: rep.Link}, nil
}

// Reach calls the instance, retrying a failed handshake (connection refused, a
// wrong proof, a timeout) every half second for up to window while stillHeld says
// the lock is held — a slow start, or an instance file that was just rewritten —
// and returning the last error after that [CC21]. A quiet caller needs the link,
// so it waits out a busy answer and asks again; a launching client takes busy as
// done.
func Reach(ctx context.Context, addr string, key []byte, quiet bool, window time.Duration, stillHeld func() bool) (Reply, error) {
	deadline := time.Now().Add(window)
	for {
		rep, err := Call(ctx, addr, key, quiet)
		switch {
		case err == nil && !(rep.Busy && quiet):
			return rep, nil
		case err == nil: // busy, and we need a link: let the second pass
			if !sleepUntil(ctx, handoffRate+100*time.Millisecond, deadline) {
				return rep, errors.New("another handoff was in progress for the whole wait")
			}
			continue
		}
		if ctx.Err() != nil || time.Now().After(deadline) || (stillHeld != nil && !stillHeld()) {
			return Reply{}, err
		}
		if !sleepUntil(ctx, 500*time.Millisecond, deadline) {
			return Reply{}, err
		}
	}
}

// sleepUntil sleeps d unless ctx ends or the deadline would pass first.
func sleepUntil(ctx context.Context, d time.Duration, deadline time.Time) bool {
	if time.Now().Add(d).After(deadline) {
		return false
	}
	select {
	case <-time.After(d):
		return true
	case <-ctx.Done():
		return false
	}
}
