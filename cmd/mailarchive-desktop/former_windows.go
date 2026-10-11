//go:build windows

package main

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	iphlpapi                = windows.NewLazySystemDLL("iphlpapi.dll")
	procGetExtendedTcpTable = iphlpapi.NewProc("GetExtendedTcpTable")
)

const (
	afInet                 = 2 // AF_INET
	tcpTableOwnerPidListen = 3 // TCP_TABLE_OWNER_PID_LISTENER
)

// mibTCPRowOwnerPID is MIB_TCPROW_OWNER_PID.
type mibTCPRowOwnerPID struct {
	State      uint32
	LocalAddr  uint32
	LocalPort  uint32 // network byte order in the low 16 bits
	RemoteAddr uint32
	RemotePort uint32
	OwningPID  uint32
}

// endPreUpgrade ends an older MailArchive Desktop that still holds addr, when it
// is this account's own — the same program file, run by the same user — and
// reports whether the address is free afterwards. Anything else is left alone:
// the dashboard and `status` name it instead [CC22]. The owner of a listening
// port comes from GetExtendedTcpTable.
func endPreUpgrade(addr string) bool {
	_, ps, err := net.SplitHostPort(addr)
	port, perr := strconv.Atoi(ps)
	if err != nil || perr != nil {
		return false
	}
	pid := listenerPID(port)
	if pid == 0 || !isOurOwnProgram(pid) {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	if err := windows.TerminateProcess(h, 1); err != nil {
		return false
	}
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if probeFormer(addr, 200*time.Millisecond) == "" {
			return true
		}
	}
	return false
}

// listenerPID is the process listening on the IPv4 port, or 0.
func listenerPID(port int) int {
	var size uint32
	procGetExtendedTcpTable.Call(0, uintptr(unsafe.Pointer(&size)), 0, afInet, tcpTableOwnerPidListen, 0)
	if size == 0 {
		return 0
	}
	buf := make([]byte, size)
	if r, _, _ := procGetExtendedTcpTable.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)), 0, afInet, tcpTableOwnerPidListen, 0); r != 0 {
		return 0
	}
	n := *(*uint32)(unsafe.Pointer(&buf[0]))
	rows := unsafe.Slice((*mibTCPRowOwnerPID)(unsafe.Pointer(&buf[4])), int(n))
	for _, row := range rows {
		p := int(row.LocalPort&0xff)<<8 | int(row.LocalPort>>8&0xff)
		if p == port {
			return int(row.OwningPID)
		}
	}
	return 0
}

// isOurOwnProgram reports whether pid runs this program file as this user.
func isOurOwnProgram(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var buf [windows.MAX_PATH * 4]uint16
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return false
	}
	self, err := os.Executable()
	if err != nil || !strings.EqualFold(filepath.Clean(syscall.UTF16ToString(buf[:size])), filepath.Clean(self)) {
		return false
	}
	var tok windows.Token
	if err := windows.OpenProcessToken(h, windows.TOKEN_QUERY, &tok); err != nil {
		return false
	}
	defer tok.Close()
	theirs, err := tok.GetTokenUser()
	if err != nil {
		return false
	}
	ours, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return false
	}
	return windows.EqualSid(theirs.User.Sid, ours.User.Sid)
}
