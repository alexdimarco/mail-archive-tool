// Package dialogfront brings the program's own dialogs in front of the browser on
// Windows, when the person asked for them, and never lets one take the keyboard
// when they did not (R39).
//
// MailArchive Desktop is a local web page. On Windows it runs as mailarchivew.exe,
// a program with no window of its own, and the person works in the browser. When
// they click a button that opens a dialog (Make a new archive…, Open an archive
// folder…), the dialog comes from our program while the browser has their
// attention, and Windows lets only the program with the person's attention bring
// a window to the front. So the dialog opened behind the browser and was not seen
// (owner report, 2 November 2026). The dialog library (github.com/ncruces/zenity)
// tries to bring a window of ours to the front first, but it finds none.
//
// Install watches, once per process, for each window of this process that is
// shown or hidden, and for each change of the active window. A dialog — a whole
// top-level window with a title bar — may take the keyboard only when the person
// asked for it: the active window is already one of ours, or Arm was called less
// than armFor ago and has not been used. Arm is called where the person asks: the
// Desktop entrances a button press reaches (the folder pickers, the wizard's add,
// add-encrypted and resume flows) and the wizard's own start; and the watch calls
// it itself when one of our dialogs closes while it is the active window (the
// person answered it, so the next window of the same flow is theirs too). Such a
// dialog is lifted above the other windows once (always-on-top, then at once not,
// so it never stays on top), sent one mouse move of zero distance so Windows counts
// this program as the source of the last input, and made the active window; the
// file and folder pickers reorder themselves just after they show, so this is
// tried again at 200, 600 and 1200 ms, while the dialog has never yet been the
// active window and the person has not moved to another one (MA-1110, MA-1112).
// Any other dialog — one that opens after a long export while the person types in
// the browser — is left where Windows puts it and only flashes its taskbar button
// (MA-1114). When the last of our dialogs closes and no window is active, the
// window that was active before the dialogs (the browser) is made active again
// (MA-1116). No dialog call changes. Off Windows Install and Arm do nothing: there
// each dialog is a window of a separate helper program (osascript on macOS, zenity
// on Linux), and the report is from Windows.
//
// The entries that open dialogs — desktop.Launch and wizard.Run — call Install as
// their first statement, and a walk of the code checks that every dialog is reached
// only through them (MA-1111) and, inside Desktop, only after an Arm (MA-1117).
package dialogfront

import (
	"sync"
	"sync/atomic"
	"time"
)

var once sync.Once

// Install starts the watch for this process, once; later calls do nothing. It
// returns when the watch is in place, so a dialog shown just after is covered.
func Install() { once.Do(install) }

// armFor is how long after the person asks (Arm) the next dialog may take the
// keyboard: long enough for the wizard to look for mail stores before its first
// window, short enough that a person who has gone back to typing elsewhere is not
// answered for.
const armFor = 8 * time.Second

// asked is when Arm was last called (UnixNano), or 0 once a dialog has used it.
var asked atomic.Int64

// Arm records that the person just asked for a window (a button press that opens
// a dialog, the wizard's start, or their answer to our previous dialog): the next
// dialog this process shows within armFor comes in front and takes the keyboard.
func Arm() { asked.Store(time.Now().UnixNano()) }

// takeAsk reads and clears the ask: one ask brings one dialog forward. ok is false
// when there is none; since is how long ago it was made.
func takeAsk(now time.Time) (since time.Duration, ok bool) {
	v := asked.Swap(0)
	if v == 0 {
		return 0, false
	}
	return now.Sub(time.Unix(0, v)), true
}

// mayTakeKeyboard reports whether a dialog shown now may become the active window:
// the active window is already one of ours (a dialog opened from our own), or the
// person asked less than armFor ago.
func mayTakeKeyboard(activeIsOurs, wasAsked bool, sinceAsked time.Duration) bool {
	return activeIsOurs || (wasAsked && sinceAsked >= 0 && sinceAsked <= armFor)
}

// The Windows values the window test reads; plain numbers so the test of the
// logic runs on every system.
const (
	objidWindow = 0          // OBJID_WINDOW: the notice is about the window itself
	childidSelf = 0          // CHILDID_SELF: not about one of its parts
	wsChild     = 0x40000000 // WS_CHILD
	wsCaption   = 0x00C00000 // WS_CAPTION (a border and a title bar)
)

// isDialogWindow reports whether a "window shown" notice is about a dialog: the
// whole window (not a part of it), a top-level window (its own root), with a
// title bar and not a child window. A notification balloon, a tooltip and a
// control inside a dialog are left alone.
func isDialogWindow(idObject, idChild int32, isRoot bool, style uint32) bool {
	return idObject == objidWindow && idChild == childidSelf && isRoot &&
		style&wsChild == 0 && style&wsCaption == wsCaption
}

// retryAfter are the later tries, counted from when the window showed.
var retryAfter = []time.Duration{200 * time.Millisecond, 600 * time.Millisecond, 1200 * time.Millisecond}

// tryAgain reports whether a later try should still bring the window forward: it
// is still shown, it has never been the active window, and the active window is
// still the one that was active when it showed (or none). Once it has been active,
// or the person has moved to another window, it is left where it is: a person who
// clicks back into the browser is not pulled into the dialog again.
func tryAgain(visible, beenActive bool, active, window, activeWhenShown uintptr) bool {
	return visible && !beenActive && active != window && (active == activeWhenShown || active == 0)
}

// giveBackAfter is how long after the last of our dialogs closes the watch waits
// before giving the keyboard back, so the next window of a flow that follows at
// once is not interrupted.
const giveBackAfter = 600 * time.Millisecond

// watch is what the watch remembers between notices: our dialogs now shown, the one
// that is the active window, those that have been, and the window to give the
// keyboard back to. Its methods are the watch's choices, kept free of Windows calls
// so they are tested on every system.
type watch struct {
	mu         sync.Mutex
	shown      map[uintptr]bool // our dialogs now shown
	beenActive map[uintptr]bool // those of them that have been the active window
	active     uintptr          // our dialog that is the active window, or was when it closed
	returnTo   uintptr          // the window active before our dialogs took the keyboard
}

func newWatch() *watch {
	return &watch{shown: map[uintptr]bool{}, beenActive: map[uintptr]bool{}}
}

// showed records a dialog of ours being shown. When it is to take the keyboard,
// activeBefore (the active window then, if not ours) is the window to give it back
// to afterwards.
func (w *watch) showed(h uintptr, takesKeyboard bool, activeBefore uintptr, activeBeforeIsOurs bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.shown[h] = true
	if takesKeyboard && activeBefore != 0 && !activeBeforeIsOurs {
		w.returnTo = activeBefore
	}
}

// foreground records that h is the active window (read from Windows when a change
// is noticed, not taken from the notice: Windows also sends one when a window only
// becomes its own thread's active window, and the notice of a dialog's activation
// can arrive before the notice of its showing, so the caller also records it right
// after a dialog shows). A window of another program becoming active while our
// active dialog is still shown means the person moved away from it; while it is
// closing (no longer shown on screen), it is only Windows passing the keyboard on.
func (w *watch) foreground(h uintptr, isOurs bool, stillOnScreen func(uintptr) bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	switch {
	case w.shown[h]:
		w.active = h
		w.beenActive[h] = true
	case isOurs:
	case w.active != 0 && stillOnScreen(w.active):
		w.active = 0
	}
}

// hidden records a dialog of ours closing. answered is true when it was the active
// window as it closed: the person answered it, so the next dialog of the flow may
// take the keyboard (the caller calls Arm). last is true when no dialog of ours is
// left shown (the caller may then give the keyboard back).
func (w *watch) hidden(h uintptr) (answered, last bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.shown[h] {
		return false, false
	}
	delete(w.shown, h)
	delete(w.beenActive, h)
	if w.active == h {
		answered = true
		w.active = 0
	}
	return answered, len(w.shown) == 0
}

// wasActive reports whether dialog h has been the active window since it showed.
func (w *watch) wasActive(h uintptr) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.beenActive[h]
}

// giveBackTo is the window to make active again once our dialogs are closed, or 0:
// only when none of ours is shown, no window at all is active (so the keyboard is
// taken from no one), and the window to return to is still on screen. It is kept
// after use: a flow's next dialog may show just after the keyboard was given back
// between two of its windows, from our own active window, and the browser is still
// the window to return to when the flow ends.
func (w *watch) giveBackTo(activeNow uintptr, onScreen func(uintptr) bool) uintptr {
	w.mu.Lock()
	defer w.mu.Unlock()
	r := w.returnTo
	if len(w.shown) != 0 || activeNow != 0 || r == 0 || !onScreen(r) {
		return 0
	}
	return r
}
