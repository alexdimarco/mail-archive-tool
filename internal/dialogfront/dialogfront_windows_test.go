//go:build windows

package dialogfront

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/ncruces/zenity"
	"golang.org/x/sys/windows"
)

var (
	procFindWindowW      = user32.NewProc("FindWindowW")
	procPostMessageW     = user32.NewProc("PostMessageW")
	procGetLastInputInfo = user32.NewProc("GetLastInputInfo")
	procGetTickCount     = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetTickCount")
)

const (
	wsExTopmost     = 0x00000008 // WS_EX_TOPMOST
	wmClose         = 0x0010     // WM_CLOSE
	detachedProcess = 0x00000008 // DETACHED_PROCESS: no console, as mailarchivew.exe has none
	labEnv          = "MAILARCHIVE_DESKTOP_LAB"
	childEnv        = "MAILARCHIVE_DESKTOP_LAB_CHILD"
)

var gwlExStyle = ^uintptr(19) // GWL_EXSTYLE (-20)

// processOf is the id of the process that owns hwnd.
func processOf(hwnd uintptr) uint32 {
	var pid uint32
	windows.GetWindowThreadProcessId(windows.HWND(hwnd), &pid)
	return pid
}

// windowTitle is hwnd's title.
func windowTitle(hwnd uintptr) string {
	buf := make([]uint16, 256)
	user32.NewProc("GetWindowTextW").Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), 256)
	return windows.UTF16ToString(buf)
}

// lastInputTick is Windows' tick count at the session's last input (real or
// injected), from GetLastInputInfo.
func lastInputTick() uint32 {
	var lii struct{ size, time uint32 }
	lii.size = uint32(unsafe.Sizeof(lii))
	procGetLastInputInfo.Call(uintptr(unsafe.Pointer(&lii)))
	return lii.time
}

// idleFor is how long the session has had no input.
func idleFor() time.Duration {
	last := lastInputTick()
	now, _, _ := procGetTickCount.Call()
	return time.Duration(uint32(now)-last) * time.Millisecond
}

// waitFor polls cond every 20 ms until it holds or d has passed.
func waitFor(d time.Duration, cond func() bool) bool {
	for end := time.Now().Add(d); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
		if cond() {
			return true
		}
	}
	return cond()
}

// openDialog opens one dialog on its own goroutine and finds its window by its
// unique title (of this process).
func openDialog(t *testing.T, kind string, open func(ctx context.Context, title string) error) (hwnd uintptr, done chan error, cancel context.CancelFunc) {
	t.Helper()
	title := fmt.Sprintf("MA-1113 %s %d", kind, time.Now().UnixNano())
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	done = make(chan error, 1)
	go func() { done <- open(ctx, title) }()
	ptitle, _ := windows.UTF16PtrFromString(title)
	if !waitFor(10*time.Second, func() bool {
		hwnd, _, _ = procFindWindowW.Call(0, uintptr(unsafe.Pointer(ptitle)))
		return hwnd != 0 && processOf(hwnd) == uint32(os.Getpid())
	}) {
		cancel()
		t.Fatalf("%s: the dialog %q did not appear within 10 s", kind, title)
	}
	return hwnd, done, cancel
}

// closeDialog asks the dialog to close and waits for its call to return a cancel.
func closeDialog(t *testing.T, kind string, hwnd uintptr, done chan error, cancel context.CancelFunc) {
	t.Helper()
	defer cancel()
	procPostMessageW.Call(hwnd, wmClose, 0, 0)
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, zenity.ErrCanceled) {
			t.Errorf("%s: closing the dialog returned %v, want a cancel", kind, err)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("%s: the dialog did not close within 10 s of being asked to (a hang)", kind)
	}
}

// useUpTheStartGrant shows and destroys a plain window of this process (no title
// bar, so the watch leaves it alone) and hands the keyboard back to before. Windows
// lets a just-started process's first window come forward by itself (seen in the
// lab, 3 November 2026: with no watch at all the child's first dialog took the
// keyboard); MailArchive Desktop used that long before a button is pressed, so the
// test uses it up first.
func useUpTheStartGrant(t *testing.T, before uintptr) {
	t.Helper()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	var (
		createWindowEx = user32.NewProc("CreateWindowExW")
		showWindow     = user32.NewProc("ShowWindow")
		destroyWindow  = user32.NewProc("DestroyWindow")
	)
	pump := func() {
		var msg [64]byte
		for {
			if r, _, _ := procPeekMessageW.Call(uintptr(unsafe.Pointer(&msg[0])), 0, 0, 0, 1); r == 0 {
				return
			}
			procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg[0])))
			procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg[0])))
		}
	}
	class, _ := windows.UTF16PtrFromString("STATIC")
	const wsPopup = 0x80000000
	w, _, err := createWindowEx.Call(0, uintptr(unsafe.Pointer(class)), 0, wsPopup, 0, 0, 1, 1, 0, 0, 0, 0)
	if w == 0 {
		t.Fatalf("could not make the plain window: %v", err)
	}
	showWindow.Call(w, 5) // SW_SHOW
	procSetForegroundWindow.Call(w)
	pump()
	t.Logf("the plain window %#x took the keyboard by itself: %v", w, activeWindow() == w)
	procSetForegroundWindow.Call(before)
	pump()
	destroyWindow.Call(w)
	pump()
	if !waitFor(2*time.Second, func() bool { return activeWindow() == before }) {
		t.Fatalf("could not give the keyboard back to %#x after using up the start grant (active: %#x)", before, activeWindow())
	}
}

// covers: MA-1113, R39, R33
// LAB (the Windows 11 lab VM's signed-in session, through winrun with
// MAILARCHIVE_DESKTOP_LAB=1 and a key tapped into the active window just before, so
// Windows' foreground lock is fresh; it opens real dialogs, so it does not run
// elsewhere). It re-runs itself in a child with no console, as mailarchivew.exe
// runs, and there, with another program's window active (the browser, in the owner's
// report):
//   - a dialog nobody asked for (no Arm) stays out of the way: the other program's
//     window keeps the keyboard and the dialog is not left always on top (review
//     finding 1);
//   - each kind of dialog asked for (Arm, as a Desktop button does) — a message box
//     (Question), zenity's own windows (Entry, List) and the folder picker — comes
//     up as the active window, is not left always on top once the tries are over,
//     and closes within 10 s when asked, returning a cancel (nothing hangs);
//   - after the last one closes, the window that was active before has the keyboard
//     again (review finding 3).
//
// Prove-fail and prove-pass are in the commit and the MA-1113 row.
func TestEachKindOfDialogComesUpActiveInFrontOfAnotherProgram(t *testing.T) {
	if os.Getenv(childEnv) != "1" {
		if os.Getenv(labEnv) != "1" {
			t.Skip("desktop lab: set " + labEnv + "=1 in a signed-in, unlocked Windows session with another program's window active, a key tapped just before (the lab VM through winrun); it opens real dialogs")
		}
		cmd := exec.Command(os.Args[0], "-test.run=^TestEachKindOfDialogComesUpActiveInFrontOfAnotherProgram$", "-test.count=1", "-test.v")
		cmd.Env = append(os.Environ(), childEnv+"=1")
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: detachedProcess}
		out, err := cmd.CombinedOutput()
		t.Logf("the child with no console said:\n%s", out)
		if err != nil || !strings.Contains(string(out), "--- PASS") {
			t.Fatalf("the child with no console failed (%v)", err)
		}
		return
	}

	if c, _, _ := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetConsoleWindow").Call(); c != 0 {
		t.Fatalf("the child has a console window %#x; it must run as mailarchivew.exe does, with none", c)
	}
	before := activeWindow()
	if before == 0 || isOurs(before) {
		t.Fatalf("the active window is %#x (process %d, this test is %d): run this in a signed-in session with another program's window active",
			before, processOf(before), os.Getpid())
	}
	if idle := idleFor(); idle > 150*time.Second {
		t.Fatalf("the session has had no input for %v: Windows then lets any program take the front, so a dialog nobody asked for could not be told apart; tap a key into %#x and run again", idle.Round(time.Second), before)
	}
	t.Logf("active before the dialogs: %#x (process %d, %q, on screen %v)", before, processOf(before), windowTitle(before), onScreen(before))
	useUpTheStartGrant(t, before)
	var (
		traceMu sync.Mutex
		traced  []string
	)
	trace = func(format string, args ...any) {
		traceMu.Lock()
		defer traceMu.Unlock()
		traced = append(traced, time.Now().Format("15:04:05.000 ")+fmt.Sprintf(format, args...))
	}
	defer func() {
		traceMu.Lock()
		defer traceMu.Unlock()
		t.Logf("the watch's choices:\n%s", strings.Join(traced, "\n"))
	}()
	Install()
	Install() // a second call does nothing

	// A dialog nobody asked for.
	asked.Store(0)
	hwnd, done, cancel := openDialog(t, "unasked Question", func(ctx context.Context, title string) error {
		return zenity.Question("MA-1113 (nobody asked)", zenity.Title(title), zenity.Context(ctx))
	})
	tookIt := waitFor(retryAfter[len(retryAfter)-1]+800*time.Millisecond, func() bool { return isOurs(activeWindow()) })
	ex, _, _ := procGetWindowLongW.Call(hwnd, gwlExStyle)
	closeDialog(t, "unasked Question", hwnd, done, cancel)
	if tookIt {
		t.Errorf("a dialog nobody asked for took the keyboard from %#x", before)
	}
	if waitFor(time.Second, func() bool { return asked.Load() != 0 }) {
		t.Error("closing a dialog that never had the keyboard counted as the person's answer (the next dialog would take the keyboard)")
	}
	if ex&wsExTopmost != 0 {
		t.Error("a dialog nobody asked for was left always on top")
	}
	if a := activeWindow(); a != before {
		t.Logf("after the unasked dialog the active window is %#x (process %d)", a, processOf(a))
	}

	kinds := []struct {
		name string
		open func(ctx context.Context, title string) error
	}{
		{"Question", func(ctx context.Context, title string) error {
			return zenity.Question("MA-1113", zenity.Title(title), zenity.Context(ctx))
		}},
		{"Entry", func(ctx context.Context, title string) error {
			_, err := zenity.Entry("MA-1113", zenity.Title(title), zenity.Context(ctx))
			return err
		}},
		{"List", func(ctx context.Context, title string) error {
			_, err := zenity.List("MA-1113", []string{"one", "two"}, zenity.Title(title), zenity.Context(ctx))
			return err
		}},
		{"folder picker", func(ctx context.Context, title string) error {
			_, err := zenity.SelectFile(zenity.Directory(), zenity.Title(title), zenity.Context(ctx))
			return err
		}},
	}
	shown := 0
	for _, k := range kinds {
		Arm() // the person pressed the button that opens it
		hwnd, done, cancel := openDialog(t, k.name, k.open)
		active := waitFor(3*time.Second, func() bool { return activeWindow() == hwnd })
		// Lifted and let down again: once the tries are over (the last at 1.2 s),
		// it is not always on top.
		time.Sleep(retryAfter[len(retryAfter)-1] + 300*time.Millisecond)
		ex, _, _ := procGetWindowLongW.Call(hwnd, gwlExStyle)
		stillActive := activeWindow() == hwnd
		asked.Store(0)
		closeDialog(t, k.name, hwnd, done, cancel)
		// Closed while it had the keyboard: the person answered it, so the flow's next
		// window may take the keyboard too.
		if stillActive && !waitFor(time.Second, func() bool { return asked.Load() != 0 }) {
			t.Errorf("%s: closing the dialog while it had the keyboard did not count as the person's answer", k.name)
		}
		if !active {
			t.Errorf("%s: the dialog asked for is not the active window (the active one is %#x, process %d)", k.name, activeWindow(), processOf(activeWindow()))
		}
		if ex&wsExTopmost != 0 {
			t.Errorf("%s: the dialog was left always on top", k.name)
		}
		shown++
	}
	if shown != len(kinds) {
		t.Fatalf("%d of %d dialogs were shown", shown, len(kinds))
	}

	// The keyboard goes back to the window active before the dialogs.
	gaveBack := func(when string) {
		t.Helper()
		if !waitFor(giveBackAfter+2*time.Second, func() bool { return activeWindow() == before }) {
			the.mu.Lock()
			state := fmt.Sprintf("dialogs still recorded as shown %v, window to return to %#x", the.shown, the.returnTo)
			the.mu.Unlock()
			t.Errorf("after %s, the active window is %#x (process %d), not %#x, the one active before (%s)", when, activeWindow(), processOf(activeWindow()), before, state)
		}
	}
	gaveBack("the last dialog closed")

	// A window opened while one of ours is active (the summary after our progress
	// window) comes forward with no injected mouse move, so Windows' idle countdown
	// is not restarted (adversarial pass L2): the session's last-input time is the
	// same before and after it becomes active. Positive twin: it does become active.
	Arm()
	first, firstDone, firstCancel := openDialog(t, "chain: first", kinds[0].open)
	if !waitFor(3*time.Second, func() bool { return activeWindow() == first }) {
		t.Errorf("chain: the first dialog asked for is not the active window (the active one is %#x)", activeWindow())
	}
	time.Sleep(retryAfter[len(retryAfter)-1] + 300*time.Millisecond) // its tries are over
	tick := lastInputTick()
	asked.Store(0)
	next, nextDone, nextCancel := openDialog(t, "chain: next", kinds[1].open)
	nextActive := waitFor(3*time.Second, func() bool { return activeWindow() == next })
	time.Sleep(retryAfter[len(retryAfter)-1] + 300*time.Millisecond)
	tickAfter := lastInputTick()
	closeDialog(t, "chain: next", next, nextDone, nextCancel)
	closeDialog(t, "chain: first", first, firstDone, firstCancel)
	if !nextActive {
		t.Errorf("chain: a dialog opened while ours was active is not the active window (the active one is %#x)", activeWindow())
	}
	if tickAfter != tick {
		t.Errorf("chain: the session's last input moved from tick %d to %d while a dialog came forward from our own active one: an input was injected", tick, tickAfter)
	}
	gaveBack("the chain's dialogs closed")
}
