package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/desktop"
	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/instance"
	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/util"
)

// delete-archive and unlist-archive (docs/design-archive-delete.md §3.3, §8.1, slice 4): Delete…
// and Remove from list of MailArchive Desktop's Your archives card, at a terminal. What a delete
// removes, in which order and with which words is archdelete's; the seams are the Desktop's own
// (desktop.CommandLineDelete). These verbs add only the terminal, the check that no Desktop runs,
// the row rule, the plan and the questions, the saved files, the signals and the exit codes.

// The seams the two verbs reach this computer through; tests give their own.
var (
	// desktopLockDir is where a running MailArchive Desktop holds its instance locks.
	desktopLockDir = instance.Dir
	// deleteScheduler and deleteInstalled are this computer's scheduler and its installed
	// entries (nil: the real ones).
	deleteScheduler archdelete.Scheduler
	deleteInstalled func() []schedule.InstalledEntry
	// beforeDeleteStep runs before the re-check before steps 2, 4, 8 and 9 (a test starts a
	// Desktop there); nothing in a real run.
	beforeDeleteStep = func(step int) {}
	// openSavedFile creates the -plan-out or -record-out file, new (never over an existing name
	// or a link) and not owner-only: it is meant to be shared (§8.1).
	openSavedFile = func(path string) (*os.File, error) {
		return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	}
	// openProbeFile creates checkSavedFile's probe file, new and owner-only.
	openProbeFile = func(path string) (*os.File, error) {
		return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	}
	// removeOwnedFile removes a file the verb made when a signal stops it (a test refuses it while
	// the file is open, as Windows does).
	removeOwnedFile = os.Remove
	// createWait is how long a signal waits for a file being created to be made, so it can be
	// removed: a folder on a network share that stopped answering is not waited for longer.
	createWait = 2 * time.Second
	// afterDeleteReturned runs between Execute's return and the result being printed (a test
	// sends a signal there); nothing in a real run.
	afterDeleteReturned = func() {}
	// notifyDeleteSignals subscribes c to the signals the verbs watch (signal.Notify).
	notifyDeleteSignals = func(c chan<- os.Signal, sigs ...os.Signal) { signal.Notify(c, sigs...) }
	// deleteExit ends the process with a signal's code.
	deleteExit = os.Exit
	// startVerbWatch starts a verb's signal watch, over Ctrl-C and the terminate signal, which
	// lasts the whole verb (del-s4-A2-07, conformance 4).
	startVerbWatch = func(stderr, stdout io.Writer, verb string) *verbWatch {
		sigs := make(chan os.Signal, 2)
		notifyDeleteSignals(sigs, os.Interrupt, syscall.SIGTERM)
		v := newVerbWatch(sigs, stderr, stdout, verb, deleteExit)
		v.unsubscribe = func() { signal.Stop(sigs) }
		return v
	}
)

// deleteVerbExit and unlistVerbExit are the verbs' exit lines in their help (X1).
const (
	deleteVerbExit = "Exit: 0 deleted (or the plan saved); 3 not fully deleted (it says what is left and what to do); 1 refused, nothing deleted (including a name that does not match); 130 or 143 stopped by Ctrl-C or a terminate signal (stopped at a question, while planning or before the delete changed anything, nothing changed; once it changed something, it did not finish: run it again to finish it)."
	unlistVerbExit = "Exit: 0 removed from the list; 3 stopped part way (a schedule was stopped and the row kept: it says which); 1 refused or declined, nothing changed; 130 or 143 stopped by Ctrl-C or a terminate signal (nothing changed; a signal while the row is being removed waits for that to finish)."
)

// desktopRunningText refuses either verb while a Desktop runs (§3.3 [R3:C17]).
// It names where Shut down is, with a final period (the 1.5.3 friction review's B12, item 32).
const desktopRunningText = "MailArchive Desktop is running for your account (in this or another sign-in to this computer), so nothing was changed. Use the Desktop, or shut it down first: click Shut down at the top of MailArchive Desktop, then run this again."

// errDesktopIsRunning is desktopRunning's refusal; errDesktopStarted is the re-check's stop
// before steps 4, 8 and 9.
var (
	errDesktopIsRunning = errors.New(desktopRunningText)
	errDesktopStarted   = errors.New("MailArchive Desktop started while the delete was running")
)

// cannotTellError is desktopRunning's refusal when it cannot tell: nothing was changed at the
// start; midway is its words once the delete has changed something (del-s4-A2-03).
type cannotTellError struct{ cause string }

func (c *cannotTellError) Error() string {
	return fmt.Sprintf("MailArchive cannot tell whether MailArchive Desktop is running for your account (%s), so nothing was changed", c.cause)
}

func (c *cannotTellError) midway() error {
	return fmt.Errorf("MailArchive cannot tell whether MailArchive Desktop is running for your account (%s)", c.cause)
}

// desktopRunning reports a MailArchive Desktop running in any logon session of this account: the
// lock of desktop.lock and of every desktop-s<N>.lock in instance.Dir(), probed without writing
// (lockfile.Probe). A lock file that is not there is no Desktop; a folder that cannot be found or
// read, or a lock that cannot be probed (another account's, a link, a folder), refuses, since
// then it cannot tell (del-s4-A1-02).
func desktopRunning() error {
	dir, err := desktopLockDir()
	if err != nil {
		return cannotTell(err)
	}
	ents, err := os.ReadDir(dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return cannotTell(err)
	}
	for _, en := range ents {
		if _, ok := desktopLockSession(en.Name()); !ok {
			continue
		}
		held, err := lockfile.Probe(filepath.Join(dir, en.Name()))
		if err != nil {
			return cannotTell(err)
		}
		if held {
			return errDesktopIsRunning
		}
	}
	return nil
}

func cannotTell(err error) error {
	return &cannotTellError{cause: util.StripControl(err.Error())}
}

// desktopLockSession is the logon session a Desktop's lock file is for: "" for desktop.lock, N
// for desktop-s<N>.lock (instance.Names), whatever the case of the name (Windows, macOS).
func desktopLockSession(name string) (string, bool) {
	lower := strings.ToLower(name)
	if lower == instance.LockName {
		return "", true
	}
	n, ok := strings.CutPrefix(lower, "desktop-s")
	if !ok {
		return "", false
	}
	n, ok = strings.CutSuffix(n, ".lock")
	if !ok || n == "" || strings.Trim(n, "0123456789") != "" {
		return "", false
	}
	return n, true
}

// commandLineDelete is the Desktop's delete seams for these verbs, and why the secret store could
// not be opened (nil when it was): without it a remembered key is never forgotten, and step 8
// keeps the record, the key and the row (del-s4-A1-04).
// cfgPath is the Desktop's config file the verb was given with -config ("" is the default one): a
// Desktop started with -config keeps its settings, its list and deletions.log beside that file
// (MA-1018), so the verbs reach the same ones (the 1.5.3 friction review's B19).
func commandLineDelete(cfgPath string) (*desktop.CommandLineDelete, error, error) {
	if err := configFolderMissing(cfgPath); err != nil {
		return nil, nil, err
	}
	store, storeErr := secretStore()
	if storeErr != nil {
		store = nil
	}
	if cfgPath != "" {
		return desktop.NewCommandLineDeleteAt(abspath(cfgPath), store, deleteScheduler, deleteInstalled), storeErr, nil
	}
	seams, err := desktop.NewCommandLineDelete(store, deleteScheduler, deleteInstalled)
	return seams, storeErr, err
}

// configFolderMissing refuses a -config FILE whose folder does not exist (the 1.5.3 friction fixes'
// adversarial pass, finding 4): a mistyped folder otherwise made a new, empty list there, and the
// verbs then listed, deleted and logged against a list and a deletions.log the real Desktop never
// reads. FILE itself may be missing — a Desktop started with -config keeps its list beside FILE
// before it writes FILE — but its folder is there once that Desktop has run.
func configFolderMissing(cfgPath string) error {
	if cfgPath == "" {
		return nil
	}
	dir := filepath.Dir(abspath(cfgPath))
	if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
		return nil
	}
	return fmt.Errorf("There are no MailArchive Desktop settings beside %s: its folder %s does not exist, so nothing was changed. Give -config the config file MailArchive Desktop was started with.",
		encmark.CommandPath(util.StripControl(abspath(cfgPath))), encmark.CommandPath(util.StripControl(dir)))
}

// configFlag is the -config flag the three Your-archives verbs share.
func configFlag(fs *flag.FlagSet) *string {
	return fs.String("config", "", "the config file of a MailArchive Desktop started with -config, to reach its list of archives (default: the usual one)")
}

// configArg is " -config FILE" for a command the verb prints, when it was given one.
func configArg(cfgPath string) string {
	if cfgPath == "" {
		return ""
	}
	return " -config " + encmark.CommandPath(abspath(cfgPath))
}

// rowOf is §3.1's row rule at the terminal: the row of Your archives that out is. A list that
// cannot be read refuses (it cannot tell, del-s4-A2-02); so does a folder that is not a row
// (del-s4-A1-01), with what to do in the refusal for verb.
func rowOf(seams *desktop.CommandLineDelete, out, notRow string) (string, error) {
	row, ok, err := seams.Row(out)
	list := `"` + util.StripControl(seams.ListPath()) + `"` // quoted: "Application Support" has a space (item 4)
	switch {
	case err != nil && errors.Is(err, util.ErrReadableByOthers):
		// "readable by other users" said once, with the remedy (the 1.5.3 friction review's item 4).
		return "", fmt.Errorf("Your archives (%s) cannot be read: other users of this computer can read it, so MailArchive will not, and cannot tell whether %s is in it; nothing was changed. Run chmod 600 %s, then try again.", list, util.StripControl(out), list)
	case err != nil:
		return "", fmt.Errorf("Your archives (%s) cannot be read (%s), so MailArchive cannot tell whether %s is in it; nothing was changed.", list, util.StripControl(err.Error()), util.StripControl(out))
	case !ok:
		return "", errors.New(notRow)
	}
	return row, nil
}

func runDeleteArchive(args []string) error {
	fs := flag.NewFlagSet("mailarchive delete-archive", flag.ContinueOnError)
	out := fs.String("out", "", "the archive folder to delete, as its row in Your archives shows it (required)")
	planOut := fs.String("plan-out", "", "save the plan to this NEW file and delete nothing (to get approval first)")
	recordOut := fs.String("record-out", "", "save the deletion record to this NEW file (without it, the record is printed at the end)")
	cfgPath := configFlag(fs)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: mailarchive delete-archive -out DIR [-plan-out FILE | -record-out FILE] [-config FILE]\n\n"+
			"Permanently delete the archive in DIR, a folder in Your archives in MailArchive Desktop: its mail files, its index and MailArchive's own files in it — never a file it did not write, unless you choose to delete the mail copies it did not record — and stop its scheduled backups, remove it from Your archives and, for an encrypted archive, make this computer forget its record and key when this is the last copy it recorded. There is no Recycle Bin or Trash. The plan is shown first; then you type the folder's name to confirm. It runs only at a terminal, and not while MailArchive Desktop is running (use Delete… on its Your archives card instead). A folder not in Your archives can be added first with mailarchive add-archive DIR.\n\n%s\n\nOptions:\n", deleteVerbExit)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q (the folder is given with -out)", fs.Arg(0))
	}
	if *out == "" {
		return errors.New("-out is required (the archive folder to delete)")
	}
	return interrupted(deleteArchiveWith(*cfgPath, stdAsker(), abspath(*out), *planOut, *recordOut, os.Stdout), os.Stderr)
}

// notRowToDelete is delete-archive's refusal of a folder that is not a row of Your archives: it
// names the folder as given, and the usual causes (an elevated or another account's terminal
// reads that account's list; a mapped drive is another spelling there), and how to list it: the
// Desktop's Open an archive folder… or, at this terminal, add-archive (the 1.5.3 friction review's
// H3: it named Add another archive, which makes a new archive, and the command line had no way).
func notRowToDelete(out, cfgPath string) string {
	return fmt.Sprintf("%s is not in Your archives (MailArchive Desktop's list for the account this terminal runs as), so nothing was deleted. Give the folder as its row in Your archives shows it, in a terminal of your own account (not one run as administrator or as another user). To list a folder that holds an archive, run mailarchive add-archive%s %s, or click Open an archive folder… in MailArchive Desktop; then delete it.",
		util.StripControl(out), configArg(cfgPath), encmark.CommandPath(util.StripControl(out)))
}

// deleteArchive is delete-archive after its flags (§3.3), for the Desktop's default config.
func deleteArchive(a asker, out, planOut, recordOut string, stdout io.Writer) error {
	return deleteArchiveWith("", a, out, planOut, recordOut, stdout)
}

// deleteArchiveWith is delete-archive after its flags (§3.3), for the Desktop whose config file is
// cfgPath ("" the default): the terminal and no Desktop, the row, the saved files' checks, the
// plan, §4.3's question, the folder's name, the delete with its re-checks, the result, and the
// deletion record saved or printed. A signal is watched throughout (verbWatch).
func deleteArchiveWith(cfgPath string, a asker, out, planOut, recordOut string, stdout io.Writer) error {
	if !a.attended() {
		return keyVerbTerminal("delete-archive", nil)
	}
	watch := startVerbWatch(os.Stderr, stdout, "delete-archive")
	defer watch.stop()
	if err := desktopRunning(); err != nil {
		return err
	}
	if planOut != "" && recordOut != "" {
		return errors.New("-plan-out saves the plan and deletes nothing, and -record-out saves the record of a delete: give one of them; nothing was deleted")
	}
	seams, storeErr, err := commandLineDelete(cfgPath)
	if err != nil {
		return err
	}
	row, err := rowOf(seams, out, notRowToDelete(out, cfgPath))
	if err != nil {
		return err
	}
	watch.setRow(row)
	watch.mu.Lock()
	watch.cfg = cfgPath
	watch.mu.Unlock()
	if planOut != "" || recordOut != "" {
		known, kerr := seams.KnownArchives()
		if kerr != nil {
			return fmt.Errorf("MailArchive cannot read every archive it knows (%s), so it cannot check that the file you named is outside all of them; nothing was deleted", util.StripControl(kerr.Error()))
		}
		for _, f := range []struct{ flag, what, path string }{{"-plan-out", "the plan", planOut}, {"-record-out", "the deletion record", recordOut}} {
			if f.path == "" {
				continue
			}
			abs, err := checkSavedFile(f.flag, f.what, f.path, []string{out, row}, known, watch.creator(openProbeFile))
			watch.disown(phaseReading) // the probe is removed
			if err != nil {
				return err
			}
			if f.flag == "-plan-out" {
				planOut = abs
			} else {
				recordOut = abs
			}
		}
	}
	plan, err := archdelete.Plan(row, seams.PlanDeps())
	if err != nil {
		return terminalRefusal(err.Error(), row, cfgPath)
	}
	for _, l := range deletePlanLines(plan, seams.LogPath()) {
		a.say("%s", l)
	}
	if storeErr != nil && plan.Kind == archdelete.Encrypted {
		a.say("  This computer's secret store could not be opened (%s): if this computer remembers the archive's key, the key and the archive's row are kept, and the result says so.", util.StripControl(storeErr.Error()))
	}
	if planOut != "" {
		text := seams.PlannedText(plan)
		watch.savingPlan(planOut)
		err := writeSavedFile(watch.creator(openSavedFile), planOut, text)
		if err != nil {
			watch.disown(phaseReading)
			stdout.Write(text)
			return fmt.Errorf("The plan could not be saved to %s: it is shown above (%s). Nothing was deleted", planOut, util.StripControl(err.Error()))
		}
		watch.disown(phaseDone)
		fmt.Fprintf(stdout, "The plan was saved to %s. Nothing was deleted.\n", planOut)
		return nil
	}
	unrecorded := false
	if n := len(plan.Unrecorded); n > 0 {
		watch.set(phaseAsking)
		q := fmt.Sprintf("Also delete these %d files (for retention)?", n)
		if n == 1 {
			q = "Also delete this 1 file (for retention)?" // B7, item 12
		}
		unrecorded, err = askYes(a, q, false)
		watch.set(phaseReading)
		if err != nil {
			return promptEnded(err)
		}
	}
	shown := visibleName(plan.Name)
	// The folder's name is the decision: up to nameTries tries, a wrong one said each time and
	// nothing deleted; the last wrong one refuses as before (the 1.5.3 friction review's B10, B8).
	var typed string
	for try := 1; ; try++ {
		watch.set(phaseAsking)
		typed, err = a.line(fmt.Sprintf("Type the folder's name, %s, to delete it (capital letters count): ", shown))
		watch.set(phaseReading)
		if err != nil {
			return promptEnded(err)
		}
		if shown != plan.Name && archdelete.NameMatches(typed, shown) {
			// The name as the terminal shows it, its control characters written out, is the name
			// typed (del-s4-A2-06).
			typed = plan.Name
		}
		if archdelete.NameMatches(typed, plan.Name) || try == nameTries {
			break
		}
		a.say("That is not the folder's name, %s (capital letters count). Nothing was deleted; %s.", shown, triesLeft(nameTries-try))
	}
	// The person may have been a while: a Desktop started meanwhile refuses, nothing changed.
	if err := desktopRunning(); err != nil {
		return err
	}
	before := func(step int) error {
		beforeDeleteStep(step)
		err := desktopRunning()
		if step == 2 {
			// Nothing has changed yet: a Desktop found, or a signal held, refuses (del-s4-A2-01).
			if err != nil {
				return err
			}
			// A row only the Desktop draws is listed first, as its tidy-up would list it: step 4
			// clears what draws it, and a delete stopped after that is finished from its row.
			if err := seams.ListRow(row); err != nil {
				return listNotSaved(seams, err, "deleted")
			}
			return watch.beginChanges()
		}
		var ct *cannotTellError
		switch {
		case err == nil:
			return nil
		case errors.Is(err, errDesktopIsRunning):
			return errDesktopStarted
		case errors.As(err, &ct):
			return ct.midway()
		}
		return err
	}
	x := seams.ExecDeps(row, before)
	// The delete says it is over right before its finished line is written, so a signal from
	// then on, and between its return and the result, is never "did not finish": the result and
	// the record are printed (del-s4 residual 2, and its adversarial pass's finding 5).
	x.Finished = func() { watch.set(phaseDone) }
	watch.set(phaseHolding)
	res := archdelete.Execute(plan, typed, unrecorded, x)
	afterDeleteReturned()
	held := watch.over()
	if res.Status == archdelete.StatusRefused {
		msg := res.Message()
		if res.Refusal != nil && res.Refusal.Case == archdelete.CaseName {
			// The name quoted as the question showed it, its control characters written out
			// (del-s4 residual 4), not stripped away.
			msg = strings.Replace(msg, plan.Name, visibleName(plan.Name), 1)
		}
		return terminalRefusal(msg, row, cfgPath)
	}
	for _, l := range res.Lines {
		fmt.Fprintln(stdout, util.StripControl(archdelete.LocalLine(l, runtime.GOOS)))
	}
	for _, h := range terminalHints(res.Message(), row, cfgPath) {
		fmt.Fprintln(stdout, h)
	}
	printRecord := func() {
		fmt.Fprintln(stdout, recordHeading)
		stdout.Write(res.Record)
	}
	// A stop before anything changed (Ctrl-C held until step 2, or a Desktop found there) prints
	// its short line, not a whole record of nothing (the 1.5.3 friction review's B17); the record
	// is still saved to -record-out when one was named, and its digest is in deletions.log.
	stoppedEarly := res.Refusal != nil && res.Refusal.Case == archdelete.CaseStopped
	switch {
	case res.Record == nil:
	case recordOut == "" && stoppedEarly:
	case recordOut == "":
		printRecord()
	default:
		watch.saving(recordOut, res.Record, res.ExitCode())
		err := writeSavedFile(openSavedFile, recordOut, res.Record)
		watch.set(phaseDone)
		if err != nil {
			printRecord()
			fmt.Fprintf(stdout, "The deletion record could not be saved to %s: it is shown above (%s).\n", recordOut, util.StripControl(err.Error()))
		} else {
			fmt.Fprintf(stdout, "The deletion record was saved to %s.\n", recordOut)
		}
	}
	if held != nil && res.Status == archdelete.StatusRefusedAfterStart {
		return &exitError{code: signalExit(held)}
	}
	if code := res.ExitCode(); code != 0 {
		return &exitError{code: code}
	}
	return nil
}

// nameTries is how many times delete-archive asks for the folder's name before it refuses (B10).
const nameTries = 3

// triesLeft says how many tries are left.
func triesLeft(n int) string {
	if n == 1 {
		return "1 try left"
	}
	return fmt.Sprintf("%d tries left", n)
}

// listNotSaved is either verb's refusal when the row could not be written into Your archives
// before it changed anything (CommandLineDelete.ListRow): nothing was done (what: "deleted",
// "changed").
func listNotSaved(seams *desktop.CommandLineDelete, err error, what string) error {
	var busy *archindex.BusyError
	if errors.As(err, &busy) {
		// Another MailArchive program is changing the list (CON-07).
		return fmt.Errorf("Your archives (%s) is being changed by another MailArchive window, so nothing was %s; try again", util.StripControl(seams.ListPath()), what)
	}
	return fmt.Errorf("Your archives (%s) could not be saved (%s), so nothing was %s", util.StripControl(seams.ListPath()), util.StripControl(err.Error()), what)
}

// recordHeading heads the deletion record printed at the terminal.
const recordHeading = "\nDeletion record (copy it now: MailArchive does not keep a copy)"

// terminalRefusal is a refusal in archdelete's words at the terminal: the folder named, since the
// words were written to sit beside its row (del-s4-A3-05), as the plan shows it (its control
// characters written out, del-s4 residual 4), and the terminal's equivalents of the Desktop
// controls they name. cfgPath is the verb's -config, carried into the commands it names.
func terminalRefusal(msg, row, cfgPath string) error {
	msg = util.StripControl(msg)
	lines := append([]string{fmt.Sprintf("%s (Folder: %s)", msg, visibleName(row))}, terminalHints(msg, row, cfgPath)...)
	return errors.New(strings.Join(lines, "\n"))
}

// terminalHints are the terminal's equivalents of the Desktop controls a line in archdelete's
// words names (del-s4-A3-06), for the row: the words are shared with the Desktop, where they fit.
// The delete verbs' commands carry the verb's own -config (B19).
func terminalHints(text, row, cfgPath string) []string {
	l := strings.ToLower(text)
	p := encmark.CommandPath(util.StripControl(row))
	var hints []string
	if strings.Contains(l, "delete again") {
		hints = append(hints, "At this terminal, Delete again is: mailarchive delete-archive"+configArg(cfgPath)+" -out "+p)
	}
	if strings.Contains(l, "remove from list") {
		hints = append(hints, "At this terminal, Remove from list is: mailarchive unlist-archive"+configArg(cfgPath)+" -out "+p)
	}
	if strings.Contains(l, "forget this encrypted archive here") {
		hints = append(hints, "At this terminal, Forget this encrypted archive here is: mailarchive forget-archive -out "+p)
	}
	if strings.Contains(l, "switch to another archive") || strings.Contains(l, "add another archive") || strings.Contains(l, "in settings") {
		hints = append(hints, "Switching archives, Add another archive and Settings are in MailArchive Desktop.")
	}
	return hints
}

// promptEnded is a question ended without an answer, said plainly: input that ran out (Ctrl-D,
// or Ctrl-Z then Enter on Windows) changes nothing (del-s4-A3-08). A signal is interrupted's.
func promptEnded(err error) error {
	if errors.Is(err, io.EOF) {
		return errors.New("no answer was typed (the input ended), so nothing was changed")
	}
	return err
}

// visibleName is a folder's name as the terminal shows it: each control character written out as
// \uXXXX, so a name holding one can still be typed as shown (del-s4-A2-06).
func visibleName(name string) string {
	var b strings.Builder
	for _, r := range name {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			fmt.Fprintf(&b, `\u%04X`, r)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// deletePlanLines are the plan as the terminal shows it (§3.1's panel, §3.3): every path and
// name stripped of control characters (the folder's own written out, as its name is asked); each
// store's number beside its full name (§8.1).
func deletePlanLines(p *archdelete.DeletePlan, logPath string) []string {
	c := util.StripControl
	var lines []string
	if l := p.FinishesLine(); l != "" {
		lines = append(lines, l) // first: it finishes a delete that stopped part way (D1)
	}
	if l := p.HeldLine(); l != "" {
		lines = append(lines, c(l)) // it would stop at the same file again (the 1.5.3 re-walk's N17)
	}
	lines = append(lines, fmt.Sprintf("MailArchive will permanently delete the %s archive in this folder (no Recycle Bin or Trash):", p.Kind), "  "+visibleName(p.Folder))
	lines = append(lines, fmt.Sprintf("  files: %d (%s)", p.Files, sizeWords(p.Bytes)))
	for i := range p.Stores {
		// Not "store" (B12, item 6); the mail folders inside it named too (the 1.5.3 re-walk's N16).
		lines = append(lines, fmt.Sprintf("  mail folder %d: %s", i+1, c(p.StoreLabel(i))))
	}
	// Only what step 2 would stop, by when it runs, or "none"; a Run now backup once (B1, B15).
	for _, l := range p.ScheduleLines() {
		lines = append(lines, "  "+c(l))
	}
	for _, n := range p.Schedules.Unconfirmed {
		lines = append(lines, "  Not removed, check it yourself: "+c(n.Text))
	}
	for _, n := range p.Schedules.OtherHost {
		lines = append(lines, "  Not removed, check it yourself: "+c(n.Text))
	}
	for _, n := range p.Schedules.CheckYourself {
		lines = append(lines, "  Not removed, check it yourself: "+c(n))
	}
	if p.Paused {
		lines = append(lines, "  This archive is paused.")
	}
	if p.Synced != "" {
		lines = append(lines, "  "+c(archdelete.SyncedLine(p.Synced)))
	}
	if l := p.KeptLine(); l != "" {
		lines = append(lines, "  "+c(l)) // named, as the panel names them (the 1.5.3 re-walk's N7)
	}
	if n := len(p.Unrecorded); n > 0 {
		lines = append(lines, fmt.Sprintf("  Mail copies this archive did not record: %d (kept unless you choose to delete them)", n))
		for i, it := range p.Unrecorded {
			if i == 10 {
				lines = append(lines, fmt.Sprintf("    and %d more", n-10))
				break
			}
			lines = append(lines, "    "+c(it.Rel))
		}
	}
	if p.Kind == archdelete.Encrypted {
		lines = append(lines, "  "+p.KeyLine())
	}
	// This computer's items only, as the Desktop shows it (B4, MA-1019); the saved plan keeps all.
	return append(lines, "  "+archdelete.DoesNotReachLineOn(runtime.GOOS), "  "+archdelete.PlanLogLineAt(logPath))
}

// sizeWords is a byte count in words.
func sizeWords(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d bytes", n)
}

// checkSavedFile is §8.1's rule for -plan-out and -record-out [O2-02], before the plan is shown
// and before step 0: keyring.CheckNewFileOut's rules (not inside the folder being deleted, not
// inside an encrypted archive, not over an existing name or a link), not inside any known
// archive, and its folder exists and can be written (a probe file created by create, new, and
// removed at once). It returns the absolute path.
func checkSavedFile(flagName, what, path string, folders, known []string, create func(string) (*os.File, error)) (string, error) {
	var abs string
	for _, f := range folders {
		a, err := keyring.CheckNewFileOut(flagName, what, path, f)
		if err != nil {
			return "", err
		}
		abs = a
	}
	for _, k := range known {
		if util.Within(util.ResolveExisting(k), util.ResolveExisting(abs)) {
			return "", fmt.Errorf("%s %s is inside the archive %s; save %s outside every archive", flagName, abs, k, what)
		}
	}
	dir := filepath.Dir(abs)
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return "", fmt.Errorf("%s %s: its folder %s does not exist; choose a folder that does", flagName, abs, dir)
	}
	var nonce [8]byte
	rand.Read(nonce[:])
	probe := filepath.Join(dir, ".mailarchive-probe-"+hex.EncodeToString(nonce[:])+".tmp")
	f, err := create(probe)
	if err != nil {
		return "", fmt.Errorf("%s %s: its folder %s cannot be written (%s); choose a folder you can write to", flagName, abs, dir, util.StripControl(err.Error()))
	}
	f.Close()
	os.Remove(probe)
	return abs, nil
}

// writeSavedFile creates path new with open and writes data, flushed; a file it could not finish
// is removed, so no partial plan or record is left.
func writeSavedFile(open func(string) (*os.File, error), path string, data []byte) error {
	f, err := open(path)
	if err != nil {
		return err
	}
	_, werr := f.Write(data)
	if werr == nil {
		werr = f.Sync()
	}
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		os.Remove(path)
		return werr
	}
	return nil
}

// watchPhase is where a delete verb is, for what Ctrl-C or a terminate signal does there.
type watchPhase int

const (
	// phaseReading: only reading (the checks, the plan, the list): the verb stops at once with
	// the signal's code, nothing changed (a probe file it made is removed first).
	phaseReading watchPhase = iota
	// phasePlanSaving: -plan-out is being written: the file, once made, is removed and the verb
	// stops at once with the signal's code, nothing deleted (del-s4 residual 1).
	phasePlanSaving
	// phaseAsking: a question is asked; the prompt tells its own signal (interrupted).
	phaseAsking
	// phaseHolding: the delete began (its started line, its lock, the plan checked again) and has
	// changed nothing yet: the signal is held, and the delete refuses right before step 2, its
	// finished line written, so the row is never "Delete not finished" (del-s4-A2-01).
	phaseHolding
	// phaseChanging: step 2 began: the delete stops at once, not finished; running it again by
	// its row's spelling finishes it (§5.4, del-s4-A3-01).
	phaseChanging
	// phaseFinishing: Remove from list is acting: the signal waits for it to finish.
	phaseFinishing
	// phaseSaving: the delete is over and its record is being saved: the record is printed, the
	// file named as maybe incomplete, and the verb ends with the delete's own code (del-s4-A2-08).
	phaseSaving
	// phaseDone: the delete is over (ExecDeps.Finished) or the plan saved, and the result is
	// being printed: a signal changes nothing, and the verb ends with its own code.
	phaseDone
)

// verbWatch is a delete verb's Ctrl-C and terminate signal, from its start to its end.
type verbWatch struct {
	mu        sync.Mutex
	phase     watchPhase
	held      os.Signal
	row       string
	cfg       string // the verb's -config, carried into the command it names (B19)
	savePath  string
	owned     string
	ownedFile *os.File
	// creating is the file creator is making now, and made is closed once that returns; acting
	// is set by a signal that waits for it, and closed once the signal has acted.
	creating    string
	made        chan struct{}
	acting      chan struct{}
	record      []byte
	code        int
	w, stdout   io.Writer
	verb        string
	exit        func(int)
	quit, done  chan struct{}
	unsubscribe func()
	stopOnce    sync.Once
}

// newVerbWatch watches sigs for verb until stop: what it says goes to w (the record, when saving
// it, to stdout), and exit ends the process.
func newVerbWatch(sigs <-chan os.Signal, w, stdout io.Writer, verb string, exit func(int)) *verbWatch {
	v := &verbWatch{w: w, stdout: stdout, verb: verb, exit: exit, quit: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(v.done)
		for {
			select {
			case s := <-sigs:
				v.signal(s)
			case <-v.quit:
				return
			}
		}
	}()
	return v
}

func (v *verbWatch) set(p watchPhase) {
	v.mu.Lock()
	v.phase = p
	v.mu.Unlock()
}

func (v *verbWatch) setRow(row string) {
	v.mu.Lock()
	v.row = row
	v.mu.Unlock()
}

// now is the phase the verb is in.
func (v *verbWatch) now() watchPhase {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.phase
}

// beginChanges is the delete's last word before step 2: a held signal refuses it; otherwise a
// signal from now on stops it at once.
func (v *verbWatch) beginChanges() error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.held != nil {
		return fmt.Errorf("Stopped by %s before anything was deleted, so nothing was changed", signalWords(v.held))
	}
	v.phase = phaseChanging
	return nil
}

// over ends the delete's watch over its steps and returns a signal it held.
func (v *verbWatch) over() os.Signal {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.phase = phaseDone
	return v.held
}

// saving is the record being saved to path.
func (v *verbWatch) saving(path string, record []byte, code int) {
	v.mu.Lock()
	v.phase, v.savePath, v.record, v.code = phaseSaving, path, record, code
	v.mu.Unlock()
}

// savingPlan is the plan being saved to path.
func (v *verbWatch) savingPlan(path string) {
	v.mu.Lock()
	v.phase, v.savePath = phasePlanSaving, path
	v.mu.Unlock()
}

// creator is open made through the watch: the file it creates is the verb's own until disown,
// and a signal meanwhile removes it before the verb exits (del-s4 residual 1). A signal while
// open runs waits a moment (createWait) for it, then acts first, so a file just made is never
// left behind unowned; open is never run under v.mu, so a folder that hangs (a network share
// that stopped answering) does not hold the signal off.
func (v *verbWatch) creator(open func(string) (*os.File, error)) func(string) (*os.File, error) {
	return func(path string) (*os.File, error) {
		made := make(chan struct{})
		v.mu.Lock()
		v.creating, v.made = path, made
		v.mu.Unlock()
		f, err := open(path)
		v.mu.Lock()
		v.creating, v.made = "", nil
		if err == nil {
			v.owned, v.ownedFile = path, f
		}
		acting := v.acting
		close(made)
		v.mu.Unlock()
		if acting != nil {
			<-acting // a signal waited for this file: it acts on it first
		}
		return f, err
	}
}

// disown keeps the file creator made, or it is gone already; then is the phase from now on.
func (v *verbWatch) disown(then watchPhase) {
	v.mu.Lock()
	v.owned, v.ownedFile, v.phase = "", nil, then
	v.mu.Unlock()
}

// removeOwned removes the file the verb made and owns now, under v.mu; false when it is there
// still. The verb's own handle is closed first: Windows removes no file a program holds open
// (Go opens files without delete sharing), so the plan being written would otherwise stay. A
// close that waits on a write to a folder that hangs is not waited for longer than createWait, and
// neither is the remove after it: one that has not answered by then is "there still", so the verb
// exits naming the file as maybe incomplete rather than stalling under v.mu (the delete residuals'
// security pass, CON-03).
func (v *verbWatch) removeOwned() bool {
	if v.owned == "" {
		return true
	}
	if f := v.ownedFile; f != nil {
		closed := make(chan struct{})
		go func() { f.Close(); close(closed) }()
		select {
		case <-closed:
		case <-time.After(createWait):
		}
	}
	removed := make(chan error, 1)
	go func(path string) { removed <- removeOwnedFile(path) }(v.owned)
	select {
	case err := <-removed:
		return err == nil || errors.Is(err, fs.ErrNotExist)
	case <-time.After(createWait):
		return false
	}
}

// waitMade is a signal's wait, under v.mu, for a file creator is making: up to createWait, with
// v.mu released meanwhile. It returns the file still being made then ("" when none is).
func (v *verbWatch) waitMade() string {
	if v.made == nil {
		return ""
	}
	made := v.made
	v.acting = make(chan struct{})
	v.mu.Unlock()
	select {
	case <-made:
	case <-time.After(createWait):
	}
	v.mu.Lock()
	return v.creating
}

// signal acts on one signal by the phase.
func (v *verbWatch) signal(s os.Signal) {
	v.mu.Lock()
	unmade := ""
	if v.phase == phaseReading || v.phase == phasePlanSaving {
		unmade = v.waitMade()
		if v.acting != nil {
			defer func(acting chan struct{}) { close(acting) }(v.acting)
			v.acting = nil
		}
	}
	phase, row, path, record, code, cfg := v.phase, v.row, v.savePath, v.record, v.code, v.cfg
	first := v.held == nil
	if first && (phase == phaseHolding || phase == phaseFinishing) {
		v.held = s
	}
	if phase == phaseReading || phase == phasePlanSaving {
		// The verb's own file goes before it exits, and no other is made meanwhile: v.mu is
		// held to the end.
		defer v.mu.Unlock()
		owned, removed := v.owned != "", v.removeOwned()
		if phase == phaseReading {
			if unmade != "" {
				fmt.Fprintf(v.w, "mailarchive: %s was stopped by %s; nothing was deleted, but %s was still being created: delete that file if it is there\n", v.verb, signalWords(s), unmade)
			} else {
				fmt.Fprintf(v.w, "mailarchive: %s was stopped by %s; nothing was changed\n", v.verb, signalWords(s))
			}
			v.exit(signalExit(s))
			return
		}
		what := "nothing was saved"
		switch {
		case unmade != "":
			what = "the file was still being created, so delete it if it is there"
		case !removed:
			what = "that file may be incomplete, so delete it"
		case owned:
			what = "the unfinished file was removed"
		}
		fmt.Fprintf(v.w, "mailarchive: delete-archive was stopped by %s while saving the plan to %s: %s. Nothing was deleted.\n", signalWords(s), path, what)
		v.exit(signalExit(s))
		return
	}
	v.mu.Unlock()
	switch phase {
	case phaseHolding:
		if first {
			fmt.Fprintln(v.w, "mailarchive: stopping: the delete stops before it changes anything.")
		}
	case phaseFinishing:
		if first {
			fmt.Fprintln(v.w, "mailarchive: Remove from list is finishing what it began; it stops when that is done.")
		}
	case phaseChanging:
		fmt.Fprintf(v.w, "mailarchive: delete-archive was stopped by %s and did not finish: the folder may be partly deleted. Run `mailarchive delete-archive%s -out %s` again (or Delete… in MailArchive Desktop) to finish it.\n", signalWords(s), configArg(cfg), encmark.CommandPath(util.StripControl(row)))
		v.exit(signalExit(s))
	case phaseSaving:
		fmt.Fprintln(v.stdout, recordHeading)
		v.stdout.Write(record)
		fmt.Fprintf(v.w, "mailarchive: stopped by %s while saving the deletion record to %s: that file may be incomplete, so delete it; the record is shown above. The delete itself had finished, as its result says.\n", signalWords(s), path)
		v.exit(code)
	}
}

// stop ends the watch and waits for it.
func (v *verbWatch) stop() {
	v.stopOnce.Do(func() {
		if v.unsubscribe != nil {
			v.unsubscribe()
		}
		close(v.quit)
		<-v.done
	})
}

// signalWords names a signal plainly.
func signalWords(s os.Signal) string {
	if s == syscall.SIGTERM {
		return "a terminate signal"
	}
	return "Ctrl-C"
}

func runUnlistArchive(args []string) error {
	fs := flag.NewFlagSet("mailarchive unlist-archive", flag.ContinueOnError)
	out := fs.String("out", "", "the archive folder to remove from Your archives (required)")
	cfgPath := configFlag(fs)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: mailarchive unlist-archive -out DIR [-config FILE]\n\n"+
			"Remove DIR from Your archives in MailArchive Desktop. The folder and its files are kept. If the folder has scheduled backups, the question names them and they are stopped too. It runs only at a terminal, and not while MailArchive Desktop is running (use Remove from list on its Your archives card instead).\n\n%s\n\nOptions:\n", unlistVerbExit)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("unexpected argument %q (the folder is given with -out)", fs.Arg(0))
	}
	if *out == "" {
		return errors.New("-out is required (the archive folder to remove from Your archives)")
	}
	return interrupted(unlistArchiveWith(*cfgPath, stdAsker(), abspath(*out), os.Stdout), os.Stderr)
}

// unlistArchive is unlist-archive after its flags, for the Desktop's default config.
func unlistArchive(a asker, out string, stdout io.Writer) error {
	return unlistArchiveWith("", a, out, stdout)
}

// unlistArchiveWith is unlist-archive after its flags (§3.3, §3.2, §5.2), for the Desktop whose
// config file is cfgPath ("" the default): the terminal and no Desktop, a row of Your archives,
// its question, then Remove from list stopping only what the question named.
func unlistArchiveWith(cfgPath string, a asker, out string, stdout io.Writer) error {
	if !a.attended() {
		return keyVerbTerminal("unlist-archive", nil)
	}
	watch := startVerbWatch(os.Stderr, stdout, "unlist-archive")
	defer watch.stop()
	if err := desktopRunning(); err != nil {
		return err
	}
	seams, _, err := commandLineDelete(cfgPath)
	if err != nil {
		return err
	}
	row, err := rowOf(seams, out, fmt.Sprintf("%s is not in Your archives, so there is nothing to remove from the list; nothing was changed.", util.StripControl(out)))
	if err != nil {
		return err
	}
	q, err := archdelete.PlanUnlist(row, seams.UnlistDeps(nil))
	if err != nil {
		return terminalRefusal(err.Error(), row, cfgPath)
	}
	a.say("Folder: %s", util.StripControl(row)) // labelled (B12, item 31)
	watch.set(phaseAsking)
	yes, err := askYes(a, util.StripControl(q.Question), false)
	watch.set(phaseReading)
	if err != nil {
		return promptEnded(err)
	}
	if !yes {
		return errors.New("nothing was changed")
	}
	if err := desktopRunning(); err != nil {
		return err
	}
	watch.set(phaseFinishing)
	// A row only the Desktop draws is listed first, so Remove from list removes it as any row.
	if err := seams.ListRow(row); err != nil {
		watch.over()
		return listNotSaved(seams, err, "changed")
	}
	res := archdelete.Unlist(row, seams.UnlistDeps(q.Named))
	watch.over()
	if res.Refusal != nil {
		return terminalRefusal(res.Refusal.Message, row, cfgPath)
	}
	for _, l := range res.Lines {
		fmt.Fprintln(stdout, util.StripControl(l))
	}
	for _, h := range terminalHints(strings.Join(res.Lines, "\n"), row, cfgPath) {
		fmt.Fprintln(stdout, h)
	}
	if code := res.ExitCode(); code != 0 {
		return &exitError{code: code}
	}
	return nil
}

// addVerbExit is add-archive's exit line in its help (X1).
const addVerbExit = "Exit: 0 added (or already listed); 1 refused, nothing changed (a folder that holds no archive, or Your archives that cannot be read or saved)."

// runAddArchive is `mailarchive add-archive FOLDER` (the 1.5.3 friction review's H3): an archive
// folder you already have listed in Your archives, as MailArchive Desktop's "Open an archive
// folder…" accepts one, so delete-archive and unlist-archive can reach it — on a computer with no
// Desktop too. It changes no file in the folder and does not make it the current archive.
func runAddArchive(args []string) error {
	fs := flag.NewFlagSet("mailarchive add-archive", flag.ContinueOnError)
	out := fs.String("out", "", "the archive folder to add (or give it as the one argument)")
	cfgPath := configFlag(fs)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: mailarchive add-archive [-config FILE] DIR\n\n"+
			"Add DIR, a folder that holds an archive you already have (plain or encrypted), to Your archives, MailArchive Desktop's list of archives, as Open an archive folder… in MailArchive Desktop does; then delete-archive and unlist-archive can reach it. Nothing in the folder is changed, and it does not become the archive MailArchive Desktop shows.\n\n%s\n\nOptions:\n", addVerbExit)
		fs.PrintDefaults()
	}
	// The flags may come before or after the folder (the 1.5.3 friction fixes' adversarial pass,
	// finding 3: Go's flag parsing stops at the first plain argument, so "add-archive DIR -config
	// FILE" read -config as a second folder). After a "--" every argument is a folder.
	var dirs []string
	rest := args
	for {
		if err := fs.Parse(rest); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil
			}
			return err
		}
		if fs.NArg() == 0 {
			break
		}
		consumed := len(rest) - fs.NArg()
		if consumed > 0 && rest[consumed-1] == "--" {
			dirs = append(dirs, fs.Args()...)
			break
		}
		dirs = append(dirs, fs.Arg(0))
		rest = fs.Args()[1:]
	}
	dir := *out
	switch {
	case len(dirs) > 1 || (len(dirs) == 1 && dir != ""):
		return errors.New("give one folder: mailarchive add-archive [-config FILE] DIR.")
	case len(dirs) == 1:
		dir = dirs[0]
	}
	if strings.TrimSpace(dir) == "" {
		return errors.New("the folder is required: mailarchive add-archive [-config FILE] DIR.")
	}
	return addArchiveWith(*cfgPath, abspath(dir), os.Stdout)
}

// addArchiveWith is add-archive after its flags, for the Desktop whose config file is cfgPath ("" the
// default): the folder's checks and the row written under the list's lock (CommandLineDelete.AddRow).
func addArchiveWith(cfgPath, dir string, stdout io.Writer) error {
	// Listing needs no secret store: none is opened.
	if err := configFolderMissing(cfgPath); err != nil {
		return err
	}
	var seams *desktop.CommandLineDelete
	if cfgPath != "" {
		seams = desktop.NewCommandLineDeleteAt(abspath(cfgPath), nil, deleteScheduler, deleteInstalled)
	} else {
		s, err := desktop.NewCommandLineDelete(nil, deleteScheduler, deleteInstalled)
		if err != nil {
			return err
		}
		seams = s
	}
	row, added, problem, err := seams.AddRow(dir)
	list := `"` + util.StripControl(seams.ListPath()) + `"` // quoted, as rowOf quotes it (item 4)
	switch {
	case err != nil:
		var busy *archindex.BusyError
		if errors.As(err, &busy) {
			return fmt.Errorf("Your archives (%s) is being changed by another MailArchive window, so nothing was changed; try again.", util.StripControl(seams.ListPath()))
		}
		if errors.Is(err, util.ErrReadableByOthers) {
			// "readable by other users" said once, with the remedy, as delete-archive says it
			// (the 1.5.3 friction fixes' adversarial pass, finding 11).
			return fmt.Errorf("Your archives (%s) cannot be read: other users of this computer can read it, so MailArchive will not, and nothing was changed. Run chmod 600 %s, then try again.", list, list)
		}
		return fmt.Errorf("Your archives (%s) could not be read or saved (%s), so nothing was changed.", list, util.StripControl(err.Error()))
	case problem != "":
		return fmt.Errorf("%s. Nothing was changed.", strings.TrimSuffix(util.StripControl(problem), "."))
	case !added:
		fmt.Fprintf(stdout, "%s is already in Your archives; nothing was changed.\n", util.StripControl(row))
		return nil
	}
	// delete-archive refuses while a Desktop runs, so its command is given for then with the Desktop
	// shut down, beside the Desktop's own Delete… (the 1.5.3 re-walk's N11); when this cannot tell,
	// the condition is said too.
	when, desk := "", ""
	if desktopRunning() != nil {
		when, desk = " (with MailArchive Desktop shut down)", ", or click Delete… on its row in Your archives in MailArchive Desktop."
	}
	fmt.Fprintf(stdout, "Added to Your archives: %s\nThe folder and its files are unchanged. To delete it%s: mailarchive delete-archive%s -out %s%s\n",
		util.StripControl(row), when, configArg(cfgPath), encmark.CommandPath(util.StripControl(row)), desk)
	return nil
}
