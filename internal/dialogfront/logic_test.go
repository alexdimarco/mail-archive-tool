package dialogfront

import (
	"testing"
	"time"
)

// covers: MA-1112, R33
// The watch's choices, as plain logic: only a whole top-level window with a title
// bar is brought forward (zenity's own dialog style and a message box's are; a
// control inside a dialog, a part of a window, a tooltip-like popup with no title
// bar and a window that is not its own root are not), and a later try happens only
// while the window is shown, not yet active, and the person has not moved to
// another window. The tries come at 200, 600 and 1200 ms, in order.
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
		visible                 bool
		active, activeWhenShown uintptr
		want                    bool
	}{
		{"still behind the browser", true, browser, browser, true},
		{"no window active", true, 0, browser, true},
		{"already in front", true, dialog, browser, false},
		{"closed", false, browser, browser, false},
		{"the person moved to another window", true, other, browser, false},
	} {
		if got := tryAgain(c.visible, c.active, dialog, c.activeWhenShown); got != c.want {
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
