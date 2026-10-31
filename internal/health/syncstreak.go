package health

import (
	"fmt"
	"runtime"
	"time"

	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/presync"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/state"
)

// This file is the Thunderbird sync's part of status and the health view
// (design-presync-attended §4.7, §5.4, slice AS5): the last backup's sync line, the
// 3-day "not synced" WARN with a remedy chosen by the last miss's reason (C-20), the
// softer "nothing arrived" WARN (owner ruling OQ-5), and status -json's
// thunderbird_sync (C-55). Every outcome name is shown only through
// presync.TagPhrase's closed map, never as written (C-37), and each surface gets its
// own words: the Desktop names buttons, the command line names commands (C-38). The
// wording is the design's draft, for the friction review.

// assessSync adds the sync's lines for a plaintext archive. An encrypted archive keeps
// nothing about the sync beside it (§4.4), so it has none here.
func assessSync(r *Report, in Input) {
	if in.Encrypted {
		return
	}
	if line := syncLastLine(in); line != "" {
		r.note("presync_last", line) // never a WARN by itself (§4.7)
	}
	// Only where the sync is really configured: a streak left from a schedule since
	// switched off says nothing (§5.4).
	if !in.HasDescriptor || !schedulePresyncConfigured(in) {
		return
	}
	s := in.SyncStreak
	switch {
	case s.Stale():
		r.warn("presync_not_synced", notSyncedText(in, s))
	case s.NothingArrived():
		// The softer warning is left out while the missed-sync one shows (§5.4).
		r.warn("presync_nothing_arrived", nothingArrivedText(in, s))
	}
}

// syncLastLine is the Status line from the last-run record's sync (§4.7): "" when the
// record has none, or a sync that was cancelled (no phrase).
func syncLastLine(in Input) string {
	if in.LastRunState != state.LastRunPresent || in.LastRun.Presync == nil {
		return ""
	}
	phrase := presync.TagPhrase(in.LastRun.Presync.Name)
	if phrase == "" {
		return ""
	}
	return fmt.Sprintf("Thunderbird sync at the last backup (%s): %s", in.LastRun.Started.Local().Format(localStamp), phrase)
}

// syncWords are the words of one surface on one platform.
type syncWords struct {
	desktop bool   // the Desktop names its buttons; elsewhere commands are named
	runNow  bool   // the Desktop's Run now starts Thunderbird on this platform (AttendedStartAllowed)
	hidden  string // how a started Thunderbird is kept: "out of sight", or "minimized"
	place   string // where it is quit from
	started string // "started out of sight", or "started minimized"
}

func syncWordsFor(in Input) syncWords {
	goos := in.GOOS
	if goos == "" {
		goos = runtime.GOOS
	}
	w := syncWords{desktop: in.Surface == "desktop", runNow: presync.AttendedStartAllowed(goos),
		hidden: "out of sight", place: "the Dock", started: "started out of sight"}
	if goos == "windows" {
		w.hidden, w.place, w.started = "minimized", "the taskbar or the notification area", "started minimized"
	}
	return w
}

// syncNow is the sentence that backs up the mail now, in the surface's words. "Click
// Run now" alone is said only where Run now starts Thunderbird (C-20); elsewhere the
// person opens Thunderbird first, and the backup reads what it fetched.
func (w syncWords) syncNow() string {
	switch {
	case w.desktop && w.runNow:
		return "Click Run now on the Scheduled backup card to sync while you watch."
	case w.desktop:
		return "To back up the mail now, open Thunderbird, let its mail arrive, then click Run now on the Scheduled backup card."
	}
	return "To back up the mail now, open Thunderbird, let its mail arrive, then run the backup by hand with `mailarchive -thunderbird-sync`."
}

// thenRunNow ends a remedy whose first step fixes the cause: ", then click Run now."
// where Run now syncs, or the fix's full stop and the syncNow sentence.
func (w syncWords) thenRunNow() string {
	if w.desktop && w.runNow {
		return ", then click Run now."
	}
	return ". " + w.syncNow()
}

// repair is the "repair the schedule" step in the surface's words.
func (w syncWords) repair() string {
	if w.desktop {
		return "Click Repair on the Scheduled backup card."
	}
	return "Repair the schedule from the Scheduled backup card in MailArchive Desktop, or from the wizard."
}

// syncRemedy is the "Do this" for the last miss's reason (§5.4's table, C-20). A name
// outside the table — a hostile one included — gets the generic remedy.
func syncRemedy(in Input, lastName string) string {
	w := syncWordsFor(in)
	switch lastName {
	case presync.OutcomeOwnerActiveSkipped.Name:
		first := w.syncNow()
		if w.desktop && w.runNow {
			first = "Click Run now on the Scheduled backup card to sync while you are here."
		}
		when := "change the backup's time to when you are usually away"
		if w.desktop {
			when = "change the backup's time in Settings to when you are usually away"
		}
		return first + " To stop this coming back, " + when + ", or leave Thunderbird open at that time."
	case presync.OutcomeLeftoverFound.Name:
		return "A Thunderbird an earlier backup started is still open " + w.hidden + ". Quit it from " + w.place + w.thenRunNow()
	case presync.OutcomeOtherProfileRunning.Name:
		return "Thunderbird was open with another profile at backup time. Quit it before the backup time, or leave this profile's Thunderbird open."
	case presync.OutcomeAppNotFound.Name:
		return "Thunderbird was not found in its usual place. Install it there" + w.thenRunNow()
	case presync.OutcomeRefusedElevated.Name:
		return "The backup ran as administrator, and the sync never runs that way. " + w.repair()
	case presync.OutcomeStartBlocked.Name:
		if w.desktop {
			return "Thunderbird could not be started from the way Mail Archive was started. Click Repair on the Scheduled backup card; if you use Run now, start Mail Archive Desktop from its shortcut."
		}
		return "Thunderbird could not be started from the way Mail Archive was started. " + w.repair()
	case presync.OutcomeProfileUnusable.Name:
		return "This Thunderbird profile could not be used. Open Thunderbird once" + w.thenRunNow()
	case presync.OutcomeNoDesktopSession.Name:
		return "No one was logged in at backup time, so Thunderbird could not be started. Stay logged in at the backup time."
	case presync.OutcomeNotSetUp.Name:
		return "The sync could not be prepared on this computer. " + w.repair()
	case presync.OutcomeProfileBusy.Name:
		if w.desktop {
			return "Another backup kept this Thunderbird profile busy. Shut down Mail Archive, open it again" + w.thenRunNow()
		}
		return "Another backup kept this Thunderbird profile busy. Make sure no other backup of this archive is still running" + w.thenRunNow()
	}
	return w.syncNow()
}

// streakDate is a streak's start as status writes it, or "recently" when unknown.
func streakDate(t time.Time) string {
	if t.IsZero() {
		return "recently"
	}
	return t.Local().Format("Jan 2, 2006")
}

// syncPhrase is the closed map's phrase for a miss, never the name itself (C-37).
func syncPhrase(name string) string {
	if p := presync.TagPhrase(name); p != "" {
		return p
	}
	return presync.TagPhrase("unknown") // the generic phrase
}

// SyncAlertWords are the phrase and remedy the Thunderbird-not-synced email gives for a
// streak (§5.5): the same closed maps as the presync_not_synced WARN, in the Desktop's
// words, since the email goes from a schedule the Desktop or the wizard made. A name
// outside the maps reads as the generic phrase and remedy (C-37).
func SyncAlertWords(goos string, s state.SyncStreak) (phrase, remedy string) {
	return syncPhrase(s.LastName), syncRemedy(Input{GOOS: goos, Surface: "desktop"}, s.LastName)
}

// notSyncedText is the presync_not_synced WARN (§5.4).
func notSyncedText(in Input, s state.SyncStreak) string {
	return fmt.Sprintf("Thunderbird has not been synced before this archive's scheduled backups since %s (%d backups in a row; the last time: %s). The backups still ran, but mail that had only reached the mail server may be missing. Do this: %s",
		streakDate(s.FirstMissed), s.Missed, syncPhrase(s.LastName), syncRemedy(in, s.LastName))
}

// nothingArrivedText is the softer presync_nothing_arrived WARN (§5.4, OQ-5): its first
// sentence says the backups ran, and it names a hidden password or sign-in window.
func nothingArrivedText(in Input, s state.SyncStreak) string {
	w := syncWordsFor(in)
	then := "then click Run now."
	if !w.desktop {
		then = "then run the backup by hand with `mailarchive -thunderbird-sync`."
	}
	return fmt.Sprintf("This is not a failed backup: the backups still ran. Before this archive's scheduled backups since %s, Thunderbird was started but no new mail arrived (%d backups in a row). That can mean there was no new mail, or that Thunderbird is waiting behind a password or sign-in window nobody can see, because it was %s. Do this: open Thunderbird; if it asks for a password or a sign-in, answer it, %s",
		streakDate(s.FirstNoChange), s.NoChange, w.started, then)
}

// JSONThunderbirdSync is status -json's thunderbird_sync (§4.7, C-55): the setting, and
// for a plaintext archive the last backup's sync. An encrypted archive carries the
// setting only.
type JSONThunderbirdSync struct {
	Enabled bool             `json:"enabled"`
	Last    *JSONPresyncLast `json:"last,omitempty"`
}

// JSONPresyncLast is the last backup's sync: its outcome's name (one the vocabulary
// knows, else "unknown"), the record's result, the closed map's phrase, when the
// backup started, and when this sync started Thunderbird (null when it started none).
type JSONPresyncLast struct {
	Name   string     `json:"name"`
	Result string     `json:"result"`
	Phrase string     `json:"phrase"`
	Run    time.Time  `json:"run_started"`
	Since  *time.Time `json:"since,omitempty"`
}

// jsonThunderbirdSync builds thunderbird_sync, or nil when the sync is off and nothing
// is recorded.
func jsonThunderbirdSync(in Input) *JSONThunderbirdSync {
	if in.Encrypted {
		if !in.RunNowSync {
			return nil
		}
		return &JSONThunderbirdSync{Enabled: true} // the setting only (§4.7, OD-3)
	}
	doc := &JSONThunderbirdSync{Enabled: in.HasDescriptor && schedulePresyncConfigured(in)}
	if in.LastRunState == state.LastRunPresent && in.LastRun.Presync != nil {
		p := in.LastRun.Presync
		last := &JSONPresyncLast{Name: knownOutcomeName(p.Name), Result: knownResult(p.Result), Phrase: presync.TagPhrase(p.Name), Run: in.LastRun.Started}
		if !p.Since.IsZero() {
			since := p.Since
			last.Since = &since
		}
		doc.Last = last
	}
	if !doc.Enabled && doc.Last == nil {
		return nil
	}
	return doc
}

// knownOutcomeName is name when the sync's vocabulary has it, else "unknown": a
// hand-edited record cannot put its own text in status -json (C-37).
func knownOutcomeName(name string) string {
	for _, o := range presync.Outcomes() {
		if o.Name == name && name != "" {
			return name
		}
	}
	return "unknown"
}

// knownResult is one of the record's result values, else "unknown".
func knownResult(r string) string {
	switch r {
	case state.PresyncResultSynced, state.PresyncResultNotSynced, state.PresyncResultCancelled, state.PresyncResultWaitEnded:
		return r
	}
	return "unknown"
}

// runNowSyncConfigured reports whether the job file this computer keeps for the
// archive at out carries the Thunderbird sync (§4.7): read from the settings folder,
// never the archive. A missing or unreadable job is false.
func runNowSyncConfigured(out string) bool {
	p, ok := schedule.RunJobPathFor(out)
	if !ok {
		return false
	}
	j, err := job.Read(p)
	return err == nil && j.ThunderbirdSync != nil
}
