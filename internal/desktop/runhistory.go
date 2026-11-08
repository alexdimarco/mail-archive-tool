package desktop

import (
	"strings"
	"time"

	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/presync"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/util"
)

// runRow is one line of the Recent backups card (MA-701).
type runRow struct {
	When   string
	Status string // "OK" | "Incomplete" | "Failed" | "Cancelled"
	Failed bool
	Detail string
	// Sync is the run's Thunderbird sync tag (design-presync-attended §4.5), in the
	// closed map's words only; "" for no tag and on every row of an encrypted archive.
	Sync string
	// Repeat says how often in a row a run ended in these same words, and when the last
	// did ("3 times, last at 5:04 PM"); "" for a run told once (MA-1152). When is then the
	// first of them.
	Repeat string
	// times counts the runs this row stands for; first and last are when the oldest and the
	// newest of them ended.
	times       int
	first, last time.Time
}

// detailPart is a piece of a row's detail: words, or the one report address, which the card
// shows as a link (MA-1151).
type detailPart struct {
	Text string
	Link bool
}

// DetailParts is the row's detail split around state.ReportAddress, so the template links that
// address alone: only the fixed constant is ever a link, whatever else the words hold.
func (r runRow) DetailParts() []detailPart { return linkParts(r.Detail) }

// linkParts splits s around every state.ReportAddress in it.
func linkParts(s string) []detailPart {
	var parts []detailPart
	for {
		before, after, found := strings.Cut(s, state.ReportAddress)
		if before != "" {
			parts = append(parts, detailPart{Text: before})
		}
		if !found {
			return parts
		}
		parts = append(parts, detailPart{Text: state.ReportAddress, Link: true})
		s = after
	}
}

// recentRows is how many rows the Recent backups card shows.
const recentRows = 10

// recentRunRows reads the archive's rolling run history and formats the most recent runs,
// newest first (up to 10 rows), for the Recent backups card. Runs in a row that ended in the
// same words are one row with how often and when the last did (the 1.6.1 friction review's P2,
// MA-1152): hourly backups with a standing cause no longer fill the card with the same lines.
// Best effort: a missing or unreadable history shows no rows rather than an error.
func recentRunRows(out string) []runRow {
	if out == "" {
		return nil
	}
	h, err := state.ReadRunHistory(out)
	if err != nil {
		return nil
	}
	// An encrypted archive's rows carry no sync tag, judged by the archive's kind and
	// not by an entry, so a name planted in its history shows nothing (§4.4, §4.5,
	// C-16/T12). A folder whose kind cannot be told counts as encrypted.
	tagged := !encmark.MustMinimise(out)
	rows := make([]runRow, 0, recentRows)
	for i := len(h) - 1; i >= 0; i-- {
		r := runRowOf(h[i], tagged)
		if n := len(rows); n > 0 && sameWords(rows[n-1], r) {
			// One more run in a row that ended this way: the row now starts at this older one.
			rows[n-1].When, rows[n-1].first, rows[n-1].times = r.When, h[i].When, rows[n-1].times+1
			continue
		}
		if len(rows) == recentRows {
			break
		}
		r.times, r.first, r.last = 1, h[i].When, h[i].When
		rows = append(rows, r)
	}
	for i := range rows {
		if rows[i].times > 1 {
			rows[i].Repeat = repeatWords(rows[i])
		}
	}
	return rows
}

// sameWords reports whether two rows say the same thing apart from when.
func sameWords(a, b runRow) bool {
	return a.Status == b.Status && a.Failed == b.Failed && a.Detail == b.Detail && a.Sync == b.Sync
}

// repeatWords is a repeated row's count and when its newest run ended: the time alone when that
// was on the day its first run ended, else with the date; the count alone when it ended in the
// minute the row already shows (the polish-161 walk, N1).
func repeatWords(r runRow) string {
	at := r.last.Local().Format("Jan 2, 2006 3:04 PM")
	if at == r.first.Local().Format("Jan 2, 2006 3:04 PM") {
		return state.Count(r.times, "time", "times")
	}
	if r.last.Local().Format("2006-01-02") == r.first.Local().Format("2006-01-02") {
		at = r.last.Local().Format("3:04 PM")
	}
	return state.Count(r.times, "time", "times") + ", last at " + at
}

// runRowOf words one history entry as its row.
func runRowOf(e state.RunHistoryEntry, tagged bool) runRow {
	r := runRow{When: e.When.Local().Format("Jan 2, 2006 3:04 PM"), Failed: e.Status == state.RunFailed}
	if tagged {
		r.Sync = presync.TagPhrase(e.Presync)
	}
	switch e.Status {
	case state.RunOK:
		r.Status = "OK"
		switch {
		case e.Encrypted && e.ErrorKind == state.NoteIndexBehind:
			r.Detail = "backup completed; some messages are not searchable yet — the next backup adds them"
		case e.Encrypted: // its counts are sealed with the archive (F15)
			r.Detail = "backup completed (its counts are sealed)"
		case e.Exported > 0 && e.Filled > 0:
			r.Detail = state.Count(e.Exported, "message", "messages") + ", " + state.Count(e.Filled, "filled", "filled")
		case e.Exported > 0:
			r.Detail = state.Count(e.Exported, "message", "messages")
		default:
			r.Detail = "no new mail"
		}
		// Messages not downloaded in full yet, in the sentence Run now says too, worded by where
		// the run read its mail (design-plain-failure-words, MA-1149, MA-1157).
		if !e.Encrypted && e.Fillable > 0 {
			r.Detail += " · " + state.NotDownloadedSentence(e.Fillable, e.FillableFrom)
		}
	case state.RunFailed:
		r.Status, r.Detail = "Failed", e.Error
		switch {
		case e.ErrorKind != "": // an encrypted archive's entry: the kind's fixed words
			r.Detail = state.FailureText(e.ErrorKind)
		case e.Partial != nil:
			// A partial run saved everything else it found: "Incomplete", in its kinds'
			// words, the report named only as the details (design-plain-failure-words,
			// MA-1146; one truth with status's "incomplete", X10).
			r.Status = "Incomplete"
			r.Detail = strings.Join(state.PartialSentences(*e.Partial), " ") + " " + state.PartialDetails
			// What of its mail is not in full yet, as an OK row and Run now's note say it (the
			// polish-161 friction walk's N4, MA-1162).
			if !e.Encrypted && e.Fillable > 0 {
				r.Detail += " · " + state.NotDownloadedSentence(e.Fillable, e.FillableFrom)
			}
		}
	case state.RunCancelled:
		r.Status, r.Detail = "Cancelled", "stopped before it finished"
	default:
		r.Status = e.Status
	}
	// The history's words are anyone's who can write the archive folder: their direction-changing
	// characters go before the card shows them beside the report link (the polish-161 adversarial
	// pass, L2, MA-1167).
	r.Status, r.Detail = util.StripBidi(r.Status), util.StripBidi(r.Detail)
	return r
}
