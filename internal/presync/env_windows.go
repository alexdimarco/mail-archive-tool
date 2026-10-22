//go:build windows

package presync

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"mail-archive-tool/internal/lockfile"
)

// This file is the Windows adapter (design-thunderbird-presync-windows.md). Win32
// is reached through a win32 seam so the winEnv logic is testable with a fake
// (the equivalent of macEnv's exec seam). The real Win32 calls below are the ones
// the 2026-10-22 viability spike confirmed on real Windows: the command-line read
// via NtQueryInformationProcess(ProcessCommandLineInformation) with only
// PROCESS_QUERY_LIMITED_INFORMATION (PC-W1), Toolhelp enumeration, GetProcessTimes,
// QueryFullProcessImageNameW, NtQuerySystemInformation boot time, GetLastInputInfo,
// and the token-elevation check. It never force-terminates a process: neither the
// forceful terminate call nor an OpenProcess for terminate rights appears in this
// file, which the MA-557 hygiene walk enforces by name (PC-W7). startHidden/quit
// land in the next piece.

// winProc is one Thunderbird-named process as Windows reports it: the raw command
// line (which carries -profile, PC-W1), the full exe path and the creation time.
type winProc struct {
	pid     uint32
	exe     string
	startNs int64 // creation time, unix nanoseconds (UTC)
	cmdline string
}

// win32 is the seam over the Win32 calls the adapter needs. realWin32 makes the
// syscalls; tests inject a fake, so no real Win32 (or GUI) runs in a unit test.
type win32 interface {
	thunderbirdProcs() []winProc       // Toolhelp thunderbird.exe + per-proc reads
	alive(pid uint32) bool             // WaitForSingleObject on a handle; no signal
	exePath(pid uint32) (string, bool) // QueryFullProcessImageNameW
	bootUnix() int64                   // NtQuerySystemInformation boot time (absolute)
	idleSeconds() (float64, bool)      // GetLastInputInfo
	elevated() bool                    // OpenProcessToken + TokenElevation
}

// winEnv is the real Windows env (design §2). It holds the current uid-equivalent
// is implicit (the session's user); the win32 seam, the clock, and the per-profile
// session lock are its state.
type winEnv struct {
	w     win32
	clock func() time.Time
	lock  *lockfile.Lock
}

func newWinEnv() *winEnv {
	return &winEnv{w: realWin32{}, clock: time.Now}
}

func (e *winEnv) now() time.Time                       { return e.clock() }
func (e *winEnv) sleep(d time.Duration)                { time.Sleep(d) }
func (e *winEnv) digest(roots []string) (uint64, bool) { return digestWatch(roots) }
func (e *winEnv) bootTime() int64                      { return e.w.bootUnix() }
func (e *winEnv) pidAlive(pid int) bool                { return pid > 0 && e.w.alive(uint32(pid)) }
func (e *winEnv) idleSeconds() (float64, bool)         { return e.w.idleSeconds() }

func (e *winEnv) procExe(pid int) (string, bool) {
	if pid <= 0 {
		return "", false
	}
	return e.w.exePath(uint32(pid))
}

// thunderbirdProcs converts the Win32 process snapshot into the core's proc type:
// the raw command line is kept as proc.args (for the state record and logs) and
// is also parsed into a faithful argv so the reused ownership predicate matches
// `-profile` exactly (PC-W1); the creation time becomes start (UTC); the exe path
// carries through.
func (e *winEnv) thunderbirdProcs() []proc {
	var procs []proc
	for _, p := range e.w.thunderbirdProcs() {
		procs = append(procs, proc{
			pid:   int(p.pid),
			exe:   p.exe,
			start: time.Unix(0, p.startNs).UTC(),
			args:  p.cmdline,
			argv:  decodeArgv(p.cmdline),
		})
	}
	return procs
}

// decodeArgv splits a Windows command line into arguments with the OS's own rules
// (CommandLineToArgvW, via x/sys), so a quoted path — the usual shape, since a
// user folder like C:\Users\First Last\... contains a space — yields the unquoted
// -profile value as one element and profileMatches matches it (PC-W1). An empty or
// malformed (NUL-bearing) command line yields nil, and a proc with a nil argv is
// never matched as ours — nothing is then quit, which is the safe default.
func decodeArgv(cmdline string) []string {
	if cmdline == "" {
		return nil
	}
	argv, err := windows.DecomposeCommandLine(cmdline)
	if err != nil {
		return nil
	}
	return argv
}

// lockPID reads the Windows profile lock: Mozilla holds parent.lock open while
// running (there is no pid in it, unlike the macOS symlink), so a successful
// held-probe is "present" with pid 0, which detectRunning treats as running
// (signal A). A lock that opens for writing is not held (stale/absent).
func (e *winEnv) lockPID(profile string) (int, bool) {
	p := filepath.Join(profile, "parent.lock")
	// O_RDWR fails with a sharing violation while Thunderbird holds parent.lock
	// open; a missing file is no lock. Any other open error is treated as held
	// (fail safe: a false "running" only costs the sync, never a wrong quit).
	f, err := os.OpenFile(p, os.O_RDWR, 0)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, false
		}
		return 0, true
	}
	f.Close()
	return 0, false // opened for writing: not held
}

func (e *winEnv) acquireLock(path string) (bool, error) {
	l, err := lockfile.Acquire(path)
	if err != nil {
		if errors.Is(err, lockfile.ErrHeld) {
			return false, nil
		}
		return false, err
	}
	e.lock = l
	return true, nil
}

func (e *winEnv) releaseLock(string) {
	if e.lock != nil {
		_ = e.lock.Release()
		e.lock = nil
	}
}

// recheckProfile re-resolves the profile's canonical path immediately before the
// launch (§5.2 TOCTOU guard, PC-W10); its behaviour on NTFS junctions is a lab
// item (L-W-new).
func (e *winEnv) recheckProfile(canonical string) bool {
	c, err := filepath.EvalSymlinks(canonical)
	return err == nil && c == canonical
}

// --- the real Win32, adapted from the proven viability spike ---

var (
	ntdll    = syscall.NewLazyDLL("ntdll.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	advapi32 = syscall.NewLazyDLL("advapi32.dll")
	user32   = syscall.NewLazyDLL("user32.dll")

	procNtQueryInformationProcess = ntdll.NewProc("NtQueryInformationProcess")
	procNtQuerySystemInformation  = ntdll.NewProc("NtQuerySystemInformation")
	procCreateToolhelp32Snapshot  = kernel32.NewProc("CreateToolhelp32Snapshot")
	procProcess32FirstW           = kernel32.NewProc("Process32FirstW")
	procProcess32NextW            = kernel32.NewProc("Process32NextW")
	procOpenProcess               = kernel32.NewProc("OpenProcess")
	procCloseHandle               = kernel32.NewProc("CloseHandle")
	procQueryFullProcessImageName = kernel32.NewProc("QueryFullProcessImageNameW")
	procGetProcessTimes           = kernel32.NewProc("GetProcessTimes")
	procWaitForSingleObject       = kernel32.NewProc("WaitForSingleObject")
	procGetCurrentProcess         = kernel32.NewProc("GetCurrentProcess")
	procOpenProcessToken          = advapi32.NewProc("OpenProcessToken")
	procGetTokenInformation       = advapi32.NewProc("GetTokenInformation")
	procGetLastInputInfo          = user32.NewProc("GetLastInputInfo")
	procGetTickCount64            = kernel32.NewProc("GetTickCount64")
)

const (
	wProcessQueryLimitedInformation = 0x1000
	wSynchronize                    = 0x00100000
	wProcessCommandLineInformation  = 60
	wSystemTimeOfDayInformation     = 3
	wStatusInfoLengthMismatch       = 0xC0000004
	wTokenQuery                     = 0x0008
	wTokenElevation                 = 20
	wWaitTimeout                    = 0x00000102
	wTH32CSSnapProcess              = 0x00000002
	wMaxPath                        = 260
)

type realWin32 struct{}

type processEntry32 struct {
	Size            uint32
	CntUsage        uint32
	ProcessID       uint32
	DefaultHeapID   uintptr
	ModuleID        uint32
	CntThreads      uint32
	ParentProcessID uint32
	PriClassBase    int32
	Flags           uint32
	ExeFile         [wMaxPath]uint16
}

type wUnicodeString struct {
	Length        uint16
	MaximumLength uint16
	_             uint32
	Buffer        uintptr
}

func (realWin32) thunderbirdProcs() []winProc {
	snap, _, _ := procCreateToolhelp32Snapshot.Call(wTH32CSSnapProcess, 0)
	if snap == 0 || snap == uintptr(^uintptr(0)) {
		return nil
	}
	defer procCloseHandle.Call(snap)
	var e processEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	var out []winProc
	r, _, _ := procProcess32FirstW.Call(snap, uintptr(unsafe.Pointer(&e)))
	for r != 0 {
		if name := syscall.UTF16ToString(e.ExeFile[:]); eqFold(name, "thunderbird.exe") {
			pid := e.ProcessID
			wp := winProc{pid: pid}
			if exe, ok := (realWin32{}).exePath(pid); ok {
				wp.exe = exe
			}
			wp.startNs = creationUnixNs(pid)
			wp.cmdline = commandLine(pid)
			out = append(out, wp)
		}
		r, _, _ = procProcess32NextW.Call(snap, uintptr(unsafe.Pointer(&e)))
	}
	return out
}

func (realWin32) alive(pid uint32) bool {
	h, _, _ := procOpenProcess.Call(wSynchronize, 0, uintptr(pid))
	if h == 0 {
		return false
	}
	defer procCloseHandle.Call(h)
	r, _, _ := procWaitForSingleObject.Call(h, 0)
	return uint32(r) == wWaitTimeout // still running = the wait times out
}

func (realWin32) exePath(pid uint32) (string, bool) {
	h, _, _ := procOpenProcess.Call(wProcessQueryLimitedInformation, 0, uintptr(pid))
	if h == 0 {
		return "", false
	}
	defer procCloseHandle.Call(h)
	buf := make([]uint16, 1024)
	size := uint32(len(buf))
	r, _, _ := procQueryFullProcessImageName.Call(h, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if r == 0 {
		return "", false
	}
	return syscall.UTF16ToString(buf[:size]), true
}

func (realWin32) bootUnix() int64 {
	buf := make([]byte, 48)
	var ret uint32
	st, _, _ := procNtQuerySystemInformation.Call(wSystemTimeOfDayInformation,
		uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), uintptr(unsafe.Pointer(&ret)))
	if st != 0 {
		return 0
	}
	// SYSTEM_TIMEOFDAY_INFORMATION.BootTime is the first LARGE_INTEGER (100ns since
	// 1601, adjusted on clock steps). Convert to unix seconds.
	bootFiletime := *(*int64)(unsafe.Pointer(&buf[0]))
	return filetimeToUnix(bootFiletime)
}

func (realWin32) idleSeconds() (float64, bool) {
	type lastInputInfo struct {
		cbSize uint32
		time   uint32
	}
	li := lastInputInfo{cbSize: 8}
	r, _, _ := procGetLastInputInfo.Call(uintptr(unsafe.Pointer(&li)))
	if r == 0 {
		return 0, false
	}
	tick, _, _ := procGetTickCount64.Call()
	idleMs := uint64(tick) - uint64(li.time)
	return float64(idleMs) / 1000, true
}

func (realWin32) elevated() bool {
	cur, _, _ := procGetCurrentProcess.Call()
	var tok syscall.Handle
	r, _, _ := procOpenProcessToken.Call(cur, wTokenQuery, uintptr(unsafe.Pointer(&tok)))
	if r == 0 {
		return false
	}
	defer procCloseHandle.Call(uintptr(tok))
	var el, ret uint32
	r, _, _ = procGetTokenInformation.Call(uintptr(tok), wTokenElevation, uintptr(unsafe.Pointer(&el)), 4, uintptr(unsafe.Pointer(&ret)))
	return r != 0 && el != 0
}

func creationUnixNs(pid uint32) int64 {
	h, _, _ := procOpenProcess.Call(wProcessQueryLimitedInformation, 0, uintptr(pid))
	if h == 0 {
		return 0
	}
	defer procCloseHandle.Call(h)
	var c, ex, k, u syscall.Filetime
	r, _, _ := procGetProcessTimes.Call(h, uintptr(unsafe.Pointer(&c)), uintptr(unsafe.Pointer(&ex)), uintptr(unsafe.Pointer(&k)), uintptr(unsafe.Pointer(&u)))
	if r == 0 {
		return 0
	}
	return c.Nanoseconds()
}

// commandLine reads a process's command line with only
// PROCESS_QUERY_LIMITED_INFORMATION (PC-W1, spike-confirmed). "" on any failure;
// an unreadable command line means the process is never matched as ours.
func commandLine(pid uint32) string {
	h, _, _ := procOpenProcess.Call(wProcessQueryLimitedInformation, 0, uintptr(pid))
	if h == 0 {
		return ""
	}
	defer procCloseHandle.Call(h)
	var retLen uint32
	st, _, _ := procNtQueryInformationProcess.Call(h, wProcessCommandLineInformation, 0, 0, uintptr(unsafe.Pointer(&retLen)))
	if uint32(st) != wStatusInfoLengthMismatch && st != 0 {
		return ""
	}
	if retLen == 0 {
		return ""
	}
	buf := make([]byte, retLen)
	st, _, _ = procNtQueryInformationProcess.Call(h, wProcessCommandLineInformation,
		uintptr(unsafe.Pointer(&buf[0])), uintptr(retLen), uintptr(unsafe.Pointer(&retLen)))
	if st != 0 {
		return ""
	}
	us := (*wUnicodeString)(unsafe.Pointer(&buf[0]))
	if us.Buffer == 0 || us.Length == 0 {
		return ""
	}
	n := int(us.Length) / 2
	return syscall.UTF16ToString(unsafe.Slice((*uint16)(unsafe.Pointer(us.Buffer)), n))
}

// filetimeToUnix converts a Windows FILETIME (100ns ticks since 1601-01-01) to
// unix seconds.
func filetimeToUnix(ft int64) int64 {
	const ticksPerSecond = 10000000
	const epochDiffSeconds = 11644473600 // 1601 -> 1970
	return ft/ticksPerSecond - epochDiffSeconds
}

func eqFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}
