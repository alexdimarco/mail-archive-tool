// Package memguard is the process-wide memory guard of an encrypted archive's
// reader (design-encrypted-archives-phase3 §3.7, §7.4): one key derivation at a
// time — each may take up to the cost ceiling's gigabyte — and the reader's whole
// unit reads within one shared budget. A derivation waits while unit reads hold
// budget, and unit reads wait while a derivation runs or waits, so the process peak
// is the larger of the two, not their sum.
package memguard

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// UnitBudget is the unit reads' shared budget: a message at the 1 GiB attachment
// ceiling, read whole (about twice its sealed size), opens.
const UnitBudget int64 = 9 << 28 // 2.25 GiB

// ErrTooLarge refuses a read that the budget could never hold.
var ErrTooLarge = errors.New("memguard: too large to open on this computer")

// Guard is one memory guard. The zero value is not usable; use New.
type Guard struct {
	mu       sync.Mutex
	cond     *sync.Cond
	budget   int64
	used     int64
	deriving bool
	waiting  int // derivations waiting: new reads wait behind them
}

// New is a guard whose unit reads share budget bytes.
func New(budget int64) *Guard {
	g := &Guard{budget: budget}
	g.cond = sync.NewCond(&g.mu)
	return g
}

// Process is this process's guard.
var Process = New(UnitBudget)

// Derivations is the guard's derivation turn as a sync.Locker: Lock waits until no
// derivation runs and no unit read holds budget, and holds back new reads while it
// waits; Unlock ends the turn.
func (g *Guard) Derivations() sync.Locker { return derivations{g} }

type derivations struct{ g *Guard }

func (d derivations) Lock() {
	g := d.g
	g.mu.Lock()
	g.waiting++
	for g.deriving || g.used > 0 {
		g.cond.Wait()
	}
	g.waiting--
	g.deriving = true
	g.mu.Unlock()
}

func (d derivations) Unlock() {
	g := d.g
	g.mu.Lock()
	if !g.deriving {
		g.mu.Unlock()
		panic("memguard: Unlock of a derivation turn not held")
	}
	g.deriving = false
	g.cond.Broadcast()
	g.mu.Unlock()
}

// Reserve takes n bytes of the unit budget for a whole unit read, waiting while a
// derivation runs or waits, or while other reads hold too much. A read the budget
// could never hold is refused at once (ErrTooLarge); a cancelled ctx ends the wait.
// The release it returns gives the bytes back; calling it again does nothing.
func (g *Guard) Reserve(ctx context.Context, n int64) (release func(), err error) {
	if n < 0 {
		n = 0
	}
	if n > g.budget {
		return nil, fmt.Errorf("%w (%d MB)", ErrTooLarge, (n+(1<<20)-1)>>20)
	}
	stop := context.AfterFunc(ctx, func() {
		g.mu.Lock()
		g.cond.Broadcast()
		g.mu.Unlock()
	})
	defer stop()
	g.mu.Lock()
	defer g.mu.Unlock()
	for g.deriving || g.waiting > 0 || g.used+n > g.budget {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		g.cond.Wait()
	}
	g.used += n
	var once sync.Once
	return func() {
		once.Do(func() {
			g.mu.Lock()
			g.used -= n
			g.cond.Broadcast()
			g.mu.Unlock()
		})
	}, nil
}

// Used is the budget the unit reads hold now.
func (g *Guard) Used() int64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.used
}
