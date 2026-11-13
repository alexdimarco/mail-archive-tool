//go:build windows

package desktop

import (
	"testing"
	"time"
)

// covers: MA-1435, MA-1424, R39, R52
// The instance's hidden window on Windows (design-menubar MB-8, MB-17, MB-2). Positive
// twin first: a real hidden window is made and its message loop ends when the serving
// ends. Its style has no title bar and is not a child, so dialogfront never counts it as
// a dialog; a posted WM_COMMAND does nothing (no other process of the session can post a
// command to the instance); WM_QUERYENDSESSION allows the end; WM_ENDSESSION runs the
// one stop, waiting, and asks nothing.
func TestMA1435_TheHiddenWindowIsNoDialogAndTakesNoCommand(t *testing.T) {
	served := make(chan struct{})
	ended := 0
	err := windowsLoop(func() error {
		defer close(served)
		time.Sleep(200 * time.Millisecond)
		return nil
	}, LoopEvents{EndSession: func(time.Duration) { ended++ }, Busy: func() bool { return false }})
	if err != nil {
		t.Fatalf("the loop around a serving that ends: %v", err)
	}
	<-served
	if hiddenWindowStyle&0x00C00000 != 0 || hiddenWindowStyle&0x40000000 != 0 {
		t.Errorf("the hidden window's style %#x has a title bar or is a child", hiddenWindowStyle)
	}
	loopMu.Lock()
	loopEvents = LoopEvents{EndSession: func(time.Duration) { ended++ }, Busy: func() bool { return true }}
	loopMu.Unlock()
	if r := wndProc(0, wmCommand, 1, 0); r != 0 || ended != 0 {
		t.Errorf("WM_COMMAND: %d, stops %d; want ignored", r, ended)
	}
	if r := wndProc(0, wmQueryEndSession, 0, 0); r != 1 {
		t.Errorf("WM_QUERYENDSESSION answered %d, want 1 (allowed)", r)
	}
	if r := wndProc(0, wmEndSession, 1, 0); r != 0 || ended != 1 {
		t.Errorf("WM_ENDSESSION: %d, stops %d; want the one stop", r, ended)
	}
	if r := wndProc(0, wmEndSession, 0, 0); ended != 1 || r != 0 {
		t.Errorf("WM_ENDSESSION with the end cancelled stopped the instance")
	}
}
