//go:build windows

package desktop

import (
	"runtime"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The Windows side of the instance (design-menubar §7.3, §8.1; slice S1). A hidden
// top-level window — never shown, and with no title bar so it never counts as a dialog
// (dialogfront's isDialogWindow needs one; MB-8) — on a goroutine locked to its OS
// thread, with its own message loop, so the instance hears the end of the session:
//   - WM_QUERYENDSESSION (sign-out, shut down, Restart Manager's close request during an
//     MSI upgrade): allowed, with a shutdown-block reason while work runs;
//   - WM_ENDSESSION: the one stop, asking nothing, waiting for running work for about 5
//     seconds, then letting go (MB-2 items 1 to 3).
// WM_COMMAND is ignored: no other process of the session can post a command to it
// (MB-17). The icon in the notification area comes with slice S3.

var (
	user32                     = windows.NewLazySystemDLL("user32.dll")
	procAllowSetForeground     = user32.NewProc("AllowSetForegroundWindow")
	procRegisterClassExW       = user32.NewProc("RegisterClassExW")
	procCreateWindowExW        = user32.NewProc("CreateWindowExW")
	procDefWindowProcW         = user32.NewProc("DefWindowProcW")
	procGetMessageW            = user32.NewProc("GetMessageW")
	procTranslateMessage       = user32.NewProc("TranslateMessage")
	procDispatchMessageW       = user32.NewProc("DispatchMessageW")
	procPostMessageW           = user32.NewProc("PostMessageW")
	procDestroyWindow          = user32.NewProc("DestroyWindow")
	procPostQuitMessage        = user32.NewProc("PostQuitMessage")
	procShutdownBlockReasonCrt = user32.NewProc("ShutdownBlockReasonCreate")
	procShutdownBlockReasonDst = user32.NewProc("ShutdownBlockReasonDestroy")
)

// The window messages and styles the loop uses.
const (
	wmDestroy          = 0x0002
	wmClose            = 0x0010
	wmQueryEndSession  = 0x0011
	wmEndSession       = 0x0016
	wmCommand          = 0x0111
	wsPopup            = 0x80000000
	endSessionBound    = 5 * time.Second
	shutdownBlockWords = "MailArchive is finishing its work. It stops by itself in a few seconds."
)

func init() { appLoop = windowsLoop }

// allowForeground lets pid bring a window to the front (§7.3); the launcher calls it
// only for the holder-confirmed pid (MB-17).
var allowForeground = func(pid int) { procAllowSetForeground.Call(uintptr(uint32(pid))) }

// bundleVersionOnDisk: no bundle on Windows; an MSI upgrade meets the running copy
// through Restart Manager instead (§7.3 MB-7 note).
func bundleVersionOnDisk() string { return "" }

type wndClassEx struct {
	size       uint32
	style      uint32
	wndProc    uintptr
	clsExtra   int32
	wndExtra   int32
	instance   windows.Handle
	icon       windows.Handle
	cursor     windows.Handle
	background windows.Handle
	menuName   *uint16
	className  *uint16
	iconSm     windows.Handle
}

type winMsg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	ptX     int32
	ptY     int32
	private uint32
}

// The one window procedure (windows.NewCallback has a process-wide limit, so it is made
// once) reads the events of the loop now running.
var (
	loopMu     sync.Mutex
	loopEvents LoopEvents
	wndProcCB  uintptr
	classOnce  sync.Once
	className  = windows.StringToUTF16Ptr("MailArchiveInstanceWindow")
	classErr   error
)

func wndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	loopMu.Lock()
	ev := loopEvents
	loopMu.Unlock()
	switch msg {
	case wmQueryEndSession:
		if ev.Busy != nil && ev.Busy() {
			procShutdownBlockReasonCrt.Call(hwnd, uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(shutdownBlockWords))))
		}
		return 1 // allow: the stop happens in WM_ENDSESSION
	case wmEndSession:
		if wParam != 0 && ev.EndSession != nil {
			ev.EndSession(endSessionBound)
		}
		procShutdownBlockReasonDst.Call(hwnd)
		return 0
	case wmCommand:
		return 0 // MB-17: no command reaches the instance through a posted message
	case wmClose:
		procDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
	return r
}

// newHiddenWindow registers the class once and makes the hidden window: WS_POPUP, no
// WS_CAPTION, never shown.
func newHiddenWindow() uintptr {
	classOnce.Do(func() {
		wndProcCB = windows.NewCallback(wndProc)
		var inst windows.Handle
		_ = windows.GetModuleHandleEx(0, nil, &inst)
		wc := wndClassEx{wndProc: wndProcCB, instance: inst, className: className}
		wc.size = uint32(unsafe.Sizeof(wc))
		if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
			classErr = err
		}
	})
	if classErr != nil {
		return 0
	}
	h, _, _ := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(className)),
		wsPopup, 0, 0, 0, 0, 0, 0, 0, 0)
	return h
}

// windowsLoop serves on the calling goroutine while the hidden window's message loop
// runs on a locked thread of its own.
func windowsLoop(serve func() error, ev LoopEvents) error {
	loopMu.Lock()
	loopEvents = ev
	loopMu.Unlock()
	hwndc := make(chan uintptr, 1)
	go func() {
		runtime.LockOSThread()
		h := newHiddenWindow()
		hwndc <- h
		if h == 0 {
			return
		}
		var m winMsg
		for {
			r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
			if int32(r) <= 0 {
				return
			}
			procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
			procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
		}
	}()
	h := <-hwndc
	err := serve()
	if h != 0 {
		procPostMessageW.Call(h, wmClose, 0, 0)
	}
	return err
}

// hiddenWindowStyle is the hidden window's style, for MA-1435's check that it has no
// title bar.
const hiddenWindowStyle uint32 = wsPopup
