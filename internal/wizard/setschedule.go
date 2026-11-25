package wizard

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"runtime"
	"strings"

	"github.com/ncruces/zenity"

	"mail-archive-tool/internal/backupalert"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/util"
)

// A set's schedule (docs/design-per-mailbox-archives.md §11, revision 2's C6, C11, C27, revision
// 2.1's F-10d and F-11; MA-1497): asked once for the set, after its summary; only an exact Yes row
// installs (MA-1474's rule); each archive made in this run, or brought up to date with no job file
// yet, gets its own job file and scheduler entry, 10 minutes apart from the time chosen; one brought
// up to date that has a job file keeps it and its entry as they are. No Thunderbird sync is offered or
// written for a set (the owner's answer 4).

// The set's schedule question (§11). The rows are one archive's (MA-1474).
const (
	setScheduleHead = "Keep these archives current automatically?\n\nEach archive gets its own scheduled backup. "
	// setApartFirst says the archives do not all run at once; setApartRerun is said instead on a
	// re-run, where an archive that keeps its own schedule keeps its time (F-10d).
	setApartFirst   = "They start 10 minutes apart, from the time you choose, so they do not all run at once. "
	setApartRerun   = "New archives start 10 minutes apart from the time you choose. "
	setScheduleRuns = "A scheduled run repeats exactly this export (incremental: only new mail and any content that was still missing). It runs only while you are signed in. "
	// setWake follows the missed-time sentence on Windows and macOS (C27), where a time the computer
	// slept through runs when it wakes.
	setWake = " After a night the computer was asleep, they may all start together when it wakes."
	// setApart is how many minutes apart new archives start.
	setApart = 10
)

// setScheduleQuestionFor is the set's schedule question on goos, the first time into a parent.
func setScheduleQuestionFor(goos string) string { return setScheduleQuestion(goos, setApartFirst) }

// setScheduleRerunQuestionFor is the set's schedule question on goos when an archive of the set keeps
// its own schedule (F-10d).
func setScheduleRerunQuestionFor(goos string) string {
	return setScheduleQuestion(goos, setApartRerun)
}

func setScheduleQuestion(goos, apart string) string {
	q := setScheduleHead + apart + setScheduleRuns + scheduleMissedFor(goos)
	if goos == "darwin" || goos == "windows" {
		q += setWake
	}
	return q
}

// askSetSchedule asks the set's question through the listDialog seam by askSchedule's rule
// (askScheduleBy: No preselected, an empty pick refused where the dialog can, only the daily or the
// weekly row installs). rerun chooses F-10d's words.
func askSetSchedule(goos string, rerun bool) (schedule.Interval, string, bool, error) {
	return askScheduleBy(goos, func(rows []string) (string, error) {
		if rerun {
			return listDialog(setScheduleRerunQuestionFor(runtime.GOOS), rows, zenity.Title(appTitle), zenity.DefaultItems(scheduleNo), zenity.DisallowEmpty())
		}
		return listDialog(setScheduleQuestionFor(runtime.GOOS), rows, zenity.Title(appTitle), zenity.DefaultItems(scheduleNo), zenity.DisallowEmpty())
	})
}

// scheduleProgram is the program a schedule runs (schedule.ProgramForSchedule in the product).
var scheduleProgram = schedule.ProgramForSchedule

// setJobState says whether the archive folder out has a job file, and whether it reads.
func setJobState(out string) (exists, reads bool) {
	p, err := job.PathFor(schedule.DefaultNameFor(out))
	if err != nil {
		return true, false
	}
	if _, err := os.Lstat(p); errors.Is(err, fs.ErrNotExist) {
		return false, false
	}
	_, err = job.Read(p)
	return true, err == nil
}

// setScheduleLive says whether the archive at out has a scheduled backup now (the adversarial pass's
// CON-01; §11's "installed and not paused"): its record is not "Run now only" (manual), and the host
// scheduler (queryLiveEntry, schedule.Query) says the entry its record names is installed (the name
// bound to this archive, schedule.RecordedBackupName; with no record, the name a set installs under,
// schedule.DefaultNameFor). A record that cannot be read, or whose name is not this archive's, is no
// scheduled backup. A job file alone never is: an install that failed after the job file was written
// and an auto-pause both leave one with no entry.
func setScheduleLive(out string) (live, manual bool) {
	name := schedule.DefaultNameFor(out)
	d, err := schedule.ReadDescriptor(out)
	switch {
	case err == nil && d.IsManual():
		return false, true
	case err == nil:
		bound, berr := schedule.RecordedBackupName(out)
		if berr != nil {
			return false, false
		}
		name = bound
	case !errors.Is(err, fs.ErrNotExist):
		return false, false
	}
	return queryLiveEntry(name) == schedule.Installed, false
}

// offerSetSchedule is the set's schedule step (§11). An archive continued (brought up to date, or a
// folder an earlier run made) whose job file is there keeps its job and its scheduler entry exactly
// as they are, whatever the answer (ScheduleKept, or ScheduleKeptUnreadable when the file cannot be
// read: never installed over, F-11). It counts as scheduled (ScheduleKept) only when the scheduler
// says its own entry is installed and its record is not "Run now only" (setScheduleLive, CON-01);
// otherwise it is ScheduleKeptOff or ScheduleKeptManual, still never installed over, and the end note
// says it has no scheduled backup now. The question is asked once, only when a Yes would install a
// schedule for an archive that finished and may be scheduled (not saved once): one made in this run,
// or one continued with no job file. With an exact Yes row, each such archive gets its own job file
// and entry through installOffered (named from its own path), the first at the chosen time and each
// next one 10 minutes later; an install that fails does not stop the others; no job written holds a
// Thunderbird sync. The end note (setScheduleNote) follows a Yes. No and Cancel install nothing and
// show nothing.
func offerSetSchedule(p setPlan, arcs []*setArchive) error {
	setKeptSchedules(arcs)
	var installs []*setArchive
	rerun := false
	for _, a := range arcs {
		if setKept(a.made.Schedule) {
			rerun = true
			continue
		}
		if a.made.Outcome == MadeFinished && !a.it.row.once {
			installs = append(installs, a)
		}
	}
	if len(installs) == 0 {
		return nil
	}
	iv, at, install, err := askSetSchedule(runtime.GOOS, rerun)
	if err != nil || !install {
		return err
	}
	exe, progReason, perr := scheduleProgram(false)
	if perr == nil {
		proceed, qerr := volatileDirCheck(exe)
		if qerr != nil {
			return qerr
		}
		if !proceed {
			return nil
		}
	}
	wrapper := false
	slot := 0
	for _, a := range installs {
		if perr != nil {
			a.made.Schedule, a.made.ScheduleWhy = ScheduleFailed, oneLine(perr)
			continue
		}
		c := a.c
		c.tbSync = false // no Thunderbird sync for a set (§11, the owner's answer 4)
		spec, err := installOffered(c, iv, setStartAt(at, slot), exe, "")
		if err != nil {
			a.made.Schedule, a.made.ScheduleWhy = ScheduleFailed, oneLine(err)
			continue
		}
		slot++
		a.made.Schedule = ScheduleInstalled
		// A new schedule starts with no failing streak: its first failure sends (R3C-10).
		_ = backupalert.ClearStreakDefault(backupalert.KeyFor(a.made.Folder))
		wrapper = wrapper || spec.Wrapper
	}
	// The lines about existing archives that back up the same mail: after a Yes only (MA-1501).
	note := setScheduleNote(p.parent, arcs, iv, at, runtime.GOOS == "windows" && wrapper, existingLines(p, arcs))
	if pn := schedule.ProgramNote(exe, progReason); perr == nil && pn != "" {
		note += "\n\n" + pn
	}
	// Through the messageDialog seam (zenity.Info).
	return noticeClosed(messageDialog(note, zenity.Title(appTitle)))
}

// setKeptSchedules sets, for each archive of arcs continued (brought up to date, or a folder an earlier
// run made) whose job file is there, what its own schedule is (offerSetSchedule's rule): ScheduleKept,
// ScheduleKeptOff, ScheduleKeptManual, or ScheduleKeptUnreadable for a job file that cannot be read.
// An archive whose schedule is already set is left as it is, so runSet may ask it before the summary
// and offerSetSchedule again after it, and the scheduler is asked once.
func setKeptSchedules(arcs []*setArchive) {
	for _, a := range arcs {
		if a.it.outcome == outNew || a.made.Schedule != ScheduleNone {
			continue
		}
		exists, reads := setJobState(a.made.Folder)
		if !exists {
			continue
		}
		a.made.Schedule = ScheduleKeptUnreadable
		if reads {
			switch live, manual := setScheduleLive(a.made.Folder); {
			case manual:
				a.made.Schedule = ScheduleKeptManual
			case live:
				a.made.Schedule = ScheduleKept
			default:
				a.made.Schedule = ScheduleKeptOff
			}
		}
	}
}

// setKept reports whether s is a continued archive's own schedule, kept as it is (any of the Kept
// outcomes).
func setKept(s MadeSchedule) bool {
	switch s {
	case ScheduleKept, ScheduleKeptUnreadable, ScheduleKeptOff, ScheduleKeptManual:
		return true
	}
	return false
}

// setNothingNew reports whether a Yes to the set's schedule question would install nothing: no
// archive finished that may be scheduled (not saved once) and has no schedule of its own (its kept
// schedules already set by setKeptSchedules). offerSetSchedule then asks nothing and shows no end
// note.
func setNothingNew(arcs []*setArchive) bool {
	for _, a := range arcs {
		if a.made.Schedule == ScheduleNone && a.made.Outcome == MadeFinished && !a.it.row.once {
			return false
		}
	}
	return true
}

// setStartAt is the time at ("02:00") plus slot times setApart minutes, around the clock.
func setStartAt(at string, slot int) string {
	var h, m int
	if _, err := fmt.Sscanf(at, "%d:%d", &h, &m); err != nil {
		return at
	}
	t := (h*60 + m + slot*setApart) % (24 * 60)
	return fmt.Sprintf("%02d:%02d", t/60, t%60)
}

// oneLine is err's words on one line, control characters dropped and a last full stop left off.
func oneLine(err error) string {
	return strings.TrimSuffix(util.StripControl(strings.Join(strings.Fields(err.Error()), " ")), ".")
}

// The end note's words (§11).
const (
	setNoteLogs  = "Each archive keeps its own log, mailarchive.log, in its own folder. These backups save the mail Thunderbird has already downloaded to this computer; they do not start Thunderbird first."
	setNoteRows  = "MailArchive Desktop lists them all under Your archives; a row whose last backup did not finish says so."
	setNoteKept  = " kept its own schedule."
	setNoteKeptX = " kept its own schedule (its settings could not be read)."
	// setNoteKeptOff and setNoteKeptManual follow the name of an archive brought up to date that has
	// its own settings but no scheduled backup now (CON-01, the lead's fold's words, with where
	// Repair is: on the Scheduled backup card of the archive switched to, never on its row; "Run now
	// only" has no Repair, so its remedy is its own).
	setNoteKeptOff    = " has its own settings but no scheduled backup now: in MailArchive Desktop, switch to it and click Repair on its Scheduled backup card."
	setNoteKeptManual = " has its own settings but no scheduled backup now: it is backed up only when you click Run now."
)

// setScheduleNote is the end note after a Yes (§11): what was scheduled ("Scheduled: daily from
// 02:00, one archive every 10 minutes, for 3 archives in …"; one archive: "daily at 02:00, for 1
// archive"; weekly: "weekly on Sunday from 03:00"), each archive that kept its own schedule, the logs
// and the Thunderbird line (with Windows' console-window line when a wrapper runs), each archive not
// scheduled and why (saved once; its install failed; it did not finish), the lines about existing
// archives that back up the same mail (existing, MA-1501), then the Desktop's line.
func setScheduleNote(parent string, arcs []*setArchive, iv schedule.Interval, at string, console bool, existing []string) string {
	n := 0
	for _, a := range arcs {
		if a.made.Schedule == ScheduleInstalled {
			n++
		}
	}
	where := util.StripControl(parent)
	var lines []string
	if n > 0 {
		when := "daily"
		if iv == schedule.Weekly {
			when = "weekly on Sunday"
		}
		if n == 1 {
			lines = append(lines, fmt.Sprintf("Scheduled: %s at %s, for 1 archive in %s.", when, at, where))
		} else {
			lines = append(lines, fmt.Sprintf("Scheduled: %s from %s, one archive every %d minutes, for %d archives in %s.", when, at, setApart, n, where))
		}
	}
	for _, a := range arcs {
		switch a.made.Schedule {
		case ScheduleKept:
			lines = append(lines, util.StripControl(a.made.Name)+setNoteKept)
		case ScheduleKeptUnreadable:
			lines = append(lines, util.StripControl(a.made.Name)+setNoteKeptX)
		case ScheduleKeptOff:
			lines = append(lines, util.StripControl(a.made.Name)+setNoteKeptOff)
		case ScheduleKeptManual:
			lines = append(lines, util.StripControl(a.made.Name)+setNoteKeptManual)
		}
	}
	if n > 0 {
		logs := setNoteLogs
		if console {
			logs += " A console window will appear briefly while it runs."
		}
		lines = append(lines, logs)
	}
	for _, a := range arcs {
		name := util.StripControl(a.made.Name)
		switch {
		case a.made.Schedule == ScheduleFailed:
			lines = append(lines, "Not scheduled: "+name+" ("+a.made.ScheduleWhy+").")
		case a.made.Schedule != ScheduleNone:
		case a.made.Outcome == MadeNotStarted:
			// The friction review's F10: an archive never started did not "not finish".
			lines = append(lines, "Not scheduled: "+name+", which was not started.")
		case a.made.Outcome != MadeFinished:
			lines = append(lines, "Not scheduled: "+name+", which did not finish.")
		case a.it.row.once:
			lines = append(lines, "Not scheduled: "+name+", saved once.")
		}
	}
	lines = append(lines, existing...)
	// Then how to back up a mailbox added later (the friction review's F1), the note's last line.
	lines = append(lines, setNoteRows, fmt.Sprintf(setLaterMailbox, where))
	return strings.Join(lines, "\n")
}
