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
	procFlashWindowEx       = user32.NewProc("FlashWindowEx")
)

const (
	eventSystemForeground = 0x0003 // EVENT_SYSTEM_FOREGROUND: the active window changed
	eventObjectDestroy    = 0x8001 // EVENT_OBJECT_DESTROY
	eventObjectShow       = 0x8002 // EVENT_OBJECT_SHOW
	eventObjectHide       = 0x8003 // EVENT_OBJECT_HIDE
	wineventOutOfContext  = 0x0000 // WINEVENT_OUTOFCONTEXT: notices come through this thread's messages
	wineventSkipOwnThread = 0x0001 // WINEVENT_SKIPOWNTHREAD
	gaRoot                = 2      // GA_ROOT
	swpNoSize             = 0x0001
	swpNoMove             = 0x0002
	swpNoActivate         = 0x0010
	inputMouse            = 0 // INPUT_MOUSE
	mouseeventfMove       = 0x0001
	flashwTray            = 0x0002 // FLASHW_TRAY: flash the taskbar button
	flashwTimerNoFG       = 0x000C // FLASHW_TIMERNOFG: until the window is brought to the front
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

// flashInfo is Windows' FLASHWINFO.
type flashInfo struct {
	size          uint32
	hwnd          uintptr
	flags, count  uint32
	timeoutMillis uint32
}

// the is the watch of this process.
var the = newWatch()

// trace receives a line for each choice the watch makes; nothing by default (the lab
// test, MA-1113, prints them).
var trace = func(format string, args ...any) {}

// install starts the watch on a thread of its own, which it never leaves: an
// out-of-context hook's notices arrive as messages on the thread that set it, so
// that thread waits for messages for the life of the process. Two hooks: this
// process's windows being shown, hidden or destroyed (not this thread's, which has
// none), and every change of the active window.
func install() {
	ready := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		cb := syscall.NewCallback(onEvent)
		own, _, _ := procSetWinEventHook.Call(eventObjectDestroy, eventObjectHide, 0,
			cb, uintptr(os.Getpid()), 0, wineventOutOfContext|wineventSkipOwnThread)
		procSetWinEventHook.Call(eventSystemForeground, eventSystemForeground, 0,
			cb, 0, 0, wineventOutOfContext)
		close(ready)
		if own == 0 {
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

func isOurs(hwnd uintptr) bool {
	if hwnd == 0 {
		return false
	}
	var pid uint32
	windows.GetWindowThreadProcessId(windows.HWND(hwnd), &pid)
	return int(pid) == os.Getpid()
}

func onScreen(hwnd uintptr) bool {
	v, _, _ := procIsWindowVisible.Call(hwnd)
	return v != 0
}

func activeWindow() uintptr {
	h, _, _ := procGetForegroundWindow.Call()
	return h
}

// noteActive records the window Windows says is active now (see watch.foreground).
func noteActive() {
	a := activeWindow()
	trace("active window now %#x (ours %v)", a, isOurs(a))
	the.foreground(a, isOurs(a), onScreen)
}

// onEvent is the hooks' callback (a WinEventProc).
func onEvent(_, event, hwnd, idObject, idChild, _, _ uintptr) uintptr {
	if hwnd == 0 {
		return 0
	}
	switch event {
	case eventSystemForeground:
		noteActive()
	case eventObjectShow:
		root, _, _ := procGetAncestor.Call(hwnd, gaRoot)
		style, _, _ := procGetWindowLongW.Call(hwnd, gwlStyle)
		if isDialogWindow(int32(idObject), int32(idChild), root == hwnd, uint32(style)) {
			onShow(hwnd)
		}
	case eventObjectHide, eventObjectDestroy:
		if int32(idObject) != objidWindow || int32(idChild) != childidSelf {
			return 0
		}
		answered, last := the.hidden(hwnd)
		if answered || last {
			trace("closed %#x: answered %v, the last %v", hwnd, answered, last)
		}
		if answered {
			Arm() // the next window of the flow follows the person's answer
		}
		if last {
			go onThreadWithQueue(func() {
				time.Sleep(giveBackAfter)
				a := activeWindow()
				r := the.giveBackTo(a, onScreen)
				if r != 0 {
					giveBack(r)
				}
				trace("give back after %#x closed: active %#x, to %#x, active now %#x", hwnd, a, r, activeWindow())
			})
		}
	}
	return 0
}

// onShow decides for a dialog of ours just shown: brought forward and given the
// keyboard when the person asked for it (and again at each of retryAfter while it
// still needs it), otherwise left where it is with its taskbar button flashing.
func onShow(hwnd uintptr) {
	activeWhenShown := activeWindow()
	ours := isOurs(activeWhenShown)
	takes := ours
	if !takes {
		since, ok := takeAsk()
		takes = mayTakeKeyboard(false, ok, since)
	}
	the.showed(hwnd, takes, activeWhenShown, ours)
	trace("shown %#x: active then %#x (ours %v), takes the keyboard %v", hwnd, activeWhenShown, ours, takes)
	if !takes {
		fi := flashInfo{hwnd: hwnd, flags: flashwTray | flashwTimerNoFG}
		fi.size = uint32(unsafe.Sizeof(fi))
		procFlashWindowEx.Call(uintptr(unsafe.Pointer(&fi)))
		noteActive()
		return
	}
	bringForward(hwnd)
	noteActive()
	go onThreadWithQueue(func() {
		start := time.Now()
		for _, at := range retryAfter {
			time.Sleep(time.Until(start.Add(at)))
			noteActive()
			active := activeWindow()
			if !tryAgain(onScreen(hwnd), the.wasActive(hwnd), active, hwnd, activeWhenShown) {
				return
			}
			bringForward(hwnd)
			noteActive()
		}
	})
}

// onThreadWithQueue runs f on a locked thread with a message queue of its own, as
// the hook's thread has, which SetForegroundWindow expects of its caller.
func onThreadWithQueue(f func()) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	var msg [64]byte
	procPeekMessageW.Call(uintptr(unsafe.Pointer(&msg[0])), 0, 0, 0, 0)
	f()
}

// giveBack makes r the active window again; no window is active, so the keyboard is
// taken from no one. Windows does not always allow it at once (the closed dialog's
// thread may still count as the foreground), so a refusal is tried once more after
// the zero-distance mouse move.
func giveBack(r uintptr) {
	procSetForegroundWindow.Call(r)
	if activeWindow() == r {
		return
	}
	winOps{}.nudge()
	procSetForegroundWindow.Call(r)
}

// bringForward brings hwnd forward with the real Windows calls (see bring).
func bringForward(hwnd uintptr) { bring(hwnd, winOps{}) }

// winOps are the real Windows calls behind bring.
type winOps struct{}

// lift puts h above the other windows without leaving it always on top.
func (winOps) lift(h uintptr) {
	const flags = swpNoMove | swpNoSize | swpNoActivate
	procSetWindowPos.Call(h, hwndTopmost, 0, 0, 0, 0, flags)
	procSetWindowPos.Call(h, hwndNoTopmost, 0, 0, 0, 0, flags)
}

func (winOps) active() uintptr { return activeWindow() }

func (winOps) isOurs(h uintptr) bool { return isOurs(h) }

// nudge sends one mouse move of zero distance, so Windows counts this process as
// the source of the last input.
func (winOps) nudge() {
	in := input{typ: inputMouse, mi: mouseInput{flags: mouseeventfMove}}
	procSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
}

func (winOps) setForeground(h uintptr) bool {
	r, _, _ := procSetForegroundWindow.Call(h)
	return r != 0
}
