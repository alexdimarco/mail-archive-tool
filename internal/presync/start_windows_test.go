//go:build windows

package presync

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// The Windows start details of design-presync-attended slice AS4 (§2.7, A14; C-56,
// C-58, C-65) through the win32 fake layer. The untagged parts (minimizeMode, the
// session's retry, start-blocked's mapping) are in windows_start_test.go.

// covers: MA-928
// A launch Windows refuses with "access denied" is errStartBlocked (C-65), after
// exactly one launch request — never a second launch without breakaway. Positive
// twin first: a launch that fails another way is a plain error, not start-blocked.
func TestWinStartBlocked(t *testing.T) {
	calls := 0
	old := winStart
	t.Cleanup(func() { winStart = old })
	e := &winEnv{w: fakeWin32{}, clock: time.Now}

	winStart = func(*exec.Cmd) error {
		calls++
		return &os.PathError{Op: "fork/exec", Path: "thunderbird.exe", Err: windows.ERROR_FILE_NOT_FOUND}
	}
	if err := e.startHidden(`C:\Program Files\Mozilla Thunderbird\thunderbird.exe`, `C:\p`); err == nil || errors.Is(err, errStartBlocked) {
		t.Fatalf("twin: a missing program gave %v, want a plain launch error", err)
	}

	calls = 0
	winStart = func(*exec.Cmd) error {
		calls++
		return &os.PathError{Op: "fork/exec", Path: "thunderbird.exe", Err: windows.ERROR_ACCESS_DENIED}
	}
	err := e.startHidden(`C:\Program Files\Mozilla Thunderbird\thunderbird.exe`, `C:\p`)
	if !errors.Is(err, errStartBlocked) {
		t.Errorf("access denied gave %v, want errStartBlocked", err)
	}
	if calls != 1 {
		t.Errorf("%d launch requests, want exactly 1 (no retry without breakaway)", calls)
	}
}

// covers: MA-928
// An attended run's minimize takes minimizeMode's show value through the win32
// layer (A14 (3), C-58): an already minimized window gets no call and counts as
// minimized; one that is not the foreground window gets SW_SHOWMINNOACTIVE; the
// foreground window gets SW_MINIMIZE. A scheduled run keeps SW_MINIMIZE whatever
// the window's state (§1.3). minimizeAgain makes one try with no wait.
func TestWinMinimizeShowMode(t *testing.T) {
	const pid = 4242
	const hwnd = uintptr(0x100)
	start := time.Date(2026, 10, 22, 2, 0, 0, 0, time.UTC)
	child := proc{pid: pid, start: start}
	run := func(iconic bool, fg uintptr, attended bool) ([]int, bool) {
		var shown []int
		e := &winEnv{clock: time.Now, w: fakeWin32{handleOK: true, creation: start.UnixNano(),
			mainOf: map[uint32]uintptr{pid: hwnd}, winPIDof: map[uintptr]uint32{hwnd: pid}, shown: &shown,
			iconicOf: map[uintptr]bool{hwnd: iconic}, fg: fg}}
		ok := e.minimizeMain(context.Background(), nil, child, attended)
		return shown, ok
	}
	if shown, ok := run(false, 0x999, false); !ok || len(shown) != 1 || shown[0] != swMinimize {
		t.Errorf("scheduled: shown %v ok %v, want [SW_MINIMIZE]", shown, ok)
	}
	if shown, ok := run(true, hwnd, false); !ok || len(shown) != 1 || shown[0] != swMinimize {
		t.Errorf("scheduled, already minimized: shown %v ok %v, want [SW_MINIMIZE] (unchanged)", shown, ok)
	}
	if shown, ok := run(true, 0x999, true); !ok || len(shown) != 0 {
		t.Errorf("attended, already minimized: shown %v ok %v, want no call, minimized", shown, ok)
	}
	if shown, ok := run(false, 0x999, true); !ok || len(shown) != 1 || shown[0] != swShowMinNoActive {
		t.Errorf("attended, not the foreground: shown %v ok %v, want [SW_SHOWMINNOACTIVE]", shown, ok)
	}
	if shown, ok := run(false, hwnd, true); !ok || len(shown) != 1 || shown[0] != swMinimize {
		t.Errorf("attended, the foreground: shown %v ok %v, want [SW_MINIMIZE]", shown, ok)
	}

	var shown []int
	e := &winEnv{clock: time.Now, w: fakeWin32{handleOK: true, creation: start.UnixNano(), mainOf: map[uint32]uintptr{}, shown: &shown}}
	if e.minimizeAgain(child, true) || len(shown) != 0 {
		t.Errorf("no window yet: minimizeAgain minimized (%v)", shown)
	}
	e = &winEnv{clock: time.Now, w: fakeWin32{handleOK: true, creation: start.UnixNano(), mainOf: map[uint32]uintptr{pid: hwnd},
		iconicOf: map[uintptr]bool{}, shown: &shown, fg: 0x999}}
	if !e.minimizeAgain(child, true) || len(shown) != 1 || shown[0] != swShowMinNoActive {
		t.Errorf("the window there: minimizeAgain shown %v", shown)
	}
}

// winChildPID and winChildStart are the proven child the minimize tests address.
const winChildPID = 4242

var winChildStart = time.Date(2026, 10, 31, 2, 0, 0, 0, time.UTC)

// tryMinimizeMain and tryMinimizeAgain call the adapter's two minimize attempts on
// the proven child.
func tryMinimizeMain(e *winEnv, attended bool) bool {
	return e.minimizeMain(context.Background(), nil, proc{pid: winChildPID, start: winChildStart}, attended)
}

func tryMinimizeAgain(e *winEnv, attended bool) bool {
	return e.minimizeAgain(proc{pid: winChildPID, start: winChildStart}, attended)
}

// ourWindowFake is a win32 fake with the proven child's single main window 0x100,
// a held handle and the child's own creation time, the window shown (not iconic).
func ourWindowFake(shown *[]int) fakeWin32 {
	return fakeWin32{
		handleOK: true, handleLive: true, creation: winChildStart.UnixNano(),
		mainOf:   map[uint32]uintptr{winChildPID: 0x100},
		winPIDof: map[uintptr]uint32{0x100: winChildPID, 0x999: 777},
		iconicOf: map[uintptr]bool{},
		fg:       0x999, // another program's window has the keyboard
		shown:    shown,
	}
}

// covers: MA-928
// A minimize counts as succeeded only once the window IS minimized (gate-2 pass 1,
// as4win-2; A14 (2), §2.7 "if it never succeeds, Result.Minimized is false"): a
// ShowWindow that did not take — Thunderbird shows its window again — is not
// success, so the session tries again on the next poll and every Windows text says
// "its window may still be on screen". Positive twin first: a minimize that takes
// counts, from both attempts and in both modes.
func TestWinMinimizeCountsOnlyWhenItTook(t *testing.T) {
	for _, attended := range []bool{false, true} {
		var shown []int
		if !tryMinimizeMain(&winEnv{clock: time.Now, w: ourWindowFake(&shown)}, attended) || len(shown) != 1 {
			t.Fatalf("twin (attended %v): a minimize that took was not counted (shown %v)", attended, shown)
		}
		shown = nil
		if !tryMinimizeAgain(&winEnv{clock: time.Now, w: ourWindowFake(&shown)}, attended) || len(shown) != 1 {
			t.Fatalf("twin (attended %v): a retry that took was not counted (shown %v)", attended, shown)
		}

		shown = nil
		f := ourWindowFake(&shown)
		f.noTake = true
		if tryMinimizeMain(&winEnv{clock: time.Now, w: f}, attended) {
			t.Errorf("attended %v: minimizeMain counted a minimize that did not take (shown %v)", attended, shown)
		}
		if tryMinimizeAgain(&winEnv{clock: time.Now, w: f}, attended) {
			t.Errorf("attended %v: minimizeAgain counted a minimize that did not take (shown %v)", attended, shown)
		}
		if len(shown) == 0 {
			t.Errorf("attended %v: nothing was tried", attended)
		}
	}
}

// covers: MA-928
// Thunderbird has the keyboard when the foreground window is ANY window of its
// process — its main window or a dialog it owns, such as the not-the-default-mail-app
// prompt (gate-2 pass 1, as4win-3; A14 (3)) — so an attended run then minimizes
// with SW_MINIMIZE, which hands activation back, never SW_SHOWMINNOACTIVE, which
// would leave the keyboard with Thunderbird. Positive twin first: another program's
// window in front gets SW_SHOWMINNOACTIVE.
func TestWinMinimizeForegroundDialog(t *testing.T) {
	var shown []int
	if tryMinimizeMain(&winEnv{clock: time.Now, w: ourWindowFake(&shown)}, true); len(shown) != 1 || shown[0] != swShowMinNoActive {
		t.Fatalf("twin: another program in front: shown %v, want [SW_SHOWMINNOACTIVE]", shown)
	}
	for _, again := range []bool{false, true} {
		shown = nil
		f := ourWindowFake(&shown)
		f.fg = 0x101 // a dialog Thunderbird's main window owns
		f.winPIDof[0x101] = winChildPID
		e := &winEnv{clock: time.Now, w: f}
		if again {
			tryMinimizeAgain(e, true)
		} else {
			tryMinimizeMain(e, true)
		}
		if len(shown) != 1 || shown[0] != swMinimize {
			t.Errorf("retry %v: Thunderbird's own dialog in front: shown %v, want [SW_MINIMIZE]", again, shown)
		}
	}
}

// covers: MA-930
// A retry never moves the person's keyboard (gate-2 pass 1, as4win-5): the session
// retries the minimize on every poll for up to 20 minutes, long after a scheduled
// run's idle check, so each retry — scheduled or attended — takes minimizeMode's
// choice: SW_SHOWMINNOACTIVE while another program has the keyboard, SW_MINIMIZE
// only when Thunderbird took it. Positive twin first: a scheduled run's first
// attempt, made right after its idle check, keeps SW_MINIMIZE (§1.3).
func TestWinMinimizeRetryNeverActivates(t *testing.T) {
	var shown []int
	if tryMinimizeMain(&winEnv{clock: time.Now, w: ourWindowFake(&shown)}, false); len(shown) != 1 || shown[0] != swMinimize {
		t.Fatalf("twin: a scheduled first attempt: shown %v, want [SW_MINIMIZE]", shown)
	}
	for _, attended := range []bool{false, true} {
		shown = nil
		tryMinimizeAgain(&winEnv{clock: time.Now, w: ourWindowFake(&shown)}, attended)
		if len(shown) != 1 || shown[0] != swShowMinNoActive {
			t.Errorf("attended %v: a retry while another program has the keyboard: shown %v, want [SW_SHOWMINNOACTIVE]", attended, shown)
		}
		shown = nil
		f := ourWindowFake(&shown)
		f.fg = 0x100
		tryMinimizeAgain(&winEnv{clock: time.Now, w: f}, attended)
		if len(shown) != 1 || shown[0] != swMinimize {
			t.Errorf("attended %v: a retry while Thunderbird has the keyboard: shown %v, want [SW_MINIMIZE]", attended, shown)
		}
	}
}

// covers: MA-930
// The minimize addresses only the process the session adopted (gate-2 pass 1,
// as4win-6), as the quit does (F1, PC-W3): it holds a handle to the pid across the
// attempt, so Windows cannot hand the number to another program meanwhile, and
// re-proves through it, by exact creation time, that the process is the child —
// a pid reused after our Thunderbird exited is another program, whose window is
// never minimized. Positive twin first: the child's own window is minimized.
func TestWinMinimizeOnlyOurProcess(t *testing.T) {
	old := minimizeWait
	minimizeWait = time.Second
	t.Cleanup(func() { minimizeWait = old })

	var shown []int
	if !tryMinimizeAgain(&winEnv{clock: time.Now, w: ourWindowFake(&shown)}, false) || len(shown) != 1 {
		t.Fatalf("twin: the child's own window: shown %v", shown)
	}
	for name, change := range map[string]func(*fakeWin32){
		"the pid reused by another program": func(f *fakeWin32) { f.creation = winChildStart.UnixNano() + 1 },
		"the creation time unreadable":      func(f *fakeWin32) { f.creation = 0 },
		"no handle held":                    func(f *fakeWin32) { f.handleOK = false },
	} {
		shown = nil
		f := ourWindowFake(&shown)
		change(&f)
		e := &winEnv{clock: time.Now, w: f}
		if tryMinimizeAgain(e, false) || tryMinimizeMain(e, false) || len(shown) != 0 {
			t.Errorf("%s: a window was minimized (shown %v), want none and not minimized", name, shown)
		}
	}
}

// covers: MA-928
// A Desktop started "as administrator" ends refused-elevated with the Desktop's own
// line (C-65): nothing is started, and the narration tells the person to start
// Mail Archive Desktop normally, then click Run now again.
func TestWinElevatedDesktopRefusal(t *testing.T) {
	var log bytes.Buffer
	e := &winEnv{w: fakeWin32{isElev: true}, clock: time.Now,
		appCands: func() []string { return nil }, roaming: func() (string, bool) { return "", false }}
	cfg := Config{Profile: t.TempDir(), StateDir: t.TempDir(), Attended: true, Surface: SurfaceDesktop, GOOS: "windows", Log: &log}
	res := runWith(context.Background(), cfg, e, sessionCtl{})
	if res.Release != nil {
		defer res.Release()
	}
	if res.Outcome != OutcomeRefusedElevated {
		t.Fatalf("outcome %s, want refused-elevated", res.Outcome.Name)
	}
	if !strings.Contains(log.String(), "Start Mail Archive Desktop normally, not as administrator, then click Run now again.") {
		t.Errorf("the narration lacks the Desktop's elevated line: %q", log.String())
	}
}
