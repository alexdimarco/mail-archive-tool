package presync

// The Windows backstop minimize's show mode (design-presync-attended §2.7, A14,
// C-58), as an untagged pure function so the Linux test gate checks it. Windows
// starts Thunderbird visibly (it ignores SW_HIDE, lab L-W4), then minimizes the
// window it showed. A scheduled run keeps SW_MINIMIZE (§1.3); an attended run —
// the person may be typing in another window — picks the mode that moves their
// keyboard focus least.

// ShowWindow's show values (Win32).
const (
	// swMinimize minimizes the window and activates the next top-level window.
	swMinimize = 6
	// swShowMinNoActive minimizes the window and activates nothing.
	swShowMinNoActive = 7
)

// minimizeMode is the show value an attended run minimizes Thunderbird's window
// with, or 0 for no call (A14 (3)): already minimized → no call; Thunderbird is
// not the foreground window → SW_SHOWMINNOACTIVE, so the person's typing stays
// where it was; Thunderbird is the foreground window (it took the keyboard) →
// SW_MINIMIZE, which hands activation back to the next window, most likely the one
// the person was typing in (inferred; lab MA-918 samples the foreground window).
func minimizeMode(iconic, foreground bool) int {
	switch {
	case iconic:
		return 0
	case foreground:
		return swMinimize
	default:
		return swShowMinNoActive
	}
}
