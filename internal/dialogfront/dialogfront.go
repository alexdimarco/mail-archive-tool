// Package dialogfront brings the program's own dialogs in front of the browser on
// Windows.
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
// shown. For a whole top-level window with a title bar — every dialog — it lifts
// the window above the others once (always-on-top, then at once not, so it never
// stays on top), sends one mouse move of zero distance so Windows counts this
// program as the source of the last input, and asks Windows to make the window
// the active one. The file and folder pickers reorder themselves just after they
// show, so it tries again at 200, 600 and 1200 ms, unless the window is gone, is
// already in front, or the person has moved to another window. No dialog call
// changes: the watch covers every dialog the process shows, in Desktop's own code
// and in the wizard it runs in-process (MA-1110). Off Windows Install does
// nothing: there each dialog is a window of a separate helper program (osascript
// on macOS, zenity on Linux), and the report is from Windows.
//
// The entries that open dialogs — desktop.Launch and wizard.Run — call Install as
// their first statement; a walk of the code checks that every dialog is reached
// only through them (MA-1111).
package dialogfront

import (
	"sync"
	"time"
)

var once sync.Once

// Install starts the watch for this process, once; later calls do nothing. It
// returns when the watch is in place, so a dialog shown just after is covered.
func Install() { once.Do(install) }

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
// is still shown, it is not yet the active window, and the active window is still
// the one that was active when it showed (or none). Once the person has moved to
// another window, the dialog is left where it is.
func tryAgain(visible bool, active, window, activeWhenShown uintptr) bool {
	return visible && active != window && (active == activeWhenShown || active == 0)
}
