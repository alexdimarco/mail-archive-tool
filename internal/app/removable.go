package app

import "strings"

// A plaintext copy of an encrypted archive made on a drive that can leave this
// computer asks twice (phase 3 §10, K5). "Removable" is any drive that is not one
// of this computer's own: a USB stick or card, and an external disk, which
// leaves as easily. A share is told apart by the view (viewShare), not here.

// removableMountDarwin says a macOS mount is removable or external: the kernel
// marks it removable, or it is mounted under /Volumes — where macOS mounts every
// volume but the boot volume and its system volumes.
func removableMountDarwin(mountedOn string, removableFlag bool) bool {
	return removableFlag || strings.HasPrefix(mountedOn, "/Volumes/")
}

// removableMountinfo says the Linux mount holding abs (the longest mount point in
// /proc/self/mountinfo it is under) is where desktops mount removable and external
// drives: /media, /run/media or /mnt.
func removableMountinfo(lines []string, abs string) bool {
	best, bestLen := "", -1
	for _, ln := range lines {
		pre, _, ok := strings.Cut(ln, " - ")
		if !ok {
			continue
		}
		f := strings.Fields(pre)
		if len(f) < 5 {
			continue
		}
		mp := unescapeMountinfo(f[4])
		if !pathUnder(abs, mp) || len(mp) <= bestLen {
			continue
		}
		best, bestLen = mp, len(mp)
	}
	for _, root := range []string{"/media/", "/run/media/", "/mnt/"} {
		if strings.HasPrefix(best+"/", root) && best+"/" != root {
			return true
		}
	}
	return false
}

// removable says dir is on a removable or external drive, through the test seam
// when one is set.
func (e *CopyExportEnv) removable(dir string) bool {
	if e.removableSeam != nil {
		return e.removableSeam(dir)
	}
	return removableDrive(dir)
}
