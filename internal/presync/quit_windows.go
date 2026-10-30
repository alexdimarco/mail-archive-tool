//go:build windows

package presync

import (
	"context"
	"fmt"
	"os/exec"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// This file is the R31 crux on Windows (design §4, §6, PC-W2/PC-W3/PC-W6/PC-W7):
// how the pre-sync starts Thunderbird hidden and later asks it to quit. The quit
// posts WM_CLOSE — the window's OWN close request, the message the title-bar X
// sends — to the proven child's single main window, and never a forceful process
// terminate, a forced window message, or a signal (the MA-557 hygiene walk forbids
// each of those by name, and TestWinQuitPostsOnlyClose pins WM_CLOSE as the only
// message posted). The behaviour that WM_CLOSE closes a HIDDEN Thunderbird cleanly
// is lab-pending (L-W4): if it cannot, the scheduled form does not ship (the macOS
// Q16 rule).

const (
	wWmClose        = 0x0010     // WM_CLOSE — the ONLY window message this adapter posts
	wGwOwner        = 4          // GetWindow: the window's owner
	wWsChild        = 0x40000000 // WS_CHILD — a child window, never the main one
	wWsCaption      = 0x00C00000 // WS_CAPTION — a title bar (the main app window has one)
	wWsExToolWindow = 0x00000080 // WS_EX_TOOLWINDOW — a tool window, never the main one
	// GWL_STYLE (-16) / GWL_EXSTYLE (-20) as their 32-bit two's-complement, so the
	// constant fits an (unsigned) uintptr; GetWindowLongPtrW reads the low 32 bits.
	wGwlStyle   = 0xFFFFFFF0
	wGwlExStyle = 0xFFFFFFEC
	// SW_MINIMIZE minimizes the window AND activates the next top-level window — so
	// Thunderbird does not keep focus — a benign window-state change, not a close.
	wSwMinimize = 6
)

var (
	procEnumWindows              = user32.NewProc("EnumWindows")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	procPostMessageW             = user32.NewProc("PostMessageW")
	procIsWindowVisible          = user32.NewProc("IsWindowVisible")
	procGetWindowLongPtr         = user32.NewProc("GetWindowLongPtrW")
	procGetWindow                = user32.NewProc("GetWindow")
	procShowWindow               = user32.NewProc("ShowWindow")
)

// winWindowInfo is one top-level window's identity, as selectMainWindow judges it.
type winWindowInfo struct {
	hwnd    uintptr
	pid     uint32
	visible bool
	style   uint32
	exStyle uint32
	owner   uintptr
}

// minimizeWait bounds how long minimizeMain waits for Thunderbird's window to
// appear before giving up (a var so a test can shrink it).
var minimizeWait = 15 * time.Second

// buildLaunchCmd builds the Thunderbird launch (design §2, §6). It launches OUT of
// the Task-Scheduler task's job object (CREATE_BREAKAWAY_FROM_JOB) so a job-object
// teardown on task End/logoff/shutdown cannot hard-kill Thunderbird mid-write — an
// R31 breach our own code never causes but Windows would (PC-W6). Breakaway
// forfeits the direct child handle, which is consistent with the scan+`-profile`
// identity (§3).
//
// It does NOT try to launch hidden: the L-W4 lab (2026-10-23) showed Thunderbird
// ignores SW_HIDE and shows its window regardless, so the ineffective HideWindow /
// CREATE_NO_WINDOW flags are dropped as misleading. Per the owner's 2026-10-23
// ruling a visible launch is acceptable (scheduled off-hours); the window is then
// minimized right after it appears (minimizeMain) so it is a taskbar blip, not a
// focus-stealing popup. A pure builder, so the flags are golden-tested.
//
// It runs Thunderbird in its own install folder (design-presync-attended C-65,
// both modes), so a Thunderbird left open for hours never pins the folder Mail
// Archive itself was started in (an archive, a removable drive).
func buildLaunchCmd(exe, profile string) *exec.Cmd {
	cmd := exec.Command(exe, "-profile", profile)
	cmd.Dir = winInstallDir(exe)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_BREAKAWAY_FROM_JOB,
	}
	return cmd
}

// startHidden launches Thunderbird detached, then releases our reference without
// ever waiting on or signalling it (the real process is re-found by the
// scan+`-profile` match, §3). The name is the cross-platform seam's: on macOS the
// launch is truly hidden; on Windows it shows a window that minimizeMain then
// minimizes. A launch failure is returned for S5 to map to presync_cannot_start.
func (e *winEnv) startHidden(exe, profile string) error {
	cmd := buildLaunchCmd(exe, profile)
	if err := cmd.Start(); err != nil {
		return err
	}
	_ = cmd.Process.Release()
	return nil
}

// minimizeMain best-effort minimizes the pre-sync's Thunderbird window so a visible
// launch is a taskbar blip, not a focus-stealing popup (owner ruling 2026-10-23).
// The session calls it once it has adopted our process (after S6): it waits briefly
// for the single main window to appear, then minimizes it with
// ShowWindow(SW_MINIMIZE) — a benign window-state change, never a close or kill. If
// no single main window appears in time it does nothing (the launch stays visible —
// the accepted fallback). macOS launches truly hidden and does not implement this.
// Its wait ends at once when ctx or stop (the session context) ends — a Lock,
// Shut down or the watchdog abandoning the session (C-64).
func (e *winEnv) minimizeMain(ctx, stop context.Context, pid int) {
	deadline := e.now().Add(minimizeWait)
	for e.now().Before(deadline) {
		if ctx.Err() != nil || (stop != nil && stop.Err() != nil) {
			return
		}
		if hwnd, ok := e.w.mainWindow(uint32(pid)); ok {
			e.w.minimizeWindow(hwnd)
			return
		}
		e.sleep(ctx, stop, 500*time.Millisecond)
	}
}

// quit asks the proven child to close (design §4, S8). It holds an OpenProcess
// handle to the pid open across the whole quit so Windows cannot recycle the pid
// mid-quit (PC-W3); finds the pid's single MAIN top-level window; re-checks, at
// the last instant, that the held handle still reports alive AND the window still
// belongs to that pid (the Windows analogue of S8's pre-quit re-verify); then
// posts WM_CLOSE — and nothing else. Every "cannot safely close" path posts nothing
// and returns errQuitNotPosted (C-60), so the session ends at once as
// presync_quit_no_window (left-running, announced) — no exit-grace wait, no text
// saying it was asked to close, never a forced close. PostMessageW returns at
// once, so ctx bounds nothing here. The `target` is the proven child (its pid);
// macOS ignores the argument and quits by bundle id.
func (e *winEnv) quit(ctx context.Context, target proc) error {
	pid := uint32(target.pid)
	h, ok := e.w.holdHandle(pid)
	if !ok {
		return errQuitNotPosted // no handle held → cannot defeat pid reuse → do not address a window
	}
	defer e.w.releaseHandle(h)

	// Re-prove identity THROUGH the held handle (adversarial finding F1, PC-W3). The
	// handle now pins the pid so Windows cannot recycle it — but a pid freed and
	// reused in the gap since S8's findOurs (no handle was held across the record
	// write) could already have been pinned. Confirm the pinned process is the SAME
	// instance we adopted, by exact creation time (per-instance and immutable; a
	// reused pid has a different one). 0 / unreadable ⇒ mismatch ⇒ do not quit.
	if e.w.creationNs(pid) != target.start.UnixNano() {
		return errQuitNotPosted
	}

	hwnd, ok := e.w.mainWindow(pid)
	if !ok {
		return errQuitNotPosted // no single main window (a hidden modal, or ambiguity) → leave it
	}
	// Last-instant re-verify immediately before the post.
	if !e.w.handleAlive(h) || e.w.windowPID(hwnd) != pid {
		return errQuitNotPosted // the process died, or the HWND no longer belongs to our pid
	}
	// Residual (adversarial finding F2, the Windows analogue of macOS PC1): the held
	// handle pins the pid, not the HWND; an HWND destroyed and its value recycled to
	// another process's window in the microseconds between this check and the post
	// is irreducible — Win32 has no atomic "post to this pid's window". If the window
	// is merely destroyed, PostMessageW returns 0 and nothing is posted (fail-safe),
	// which is said the same way: nothing was asked.
	if err := e.w.postClose(hwnd); err != nil {
		return fmt.Errorf("%w: %v", errQuitNotPosted, err)
	}
	return nil
}

// selectMainWindow returns the one window that is the application's main window,
// or false when there is none OR more than one — so an ambiguous set is never
// quit (R31-safe), exactly as findOurs refuses an ambiguous process set.
func selectMainWindow(wins []winWindowInfo) (uintptr, bool) {
	var main uintptr
	n := 0
	for _, w := range wins {
		if isMainWindow(w) {
			main, n = w.hwnd, n+1
		}
	}
	if n == 1 {
		return main, true
	}
	return 0, false
}

// isMainWindow reports whether a window is a plausible MAIN application window
// (design §4.2): visible, top-level (not WS_CHILD), with a title bar (WS_CAPTION),
// not a tool window (WS_EX_TOOLWINDOW) and with no owner window (an owned window
// is a dialog or secondary, not the main one).
func isMainWindow(w winWindowInfo) bool {
	switch {
	case !w.visible:
		return false
	case w.style&wWsChild != 0:
		return false
	case w.style&wWsCaption != wWsCaption:
		return false
	case w.exStyle&wWsExToolWindow != 0:
		return false
	case w.owner != 0:
		return false
	default:
		return true
	}
}

// --- the real Win32 window layer (lab-pending L-W4; no GUI runs in a unit test) ---

func (realWin32) holdHandle(pid uint32) (uintptr, bool) {
	h, _, _ := procOpenProcess.Call(wSynchronize, 0, uintptr(pid))
	if h == 0 {
		return 0, false
	}
	return h, true
}

func (realWin32) handleAlive(h uintptr) bool {
	r, _, _ := procWaitForSingleObject.Call(h, 0)
	return uint32(r) == wWaitTimeout // still running = the wait times out
}

func (realWin32) releaseHandle(h uintptr) { procCloseHandle.Call(h) }

func (realWin32) minimizeWindow(hwnd uintptr) { procShowWindow.Call(hwnd, wSwMinimize) }

func (realWin32) windowPID(hwnd uintptr) uint32 {
	var pid uint32
	procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	return pid
}

func (realWin32) postClose(hwnd uintptr) error {
	r, _, err := procPostMessageW.Call(hwnd, wWmClose, 0, 0)
	if r == 0 {
		return err
	}
	return nil
}

// mainWindow enumerates this process's top-level windows (filtered to pid by the
// EnumWindows callback) and returns the single main one via selectMainWindow.
func (realWin32) mainWindow(pid uint32) (uintptr, bool) {
	var wins []winWindowInfo
	cb := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		var wpid uint32
		procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&wpid)))
		if wpid == pid {
			wins = append(wins, winWindowInfo{
				hwnd:    hwnd,
				pid:     wpid,
				visible: winIsVisible(hwnd),
				style:   uint32(winLong(hwnd, wGwlStyle)),
				exStyle: uint32(winLong(hwnd, wGwlExStyle)),
				owner:   winOwner(hwnd),
			})
		}
		return 1 // continue the enumeration
	})
	procEnumWindows.Call(cb, 0)
	return selectMainWindow(wins)
}

func winIsVisible(hwnd uintptr) bool {
	r, _, _ := procIsWindowVisible.Call(hwnd)
	return r != 0
}

func winLong(hwnd uintptr, index uintptr) uintptr {
	r, _, _ := procGetWindowLongPtr.Call(hwnd, index)
	return r
}

func winOwner(hwnd uintptr) uintptr {
	r, _, _ := procGetWindow.Call(hwnd, wGwOwner)
	return r
}
