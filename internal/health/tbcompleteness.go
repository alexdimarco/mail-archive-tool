package health

import (
	"strings"
	"time"

	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/tbcomplete"
)

// The Thunderbird completeness check on `mailarchive status` and MailArchive Desktop's Status &
// health (docs/design-thunderbird-completeness.md §5, §6, §7; slice C3). Read from the plaintext
// last-run record of a backup that ended ok: an encrypted archive keeps nothing of it beside the
// archive (O1, R44), and a failed run's own words win (§11). The words are state's (one producer,
// the same as the wizard's and Run now's).

// tbRecord is the last-run record's completeness when status may say it: a plaintext record of a
// backup that ended ok, with an account or accounts left out.
func tbRecord(in Input) (state.LastRun, bool) {
	lr := in.LastRun
	if in.LastRunState != state.LastRunPresent || lr.Encrypted || in.Encrypted || lr.Status != state.RunOK {
		return lr, false
	}
	return lr, len(lr.Thunderbird) > 0 || lr.ThunderbirdMore > 0
}

// assessThunderbird adds each account's reasons (§5's Q3 and TC-19, §7's Status & health row):
//   - thunderbird_behind, a WARN on every behind backup (Q3), its remedy in its words (X6);
//   - thunderbird_limited and thunderbird_not_kept, settings the person chose: a WARN on the run
//     that first shows the setting (the record's first-seen time is that run's end, or not known),
//     then a note while it is unchanged (TC-19, L3);
//   - thunderbird_caught_up and thunderbird_unknown, notes: the backup itself is fine (§4.4);
//   - thunderbird_default_not_kept (Spam and Trash), thunderbird_labels_not_kept (TC-8) and
//     thunderbird_gmail_cap (TC-22), notes;
//   - thunderbird_more, a note for accounts the record left out (R2-06).
func assessThunderbird(r *Report, in Input) {
	lr, ok := tbRecord(in)
	if !ok {
		return
	}
	// Every WARN is said before the notes, so the account's lines are said apart (the C3 friction
	// re-look's R1); a Run-now-only archive's remedy says "Click Run now again", as its Run now
	// note does (R5).
	say := state.TBSay{Scheduled: in.HasDescriptor && !manualBackup(in), Now: r.now, Apart: true}
	if manualBackup(in) {
		say.Surface = state.TBSurfaceRunNow
	}
	firstSeen := func(since time.Time) bool { return since.IsZero() || since.Equal(lr.Finished) }
	chosen := func(code, msg string, since time.Time) {
		if firstSeen(since) {
			r.warn(code, msg)
		} else {
			r.note(code, msg)
		}
	}
	for _, a := range lr.Thunderbird {
		head := state.TBHeadline(a, say)
		switch a.Verdict {
		case "behind":
			r.warn("thunderbird_behind", head)
		case "limited":
			chosen("thunderbird_limited", strings.TrimSpace(head+" "+strings.Join(state.TBLimitLines(a), " ")), a.LimitSince)
		case "caught_up":
			r.note("thunderbird_caught_up", head)
		default:
			r.note("thunderbird_unknown", head)
		}
		if s := state.TBNotKeptLine(a); s != "" {
			chosen("thunderbird_not_kept", s, a.NotKeptSince)
		}
		if s := state.TBCapNote(a); s != "" {
			r.note("thunderbird_gmail_cap", s)
		}
		if s := state.TBDefaultNotKeptNote(a); s != "" {
			r.note("thunderbird_default_not_kept", s)
		}
		if s := state.TBLabelsNote(a); s != "" {
			r.note("thunderbird_labels_not_kept", s)
		}
	}
	if s := state.TBMoreSentence(lr.ThunderbirdMore); s != "" {
		r.note("thunderbird_more", s)
	}
}

// tbLeadSuffix is the Last run line's ending: the run's lead, built from the record through
// RunLeadWith with its thunderbird_more (tbcomplete.RecordLead), "" when status may not say it.
func tbLeadSuffix(in Input) string {
	lr, ok := tbRecord(in)
	if !ok {
		return ""
	}
	return " · " + tbcomplete.RecordLead(lr.Thunderbird, lr.ThunderbirdMore).Sentence()
}

// tbJSON fills `status -json`'s additive keys (§7's -json folds): the record's accounts, with
// complete and stale in every entry (M9), the accounts left out, and the run's lead by name.
func tbJSON(doc *JSONReport, in Input) {
	lr, ok := tbRecord(in)
	if !ok {
		return
	}
	doc.ThunderbirdCompleteness = lr.Thunderbird
	doc.ThunderbirdMore = lr.ThunderbirdMore
	doc.ThunderbirdLead = tbcomplete.RecordLead(lr.Thunderbird, lr.ThunderbirdMore).String()
}
