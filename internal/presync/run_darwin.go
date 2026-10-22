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

// Run performs one pre-sync on macOS (design §4): resolve and validate at S1,
// then run the S1..S9 session. It NEVER fails the archive — every path ends in a
// Result the caller records, and the archive reads afterwards (R32). ctx is part
// of the stable signature; the session's own bounded deadlines govern v1.
func Run(ctx context.Context, cfg Config) Result {
	return runWith(cfg, newEnv())
}

func runWith(cfg Config, e *macEnv) Result {
	plan, outcome, ok := e.resolve(cfg.Profile, cfg.ProfileNamed)
	if !ok {
		return Result{Outcome: outcome} // a refusal: nothing started, no record written
	}
	e.bundle = plan.bundle
	return Result{Outcome: runSession(e.buildDeps(plan, cfg)).outcome}
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
