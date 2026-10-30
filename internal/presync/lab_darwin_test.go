//go:build darwin

package presync

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

// covers: MA-573
// TestLabMacLaunchAndClose is the macOS viability lab (design §10, §4.7.1): the
// one thing no unit test can settle — whether the REAL Thunderbird, started
// hidden by `open -g -j`, is closed cleanly by the single AppleScript quit
// request. SKIPPED unless PRESYNC_LAB=1 and PRESYNC_LAB_PROFILE are set, because
// it LAUNCHES and asks to CLOSE the real Thunderbird for that profile. Run ONLY in
// an INTERACTIVE macOS session (so the one-time Automation prompt can be granted)
// and ONLY against a TEST profile — never your primary mail.
//
// It judges the close HONESTLY: a clean exit is OUR close only when Automation was
// permitted; a quit-not-permitted outcome (the grant was not given) still proves
// the launch + the single bounded quit request, with Thunderbird LEFT RUNNING,
// never forced — exactly the designed S8 WARN. An empty test profile's Thunderbird
// can also self-exit, which is not proof the quit worked; the lab says so.
func TestLabMacLaunchAndClose(t *testing.T) {
	if os.Getenv("PRESYNC_LAB") != "1" {
		t.Skip("viability lab: set PRESYNC_LAB=1 and PRESYNC_LAB_PROFILE (launches+closes the REAL Thunderbird; interactive macOS session + TEST profile only)")
	}
	profile := os.Getenv("PRESYNC_LAB_PROFILE")
	if profile == "" {
		t.Fatal("set PRESYNC_LAB_PROFILE to a TEST Thunderbird profile directory (never your primary mail)")
	}

	e := newMacEnv("")
	plan, outcome, ok := e.resolve(profile, true, nil) // named=true: the operator chose this profile
	if !ok {
		t.Fatalf("resolve failed (%v): is Thunderbird installed and is %q a valid profile with prefs.js?", outcome, profile)
	}
	e.bundle = plan.bundle
	t.Logf("resolved: exe=%q profile=%q", plan.exe, plan.profile)

	before := pidSet(e.thunderbirdProcs())
	t.Logf("%d thunderbird process(es) before launch", len(before))

	launchAt := e.now()
	if err := e.startHidden(plan.exe, plan.profile); err != nil {
		t.Fatalf("startHidden: %v", err)
	}

	// Find the process we started by the -profile match (handles a launcher re-exec).
	lc := launchCtx{app: plan.exe, profile: plan.profile, launchAt: launchAt, snapshot: before}
	var child proc
	found := false
	for deadline := launchAt.Add(30 * time.Second); e.now().Before(deadline); e.sleep(nil, nil, 500*time.Millisecond) {
		if m, matched := findOurs(e.thunderbirdProcs(), lc); matched {
			child, found = m, true
			t.Logf("matched our hidden process pid=%d", m.pid)
			break
		}
	}
	if !found {
		t.Fatal("LAB FAIL: no hidden thunderbird matched by -profile within 30s")
	}

	e.sleep(nil, nil, 5*time.Second) // let it settle a moment before the quit

	if !e.pidAlive(child.pid) {
		t.Skip("INCONCLUSIVE: the process self-exited during the settle (an empty profile can) — re-run against a profile where Thunderbird stays running to test the close")
	}

	qctx, qcancel := context.WithTimeout(context.Background(), quitTimeout) // the session's bound (C-5)
	quitErr := e.quit(qctx, child)
	qcancel()
	quitAt := e.now()
	closed := false
	for e.now().Sub(quitAt) < 30*time.Second {
		if !e.pidAlive(child.pid) {
			closed = true
			break
		}
		e.sleep(nil, nil, 500*time.Millisecond)
	}

	switch {
	case errors.Is(quitErr, errQuitNotPermitted):
		t.Logf("LAB (inconclusive close): the quit request was sent but Automation was not permitted (presync_quit_not_permitted) — Thunderbird is left running, never forced (the designed S8 WARN). Grant Mail Archive control of Thunderbird in System Settings → Privacy & Security → Automation, then re-run for the definitive clean-close result. still-alive=%v", !closed)
	case quitErr != nil:
		t.Fatalf("LAB FAIL: quit returned an unexpected error: %v", quitErr)
	case !closed:
		t.Fatal("LAB FAIL: the quit request was permitted but Thunderbird did not exit within 30s")
	default:
		t.Log("LAB PASS: Thunderbird was started hidden and closed cleanly by the AppleScript quit request. HUMAN-CHECK: relaunch and confirm no crash-recovery prompt and no leftover process.")
	}
}
