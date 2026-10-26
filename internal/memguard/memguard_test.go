package memguard

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
)

// waits reports whether f is still blocked after a short while; done is closed when
// it returns.
func waits(f func()) (done chan struct{}, blocked bool) {
	done = make(chan struct{})
	go func() { f(); close(done) }()
	select {
	case <-done:
		return done, false
	case <-time.After(100 * time.Millisecond):
		return done, true
	}
}

func finished(t *testing.T, done chan struct{}, what string) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("%s never went ahead", what)
	}
}

// covers: MA-794
// Unit reads share one budget: reads that fit go ahead together; one that would pass
// it waits until another gives its bytes back; a release given twice gives them back
// once (§3.7, §7.4).
func TestUnitReadsShareTheBudget(t *testing.T) {
	g := New(100)
	r1, err := g.Reserve(context.Background(), 60)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := g.Reserve(context.Background(), 40)
	if err != nil {
		t.Fatal(err)
	}
	var r3 func()
	done, blocked := waits(func() { r3, _ = g.Reserve(context.Background(), 30) })
	if !blocked {
		t.Fatal("a read past the budget went ahead")
	}
	r1()
	r1()
	finished(t, done, "the read once budget was given back")
	assure.Reached(t, g.Used() == 70, "40 and 30 held, the first read's 60 given back once")
	r2()
	r3()
	if g.Used() != 0 {
		t.Fatalf("%d bytes still held", g.Used())
	}
}

// covers: MA-794
// A read the budget could never hold is refused at once, naming its size in MB, and
// holds nothing; one exactly the budget's size goes ahead (§7.4).
func TestAReadTooLargeForTheBudgetIsRefusedAtOnce(t *testing.T) {
	g := New(3 << 20)
	_, err := g.Reserve(context.Background(), 3<<20+1)
	code := 0
	msg := ""
	if err != nil {
		code, msg = 1, err.Error()
	}
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("too large to open on this computer", "4 MB"),
		assure.NoSideEffect(func() bool { return g.Used() == 0 }))
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("want ErrTooLarge, got %v", err)
	}
	release, err := g.Reserve(context.Background(), 3<<20)
	if err != nil {
		t.Fatalf("a read of exactly the budget: %v", err)
	}
	release()
}

// covers: MA-794
// A derivation waits while unit reads hold budget, and reads wait while it runs; a
// derivation waiting holds back new reads, so a stream of reads cannot starve it
// (§3.7).
func TestADerivationAndUnitReadsTakeTurns(t *testing.T) {
	g := New(100)
	d := g.Derivations()
	read, err := g.Reserve(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	derived, blocked := waits(d.Lock)
	if !blocked {
		t.Fatal("a derivation went ahead while a read held budget")
	}
	var late func()
	lateDone, lateBlocked := waits(func() { late, _ = g.Reserve(context.Background(), 10) })
	if !lateBlocked {
		t.Fatal("a new read went ahead of a waiting derivation")
	}
	read()
	finished(t, derived, "the derivation once the read gave its budget back")
	select {
	case <-lateDone:
		t.Fatal("a read went ahead while the derivation ran")
	case <-time.After(100 * time.Millisecond):
	}
	d.Unlock()
	finished(t, lateDone, "the read once the derivation ended")
	late()
	assure.Reached(t, g.Used() == 0, "every read's budget given back")

	// One derivation at a time.
	d.Lock()
	second, blocked := waits(d.Lock)
	if !blocked {
		t.Fatal("two derivations ran at once")
	}
	d.Unlock()
	finished(t, second, "the second derivation")
	d.Unlock()
}

// covers: MA-794
// A cancelled wait ends with the context's error and holds nothing (§3.2: a stalled
// request never holds memory).
func TestACancelledReadWaitHoldsNothing(t *testing.T) {
	g := New(10)
	held, err := g.Reserve(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var werr error
	done, blocked := waits(func() { _, werr = g.Reserve(ctx, 5) })
	if !blocked {
		t.Fatal("a read past the budget went ahead")
	}
	cancel()
	finished(t, done, "the cancelled wait")
	if !errors.Is(werr, context.Canceled) || g.Used() != 10 {
		t.Fatalf("want context.Canceled holding nothing more, got %v with %d held", werr, g.Used())
	}
	held()
	assure.Reached(t, g.Used() == 0, "nothing held")
}

// covers: MA-794
// Ending a derivation turn not held is a programming error, said loudly.
func TestEndingADerivationTurnNotHeldPanics(t *testing.T) {
	defer func() {
		r := recover()
		assure.Reached(t, r != nil && strings.Contains(r.(string), "not held"), "the panic")
	}()
	New(1).Derivations().Unlock()
}
