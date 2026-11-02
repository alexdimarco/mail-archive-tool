package uninstall

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/progcache"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/util"
)

// Report is what a full uninstall will remove (a dry run) or did remove, plus — the
// point of the promise — the archives it keeps untouched (R36).
type Report struct {
	Schedules     []string // scheduler entry names removed
	ClearedSignIn bool     // a device sign-in / client secret was present and cleared
	// ConfigFiles are the settings files removed (absolute paths): the config dir's, then
	// the Thunderbird sync's state files and their folder (presyncjob.StateDir).
	ConfigFiles []string
	// Scratch is what of MailArchive's working folder (job.ScratchDir) is removed: an
	// encrypted archive's leftover working copies and their folders. Empty — and so not
	// shown — when there is none.
	Scratch []string
	// ProgramCache is the folder of downloaded MailArchive release copies (program
	// design §5, PG2), shown as one folder; programCache is what of it is removed.
	ProgramCache string
	programCache []string
	KeptArchives []string // archives left untouched, named for the owner
	BinaryNote   string   // how to remove the program itself (no self-delete, U-C6)
	// DeletionsLog is the delete's journal (deletions.log) when it is among ConfigFiles, so
	// the plan names it and says DeletionsLogNote; "" when there is none
	// (docs/design-archive-delete.md §8.1, O2-07).
	DeletionsLog string
}

// DeletionsLogNote is what uninstall's plan says under the deletion log (design-archive-delete
// §8.1): the log is the cross-check for a saved deletion record's digest, and uninstall
// removes it.
const DeletionsLogNote = "Copy deletions.log first if you need it to check saved deletion records."

// DeleteNotFinishedError refuses an uninstall while deletions.log holds a delete that is under
// way or did not finish (docs/design-archive-delete.md §5.4; del-s5-A1-06, A2-02): uninstall
// removes the log, and a delete's §5.4 re-run reads only the log, so removing it would leave
// that delete with no way to finish — a folder half deleted, or an encrypted archive's record
// and key kept on this computer for a folder that is gone — and a delete running now would
// write the log again, without its started line, after uninstall said the settings were
// removed. Nothing is removed. Folders are the rows those deletes name.
type DeleteNotFinishedError struct {
	Log     string
	Folders []string
}

func (e *DeleteNotFinishedError) Error() string {
	names := make([]string, 0, len(e.Folders))
	for _, f := range e.Folders {
		names = append(names, util.StripControl(f))
	}
	return fmt.Sprintf("A delete of %s is running or did not finish, so uninstall removed nothing: uninstalling would remove MailArchive's deletion log (%s), which that delete needs to finish. Wait for its result, or finish it (Delete… on its row in MailArchive Desktop, or mailarchive delete-archive -out FOLDER), then uninstall again. If you no longer want to finish it, copy deletions.log somewhere safe, delete it, then uninstall again; for an encrypted archive, this computer then still remembers its key until you use Forget this encrypted archive here, or Forget every encrypted archive on this computer.",
		strings.Join(names, "; "), e.Log)
}

// LogUnreadableError stops an uninstall, nothing removed, when deletions.log cannot be read to
// check for a delete that did not finish (fail closed, U-C4's rule). Its advice is for the
// cause (del-s5 re-verification, residual 5): an account that cannot read the log, a log other
// accounts can read, one larger than MailArchive writes, a name that is not an ordinary file,
// or anything else.
type LogUnreadableError struct {
	Log string
	Err error
}

func (e *LogUnreadableError) Error() string {
	return fmt.Sprintf("could not read MailArchive's deletion log %s to check for a delete that did not finish, so uninstall stopped before removing anything: %s: %v", e.Log, e.advice(), e.Err)
}

func (e *LogUnreadableError) Unwrap() error { return e.Err }

// advice is what to do about the cause, ending "then uninstall again".
func (e *LogUnreadableError) advice() string {
	switch {
	case errors.Is(e.Err, archdelete.ErrLogNotRegular):
		return "it is not an ordinary file (a link, a folder or a device), so MailArchive does not read it. Put the log file itself there, or remove what is there, then uninstall again"
	case errors.Is(e.Err, archdelete.ErrLogTooLarge):
		return "it is larger than MailArchive ever writes, so it is not MailArchive's own log. Copy it somewhere safe if you need it, delete it, then uninstall again"
	case errors.Is(e.Err, util.ErrReadableByOthers):
		return fmt.Sprintf("other accounts on this computer can read it, so MailArchive does not trust it. Run chmod 600 %s, then uninstall again", e.Log)
	case errors.Is(e.Err, fs.ErrPermission):
		return "make sure your account can read it, then uninstall again"
	}
	return "check that the drive it is on is connected and working, then uninstall again"
}

// pendingDeletes is archdelete.PendingDeletes (a seam only so a test can stand in for a delete
// that starts between the plan and the removal).
var pendingDeletes = archdelete.PendingDeletes

// checkNoPendingDelete refuses (DeleteNotFinishedError) while the log at path holds a delete
// under way or not finished, and fails closed when the log cannot be read (U-C4's rule: never
// remove what could not be checked).
func checkNoPendingDelete(path string) error {
	pending, err := pendingDeletes(path, runtime.GOOS)
	if err != nil {
		return &LogUnreadableError{Log: path, Err: err}
	}
	if len(pending) > 0 {
		return &DeleteNotFinishedError{Log: path, Folders: pending}
	}
	return nil
}

// Seams (overridable in tests, so a test touches no real scheduler or credential store):
// the live schedule sweep, the per-entry removal, and the sign-in clear.
var (
	listSchedules  = schedule.OurInstalled
	removeSchedule = schedule.RemoveLiveEntry
	// programCacheRoot is the downloaded release copies' cache (progcache.DefaultRoot).
	programCacheRoot = progcache.DefaultRoot
	clearSignIn      = func(tenant, clientID string) error {
		ts := graphconfig.DefaultTokenStore(tenant, clientID)
		tcp, _ := graphconfig.DefaultTokenCachePath(tenant, clientID)
		ss, _ := graphconfig.DefaultSecretStore()
		return ClearCredentials(tenant, clientID, ts, tcp, ss)
	}
)

// Uninstall removes the program's schedules, config and sign-in in the safe order
// (schedules first so nothing keeps running once the program is gone; then the sign-in;
// then the config files — the enumeration sources last), and NEVER touches an archive
// (R36: ConfigTargets refuses on any overlap and names only a fixed allow-list; the
// schedule removal uses RemoveLiveEntry, which keeps the archive-side descriptor). dryRun
// computes the Report without changing anything. On a real run a step's error stops it
// (fail-closed), returning the Report of what had been done.
func Uninstall(configDir string, knownArchives []string, exe string, dryRun bool) (*Report, error) {
	cfgTargets, err := ConfigTargets(configDir, knownArchives)
	if err != nil {
		return nil, err // ErrOverlap or a read error — nothing is touched
	}
	deletionsLog := ""
	for _, f := range cfgTargets {
		if filepath.Base(f) == deletionsLogName {
			deletionsLog = f
		}
	}
	// A delete under way or not finished keeps its log (§5.4): nothing is touched.
	if deletionsLog != "" {
		if err := checkNoPendingDelete(deletionsLog); err != nil {
			return nil, err
		}
	}
	var scratch []string
	if root, rerr := scratchRoot(); rerr == nil {
		if scratch, err = ScratchTargets(root, knownArchives); err != nil {
			return nil, err // ErrOverlap, ErrScratchUnexpected or a read error — nothing is touched
		}
	}
	// The Thunderbird sync's state folder (presyncjob.StateDir, per platform): its named
	// files, then the folder (design-presync-attended §5.5, C-40, C-63).
	var presyncState []string
	if dir, derr := presyncStateDir(); derr == nil && dir != "" {
		if presyncState, err = PresyncStateTargets(dir, knownArchives); err != nil {
			return nil, err // ErrOverlap or a read error — nothing is touched
		}
	}
	var cache []string
	if root, rerr := programCacheRoot(); rerr == nil {
		if cache, err = progcache.UninstallTargets(root); err != nil {
			return nil, err // a download under way, or a planted link — nothing is touched
		}
	}
	entries := listSchedules()
	tenant, clientID, gerr := graphKeys(configDir)
	if gerr != nil {
		return nil, fmt.Errorf("could not read the Microsoft sign-in settings at %s, so uninstall stopped before removing anything: clear the sign-in by hand (open MailArchive and sign out, or delete that file together with the saved token and app secret) and re-run — otherwise the saved sign-in would be left behind while its settings file is deleted (U-C4): %w", filepath.Join(configDir, "graph-config.json"), gerr)
	}

	rep := &Report{
		KeptArchives:  append([]string(nil), knownArchives...),
		BinaryNote:    binaryNote(exe),
		ClearedSignIn: tenant != "" || clientID != "",
		ConfigFiles:   append(append([]string(nil), cfgTargets...), presyncState...),
		Scratch:       scratch,
		programCache:  cache,
	}
	if len(cache) > 0 {
		rep.ProgramCache = cache[len(cache)-1]
	}
	rep.DeletionsLog = deletionsLog
	for _, e := range entries {
		rep.Schedules = append(rep.Schedules, e.Name)
	}
	if dryRun {
		return rep, nil
	}

	// 1) Schedules first (U-C5): nothing keeps running after the program is removed.
	for _, e := range entries {
		spec := schedule.Spec{Name: e.Name, Out: e.Out, WrapperPath: schedule.DefaultWrapperPath(e.Name)}
		if rerr := removeSchedule(spec); rerr != nil {
			return rep, fmt.Errorf("could not remove the schedule %q (stopping; nothing else removed): %w", e.Name, rerr)
		}
	}
	// 2) The sign-in (token + client secret). The keys were read ABOVE, so they outlive
	// the config deletion below (U-C4).
	if rep.ClearedSignIn {
		if cerr := clearSignIn(tenant, clientID); cerr != nil {
			return rep, cerr
		}
	}
	// 3) MailArchive's working folder: an encrypted archive's leftover working copies,
	// deepest first, each folder only when empty (os.Remove, never RemoveAll).
	for _, p := range scratch {
		if derr := os.Remove(p); derr != nil && !errors.Is(derr, fs.ErrNotExist) {
			return rep, fmt.Errorf("could not remove %s from MailArchive's working folder (stopping; the settings were not removed): %w", p, derr)
		}
	}
	// 3b) The downloaded MailArchive release copies: the cache's own files, deepest
	// first, each folder only when empty.
	for _, p := range cache {
		if derr := os.Remove(p); derr != nil && !errors.Is(derr, fs.ErrNotExist) {
			return rep, fmt.Errorf("could not remove %s from MailArchive's program cache (stopping; the settings were not removed): %w", p, derr)
		}
	}
	// 3c) The Thunderbird sync's state: its named files, then the folder, each with
	// os.Remove (the folder only when empty; PresyncStateTargets names it only then).
	for _, p := range presyncState {
		if derr := os.Remove(p); derr != nil && !errors.Is(derr, fs.ErrNotExist) {
			return rep, fmt.Errorf("could not remove %s from the Thunderbird sync's folder (stopping; the settings were not removed): %w", p, derr)
		}
	}
	// 4) The config files (the enumeration sources, deleted last). A delete that began since the
	// check above (another MailArchive window, the command line) is checked for again right
	// before its log would go: the settings are then kept, as for any stop here.
	if deletionsLog != "" {
		if err := checkNoPendingDelete(deletionsLog); err != nil {
			return rep, fmt.Errorf("stopping before the settings files were removed (the scheduled tasks, the sign-in and the working copies listed above were already removed): %w", err)
		}
	}
	for _, f := range cfgTargets {
		if derr := os.Remove(f); derr != nil && !errors.Is(derr, fs.ErrNotExist) {
			return rep, fmt.Errorf("could not remove %s: %w", f, derr)
		}
	}
	return rep, nil
}

// graphKeys reads the Graph registration's tenant+clientID from graph-config.json in
// configDir — BEFORE that file is deleted, so the derived credential keys survive the
// clear (U-C4). A legitimately ABSENT config yields empty keys with no error (nothing to
// clear). A config that is PRESENT but cannot be read or parsed is a non-not-found error:
// the caller MUST fail closed on it (U-C4), never silently skip the clear and then delete
// the only record of the keys — that would strand the client secret while reporting success.
func graphKeys(configDir string) (tenant, clientID string, err error) {
	c, lerr := graphconfig.Load(filepath.Join(configDir, "graph-config.json"))
	if lerr != nil {
		if errors.Is(lerr, os.ErrNotExist) {
			return "", "", nil // no config yet: nothing to clear
		}
		return "", "", lerr
	}
	if c == nil {
		return "", "", nil
	}
	return c.Tenant, c.ClientID, nil
}

// binaryNote tells the owner how to remove the program itself; uninstall never deletes a
// running executable (U-C6).
func binaryNote(exe string) string { return binaryNoteFor(runtime.GOOS, exe) }

// binaryNoteFor is binaryNote for goos. On macOS it names the bundle as it is built
// and installed, "Mail Archive.app" (packaging/macos/build-app.sh).
func binaryNoteFor(goos, exe string) string {
	switch goos {
	case "windows":
		return "To remove the program itself, open Settings → Apps → Installed apps (or Add/Remove Programs) and uninstall MailArchive."
	case "darwin":
		return "To remove the program itself, drag Mail Archive.app (in your Applications folder, or wherever you put it) to the Trash."
	default:
		note := "To remove the program itself, delete the mailarchive binary"
		if exe != "" {
			note += " (" + exe + ")"
		}
		return note + "."
	}
}
