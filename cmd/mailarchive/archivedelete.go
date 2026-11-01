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
	"syscall"

	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/desktop"
	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/instance"
	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/util"
)

// delete-archive and unlist-archive (docs/design-archive-delete.md §3.3, §8.1, slice 4): Delete…
// and Remove from list of MailArchive Desktop's Your archives card, at a terminal. What a delete
// removes, in which order and with which words is archdelete's; the seams are the Desktop's own
// (desktop.CommandLineDelete). These verbs add only the terminal, the check that no Desktop runs,
// the plan and the questions, the saved files and the exit codes.

// The seams the two verbs reach this computer through; tests give their own.
var (
	// desktopLockDir is where a running MailArchive Desktop holds its instance locks.
	desktopLockDir = instance.Dir
	// deleteScheduler and deleteInstalled are this computer's scheduler and its installed
	// entries (nil: the real ones).
	deleteScheduler archdelete.Scheduler
	deleteInstalled func() []schedule.InstalledEntry
	// beforeDeleteStep runs before the re-check before steps 4, 8 and 9 (a test starts a
	// Desktop there); nothing in a real run.
	beforeDeleteStep = func(step int) {}
	// openSavedFile creates the -plan-out or -record-out file, new (never over an existing name
	// or a link) and not owner-only: it is meant to be shared (§8.1).
	openSavedFile = func(path string) (*os.File, error) {
		return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	}
	// watchDeleteSignals watches for Ctrl-C and a terminate signal while the delete runs.
	watchDeleteSignals = func(w io.Writer, out string) (stop func()) {
		sigs := make(chan os.Signal, 1)
		signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
		stopWatch := watchDeleteStop(sigs, w, out, os.Exit)
		return func() { signal.Stop(sigs); stopWatch() }
	}
)

// deleteVerbExit and unlistVerbExit are the verbs' exit lines in their help (X1).
const (
	deleteVerbExit = "Exit: 0 deleted (or the plan saved); 3 not fully deleted (it says what is left and what to do); 1 refused, nothing deleted (including a name that does not match); 130 or 143 stopped by Ctrl-C or a terminate signal (a question so stopped changes nothing; the delete so stopped did not finish: run it again to finish it)."
	unlistVerbExit = "Exit: 0 removed from the list; 3 stopped part way (a schedule was stopped and the row kept: it says which); 1 refused or declined, nothing changed; 130 or 143 stopped by Ctrl-C or a terminate signal (nothing changed)."
)

// desktopRunningText refuses either verb while a Desktop runs (§3.3 [R3:C17]).
const desktopRunningText = "MailArchive Desktop is running for your account (in this or another sign-in to this computer), so nothing was changed. Use the Desktop, or shut it down first"

// errDesktopStarted is the re-check's stop before steps 4, 8 and 9.
var errDesktopStarted = errors.New("MailArchive Desktop started while the delete was running")

// desktopRunning reports a MailArchive Desktop running in any logon session of this account:
// instance.LockIsHeld on desktop.lock and on every desktop-s<N>.lock in instance.Dir(). A lock
// file that is not there is no Desktop; a folder that cannot be found or read refuses, since then
// it cannot tell.
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
		if session, ok := desktopLockSession(en.Name()); ok && instance.LockIsHeld(dir, session) {
			return errors.New(desktopRunningText)
		}
	}
	return nil
}

func cannotTell(err error) error {
	return fmt.Errorf("MailArchive cannot tell whether MailArchive Desktop is running for your account (%s), so nothing was changed", util.StripControl(err.Error()))
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

// commandLineDelete is the Desktop's delete seams for these verbs.
func commandLineDelete() (*desktop.CommandLineDelete, error) {
	store, err := secretStore()
	if err != nil {
		store = nil // no remembered key is forgotten; the record still is
	}
	return desktop.NewCommandLineDelete(store, deleteScheduler, deleteInstalled)
}

func runDeleteArchive(args []string) error {
	fs := flag.NewFlagSet("mailarchive delete-archive", flag.ContinueOnError)
	out := fs.String("out", "", "the archive folder to delete (required)")
	planOut := fs.String("plan-out", "", "save the plan to this NEW file and delete nothing (to get approval first)")
	recordOut := fs.String("record-out", "", "save the deletion record to this NEW file (without it, the record is printed at the end)")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: mailarchive delete-archive -out DIR [-plan-out FILE | -record-out FILE]\n\n"+
			"Permanently delete the archive in DIR: its mail files, its index and MailArchive's own files in it — never a file it did not write, unless you choose to delete the mail copies it did not record — and stop its scheduled backups, remove it from Your archives and, for an encrypted archive, make this computer forget its key. There is no Recycle Bin or Trash. The plan is shown first; then you type the folder's name to confirm. It runs only at a terminal, and not while MailArchive Desktop is running (use Delete… on its Your archives card instead).\n\n%s\n\nOptions:\n", deleteVerbExit)
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

// deleteArchive is delete-archive after its flags (§3.3): the terminal and no Desktop, the saved
// files' checks, the plan, §4.3's question, the folder's name, the delete with its re-checks, the
// result, and the deletion record saved or printed.
func deleteArchive(a asker, out, planOut, recordOut string, stdout io.Writer) error {
	if !a.attended() {
		return keyVerbTerminal("delete-archive", nil)
	}
	if err := desktopRunning(); err != nil {
		return err
	}
	if planOut != "" && recordOut != "" {
		return errors.New("-plan-out saves the plan and deletes nothing, and -record-out saves the record of a delete: give one of them; nothing was deleted")
	}
	seams, err := commandLineDelete()
	if err != nil {
		return err
	}
	row, listed := seams.Row(out)
	if !listed {
		row = out
	}
	known := seams.KnownArchives()
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
	plan, err := archdelete.Plan(row, seams.PlanDeps())
	if err != nil {
		return err
	}
	for _, l := range deletePlanLines(plan) {
		a.say("%s", l)
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
		if unrecorded, err = askYes(a, fmt.Sprintf("Also delete these %d files (for retention)?", n), false); err != nil {
			return err
		}
	}
	typed, err := a.line(fmt.Sprintf("Type the folder's name, %s, to delete it: ", util.StripControl(plan.Name)))
	if err != nil {
		return err
	}
	// The person may have been a while: a Desktop started meanwhile refuses, nothing changed.
	if err := desktopRunning(); err != nil {
		return err
	}
	before := func(step int) error {
		beforeDeleteStep(step)
		if err := desktopRunning(); err != nil {
			if err.Error() == desktopRunningText {
				return errDesktopStarted
			}
			return err
		}
		return nil
	}
	stopWatch := watchDeleteSignals(os.Stderr, out)
	res := archdelete.Execute(plan, typed, unrecorded, seams.ExecDeps(row, before))
	stopWatch()
	if res.Status == archdelete.StatusRefused {
		return errors.New(util.StripControl(res.Message()))
	}
	for _, l := range res.Lines {
		fmt.Fprintln(stdout, util.StripControl(l))
	}
	printRecord := func() {
		fmt.Fprintln(stdout, "\nDeletion record (copy it now: MailArchive does not keep a copy)")
		stdout.Write(res.Record)
	}
	switch {
	case res.Record == nil:
	case recordOut == "":
		printRecord()
	default:
		if err := writeSavedFile(recordOut, res.Record); err != nil {
			printRecord()
			fmt.Fprintf(stdout, "The deletion record could not be saved to %s: it is shown above (%s).\n", recordOut, util.StripControl(err.Error()))
		} else {
			fmt.Fprintf(stdout, "The deletion record was saved to %s.\n", recordOut)
		}
	}
	if code := res.ExitCode(); code != 0 {
		return &exitError{code: code}
	}
	return nil
}

// deletePlanLines are the plan as the terminal shows it (§3.1's panel, §3.3): every path and
// name stripped of control characters; each store's number beside its full name (§8.1).
func deletePlanLines(p *archdelete.DeletePlan) []string {
	c := util.StripControl
	lines := []string{fmt.Sprintf("MailArchive will permanently delete the %s archive in this folder (no Recycle Bin or Trash):", p.Kind), "  " + c(p.Folder)}
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

// watchDeleteStop is the delete's Ctrl-C and terminate signal: the delete cannot be stopped
// between its steps from here, so the first signal says it did not finish and how to finish it,
// and exits at once with the signal's code (130 or 143). Its started line stays without a
// finished line, which marks the row "Delete not finished" (§5.4); running it again finishes it.
func watchDeleteStop(sigs <-chan os.Signal, w io.Writer, out string, exit func(int)) (stop func()) {
	quit, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		select {
		case s := <-sigs:
			fmt.Fprintf(w, "mailarchive: delete-archive was stopped by a signal and did not finish: the folder may be partly deleted. Run `mailarchive delete-archive -out %s` again (or Delete… in MailArchive Desktop) to finish it.\n", encmark.CommandPath(util.StripControl(out)))
			exit(signalExit(s))
		case <-quit:
		}
	}()
	return func() { close(quit); <-done }
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
	if err := desktopRunning(); err != nil {
		return err
	}
	seams, err := commandLineDelete()
	if err != nil {
		return err
	}
	row, ok := seams.Row(out)
	if !ok {
		return fmt.Errorf("%s is not in Your archives, so there is nothing to remove from the list; nothing was changed", util.StripControl(out))
	}
	q, err := archdelete.PlanUnlist(row, seams.UnlistDeps(nil))
	if err != nil {
		return err
	}
	a.say("%s", util.StripControl(row))
	yes, err := askYes(a, util.StripControl(q.Question), false)
	if err != nil {
		return err
	}
	if !yes {
		return errors.New("nothing was changed")
	}
	if err := desktopRunning(); err != nil {
		return err
	}
	res := archdelete.Unlist(row, seams.UnlistDeps(q.Named))
	if res.Refusal != nil {
		return errors.New(util.StripControl(res.Refusal.Message))
	}
	for _, l := range res.Lines {
		fmt.Fprintln(stdout, util.StripControl(l))
	}
	if code := res.ExitCode(); code != 0 {
		return &exitError{code: code}
	}
	return nil
}
