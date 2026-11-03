//go:build windows

package dialogfront

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"
	"unsafe"

	"github.com/ncruces/zenity"
	"golang.org/x/sys/windows"
)

var (
	procFindWindowW  = user32.NewProc("FindWindowW")
	procPostMessageW = user32.NewProc("PostMessageW")
)

const (
	wsExTopmost = 0x00000008 // WS_EX_TOPMOST
	wmClose     = 0x0010     // WM_CLOSE
)

var gwlExStyle = ^uintptr(19) // GWL_EXSTYLE (-20)

// processOf is the id of the process that owns hwnd.
func processOf(hwnd uintptr) uint32 {
	var pid uint32
	windows.GetWindowThreadProcessId(windows.HWND(hwnd), &pid)
	return pid
}

func activeWindow() uintptr {
	h, _, _ := procGetForegroundWindow.Call()
	return h
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

// covers: MA-1113, R33
// On Windows, with a window of another program active (the browser, in the
// owner's report), each kind of dialog MailArchive opens — a message box
// (Question), zenity's own window (Entry, List) and the folder picker — comes up
// as the active window once Install has run, is not left always on top, and
// closes cleanly when asked (nothing hangs). Run in a signed-in, unlocked session
// (the Windows 11 lab VM, through winrun-at.sh). Prove-fail (VM, 3 November 2026):
// with Install made to do nothing, Entry and List stay behind the other program's
// window and the test is red (in that run the message box and the folder picker
// came forward on their own).
func TestEachKindOfDialogComesUpActiveInFrontOfAnotherProgram(t *testing.T) {
	before := activeWindow()
	if before == 0 || processOf(before) == uint32(os.Getpid()) {
		t.Fatalf("the active window is %#x (process %d, this test is %d): run this in a signed-in session with another program's window active",
			before, processOf(before), os.Getpid())
	}
	t.Logf("active before the dialogs: %#x (process %d)", before, processOf(before))
	Install()
	Install() // a second call does nothing

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
		title := fmt.Sprintf("MA-1113 %s %d", k.name, time.Now().UnixNano())
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		done := make(chan error, 1)
		go func() { done <- k.open(ctx, title) }()

		ptitle, _ := windows.UTF16PtrFromString(title)
		var hwnd uintptr
		if !waitFor(10*time.Second, func() bool {
			hwnd, _, _ = procFindWindowW.Call(0, uintptr(unsafe.Pointer(ptitle)))
			return hwnd != 0 && processOf(hwnd) == uint32(os.Getpid())
		}) {
			cancel()
			t.Fatalf("%s: the dialog %q did not appear within 10 s", k.name, title)
		}
		active := waitFor(3*time.Second, func() bool { return activeWindow() == hwnd })
		// Lifted and let down again: once the tries are over (the last at 1.2 s),
		// it is not always on top.
		time.Sleep(retryAfter[len(retryAfter)-1] + 300*time.Millisecond)
		ex, _, _ := procGetWindowLongW.Call(hwnd, gwlExStyle)
		procPostMessageW.Call(hwnd, wmClose, 0, 0)
		var err error
		select {
		case err = <-done:
		case <-time.After(10 * time.Second):
			cancel()
			t.Fatalf("%s: the dialog did not close within 10 s of being asked to (a hang)", k.name)
		}
		cancel()
		if !active {
			t.Errorf("%s: the dialog is not the active window (the active one is %#x, process %d)", k.name, activeWindow(), processOf(activeWindow()))
		}
		if ex&wsExTopmost != 0 {
			t.Errorf("%s: the dialog was left always on top", k.name)
		}
		if err != nil && !errors.Is(err, zenity.ErrCanceled) {
			t.Errorf("%s: closing the dialog returned %v, want a cancel", k.name, err)
		}
		shown++
	}
	if shown != len(kinds) {
		t.Fatalf("%d of %d dialogs were shown", shown, len(kinds))
	}
}
