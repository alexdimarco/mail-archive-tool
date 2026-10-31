// Package uninstall computes a full uninstall of MailArchive — removing the program,
// its schedules, config and sign-in — while NEVER touching a mail archive (hard
// invariant R36, docs/design-archive-manager.md §5.8/§5.8.1). This file is the
// data-loss-critical core: deciding exactly which config-dir files may be deleted,
// with the guards an uninstall pre-code pass found a naive version would miss.
//
// Two R36 holes the guards here close (both confirmed in code): a recursive delete of
// the config dir would eat an archive placed inside it (the CLI create path has no
// overlap guard), and a `*.json` sweep would match .mailarchive-manifest.json /
// .mailarchive-paused.json because Go's '*' matches a leading dot. So this never
// recurses, never globs, refuses on any overlap, and never names an archive marker.
package uninstall

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"mail-archive-tool/internal/util"
)

// manifestName marks a folder as a MailArchive archive.
const manifestName = ".mailarchive-manifest.json"

// fixedConfigNames are the exact config-dir files uninstall owns, beside the job files
// (<name>.json) and the *.jobfail.log sinks.
var fixedConfigNames = map[string]bool{
	"desktop-settings.json": true,
	"graph-config.json":     true,
	"last-archive.txt":      true,
	"known-archives.json":   true,
	"desktop-capture.log":   true,
}

// alertStreakSuffix ends a scheduled backup's alert streak file, <key>.alert-streak, kept
// beside the Desktop settings (backupalert.StreakSuffix; a test keeps the two in step).
const alertStreakSuffix = ".alert-streak"

// syncAlertSuffix ends a scheduled backup's Thunderbird-not-synced told-file,
// <key>.sync-alert, beside the alert streak (backupalert.SyncAlertSuffix; MA-910's test
// keeps the two in step by behaviour).
const syncAlertSuffix = ".sync-alert"

// ErrOverlap refuses the WHOLE config cleanup because the config dir is, contains, or
// sits inside a mail archive; deleting its contents could destroy mail (R36, U-C1).
var ErrOverlap = errors.New("refusing to clean the config directory: it overlaps a mail archive")

// ConfigTargets is the exact set of absolute paths inside configDir that uninstall may
// delete, with the R36 guards (U-C1/U-C2). It REFUSES (ErrOverlap, no targets) when
// configDir holds a .mailarchive-manifest.json, or is within/contains any knownArchive
// -out. Otherwise it returns ONLY regular files whose name is a known config file, a GUI
// job file (<name>.json) or a .jobfail.log — never an archive marker (.mailarchive-*),
// a directory, or a symlink. It deletes nothing itself, and never recurses or globs.
func ConfigTargets(configDir string, knownArchives []string) ([]string, error) {
	configDir = strings.TrimSpace(configDir)
	if configDir == "" {
		return nil, errors.New("uninstall: no config directory")
	}
	// Guard 1: the config dir must not itself be / hold an archive.
	if isArchiveDir(configDir) {
		return nil, ErrOverlap
	}
	// Guard 2: the config dir must not overlap any known archive, either direction.
	rc := util.ResolveExisting(configDir)
	for _, a := range knownArchives {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		ra := util.ResolveExisting(a)
		if util.Within(rc, ra) || util.Within(ra, rc) {
			return nil, ErrOverlap
		}
	}
	entries, err := os.ReadDir(configDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil // nothing to clean
		}
		return nil, err
	}
	var targets []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".mailarchive") {
			continue // never an archive marker (defense-in-depth beyond the overlap guards)
		}
		info, ierr := e.Info()
		if ierr != nil || !info.Mode().IsRegular() {
			continue // never a directory, symlink, or other irregular entry
		}
		if deletableConfigName(name) {
			targets = append(targets, filepath.Join(configDir, name))
		}
	}
	return targets, nil
}

// deletableConfigName reports whether a config-dir basename is one uninstall owns.
func deletableConfigName(name string) bool {
	switch {
	case fixedConfigNames[name]:
		return true
	case strings.HasSuffix(name, ".json"): // GUI scheduled-job files, <name>.json
		return true
	case strings.HasSuffix(name, ".jobfail.log"):
		return true
	case strings.HasSuffix(name, alertStreakSuffix): // a scheduled backup's alert streak
		return true
	case strings.HasSuffix(name, syncAlertSuffix): // its Thunderbird-not-synced told-file
		return true
	}
	return false
}

// isArchiveDir reports whether dir directly holds a MailArchive manifest (so it IS or
// contains an archive). Lstat on the fixed marker name, no follow, no recursion.
func isArchiveDir(dir string) bool {
	fi, err := os.Lstat(filepath.Join(dir, manifestName))
	return err == nil && fi.Mode().IsRegular()
}
