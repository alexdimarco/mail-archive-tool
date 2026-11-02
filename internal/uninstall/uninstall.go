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
	"mail-archive-tool/internal/encmark"
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
// removes it. It says where to put the copy (the 1.5.3 friction review's B11).
const DeletionsLogNote = "Copy deletions.log first, somewhere you keep records (your Documents folder, for example), if you need it to check saved deletion records."

// LogNotCopiedError refuses `uninstall -yes`, nothing removed, while deletions.log holds any line
// and -log-copied was not given (the 1.5.3 friction review's D2, the main session's decision):
// the run that removes the log must not be the one that first says to copy it. Kept is the
// refusal made right before the settings files would go (UninstallYes's step 4), after the
// schedules, the sign-in and the working copies were removed: the log and the settings are kept.
type LogNotCopiedError struct {
	Log  string
	Kept bool
}

// The log's path is quoted, so it can be pasted into a copy command: on a Mac it holds a space
// ("Application Support"; the 1.5.3 re-walk's N10).
func (e *LogNotCopiedError) Error() string {
	log := encmark.CommandPath(util.StripControl(e.Log))
	if e.Kept {
		return fmt.Sprintf("MailArchive's deletion log, %s, now lists deletes made on this computer, and uninstall -yes would remove it, so it and the settings files were kept. Copy it somewhere you keep records (your Documents folder, for example), then run mailarchive uninstall -yes -log-copied.", log)
	}
	return fmt.Sprintf("MailArchive's deletion log, %s, lists the deletes made on this computer, and uninstall -yes removes it, so nothing was removed. Copy it somewhere you keep records (your Documents folder, for example), then run mailarchive uninstall -yes -log-copied.", log)
}

// DeletionsLogCopyNext is what the terminal's plan adds under DeletionsLogNote when the log holds
// any line: how to go on once it is copied, so -log-copied is not first learnt from a refusal (the
// 1.5.3 re-walk's N10; builder's words).
const DeletionsLogCopyNext = "Once it is copied, run mailarchive uninstall -yes -log-copied."

// CheckLogCopied is `uninstall -yes`'s check before it removes anything: with the deletion log
// at log ("" when there is none) holding any line, and copied (-log-copied) not given, it is
// LogNotCopiedError; a log that cannot be read stops it the same way as Uninstall's own check
// (LogUnreadableError, fail closed).
func CheckLogCopied(log string, copied bool) error {
	if log == "" || copied {
		return nil
	}
	holds, err := archdelete.LogHoldsLines(log)
	if err != nil {
		return &LogUnreadableError{Log: log, Err: err}
	}
	if holds {
		return &LogNotCopiedError{Log: log}
	}
	return nil
}

// DeleteNotFinishedError refuses an uninstall while deletions.log holds a delete that is under
// way or did not finish (docs/design-archive-delete.md §5.4; del-s5-A1-06, A2-02): uninstall
// removes the log, and a delete's §5.4 re-run reads only the log, so removing it would leave
// that delete with no way to finish — a folder half deleted, or an encrypted archive's record
// and key kept on this computer for a folder that is gone — and a delete running now would
// write the log again, without its started line, after uninstall said the settings were
// removed. Nothing is removed. Folders are the rows those deletes name. A dry run (the plan)
// returns it with its Report, and says Until in place of Error (the 1.5.3 friction review's
// B11: a plan, not "removed nothing").
type DeleteNotFinishedError struct {
	Log     string
	Folders []string
	// Config is the config file of a MailArchive Desktop started with -config, whose uninstall
	// this is ("" the usual one): the finishing command then carries -config FILE, so it reaches
	// that Desktop's list and log (the uninstall-docs group's adversarial pass, finding 8).
	Config string
	// Encrypted are the Folders whose delete is of an encrypted archive, or of one whose kind the
	// log does not say: only for them is it said that the key stays remembered (the 1.5.3
	// re-walk's N9, the operator's delegate's decision).
	Encrypted []string
}

// With several deletes the words are plural, each folder and its finishing command on a line of
// its own (the 1.5.3 re-walk's N9, the operator's delegate's decision: "the delete of A; B; C is
// finished … which that delete needs", every command in one bracket, read badly).
func (e *DeleteNotFinishedError) Error() string {
	if len(e.Folders) > 1 {
		return fmt.Sprintf("Deletes of these %d folders are running or did not finish, so uninstall removed nothing: uninstalling would remove MailArchive's deletion log (%s), which they need to finish.\n%s\n%s",
			len(e.Folders), e.logPath(), e.eachLine(), e.howToGoOnAll())
	}
	return fmt.Sprintf("A delete of %s is running or did not finish, so uninstall removed nothing: uninstalling would remove MailArchive's deletion log (%s), which that delete needs to finish. %s",
		e.names(), e.logPath(), e.howToGoOn())
}

// Until is what a dry run says after the plan: uninstall will refuse until the delete ends.
func (e *DeleteNotFinishedError) Until() string {
	if len(e.Folders) > 1 {
		return fmt.Sprintf("Uninstall will refuse until these %d deletes are finished (each is running or did not finish): uninstalling would remove MailArchive's deletion log (%s), which they need to finish.\n%s\n%s",
			len(e.Folders), e.logPath(), e.eachLine(), e.howToGoOnAll())
	}
	return fmt.Sprintf("Uninstall will refuse until the delete of %s is finished (it is running or did not finish): uninstalling would remove MailArchive's deletion log (%s), which that delete needs to finish. %s",
		e.names(), e.logPath(), e.howToGoOn())
}

// Waits is the Desktop's tooltip on its disabled Remove (MA-1030), plural for several deletes (N9).
func (e *DeleteNotFinishedError) Waits() string {
	if len(e.Folders) > 1 {
		return "Uninstall waits until those deletes are finished."
	}
	return "Uninstall waits until that delete is finished."
}

// names are the deletes' rows, control characters stripped.
func (e *DeleteNotFinishedError) names() string {
	names := make([]string, 0, len(e.Folders))
	for _, f := range e.Folders {
		names = append(names, util.StripControl(f))
	}
	return strings.Join(names, "; ")
}

// logPath is the deletion log's path quoted, as LogNotCopiedError quotes it, so it can be pasted
// into a copy command (the settle pass's LOW-4: on a Mac it holds a space, "Application Support").
func (e *DeleteNotFinishedError) logPath() string {
	return encmark.CommandPath(util.StripControl(e.Log))
}

// command is the terminal's command that finishes the delete of folder.
func (e *DeleteNotFinishedError) command(folder string) string {
	config := ""
	if c := strings.TrimSpace(e.Config); c != "" {
		config = " -config " + encmark.CommandPath(util.StripControl(c))
	}
	return "mailarchive delete-archive" + config + " -out " + encmark.CommandPath(util.StripControl(folder))
}

// eachLine is one line per delete: its folder, then the command that finishes it.
func (e *DeleteNotFinishedError) eachLine() string {
	lines := make([]string, 0, len(e.Folders))
	for _, f := range e.Folders {
		lines = append(lines, "  - "+util.StripControl(f)+": "+e.command(f))
	}
	return strings.Join(lines, "\n")
}

// keyKept is what giving up on a delete leaves of an encrypted archive's key, said only when one
// of the deletes is of an encrypted archive (or one whose kind is not known).
func (e *DeleteNotFinishedError) keyKept(several bool) string {
	switch {
	case len(e.Encrypted) == 0:
		return ""
	case several:
		return "; for an encrypted archive among them, this computer then still remembers its key until you use Forget this encrypted archive here, or Forget every encrypted archive on this computer"
	}
	return "; for an encrypted archive, this computer then still remembers its key until you use Forget this encrypted archive here, or Forget every encrypted archive on this computer"
}

// howToGoOn says how to finish the one delete, its real folder in the command, or how to give up
// on it.
func (e *DeleteNotFinishedError) howToGoOn() string {
	cmds := make([]string, 0, len(e.Folders))
	for _, f := range e.Folders {
		cmds = append(cmds, e.command(f))
	}
	return fmt.Sprintf("Wait for its result, or finish it (Delete… on its row in MailArchive Desktop, or %s), then uninstall again. If you no longer want to finish it, copy deletions.log somewhere you keep records, delete it, then uninstall again%s.",
		strings.Join(cmds, "; "), e.keyKept(false))
}

// howToGoOnAll is howToGoOn for several deletes, whose commands are on the lines above it.
func (e *DeleteNotFinishedError) howToGoOnAll() string {
	return "Wait for their results, or finish each one (Delete… on its row in MailArchive Desktop, or the command beside it above), then uninstall again. If you no longer want to finish them, copy deletions.log somewhere you keep records, delete it, then uninstall again" + e.keyKept(true) + "."
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

// pendingKinds is archdelete.PendingDeleteKinds, asked only to word the refusal: which pending
// deletes are of an encrypted archive (N9).
var pendingKinds = archdelete.PendingDeleteKinds

// checkNoPendingDelete refuses (DeleteNotFinishedError) while the log at path holds a delete
// under way or not finished, and fails closed when the log cannot be read (U-C4's rule: never
// remove what could not be checked).
func checkNoPendingDelete(path string) error {
	pending, err := pendingDeletes(path, runtime.GOOS)
	if err != nil {
		return &LogUnreadableError{Log: path, Err: err}
	}
	if len(pending) > 0 {
		return &DeleteNotFinishedError{Log: path, Folders: pending, Encrypted: encryptedAmong(path, pending)}
	}
	return nil
}

// encryptedAmong are the folders of pending whose delete the log says is not of a plain archive:
// an encrypted one, one whose kind it does not say, and every one when the kinds cannot be read —
// the key's sentence is left out only where it is known not to apply (N9).
func encryptedAmong(path string, pending []string) []string {
	plain := map[string]bool{}
	if kinds, err := pendingKinds(path, runtime.GOOS); err == nil {
		for _, k := range kinds {
			if !k.Encrypted {
				plain[k.Row] = true
			}
		}
	}
	var out []string
	for _, f := range pending {
		if !plain[f] {
			out = append(out, f)
		}
	}
	return out
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
// (fail-closed), returning the Report of what had been done. It removes a deletion log however
// many lines it holds: the Desktop's Remove removes only the plan the page showed, which names
// the log and says to copy it first (MA-1008). `uninstall -yes` is UninstallYes.
func Uninstall(configDir string, knownArchives []string, exe string, dryRun bool) (*Report, error) {
	return run(configDir, knownArchives, exe, dryRun, true)
}

// UninstallYes is `uninstall -yes`'s real run: Uninstall, refusing (LogNotCopiedError) while
// deletions.log holds any line and logCopied (-log-copied) is false. It asks before anything is
// removed and again right before the log would go, so a log made or filled by a delete since the
// plan was printed (another Desktop tab, another terminal) is never removed without -log-copied
// (MA-1031 amended; the uninstall-docs group's adversarial pass, finding 1). A log made after the
// settings folder was read is not among the files removed.
func UninstallYes(configDir string, knownArchives []string, exe string, logCopied bool) (*Report, error) {
	return run(configDir, knownArchives, exe, false, logCopied)
}

// run is Uninstall; logCopied false is UninstallYes's check of the deletion log.
func run(configDir string, knownArchives []string, exe string, dryRun, logCopied bool) (*Report, error) {
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
	// ConfigTargets lists only ordinary files, so a deletions.log that is a folder or a link (to a
	// log holding a delete that did not finish, or to nothing) would leave both checks below
	// unasked and every settings file removed: the name itself is asked, and anything there that
	// is not an ordinary file stops the uninstall, nothing removed (fail closed, U-C4's rule; the
	// 1.5.3 re-walk's F4).
	if deletionsLog == "" {
		p := filepath.Join(configDir, deletionsLogName)
		if fi, lerr := os.Lstat(p); lerr == nil {
			if !fi.Mode().IsRegular() {
				return nil, &LogUnreadableError{Log: p, Err: archdelete.ErrLogNotRegular}
			}
		} else if !errors.Is(lerr, fs.ErrNotExist) {
			return nil, &LogUnreadableError{Log: p, Err: lerr}
		}
	}
	// A delete under way or not finished keeps its log (§5.4): nothing is touched. A dry run
	// still works out the plan and returns it with the refusal, so the plan can be shown with
	// "Uninstall will refuse until…" (the 1.5.3 friction review's B11).
	var pending error
	if deletionsLog != "" {
		if err := checkNoPendingDelete(deletionsLog); err != nil {
			var dn *DeleteNotFinishedError
			if !dryRun || !errors.As(err, &dn) {
				return nil, err
			}
			pending = err
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
		return rep, pending
	}
	// -yes without -log-copied: a log holding any line stops it before anything is removed (D2).
	if err := CheckLogCopied(deletionsLog, logCopied); err != nil {
		return nil, err
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
	// before its log would go: the settings are then kept, as for any stop here. So is one that
	// began and finished since (-yes without -log-copied asks again: D2).
	if deletionsLog != "" {
		err := checkNoPendingDelete(deletionsLog)
		if err == nil {
			if err = CheckLogCopied(deletionsLog, logCopied); err != nil {
				var nc *LogNotCopiedError
				if errors.As(err, &nc) {
					err = &LogNotCopiedError{Log: nc.Log, Kept: true}
				}
			}
		}
		if err != nil {
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
