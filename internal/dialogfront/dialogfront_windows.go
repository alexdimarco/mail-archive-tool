//go:build windows

package dialogfront

import (
	"os"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32                  = windows.NewLazySystemDLL("user32.dll")
	procSetWinEventHook     = user32.NewProc("SetWinEventHook")
	procGetMessageW         = user32.NewProc("GetMessageW")
	procPeekMessageW        = user32.NewProc("PeekMessageW")
	procTranslateMessage    = user32.NewProc("TranslateMessage")
	procDispatchMessageW    = user32.NewProc("DispatchMessageW")
	procGetAncestor         = user32.NewProc("GetAncestor")
	procGetWindowLongW      = user32.NewProc("GetWindowLongW")
	procSetWindowPos        = user32.NewProc("SetWindowPos")
	procGetForegroundWindow = user32.NewProc("GetForegroundWindow")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procSendInput           = user32.NewProc("SendInput")
	procIsWindowVisible     = user32.NewProc("IsWindowVisible")
)

const (
	eventObjectShow       = 0x8002 // EVENT_OBJECT_SHOW
	wineventOutOfContext  = 0x0000 // WINEVENT_OUTOFCONTEXT: notices come through this thread's messages
	wineventSkipOwnThread = 0x0001 // WINEVENT_SKIPOWNTHREAD
	gaRoot                = 2      // GA_ROOT
	swpNoSize             = 0x0001
	swpNoMove             = 0x0002
	swpNoActivate         = 0x0010
	inputMouse            = 0 // INPUT_MOUSE
	mouseeventfMove       = 0x0001
)

var (
	hwndTopmost   = ^uintptr(0)  // HWND_TOPMOST (-1)
	hwndNoTopmost = ^uintptr(1)  // HWND_NOTOPMOST (-2)
	gwlStyle      = ^uintptr(15) // GWL_STYLE (-16)
)

// mouseInput and input are Windows' MOUSEINPUT and INPUT (a mouse input; the
// union's other members are no larger).
type mouseInput struct {
	dx, dy                 int32
	mouseData, flags, time uint32
	extraInfo              uintptr
}

type input struct {
	typ uint32
	mi  mouseInput
}

// install starts the watch on a thread of its own, which it never leaves: an
// out-of-context hook's notices arrive as messages on the thread that set it, so
// that thread waits for messages for the life of the process. Only this process's
// windows are watched, and not this thread's (it has none).
func install() {
	ready := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		hook, _, _ := procSetWinEventHook.Call(eventObjectShow, eventObjectShow, 0,
			syscall.NewCallback(onShow), uintptr(os.Getpid()), 0, wineventOutOfContext|wineventSkipOwnThread)
		close(ready)
		if hook == 0 {
			return // no watch: dialogs open as they did before
		}
		var msg [64]byte // a MSG, larger than it needs to be
		for {
			if r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg[0])), 0, 0, 0); int32(r) <= 0 {
				return
			}
			procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg[0])))
			procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg[0])))
		}
	}()
	<-ready
}

// onShow is the hook's callback (a WinEventProc) for a window of this process that
// was shown. A dialog is brought forward now and, while it still needs it, at
// each of retryAfter.
func onShow(_, _, hwnd, idObject, idChild, _, _ uintptr) uintptr {
	if hwnd == 0 {
		return 0
	}
	root, _, _ := procGetAncestor.Call(hwnd, gaRoot)
	style, _, _ := procGetWindowLongW.Call(hwnd, gwlStyle)
	if !isDialogWindow(int32(idObject), int32(idChild), root == hwnd, uint32(style)) {
		return 0
	}
	activeWhenShown, _, _ := procGetForegroundWindow.Call()
	bringForward(hwnd)
	go func() {
		// A thread with a message queue of its own, as the hook's thread has.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		var msg [64]byte
		procPeekMessageW.Call(uintptr(unsafe.Pointer(&msg[0])), 0, 0, 0, 0)
		start := time.Now()
		for _, at := range retryAfter {
			time.Sleep(time.Until(start.Add(at)))
			visible, _, _ := procIsWindowVisible.Call(hwnd)
			active, _, _ := procGetForegroundWindow.Call()
			if !tryAgain(visible != 0, active, hwnd, activeWhenShown) {
				return
			}
			bringForward(hwnd)
		}
	}()
	return 0
}

// bringForward lifts hwnd above the other windows without leaving it always on
// top, sends a mouse move of zero distance so Windows counts this process as the
// source of the last input, and makes hwnd the active window.
func bringForward(hwnd uintptr) {
	const flags = swpNoMove | swpNoSize | swpNoActivate
	procSetWindowPos.Call(hwnd, hwndTopmost, 0, 0, 0, 0, flags)
	procSetWindowPos.Call(hwnd, hwndNoTopmost, 0, 0, 0, 0, flags)
	in := input{typ: inputMouse, mi: mouseInput{flags: mouseeventfMove}}
	procSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
	procSetForegroundWindow.Call(hwnd)
}
