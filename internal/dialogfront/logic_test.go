package dialogfront

import (
	"strings"
	"testing"
	"time"
)

// covers: MA-1112, R39, R33
// The watch's choices, as plain logic: only a whole top-level window with a title
// bar is brought forward (zenity's own dialog style and a message box's are; a
// control inside a dialog, a part of a window, a tooltip-like popup with no title
// bar and a window that is not its own root are not), and a later try happens only
// while the window is shown, has never been the active window, is not active now,
// and the person has not moved to another window. A person who clicks back into
// the browser after the dialog was active is not pulled into it again (review
// finding 4). The tries come at 200, 600 and 1200 ms, in order.
func TestOnlyADialogIsBroughtForwardAndOnlyUntilThePersonMovesOn(t *testing.T) {
	const (
		wsPopup       = 0x80000000
		wsBorder      = 0x00800000
		wsDlgFrame    = 0x00400000
		wsSysMenu     = 0x00080000
		wsClipSibling = 0x04000000
		wsVisible     = 0x10000000
		zenityDialog  = wsPopup | wsBorder | wsSysMenu | wsClipSibling | wsDlgFrame // _WS_ZEN_DIALOG
		messageBox    = wsPopup | wsBorder | wsDlgFrame | wsSysMenu | wsVisible
		objidCursor   = -9
	)
	// Positive twins first.
	for name, style := range map[string]uint32{"zenity's dialog": zenityDialog, "a message box": messageBox} {
		if !isDialogWindow(objidWindow, childidSelf, true, style) {
			t.Errorf("%s is not brought forward", name)
		}
	}
	for _, c := range []struct {
		name              string
		idObject, idChild int32
		isRoot            bool
		style             uint32
	}{
		{"a control inside a dialog", objidWindow, childidSelf, false, wsChild | wsVisible},
		{"a child window with a caption", objidWindow, childidSelf, true, wsChild | wsCaption},
		{"a popup with no title bar", objidWindow, childidSelf, true, wsPopup | wsBorder},
		{"a window that is not its own root", objidWindow, childidSelf, false, zenityDialog},
		{"a part of the window", objidWindow, 3, true, zenityDialog},
		{"the cursor", objidCursor, childidSelf, true, zenityDialog},
	} {
		if isDialogWindow(c.idObject, c.idChild, c.isRoot, c.style) {
			t.Errorf("%s is brought forward", c.name)
		}
	}

	const dialog, browser, other = 0x10, 0x20, 0x30
	for _, c := range []struct {
		name                    string
		visible, beenActive     bool
		active, activeWhenShown uintptr
		want                    bool
	}{
		{"still behind the browser", true, false, browser, browser, true},
		{"no window active", true, false, 0, browser, true},
		{"already in front", true, false, dialog, browser, false},
		{"closed", false, false, browser, browser, false},
		{"the person moved to another window", true, false, other, browser, false},
		{"it was active, and the person clicked back into the browser", true, true, browser, browser, false},
		{"it was active, and no window is active now", true, true, 0, browser, false},
	} {
		if got := tryAgain(c.visible, c.beenActive, c.active, dialog, c.activeWhenShown); got != c.want {
			t.Errorf("%s: tryAgain = %v, want %v", c.name, got, c.want)
		}
	}

	want := []time.Duration{200 * time.Millisecond, 600 * time.Millisecond, 1200 * time.Millisecond}
	if len(retryAfter) != len(want) {
		t.Fatalf("retryAfter = %v, want %v", retryAfter, want)
	}
	for i := range want {
		if retryAfter[i] != want[i] {
			t.Errorf("retryAfter = %v, want %v", retryAfter, want)
		}
	}
}

// covers: MA-1114, R39
// A dialog takes the keyboard only when the person asked for it (review finding 1:
// a dialog that opened after a long export answered with keys typed into the
// browser). It may when the active window is already ours, or when Arm was called
// at most armFor (8 s) ago and no dialog has used it since: one ask, one dialog.
// Positive twins first: a dialog shown just after Arm, and one opened from our own
// active dialog, take it. Then: no ask, an ask older than armFor, an ask already
// used, and an ask "from the future" (a clock step) do not. The watch's own ask
// follows the person's answer: a dialog that closes while it is the active window
// was answered (the caller then calls Arm); one the person left for another window
// while it was still on screen was not, even when the other window became active
// before the notice of its closing arrived or after; and a window of ours with no
// title bar becoming active leaves the record as it is.
func TestADialogTakesTheKeyboardOnlyWhenThePersonAskedForIt(t *testing.T) {
	// Positive twins.
	asked.Store(0)
	Arm()
	since, ok := takeAsk()
	if !ok || !mayTakeKeyboard(false, ok, since) {
		t.Errorf("a dialog shown just after Arm does not take the keyboard (asked %v, %v ago)", ok, since)
	}
	if !mayTakeKeyboard(true, false, 0) {
		t.Error("a dialog opened from our own active dialog does not take the keyboard")
	}
	if !mayTakeKeyboard(false, true, armFor) {
		t.Errorf("an ask exactly %v old does not count", armFor)
	}

	if _, ok := takeAsk(); ok {
		t.Error("one ask brought two dialogs forward: takeAsk did not clear it")
	}
	if mayTakeKeyboard(false, false, 0) {
		t.Error("a dialog nobody asked for takes the keyboard")
	}
	if mayTakeKeyboard(false, true, armFor+time.Millisecond) {
		t.Errorf("an ask older than %v still counts", armFor)
	}
	if mayTakeKeyboard(false, true, -time.Second) {
		t.Error("an ask dated after the dialog counts")
	}
	restore := steady
	clock := time.Hour
	steady = func() time.Duration { return clock }
	Arm()
	clock += armFor + time.Second
	if since, ok := takeAsk(); mayTakeKeyboard(false, ok, since) {
		t.Errorf("an ask made %v ago still brings a dialog forward", since)
	}
	steady = restore
	if armFor != 8*time.Second {
		t.Errorf("armFor = %v, want 8s", armFor)
	}

	const dialog, next, browser, ourHidden = 0x10, 0x11, 0x20, 0x40
	visible := map[uintptr]bool{}
	onScreen := func(h uintptr) bool { return visible[h] }

	// The person answers the dialog: it is active when it closes, the keyboard goes
	// on to the browser after (in either order of the notices).
	for _, order := range []string{"closing notice first", "keyboard notice first"} {
		w := newWatch()
		w.showed(dialog, true, browser, false)
		visible[dialog] = true
		w.foreground(dialog, true, onScreen)
		if !w.wasActive(dialog) {
			t.Errorf("%s: the active dialog is not recorded as having been active", order)
		}
		w.foreground(ourHidden, true, onScreen) // a window of ours that is no dialog
		visible[dialog] = false
		var answered bool
		if order == "closing notice first" {
			answered, _ = w.hidden(dialog)
			w.foreground(browser, false, onScreen)
		} else {
			w.foreground(browser, false, onScreen)
			answered, _ = w.hidden(dialog)
		}
		if !answered {
			t.Errorf("%s: a dialog closed while it was the active window is not counted as answered", order)
		}
	}

	// The person moves to the browser while the dialog is on screen (the export's
	// progress window), then it closes: not answered, so the next dialog (the
	// summary) does not take the keyboard.
	w := newWatch()
	w.showed(dialog, true, browser, false)
	visible[dialog] = true
	w.foreground(dialog, true, onScreen)
	w.foreground(browser, false, onScreen)
	visible[dialog] = false
	if answered, _ := w.hidden(dialog); answered {
		t.Error("a dialog the person left for the browser is counted as answered when it closes")
	}
	// A dialog that never became active is not answered either; a window that was
	// never recorded is ignored.
	w.showed(next, false, browser, false)
	if answered, last := w.hidden(next); answered || !last {
		t.Errorf("a dialog never active: answered %v, last %v (want false, true)", answered, last)
	}
	if answered, last := w.hidden(0x99); answered || last {
		t.Errorf("an unknown window: answered %v, last %v (want false, false)", answered, last)
	}
}

// covers: MA-1116, R39
// When the last of our dialogs closes, the keyboard goes back to the window that
// was active before our dialogs took it (review finding 3: after the folder picker
// closed, no window was active and keys went nowhere until a click). Only when no
// dialog of ours is left on screen, no window at all is active (the keyboard is
// taken from no one), and that window is still on screen. The window given back is
// the one active when a dialog took the keyboard, not one of ours; it is kept after
// a give-back between two windows of one flow; and a dialog that did not take the
// keyboard changes nothing. Positive twin first.
func TestTheKeyboardGoesBackToTheBrowserWhenOurDialogsClose(t *testing.T) {
	const dialog, second, browser, other = 0x10, 0x11, 0x20, 0x30
	on := map[uintptr]bool{browser: true, other: true}
	onScreen := func(h uintptr) bool { return on[h] }

	w := newWatch()
	w.showed(dialog, true, browser, false)
	if _, last := w.hidden(dialog); !last {
		t.Fatal("the only dialog closed, and it is not the last")
	}
	if got := w.giveBackTo(0, onScreen); got != browser {
		t.Errorf("after the last dialog closed with no window active, the keyboard goes back to %#x, want the browser %#x", got, browser)
	}

	// A chain: the second dialog showed while the first was ours and active, so the
	// browser stays the window to return to; nothing while one is still shown.
	w = newWatch()
	w.showed(dialog, true, browser, false)
	w.showed(second, true, dialog, true)
	w.hidden(dialog)
	if got := w.giveBackTo(0, onScreen); got != 0 {
		t.Errorf("the keyboard is given back while a dialog of ours is still shown (to %#x)", got)
	}
	w.hidden(second)
	if got := w.giveBackTo(0, onScreen); got != browser {
		t.Errorf("after a chain, the keyboard goes back to %#x, want the browser %#x", got, browser)
	}

	// A chain with a pause: the keyboard went back to the browser between two of the
	// flow's windows, and the next one showed from our own active window (seen in the
	// lab, 3 November 2026): the browser is still the window to return to at its end.
	w = newWatch()
	w.showed(dialog, true, browser, false)
	w.hidden(dialog)
	if got := w.giveBackTo(0, onScreen); got != browser {
		t.Fatalf("between two windows, the keyboard goes back to %#x, want the browser %#x", got, browser)
	}
	w.showed(second, true, second, true)
	w.hidden(second)
	if got := w.giveBackTo(0, onScreen); got != browser {
		t.Errorf("after a pause in a chain, the keyboard goes back to %#x, want the browser %#x", got, browser)
	}

	// Someone already has the keyboard: it is not taken from them.
	w = newWatch()
	w.showed(dialog, true, browser, false)
	w.hidden(dialog)
	if got := w.giveBackTo(other, onScreen); got != 0 {
		t.Errorf("the keyboard is taken from the active window %#x and given to %#x", other, got)
	}

	// The browser has closed meanwhile.
	w = newWatch()
	w.showed(dialog, true, browser, false)
	w.hidden(dialog)
	on[browser] = false
	if got := w.giveBackTo(0, onScreen); got != 0 {
		t.Errorf("the keyboard is given to a window no longer on screen (%#x)", got)
	}
	on[browser] = true

	// A dialog that did not take the keyboard records no window to return to.
	w = newWatch()
	w.showed(dialog, false, browser, false)
	w.hidden(dialog)
	if got := w.giveBackTo(0, onScreen); got != 0 {
		t.Errorf("a dialog that only flashed gives the keyboard to %#x", got)
	}
}

// covers: MA-1116, R39
// The keyboard goes back to where the person was, not to an older window
// (adversarial pass L3): when the person brings one of our dialogs to the front
// themselves — a click on a flashing taskbar button — the window of another program
// that was active just before is the one to return to. Positive twin first: a flow
// started from the browser returns to the browser. Then, later, the person works in
// another program and clicks a dialog that only flashed into the front: when it
// closes, the keyboard goes to that program, not to the first flow's browser. A
// dialog of ours made active from another of ours (a flow's next window) leaves the
// window to return to as it is.
func TestTheKeyboardGoesBackToWhereThePersonWas(t *testing.T) {
	const dialog, second, third, browser, other = 0x10, 0x11, 0x12, 0x20, 0x30
	on := map[uintptr]bool{browser: true, other: true}
	onScreen := func(h uintptr) bool { return on[h] }

	w := newWatch()
	w.foreground(browser, false, onScreen)
	w.showed(dialog, true, browser, false)
	w.foreground(dialog, true, onScreen)
	w.hidden(dialog)
	w.foreground(0, false, onScreen)
	if got := w.giveBackTo(0, onScreen); got != browser {
		t.Fatalf("a flow started from the browser gives the keyboard back to %#x, want the browser %#x", got, browser)
	}
	w.foreground(browser, false, onScreen) // given back

	// Later: the person works in another program; a dialog of ours opens, only flashes,
	// and the person clicks it into the front.
	w.foreground(other, false, onScreen)
	w.showed(second, false, other, false)
	w.foreground(second, true, onScreen)
	// Its answer brings the flow's next window, from our own active one.
	w.showed(third, true, second, true)
	w.hidden(second)
	w.foreground(third, true, onScreen)
	w.hidden(third)
	w.foreground(0, false, onScreen)
	if got := w.giveBackTo(0, onScreen); got != other {
		t.Errorf("after the person clicked a flashing dialog from %#x, the keyboard goes back to %#x, want %#x (where they were)", other, got, other)
	}
}

// covers: MA-1114, R39
// The ask is timed on the process's steady clock, which setting the computer's
// clock does not move (adversarial pass L4: an ask stored as wall-clock time could
// count again after the clock was set back). With the steady clock read through its
// seam: positive twin first, an ask 1 s old counts and reads as 1 s; an ask
// armFor + 1 s old on that clock does not, however recent the wall clock says it is.
func TestAnAskIsTimedOnTheSteadyClock(t *testing.T) {
	restore := steady
	defer func() { steady = restore; asked.Store(0) }()
	clock := 100 * time.Second
	steady = func() time.Duration { return clock }

	Arm()
	clock += time.Second
	if since, ok := takeAsk(); !ok || since != time.Second || !mayTakeKeyboard(false, ok, since) {
		t.Errorf("an ask 1 s old on the steady clock: asked %v, %v ago (want true, 1s, and it counts)", ok, since)
	}

	Arm()
	clock += armFor + time.Second
	if since, ok := takeAsk(); !ok || since != armFor+time.Second || mayTakeKeyboard(false, ok, since) {
		t.Errorf("an ask %v old on the steady clock: asked %v, %v ago (want true, %v, and it does not count)", armFor+time.Second, ok, since, armFor+time.Second)
	}
}

// fakeOps records the Windows calls bring makes.
type fakeOps struct {
	activeNow uintptr
	ours      map[uintptr]bool
	refuse    int // how many SetForegroundWindow calls Windows refuses first
	calls     []string
}

func (f *fakeOps) lift(uintptr)          { f.calls = append(f.calls, "lift") }
func (f *fakeOps) active() uintptr       { return f.activeNow }
func (f *fakeOps) isOurs(h uintptr) bool { return f.ours[h] }
func (f *fakeOps) nudge()                { f.calls = append(f.calls, "nudge") }
func (f *fakeOps) setForeground(h uintptr) bool {
	f.calls = append(f.calls, "set")
	if f.refuse > 0 {
		f.refuse--
		return false
	}
	f.activeNow = h
	return true
}

// covers: MA-1118, R39
// The zero-distance mouse move is sent only when Windows needs it (adversarial pass
// L2: each fake move restarts Windows' idle countdown, so a person who left our
// progress window in front and walked away would have the screen lock put off when
// the summary came forward). Positive twin first, the path the lab proved: with
// another program's window active (the browser), the dialog is lifted, the move is
// sent, then it is made active. With a window of ours active, this program already
// has the person's attention: lifted and made active, no move. Should Windows refuse
// even so (our window closed meanwhile), the move is sent and it is tried again.
func TestTheMouseMoveIsSentOnlyWhenWindowsNeedsIt(t *testing.T) {
	const dialog, ourProgress, browser = 0x10, 0x11, 0x20
	ours := map[uintptr]bool{dialog: true, ourProgress: true}
	for _, c := range []struct {
		name   string
		active uintptr
		refuse int
		want   string
	}{
		{"the browser active", browser, 0, "lift nudge set"},
		{"no window active", 0, 0, "lift nudge set"},
		{"our window active", ourProgress, 0, "lift set"},
		{"our window active, Windows refuses once", ourProgress, 1, "lift set nudge set"},
	} {
		f := &fakeOps{activeNow: c.active, ours: ours, refuse: c.refuse}
		bring(dialog, f)
		if got := strings.Join(f.calls, " "); got != c.want {
			t.Errorf("%s: bring made %q, want %q", c.name, got, c.want)
		}
		if f.activeNow != dialog {
			t.Errorf("%s: the dialog is not the active window after bring", c.name)
		}
	}
}
