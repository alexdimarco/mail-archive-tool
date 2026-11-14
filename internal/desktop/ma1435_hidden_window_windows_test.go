//go:build windows

package desktop

import (
	"sync/atomic"
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
//
// Amended 2026-11-13 (the S1 fix round; amended, not weakened): WM_ENDSESSION stops the
// copy only when the session really ends or Restart Manager asks (ENDSESSION_CLOSEAPP;
// A5), and an installer's close request is refused while work or a wizard runs (A2).
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
	origEnding := sessionEnding
	t.Cleanup(func() { sessionEnding = origEnding })
	ending := false
	sessionEnding = func() bool { return ending }
	busy := true
	loopMu.Lock()
	loopEvents = LoopEvents{EndSession: func(time.Duration) { ended++ }, Busy: func() bool { return busy }}
	loopMu.Unlock()
	if r := wndProc(0, wmCommand, 1, 0); r != 0 || ended != 0 {
		t.Errorf("WM_COMMAND: %d, stops %d; want ignored", r, ended)
	}
	if r := wndProc(0, wmQueryEndSession, 0, 0); r != 1 {
		t.Errorf("WM_QUERYENDSESSION (sign-out) while busy answered %d, want 1 (allowed)", r)
	}
	if r := wndProc(0, wmQueryEndSession, 0, endsessionCloseApp); r != 0 {
		t.Errorf("an installer's close request while busy answered %d, want 0 (refused: A2)", r)
	}
	busy = false
	if r := wndProc(0, wmQueryEndSession, 0, endsessionCloseApp); r != 1 {
		t.Errorf("an installer's close request while idle answered %d, want 1", r)
	}
	// A5: the same WM_ENDSESSION at another time does nothing.
	if wndProc(0, wmEndSession, 1, 0); ended != 0 {
		t.Errorf("a WM_ENDSESSION while the session is not ending stopped the copy")
	}
	ending = true
	if r := wndProc(0, wmEndSession, 1, 0); r != 0 || ended != 1 {
		t.Errorf("WM_ENDSESSION at a real sign-out: %d, stops %d; want the one stop", r, ended)
	}
	ending = false
	if wndProc(0, wmEndSession, 1, endsessionCloseApp); ended != 2 {
		t.Errorf("Restart Manager's WM_ENDSESSION did not stop the copy")
	}
	if r := wndProc(0, wmEndSession, 0, endsessionCloseApp); ended != 2 || r != 0 {
		t.Errorf("WM_ENDSESSION with the end cancelled stopped the instance")
	}
}

// covers: MA-1435, MA-1424, R52
// A close from outside never leaves the copy deaf to sign-out (the S1 adversarial pass,
// A1). Positive twin first: the serving's own end takes the window down and the loop
// ends, with no stop asked. A WM_CLOSE sent to the real hidden window (what `taskkill`
// without /F and the upgrade script send) is refused while the copy is busy (A2), and,
// once it is idle, asks the one stop; the window stays in place either way, still
// answering the end of the session; a WM_QUIT posted to it, which ends its loop, is a
// stop request too.
func TestMA1435_ACloseFromOutsideIsAStopThatKeepsTheWindow(t *testing.T) {
	var closes atomic.Int32
	err := windowsLoop(func() error { time.Sleep(150 * time.Millisecond); return nil },
		LoopEvents{Close: func() { closes.Add(1) }, Busy: func() bool { return false }})
	if err != nil || closes.Load() != 0 {
		t.Fatalf("twin: the serving's own end: %v, stops %d", err, closes.Load())
	}

	release := make(chan struct{})
	done := make(chan error, 1)
	var busy atomic.Bool
	busy.Store(true)
	go func() {
		done <- windowsLoop(func() error { <-release; return nil },
			LoopEvents{Close: func() { closes.Add(1) }, Busy: busy.Load})
	}()
	var h uintptr
	deadline := time.Now().Add(5 * time.Second)
	for h == 0 && time.Now().Before(deadline) {
		loopMu.Lock()
		h = loopHwnd
		loopMu.Unlock()
		time.Sleep(20 * time.Millisecond)
	}
	if h == 0 {
		t.Fatal("no hidden window")
	}
	procSendMessage := user32.NewProc("SendMessageW")
	procIsWindow := user32.NewProc("IsWindow")
	// Busy (work or an open wizard): the close is refused, the copy keeps running (A2).
	procSendMessage.Call(h, wmClose, 0, 0)
	time.Sleep(300 * time.Millisecond)
	if closes.Load() != 0 {
		t.Errorf("a WM_CLOSE while busy stopped the copy")
	}
	busy.Store(false)
	procSendMessage.Call(h, wmClose, 0, 0)
	waitFor := func(what string, cond func() bool) {
		t.Helper()
		end := time.Now().Add(5 * time.Second)
		for !cond() {
			if time.Now().After(end) {
				t.Fatalf("timed out waiting for %s", what)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	waitFor("the stop a WM_CLOSE asks", func() bool { return closes.Load() == 1 })
	if ok, _, _ := procIsWindow.Call(h); ok == 0 {
		t.Errorf("a WM_CLOSE from outside destroyed the hidden window")
	}
	const wmQuit = 0x0012
	procPostMessageW.Call(h, wmQuit, 0, 0)
	waitFor("the stop the lost loop asks", func() bool { return closes.Load() == 2 })
	close(release)
	if err := <-done; err != nil {
		t.Errorf("the loop: %v", err)
	}
}
