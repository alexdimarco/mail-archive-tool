//go:build !darwin && !windows

package desktop

// Linux and the BSDs need no event loop for slice S1: the hand-off reaches the instance,
// and a sign-out ends it with SIGTERM, which Launch's signal context turns into the one
// stop (design-menubar MB-2). Their tray (slice S5) brings a D-Bus connection instead.

// allowForeground: no such rule here (Wayland's activation token travels in the
// hand-off instead, §7.4).
var allowForeground func(pid int)

// bundleVersionOnDisk: no bundle to read.
func bundleVersionOnDisk() string { return "" }
