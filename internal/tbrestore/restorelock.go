package tbrestore

import (
	"errors"
	"os"
	"path/filepath"

	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/thunderbird"
)

// LogName is the restore record's name in MailArchive's default settings folder.
const LogName = "restores.log"

// SettingsDir is MailArchive's default settings folder, job.Dir(), whatever -config
// says (design §7.4 foldcheck note M4): restores.log and the restore locks always live
// here, so a scheduled backup, presync and a Desktop started with -config all find
// them.
func SettingsDir() (string, error) { return job.Dir() }

// LogPath is restores.log in SettingsDir. Every caller — scheduled backups, Desktop's
// own runs, presync, status and the restore — reaches the log through it.
func LogPath() (string, error) {
	d, err := SettingsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, LogName), nil
}

// ProfileKeyOf is thunderbird.ProfileKey: the key restores.log, the marker and the
// restore lock all name a profile by.
func ProfileKeyOf(profileDir string) string { return thunderbird.ProfileKey(profileDir) }

// RestoreLock is the MailArchive restore lock of one profile (design §5.2 note item 3).
type RestoreLock struct{ l *lockfile.Lock }

// AcquireRestoreLock takes the profile's MailArchive restore lock,
// restore-<ProfileKey>.lock in SettingsDir, with lockfile.Acquire: an OS lock released
// when the process ends, so never stale. Another holder (a restore in Desktop or at a
// terminal) is W-RESTORING. While it is held presync's running check reports the
// profile in use (presync.detectRunning), so a scheduled pre-sync never starts
// Thunderbird on it.
func AcquireRestoreLock(profile string) (*RestoreLock, error) {
	p, err := thunderbird.RestoreLockPath(profile)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return nil, err
	}
	l, err := lockfile.Acquire(p)
	if err != nil {
		if errors.Is(err, lockfile.ErrHeld) {
			return nil, refuse("W-RESTORING", wRestoring, err)
		}
		return nil, err
	}
	return &RestoreLock{l: l}, nil
}

// Release releases the restore lock; safe to call more than once.
func (r *RestoreLock) Release() {
	if r != nil && r.l != nil {
		_ = r.l.Release()
		r.l = nil
	}
}

// AnyRestoreRunning reports whether any restore into Thunderbird holds its restore lock
// now: a restore-<key>.lock in SettingsDir that is held (a probe each, nothing created or
// written). Uninstall refuses while one is (R6-03, settled 2026-11-07): it would remove
// restores.log while that restore appends to it.
func AnyRestoreRunning() bool {
	d, err := SettingsDir()
	if err != nil {
		return false
	}
	entries, err := os.ReadDir(d)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if thunderbird.IsRestoreLockName(e.Name()) && lockfile.Held(filepath.Join(d, e.Name())) {
			return true
		}
	}
	return false
}

// RestoreLockHeld reports whether a restore into the profile holds its restore lock
// now (a probe: the file is never created or written). The plan asks it first, so a
// terminal and Desktop restoring at once are told W-RESTORING rather than to close a
// Thunderbird that is not open (§5.1 note).
func RestoreLockHeld(profile string) bool {
	p, err := thunderbird.RestoreLockPath(profile)
	if err != nil {
		return false
	}
	return lockfile.Held(p)
}
