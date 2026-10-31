//go:build windows

package presync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// This file is the R31 crux on Windows (design §4, §6, PC-W2/PC-W3/PC-W6/PC-W7):
// how the pre-sync starts Thunderbird minimized (the start itself is built in
// winlaunch.go, MA-923) and later asks it to quit. The quit
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
)

var (
	procEnumWindows              = user32.NewProc("EnumWindows")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	procPostMessageW             = user32.NewProc("PostMessageW")
	procIsWindowVisible          = user32.NewProc("IsWindowVisible")
	procGetWindowLongPtr         = user32.NewProc("GetWindowLongPtrW")
	procGetWindow                = user32.NewProc("GetWindow")
	procShowWindow               = user32.NewProc("ShowWindow")
	procIsIconic                 = user32.NewProc("IsIconic")
	procGetForegroundWindow      = user32.NewProc("GetForegroundWindow")
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

// winCreateProcess and winCloseHandle are the two Win32 calls of a start (vars, so
// a test sees what Windows is asked for and that both returned handles are closed,
// without starting anything; MA-923).
var (
	winCreateProcess = realCreateProcess
	winCloseHandle   = windows.CloseHandle
)

// realCreateProcess is CreateProcessW, as a named function so MA-612's walk of
// every process-start site sees it.
func realCreateProcess(app, cmdLine *uint16, inherit bool, flags uint32, dir *uint16, si *windows.StartupInfo, pi *windows.ProcessInformation) error {
	return windows.CreateProcess(app, cmdLine, nil, nil, inherit, flags, nil, dir, si, pi)
}

// winStart starts a built launch (buildLaunchCmd, winlaunch.go) with
// CreateProcessW: the program by its full path, the command line as built, the
// install folder, the STARTUPINFO show state, no handle inherited and this
// program's environment. It then closes both handles Windows returns (process and
// thread), so the long-lived Desktop does not keep two handles per Run now, and it
// never waits on or signals Thunderbird (the session re-finds it by the
// scan+`-profile` identity, §3). A failure keeps Windows' own error, wrapped with
// the program's path, so "access denied" is still recognised (errStartBlocked).
func winStart(l winLaunch) error {
	app, err := windows.UTF16PtrFromString(l.app)
	if err != nil {
		return err
	}
	// CreateProcessW may write into its command line, so it gets its own buffer.
	line, err := windows.UTF16FromString(l.cmdLine)
	if err != nil {
		return err
	}
	var dir *uint16
	if l.dir != "" {
		if dir, err = windows.UTF16PtrFromString(l.dir); err != nil {
			return err
		}
	}
	si := windows.StartupInfo{Flags: l.siFlags, ShowWindow: l.show}
	si.Cb = uint32(unsafe.Sizeof(si))
	var pi windows.ProcessInformation
	if err := winCreateProcess(app, &line[0], l.inherit, l.flags, dir, &si, &pi); err != nil {
		return &os.PathError{Op: "start", Path: l.app, Err: err}
	}
	_ = winCloseHandle(pi.Thread)
	_ = winCloseHandle(pi.Process)
	return nil
}

// startHidden starts Thunderbird minimized and not activated (buildLaunchCmd,
// MA-923; lab L-A10 showed Thunderbird honours SW_SHOWMINNOACTIVE, where it had
// ignored SW_HIDE in L-W4), detached, without ever waiting on or signalling it (the
// real process is re-found by the scan+`-profile` match, §3). The name is the
// cross-platform seam's: on macOS the launch is truly hidden; on Windows the window
// starts minimized in the taskbar, and minimizeMain stays as the backstop for a
// window that shows anyway. Scheduled and attended runs start it the same way. A
// launch failure is returned for S5 to map to presync_cannot_start; "access denied"
// — the program that started this backup runs in a job that forbids breakaway — is
// errStartBlocked, presync_start_blocked, and is never retried without breakaway
// (design-presync-attended §2.7, C-65).
func (e *winEnv) startHidden(exe, profile string) error {
	if err := winStart(buildLaunchCmd(exe, profile)); err != nil {
		if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
			return fmt.Errorf("%w: %v", errStartBlocked, err)
		}
		return err
	}
	return nil
}

// minimizeMain best-effort minimizes the pre-sync's Thunderbird window, the
// backstop for a start whose window shows anyway: the start asks for minimized and
// not activated (MA-923), and the earlier start showed it (owner ruling 2026-10-23
// accepted that as a taskbar blip). A window that is already minimized counts as
// minimized: an attended run, and every retry, makes no call for it (minimizeMode).
// The session calls it once it has adopted our process (after S6): it waits briefly
// for the single main window to appear, then minimizes it (minimizeWindow) — a
// benign window-state change, never a close or kill. If no single main window
// appears in time it does nothing, and the session tries again on each S7 poll
// (minimizeAgain) until one attempt succeeds (A14, C-56). macOS launches truly
// hidden and does not implement this. Its wait ends at once when ctx or stop (the
// session context) ends — a Lock, Shut down or the watchdog abandoning the session
// (C-64). It reports whether the window is minimized, so every Windows text can
// say "its window may still be on screen" when it is not (Result.Minimized, C-56).
// The first attempt follows the run's mode: a scheduled run, just past its idle
// check, keeps SW_MINIMIZE (§1.3).
func (e *winEnv) minimizeMain(ctx, stop context.Context, child proc, attended bool) bool {
	deadline := e.now().Add(minimizeWait)
	for e.now().Before(deadline) {
		if ctx.Err() != nil || (stop != nil && stop.Err() != nil) {
			return false
		}
		if found, minimized := e.minimizeOurs(child, attended); found {
			return minimized
		}
		e.sleep(ctx, stop, 500*time.Millisecond)
	}
	return false
}

// minimizeAgain is one more try with no wait (A14 (2), C-56): the session calls it
// on each S7 poll until one attempt has succeeded, and never after, so a person who
// restores our window to work in it is not fought. A retry may come many minutes
// after a scheduled run's idle check, when the person may be typing again, so every
// retry takes minimizeMode's choice whatever the run's mode and never moves the
// keyboard away from another program (gate-2 pass 1, as4win-5).
func (e *winEnv) minimizeAgain(child proc, _ bool) bool {
	_, minimized := e.minimizeOurs(child, true)
	return minimized
}

// minimizeOurs makes one minimize attempt on the proven child's main window. Like
// the quit (F1, PC-W3) it holds a handle to the pid across the attempt, so Windows
// cannot give the number to another program meanwhile, and re-proves through it,
// by exact creation time, that the process is the child the session adopted: a pid
// reused after our Thunderbird exited is never addressed (gate-2 pass 1, as4win-6).
// found reports whether the child's single main window was found; minimized,
// whether that window is minimized now.
func (e *winEnv) minimizeOurs(child proc, keepFocus bool) (found, minimized bool) {
	pid := uint32(child.pid)
	h, ok := e.w.holdHandle(pid)
	if !ok {
		return false, false
	}
	defer e.w.releaseHandle(h)
	if e.w.creationNs(pid) != child.start.UnixNano() {
		return false, false
	}
	hwnd, ok := e.w.mainWindow(pid)
	if !ok {
		return false, false
	}
	return true, e.minimizeWindow(hwnd, pid, keepFocus)
}

// minimizeWindow minimizes Thunderbird's main window and reports whether it is
// minimized afterwards: a ShowWindow that did not take (Thunderbird showed its
// window again) is not success, so the session tries again (gate-2 pass 1,
// as4win-2). Without keepFocus (a scheduled run's first attempt) it uses
// SW_MINIMIZE (§1.3, accepted 2026-10-23). With keepFocus — an attended run, or any
// retry — it takes minimizeMode's choice (A14 (3), C-58): an already minimized
// window gets no call (and counts as minimized), one that does not have the
// keyboard is minimized without activating anything, and one that took the keyboard
// is minimized handing activation back.
func (e *winEnv) minimizeWindow(hwnd uintptr, pid uint32, keepFocus bool) bool {
	mode := swMinimize
	if keepFocus {
		mode = minimizeMode(e.w.isIconic(hwnd), e.foregroundIsOurs(pid))
		if mode == 0 {
			return true
		}
	}
	e.w.showWindow(hwnd, mode)
	return e.w.isIconic(hwnd)
}

// foregroundIsOurs reports whether the window the keyboard goes to belongs to our
// Thunderbird's process — its main window or any dialog it shows, such as the
// not-the-default-mail-app prompt — not only whether it is the main window itself
// (gate-2 pass 1, as4win-3).
func (e *winEnv) foregroundIsOurs(pid uint32) bool {
	fg := e.w.foregroundWindow()
	return fg != 0 && e.w.windowPID(fg) == pid
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
// once, so ctx bounds no wait here; but a ctx that has ended by the last check
// posts nothing (§2.6, as1a-4). The `target` is the proven child (its pid);
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
	// No quit request is started once the quit's context has ended (§2.6, as1a-4): the
	// watchdog gave up, or a Stop came, while the checks above ran. Checked last, so a
	// context that ended at any point before here posts nothing.
	if ctx.Err() != nil {
		return errQuitNotPosted
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

func (realWin32) showWindow(hwnd uintptr, cmd int) { procShowWindow.Call(hwnd, uintptr(cmd)) }

func (realWin32) isIconic(hwnd uintptr) bool {
	r, _, _ := procIsIconic.Call(hwnd)
	return r != 0
}

func (realWin32) foregroundWindow() uintptr {
	r, _, _ := procGetForegroundWindow.Call()
	return r
}

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
