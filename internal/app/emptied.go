package app

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/util"
)

// acquireArchiveLock takes a run's archive lock (lockfile.AcquireAs); a var so a test
// can empty or remove the archive folder between a run's checks and its lock, as a
// delete that finishes at that moment would (design-archive-delete §5.3 "After step 7"),
// or plant the delete marker there, as a delete stopped part way would (§15.3). The
// other writers into an archive (reindex, the rebuilds, the program-copies setting,
// RecordRefusedRun) take their lock through it too, so the same test reaches them.
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

// firstBackupRemedy is what the emptied-folder guard's refusals say to do, in words that fit
// every entry point it refuses: the scheduled run-job and command line, the Desktop's
// scheduled Microsoft 365 capture and the Desktop's Run now (del-s2-A2-04, A3-03).
const firstBackupRemedy = "If you deleted this archive, remove its scheduled backup (on every computer that backs up into this folder). " +
	"To keep an archive in this folder, make its first backup yourself — in MailArchive Desktop, Archive now (Microsoft 365) or Make a new archive… (mail on this computer); " +
	"or a backup at the command line without -unattended — or choose an empty folder for the archive."

// RefuseEmptiedFolder is the emptied-folder guard (design-archive-delete §7, review C12,
// NF-3; MA-956). An unattended run refuses an existing out that holds no archive
// (ArchiveHere: no manifest — at manifest when the run keeps it elsewhere, "" for the
// default — no encrypted archive's marker and no backup descriptor) and holds anything
// beyond operating-system litter (util.IsLitter): Delete keeps a folder that holds the
// person's own files, and a schedule it could not find (another computer's) must not build
// a new archive among them. A folder holding nothing but litter and MailArchive's own files
// passes: the archive lock file (so the guard can run again under the run's own lock), the
// run records an earlier refused run left (runRecordNames), and own — the files and folders
// of MailArchive's own that the run's caller keeps in out (its log, the scheduler's stderr
// file, `-outlook`'s _outlook-pst, the Desktop's settings folder, CC16). So a first run
// after an install (the descriptor) and "Save and keep" into a new empty folder still work.
// An out that is not there is RequireExistingOut's; nil here. A run that found an archive
// at an earlier check is held to more under its lock (outGuard.underLock).
//
// MailArchive's own names are matched to the folder's names by identity (ownEntries); the
// refusal names the folder, never the person's files.
func RefuseEmptiedFolder(out, manifest string, own ...string) error {
	fi, err := os.Stat(out)
	if err != nil || !fi.IsDir() {
		return nil
	}
	if ArchiveHere(out, manifest) {
		return nil
	}
	return refuseOthers(out, own)
}

// refuseOthers is RefuseEmptiedFolder's count, for a folder known to hold no archive.
func refuseOthers(out string, own []string) error {
	entries, err := os.ReadDir(out)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil // gone since the Stat: RequireExistingOut's
		}
		return withKind(state.FailArchiveUnwritable, fmt.Errorf("archive directory %s cannot be listed to check that it holds an archive, so a scheduled backup, or Run now, does not write into it: %w", out, err))
	}
	paths := []string{filepath.Join(out, lockfile.Name)}
	for _, n := range runRecordNames {
		paths = append(paths, filepath.Join(out, n))
	}
	ours := ownEntries(out, entries, append(paths, own...))
	others := 0
	for _, e := range entries {
		if name := e.Name(); !util.IsLitter(name) && !ours[name] {
			others++
		}
	}
	if others == 0 {
		return nil
	}
	return withKind(state.FailArchiveAbsent, fmt.Errorf("archive directory %s holds no MailArchive archive — no %s, no encrypted archive's key files and no %s — but holds %d other %s: its archive may have been deleted, or it is a folder of your own files. A scheduled backup, or Run now, does not make a new archive among them. %s",
		out, archiveManifestName, schedule.DescriptorName, others, plural(others, "file or folder", "files or folders"), firstBackupRemedy))
}

// ArchiveHere reports whether out holds an archive by the delete design's test (§4,
// [NEW-isarchive-cap]): its manifest (at manifest when the run keeps it elsewhere, ""
// for the default) is a regular, non-empty file opened inside its folder without a link
// (os.Root + util.OpenRegularFileIn, as §4.1's reader opens it), or encmark calls out an
// encrypted archive (MA-770: its markers, or this computer's record of exactly out); or out
// holds a backup descriptor, so a first run after an install
// counts. A manifest that opens but does not parse still counts here: the run's own load
// then fails it as manifest_unreadable, under its lock, before it writes (a difference
// from §4.1's reader, which parses it, reported for the operator).
func ArchiveHere(out, manifest string) bool {
	if manifest == "" {
		manifest = filepath.Join(out, archiveManifestName)
	}
	if regularIn(filepath.Dir(manifest), filepath.Base(manifest)) || regularIn(out, schedule.DescriptorName) {
		return true
	}
	enc, _, _ := encmark.Here(out)
	return enc
}

// regularIn reports whether name in dir is a regular, non-empty file: not a link, not a
// folder, opened through an os.Root on dir so nothing outside it is reached.
func regularIn(dir, name string) bool {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return false
	}
	defer root.Close()
	if fi, err := root.Lstat(name); err != nil || !fi.Mode().IsRegular() {
		return false
	}
	f, err := util.OpenRegularFileIn(root, name)
	if err != nil {
		return false
	}
	defer f.Close()
	fi, err := f.Stat()
	return err == nil && fi.Size() > 0
}

// ownEntries are the names, as out lists them (entries), of MailArchive's own files and
// folders (paths) that sit directly in out. The folder is compared by identity
// (os.SameFile), so another spelling of it — another case on Windows, a link — is the same
// folder, and one named in another folder never excuses a file here. So is the file: a name
// listed exactly as given is it, and a path given in another spelling of a listed file —
// another case where the file system ignores case (Windows, macOS), a Windows 8.3 short name
// such as MAILAR~1.LOG — is matched to the listed name of the same file. Where the file
// system tells cases apart (Linux), a name in another case is another file: the person's.
func ownEntries(out string, entries []fs.DirEntry, paths []string) map[string]bool {
	ours := map[string]bool{}
	outInfo, err := os.Stat(out)
	if err != nil {
		return ours
	}
	listed := map[string]bool{}
	for _, e := range entries {
		listed[e.Name()] = true
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
		if base := filepath.Base(p); listed[base] {
			ours[base] = true
			continue
		}
		pi, err := os.Lstat(p)
		if err != nil {
			continue // not there by this name either: nothing listed is it
		}
		for _, e := range entries {
			if ei, err := os.Lstat(filepath.Join(out, e.Name())); err == nil && os.SameFile(pi, ei) {
				ours[e.Name()] = true
				break
			}
		}
	}
	return ours
}

// PathIn reports whether path sits in dir or in a folder below it, the folders compared by
// identity (os.SameFile), so another spelling of dir — another case on Windows, a link — is
// dir. A caller keeps a run log of a refused archive folder out of it with it (MA-956).
func PathIn(path, dir string) bool {
	if path == "" || dir == "" {
		return false
	}
	di, err := os.Stat(dir)
	if err != nil {
		return false
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = filepath.Clean(path)
	}
	for p := filepath.Dir(abs); ; p = filepath.Dir(p) {
		if fi, err := os.Stat(p); err == nil && os.SameFile(fi, di) {
			return true
		}
		if filepath.Dir(p) == p {
			return false
		}
	}
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

// outGuard is an unattended run's folder checks (design-archive-delete §7): before its
// lock and again under it, the folder is there (RequireExistingOut, PM4) and is not a
// folder of the person's files an archive was deleted from (RefuseEmptiedFolder). It
// remembers whether any check — the caller's own at its entry (Options.ArchiveAtEntry)
// included — found an archive, so the check under the lock refuses a folder whose archive
// is gone since, whatever is left in it (findings del-s2-A1-01, A2-01: on Windows a delete
// cannot remove the files a run holds open before its lock, its log and PSTs, and those
// are MailArchive's own names). It is applied only under the lock: a check before it
// holds nothing against another writer part way through its own work in the folder.
type outGuard struct {
	opts       Options
	sawArchive bool
}

func newOutGuard(opts Options) *outGuard {
	return &outGuard{opts: opts, sawArchive: opts.ArchiveAtEntry}
}

// before is a check before the lock; requireOut adds RequireExistingOut.
func (g *outGuard) before(requireOut bool) error {
	if requireOut {
		if err := RequireExistingOut(g.opts.Out); err != nil {
			return err
		}
	}
	if fi, err := os.Stat(g.opts.Out); err != nil || !fi.IsDir() {
		return nil // RequireExistingOut's, where it applies
	}
	if ArchiveHere(g.opts.Out, g.opts.Manifest) {
		g.sawArchive = true
		return nil
	}
	return refuseOthers(g.opts.Out, g.opts.OwnFiles)
}

// underLock is the check under the run's own lock.
func (g *outGuard) underLock() error {
	if err := RequireExistingOut(g.opts.Out); err != nil {
		return err
	}
	if ArchiveHere(g.opts.Out, g.opts.Manifest) {
		return nil
	}
	if g.sawArchive {
		return withKind(state.FailArchiveAbsent, fmt.Errorf("archive directory %s held a MailArchive archive when this backup started, but holds none now: it may have been deleted while the backup was getting ready, so the backup does not make a new archive there. %s",
			g.opts.Out, firstBackupRemedy))
	}
	return refuseOthers(g.opts.Out, g.opts.OwnFiles)
}
