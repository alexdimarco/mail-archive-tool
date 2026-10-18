package main

import (
	"syscall"
	"unsafe"
)

// readNoArgInputs reads §4.3's inputs on Windows: whether a console is attached,
// and whether this program is its only process. The macOS and Linux inputs are
// false here. (The invoked path is not needed on Windows.)
func readNoArgInputs(string) noArgInputs {
	attached, own := consoleState()
	return noArgInputs{consoleAttached: attached, consoleIsOwn: own}
}

// consoleState reports whether a console window is attached (GetConsoleWindow !=
// 0) and, if so, whether this process is the only one attached to it
// (GetConsoleProcessList returns 1). A count of 1 is Explorer's console for a
// double-click of the console build (W2); 2+ is a shell, `start` or a script
// sharing it (W1). A failed call leaves own=false, i.e. W1. L-W4 confirms the
// count under conhost and Windows Terminal; if it proves unreliable W2 collapses
// into W1 (§4.3, §12).
func consoleState() (attached, own bool) {
	k32 := syscall.NewLazyDLL("kernel32.dll")
	hwnd, _, _ := k32.NewProc("GetConsoleWindow").Call()
	if hwnd == 0 {
		return false, false
	}
	var buf [4]uint32
	n, _, _ := k32.NewProc("GetConsoleProcessList").Call(
		uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return true, n == 1
}
