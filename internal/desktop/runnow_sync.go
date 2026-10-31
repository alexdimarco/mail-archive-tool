package desktop

import (
	"encoding/json"
	"net/http"
	"strings"

	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/presync"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/seal"
)

// An encrypted archive can sync Thunderbird before its Run now (design-presync-attended
// §7, §8, slice AS8; owner rulings OQ-3, OQ-8; C-18, C-24, C-25, C-26, C-27, C-49,
// C-52): the Settings card's sync-only box, its own Save through api/runnow-sync, and
// the encrypted texts. An encrypted archive is backed up only when the person clicks
// Run now (phase 3 O5), so the sync runs only then, while they are here. Draft wording,
// settled by the friction review (§14 gate 3).

// encLockSentence is what every encrypted Run now text says about a Lock (§3.2, OQ-3,
// OQ-8): it cancels the backup until it starts reading mail.
const encLockSentence = "Until it starts reading mail, Lock cancels it; after that, Cancel stops it."

// encRunNowBase is the encrypted Run now's tooltip before slice AS8.
const encRunNowBase = "Back up this encrypted archive now, while it is unlocked here."

// encRunNowStartOff is the encrypted Run now's tooltip, sync on, where this platform's
// attended start is not allowed yet (C-49, OQ-7): no scheduled backup syncs an
// encrypted archive, so it names none.
const encRunNowStartOff = encRunNowBase + " On this computer Run now does not start Thunderbird yet, so it backs up the mail already here. " + encLockSentence

// encRunNowTitle is the encrypted Run now's tooltip (§7, OQ-3, OQ-8): with the sync
// off (or where it never runs, Linux) the Lock sentence alone joins today's words;
// with it on, the plaintext sync text, that the archive stays unlocked for the backup
// while it waits, and the Lock sentence.
func encRunNowTitle(goos string, syncOn bool) string {
	switch {
	case !syncOn || (goos != "darwin" && goos != "windows"):
		return encRunNowBase + " " + encLockSentence
	case !presync.AttendedStartAllowed(goos):
		return encRunNowStartOff
	}
	return runNowTitle(goos, true) + " While it waits for Thunderbird, the archive stays unlocked for this backup. " + encLockSentence
}

// encSyncLabel and encSyncLine are the box's label and its line (§8.1, OQ-3).
const (
	encSyncLabel = "Sync Thunderbird before each Run now"
	encSyncLine  = "This archive is backed up only when you click Run now, so the sync runs then, while you are here. While it waits for Thunderbird, the archive stays unlocked for that backup. " + encLockSentence
	// encSyncStartOffLine is the box's reason where this platform's Run now does not
	// start Thunderbird yet (C-49): shown so the setting never promises a sync.
	encSyncStartOffLine = "On this computer Run now does not start Thunderbird yet: until it does, Run now backs up the mail already here."
)

// encSyncNotice is SyncNotice's encrypted card variant (§7, C-25, C-52, OQ-2): every
// X16 disclosure that applies to a Run now that never closes Thunderbird — what
// starting it does, that an open Thunderbird is left alone, the one-profile limit —
// and no scheduled-backup sentence, since an encrypted archive has none. The close
// variant ("…then closes it, which runs Thunderbird's on-exit actions…") arrives with
// slice AS-Q.
func encSyncNotice(goos string) string {
	w := runNowWordsFor(goos)
	return "Before each Run now, MailArchive starts Thunderbird " + w.hidden + " and waits for its mail to arrive (usually 13 to 20 minutes), so the archive includes mail that had only reached the server, then leaves it open for you to quit from " + w.place + ". " +
		"Because it drives your real Thunderbird, starting it does what opening Thunderbird always does: it checks all your accounts, sends anything waiting in your Outbox, runs your message filters, and may show notifications. " +
		"A Thunderbird you already have open is left alone — never started or closed. If you use more than one Thunderbird profile, only the one holding this mailbox is synced."
}

// encSyncSettingLine is the health view's line for an encrypted archive whose Run now
// syncs Thunderbird (§4.7, C-27): the setting, and while it is unlocked here, the last
// Run now's sync in §4.5's words, kept in memory only.
func encSyncSettingLine(last string) string {
	s := "Thunderbird sync: on (Run now only)"
	if last != "" {
		s += "; last Run now here: " + last
	}
	return s + "."
}

// runNowJobFor is the job Run now would run for the archive at out (§8.2, C-18/M2):
// BuildKindEvidence's JobPath, re-resolved now (it can fall back to scanning the job
// folder), read with readJobToEdit's same-archive check. Evidence counts only a job
// that reads, so when it finds none the default-named file is looked at too: one
// that is there but can't be read, or names another archive, is reported, never
// skipped. reason says why a job that is there cannot be used; path "" means no Run
// now job here.
func runNowJobFor(out string) (path string, j job.Job, reason string) {
	path = schedule.BuildKindEvidence(out).JobPath
	if path == "" {
		p, err := job.PathFor(schedule.DefaultNameFor(out))
		if err != nil {
			return "", job.Job{}, ""
		}
		path = p
	}
	j, exists, reason := readJobToEdit(path, out)
	if !exists && reason == "" {
		return "", job.Job{}, ""
	}
	return path, j, reason
}

// jobThunderbirdSourced reports whether the job is made from Thunderbird on this
// computer (§8.3, C-24): not an Outlook job, on macOS or Windows (goos, so it is
// tested on every platform), with an input that is a local path inside a Thunderbird
// profile here — the wizard's own test (presyncOfferable). A network path is never
// looked at (C-17).
func jobThunderbirdSourced(j job.Job, goos string) bool {
	if j.Outlook || (goos != "darwin" && goos != "windows") {
		return false
	}
	for _, in := range j.Inputs {
		if isRemotePath(in) {
			continue
		}
		if _, ok := findProfileDir(in); ok {
			return true
		}
	}
	return false
}

// encSyncCard is the Settings card of an encrypted archive (§8.1, C-25, C-26, C-27):
// the location, and the sync-only box when all hold — macOS or Windows; the job Run
// now would run is there and reads securely; and it carries the sync already (never
// hidden while on) or, where this platform's Run now starts Thunderbird, it is made
// from Thunderbird here. Where the job cannot be read, a one-line reason. last is the
// last Run now's sync phrase while the archive is unlocked here.
func encSyncCard(out, goos, last string) settingsCard {
	var s settingsCard
	if goos != "darwin" && goos != "windows" {
		return s
	}
	path, j, reason := runNowJobFor(out)
	switch {
	case path == "":
		return s
	case reason != "":
		s.SyncUnreadable = "Thunderbird sync can't be shown: this archive's backup settings file " + path + " can't be read (" + reason + ")."
		return s
	}
	on := j.ThunderbirdSync != nil
	if !on && (!presync.AttendedStartAllowed(goos) || !jobThunderbirdSourced(j, goos)) {
		return s
	}
	s.Sync, s.SyncOnly, s.SyncOn = true, true, on
	if !presync.AttendedStartAllowed(goos) {
		s.SyncReason = encSyncStartOffLine
	}
	s.SyncNotice = encSyncNotice(goos)
	if on {
		s.SyncLast = last
	}
	return s // no Mac note before macOS's attended close (AS9, C-52)
}

// runNowSyncRequest is what the encrypted card's Save posts (§8.2): the archive the
// page was drawn for and the box. Both are required: a missing drawnFor or presync is
// refused, never read as "off".
type runNowSyncRequest struct {
	DrawnFor *string         `json:"drawnFor"`
	Presync  json.RawMessage `json:"presync"`
}

// presync is the box's value: true or false exactly, else not given (never "off").
func (in runNowSyncRequest) presync() (on, given bool) {
	switch strings.TrimSpace(string(in.Presync)) {
	case "true":
		return true, true
	case "false":
		return false, true
	}
	return false, false
}

// runNowSyncAction is POST api/runnow-sync (§8.2, C-18, C-26), through d.api (POST
// only, the body limit, no-store, GuardLocalPOST and the CSRF token): it switches the
// Thunderbird sync of the job Run now would run for the current encrypted archive on
// or off. Every refusal comes before any write and ends "Nothing was changed.". It
// changes only thunderbird_sync, through writeJob, and never writes a Profile. It
// needs no key: the job file is in the settings folder, not the archive.
func (d *dashboard) runNowSyncAction(w http.ResponseWriter, r *http.Request) {
	var in runNowSyncRequest
	_ = json.NewDecoder(r.Body).Decode(&in)
	refuse := func(msg string) { writeJSON(w, map[string]any{"ok": false, "error": msg}) }
	out := d.cfg.effectiveOut()
	goos := d.cfg.goos()
	on, given := in.presync()
	switch {
	case in.DrawnFor == nil || d.stalePage(in.DrawnFor):
		refuse(staleRefusal) // no older page posts here, so a missing drawnFor is refused too
		return
	case !given:
		refuse("Say whether Run now should sync Thunderbird. Nothing was changed.")
		return
	case out == "" || archiveKind(out) != kindEncrypted:
		refuse("This archive is not encrypted: use Save settings in Settings. Nothing was changed.")
		return
	case goos != "darwin" && goos != "windows":
		refuse("Thunderbird sync works on macOS and Windows only. Nothing was changed.")
		return
	case seal.IsSealed(out):
		refuse(pausedRefusal)
		return
	case d.captureRunning():
		refuse(runningRefusal)
		return
	}
	path, j, reason := runNowJobFor(out)
	switch {
	case path == "":
		refuse("No backup is set up on this computer for this encrypted archive, so there is no Run now to sync Thunderbird before. Nothing was changed.")
		return
	case reason != "":
		refuse(unreadableJobText(path, reason))
		return
	case on && !presync.AttendedStartAllowed(goos):
		refuse("On this computer Run now does not start Thunderbird yet, so the sync can't be switched on here. Nothing was changed.")
		return
	case on && !jobThunderbirdSourced(j, goos):
		refuse("This archive's backup wasn't made from a Thunderbird profile on this computer, so there is no Thunderbird to sync. Nothing was changed.")
		return
	}
	if on {
		if j.ThunderbirdSync == nil {
			j.ThunderbirdSync = &job.ThunderbirdSync{} // never a Profile (§8.2)
		}
	} else {
		j.ThunderbirdSync = nil
	}
	if err := writeJob(path, j); err != nil {
		refuse("The setting could not be saved (" + strings.TrimSpace(err.Error()) + "). Nothing was changed.")
		return
	}
	note := "Saved. Run now will not sync Thunderbird."
	if on {
		note = "Saved. Run now will sync Thunderbird first."
	}
	writeJSON(w, map[string]any{"ok": true, "note": note})
}
