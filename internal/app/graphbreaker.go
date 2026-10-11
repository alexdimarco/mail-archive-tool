package app

import (
	"fmt"
	"time"

	"mail-archive-tool/internal/graph"
	"mail-archive-tool/internal/state"
)

// The Graph breaker (fault-isolation §4.8.5, FC10): a rate bound and a time bound
// on the faults that are about the service rather than the item, so an outage costs
// a bounded number of timeouts per mailbox and not one per message, and a hostile
// sender cannot stop a mailbox walk by mailing poison. Every threshold is fixed by
// design; the time bound is the one a test scales.

const (
	breakerRun         = 25               // consecutive counted faults with no successful download between: the fast path
	breakerWindow      = 50               // the attempts the rate bound reads
	breakerRate        = 25               // counted faults in a full window: at least 50% of the last 50
	defaultBreakerTime = 30 * time.Minute // cumulative time spent in counted faults in one mailbox
)

// What one attempt came to.
type attemptKind int

const (
	attemptOK        attemptKind = iota // a download that succeeded: it ends the run of consecutive faults
	attemptItemFault                    // a fault about the item (an item-class fault, or known poison): never counted
	attemptCounted                      // a fault about the service (transport, timeout): counted
)

// graphBreaker counts one mailbox's attempts. It decides nothing by itself: when
// a bound is crossed it says which, and the walk makes the control request that
// decides whether the service or the items are failing.
type graphBreaker struct {
	timeBound   time.Duration
	consecutive int
	ring        [breakerWindow]bool // the last attempts: whether each was a counted fault
	filled      int
	next        int
	faults      int           // counted faults in the ring
	spent       time.Duration // time spent in counted faults
}

func newGraphBreaker(timeBound time.Duration) *graphBreaker {
	if timeBound <= 0 {
		timeBound = defaultBreakerTime
	}
	return &graphBreaker{timeBound: timeBound}
}

// attempt records one attempt and, for a counted fault that took took, says which
// bound it crossed ("" for none): the words the refusal prints (§7.5). The rate
// bound reads only a full window, so a run of faults that ends a short listing is
// not 50% of anything.
func (b *graphBreaker) attempt(kind attemptKind, took time.Duration) (fired string) {
	if b.filled == breakerWindow {
		if b.ring[b.next] {
			b.faults--
		}
	} else {
		b.filled++
	}
	counted := kind == attemptCounted
	b.ring[b.next] = counted
	b.next = (b.next + 1) % breakerWindow
	switch kind {
	case attemptOK:
		b.consecutive = 0
		return ""
	case attemptItemFault:
		return ""
	}
	b.faults++
	b.consecutive++
	b.spent += took
	switch {
	case b.consecutive >= breakerRun:
		return fmt.Sprintf("%d downloads in a row failed", b.consecutive)
	case b.filled == breakerWindow && b.faults >= breakerRate:
		return fmt.Sprintf("%d of the last %d downloads failed", b.faults, breakerWindow)
	case b.spent > b.timeBound:
		return fmt.Sprintf("failed downloads had taken %s", b.spent.Round(time.Second))
	}
	return ""
}

// reset forgets the counts: a control request answered, so what failed was the
// items, and the walk starts counting again.
func (b *graphBreaker) reset() {
	*b = graphBreaker{timeBound: b.timeBound}
}

// GraphBreakerError ends one mailbox's walk: its downloads kept failing for the
// service's sake, and a control request unrelated to them failed too (§7.5).
// What the mailbox had archived is kept, and nothing was marked deleted.
type GraphBreakerError struct {
	Mailbox   string
	Threshold string // the bound that fired, in words
	Last      string // the last fault's text
	Out       string // the archive location, for the status command named in the text
}

func (e *GraphBreakerError) Error() string {
	return cleanText(fmt.Sprintf("mailbox %s: stopped after %s and a control request also failed (last: %s) — Microsoft 365 or the network appears to be unavailable. Messages archived so far are kept and nothing was marked deleted. Re-run when the service is reachable; `mailarchive status -out %q` shows what is missing.",
		e.Mailbox, e.Threshold, e.Last, e.Out))
}

// RunBreakerError ends the run: two mailboxes in a row tripped, so the service or
// the network is down, and the rest would only pay the same budget each (§7.5).
type RunBreakerError struct {
	First, Second *GraphBreakerError
}

func (e *RunBreakerError) Error() string {
	return cleanText(fmt.Sprintf("stopped the run after two mailboxes in a row could not reach Microsoft 365 (%s: %s; %s: %s) — the service or the network appears to be unavailable; re-run when it is reachable.",
		e.First.Mailbox, e.First.Threshold, e.Second.Mailbox, e.Second.Threshold))
}

// knownPoison says whether a fault on ref is evidence about the item and not about
// the service: the item already holds an open gap, from an earlier run, with the
// same reason. It never counts toward the breaker, whatever the control request
// returns (§4.8.5, RR6-C4); it only reads the ledger after the fetch has failed, to
// classify the failure, and never to decide whether to fetch (P5).
func (w *mailboxWalk) knownPoison(ref graph.MessageRef, reason string) bool {
	g, ok := w.manifest.GapFor(state.GapKey(w.token, state.GapScopeItem, graphLocator(ref)))
	return ok && g.Reason == reason && !g.LastRun.Equal(w.lr.runAt)
}
