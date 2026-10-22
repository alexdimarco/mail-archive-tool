package presync

import (
	"hash/fnv"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// This file is the platform-neutral run scaffolding shared by every OS adapter
// (PC-W9): the whole-session watchdog, the deps assembly, and the watch-root /
// profile-hash helpers. Each adapter's Run is a thin wrapper around runWatchdog,
// so the R32 safety watchdog is written and reviewed ONCE, never copied per
// platform where it could drift.

// sessionBudget bounds the WHOLE pre-sync, a little beyond the §4.11 worst case
// (await-up + max-wait + quit-grace + slack). filepath.WalkDir / os.ReadDir /
// DirEntry.Info cannot honour a context, so a watch root on an unresponsive
// mount would otherwise block forever; the watchdog abandons such a session at
// this bound so the archive still runs (R32). A var so a test can shrink it.
var sessionBudget = defAwaitUp + defMaxWait + defQuitGrace + 30*time.Second

// runWatchdog runs fn (one platform's whole pre-sync) under sessionBudget. If fn
// stalls in an un-interruptible syscall past the bound, its goroutine is
// abandoned — it only reads and asks, so R31 holds — and a WARN is returned so
// the archive reads anyway (R32). The abandoned goroutine ends with the process.
func runWatchdog(fn func() Result) Result {
	ch := make(chan Result, 1) // buffered: an abandoned goroutine can still send
	go func() { ch <- fn() }()
	select {
	case r := <-ch:
		return r
	case <-time.After(sessionBudget):
		return Result{Outcome: OutcomeInternalError}
	}
}

// buildDeps assembles the session inputs from the resolved executable and profile
// and the config: the fixed watch roots (§4.4.2/§4.3), the state record and lock
// under the state directory (never the archive, §4.6), the record template, and
// the default timings. Platform-neutral — each adapter resolves exe+profile its
// own way, then hands them here.
func buildDeps(e env, exe, profile string, cfg Config) deps {
	_ = os.MkdirAll(cfg.StateDir, 0o700)
	hash := profileHash(profile)
	return deps{
		env:       e,
		cfg:       quietConfig{settle: defSettle, quiet: defQuiet, maxWait: defMaxWait},
		app:       exe,
		profile:   profile,
		watch:     watchRoots(profile, cfg.Inputs),
		statePath: filepath.Join(cfg.StateDir, "presync-"+hash+".json"),
		lockPath:  filepath.Join(cfg.StateDir, "presync-"+hash+".lock"),
		base: record{
			Version:   recordVersion,
			Profile:   profile,
			App:       exe,
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
