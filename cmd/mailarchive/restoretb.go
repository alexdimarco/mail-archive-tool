package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/tbengine"
	"mail-archive-tool/internal/tbrestore"
)

// restore-thunderbird writes an archive, or some of its folders, into a new folder
// under a Thunderbird profile's Local Folders (docs/design-restore-thunderbird.md §10 and
// its folded notes; slice R6). The plan goes to stderr, the result to stdout (X8). It
// takes no -config: restores.log and the restore lock always live in MailArchive's own
// settings folder (§7.4 foldcheck M4).

type restoreTBOpts struct {
	out, profile, profileName, store, name, recordOut *string
	folders                                           stringSlice
	plan, yes, notEncryptedOK, listProfiles           *bool
}

func restoreTBFlags(fs *flag.FlagSet) *restoreTBOpts {
	o := &restoreTBOpts{}
	o.out = fs.String("out", "", "the archive folder to restore from")
	o.profile = fs.String("profile", "", "the Thunderbird profile FOLDER to restore into (default: the one MailArchive picks, named in the plan)")
	o.profileName = fs.String("profile-name", "", "the Thunderbird profile to restore into, by its NAME (see -list-profiles)")
	fs.Var(&o.folders, "folder", "a folder to restore, as the archive spells it (\"Inbox/Receipts\"), with its subfolders; repeatable (default: the whole archive)")
	o.store = fs.String("store", "", "with -folder: which part of the archive, when two have a folder of that name")
	o.name = fs.String("name", "", "the name after \"Restored — \" (default: the archive folder's name)")
	o.plan = fs.Bool("plan", false, "print the plan and stop; nothing is written")
	o.yes = fs.Bool("yes", false, "restore without asking (needed without a terminal)")
	o.recordOut = fs.String("record-out", "", "also save the plan and the result in this new FILE")
	o.notEncryptedOK = fs.Bool("not-encrypted-ok", false, "for an encrypted archive without a terminal: you understand the restored mail will NOT be encrypted (with -yes)")
	o.listProfiles = fs.Bool("list-profiles", false, "list this computer's Thunderbird profiles and stop")
	return o
}

func runRestoreThunderbird(args []string) error {
	fs := flag.NewFlagSet("mailarchive restore-thunderbird", flag.ContinueOnError)
	fs.Usage = restoreTBUsage(fs)
	o := restoreTBFlags(fs)
	k := readKeyFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	return restoreTBWith(stdAsker(), o, k, fs, os.Stdout, os.Stderr, nil)
}

// Seams: the engine's run (a test stops it through a signal at a step), how often
// progress is said, and the profiles listed.
var (
	restoreTBRun       = tbengine.Run
	restoreTBEvery     = 3 * time.Second
	restoreTBProfiles  = tbrestore.ListProfiles
	restoreTBNotifySig = func(c chan<- os.Signal) { signal.Notify(c, os.Interrupt, syscall.SIGTERM) }
)

// restoreTBWith is restore-thunderbird after its flags. sigs, when not nil, stands for the
// process's Ctrl-C and terminate signals.
func restoreTBWith(a asker, o *restoreTBOpts, k *keyFlags, fs *flag.FlagSet, stdout, stderr io.Writer, sigs chan os.Signal) error {
	if *o.listProfiles {
		return listTBProfiles(stdout)
	}
	if *o.out == "" {
		return errors.New("-out is required (the archive folder to restore from)")
	}
	if *o.profile != "" && *o.profileName != "" {
		return errors.New("choose either -profile FOLDER or -profile-name NAME, not both")
	}
	if *o.store != "" && len(o.folders) == 0 {
		return errors.New("-store is for use with -folder: it says which part of the archive a folder is in")
	}
	attended := a.attended()
	// Refused before anything is read: without a terminal nothing can be asked (§10).
	if !*o.plan && !attended && !*o.yes {
		return errors.New("restore-thunderbird asks before it restores, and there is no terminal to ask at, so nothing was restored: run it at a terminal, or add -yes (and -plan first to see what it will do)")
	}
	out := abspath(*o.out)
	var keys *keyring.Unlocked
	if kerr := app.RefuseKeyless("restore from it", out); kerr != nil {
		if !app.UnlockableAt(out) {
			return keylessRefusal(kerr, out)
		}
		if !*o.plan && !attended && !*o.notEncryptedOK {
			return fmt.Errorf("%s is an encrypted archive and the restored mail will NOT be encrypted; without a terminal that needs -yes -not-encrypted-ok, so nothing was restored", out)
		}
		if err := readKeyAvailable(a, out, "restoring it into Thunderbird", k); err != nil {
			return err
		}
		if err := refuseBusy(out); err != nil {
			return err
		}
		u, err := unlockForRead(a, out, "restoring it into Thunderbird", k)
		if err != nil {
			return interrupted(err, stderr)
		}
		defer u.Destroy()
		keys = u
	} else {
		if err := refuseReadKeyFlags(fs); err != nil {
			return err
		}
		if *o.notEncryptedOK {
			return errors.New("-not-encrypted-ok: for an encrypted archive only, and this archive is not encrypted")
		}
	}
	req := tbengine.Request{Out: out, Keys: keys, Profile: tbengine.ProfileChoice{Name: *o.profileName},
		FolderPaths: o.folders, Store: *o.store, Name: *o.name, Inputs: tbengine.ArchiveInputs(out)}
	if *o.profile != "" {
		req.Profile.Dir = abspath(*o.profile)
	}
	plan, err := tbengine.MakePlan(req)
	if err != nil {
		return err
	}
	fmt.Fprintln(stderr, plan.Text())
	if *o.plan {
		if *o.recordOut != "" {
			if err := saveTBRecord(*o.recordOut, plan.Text()); err != nil {
				return err
			}
		}
		return nil
	}
	if plan.AskTwice() && !attended {
		return fmt.Errorf("Thunderbird's Local Folders may leave this computer (%s), and the restored mail will not be encrypted; that is asked at a terminal only, so nothing was restored. Run it at a terminal, or choose a profile on this computer's own drive", strings.Join(plan.Leaves, "; "))
	}
	start := tbengine.Start{NotEncryptedOK: !plan.Encrypted || *o.notEncryptedOK}
	if attended && !*o.yes {
		q := "Restore?"
		if plan.Encrypted {
			q = "The restored mail will not be encrypted. Restore it?"
		}
		ok, err := askYes(a, q, false)
		if err != nil {
			return interrupted(err, stderr)
		}
		if !ok {
			return errors.New("nothing was restored")
		}
		start.NotEncryptedOK = true
	}
	if plan.AskTwice() {
		typed, err := a.line("The restored mail may leave this computer. To restore anyway, type the Local Folders path exactly as shown above: ")
		if err != nil {
			return interrupted(err, stderr)
		}
		if strings.TrimSpace(typed) != plan.Local.Dir {
			return errors.New("the path typed does not match; nothing was restored")
		}
		start.ConfirmAgain = true
	}

	if sigs == nil {
		sigs = make(chan os.Signal, 1)
		restoreTBNotifySig(sigs)
		defer signal.Stop(sigs)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stopCode atomic.Int32
	stopWatch := watchVerifyStop(sigs, cancel, &stopCode, os.Exit)
	defer stopWatch()

	fmt.Fprintln(stderr, tbrestore.WaitWords())
	var last time.Time
	start.Progress = func(g tbengine.Progress) {
		if now := time.Now(); last.IsZero() || now.Sub(last) >= restoreTBEvery || g.Done == g.Total {
			last = now
			line := fmt.Sprintf("Restoring: %s of %s messages", commas(g.Done), commas(g.Total))
			if g.Folder != "" {
				line += " (" + g.Folder + ")"
			}
			fmt.Fprintln(stderr, line)
		}
	}
	res, err := restoreTBRun(ctx, plan, start)
	if err != nil {
		if c := stopCode.Load(); c != 0 && tbrestore.RefusalCode(err) == "W-STOPPED" {
			fmt.Fprintln(stderr, "mailarchive: "+err.Error())
			return &exitError{code: int(c)}
		}
		return err
	}
	fmt.Fprintln(stdout, res.Words)
	if *o.recordOut != "" {
		if err := saveTBRecord(*o.recordOut, plan.Text()+"\n\n"+res.Words); err != nil {
			fmt.Fprintln(stderr, "mailarchive: the record was not saved: "+err.Error())
		}
	}
	if res.Stopped() {
		code := int(stopCode.Load())
		if code == 0 {
			code = 130
		}
		return &exitError{code: code}
	}
	if code := res.ExitCode(); code != 0 {
		return &exitError{code: code}
	}
	return nil
}

// listTBProfiles prints this computer's Thunderbird profiles, one a line.
func listTBProfiles(stdout io.Writer) error {
	list, err := restoreTBProfiles()
	if err != nil {
		return err
	}
	if len(list.Profiles) == 0 {
		return tbrestore.NoProfileRefusal()
	}
	for _, p := range list.Profiles {
		line := p.Name + " — " + p.Dir
		if p.OpensByDefault {
			line += " (Thunderbird opens it by default)"
		}
		if !p.Usable {
			line += " (not usable: " + p.Unusable + ")"
		}
		fmt.Fprintln(stdout, line)
	}
	return nil
}

// saveTBRecord saves the record in a new file only (never over one), readable by its owner.
func saveTBRecord(path, text string) error {
	f, err := os.OpenFile(abspath(path), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("-record-out %s: %w (it is saved only as a new file)", path, err)
	}
	if _, err := f.WriteString(text + "\n"); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// commas is n with thousands separators.
func commas(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

func restoreTBUsage(fs *flag.FlagSet) func() {
	return func() {
		fmt.Fprintf(os.Stderr, `mailarchive restore-thunderbird - put an archive's mail back into Thunderbird

Usage:
  mailarchive restore-thunderbird -out DIR [-profile FOLDER | -profile-name NAME]
      [-folder PATH]... [-store LABEL] [-name NAME] [-plan] [-yes] [-record-out FILE]
      [-not-encrypted-ok] [-password-file FILE | -recovery-key-file FILE]
  mailarchive restore-thunderbird -list-profiles

Writes the archive (or the folders named with -folder) into ONE new folder,
"Restored — NAME", under the Thunderbird profile's Local Folders: one mbox
file per folder, each message the archive's preserved original. It never
changes anything already in the profile and never starts or quits Thunderbird:
close Thunderbird first. The plan is shown first (on stderr); at a terminal it
asks "Restore? [y/N]"; without one it needs -yes. An encrypted archive is
unlocked as extract does; its restored mail is NOT encrypted, so without a
terminal it needs -yes -not-encrypted-ok, and a Local Folders that may leave
this computer (synced, removable, a network share) is used only when its path
is typed back at a terminal.

Exit: 0 every chosen message restored; 3 partial (some could not go back, each
reason said); 1 not restored (a refusal; the words name anything left behind);
130 or 143 stopped by Ctrl-C or a terminate signal.

It holds the archive's lock while it writes, so a scheduled backup that fires
meanwhile is skipped and runs at its next time. It cannot be scheduled.

Flags:
`)
		fs.PrintDefaults()
	}
}
