package app

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/util"
	"mail-archive-tool/internal/vault"
)

// acquireArchiveLock takes a run's archive lock (lockfile.AcquireAs); a var so a test
// can empty or remove the archive folder between a run's checks and its lock, as a
// delete that finishes at that moment would (design-archive-delete §5.3 "After step 7").
var acquireArchiveLock = lockfile.AcquireAs

// makeArchiveDir creates an attended run's archive folder (os.MkdirAll); a var so a test
// can see that an unattended run never calls it (design-archive-delete §7).
var makeArchiveDir = os.MkdirAll

// runRecordNames are the files a scheduled run that was refused before its folder held an
// archive leaves there (recordStartFailure, MA-646; RecordRefusedRun and its history,
// source streak and attention sidecar): MailArchive's own, never the person's. A delete
// removes them before the manifest (design-archive-delete §4.1, §5.3 step 6), so a folder
// a delete left part way still holds its manifest.
var runRecordNames = []string{state.LastRunName, state.AttentionName, state.RunHistoryName, state.SourceStreakName}

// RefuseEmptiedFolder is the emptied-folder guard (design-archive-delete §7, review C12,
// NF-3; MA-956). An unattended run refuses an existing out that holds no archive — no
// manifest (at manifest when the run keeps it elsewhere, "" for the default), no
// encrypted archive's marker and no backup descriptor — and holds anything beyond
// operating-system litter (util.IsLitter): Delete keeps a folder that holds the person's
// own files, and a schedule it could not find (another computer's) must not build a new
// archive among them. A folder holding nothing but litter and MailArchive's own files
// passes: the archive lock file (so the guard can run again under the run's own lock),
// the run records an earlier refused run left (runRecordNames), and own — the files and
// folders of MailArchive's own that the run's caller keeps in out (its log, the
// scheduler's stderr file, `-outlook`'s _outlook-pst, the Desktop's settings folder,
// CC16). So a first run after an install (the descriptor) and "Save and keep" into a new
// empty folder still work. An out that is not there is RequireExistingOut's; nil here.
//
// Names are compared without regard to case, as Windows and macOS file systems compare
// them; the refusal names the folder, never the person's files.
func RefuseEmptiedFolder(out, manifest string, own ...string) error {
	fi, err := os.Stat(out)
	if err != nil || !fi.IsDir() {
		return nil
	}
	if manifest == "" {
		manifest = filepath.Join(out, archiveManifestName)
	}
	if present(manifest) || present(filepath.Join(out, schedule.DescriptorName)) {
		return nil
	}
	if enc, _, _ := vault.EncryptedMarker(out); enc {
		return nil
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil // gone since the Stat: RequireExistingOut's
		}
		return withKind(state.FailArchiveUnwritable, fmt.Errorf("archive directory %s cannot be listed to check that it holds an archive: %w (unattended run)", out, err))
	}
	ours := ownNames(out, own)
	ours[strings.ToLower(lockfile.Name)] = true
	for _, n := range runRecordNames {
		ours[strings.ToLower(n)] = true
	}
	others := 0
	for _, e := range entries {
		if name := e.Name(); !util.IsLitter(name) && !ours[strings.ToLower(name)] {
			others++
		}
	}
	if others == 0 {
		return nil
	}
	return withKind(state.FailArchiveAbsent, fmt.Errorf("archive directory %s holds no MailArchive archive — no %s, no encrypted archive's key files and no %s — but holds %d other %s, so its archive may have been deleted: refusing to make a new archive among them (unattended run). If you deleted this archive, remove its scheduled backup (on every computer that backs up into this folder); to start a new archive in this folder, run its first backup yourself (the MailArchive wizard, or a backup at the command line without -unattended)",
		out, archiveManifestName, schedule.DescriptorName, others, plural(others, "file or folder", "files or folders")))
}

// present reports a name that is there and not a folder (a link counts, as the
// encrypted marker's Lstat does).
func present(path string) bool {
	fi, err := os.Lstat(path)
	return err == nil && !fi.IsDir()
}

// ownNames are the lower-cased names of the run's own files and folders that sit directly
// in out: one named in another folder never excuses a same-named file here. The folder is
// compared by identity (os.SameFile), so another spelling of it — another case on
// Windows, a link — is the same folder.
func ownNames(out string, paths []string) map[string]bool {
	names := map[string]bool{}
	outInfo, err := os.Stat(out)
	if err != nil {
		return names
	}
	for _, p := range paths {
		if p == "" {
			continue
		}
		p = filepath.Clean(p)
		dir, err := os.Stat(filepath.Dir(p))
		if err != nil || !os.SameFile(dir, outInfo) {
			continue
		}
		names[strings.ToLower(filepath.Base(p))] = true
	}
	return names
}

// lockRefusal words a run's failed archive lock: a held lock is named by ErrHeld first
// (busyRefusal). For an unattended run whose folder is gone since its checks — it creates
// no folder, so the lock's open fails — the refusal is RequireExistingOut's, the archive not
// present (archive_absent, MA-689), not "archive unwritable" (design-archive-delete §7,
// R3:NF-R3-GONE-NOT-MA689).
func lockRefusal(opts Options, lockPath string, err error) error {
	if opts.Unattended && !errors.Is(err, lockfile.ErrHeld) {
		if absent := RequireExistingOut(opts.Out); absent != nil {
			return absent
		}
	}
	return withKind(state.FailArchiveUnwritable, busyRefusal(lockPath, err))
}

// refuseUnattendedOut is an unattended run's two folder checks, before its lock and again
// under it (design-archive-delete §7): the folder is there (RequireExistingOut, PM4) and
// is not a folder of the person's files an archive was deleted from (RefuseEmptiedFolder).
func refuseUnattendedOut(opts Options) error {
	if err := RequireExistingOut(opts.Out); err != nil {
		return err
	}
	return RefuseEmptiedFolder(opts.Out, opts.Manifest, opts.OwnFiles...)
}
