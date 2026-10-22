//go:build windows

package presync

import "context"

// newEnv builds the production Windows env; a test overrides it to inject fakes.
var newEnv = func() *winEnv { return newWinEnv() }

// Run performs one pre-sync on Windows (design §4): resolve and validate at S1,
// then run the shared S1..S9 session, under the shared whole-session watchdog
// (run.go). It NEVER fails and NEVER unboundedly blocks the archive (R32). The
// engine is inert until a caller invokes it; the scheduled wiring that would call
// it is gated on the L-W4 viability lab (the Q16 rule).
func Run(ctx context.Context, cfg Config) Result {
	return runWatchdog(func() Result { return runWith(ctx, cfg, newEnv()) })
}

func runWith(ctx context.Context, cfg Config, e *winEnv) Result {
	plan, outcome, ok := e.resolve(cfg.Profile, cfg.ProfileNamed)
	if !ok {
		narrate(cfg.Log, SummaryLine(outcome)) // the refusal reason; nothing started
		return Result{Outcome: outcome}
	}
	narrate(cfg.Log, startLine) // the wait can be minutes — say so before it begins
	res := runSession(ctx, buildDeps(e, plan.exe, plan.profile, cfg))
	narrate(cfg.Log, SummaryLine(res.outcome))
	return Result{Outcome: res.outcome}
}
