package presync

import (
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/thunderbird"
)

// realRestoreLockHeld is the real restoreLockHeld seam: the MailArchive restore lock
// of the profile (thunderbird.RestoreLockPath, always in MailArchive's default
// settings folder) is held by a running restore. It probes the lock as lockfile.Held
// does, never creating or writing it. When the settings folder cannot be found no
// restore can hold a lock there either (a restore takes it in that folder before it
// writes), so that is not held.
func realRestoreLockHeld(profile string) bool {
	p, err := thunderbird.RestoreLockPath(profile)
	if err != nil {
		return false
	}
	return lockfile.Held(p)
}
