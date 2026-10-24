//go:build windows

package presync

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

var (
	procGetWindowText = user32.NewProc("GetWindowTextW")
	procIsIconic      = user32.NewProc("IsIconic")
)

// labWin is one top-level window of a pid, with its title, whether isMainWindow
// accepts it, and whether it is minimized (iconic) — the diagnostic the lab logs.
type labWin struct {
	info   winWindowInfo
	title  string
	main   bool
	iconic bool
}

// labListWindows enumerates EVERY top-level window of pid (visible AND hidden),
// with its style flags, owner, title and whether isMainWindow accepts it. This is
// what reveals whether a hidden Thunderbird has a findable main window.
func labListWindows(pid uint32) []labWin {
	var out []labWin
	cb := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		var wpid uint32
		procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&wpid)))
		if wpid == pid {
			w := winWindowInfo{
				hwnd:    hwnd,
				pid:     wpid,
				visible: winIsVisible(hwnd),
				style:   uint32(winLong(hwnd, wGwlStyle)),
				exStyle: uint32(winLong(hwnd, wGwlExStyle)),
				owner:   winOwner(hwnd),
			}
			buf := make([]uint16, 256)
			procGetWindowText.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
			ic, _, _ := procIsIconic.Call(hwnd)
			out = append(out, labWin{info: w, title: syscall.UTF16ToString(buf), main: isMainWindow(w), iconic: ic != 0})
		}
		return 1
	})
	procEnumWindows.Call(cb, 0)
	return out
}

func labLogWindows(t *testing.T, when string, pid uint32) (mainCount, visibleCount int) {
	wins := labListWindows(pid)
	t.Logf("windows for pid %d %s: %d total top-level", pid, when, len(wins))
	for _, w := range wins {
		if w.main {
			mainCount++
		}
		if w.info.visible {
			visibleCount++
		}
		t.Logf("  hwnd=%#x visible=%v iconic=%v caption=%v child=%v tool=%v owned=%v isMain=%v title=%q",
			w.info.hwnd, w.info.visible, w.iconic,
			w.info.style&wWsCaption == wWsCaption,
			w.info.style&wWsChild != 0,
			w.info.exStyle&wWsExToolWindow != 0,
			w.info.owner != 0, w.main, w.title)
	}
	return mainCount, visibleCount
}

// covers: MA-571, MA-570
// TestLabHiddenLaunchAndClose is the L-W3/L-W4 viability lab (design §10): the one
// thing no unit test can settle — whether a HIDDEN Thunderbird presents a window
// that WM_CLOSE closes cleanly. SKIPPED unless PRESYNC_LAB=1 and PRESYNC_LAB_PROFILE
// are set, because it LAUNCHES and asks to CLOSE the real Thunderbird for that
// profile. Run ONLY in an INTERACTIVE Windows session and ONLY against a TEST
// profile.
//
// It enumerates every top-level window of the hidden process (so a human can see
// whether a hidden main window exists), and judges the close HONESTLY: a clean exit
// only counts as our close when a main window was actually found to post WM_CLOSE
// to. An empty test profile's Thunderbird can exit on its own, which is NOT proof
// the close worked — so for definitive close-causation use a profile where
// Thunderbird stays running; this lab says so when it cannot tell.
func TestLabHiddenLaunchAndClose(t *testing.T) {
	if os.Getenv("PRESYNC_LAB") != "1" {
		t.Skip("viability lab: set PRESYNC_LAB=1 and PRESYNC_LAB_PROFILE (launches+closes the REAL Thunderbird; interactive session + TEST profile only)")
	}
	profile := os.Getenv("PRESYNC_LAB_PROFILE")
	if profile == "" {
		t.Fatal("set PRESYNC_LAB_PROFILE to a TEST Thunderbird profile directory (never your primary mail)")
	}

	e := newWinEnv()
	plan, outcome, ok := e.resolve(profile, true, nil) // named=true: the operator chose this profile
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
			t.Logf("L-W3: matched our process pid=%d start=%s (%d thunderbird.exe total now)", m.pid, m.start.Format(time.RFC3339), len(procs))
			break
		}
	}
	if !found {
		t.Fatal("L-W3 FAIL: no hidden thunderbird.exe matched by -profile within 30s")
	}

	labLogWindows(t, "right after launch", uint32(child.pid))

	// Minimize the window once it appears — exactly what the session's S6 hook does
	// (owner ruling 2026-10-23: a visible launch is acceptable; minimize it to a
	// taskbar blip). minimizeMain waits for the window, so it doubles as the settle.
	e.minimizeMain(context.Background(), child.pid)
	mainCount, visibleCount := labLogWindows(t, "after minimize", uint32(child.pid))

	iconic := 0
	for _, w := range labListWindows(uint32(child.pid)) {
		if w.iconic {
			iconic++
		}
	}
	t.Logf("L-W3: %d visible / %d minimized(iconic) window(s) for our pid — expect the main window minimized (a visible launch is acceptable; truly-hidden is a nice-to-have).", visibleCount, iconic)

	e.sleep(5 * time.Second) // let it "sync" a moment before the quit

	aliveBeforeQuit := e.pidAlive(child.pid)
	t.Logf("alive right before quit: %v", aliveBeforeQuit)
	if !aliveBeforeQuit {
		t.Skip("INCONCLUSIVE: the process self-exited during the settle (empty profiles do this) — cannot test the close here. Re-run against a profile where Thunderbird stays running.")
	}
	_, mainFound := e.w.mainWindow(uint32(child.pid))
	t.Logf("L-W4: single main window found to target at quit time: %v (isMain count=%d)", mainFound, mainCount)

	if err := e.quit(child); err != nil {
		t.Fatalf("quit returned an error (should be nil on every path): %v", err)
	}
	quitAt := e.now()
	e.sleep(1 * time.Second)
	labLogWindows(t, "1s after the quit request", uint32(child.pid))

	closed := !e.pidAlive(child.pid)
	for !closed && e.now().Sub(quitAt) < 30*time.Second {
		e.sleep(500 * time.Millisecond)
		closed = !e.pidAlive(child.pid)
	}
	t.Logf("L-W4: %d thunderbird.exe remain; our pid exited=%v", len(e.thunderbirdProcs()), closed)

	switch {
	case !mainFound:
		t.Fatalf("L-W4 FAIL: no single main window to post WM_CLOSE to — a hidden Thunderbird presents no findable main window, so the feature cannot close it. (the process exited=%v, but with nothing posted that is a self-exit, not our close)", closed)
	case !closed:
		t.Fatal("L-W4 FAIL: WM_CLOSE was posted to the main window, but Thunderbird did not exit within 30s")
	default:
		t.Log("L-W4: a main window WAS found and WM_CLOSE was posted, and Thunderbird exited. CAUSATION CAVEAT: an empty profile can also self-exit, so for a DEFINITIVE result re-run against a profile where Thunderbird stays running on its own. HUMAN-CHECK: relaunch and confirm no crash-recovery/compaction prompt and no leftover process.")
	}
}
