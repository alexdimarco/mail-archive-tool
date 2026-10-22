//go:build windows

package presync

import (
	"os"
	"testing"
	"time"
)

// covers: MA-571, MA-570
// TestLabHiddenLaunchAndClose is the L-W3/L-W4 viability lab (design §10): the one
// thing no unit test can settle — whether a HIDDEN Thunderbird presents a window
// that WM_CLOSE closes cleanly. It is SKIPPED unless PRESYNC_LAB=1 and
// PRESYNC_LAB_PROFILE point at a Thunderbird profile, because it LAUNCHES and then
// asks to CLOSE the real Thunderbird for that profile.
//
// RUN IT ONLY:
//   - in an INTERACTIVE Windows session (it needs the desktop + a signed-in
//     Thunderbird; a SYSTEM/session-0 context such as `prlctl exec` cannot see
//     the GUI), and
//   - against a TEST / throwaway profile, NEVER your primary mail — because
//     whether the close is clean is exactly the open question this lab answers.
//
// It ASSERTS the viability core (our scan+match finds the hidden process, L-W3;
// WM_CLOSE makes it exit within the grace, L-W4) and LOGS the rest for a human to
// read: whether a window was visible (a flash), whether thunderbird.exe re-exec'd
// to a new pid, whether a single main window was found, and whether any process
// was left behind. After a PASS, a human still confirms: relaunch Thunderbird and
// see NO crash-recovery/compaction prompt and NO surviving tray process.
func TestLabHiddenLaunchAndClose(t *testing.T) {
	if os.Getenv("PRESYNC_LAB") != "1" {
		t.Skip("viability lab: set PRESYNC_LAB=1 and PRESYNC_LAB_PROFILE (launches+closes the REAL Thunderbird; interactive session + TEST profile only)")
	}
	profile := os.Getenv("PRESYNC_LAB_PROFILE")
	if profile == "" {
		t.Fatal("set PRESYNC_LAB_PROFILE to a TEST Thunderbird profile directory (never your primary mail)")
	}

	e := newWinEnv()
	plan, outcome, ok := e.resolve(profile, true) // named=true: the operator chose this profile
	if !ok {
		t.Fatalf("resolve failed (%v): is Thunderbird installed and is %q a valid profile with prefs.js?", outcome, profile)
	}
	t.Logf("resolved: exe=%q profile=%q", plan.exe, plan.profile)

	before := pidSet(e.thunderbirdProcs())
	t.Logf("L-W3: %d thunderbird.exe process(es) before launch", len(before))

	launchAt := e.now()
	if err := e.startHidden(plan.exe, plan.profile); err != nil {
		t.Fatalf("startHidden: %v", err)
	}

	// L-W3: find our hidden process by scan + -profile (handles a launcher re-exec).
	lc := launchCtx{app: plan.exe, profile: plan.profile, launchAt: launchAt, snapshot: before}
	var child proc
	found := false
	for deadline := launchAt.Add(30 * time.Second); e.now().Before(deadline); e.sleep(500 * time.Millisecond) {
		procs := e.thunderbirdProcs()
		if m, matched := findOurs(procs, lc); matched {
			child, found = m, true
			t.Logf("L-W3: matched our process pid=%d start=%s (%d thunderbird.exe total now — >1 means a re-exec, which the scan+match handled)",
				m.pid, m.start.Format(time.RFC3339), len(procs))
			break
		}
	}
	if !found {
		t.Fatal("L-W3 FAIL: no hidden thunderbird.exe matched by -profile within 30s (launcher re-exec not found, or launch did nothing)")
	}
	if hwnd, haveWin := e.w.mainWindow(uint32(child.pid)); haveWin {
		t.Logf("L-W3/L-W4: a single main window WAS found for our hidden pid (hwnd=%#x) — WM_CLOSE has a target", hwnd)
	} else {
		t.Logf("L-W3/L-W4 WARNING: NO single visible main window for our hidden pid. If this holds, WM_CLOSE has no target and the feature cannot quit a hidden Thunderbird — the central L-W4 risk; record it for the design (the 'visible main window vs hidden launch' question).")
	}

	e.sleep(5 * time.Second) // a real run waits for the mail files to go quiet; here, a fixed settle

	// L-W4: ask it to close (WM_CLOSE only).
	if err := e.quit(child); err != nil {
		t.Fatalf("quit returned an error (should be nil on every path): %v", err)
	}
	quitAt := e.now()
	closed := false
	for e.now().Sub(quitAt) < 30*time.Second {
		if !e.pidAlive(child.pid) {
			closed = true
			t.Logf("L-W4: our pid exited %s after WM_CLOSE", e.now().Sub(quitAt).Round(time.Millisecond))
			break
		}
		e.sleep(500 * time.Millisecond)
	}
	remaining := e.thunderbirdProcs()
	t.Logf("L-W4: %d thunderbird.exe process(es) remain after the close", len(remaining))
	if !closed {
		t.Fatalf("L-W4 FAIL: our pid did not exit within 30s of WM_CLOSE — left running (a hidden modal, or no window to close). %d thunderbird.exe remain.", len(remaining))
	}
	t.Log("L-W4 CORE PASS. HUMAN-CHECK REQUIRED: relaunch Thunderbird and confirm NO crash-recovery/compaction prompt and NO surviving background/tray thunderbird.exe. L-W6 (breakaway surviving a Task-Scheduler End) is a separate step.")
}
