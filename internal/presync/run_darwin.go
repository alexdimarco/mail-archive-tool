//go:build darwin

package presync

import "context"

// newEnv builds the production macOS env; a test overrides it to inject fakes.
var newEnv = func() *macEnv { return newMacEnv("") }

// Run performs one pre-sync on macOS (design §4): resolve and validate at S1,
// then run the S1..S9 session, under the shared whole-session watchdog (run.go).
// It NEVER fails and NEVER unboundedly blocks the archive (R32).
func Run(ctx context.Context, cfg Config) Result {
	return runWatchdog(func() Result { return runWith(ctx, cfg, newEnv()) })
}

func runWith(ctx context.Context, cfg Config, e *macEnv) Result {
	plan, outcome, ok := e.resolve(cfg.Profile, cfg.ProfileNamed, cfg.Inputs)
	if !ok {
		narrate(cfg.Log, SummaryLine(outcome)) // the refusal reason; nothing started
		return Result{Outcome: outcome}
	}
	e.bundle = plan.bundle
	narrate(cfg.Log, startLine) // the wait can be minutes — say so before it begins
	res := runSession(ctx, buildDeps(e, plan.exe, plan.profile, cfg))
	narrate(cfg.Log, SummaryLine(res.outcome))
	return Result{Outcome: res.outcome, Release: res.release}
}
