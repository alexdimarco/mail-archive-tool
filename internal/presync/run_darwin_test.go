//go:build darwin

package presync

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// covers: MA-572
// Run refuses a pre-sync under elevation and starts nothing: no session runs and
// no state directory is created.
func TestRunWithRefusesElevation(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "presync")
	e := newMacEnv("")
	e.geteuid = func() int { return 0 }
	ran := false
	e.run = func(*exec.Cmd) (string, string, error) { ran = true; return "", "", nil }

	var log bytes.Buffer
	res := runWith(context.Background(), Config{Profile: "/p", ProfileNamed: true, StateDir: stateDir, Log: &log}, e)
	if res.Outcome != OutcomeRefusedElevated {
		t.Errorf("outcome = %q, want refused-elevated", res.Outcome.Name)
	}
	if ran {
		t.Error("a helper ran despite the elevation refusal")
	}
	if _, err := os.Stat(stateDir); !os.IsNotExist(err) {
		t.Errorf("the state directory was created on a refusal: %v", err)
	}
	// The owner is told why the manual run skipped the sync (the refusal is narrated).
	if !strings.Contains(log.String(), "root or an administrator") {
		t.Errorf("refusal was not narrated to the log: %q", log.String())
	}
}

// covers: MA-560
// A session that stalls (e.g. a watch root on an unresponsive mount, where
// WalkDir cannot honour a deadline) is abandoned by Run's watchdog at
// sessionBudget and returns a WARN within the budget, so the archive is never
// blocked (R32).
func TestRunWatchdogBoundsAStalledSession(t *testing.T) {
	origImpl, origBudget := runSessionImpl, sessionBudget
	defer func() { runSessionImpl, sessionBudget = origImpl, origBudget }()

	sessionBudget = 50 * time.Millisecond
	blocked := make(chan struct{})
	runSessionImpl = func(ctx context.Context, cfg Config, e *macEnv) Result {
		<-blocked // a stuck syscall that never returns within the budget
		return Result{Outcome: OutcomeOK}
	}

	start := time.Now()
	res := Run(context.Background(), Config{})
	if res.Outcome != OutcomeInternalError {
		t.Errorf("outcome = %q, want internal-error (the watchdog fired)", res.Outcome.Name)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("Run did not honour sessionBudget: took %v", elapsed)
	}
	close(blocked) // release the abandoned goroutine
}

// covers: MA-555, MA-564
// buildDeps watches only the profile's ImapMail/Mail and inputs inside the
// profile (never one outside it), puts the state record and lock under the state
// directory (never the archive), and uses the default timings.
func TestBuildDeps(t *testing.T) {
	profile := t.TempDir()
	mustMkdir(t, filepath.Join(profile, "ImapMail"))
	mustMkdir(t, filepath.Join(profile, "Mail"))
	inside := filepath.Join(profile, "Mail", "Local Folders")
	mustMkdir(t, inside)
	outside := t.TempDir() // a store not under the profile

	stateDir := filepath.Join(t.TempDir(), "state")
	e := newMacEnv("")
	cp, _ := filepath.EvalSymlinks(profile)
	plan := macPlan{
		bundle:  "/Applications/Thunderbird.app",
		exe:     "/Applications/Thunderbird.app/Contents/MacOS/thunderbird",
		profile: cp,
	}
	d := e.buildDeps(plan, Config{Inputs: []string{inside, outside}, StateDir: stateDir})

	want := map[string]bool{
		filepath.Join(cp, "ImapMail"): true,
		filepath.Join(cp, "Mail"):     true,
	}
	if ci, err := filepath.EvalSymlinks(inside); err == nil {
		want[ci] = true
	}
	for _, w := range d.watch {
		if !want[w] {
			t.Errorf("unexpected watch root %q (an input outside the profile must not be watched)", w)
		}
		delete(want, w)
	}
	if len(want) != 0 {
		t.Errorf("missing watch roots: %v", want)
	}

	if filepath.Dir(d.statePath) != stateDir || filepath.Dir(d.lockPath) != stateDir {
		t.Errorf("state/lock not under the state dir: %q, %q", d.statePath, d.lockPath)
	}
	if !strings.HasPrefix(filepath.Base(d.statePath), "presync-") || filepath.Ext(d.statePath) != ".json" {
		t.Errorf("unexpected state path name: %q", d.statePath)
	}
	if d.cfg.quiet != defQuiet || d.cfg.maxWait != defMaxWait || d.idleAway != defIdleAway || d.quitGrace != defQuitGrace {
		t.Errorf("timings are not the defaults: cfg=%+v idle=%v grace=%v", d.cfg, d.idleAway, d.quitGrace)
	}
	if d.app != plan.exe || d.profile != cp {
		t.Errorf("deps app/profile = %q / %q", d.app, d.profile)
	}
}
