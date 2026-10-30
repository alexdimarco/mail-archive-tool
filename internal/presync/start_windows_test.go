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
	run := func(iconic bool, fg uintptr, attended bool) ([]int, bool) {
		var shown []int
		e := &winEnv{clock: time.Now, w: fakeWin32{mainOf: map[uint32]uintptr{pid: hwnd}, shown: &shown,
			iconicOf: map[uintptr]bool{hwnd: iconic}, fg: fg}}
		ok := e.minimizeMain(context.Background(), nil, pid, attended)
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
	e := &winEnv{clock: time.Now, w: fakeWin32{mainOf: map[uint32]uintptr{}, shown: &shown}}
	if e.minimizeAgain(pid, true) || len(shown) != 0 {
		t.Errorf("no window yet: minimizeAgain minimized (%v)", shown)
	}
	e = &winEnv{clock: time.Now, w: fakeWin32{mainOf: map[uint32]uintptr{pid: hwnd}, shown: &shown, fg: 0x999}}
	if !e.minimizeAgain(pid, true) || len(shown) != 1 || shown[0] != swShowMinNoActive {
		t.Errorf("the window there: minimizeAgain shown %v", shown)
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
