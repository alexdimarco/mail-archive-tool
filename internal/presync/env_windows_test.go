//go:build windows

package presync

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// fakeWin32 scripts the Win32 seam so the winEnv logic is tested without real
// Win32 or a GUI (the equivalent of macEnv's fake exec runner).
type fakeWin32 struct {
	procs     []winProc
	aliveSet  map[uint32]bool
	exeOf     map[uint32]string
	boot      int64
	idle      float64
	idleOK    bool
	isElev    bool
}

func (f fakeWin32) thunderbirdProcs() []winProc       { return f.procs }
func (f fakeWin32) alive(pid uint32) bool             { return f.aliveSet[pid] }
func (f fakeWin32) exePath(pid uint32) (string, bool) { e, ok := f.exeOf[pid]; return e, ok }
func (f fakeWin32) bootUnix() int64                   { return f.boot }
func (f fakeWin32) idleSeconds() (float64, bool)      { return f.idle, f.idleOK }
func (f fakeWin32) elevated() bool                    { return f.isElev }

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
