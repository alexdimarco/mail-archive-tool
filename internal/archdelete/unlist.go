package archdelete

import (
	"errors"
	"fmt"
	"strings"

	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/schedule"
)

// UnlistPlan is Remove from list's question (§3.2, §5.2 [C3]): the folder, the
// scheduler entries this computer's records find for it, and the question naming
// those that would be stopped.
type UnlistPlan struct {
	Folder string
	// Entries are every entry found; Named those the question names: an entry a job
	// file or a descriptor records, or one the scheduler has.
	Entries  []Entry
	Named    []string
	Question string
}

// UnlistDeps are Remove from list's seams: Deps.Records finds the entries (§5.2:
// this computer's records only, so it works with the drive unplugged).
type UnlistDeps struct {
	Deps      Deps
	Scheduler Scheduler
	Pointers  PointerStore
	// Named are the entries the question the person answered Yes to named
	// (UnlistPlan.Named): Unlist stops no entry the question did not name; one found
	// at Yes that it did not name refuses, nothing changed (§5.2 "the question names
	// them", conformance 10).
	Named []string
	// SettingsDir is ExecDeps.SettingsDir: where the Desktop keeps its settings, its
	// list of archives and the alert files ("" is job.Dir()).
	SettingsDir string
	// ForgetRow removes the row; nil removes every entry naming the folder from the
	// list in SettingsDir.
	ForgetRow func(row string) error
}

// UnlistResult is what Remove from list did.
type UnlistResult struct {
	// Removed: the row is gone.
	Removed bool
	Refusal *Refusal
	Lines   []string
	// Stopped are the entries this Remove from list stopped.
	Stopped []string
}

// ExitCode is the command line's exit code (§3.3): 0 removed from the list, 1
// refused or nothing changed, 3 stopped part way with something changed.
func (r *UnlistResult) ExitCode() int {
	switch {
	case r.Removed:
		return 0
	case r.Refusal == nil && len(r.Stopped) > 0:
		return 3
	}
	return 1
}

// PlanUnlist is Remove from list's question for the row (§3.2): §7's refusals
// first (UnlistRefusal), then the entries this computer's records find — derived
// names, job files under SameArchivePath, the descriptors' names when the folder
// can be read, and for the current archive the capture registered under BackupFor
// [NF-REMOVE-CURRENT-BACKUPFOR]. It changes nothing; it asks the scheduler only
// whether a derived name is installed.
func PlanUnlist(row string, x UnlistDeps) (*UnlistPlan, error) {
	if err := UnlistRefusal(row, x.Deps); err != nil {
		return nil, err
	}
	abs := cleanAbs(row)
	p := &DeletePlan{Folder: abs}
	if x.Deps.Records != nil {
		d, err := x.Deps.Records.Discover(abs)
		if err != nil {
			return nil, err
		}
		p.Schedules = d
	}
	addCaptureBackupFor(p, abs, x.Deps)
	q := &UnlistPlan{Folder: abs, Entries: p.Schedules.Entries}
	for _, e := range q.Entries {
		if e.From != "derived" || (x.Scheduler != nil && x.Scheduler.Query(e.Name) != schedule.NotInstalled) {
			q.Named = append(q.Named, e.Name)
		}
	}
	const tail = "Remove from list? The folder and its files are kept."
	switch len(q.Named) {
	case 0:
		q.Question = tail
	case 1:
		q.Question = fmt.Sprintf("Its scheduled backup %s will also be stopped. %s", q.Named[0], tail)
	default:
		q.Question = fmt.Sprintf("Its scheduled backups %s will also be stopped. %s", strings.Join(q.Named, ", "), tail)
	}
	return q, nil
}

// Unlist is Remove from list after the person said Yes (§5.2 [C3]): §7's refusals
// again; each entry stopped by step 2's absent rule (an absent one counts as
// stopped) — a failure changes nothing else and keeps the row; then the stopped
// names' settings files (step 3's list), BackupFor cleared when its entry was
// removed, last-archive.txt when it names the folder, Out when the folder is the
// current one (allowed only when it is unreachable or no longer an archive), and
// the row. The folder and everything in it is never touched.
func Unlist(row string, x UnlistDeps) *UnlistResult {
	res := &UnlistResult{}
	if x.Pointers == nil {
		res.Refusal = &Refusal{Case: CaseInternal, Message: "MailArchive could not remove this archive from the list: it was not given its settings. Nothing was changed."}
		res.Lines = []string{res.Refusal.Message}
		return res
	}
	q, err := PlanUnlist(row, x)
	if err != nil {
		r := AsRefusal(err)
		if r == nil {
			r = &Refusal{Case: CaseUnreadable, Message: cleanErr(err)}
		}
		res.Refusal, res.Lines = r, []string{r.Message}
		return res
	}
	if len(q.Entries) > 0 && x.Scheduler == nil {
		res.Refusal = &Refusal{Case: CaseInternal, Message: "MailArchive could not remove this archive from the list: it was not given the scheduler. Nothing was changed."}
		res.Lines = []string{res.Refusal.Message}
		return res
	}
	asked := map[string]bool{}
	for _, n := range x.Named {
		asked[n] = true
	}
	for _, n := range q.Named {
		if !asked[n] {
			res.Refusal = &Refusal{Case: CaseChanged, Message: fmt.Sprintf("This archive's scheduled backups changed since you were asked: %s was not named. Nothing was changed. Click Remove from list again.", n)}
			res.Lines = []string{res.Refusal.Message}
			return res
		}
	}
	goos := x.Deps.goos()
	var names, spellings []string
	backupFor := false
	for _, e := range q.Entries {
		absent, err := stopEntry(x.Scheduler, goos, e.Name)
		if err != nil {
			res.Lines = []string{stopEntryMessage(e.Name, err, res.Stopped, "click Remove from list again", "The row is kept.")}
			return res
		}
		if !absent {
			res.Stopped = append(res.Stopped, e.Name)
		}
		names = append(names, e.Name)
		if e.RegisteredFor != "" {
			backupFor = true
		}
		spellings = append(spellings, e.Out)
	}
	spellings = append([]string{q.Folder, row}, spellings...)
	if f, err := removeNameFiles(names, spellings, x.SettingsDir); err != nil {
		res.Lines = []string{fmt.Sprintf("Could not remove %s in MailArchive's settings folder (%s). The row is kept.", f, cleanErr(err))}
		return res.withStopped()
	}
	isThis := func(s string) bool {
		for _, sp := range spellings {
			if s != "" && sp != "" && sameFolder(s, sp, goos) {
				return true
			}
		}
		return false
	}
	ptr, err := x.Pointers.Load()
	if err == nil {
		np := ptr
		if isThis(ptr.LastArchive) {
			np.LastArchive = ""
		}
		if isThis(ptr.Out) {
			np.Out = ""
		}
		// BackupFor naming the folder itself is cleared too, so nothing re-adds the row
		// (§5.2; conformance 3).
		if backupFor || isThis(ptr.BackupFor) {
			np.BackupFor = ""
		}
		if np != ptr {
			err = x.Pointers.Save(np)
		}
	}
	if err != nil {
		res.Lines = []string{"Could not save MailArchive's settings, so the row is kept. Click Remove from list again."}
		return res.withStopped()
	}
	forget := x.ForgetRow
	if forget == nil {
		forget = func(row string) error { return forgetRowIn(x.SettingsDir, row, goos) }
	}
	if err := forget(row); err != nil {
		res.Lines = []string{msgRowFailed}
		var busy *archindex.BusyError
		switch {
		case errors.Is(err, ErrRowNotListed):
			res.Lines = []string{msgRowNotListed}
		case errors.As(err, &busy):
			// Another MailArchive program is changing the list (CON-07): say so, and to try again.
			res.Lines = []string{msgRowFailed + " " + busy.Error() + "."}
		}
		return res.withStopped()
	}
	res.Removed = true
	res.Lines = []string{"Removed from Your archives. The folder and its files are kept."}
	return res.withStopped()
}

func (r *UnlistResult) withStopped() *UnlistResult {
	for _, n := range r.Stopped {
		r.Lines = append(r.Lines, "Stopped the scheduled backup "+n)
	}
	return r
}
