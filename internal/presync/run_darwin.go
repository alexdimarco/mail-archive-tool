//go:build darwin

package presync

import (
	"context"
	"hash/fnv"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// newEnv builds the production macOS env; a test overrides it to inject fakes.
var newEnv = func() *macEnv { return newMacEnv("") }

// sessionBudget bounds the WHOLE pre-sync, a little beyond the §4.11 worst case
// (await-up + max-wait + quit-grace + slack). filepath.WalkDir / os.ReadDir /
// DirEntry.Info cannot honour a context, so a watch root on an unresponsive
// mount would otherwise block forever; the watchdog in Run abandons such a
// session at this bound so the archive still runs (R32). A var so a test shrinks it.
var sessionBudget = defAwaitUp + defMaxWait + defQuitGrace + 30*time.Second

// runSessionImpl is the session body the watchdog wraps; a test overrides it to
// drive the watchdog without a real session.
var runSessionImpl = runWith

// Run performs one pre-sync on macOS (design §4): resolve and validate at S1,
// then run the S1..S9 session, under a whole-session watchdog. It NEVER fails
// and NEVER unboundedly blocks the archive — every path ends in a Result within
// sessionBudget, and the archive reads afterwards (R32).
func Run(ctx context.Context, cfg Config) Result {
	e := newEnv()
	ch := make(chan Result, 1) // buffered: an abandoned goroutine can still send
	go func() { ch <- runSessionImpl(ctx, cfg, e) }()
	select {
	case r := <-ch:
		return r
	case <-time.After(sessionBudget):
		// A read-only syscall on an unresponsive watch root stalled past the whole
		// session bound. Abandon the goroutine — it only reads and asks, so R31
		// holds — and warn; the archive reads anyway (R32). The goroutine ends with
		// the process (a one-shot CLI); a long-lived scheduler owes a stall-notice
		// (TP4).
		return Result{Outcome: OutcomeInternalError}
	}
}

func runWith(ctx context.Context, cfg Config, e *macEnv) Result {
	plan, outcome, ok := e.resolve(cfg.Profile, cfg.ProfileNamed)
	if !ok {
		narrate(cfg.Log, SummaryLine(outcome)) // the refusal reason; nothing started
		return Result{Outcome: outcome}
	}
	e.bundle = plan.bundle
	narrate(cfg.Log, startLine) // the wait can be minutes — say so before it begins
	res := runSession(ctx, e.buildDeps(plan, cfg))
	narrate(cfg.Log, SummaryLine(res.outcome))
	return Result{Outcome: res.outcome}
}

// buildDeps assembles the session inputs from the plan and the config: the fixed
// watch roots (§4.4.2/§4.3), the state record and lock under the state directory
// (never the archive, §4.6), the record template, and the default timings.
func (e *macEnv) buildDeps(plan macPlan, cfg Config) deps {
	_ = os.MkdirAll(cfg.StateDir, 0o700)
	hash := profileHash(plan.profile)
	return deps{
		env:       e,
		cfg:       quietConfig{settle: defSettle, quiet: defQuiet, maxWait: defMaxWait},
		app:       plan.exe,
		profile:   plan.profile,
		watch:     watchRoots(plan.profile, cfg.Inputs),
		statePath: filepath.Join(cfg.StateDir, "presync-"+hash+".json"),
		lockPath:  filepath.Join(cfg.StateDir, "presync-"+hash+".lock"),
		base: record{
			Version:   recordVersion,
			Profile:   plan.profile,
			App:       plan.exe,
			ToolPID:   os.Getpid(),
			ToolStart: time.Now().Unix(),
		},
		awaitPoll: defAwaitPoll,
		awaitUp:   defAwaitUp,
		pollEvery: defPollEvery,
		quitGrace: defQuitGrace,
		idleAway:  defIdleAway,
	}
}

// watchRoots are the mail directories a session watches for quiet (§4.4.2): the
// profile's ImapMail and Mail, plus any archive input that lies inside the
// profile. Only existing directories are returned; nothing outside the profile
// is ever watched (§4.3: inputs not inside it are archived as named, not watched).
func watchRoots(profile string, inputs []string) []string {
	var roots []string
	seen := map[string]bool{}
	add := func(p string) {
		if p == "" || seen[p] {
			return
		}
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			seen[p] = true
			roots = append(roots, p)
		}
	}
	add(filepath.Join(profile, "ImapMail"))
	add(filepath.Join(profile, "Mail"))
	for _, in := range inputs {
		if c, err := filepath.EvalSymlinks(in); err == nil && underDir(profile, c) {
			add(c)
		}
	}
	return roots
}

// underDir reports whether path is dir itself or lies inside it.
func underDir(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func profileHash(profile string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(profile))
	return strconv.FormatUint(h.Sum64(), 16)
}
