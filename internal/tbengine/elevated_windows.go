//go:build windows

package tbengine

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// elevated reports a process whose token is elevated (running as administrator,
// design §9.6, W-ELEVATED), as presync refuses one (resolve_windows.go). It fails
// closed: a token or an elevation that cannot be read counts as elevated, so the
// restore refuses rather than write files Thunderbird could not change afterwards.
func elevated() bool {
	var tok windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &tok); err != nil {
		return true
	}
	defer tok.Close()
	var el, n uint32
	if err := windows.GetTokenInformation(tok, windows.TokenElevation, (*byte)(unsafe.Pointer(&el)), uint32(unsafe.Sizeof(el)), &n); err != nil {
		return true
	}
	return el != 0
}

const elevatedWords = wElevated
