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

	mu   sync.Mutex
	last time.Time
}

type handoffRequest struct {
	N     string `json:"n"`
	P     string `json:"p"`
	Quiet bool   `json:"quiet"`
}

type handoffReply struct {
	Opened bool   `json:"opened"`
	Busy   bool   `json:"busy,omitempty"`
	Link   string `json:"link,omitempty"`
	P      string `json:"p"`
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

	if !SameMAC(req.P, MAC(h.Key, "handoff-req", req.N)) {
		http.Error(w, "refused", http.StatusForbidden)
		return
	}

	h.mu.Lock()
	now := h.now()
	if !h.last.IsZero() && now.Sub(h.last) < handoffRate {
		h.mu.Unlock()
		writeReply(w, handoffReply{Busy: true, P: MAC(h.Key, "handoff-busy", req.N)})
		return
	}
	h.last = now
	h.mu.Unlock()

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
}

// ErrBadProof means the listener answered but did not prove it holds the key: a
// squatter, another account's instance, or an old program. The caller opens
// nothing at that address.
var ErrBadProof = errors.New("the listener did not prove it is this account's MailArchive")

// Call is the client side: one handoff to the instance at addr (host:port) with
// key K, quiet to ask for a link and no browser. It returns ErrBadProof for an
// answer that is not covered by K.
func Call(ctx context.Context, addr string, key []byte, quiet bool) (Reply, error) {
	nb := make([]byte, 32)
	rand.Read(nb)
	n := hex.EncodeToString(nb)
	body, _ := json.Marshal(handoffRequest{N: n, P: MAC(key, "handoff-req", n), Quiet: quiet})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+addr+"/_handoff", bytes.NewReader(body))
	if err != nil {
		return Reply{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	c := &http.Client{
		Transport: &http.Transport{DialContext: (&net.Dialer{Timeout: 3 * time.Second}).DialContext, DisableKeepAlives: true},
		// The instance waits up to DefaultWait for the browser to arrive before it
		// answers, so the whole call outlasts that.
		Timeout:       DefaultWait + 10*time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := c.Do(req)
	if err != nil {
		return Reply{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, io.LimitReader(resp.Body, maxReplyBody))
		return Reply{}, fmt.Errorf("%w (status %d)", ErrBadProof, resp.StatusCode)
	}
	var rep handoffReply
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxReplyBody)).Decode(&rep); err != nil {
		return Reply{}, fmt.Errorf("%w (unreadable answer)", ErrBadProof)
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
