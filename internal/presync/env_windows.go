//go:build windows

package presync

import (
	"context"
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
	// The quit (the R31 crux, quit_windows.go): hold a handle across the quit so the
	// pid cannot be recycled, find the proven pid's single MAIN window, re-verify,
	// and post the window's OWN close request — never a kill.
	holdHandle(pid uint32) (uintptr, bool) // OpenProcess(SYNCHRONIZE), held across the quit
	handleAlive(h uintptr) bool            // WaitForSingleObject(h,0)==WAIT_TIMEOUT
	releaseHandle(h uintptr)               // CloseHandle
	showWindow(hwnd uintptr, cmd int)      // ShowWindow(cmd), cmd a minimize — a visible launch → taskbar blip
	isIconic(hwnd uintptr) bool            // IsIconic: already minimized
	foregroundWindow() uintptr             // GetForegroundWindow: the window the keyboard goes to
	mainWindow(pid uint32) (uintptr, bool) // the pid's single MAIN top-level window
	windowPID(hwnd uintptr) uint32         // GetWindowThreadProcessId → pid (pre-post re-check)
	postClose(hwnd uintptr) error          // PostMessageW(hwnd, WM_CLOSE, 0, 0)
	creationNs(pid uint32) int64           // creation time, unix ns — the quit's identity re-proof (F1)
}

// winEnv is the real Windows env (design §2). The uid-equivalent is implicit (the
// session's user); the win32 seam, the clock, the per-profile session lock, and
// two resolution seams (the candidate thunderbird.exe paths and the %APPDATA%
// base for profiles.ini) are its state. The resolution seams exist so S1 is
// unit-testable without a real install or real known folders (the analogue of
// macEnv's homeDir seam).
type winEnv struct {
	w        win32
	clock    func() time.Time
	lock     *lockfile.Lock
	appCands func() []string       // candidate thunderbird.exe paths; real = known folders
	roaming  func() (string, bool) // %APPDATA% base for profiles.ini; real = FOLDERID_RoamingAppData
}

func newWinEnv() *winEnv {
	return &winEnv{
		w:        realWin32{},
		clock:    time.Now,
		appCands: realAppCandidates,
		roaming:  realRoamingAppData,
	}
}

func (e *winEnv) now() time.Time { return e.clock() }
func (e *winEnv) sleep(ctx, stop context.Context, d time.Duration) {
	sleepCtx(ctx, stop, d)
}
func (e *winEnv) digest(roots []string) (watchSnap, bool) { return digestWatch(roots) }

// rawThunderbirdCount is the Toolhelp snapshot's thunderbird.exe count, every one
// listed whether or not its details read. Unused on Windows today: the quit goes
// to the proven pid's own window, so quitAffectsAllInstances is false there.
func (e *winEnv) rawThunderbirdCount() (int, bool) { return len(e.w.thunderbirdProcs()), true }
func (e *winEnv) bootTime() int64                  { return e.w.bootUnix() }
func (e *winEnv) pidAlive(pid int) bool            { return pid > 0 && e.w.alive(uint32(pid)) }
func (e *winEnv) idleSeconds() (float64, bool)     { return e.w.idleSeconds() }

// quitAffectsAllInstances is false on Windows: quit posts WM_CLOSE to the proven pid's
// own window, so it never touches another Thunderbird (C3).
func (e *winEnv) quitAffectsAllInstances() bool { return false }

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
// malformed (NUL-bearing) command line yields a NON-nil EMPTY argv (adversarial
// finding F7): the Windows adapter always sets a non-nil argv, so profileMatches
// always compares exact argv elements and NEVER falls back to the macOS whitespace
// split — an unreadable command line then carries no -profile and the process is
// never matched as ours, made explicit rather than resting on args being "" too.
func decodeArgv(cmdline string) []string {
	if cmdline == "" {
		return []string{}
	}
	argv, err := windows.DecomposeCommandLine(cmdline)
	if err != nil {
		return []string{}
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
	// NewLazySystemDLL forces the System32 directory, so a planted ntdll.dll /
	// user32.dll in the application or current directory can never be loaded
	// instead (PC-W8b) — unlike loading a system DLL by bare name, which the MA-557
	// hygiene walk now forbids (adversarial finding F5).
	ntdll    = windows.NewLazySystemDLL("ntdll.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	advapi32 = windows.NewLazySystemDLL("advapi32.dll")
	user32   = windows.NewLazySystemDLL("user32.dll")

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
	return float64(idleMillis32(uint64(tick), li.time)) / 1000, true
}

// decideElevated is fail-CLOSED (adversarial finding F3): when the token or its
// elevation cannot be read, the elevation state is UNKNOWN and we treat it as
// elevated, so resolve refuses (MA-572). A false refusal only skips the pre-sync —
// the archive still runs (R32) — whereas a false "not elevated" would launch
// Thunderbird from an elevated token and leave profile files the owner cannot
// open. macOS geteuid()==0 cannot fail; only this Windows path needs the guard.
func decideElevated(openOK, infoOK bool, elevation uint32) bool {
	if !openOK || !infoOK {
		return true
	}
	return elevation != 0
}

func (realWin32) elevated() bool {
	cur, _, _ := procGetCurrentProcess.Call()
	var tok syscall.Handle
	r, _, _ := procOpenProcessToken.Call(cur, wTokenQuery, uintptr(unsafe.Pointer(&tok)))
	if r == 0 {
		return decideElevated(false, false, 0)
	}
	defer procCloseHandle.Call(uintptr(tok))
	var el, ret uint32
	r, _, _ = procGetTokenInformation.Call(uintptr(tok), wTokenElevation, uintptr(unsafe.Pointer(&el)), 4, uintptr(unsafe.Pointer(&ret)))
	return decideElevated(true, r != 0, el)
}

// creationNs returns a process's creation time in unix nanoseconds (0 if
// unreadable), for the quit's identity re-proof through the held handle (F1).
func (realWin32) creationNs(pid uint32) int64 { return creationUnixNs(pid) }

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

// wCommandLineMax caps the command-line buffer we will allocate (adversarial
// finding F9): a real ProcessCommandLineInformation return is a UNICODE_STRING
// header plus a command line whose length is a uint16, so ~64 KiB + 16 is the
// structural ceiling; a larger retLen is anomalous and refused, never allocated.
const wCommandLineMax = 128 * 1024

// commandLine reads a process's command line with only
// PROCESS_QUERY_LIMITED_INFORMATION (PC-W1, spike-confirmed). "" on any failure;
// an unreadable command line means the process is never matched as ours. The
// NTSTATUS is masked to its low 32 bits consistently (F8) so a dirty high word can
// never make a success look like a failure (or vice versa).
func commandLine(pid uint32) string {
	h, _, _ := procOpenProcess.Call(wProcessQueryLimitedInformation, 0, uintptr(pid))
	if h == 0 {
		return ""
	}
	defer procCloseHandle.Call(h)
	var retLen uint32
	st, _, _ := procNtQueryInformationProcess.Call(h, wProcessCommandLineInformation, 0, 0, uintptr(unsafe.Pointer(&retLen)))
	if uint32(st) != wStatusInfoLengthMismatch && uint32(st) != 0 {
		return ""
	}
	if retLen == 0 || retLen > wCommandLineMax {
		return ""
	}
	buf := make([]byte, retLen)
	st, _, _ = procNtQueryInformationProcess.Call(h, wProcessCommandLineInformation,
		uintptr(unsafe.Pointer(&buf[0])), uintptr(retLen), uintptr(unsafe.Pointer(&retLen)))
	if uint32(st) != 0 {
		return ""
	}
	return parseCmdlineBuffer(buf)
}

// parseCmdlineBuffer extracts the command-line string from a raw
// ProcessCommandLineInformation buffer. It refuses a buffer too small to hold even
// the UNICODE_STRING header (adversarial finding F6), so the struct cast can never
// over-read the backing array, and an absent or empty inner string yields "". Any
// violation ⇒ "" ⇒ the process is never matched as ours. Pure, so the bounds are
// unit-tested without a Windows kernel.
func parseCmdlineBuffer(buf []byte) string {
	if len(buf) < int(unsafe.Sizeof(wUnicodeString{})) {
		return ""
	}
	us := (*wUnicodeString)(unsafe.Pointer(&buf[0]))
	if us.Buffer == 0 || us.Length == 0 {
		return ""
	}
	// The string lives in buf itself, after the header: read it from buf by its offset,
	// bounds-checked, never through the raw address (P3-8 Windows lab, the vet finding).
	off := us.Buffer - uintptr(unsafe.Pointer(&buf[0]))
	head := uintptr(unsafe.Sizeof(wUnicodeString{}))
	if us.Buffer < uintptr(unsafe.Pointer(&buf[0])) || off < head || off+uintptr(us.Length) > uintptr(len(buf)) {
		return ""
	}
	raw := buf[off : off+uintptr(us.Length)]
	u := make([]uint16, len(raw)/2)
	for i := range u {
		u[i] = uint16(raw[2*i]) | uint16(raw[2*i+1])<<8
	}
	return syscall.UTF16ToString(u)
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
