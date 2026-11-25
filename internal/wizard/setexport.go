package wizard

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/ncruces/zenity"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/tbcomplete"
	"mail-archive-tool/internal/util"
)

// Making a set's archives (docs/design-per-mailbox-archives.md §8 to §11, revision 2.2; step 3b of
// its build order): after the plan, the download question for each account that gets a new archive,
// the shared answers once, each archive's export in the plan's order, one summary, and the schedule
// once for the set.

// Made is one archive of a set, in the plan's order: a folder the wizard made or continued, or one it
// was to make or continue and did not start (§10, revision 2's C11). MailArchive Desktop's
// AddArchives (step 4) registers each Folder that holds an archive and names each one that did not
// finish. A person who backed out before any folder was made gets none.
type Made struct {
	// Folder is the archive's folder, an absolute path; "" for a new archive whose folder was not
	// made (not started, or its make failed).
	Folder string
	// Name is the archive's name as the summary says it: its folder's final name in the parent (for a
	// new archive not made, the name it was to have).
	Name string
	// New: its folder was made in this run. Otherwise it was continued: brought up to date, or a
	// folder an earlier run made and never finished.
	New     bool
	Outcome MadeOutcome
	// Reason is why it did not finish (MadeStopped, MadeFailed): one line, the summary's.
	Reason string
	// Schedule is what became of its schedule (§11); ScheduleWhy is why its install failed.
	Schedule    MadeSchedule
	ScheduleWhy string
	// Parent is the set's folder, which holds every archive of the set: what the Desktop's notes
	// name (§10, revision 2's C8). "" for the archive of the one-archive path (AddArchives), which
	// says only its Folder, Name, Outcome and Reason.
	Parent string
}

// MadeSchedule is what became of one archive's schedule in a set (§11, revision 2's C6 and C11,
// revision 2.1's F-11).
type MadeSchedule int

const (
	// ScheduleNone: no schedule was installed for it in this run (the answer was No or Cancel, the
	// question was not asked, or it is saved once or did not finish), and it had none of its own.
	ScheduleNone MadeSchedule = iota
	// ScheduleInstalled: its own job file and scheduler entry were installed in this run.
	ScheduleInstalled
	// ScheduleKept: brought up to date with a job file that reads: its job and entry are as they were.
	ScheduleKept
	// ScheduleKeptUnreadable: its job file is there and cannot be read: never installed over (F-11).
	ScheduleKeptUnreadable
	// ScheduleFailed: its install failed (ScheduleWhy); the others went on.
	ScheduleFailed
	// ScheduleKeptOff: brought up to date with a job file that reads, but the scheduler has no entry
	// for it now (an install that failed after its job file was written, a power cut between the
	// two, an auto-pause): not scheduled, never installed over (C6), and the end note says so (the
	// adversarial pass's CON-01).
	ScheduleKeptOff
	// ScheduleKeptManual: brought up to date with a job file that reads, whose record says it is
	// backed up only when the person clicks Run now (MA-949): not scheduled, never installed over
	// (C6), and the end note says so (CON-01).
	ScheduleKeptManual
)

// MadeOutcome is what became of one archive of a set (§10).
type MadeOutcome int

const (
	// MadeFinished: its export finished.
	MadeFinished MadeOutcome = iota + 1
	// MadeStopped: stopped part way by Cancel; the mail copied so far is kept.
	MadeStopped
	// MadeFailed: ended with an error (Reason); the mail copied so far is kept.
	MadeFailed
	// MadeNotStarted: not started, since an earlier archive was stopped or met an error about the
	// parent folder.
	MadeNotStarted
)

// askSetDownloads asks the download question (§8, revision 2's C7; MA-1502) for each mailbox of the
// plan p that gets a new archive and whose account is asked about (setItem.ask: a Thunderbird IMAP
// folder a live account uses), in the plan's order, each named as the list showed it and numbered
// among them ("Account 1 of 3"). An archive brought up to date is not asked again (its default answer
// would export it again in full), nor is a row saved once. changed carries the accounts this run's
// Yes, prepare changed across the questions (MA-1267). It says which items were prepared: their
// archives run in Full mode.
func askSetDownloads(p setPlan, changed *[]string) (map[int]bool, error) {
	n := 0
	for _, it := range p.items {
		if it.ask {
			n++
		}
	}
	asks := prepAsks{n: n}
	prepared := map[int]bool{}
	for i, it := range p.items {
		if !it.ask {
			continue
		}
		ok, err := prepareThunderbirdGUI(it.row.path, asks.nextAs(it.row.listName()), changed)
		if err != nil {
			return nil, err
		}
		prepared[i] = ok
	}
	return prepared, nil
}

// setArchive is one archive of a set's run: its plan item and what became of it.
type setArchive struct {
	it       setItem
	prepared bool // its account was just prepared: it runs in Full mode (§8)
	made     Made
	res      app.Result
	log      string       // its run log, once its export opened one
	c        wizardChoice // the answers it was exported with, which its schedule repeats
	// notMade: a new archive whose folder was never made (or was taken away again), so nothing was
	// copied (the adversarial pass's CON-10, the friction review's F2); remedy is its own remedy,
	// when it has one.
	notMade bool
	remedy  string
}

// setOpen opens a folder for the summary's "Open the folder" (openPath in the product).
var setOpen = openPath

// makeSet is One archive for each mailbox, from its parent folder to its end (§6 to §11): the parent
// folder and the plan (chooseSet), then runSet. Cancel before any folder is made returns no Made and
// the dialog's error (zenity.ErrCanceled). Each Made carries the set's parent folder (§10, C8). The
// set lock beside the made-list (job.HoldSetLock) is held from before the folder question to the end
// (the adversarial pass's CON-03): a second window's set is refused at once with job.ErrSetBusy,
// before it asks anything, so no two plans are made at once and no mailbox gets two archives.
func makeSet(rows []setRow, goos string) ([]Made, error) {
	if f, err := job.MadeListFile(); err == nil {
		release, err := job.HoldSetLock(f)
		if err != nil {
			return nil, err
		}
		defer release()
	}
	ch, err := chooseSet(rows, goos)
	if err != nil {
		return nil, err
	}
	made, err := runSet(ch, goos)
	for i := range made {
		made[i].Parent = ch.plan.parent
	}
	return made, err
}

// runSet makes the set the person agreed to in ch (§8, §9): the download question for each account
// that gets a new archive, the shared answers once (Full mode for a just-prepared account's archive
// only), then each archive one after another in the plan's order through today's export steps with
// its one input and auto-discovery off, a new one's folder made just before its own export and
// recorded in the made-list first; then the one summary; then last-archive.txt once (C22). It returns
// each archive made or continued, or not started, in the plan's order, also with an error.
func runSet(ch setChosen, goos string) ([]Made, error) {
	p := ch.plan
	var changed []string
	prepared, err := askSetDownloads(p, &changed)
	if err != nil {
		return nil, err
	}
	var arcs []*setArchive
	all := wizardChoice{}
	allPrepared := true
	for i, it := range p.items {
		switch it.outcome {
		case outNew, outUnfinished, outContinue:
			arcs = append(arcs, &setArchive{it: it, prepared: prepared[i]})
			all.inputs = append(all.inputs, it.row.path)
			allPrepared = allPrepared && prepared[i]
		}
	}
	if len(arcs) > 0 {
		if err := setOutlookNote(all.inputs); err != nil {
			return nil, err
		}
		// Asked once for all the archives, as today and with today's defaults; the mode question is
		// skipped only when every archive's account was just prepared.
		if err := askAnswers(&all, srcAuto, allPrepared); err != nil {
			return nil, err
		}
	}
	if err := exportSet(ch.parent, p, arcs, all, goos); err != nil {
		return madeOf(arcs), err
	}
	// Nothing ran at all (every mailbox paused, kept, or not made): no summary (the friction review's
	// F11; the plan said it all).
	if len(arcs) > 0 {
		if err := showSetSummary(p.parent, arcs); err != nil {
			return madeOf(arcs), err
		}
	}
	err = offerSetSchedule(p, arcs)
	// Once, after the schedule step (C22), whatever its answer.
	rememberSet(arcs)
	return madeOf(arcs), err
}

// setOutlookNote shows today's guidance for an Outlook .ost once, when a set's mailboxes hold one.
func setOutlookNote(inputs []string) error {
	for _, in := range inputs {
		if fi, err := os.Stat(in); err == nil && !fi.IsDir() && strings.EqualFold(filepath.Ext(in), ".ost") {
			return outlookOfflineNote()
		}
	}
	return nil
}

// madeOf is each archive's Made, in the plan's order.
func madeOf(arcs []*setArchive) []Made {
	out := make([]Made, 0, len(arcs))
	for _, a := range arcs {
		out = append(out, a.made)
	}
	return out
}

// exportSet exports each archive of arcs in turn (§9). A new archive's folder is made just before its
// own export (makeSetArchiveFolder, which records it in the made-list first). Cancel on a progress
// window stops that archive, which keeps what it copied, and starts no more. An error in one goes on
// to the next, except an error about the parent folder itself (it cannot be written, is full, or is
// gone; C29), which stops the set as Cancel does. A progress window that cannot be opened ends the set
// with its error.
func exportSet(sp *app.SetParent, p setPlan, arcs []*setArchive, all wizardChoice, goos string) error {
	stop := false
	m := len(arcs)
	for k, a := range arcs {
		a.made = Made{Name: a.it.folder}
		path := filepath.Join(p.parent, a.it.folder)
		if a.it.outcome != outNew {
			a.made.Folder = path
		}
		if stop {
			a.made.Outcome = MadeNotStarted
			continue
		}
		id := a.it.id
		if a.it.outcome == outNew {
			made, name, madeID, err := makeSetArchiveFolder(sp, p, a.it, goos)
			a.made.Name, id = name, madeID
			if err != nil {
				// Its folder was never made, or was taken away again: nothing was copied (CON-10).
				a.notMade = true
			}
			var r *app.CopyRefusal
			switch {
			case errors.Is(err, errSetFolderChanged):
				// Made, then not the folder made by the time it was read (INS-02): never written into.
				a.made.Outcome, a.made.Reason = MadeFailed, setChangedReason(filepath.Join(p.parent, name))
				continue
			case errors.Is(err, errNotRecorded):
				// Made, but not in the made-list, which the unfinished-folder rule and MailArchive
				// Desktop read: not exported into, and the empty folder taken away again, so a re-run
				// makes it afresh under its own name.
				if !removeEmptySetFolder(p.parent, name) {
					a.made.Folder = made
				}
				a.made.Outcome, a.made.Reason = MadeFailed, firstLine(err)
				continue
			case errors.Is(err, errNoFreeNumber):
				a.made.Outcome, a.made.Reason = MadeFailed, fmt.Sprintf("this folder already holds folders named %s (2) to (%d)", util.StripControl(a.it.row.name), setNumberMax)
				continue
			case errors.As(err, &r) && r.ID == "R-w":
				// This computer did not let MailArchive write in the parent (F2's words and remedy, which
				// the summary cut off before).
				a.made.Outcome, a.made.Reason = MadeFailed, "this computer did not let MailArchive make a folder in "+util.StripControl(p.parent)
				a.remedy = setWriteRemedy(runtime.GOOS)
				stop = true
				continue
			case err != nil:
				// About the parent itself (it is gone or changed, cannot be written, or is now inside an
				// archive): every later archive would meet it too.
				a.made.Outcome, a.made.Reason = MadeFailed, setParentReason(err, p.parent, name)
				stop = true
				continue
			}
			a.made.Folder, a.made.New, path = made, true, made
		}
		c := all
		c.inputs, c.auto, c.out = []string{a.it.row.path}, false, path
		c.followLinks = nil
		for _, f := range all.followLinks {
			if f == a.it.row.path {
				c.followLinks = []string{f}
			}
		}
		if a.prepared {
			c.mode = export.Full
		}
		a.c = c
		// The folder the plan read or the set made, proved again through the parent's handle right
		// before the log opens and again right before the export starts (INS-02).
		folderName := filepath.Base(path)
		same := func() error {
			if setChildSame(p.parent, p.parentID, folderName, id) {
				return nil
			}
			return errors.New(setChangedReason(path))
		}
		res, logPath, stopped, runErr, dlgErr := exportSetArchive(c, k+1, m, same)
		a.res, a.log = res, logPath
		switch {
		case dlgErr != nil:
			a.made.Outcome = MadeNotStarted
			for _, b := range arcs[k+1:] {
				b.made = Made{Name: b.it.folder, Outcome: MadeNotStarted}
				if b.it.outcome != outNew {
					b.made.Folder = filepath.Join(p.parent, b.it.folder)
				}
			}
			return dlgErr
		case runErr == nil:
			a.made.Outcome = MadeFinished
			stop = stopped // closed just as it finished: it is whole, and no more start
		case errors.Is(runErr, context.Canceled):
			a.made.Outcome, a.made.Reason = MadeStopped, setCancelled
			stop = true
		default:
			a.made.Outcome, a.made.Reason = MadeFailed, firstLine(runErr)
			stop = setParentProblem(p.parent, runErr)
		}
	}
	return nil
}

// setCancelled is the reason of an archive stopped by Cancel on its progress window.
const setCancelled = "the export was cancelled"

// errNoFreeNumber is makeSetArchiveFolder's error when every number to setNumberMax is taken.
var errNoFreeNumber = errors.New("no free number is left for the folder's name")

// makeSetArchiveFolder makes a new archive's folder just before its export (§9, revision 2's C5),
// through makeSetFolder (which records it in the made-list right after): named as the plan named it,
// or, when that name is found taken only now (something appeared there since the plan; C18), the
// plan's name before any number with the next free number, as the plan numbers names. It returns the
// folder made and its final name. Its errors: errNotRecorded (made, not recorded; the path is
// returned), errNoFreeNumber, or makeSetFolder's refusal, which is about the parent folder.
func makeSetArchiveFolder(sp *app.SetParent, p setPlan, it setItem, goos string) (string, string, os.FileInfo, error) {
	name := it.folder
	for try := 0; try <= setNumberMax; try++ {
		path, id, err := makeSetFolderAt(sp, name, it.row.name, state.PathSourceID(it.row.path))
		if !setNameTaken(err, name) {
			return path, name, id, err
		}
		next := setFreeName(p, it, goos)
		if next == "" || next == name {
			return "", name, nil, errNoFreeNumber
		}
		name = next
	}
	return "", name, nil, errNoFreeNumber
}

// setNameTaken reports whether err is MakeSetFolder's "exists": name is one this computer can use as
// a folder's, and something is already there under it.
func setNameTaken(err error, name string) bool {
	var r *app.CopyRefusal
	return errors.As(err, &r) && r.ID == "R-q" && app.FolderNameWhy(name) == ""
}

// setFreeName is the name for the new archive of it when the plan's name is taken at make time
// (C18): its name before any number, with the first number from 2 that neither another of the plan's
// folders nor anything in the parent now has, told apart as the plan tells names apart (setNamesUsed);
// "" when none is free.
func setFreeName(p setPlan, it setItem, goos string) string {
	used := setNamesUsed{transcoded: answerMayBeTranscoded(goos), keys: map[string]bool{}, ascii: map[string]bool{}}
	for _, o := range p.items {
		if o.folder != "" {
			used.add(o.folder)
		}
		for _, a := range o.also {
			used.add(a)
		}
	}
	root, err := os.OpenRoot(p.parent)
	if err != nil {
		return ""
	}
	defer root.Close()
	if d, err := root.Open("."); err == nil {
		names, _ := d.Readdirnames(setEntryMax)
		d.Close()
		for _, n := range names {
			used.add(n)
		}
	}
	return used.free(it.row.name, func(n string) bool {
		_, err := root.Lstat(n)
		return !errors.Is(err, fs.ErrNotExist)
	})
}

// removeEmptySetFolder takes away the empty folder name a set just made in parent and could not
// record, through the parent's os.Root (a folder that is not empty is never removed); it reports
// whether the folder is gone.
func removeEmptySetFolder(parent, name string) bool {
	root, err := os.OpenRoot(parent)
	if err != nil {
		return false
	}
	defer root.Close()
	return root.Remove(name) == nil
}

// exportSetArchive runs one archive of a set through today's export steps, as runExport does: the
// unfinished-delete and keyless refusals before anything is written, the run log in the archive's
// folder, app.Run, with its one input and auto-discovery off. Its progress window is titled
// "Exporting n of m…" and shows today's counts line, never a name (§9). stopped says the person closed
// the window (Cancel): the archive keeps what it copied, and the set starts no more. logPath is the
// run log's, once it was opened. dlgErr is a progress window that could not be opened. same proves
// the archive's folder is still the one the plan read or the set made (setChildSame, INS-02); it is
// asked right before the log opens and again right before the export starts, and a folder that is
// not the same is never written into.
func exportSetArchive(c wizardChoice, n, m int, same func() error) (res app.Result, logPath string, stopped bool, runErr, dlgErr error) {
	// Every archive of a set has its folder before its export (made just now, or continued): one gone
	// since (its parent unplugged or moved) is never made again here, as the run log and app.Run would
	// make a missing folder, so no archive lands in a decoy folder.
	if err := setFolderThere(c.out); err != nil {
		return res, "", false, err, nil
	}
	if err := app.RefuseUnfinishedDelete(c.out, ""); err != nil {
		return res, "", false, err, nil
	}
	if err := app.RefuseKeyless("archive into it", c.out); err != nil {
		return res, "", false, err, nil
	}
	if err := same(); err != nil {
		return res, "", false, err, nil
	}
	logger := newLogger(c.out)
	logPath = filepath.Join(c.out, "mailarchive.log")

	dlg, err := progressDialog(fmt.Sprintf("Exporting %d of %d…", n, m))
	if err != nil {
		return res, logPath, false, nil, err
	}
	if err := setFolderThere(c.out); err != nil {
		_ = dlg.Close()
		return res, logPath, false, err, nil
	}
	if err := same(); err != nil {
		_ = dlg.Close()
		return res, logPath, false, err, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Cancelling the progress window cancels this archive's export.
	go func() {
		<-dlg.Done()
		cancel()
	}()

	var since time.Time
	if c.since != "" {
		since, _ = util.ParseSince(c.since, time.Now())
	}
	opts := app.Options{
		Inputs:    c.inputs,
		Auto:      false,
		Out:       c.out,
		Mode:      c.mode,
		Since:     since,
		CopyFirst: c.copyFirst,
		Index:     true,
		Pages:     true,
		KeepRaw:   c.keepRaw,

		FollowLinks: c.followLinks,
	}
	var ticks int
	res, runErr = app.Run(ctx, opts, logger, func(s export.Stats) {
		ticks++
		if ticks%20 == 0 {
			_ = dlg.Text(progressCounts(s))
		}
	})
	select {
	case <-dlg.Done():
		stopped = true
	default:
	}
	_ = dlg.Complete()
	_ = dlg.Close()
	return res, logPath, stopped || errors.Is(runErr, context.Canceled), runErr, nil
}

// setChangedReason is the summary's reason for an archive whose folder at path is no longer the one
// the plan read or the set made (INS-02).
func setChangedReason(path string) string {
	return fmt.Sprintf("its folder %s is no longer the folder MailArchive checked (it was moved, or something else was put in its place), so nothing was written into it", util.StripControl(path))
}

// setFolderThere refuses an archive folder of a set that is not there any more (or is no longer a
// folder): its export would make it again.
func setFolderThere(out string) error {
	if fi, err := os.Stat(out); err != nil || !fi.IsDir() {
		return fmt.Errorf("its folder %s is not there any more, so nothing was copied into it", util.StripControl(out))
	}
	return nil
}

// setParentReason is a refusal about the set's parent folder, err, as a summary line's reason: the
// first sentence of the wizard's words for it (setParentWords), whose advice to choose another folder
// is the parent question's, not the summary's.
func setParentReason(err error, parent, name string) string {
	w := setParentWords(err, parent, name)
	from := 0
	if p := util.StripControl(parent); p != "" {
		if i := strings.LastIndex(w, p); i >= 0 {
			from = i + len(p)
		}
	}
	if j := strings.Index(w[from:], ". "); j >= 0 {
		w = w[:from+j]
	}
	return strings.TrimSuffix(w, ".")
}

// setParentProblem reports whether err, an archive's error, is about the set's parent folder itself
// (C29): the parent is gone or no longer a folder, or the disk is full, so every later archive would
// meet it too.
func setParentProblem(parent string, err error) bool {
	if fi, serr := os.Stat(parent); serr != nil || !fi.IsDir() {
		return true
	}
	return diskFull(err)
}

// diskFull reports whether err says the disk is full: ENOSPC, or Windows' ERROR_HANDLE_DISK_FULL (39)
// and ERROR_DISK_FULL (112).
func diskFull(err error) bool {
	if errors.Is(err, syscall.ENOSPC) {
		return true
	}
	var en syscall.Errno
	return runtime.GOOS == "windows" && errors.As(err, &en) && (en == 39 || en == 112)
}

// firstLine is err's first line, control characters dropped and its last full stop left off, for a
// summary line that adds its own.
func firstLine(err error) string {
	line, _, _ := strings.Cut(err.Error(), "\n")
	return strings.TrimSuffix(strings.TrimSpace(util.StripControl(line)), ".")
}

// rememberSet writes last-archive.txt once for the set, after the schedule step (§9, C22): its first
// scheduled archive (installed in this run, or keeping its own schedule that reads), or, with none,
// its first finished archive that is not saved once; nothing when there is neither.
func rememberSet(arcs []*setArchive) {
	for _, a := range arcs {
		if a.made.Schedule == ScheduleInstalled || a.made.Schedule == ScheduleKept {
			rememberArchive(a.made.Folder)
			return
		}
	}
	for _, a := range arcs {
		if a.made.Outcome == MadeFinished && !a.it.row.once {
			rememberArchive(a.made.Folder)
			return
		}
	}
}

// The summary's words (§9, revision 2's C21, revision 2.1's F-10c, R1).
const (
	setSummaryOpen = "Open the folder"
	// setSummaryEnd is R1's sentences after every archive's line.
	setSummaryEnd = "Each archive is its own folder: you can hand over or delete one without touching the others. Open index.html in any of them to browse. MailArchive Desktop lists them all under Your archives; a row whose last backup did not finish says so."
	// setUnfinishedEnd follows the reason of an archive that did not finish, whose folder was made.
	// Round B (the friction review's F4) moved "run this again with the same folder to finish it",
	// which said nothing of how, to one closing sentence (setFinishOne, setFinishMany): it read "The
	// mail copied so far is kept; run this again with the same folder to finish it.".
	setUnfinishedEnd = "The mail copied so far is kept."
	// setNotStarted follows the name of an archive that was not started (F4: it read "Not started. Run
	// this again with the same folder to back it up.").
	setNotStarted = "Not started."
	// setNotMadeLead leads the line of an archive whose folder was never made, so nothing was copied
	// (the adversarial pass's CON-10, the friction review's F2; the builder's form of F2's words).
	setNotMadeLead = "Not made, so nothing was copied: "
	// setFinishOne and setFinishMany close the archives' lines when any did not finish, was not
	// made or was not started (F4's words).
	setFinishOne  = "To finish it, run this again: choose All of them, then One archive for each mailbox, and this same folder."
	setFinishMany = "To finish them, run this again: choose All of them, then One archive for each mailbox, and this same folder."
	// setLaterMailbox is the summary's and the end note's last line (the friction review's F1).
	setLaterMailbox = "To back up a mailbox you add to Thunderbird later: click Make a new archive…, choose All of them, then One archive for each mailbox, and this same folder, %s."
	setOnceLead     = "saved once."
)

// setWriteRemedy is the remedy of a folder this computer did not let MailArchive make (F2's words,
// with each system's own setting as the wizard's refusal of the parent names it).
func setWriteRemedy(goos string) string {
	switch goos {
	case "windows":
		return "Allow MailArchive there (Windows Security, Controlled folder access), or run this again and choose another folder."
	case "darwin":
		return "Allow MailArchive there (System Settings, Privacy and Security, Files and Folders), or run this again and choose another folder."
	}
	return "Allow MailArchive to write there, or run this again and choose another folder."
}

// setSummaryText is the set's one summary (§9): the lead counts the archives that finished, with
// singular and zero forms ("1 archive is ready in …", "No archive was finished in …"); then each
// archive's line in the plan's order, by its folder's final name ("(brought up to date)" after one
// continued), with that archive's lead (tbcomplete.RunLead's, or "saved once." for a row saved once),
// how many messages it holds and the counts the one-archive summary shows with their remedies (only
// those above zero), then its Thunderbird account lines (none for a row saved once); an archive that
// did not finish says why (with its log, when there is one), and
// one not started says so; then R1's sentences.
func setSummaryText(parent string, arcs []*setArchive) string {
	finished := 0
	for _, a := range arcs {
		if a.made.Outcome == MadeFinished {
			finished++
		}
	}
	where := util.StripControl(parent)
	var head string
	switch finished {
	case 0:
		head = "No archive was finished in " + where
	case 1:
		head = "1 archive is ready in " + where
	default:
		head = fmt.Sprintf("%d archives are ready in %s", finished, where)
	}
	var lines []string
	unfinished := 0
	for _, a := range arcs {
		label := util.StripControl(a.made.Name)
		if a.it.outcome == outContinue {
			label += " (brought up to date)"
		}
		switch a.made.Outcome {
		case MadeFinished:
			// A row saved once (a folder no account uses now, an older copy) says "saved once." and no
			// account lines: no live account downloads into it (the design's example).
			lead := tbLeadSentence(a.res)
			if a.it.row.once {
				lead = setOnceLead
			}
			parts := append([]string{label + ": " + lead, messages(a.res.ManifestSize) + "."}, setCountSentences(a.res)...)
			lines = append(lines, strings.Join(parts, " "))
			if !a.it.row.once {
				lines = append(lines, setTBLines(a.res)...)
			}
		case MadeStopped, MadeFailed:
			unfinished++
			if a.notMade {
				line := label + ": " + setNotMadeLead + a.made.Reason + "."
				if a.remedy != "" {
					line += " " + a.remedy
				}
				lines = append(lines, line)
				continue
			}
			line := label + ": Did not finish: " + a.made.Reason + ". " + setUnfinishedEnd
			if _, err := os.Stat(a.log); a.made.Outcome == MadeFailed && a.log != "" && err == nil {
				line += " Log: " + util.StripControl(a.log)
			}
			lines = append(lines, line)
		case MadeNotStarted:
			unfinished++
			lines = append(lines, label+": "+setNotStarted)
		}
	}
	switch {
	case unfinished == 1:
		lines = append(lines, setFinishOne)
	case unfinished > 1:
		lines = append(lines, setFinishMany)
	}
	parts := []string{head}
	if len(lines) > 0 {
		parts = append(parts, strings.Join(lines, "\n"))
	}
	parts = append(parts, setSummaryEnd, fmt.Sprintf(setLaterMailbox, where))
	return strings.Join(parts, "\n\n")
}

// tbLeadSentence is the lead of one archive's run (its weakest Thunderbird account's, as the
// one-archive summary's): "Backup complete.", "Backup finished." or "Backed up what Thunderbird has so
// far.".
func tbLeadSentence(r app.Result) string { return tbcomplete.RunLead(r.Thunderbird).Sentence() }

// setTBLines are one archive's Thunderbird account lines, as the one-archive summary says them
// (state.TBLines: with two or more accounts behind, their remedy once).
func setTBLines(r app.Result) []string {
	if accts, more := app.TBAccounts(r); len(accts) > 0 || more > 0 {
		return state.TBLines(accts, more, state.TBSay{})
	}
	return nil
}

// setCountSentences are the counts the one-archive summary (exportSummaryFor) shows for r, each with
// its remedy, in its words, one sentence each and only those above zero (§9, C21; UX contract X6):
// not fully downloaded yet (or cut short in the mailbox file), empty at the source, kept as received,
// kept as an earlier version read them, where the details are, and the messages search could not
// index. TestASetsSummaryKeepsEachArchivesCounts holds each one to exportSummaryFor's words.
func setCountSentences(r app.Result) []string {
	var out []string
	add := func(s string) {
		if !strings.HasSuffix(s, ".") {
			s += "."
		}
		out = append(out, s)
	}
	notDone := r.Fillable + r.Unknown
	switch {
	case r.Fillable > 0 && r.FillableOnlyParseGap >= r.Fillable:
		add(fmt.Sprintf("Cut short, or with parts that could not be told apart, in the mailbox file: %d — a download does not change a mailbox file: replace it with a complete export from your mail app, then run this again to add them (an IMAP cache: download the messages for offline use instead). A message that was malformed when it was sent cannot be completed by either, and stays listed", r.Fillable))
		add(fmt.Sprintf("%d of them %s also counted under \"Kept as received\" below", r.FillableParseGap, guiVerb(r.FillableParseGap, "is", "are")))
		if r.Unknown > 0 {
			add(fmt.Sprintf("Not yet re-checked: %d (from an older archive; re-checked automatically each run)", r.Unknown))
		}
	case notDone > 0:
		line := fmt.Sprintf("Not fully downloaded yet: %d — open these in your mail app so it downloads them, then run this again to add them", notDone)
		if r.Unknown > 0 {
			line += fmt.Sprintf(" (%d are from an older archive and get re-checked automatically each run)", r.Unknown)
		}
		add(line)
		if r.FillableParseGap > 0 {
			add(fmt.Sprintf("%d of them %s cut short, or %s parts that could not be told apart, in the mailbox file: a download does not change it, so replace the file with a complete export from your mail app (the report says which)",
				r.FillableParseGap, guiVerb(r.FillableParseGap, "is", "are"), guiVerb(r.FillableParseGap, "has", "have")))
			add(fmt.Sprintf("%d of them %s also counted under \"Kept as received\" below", r.FillableParseGap, guiVerb(r.FillableParseGap, "is", "are")))
		}
	}
	if r.Terminal > 0 {
		add(fmt.Sprintf("Empty at the source (nothing to download): %d", r.Terminal))
	}
	if r.Undecodable > 0 {
		add(fmt.Sprintf("Kept as received, because this version cannot decode a part of them: %d (the files are in each message's attachments zip)", r.Undecodable))
	}
	if r.FidelityFallback > 0 {
		add(fmt.Sprintf("Kept as an earlier version read them, because this version could not decode them in full: %d (nothing is lost)", r.FidelityFallback))
	}
	if (notDone > 0 || r.Terminal > 0 || r.Undecodable > 0 || r.FidelityFallback > 0) && r.ReportPath != "" {
		out = append(out, "Details: "+util.StripControl(r.ReportPath)) // a path takes no full stop
	}
	if r.IndexErrors > 0 {
		add(fmt.Sprintf("WARNING: %d message(s) could not be indexed for search.", r.IndexErrors))
	}
	return out
}

// showSetSummary shows the set's summary through the question seam, with the buttons "Open the folder"
// (which opens the parent) and "Close" (§9).
func showSetSummary(parent string, arcs []*setArchive) error {
	err := questionDialog(setSummaryText(parent, arcs), zenity.Title(appTitle), zenity.OKLabel(setSummaryOpen), zenity.CancelLabel("Close"))
	switch {
	case err == nil:
		setOpen(parent)
	case !errors.Is(err, zenity.ErrCanceled):
		return err
	}
	return nil
}
