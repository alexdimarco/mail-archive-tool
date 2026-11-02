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
	"strings"
	"sync"
	"syscall"

	"mail-archive-tool/internal/archdelete"
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
const desktopRunningText = "MailArchive Desktop is running for your account (in this or another sign-in to this computer), so nothing was changed. Use the Desktop, or shut it down first"

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
func commandLineDelete() (*desktop.CommandLineDelete, error, error) {
	store, storeErr := secretStore()
	if storeErr != nil {
		store = nil
	}
	seams, err := desktop.NewCommandLineDelete(store, deleteScheduler, deleteInstalled)
	return seams, storeErr, err
}

// rowOf is §3.1's row rule at the terminal: the row of Your archives that out is. A list that
// cannot be read refuses (it cannot tell, del-s4-A2-02); so does a folder that is not a row
// (del-s4-A1-01), with what to do in the refusal for verb.
func rowOf(seams *desktop.CommandLineDelete, out, notRow string) (string, error) {
	row, ok, err := seams.Row(out)
	switch {
	case err != nil:
		return "", fmt.Errorf("Your archives (%s) cannot be read (%s), so MailArchive cannot tell whether %s is in it; nothing was changed", util.StripControl(seams.ListPath()), util.StripControl(err.Error()), util.StripControl(out))
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
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: mailarchive delete-archive -out DIR [-plan-out FILE | -record-out FILE]\n\n"+
			"Permanently delete the archive in DIR, a folder in Your archives in MailArchive Desktop: its mail files, its index and MailArchive's own files in it — never a file it did not write, unless you choose to delete the mail copies it did not record — and stop its scheduled backups, remove it from Your archives and, for an encrypted archive, make this computer forget its key. There is no Recycle Bin or Trash. The plan is shown first; then you type the folder's name to confirm. It runs only at a terminal, and not while MailArchive Desktop is running (use Delete… on its Your archives card instead).\n\n%s\n\nOptions:\n", deleteVerbExit)
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
	return interrupted(deleteArchive(stdAsker(), abspath(*out), *planOut, *recordOut, os.Stdout), os.Stderr)
}

// notRowToDelete is delete-archive's refusal of a folder that is not a row of Your archives: it
// names the folder as given, and the usual causes (an elevated or another account's terminal
// reads that account's list; a mapped drive is another spelling there).
func notRowToDelete(out string) string {
	return fmt.Sprintf("%s is not in Your archives (MailArchive Desktop's list for the account this terminal runs as), so nothing was deleted. Give the folder as its row in Your archives shows it, in a terminal of your own account (not one run as administrator or as another user); a folder not listed can be added with Add another archive in MailArchive Desktop", util.StripControl(out))
}

// deleteArchive is delete-archive after its flags (§3.3): the terminal and no Desktop, the row,
// the saved files' checks, the plan, §4.3's question, the folder's name, the delete with its
// re-checks, the result, and the deletion record saved or printed. A signal is watched
// throughout (verbWatch).
func deleteArchive(a asker, out, planOut, recordOut string, stdout io.Writer) error {
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
	seams, storeErr, err := commandLineDelete()
	if err != nil {
		return err
	}
	row, err := rowOf(seams, out, notRowToDelete(out))
	if err != nil {
		return err
	}
	watch.setRow(row)
	if planOut != "" || recordOut != "" {
		known, kerr := seams.KnownArchives()
		if kerr != nil {
			return fmt.Errorf("MailArchive cannot read every archive it knows (%s), so it cannot check that the file you named is outside all of them; nothing was deleted", util.StripControl(kerr.Error()))
		}
		for _, f := range []struct{ flag, what, path string }{{"-plan-out", "the plan", planOut}, {"-record-out", "the deletion record", recordOut}} {
			if f.path == "" {
				continue
			}
			abs, err := checkSavedFile(f.flag, f.what, f.path, []string{out, row}, known)
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
		return terminalRefusal(err.Error(), row)
	}
	for _, l := range deletePlanLines(plan) {
		a.say("%s", l)
	}
	if storeErr != nil && plan.Kind == archdelete.Encrypted {
		a.say("  This computer's secret store could not be opened (%s): if this computer remembers the archive's key, the key and the archive's row are kept, and the result says so.", util.StripControl(storeErr.Error()))
	}
	if planOut != "" {
		text := seams.PlannedText(plan)
		if err := writeSavedFile(planOut, text); err != nil {
			stdout.Write(text)
			return fmt.Errorf("The plan could not be saved to %s: it is shown above (%s). Nothing was deleted", planOut, util.StripControl(err.Error()))
		}
		fmt.Fprintf(stdout, "The plan was saved to %s. Nothing was deleted.\n", planOut)
		return nil
	}
	unrecorded := false
	if n := len(plan.Unrecorded); n > 0 {
		watch.set(phaseAsking)
		unrecorded, err = askYes(a, fmt.Sprintf("Also delete these %d files (for retention)?", n), false)
		watch.set(phaseReading)
		if err != nil {
			return promptEnded(err)
		}
	}
	shown := visibleName(plan.Name)
	watch.set(phaseAsking)
	typed, err := a.line(fmt.Sprintf("Type the folder's name, %s, to delete it: ", shown))
	watch.set(phaseReading)
	if err != nil {
		return promptEnded(err)
	}
	if shown != plan.Name && archdelete.NameMatches(typed, shown) {
		// The name as the terminal shows it, its control characters written out, is the name
		// typed (del-s4-A2-06).
		typed = plan.Name
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
	watch.set(phaseHolding)
	res := archdelete.Execute(plan, typed, unrecorded, seams.ExecDeps(row, before))
	held := watch.over()
	if res.Status == archdelete.StatusRefused {
		return terminalRefusal(res.Message(), row)
	}
	for _, l := range res.Lines {
		fmt.Fprintln(stdout, util.StripControl(l))
	}
	for _, h := range terminalHints(res.Message(), row) {
		fmt.Fprintln(stdout, h)
	}
	printRecord := func() {
		fmt.Fprintln(stdout, recordHeading)
		stdout.Write(res.Record)
	}
	switch {
	case res.Record == nil:
	case recordOut == "":
		printRecord()
	default:
		watch.saving(recordOut, res.Record, res.ExitCode())
		err := writeSavedFile(recordOut, res.Record)
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

// recordHeading heads the deletion record printed at the terminal.
const recordHeading = "\nDeletion record (copy it now: MailArchive does not keep a copy)"

// terminalRefusal is a refusal in archdelete's words at the terminal: the folder named, since the
// words were written to sit beside its row (del-s4-A3-05), and the terminal's equivalents of the
// Desktop controls they name.
func terminalRefusal(msg, row string) error {
	msg = util.StripControl(msg)
	lines := append([]string{fmt.Sprintf("%s (Folder: %s)", msg, util.StripControl(row))}, terminalHints(msg, row)...)
	return errors.New(strings.Join(lines, "\n"))
}

// terminalHints are the terminal's equivalents of the Desktop controls a line in archdelete's
// words names (del-s4-A3-06), for the row: the words are shared with the Desktop, where they fit.
func terminalHints(text, row string) []string {
	l := strings.ToLower(text)
	p := encmark.CommandPath(util.StripControl(row))
	var hints []string
	if strings.Contains(l, "delete again") {
		hints = append(hints, "At this terminal, Delete again is: mailarchive delete-archive -out "+p)
	}
	if strings.Contains(l, "remove from list") {
		hints = append(hints, "At this terminal, Remove from list is: mailarchive unlist-archive -out "+p)
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
func deletePlanLines(p *archdelete.DeletePlan) []string {
	c := util.StripControl
	lines := []string{fmt.Sprintf("MailArchive will permanently delete the %s archive in this folder (no Recycle Bin or Trash):", p.Kind), "  " + visibleName(p.Folder)}
	if p.Rerun != archdelete.RerunNone {
		lines = append(lines, "  This finishes a delete of this folder that did not finish.")
	}
	lines = append(lines, fmt.Sprintf("  files: %d (%s)", p.Files, sizeWords(p.Bytes)))
	for i, s := range p.Stores {
		lines = append(lines, fmt.Sprintf("  store %d: %s", i+1, c(s)))
	}
	if names := p.Schedules.Names(); len(names) > 0 {
		lines = append(lines, "  Schedules to stop: "+c(strings.Join(names, ", ")))
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
	if n := len(p.Kept); n > 0 {
		lines = append(lines, fmt.Sprintf("  Kept, not the archive's: %d", n))
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
		lines = append(lines, "  "+archdelete.PlanKeyLine)
	}
	return append(lines, "  What Delete does not reach: "+archdelete.DoesNotReach, "  "+archdelete.PlanLogLine)
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
// archive, and its folder exists and can be written (a probe file created with O_EXCL and removed
// at once). It returns the absolute path.
func checkSavedFile(flagName, what, path string, folders, known []string) (string, error) {
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
	f, err := os.OpenFile(probe, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", fmt.Errorf("%s %s: its folder %s cannot be written (%s); choose a folder you can write to", flagName, abs, dir, util.StripControl(err.Error()))
	}
	f.Close()
	os.Remove(probe)
	return abs, nil
}

// writeSavedFile creates path new and writes data, flushed; a file it could not finish is removed,
// so no partial plan or record is left.
func writeSavedFile(path string, data []byte) error {
	f, err := openSavedFile(path)
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
	// the signal's code, nothing changed.
	phaseReading watchPhase = iota
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
	// phaseDone: the result is being printed.
	phaseDone
)

// verbWatch is a delete verb's Ctrl-C and terminate signal, from its start to its end.
type verbWatch struct {
	mu          sync.Mutex
	phase       watchPhase
	held        os.Signal
	row         string
	savePath    string
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

// signal acts on one signal by the phase.
func (v *verbWatch) signal(s os.Signal) {
	v.mu.Lock()
	phase, row, path, record, code := v.phase, v.row, v.savePath, v.record, v.code
	first := v.held == nil
	if first && (phase == phaseHolding || phase == phaseFinishing) {
		v.held = s
	}
	v.mu.Unlock()
	switch phase {
	case phaseReading:
		fmt.Fprintf(v.w, "mailarchive: %s was stopped by %s; nothing was changed\n", v.verb, signalWords(s))
		v.exit(signalExit(s))
	case phaseHolding:
		if first {
			fmt.Fprintln(v.w, "mailarchive: stopping: the delete stops before it changes anything.")
		}
	case phaseFinishing:
		if first {
			fmt.Fprintln(v.w, "mailarchive: Remove from list is finishing what it began; it stops when that is done.")
		}
	case phaseChanging:
		fmt.Fprintf(v.w, "mailarchive: delete-archive was stopped by %s and did not finish: the folder may be partly deleted. Run `mailarchive delete-archive -out %s` again (or Delete… in MailArchive Desktop) to finish it.\n", signalWords(s), encmark.CommandPath(util.StripControl(row)))
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
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: mailarchive unlist-archive -out DIR\n\n"+
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
	return interrupted(unlistArchive(stdAsker(), abspath(*out), os.Stdout), os.Stderr)
}

// unlistArchive is unlist-archive after its flags (§3.3, §3.2, §5.2): the terminal and no
// Desktop, a row of Your archives, its question, then Remove from list stopping only what the
// question named.
func unlistArchive(a asker, out string, stdout io.Writer) error {
	if !a.attended() {
		return keyVerbTerminal("unlist-archive", nil)
	}
	watch := startVerbWatch(os.Stderr, stdout, "unlist-archive")
	defer watch.stop()
	if err := desktopRunning(); err != nil {
		return err
	}
	seams, _, err := commandLineDelete()
	if err != nil {
		return err
	}
	row, err := rowOf(seams, out, fmt.Sprintf("%s is not in Your archives, so there is nothing to remove from the list; nothing was changed", util.StripControl(out)))
	if err != nil {
		return err
	}
	q, err := archdelete.PlanUnlist(row, seams.UnlistDeps(nil))
	if err != nil {
		return terminalRefusal(err.Error(), row)
	}
	a.say("%s", util.StripControl(row))
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
	res := archdelete.Unlist(row, seams.UnlistDeps(q.Named))
	watch.over()
	if res.Refusal != nil {
		return terminalRefusal(res.Refusal.Message, row)
	}
	for _, l := range res.Lines {
		fmt.Fprintln(stdout, util.StripControl(l))
	}
	for _, h := range terminalHints(strings.Join(res.Lines, "\n"), row) {
		fmt.Fprintln(stdout, h)
	}
	if code := res.ExitCode(); code != 0 {
		return &exitError{code: code}
	}
	return nil
}
