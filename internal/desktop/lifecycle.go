package desktop

import (
	"context"
	"sync"
	"time"
)

// DrainBudget is how long a stop gives the running export to finalize (XC6, MA-514).
const DrainBudget = 15 * time.Second

// Lifecycle is the process's own life as the dashboard sees it (extraction design §4.3,
// XC6): the root context main's signal handler cancels, and a real shutdown path. The
// Graph session, and every preview and run, are built on Context, never on a request's
// context, because NewDelegated keeps its context inside the refreshing token source,
// which oauth2 reuses on every refresh: a session built on r.Context() would stop
// refreshing the moment that request ended. When the root context is cancelled the
// lifecycle drains by itself: it cancels the running export and waits for it to finalize
// (its manifest says cancelled, its marker CANCELLED), within DrainBudget.
type Lifecycle struct {
	ctx context.Context

	mu      sync.Mutex
	drains  []func(context.Context) error
	once    sync.Once
	drained chan struct{}
	err     error
}

// NewLifecycle is the lifecycle of a process whose root context is ctx. When ctx is
// cancelled it drains, within DrainBudget.
func NewLifecycle(ctx context.Context) *Lifecycle {
	l := &Lifecycle{ctx: ctx, drained: make(chan struct{})}
	if ctx.Done() != nil {
		go func() {
			<-ctx.Done()
			dctx, cancel := context.WithTimeout(context.Background(), DrainBudget)
			defer cancel()
			l.Drain(dctx)
		}()
	}
	return l
}

// Context is the process-lifetime context. A nil Lifecycle (a test, the informational
// shell) is the background context.
func (l *Lifecycle) Context() context.Context {
	if l == nil || l.ctx == nil {
		return context.Background()
	}
	return l.ctx
}

// OnDrain registers fn to be run by Drain.
func (l *Lifecycle) OnDrain(fn func(context.Context) error) {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.drains = append(l.drains, fn)
	l.mu.Unlock()
}

// Drain runs every registered drain once, in order, within ctx's budget, and returns the
// first error. A second call waits for the first and returns its result.
func (l *Lifecycle) Drain(ctx context.Context) error {
	if l == nil {
		return nil
	}
	l.once.Do(func() {
		defer close(l.drained)
		l.mu.Lock()
		fns := append([]func(context.Context) error(nil), l.drains...)
		l.mu.Unlock()
		for _, fn := range fns {
			if err := fn(ctx); err != nil && l.err == nil {
				l.err = err
			}
		}
	})
	<-l.drained
	return l.err
}

// Drained is closed once the drain has finished.
func (l *Lifecycle) Drained() <-chan struct{} {
	if l == nil {
		c := make(chan struct{})
		close(c)
		return c
	}
	return l.drained
}
