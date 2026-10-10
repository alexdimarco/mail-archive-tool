package health

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/util"
)

// The capture-gap rules of `status` (fault-isolation §4.10). They read the ledger
// the manifest holds (Input.Gaps) and judge only what the data can say: an
// ATTEMPTED gap (observed on its store's last run) drives the WARN and the
// "retried" wording and is the only kind that can be stuck; any other gap is
// "never captured" and a note (owner ruling OR3, FC14). RED stays reserved for
// run-level failure and corruption (FC18): nothing here is ever RED.

// maxGapLines bounds the gap list Summary and the JSON carry (FC22).
const maxGapLines = 20

// schedulePeriod is the period the stuck threshold counts two of: the installed
// schedule's, else a day.
func schedulePeriod(in Input) time.Duration {
	if in.HasDescriptor {
		return IntervalOf(in.Desc.Interval)
	}
	return 24 * time.Hour
}

// gapSourceKind is the kind of source a gap's store is (graph, pst, ...), from
// the store id Gather read; "" when it is not known.
func (in Input) gapSourceKind(g state.CaptureGap) string {
	id, ok := in.GapSource[g.Token]
	if !ok {
		return ""
	}
	return state.SourceKind(id)
}

// gapSourceName is the source as an operator reads it: the mailbox address or the
// input path, else the store token.
func (in Input) gapSourceName(g state.CaptureGap) string {
	if id := in.GapSource[g.Token]; id != "" {
		return state.SourceDisplay(id)
	}
	return util.StripControl(g.Token)
}

// missingGap reports whether a gap is something missing from the archive
// (fillable or terminal); an info gap is an anomaly where nothing is missing.
func missingGap(g state.CaptureGap) bool {
	return g.Class == state.ClassFillable || g.Class == state.ClassTerminal
}

// countGaps phrases a set of gaps as "N item(s)", "F folder(s)" or both: items
// and folders are counted apart (FC23(3)), an overflow entry for the items it
// absorbed.
func countGaps(gaps []state.CaptureGap) string {
	items, folders := 0, 0
	for _, g := range gaps {
		if g.Scope == state.GapScopeFolder {
			folders++
		} else {
			items += g.Weight()
		}
	}
	switch {
	case items > 0 && folders > 0:
		return fmt.Sprintf("%d item(s) and %d folder(s)", items, folders)
	case folders > 0:
		return fmt.Sprintf("%d folder(s)", folders)
	}
	return fmt.Sprintf("%d item(s)", items)
}

// remedyFor names what to do about a set of gaps: the remedy of each distinct
// (reason, source kind, errno class), most numerous first, at most three.
func remedyFor(in Input, gaps []state.CaptureGap, stuck bool) string {
	type key struct{ reason, kind, errno string }
	count := map[key]int{}
	var order []key
	for _, g := range gaps {
		k := key{g.Reason, in.gapSourceKind(g), state.GapErrnoClass(g.Detail)}
		if count[k] == 0 {
			order = append(order, k)
		}
		count[k] += g.Weight()
	}
	sort.SliceStable(order, func(i, j int) bool { return count[order[i]] > count[order[j]] })
	var out []string
	seen := map[string]bool{}
	for _, k := range order {
		text := state.GapRemedy(k.reason, k.kind, k.errno)
		if stuck {
			text = state.GapStuckRemedy(k.reason, k.kind, k.errno)
		}
		if !seen[text] {
			seen[text] = true
			out = append(out, text)
		}
		if len(out) == 3 {
			break
		}
	}
	return strings.Join(out, "; ")
}

// assessCaptureGaps adds the capture-gap reasons to r: stuck (WARN, listed first
// of the gap reasons), fillable (WARN), terminal (WARN), not-attempted (a note).
func assessCaptureGaps(r *Report, in Input, now time.Time) {
	period := schedulePeriod(in)
	var stuck, fillable, terminal, notAttempted []state.CaptureGap
	for _, g := range in.Gaps {
		if !missingGap(g) {
			continue
		}
		switch {
		case !state.GapAttempted(g, in.GapLastRun):
			notAttempted = append(notAttempted, g)
		case g.Class == state.ClassTerminal:
			terminal = append(terminal, g)
		case state.GapStuck(g, now, period):
			stuck = append(stuck, g)
		default:
			fillable = append(fillable, g)
		}
	}
	if len(stuck) > 0 {
		first := stuck[0].FirstRun
		for _, g := range stuck {
			if g.FirstRun.Before(first) {
				first = g.FirstRun
			}
		}
		r.warn("capture_gaps_stuck", fmt.Sprintf("%s have failed on every run since %s — %s",
			countGaps(stuck), first.UTC().Format("2006-01-02"), remedyFor(in, stuck, true)))
	}
	if len(fillable) > 0 {
		when := "whenever the archive runs again"
		if in.HasDescriptor {
			when = "automatically whenever the archive runs again"
		}
		msg := fmt.Sprintf("%s could not be captured on the last run; they are retried %s", countGaps(fillable), when)
		if in.ReportExists {
			msg += fmt.Sprintf(" (see %s)", in.ReportPath)
		}
		r.warn("capture_gaps_fillable", msg+" — "+remedyFor(in, fillable, false))
	}
	if len(terminal) > 0 {
		r.warn("capture_gaps_terminal", fmt.Sprintf("%s are NOT in this archive and will not be without action — %s",
			countGaps(terminal), remedyFor(in, terminal, false)))
	}
	if len(notAttempted) > 0 {
		last := notAttempted[0].LastRun
		sources := map[string]bool{}
		for _, g := range notAttempted {
			if g.LastRun.After(last) {
				last = g.LastRun
			}
			sources[g.Token] = true
		}
		from := "from " + in.gapSourceName(notAttempted[0])
		if len(sources) > 1 {
			from = fmt.Sprintf("from %d sources", len(sources))
		}
		r.note("capture_gaps_not_attempted", fmt.Sprintf("%s %s were never captured (last attempted %s; this source or folder is not in recent runs)",
			countGaps(notAttempted), from, last.UTC().Format("2006-01-02")))
	}
}

// assessPartlyRead adds the reasons for records that were read only in part
// (unreadable_messages, WARN) and for unparsed stubs (unparsed_messages, a note).
func assessPartlyRead(r *Report, in Input) {
	if in.Unreadable > 0 {
		r.warn("unreadable_messages", fmt.Sprintf("%d message(s) could only be partly read from the source — %s",
			in.Unreadable, "repair the data file in place with Outlook's Inbox Repair Tool (scanpst.exe, Windows), then re-run"))
	}
	if in.Unparsed > 0 {
		r.note("unparsed_messages", fmt.Sprintf("%d message(s) could not be parsed and are kept as raw text — the original bytes are in the .eml when the archive is made with -raw; after upgrading, the next run re-examines them once", in.Unparsed))
	}
}

// gapLine is one line of Summary's gap list: source, folder, subject, date,
// reason and remedy, every untrusted part control-stripped.
func gapLine(in Input, g state.CaptureGap, now time.Time) string {
	where := in.gapSourceName(g)
	if g.Folder != "" {
		where += "/" + util.StripControl(g.Folder)
	}
	var b strings.Builder
	b.WriteString("  " + where + ": ")
	switch {
	case g.Scope == state.GapScopeFolder:
		b.WriteString(state.GapWhy(g.Reason))
		if g.Skipped > 0 {
			fmt.Fprintf(&b, " (%d item(s) not read)", g.Skipped)
		}
	case g.Scope == state.GapScopeOverflow:
		fmt.Fprintf(&b, "%d more item(s): %s", g.Skipped, state.GapWhy(g.Reason))
	default:
		if g.Subject != "" {
			fmt.Fprintf(&b, "%q", util.StripControl(g.Subject))
		} else {
			b.WriteString("(no subject)")
		}
		if d := gapDate(g); d != "" {
			b.WriteString(" (" + d + ")")
		}
		b.WriteString(" — " + state.GapWhy(g.Reason))
	}
	attempted := state.GapAttempted(g, in.GapLastRun)
	switch {
	case !attempted:
		fmt.Fprintf(&b, "; never captured (last attempted %s; not in recent runs)", g.LastRun.UTC().Format("2006-01-02"))
	case state.GapStuck(g, now, schedulePeriod(in)):
		b.WriteString("; " + state.GapStuckRemedy(g.Reason, in.gapSourceKind(g), state.GapErrnoClass(g.Detail)))
	default:
		b.WriteString("; " + state.GapRemedy(g.Reason, in.gapSourceKind(g), state.GapErrnoClass(g.Detail)))
	}
	return b.String()
}

// gapDate is a gap's date as YYYY-MM-DD: the item's own date when the producer
// knew it, else the day it was first seen.
func gapDate(g state.CaptureGap) string {
	if t, err := time.Parse(time.RFC3339, g.Received); err == nil {
		return t.UTC().Format("2006-01-02")
	}
	return ""
}

// listedGaps is the gaps Summary and the JSON list: what is missing, in the
// ledger's stable order.
func listedGaps(in Input) []state.CaptureGap {
	var out []state.CaptureGap
	for _, g := range in.Gaps {
		if missingGap(g) {
			out = append(out, g)
		}
	}
	return out
}

// gapLines is the up-to-20 gap list under the reasons (FC22), then "+N more".
func gapLines(in Input, now time.Time) []string {
	gaps := listedGaps(in)
	var lines []string
	for i, g := range gaps {
		if i == maxGapLines {
			lines = append(lines, fmt.Sprintf("  +%d more — see the report", len(gaps)-maxGapLines))
			break
		}
		lines = append(lines, gapLine(in, g, now))
	}
	return lines
}

// JSONGapItem is one entry of capture_gaps.items.
type JSONGapItem struct {
	Source    string `json:"source"`
	Folder    string `json:"folder"`
	Subject   string `json:"subject,omitempty"`
	Date      string `json:"date,omitempty"`
	Scope     string `json:"scope"`
	Reason    string `json:"reason"`
	Class     string `json:"class"`
	Remedy    string `json:"remedy"`
	Runs      int    `json:"runs"`
	Attempted bool   `json:"attempted"`
	Stuck     bool   `json:"stuck"`
}

// JSONCaptureGaps is the capture_gaps facet: the ledger's tally (attempted
// fillable, terminal and info gaps, how many fillable are stuck, how many were
// not attempted on their store's last run, the oldest first observation) and up
// to 20 items.
type JSONCaptureGaps struct {
	Fillable       int           `json:"fillable"`
	Terminal       int           `json:"terminal"`
	Info           int           `json:"info"`
	Stuck          int           `json:"stuck"`
	NotAttempted   int           `json:"not_attempted"`
	OldestFirstRun *time.Time    `json:"oldest_first_run"`
	Items          []JSONGapItem `json:"items"`
}

// jsonCaptureGaps builds the capture_gaps facet, judging stuck at the time the
// Report was assessed (so the JSON and the reasons never disagree).
func jsonCaptureGaps(in Input, rep Report) *JSONCaptureGaps {
	now := rep.now
	if now.IsZero() {
		now = time.Now()
	}
	period := schedulePeriod(in)
	t := state.TallyGaps(in.Gaps, in.GapLastRun, now, period)
	doc := &JSONCaptureGaps{Fillable: t.Fillable, Terminal: t.Terminal, Info: t.Info, Stuck: t.Stuck, NotAttempted: t.NotAttempted, Items: []JSONGapItem{}}
	if !t.Oldest.IsZero() {
		o := t.Oldest.UTC()
		doc.OldestFirstRun = &o
	}
	for i, g := range listedGaps(in) {
		if i == maxGapLines {
			break
		}
		attempted := state.GapAttempted(g, in.GapLastRun)
		stuck := attempted && state.GapStuck(g, now, period)
		remedy := state.GapRemedy(g.Reason, in.gapSourceKind(g), state.GapErrnoClass(g.Detail))
		if stuck {
			remedy = state.GapStuckRemedy(g.Reason, in.gapSourceKind(g), state.GapErrnoClass(g.Detail))
		}
		doc.Items = append(doc.Items, JSONGapItem{
			Source: in.gapSourceName(g), Folder: util.StripControl(g.Folder), Subject: util.StripControl(g.Subject),
			Date: gapDate(g), Scope: g.Scope, Reason: util.StripControl(g.Reason), Class: g.Class, Remedy: remedy,
			Runs: g.Runs, Attempted: attempted, Stuck: stuck,
		})
	}
	return doc
}
