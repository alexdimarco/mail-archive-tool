//go:build windows

package presync

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// fakeWin32 scripts the Win32 seam so the winEnv logic is tested without real
// Win32 or a GUI (the equivalent of macEnv's fake exec runner).
type fakeWin32 struct {
	procs    []winProc
	aliveSet map[uint32]bool
	exeOf    map[uint32]string
	boot     int64
	idle     float64
	idleOK   bool
	isElev   bool

	// quit seam (the R31 crux). mainOf maps pid→hwnd (0 = no single main window);
	// winPIDof maps hwnd→pid for the pre-post re-check; posted (if non-nil)
	// records every hwnd postClose was asked to close, so a test can assert that
	// nothing was posted on the "cannot safely close" paths.
	handleOK   bool
	handleLive bool
	mainOf     map[uint32]uintptr
	winPIDof   map[uintptr]uint32
	posted     *[]uintptr
	postErr    error
	creation   int64 // what creationNs returns (the quit's identity re-proof, F1)
}

func (f fakeWin32) thunderbirdProcs() []winProc       { return f.procs }
func (f fakeWin32) alive(pid uint32) bool             { return f.aliveSet[pid] }
func (f fakeWin32) exePath(pid uint32) (string, bool) { e, ok := f.exeOf[pid]; return e, ok }
func (f fakeWin32) bootUnix() int64                   { return f.boot }
func (f fakeWin32) idleSeconds() (float64, bool)      { return f.idle, f.idleOK }
func (f fakeWin32) elevated() bool                    { return f.isElev }

func (f fakeWin32) holdHandle(uint32) (uintptr, bool) { return 1, f.handleOK }
func (f fakeWin32) handleAlive(uintptr) bool          { return f.handleLive }
func (f fakeWin32) releaseHandle(uintptr)             {}
func (f fakeWin32) windowPID(hwnd uintptr) uint32     { return f.winPIDof[hwnd] }
func (f fakeWin32) mainWindow(pid uint32) (uintptr, bool) {
	w := f.mainOf[pid]
	return w, w != 0
}
func (f fakeWin32) postClose(hwnd uintptr) error {
	if f.posted != nil {
		*f.posted = append(*f.posted, hwnd)
	}
	return f.postErr
}
func (f fakeWin32) creationNs(uint32) int64 { return f.creation }

// covers: MA-553, MA-554
// The Windows realization of running-detection and the §4.4.3 identity fields:
// thunderbirdProcs converts the Win32 snapshot into the core's proc type, keeping
// the raw command line as proc.args and parsing it into a faithful argv so the
// reused ownership predicate matches -profile even when the path is quoted
// (PC-W1); the creation time becomes a UTC start, and the exe path carries
// through. pidAlive and procExe delegate to the seam. The fixture quotes the
// profile path — the shape Windows produces for a user folder with a space — so a
// whitespace split would miss it and only the OS-rules argv parse passes.
func TestWinThunderbirdProcs(t *testing.T) {
	const prof = `C:\Users\First Last\AppData\Roaming\Thunderbird\Profiles\abc.default`
	const exe = `C:\Program Files\Mozilla Thunderbird\thunderbird.exe`
	start := time.Date(2026, 10, 22, 2, 0, 0, 0, time.UTC)
	e := &winEnv{
		w: fakeWin32{
			procs: []winProc{{
				pid:     4242,
				exe:     exe,
				startNs: start.UnixNano(),
				cmdline: `"` + exe + `" -profile "` + prof + `"`,
			}},
			aliveSet: map[uint32]bool{4242: true},
			exeOf:    map[uint32]string{4242: exe},
		},
		clock: func() time.Time { return start },
	}

	procs := e.thunderbirdProcs()
	if len(procs) != 1 {
		t.Fatalf("thunderbirdProcs = %d, want 1", len(procs))
	}
	p := procs[0]
	if p.pid != 4242 || p.exe != exe {
		t.Errorf("proc = {pid:%d exe:%q}", p.pid, p.exe)
	}
	if !p.start.Equal(start) || !p.start.Equal(p.start.UTC()) {
		t.Errorf("proc.start = %v, want %v UTC", p.start, start)
	}
	// The raw command line is kept verbatim for the record/logs.
	if p.args != `"`+exe+`" -profile "`+prof+`"` {
		t.Errorf("proc.args = %q, want the raw command line", p.args)
	}
	// A plain whitespace split cannot recover a quoted, space-bearing path; the
	// OS-rules argv parse must, so the reused predicate matches (PC-W1).
	if argsHaveProfile(p.args, prof) {
		t.Fatal("precondition: a whitespace split should NOT match this quoted path")
	}
	if !profileMatches(p, prof) {
		t.Errorf("profileMatches did not find -profile %q in argv %q (PC-W1 identity would fail)", prof, p.argv)
	}
	if !e.pidAlive(4242) || e.pidAlive(9999) {
		t.Error("pidAlive did not delegate to the seam")
	}
	if ex, ok := e.procExe(4242); !ok || ex != exe {
		t.Errorf("procExe = (%q,%v)", ex, ok)
	}
}

// covers: MA-553
// lockPID reads the Windows parent.lock: a missing file is no lock (present
// false); a file that opens for writing is not held; a held file (another handle
// with a deny-write share) is present with pid 0 (detectRunning treats it as
// running, signal A).
func TestWinLockPID(t *testing.T) {
	e := &winEnv{clock: time.Now}

	dir := t.TempDir()
	if pid, present := e.lockPID(dir); present || pid != 0 {
		t.Errorf("no parent.lock -> (%d,%v), want (0,false)", pid, present)
	}

	// A plain, unheld parent.lock opens for writing -> not held.
	lock := filepath.Join(dir, "parent.lock")
	if err := os.WriteFile(lock, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, present := e.lockPID(dir); present {
		t.Error("an unheld parent.lock reported as held")
	}

	// Hold it open with a deny-write share (what a running Thunderbird does).
	held := t.TempDir()
	hp := filepath.Join(held, "parent.lock")
	h, err := openDenyWrite(hp)
	if err != nil {
		t.Skipf("cannot hold a deny-write handle here: %v", err)
	}
	defer h.Close()
	if pid, present := e.lockPID(held); !present || pid != 0 {
		t.Errorf("a held parent.lock -> (%d,%v), want (0,true)", pid, present)
	}
}

// openDenyWrite creates (if absent) and holds a file open sharing READ only, so
// another open that asks for write access fails with a sharing violation — what a
// running Thunderbird's held parent.lock does to lockPID's O_RDWR probe. The
// caller closes the returned handle.
func openDenyWrite(path string) (*os.File, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := syscall.CreateFile(p, syscall.GENERIC_WRITE, syscall.FILE_SHARE_READ,
		nil, syscall.OPEN_ALWAYS, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(h), path), nil
}

// covers: MA-572
// decideElevated fails CLOSED (adversarial finding F3): an unreadable token or
// elevation is treated as elevated, so resolve refuses rather than launching from
// a possibly-elevated token; only a readable, non-elevated token is "not elevated".
func TestDecideElevated(t *testing.T) {
	for _, c := range []struct {
		openOK, infoOK bool
		elevation      uint32
		want           bool
	}{
		{false, false, 0, true}, // token unreadable -> refuse
		{false, true, 0, true},
		{true, false, 0, true}, // elevation unreadable -> refuse
		{true, true, 0, false}, // readable, not elevated
		{true, true, 1, true},  // readable, elevated
	} {
		if got := decideElevated(c.openOK, c.infoOK, c.elevation); got != c.want {
			t.Errorf("decideElevated(%v,%v,%d) = %v, want %v", c.openOK, c.infoOK, c.elevation, got, c.want)
		}
	}
}

// covers: MA-554
// parseCmdlineBuffer refuses a buffer too small for the UNICODE_STRING header, so
// the struct cast can never over-read the backing array (adversarial finding F6),
// and extracts the string from a well-formed, self-referential buffer.
func TestParseCmdlineBuffer(t *testing.T) {
	hdr := int(unsafe.Sizeof(wUnicodeString{}))
	for _, b := range [][]byte{nil, make([]byte, 1), make([]byte, hdr-1)} {
		if got := parseCmdlineBuffer(b); got != "" {
			t.Errorf("a %d-byte buffer -> %q, want \"\" (no over-read)", len(b), got)
		}
	}
	// A header present but with a zero inner string -> "".
	if got := parseCmdlineBuffer(make([]byte, hdr)); got != "" {
		t.Errorf("empty UNICODE_STRING -> %q, want \"\"", got)
	}
	// A well-formed, self-referential buffer -> the string.
	const s = `"C:\x\thunderbird.exe" -profile "C:\p"`
	u16, _ := syscall.UTF16FromString(s) // includes a trailing NUL
	buf := make([]byte, hdr+len(u16)*2)
	for i, c := range u16 {
		binary.LittleEndian.PutUint16(buf[hdr+i*2:], c)
	}
	us := (*wUnicodeString)(unsafe.Pointer(&buf[0]))
	us.Length = uint16((len(u16) - 1) * 2) // bytes, excluding the NUL
	us.MaximumLength = us.Length
	us.Buffer = uintptr(unsafe.Pointer(&buf[hdr]))
	if got := parseCmdlineBuffer(buf); got != s {
		t.Errorf("parseCmdlineBuffer -> %q, want %q", got, s)
	}
}

// covers: MA-554
// decodeArgv yields a NON-nil empty argv on an unreadable command line, so the
// Windows identity always uses the exact-argv path and an unreadable command line
// is never matched as ours (adversarial finding F7).
func TestDecodeArgvNonNil(t *testing.T) {
	if argv := decodeArgv(""); argv == nil || len(argv) != 0 {
		t.Errorf("decodeArgv(\"\") = %v (nil=%v), want non-nil empty", argv, argv == nil)
	}
	p := proc{args: "", argv: decodeArgv("")}
	if profileMatches(p, `C:\p`) {
		t.Error("an unreadable command line matched -profile (F7)")
	}
}
