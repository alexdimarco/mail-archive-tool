//go:build windows

package presync

import (
	"context"
	"errors"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// covers: MA-570
// selectMainWindow returns the one application main window — visible, top-level
// (no WS_CHILD), titled (WS_CAPTION), not a tool window, no owner — and refuses a
// set with none OR more than one such window, so an ambiguous set is never quit
// (R31-safe), exactly as findOurs refuses an ambiguous process set.
func TestSelectMainWindow(t *testing.T) {
	main := winWindowInfo{hwnd: 0x100, visible: true, style: wWsCaption}

	if hwnd, ok := selectMainWindow([]winWindowInfo{main}); !ok || hwnd != 0x100 {
		t.Errorf("a lone main window was not selected: (%#x,%v)", hwnd, ok)
	}

	// Each disqualifier, alongside the real main window, must be ignored (and the
	// main window still chosen) — none of them is ever treated as the main window.
	bad := []winWindowInfo{
		{hwnd: 0x201, style: wWsCaption},                                  // invisible
		{hwnd: 0x202, visible: true, style: wWsCaption | wWsChild},        // a child
		{hwnd: 0x203, visible: true, style: 0},                            // no title bar
		{hwnd: 0x204, visible: true, style: wWsCaption, exStyle: wWsExToolWindow}, // tool window
		{hwnd: 0x205, visible: true, style: wWsCaption, owner: 0x999},     // owned (a dialog)
	}
	for _, b := range bad {
		if _, ok := selectMainWindow([]winWindowInfo{b}); ok {
			t.Errorf("a disqualified window (%#x) was selected as main", b.hwnd)
		}
		if hwnd, ok := selectMainWindow([]winWindowInfo{b, main}); !ok || hwnd != 0x100 {
			t.Errorf("with %#x present the real main window was not chosen: (%#x,%v)", b.hwnd, hwnd, ok)
		}
	}

	// Two qualifying windows → ambiguous → nothing chosen.
	main2 := winWindowInfo{hwnd: 0x101, visible: true, style: wWsCaption}
	if _, ok := selectMainWindow([]winWindowInfo{main, main2}); ok {
		t.Error("two main windows must be ambiguous (no quit), not a pick")
	}

	// Empty set → nothing chosen.
	if _, ok := selectMainWindow(nil); ok {
		t.Error("an empty window set selected a main window")
	}
}

// covers: MA-570, MA-900
// quit posts WM_CLOSE to the proven pid's main window ONLY when a handle is held,
// a single main window is found, and the last-instant re-checks pass (the handle
// still alive AND the window still owned by the pid). Every "cannot safely close"
// path posts nothing and returns errQuitNotPosted (C-60), so S8 ends at once as
// presync_quit_no_window (left-running, announced) with no exit-grace wait and no
// "asked to close" text — Thunderbird is never force-closed. A post Windows
// refuses also posted nothing, and says so the same way.
func TestWinQuitOrchestration(t *testing.T) {
	const pid = 4242
	const hwnd = uintptr(0x100)
	start := time.Date(2026, 10, 22, 2, 0, 0, 0, time.UTC)
	child := proc{pid: pid, start: start}
	ours := start.UnixNano() // the creation time that proves the pinned pid is ours

	run := func(f fakeWin32) ([]uintptr, error) {
		var posted []uintptr
		f.posted = &posted
		e := &winEnv{w: f}
		err := e.quit(context.Background(), child)
		return posted, err
	}

	// Happy path: handle held, identity re-proved (creation matches), one main
	// window, alive, pid still matches → posted, nil.
	posted, err := run(fakeWin32{
		handleOK: true, handleLive: true, creation: ours,
		mainOf:   map[uint32]uintptr{pid: hwnd},
		winPIDof: map[uintptr]uint32{hwnd: pid},
	})
	if len(posted) != 1 || posted[0] != hwnd || err != nil {
		t.Errorf("happy path posted %v (err %v), want [%#x] and nil", posted, err, hwnd)
	}

	// The post itself refused (the window destroyed in the gap): nothing posted.
	if _, err := run(fakeWin32{
		handleOK: true, handleLive: true, creation: ours,
		mainOf:   map[uint32]uintptr{pid: hwnd},
		winPIDof: map[uintptr]uint32{hwnd: pid},
		postErr:  errors.New("invalid window handle"),
	}); !errors.Is(err, errQuitNotPosted) {
		t.Errorf("a refused post returned %v, want errQuitNotPosted", err)
	}

	// Every "cannot safely close" path posts nothing. Each sets creation=ours so it
	// reaches the specific guard under test — except the reuse case, which is the
	// creation-time mismatch itself (F1).
	cannot := map[string]fakeWin32{
		"no handle held": {handleOK: false, handleLive: true, creation: ours,
			mainOf: map[uint32]uintptr{pid: hwnd}, winPIDof: map[uintptr]uint32{hwnd: pid}},
		"pid reused before the hold (creation mismatch)": {handleOK: true, handleLive: true, creation: ours + 1,
			mainOf: map[uint32]uintptr{pid: hwnd}, winPIDof: map[uintptr]uint32{hwnd: pid}},
		"no single main window": {handleOK: true, handleLive: true, creation: ours,
			mainOf: map[uint32]uintptr{pid: 0}, winPIDof: map[uintptr]uint32{hwnd: pid}},
		"process died at re-check": {handleOK: true, handleLive: false, creation: ours,
			mainOf: map[uint32]uintptr{pid: hwnd}, winPIDof: map[uintptr]uint32{hwnd: pid}},
		"window reassigned to another pid": {handleOK: true, handleLive: true, creation: ours,
			mainOf: map[uint32]uintptr{pid: hwnd}, winPIDof: map[uintptr]uint32{hwnd: 9999}},
	}
	for name, f := range cannot {
		posted, err := run(f)
		if len(posted) != 0 {
			t.Errorf("%s: posted %v, want nothing (leave it running)", name, posted)
		}
		if !errors.Is(err, errQuitNotPosted) {
			t.Errorf("%s: quit returned %v, want errQuitNotPosted (C-60)", name, err)
		}
	}
}

// covers: MA-921
// The Windows quit starts no WM_CLOSE once its context has ended (gate-2
// adversarial pass 1, as1a-4; design-presync-attended §2.6: "no quit request is
// started after the watchdog returns or after a Lock"). The context is the
// session's quit context, which the watchdog's give-up and a Stop cancel; a quit
// that reaches its handle, creation-time and window checks after that must still
// post nothing and say nothing was asked (errQuitNotPosted). Positive twin first: a
// live context posts once. Confirmed failing on the Windows 11 lab VM 2026-10-31
// (posted [0x100], returned nil); encoded as an open gap until the owner rules.
func TestWinQuitPostsNothingOnceItsContextEnded(t *testing.T) {
	t.Skip("open (owner decision, as1a-4): the fix is a context check inside winEnv.quit, which the MA-927 code pin covers; its hash may change only together with a new MA-918 lab record (docs/lab/presync-windows-code-pin.txt, C-66), and none exists yet. Remove this skip in the commit that lands the fix.")
	const pid = 4242
	const hwnd = uintptr(0x100)
	start := time.Date(2026, 10, 22, 2, 0, 0, 0, time.UTC)
	child := proc{pid: pid, start: start}
	run := func(ctx context.Context) ([]uintptr, error) {
		var posted []uintptr
		e := &winEnv{w: fakeWin32{
			handleOK: true, handleLive: true, creation: start.UnixNano(),
			mainOf: map[uint32]uintptr{pid: hwnd}, winPIDof: map[uintptr]uint32{hwnd: pid},
			posted: &posted,
		}}
		err := e.quit(ctx, child)
		return posted, err
	}
	if posted, err := run(context.Background()); len(posted) != 1 || err != nil {
		t.Fatalf("twin: a live context posted %v (err %v), want one WM_CLOSE", posted, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	posted, err := run(ctx)
	if len(posted) != 0 {
		t.Errorf("posted %v after the quit context ended, want nothing (§2.6)", posted)
	}
	if !errors.Is(err, errQuitNotPosted) {
		t.Errorf("quit returned %v after its context ended, want errQuitNotPosted", err)
	}
}

// covers: MA-571
// The launch (design §2, §6): OUT of the task's job object
// (CREATE_BREAKAWAY_FROM_JOB) so a Task-Scheduler teardown cannot hard-kill
// Thunderbird mid-write (PC-W6). It does NOT set HideWindow / CREATE_NO_WINDOW:
// the L-W4 lab showed Thunderbird ignores SW_HIDE, so those were dropped as
// ineffective (the window is minimized after it appears instead). A pure builder,
// so the flags are pinned without a launch.
func TestBuildLaunchCmdFlags(t *testing.T) {
	const exe = `C:\Program Files\Mozilla Thunderbird\thunderbird.exe`
	const prof = `C:\Users\First Last\AppData\Roaming\Thunderbird\Profiles\abc.default`
	cmd := buildLaunchCmd(exe, prof)

	if len(cmd.Args) != 3 || cmd.Args[1] != "-profile" || cmd.Args[2] != prof {
		t.Errorf("args = %v, want [%q -profile %q]", cmd.Args, exe, prof)
	}
	spa := cmd.SysProcAttr
	if spa == nil {
		t.Fatal("SysProcAttr is nil")
	}
	if spa.CreationFlags&windows.CREATE_BREAKAWAY_FROM_JOB == 0 {
		t.Error("launch lacks CREATE_BREAKAWAY_FROM_JOB — a task teardown could hard-kill Thunderbird (PC-W6)")
	}
	if spa.HideWindow {
		t.Error("launch sets HideWindow — dropped as ineffective (Thunderbird ignores SW_HIDE)")
	}
	if spa.CreationFlags&windows.CREATE_NO_WINDOW != 0 {
		t.Error("launch sets CREATE_NO_WINDOW — dropped as ineffective")
	}
	var _ *syscall.SysProcAttr = spa
}

// covers: MA-928
// The launch's working folder is Thunderbird's own install folder (C-65), both
// modes, so a Thunderbird left open for hours never pins the folder Mail Archive
// was started in. On real Windows, winInstallDir (the untagged pure function the
// Linux gate checks) also agrees with filepath.Dir for each path.
func TestBuildLaunchCmdWorkingFolder(t *testing.T) {
	const exe = `C:\Program Files\Mozilla Thunderbird\thunderbird.exe`
	cmd := buildLaunchCmd(exe, `C:\p`)
	if cmd.Dir != `C:\Program Files\Mozilla Thunderbird` {
		t.Errorf("launch working folder = %q, want Thunderbird's install folder", cmd.Dir)
	}
	for _, p := range []string{exe, `C:\thunderbird.exe`, `\\server\share\Thunderbird\thunderbird.exe`, `D:\Apps\Thunderbird\thunderbird.exe`} {
		if got, want := winInstallDir(p), filepath.Dir(p); got != want {
			t.Errorf("winInstallDir(%q) = %q, filepath.Dir = %q", p, got, want)
		}
	}
}

// covers: MA-571
// minimizeMain minimizes the visible-launch window once it appears (owner ruling
// 2026-10-23): when a single main window is found it is minimized via
// ShowWindow(SW_MINIMIZE) — a benign state change, never a close. It is a no-op on
// a truly-hidden adapter (macOS does not implement windowMinimizer).
func TestWinMinimizeMain(t *testing.T) {
	const pid = 4242
	const hwnd = uintptr(0x100)
	var minimized []uintptr
	e := &winEnv{
		clock: time.Now,
		w: fakeWin32{
			mainOf:    map[uint32]uintptr{pid: hwnd},
			minimized: &minimized,
		},
	}
	e.minimizeMain(context.Background(), nil, pid, false)
	if len(minimized) != 1 || minimized[0] != hwnd {
		t.Errorf("minimizeMain minimized %v, want [%#x]", minimized, hwnd)
	}

	// winEnv satisfies the session's optional windowMinimizer capability.
	var _ windowMinimizer = e
}

// covers: MA-921
// minimizeMain's wait for the window (up to minimizeWait, 15 s) ends at once when
// the session context ends — a Lock, Shut down or the watchdog abandoning the
// session (C-64) — rather than polling on with a plain sleep. Positive twin: with
// a window there and nothing ended, it minimizes.
func TestWinMinimizeMainEndsOnStop(t *testing.T) {
	const pid = 4242
	var minimized []uintptr
	e := &winEnv{clock: time.Now, w: fakeWin32{mainOf: map[uint32]uintptr{pid: 0x100}, minimized: &minimized}}
	e.minimizeMain(context.Background(), nil, pid, false)
	if len(minimized) != 1 {
		t.Fatalf("twin: minimized %v, want the window minimized", minimized)
	}

	minimized = nil
	e = &winEnv{clock: time.Now, w: fakeWin32{mainOf: map[uint32]uintptr{}, minimized: &minimized}} // no window ever appears
	stop, cancel := context.WithCancel(context.Background())
	time.AfterFunc(50*time.Millisecond, cancel)
	start := time.Now()
	e.minimizeMain(context.Background(), stop, pid, false)
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("minimizeMain waited %v after the session context ended, want it to stop at once", took)
	}
	if len(minimized) != 0 {
		t.Errorf("minimized %v with no window, want nothing", minimized)
	}
}

// covers: MA-557
// The WM_CLOSE golden (design §7, PC-W7): the only window message this adapter
// posts is WM_CLOSE (0x0010) — the title-bar X's own message, handled by
// Thunderbird's shutdown path — never WM_QUIT/WM_DESTROY/EndTask or a kill (those
// are forbidden by name in the hygiene walk, TestPresyncExecSignalHygiene).
func TestWinQuitPostsOnlyClose(t *testing.T) {
	if wWmClose != 0x0010 {
		t.Errorf("wWmClose = %#x, want WM_CLOSE 0x0010", wWmClose)
	}
}
