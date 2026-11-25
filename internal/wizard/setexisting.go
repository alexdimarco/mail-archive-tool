package wizard

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/seal"
	"mail-archive-tool/internal/util"
)

// Existing archives that back up the same mail (docs/design-per-mailbox-archives.md §11, revision 2's
// C1, in the rescue's form; MA-1501). They are never split, moved, renamed or rewritten: after a Yes
// to the set's schedule, the end note may say, for each archive this computer knows elsewhere, that
// it backs up mailboxes of this set too, and whether pausing it is safe yet. Never in the summary
// (which comes before the question), and never after No or Cancel. Only job files are read, never a
// manifest.

// The existing-archive lines' words (§11).
const (
	existingTogether = "Your archive %s also backs up these mailboxes, all together."
	existingOne      = "%s also backs up %s."
	existingPause    = "To stop backing them up twice, click Pause (make read-only) on its row in MailArchive Desktop; its mail stays readable."
	existingKeep     = "Keep it running until each of these mailboxes has its own scheduled backup."
	// The friction review's F3 (its words; the lead's call: a folder no set archive covers gives no
	// Pause advice, only these words).
	existingMissingOne  = "It also lists %s, which is not on this computer now; its mail already saved stays in it."
	existingMissingMany = "It also lists %s, which are not on this computer now; their mail already saved stays in it."
	existingUntilOne    = "Keep it running until %s finishes; then run this again into the same folder."
	existingUntilMany   = "Keep it running until %s finish; then run this again into the same folder."
)

// existingLines are the end note's lines about the archives this computer knows that back up
// mailboxes of the plan p too (§11). Each known archive's job file is read with job.Read; one it
// cannot read gives no line. Passed over: a folder of this run's plan (made, brought up to date or
// left paused) or directly inside this parent, each compared by identity (a folder that cannot be
// looked at now gives no line); a paused archive; an encrypted one; an Outlook-app job. Of the rest, a
// job with auto-discovery on, or with two or more inputs of which one is a mailbox of the plan, gives
// "Your archive PATH also backs up these mailboxes, all together."; a job with one input and
// auto-discovery off, that input a mailbox of the plan, gives "PATH also backs up NAME.". The line
// goes on with the Pause advice only when every input the job lists is a mailbox of the plan (and it
// lists one: a job with auto-discovery on and no inputs may find more), every mailbox of the plan
// that may be scheduled (not saved once) has its archive scheduled (installed now, or keeping its own
// schedule that reads), and no archive of the set failed, stopped or was not started; otherwise with
// "Keep it running until each of these mailboxes has its own scheduled backup.".
func existingLines(p setPlan, arcs []*setArchive) []string {
	parent, err := os.Stat(p.parent)
	if err != nil {
		return nil
	}
	// This run's plan: its folders, and its mailboxes.
	var planned []os.FileInfo
	for _, a := range arcs {
		if a.made.Folder != "" {
			if fi, err := os.Stat(a.made.Folder); err == nil {
				planned = append(planned, fi)
			}
		}
	}
	for _, it := range p.items {
		if it.outcome == outPaused {
			if fi, err := os.Stat(filepath.Join(p.parent, it.folder)); err == nil {
				planned = append(planned, fi)
			}
		}
	}
	type mailbox struct {
		fi   os.FileInfo
		name string
	}
	var boxes []mailbox
	for _, it := range p.items {
		if fi, err := os.Stat(it.row.path); err == nil {
			boxes = append(boxes, mailbox{fi, it.row.listName()})
		}
	}
	mailboxOf := func(in string) (string, bool) {
		fi, err := os.Stat(in)
		if err != nil {
			// A mailbox of this plan whose folder is not there now (gone since the plan, so its
			// archive failed) is still this plan's, never a folder no set archive covers (F3).
			for _, it := range p.items {
				if filepath.Clean(it.row.path) == filepath.Clean(in) {
					return it.row.listName(), true
				}
			}
			return "", false
		}
		for _, b := range boxes {
			if os.SameFile(fi, b.fi) {
				return b.name, true
			}
		}
		return "", false
	}
	pauseSafe := setAllScheduled(p, arcs)
	unfinished := setUnfinishedNames(arcs)

	var lines []string
	seen := map[string]bool{}
	for _, k := range app.KnownArchives() {
		k = filepath.Clean(k)
		if seen[k] {
			continue
		}
		seen[k] = true
		fi, err := os.Stat(k)
		if err != nil {
			continue
		}
		skip := false
		for _, f := range planned {
			skip = skip || os.SameFile(fi, f)
		}
		if up, err := os.Stat(filepath.Dir(k)); err != nil || os.SameFile(up, parent) {
			skip = true
		}
		if skip || seal.IsSealed(k) || encmark.MustMinimise(k) {
			continue
		}
		jp, err := job.PathFor(schedule.DefaultNameFor(k))
		if err != nil {
			continue
		}
		j, err := job.Read(jp)
		if err != nil || j.Outlook {
			continue
		}
		inPlan, all := 0, len(j.Inputs) > 0
		name := ""
		var missing []string // inputs not on this computer now (a removed account's folder)
		others := 0          // inputs on this computer that are no mailbox of this plan
		for _, in := range j.Inputs {
			if n, ok := mailboxOf(in); ok {
				inPlan++
				name = n
				continue
			}
			all = false
			if _, err := os.Stat(in); err != nil {
				missing = append(missing, util.StripControl(in))
			} else {
				others++
			}
		}
		where := util.StripControl(k)
		var line string
		switch {
		case j.Auto || len(j.Inputs) >= 2 && inPlan > 0:
			line = fmt.Sprintf(existingTogether, where)
		case len(j.Inputs) == 1 && inPlan == 1:
			line = fmt.Sprintf(existingOne, where, util.StripControl(name))
		default:
			continue
		}
		// A folder it lists that no set archive can ever cover is named, and its mail is said to stay
		// (the friction review's F3, the lead's call: such an archive gets no Pause advice).
		switch len(missing) {
		case 0:
		case 1:
			line += " " + fmt.Sprintf(existingMissingOne, missing[0])
		default:
			line += " " + fmt.Sprintf(existingMissingMany, strings.Join(missing, ", "))
		}
		switch {
		case all && pauseSafe:
			line += " " + existingPause
		case len(unfinished) > 0:
			// F3: say which member to finish first, not "until each … has its own scheduled backup",
			// when every mailbox but those is scheduled.
			line += " " + existingUntil(unfinished)
		case len(missing) > 0 && others == 0 && pauseSafe:
			// Every other input is a mailbox of this set, scheduled: the line ends at its mail.
		default:
			line += " " + existingKeep
		}
		lines = append(lines, line)
	}
	return lines
}

// existingUntil is the advice for an existing archive while members of the set did not finish (the
// friction review's F3, its words): "Keep it running until NAME finishes; then run this again into the
// same folder.", the names joined for two or more.
func existingUntil(names []string) string {
	if len(names) == 1 {
		return fmt.Sprintf(existingUntilOne, names[0])
	}
	return fmt.Sprintf(existingUntilMany, strings.Join(names, ", "))
}

// setUnfinishedNames are the names of the set's archives that did not finish, were not made or were
// not started.
func setUnfinishedNames(arcs []*setArchive) []string {
	var out []string
	for _, a := range arcs {
		if a.made.Outcome != MadeFinished {
			out = append(out, util.StripControl(a.made.Name))
		}
	}
	return out
}

// setAllScheduled reports whether every mailbox of the plan p that may be scheduled (it gets an
// archive and is not saved once) has its archive's schedule installed now or kept (its own, which
// reads), and no archive of the set failed, stopped or was not started (§11's Pause condition).
func setAllScheduled(p setPlan, arcs []*setArchive) bool {
	byRow := map[string]*setArchive{}
	for _, a := range arcs {
		if a.made.Outcome != MadeFinished {
			return false
		}
		byRow[a.it.row.path] = a
	}
	for _, it := range p.items {
		if !it.row.make || it.row.once {
			continue
		}
		a := byRow[it.row.path]
		if a == nil || a.made.Schedule != ScheduleInstalled && a.made.Schedule != ScheduleKept {
			return false
		}
	}
	return true
}
