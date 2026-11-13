//go:build darwin

package instance

import (
	"strconv"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

var (
	guiSessOnce sync.Once
	guiSess     string
)

// GUISession is this process's sign-in session as macOS counts it: the audit session
// id (getaudit_addr's ai_asid). Every process of one graphical login (Finder, the Dock,
// Terminal's shells) shares it; an SSH login has its own. The lock is per account on
// macOS (MB-9), so the instance compares a launcher's session with its own and, when
// they differ, answers ReasonOtherSession instead of showing a window on a screen the
// person is not at (R50, the fold's third contradiction). "" when it cannot be read.
func GUISession() string {
	guiSessOnce.Do(func() { guiSess = auditSession() })
	return guiSess
}

// auditSession calls getaudit_addr(2) from libSystem through purego (no cgo). The
// struct auditinfo_addr is: ai_auid uint32; ai_mask {uint32, uint32}; ai_termid
// {at_port uint32, at_type uint32, at_addr [4]uint32}; ai_asid int32; ai_flags
// uint64 — 48 bytes with ai_asid at offset 36.
func auditSession() string {
	lib, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		return ""
	}
	sym, err := purego.Dlsym(lib, "getaudit_addr")
	if err != nil {
		return ""
	}
	var buf [48]byte
	r, _, _ := purego.SyscallN(sym, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if int32(r) != 0 {
		return ""
	}
	asid := *(*int32)(unsafe.Pointer(&buf[36]))
	if asid <= 0 {
		return ""
	}
	return strconv.Itoa(int(asid))
}
