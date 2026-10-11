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

// kindLabel names a source kind in a headline ("" for an unknown one).
func kindLabel(kind string) string {
	switch kind {
	case state.SourceGraph:
		return "Microsoft 365"
	case state.SourcePST:
		return ".pst file"
	case state.SourceOutlookScratch:
		return "Outlook export"
	case state.SourceLocal:
		return "local mail store"
	}
	return ""
}

// retryTail is the sentence the fillable headline already says once ("they are
// retried ..."), so a remedy that ends with it, or is only it, drops it there.
const retryTail = "it is retried when you run again"

// remedyFor names what to do about a set of gaps in a headline: the most numerous
// remedy of each source kind, said once, labelled with the kind when more than one
// kind is present (a remedy holds "; " and " — " itself, so a label is what tells
// which source it is for). withOriginal adds whether the original still exists
// (friction F12); dropRetry leaves out the "retried" sentence a fillable headline
// has already said (friction F4). "" when nothing is left to say.
func remedyFor(in Input, gaps []state.CaptureGap, stuck, withOriginal, dropRetry bool) string {
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
	var kinds []string
	byKind := map[string][]key{}
	for _, k := range order {
		if _, ok := byKind[k.kind]; !ok {
			kinds = append(kinds, k.kind)
		}
		byKind[k.kind] = append(byKind[k.kind], k)
	}
	type picked struct{ text, original string }
	var out []string
	for _, kind := range kinds {
		// The most numerous remedy that still has something to say is the one named;
		// any other distinct remedy of this kind is counted, and listed below.
		var first picked
		seen := map[string]bool{}
		others := 0
		for _, k := range byKind[kind] {
			text := state.GapRemedy(k.reason, k.kind, k.errno)
			if stuck {
				text = state.GapStuckRemedy(k.reason, k.kind, k.errno)
			}
			if dropRetry {
				text = strings.TrimSuffix(text, "; "+retryTail)
				if text == retryTail {
					text = ""
				}
			}
			orig := ""
			if withOriginal {
				orig = state.GapOriginal(k.reason, k.kind)
			}
			if text == "" && orig == "" || seen[text+"\x00"+orig] {
				continue
			}
			seen[text+"\x00"+orig] = true
			if first.text == "" && first.original == "" {
				first = picked{text, orig}
			} else {
				others++
			}
		}
		text := first.text
		if first.original != "" {
			if text != "" {
				text += " (" + first.original + ")"
			} else {
				text = first.original
			}
		}
		if others > 0 {
			text += fmt.Sprintf(" (+%d other cause(s): see the list below)", others)
		}
		if text == "" {
			continue
		}
		if len(kinds) > 1 && kindLabel(kind) != "" {
			text = "[" + kindLabel(kind) + "] " + text
		}
		out = append(out, text)
	}
	return strings.Join(out, " | ")
}

// noRunRecord reports that no last-run record vouches for the ledger (the file is
// absent or unreadable, as in a copy that skipped dotfiles): whether a gap was
// attempted cannot be judged then (friction F14).
func (in Input) noRunRecord() bool {
	return in.LastRunState != state.LastRunPresent && len(in.GapLastRun) == 0
}

// attempted is state.GapAttempted, except that with no run record a terminal gap
// counts as attempted: it needs action whatever the last run did, so it keeps its
// WARN; any other gap cannot be judged and reads as not attempted.
func (in Input) attempted(g state.CaptureGap) bool {
	if in.noRunRecord() {
		return g.Class == state.ClassTerminal
	}
	return state.GapAttempted(g, in.GapLastRun)
}

// tally is state.TallyGaps with the same no-run-record rule as attempted.
func (in Input) tally(now time.Time, period time.Duration) state.GapTally {
	t := state.TallyGaps(in.Gaps, in.GapLastRun, now, period)
	if in.noRunRecord() {
		for _, g := range in.Gaps {
			if g.Class == state.ClassTerminal {
				t.Terminal += g.Weight()
				t.NotAttempted -= g.Weight()
			}
		}
	}
	return t
}

// noRecordText is the clause that replaces "not in recent runs" when there is no
// run record to say whether a run still reaches the gap.
func (in Input) noRecordText() string {
	if in.LastRunState == state.LastRunUnreadable {
		return "no readable run record"
	}
	return "no run record"
}

// gapRank orders the listed gaps most severe first: stuck, terminal, fillable,
// then not attempted (friction F3).
func gapRank(in Input, g state.CaptureGap, now time.Time) int {
	switch {
	case !in.attempted(g):
		return 3
	case g.Class == state.ClassTerminal:
		return 1
	case state.GapStuck(g, now, schedulePeriod(in)):
		return 0
	}
	return 2
}

// deletionTrackingNote is the sentence a stuck gap adds when its item could not be
// matched to any archived record (it had no listing Message-ID, or none that
// matched, CaptureGap.DefersGone): while the gap persists the mailbox's
// gone-detection is deferred for the records it could hide, so a message deleted
// in the folder is not noticed. It names the folders and how to end it: after
// saving a copy, move or delete the item out of the folder in the mailbox (§4.10,
// FC23(4), owner ruling OR10). "" when no stuck gap defers anything.
func deletionTrackingNote(stuck []state.CaptureGap) string {
	seen := map[string]bool{}
	var parts []string
	for _, g := range stuck {
		if !g.DefersGone || seen[g.Folder] {
			continue
		}
		seen[g.Folder] = true
		f := util.StripControl(g.Folder)
		parts = append(parts, fmt.Sprintf("deleted messages in %s are not being detected while this persists; after saving a copy, move or delete the item out of %s in the mailbox to resume deletion tracking there", f, f))
	}
	sort.Strings(parts)
	if len(parts) == 0 {
		return ""
	}
	return ". " + strings.Join(parts, "; ")
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
		case !in.attempted(g):
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
		r.warn("capture_gaps_stuck", fmt.Sprintf("%s have failed on every run since %s (each run could not capture them; the rest of the archive is unaffected) — %s%s",
			countGaps(stuck), first.UTC().Format("2006-01-02"), remedyFor(in, stuck, true, false, false), deletionTrackingNote(stuck)))
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
		// Each remedy once, the retry sentence not repeated (F4), and a user about to
		// lose a Microsoft 365 mailbox told how to save the item now (F11).
		if rem := remedyFor(in, fillable, false, false, true); rem != "" {
			msg += " — " + rem
		}
		if in.anyKind(fillable, state.SourceGraph) {
			msg += ". " + state.GapDeadlineRemedy
		}
		r.warn("capture_gaps_fillable", msg)
	}
	if len(terminal) > 0 {
		msg := fmt.Sprintf("%s are NOT in this archive and will not be without action", countGaps(terminal))
		if in.ReportExists {
			msg += fmt.Sprintf(" (see %s)", in.ReportPath)
		}
		// A terminal gap never escalates to stuck, so the deferral of gone-detection it
		// may carry is named here, or it would be named for no terminal gap (S3-AI3).
		r.warn("capture_gaps_terminal", msg+" — "+remedyFor(in, terminal, false, true, false)+deletionTrackingNote(terminal))
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
		if in.noRunRecord() {
			r.note("capture_gaps_not_attempted", fmt.Sprintf("%s %s were never captured; there is %s (.mailarchive-lastrun.json), so whether a run is still retrying them cannot be judged — run mailarchive against the source again",
				countGaps(notAttempted), from, in.noRecordText()))
		} else {
			r.note("capture_gaps_not_attempted", fmt.Sprintf("%s %s were never captured (last attempted %s; this source or folder is not in recent runs)",
				countGaps(notAttempted), from, last.UTC().Format("2006-01-02")))
		}
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
	// Why, and whether the original still exists (F12). A gap that has failed on
	// every run for the stuck threshold is worded for what it has become (S3-AI5).
	stuck := in.attempted(g) && state.GapStuck(g, now, schedulePeriod(in))
	why := state.GapWhy(g.Reason)
	if stuck {
		why = state.GapStuckWhy(g.Reason, in.gapSourceKind(g))
	}
	if o := state.GapOriginal(g.Reason, in.gapSourceKind(g)); o != "" {
		why += " (" + o + ")"
	}
	var b strings.Builder
	b.WriteString("  " + where + ": ")
	switch {
	case g.Scope == state.GapScopeFolder:
		b.WriteString(why)
		if g.Skipped > 0 {
			fmt.Fprintf(&b, " (%d item(s) not read)", g.Skipped)
		}
	case g.Scope == state.GapScopeOverflow:
		fmt.Fprintf(&b, "%d more item(s): %s", g.Skipped, why)
	default:
		if g.Subject != "" {
			fmt.Fprintf(&b, "%q", util.StripControl(g.Subject))
		} else {
			b.WriteString("(no subject)")
		}
		if d := gapDate(g); d != "" {
			b.WriteString(" (" + d + ")")
		}
		b.WriteString(" — " + why)
	}
	switch {
	case !in.attempted(g) && in.noRunRecord():
		fmt.Fprintf(&b, "; never captured (%s, so whether a run still retries it cannot be judged)", in.noRecordText())
	case !in.attempted(g):
		fmt.Fprintf(&b, "; never captured (last attempted %s; not in recent runs)", g.LastRun.UTC().Format("2006-01-02"))
	case state.GapStuck(g, now, schedulePeriod(in)):
		b.WriteString("; stuck since " + g.FirstRun.UTC().Format("2006-01-02") + ": " + state.GapStuckRemedy(g.Reason, in.gapSourceKind(g), state.GapErrnoClass(g.Detail)))
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

// listedGaps is the gaps Summary and the JSON list: what is missing, most severe
// first (stuck, terminal, fillable, not attempted) and in the ledger's stable
// order within each, so the cap never hides the worst one (friction F3).
func listedGaps(in Input, now time.Time) []state.CaptureGap {
	var out []state.CaptureGap
	for _, g := range in.Gaps {
		if missingGap(g) {
			out = append(out, g)
		}
	}
	rank := make(map[string]int, len(out))
	for _, g := range out {
		rank[state.GapKey(g.Token, g.Scope, g.Locator)] = gapRank(in, g, now)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return rank[state.GapKey(out[i].Token, out[i].Scope, out[i].Locator)] < rank[state.GapKey(out[j].Token, out[j].Scope, out[j].Locator)]
	})
	return out
}

// anyKind reports whether any of gaps is of the given source kind.
func (in Input) anyKind(gaps []state.CaptureGap, kind string) bool {
	for _, g := range gaps {
		if in.gapSourceKind(g) == kind {
			return true
		}
	}
	return false
}

// moreLine is the line after the capped list. It counts what is behind it: when
// the entries left are all single items it says "+N more"; otherwise (an overflow
// entry stands for thousands, a folder is not an item) it says entries and what
// they stand for. It names the report's path when the report exists (friction F5).
func moreLine(in Input, rest []state.CaptureGap) string {
	n := len(rest)
	what := fmt.Sprintf("+%d more", n)
	if counted := countGaps(rest); counted != fmt.Sprintf("%d item(s)", n) {
		what = fmt.Sprintf("+%d more entries (%s)", n, counted)
	}
	line := "  " + what + " — see the report"
	if in.ReportExists {
		line += " (" + in.ReportPath + ")"
	}
	return line
}

// gapLines is the up-to-20 gap list under the reasons (FC22), then "+N more".
func gapLines(in Input, now time.Time) []string {
	gaps := listedGaps(in, now)
	var lines []string
	for i, g := range gaps {
		if i == maxGapLines {
			lines = append(lines, moreLine(in, gaps[maxGapLines:]))
			break
		}
		lines = append(lines, gapLine(in, g, now))
	}
	return lines
}

// notCapturedLine is Summary's "Not captured:" line (friction F6): what the ledger
// says the source holds that this archive does not, split by what the reader can
// do about it, with the report's path. "" when nothing is missing, so a healthy
// archive prints none. The count is the ledger's (items and folders); the records
// the Incomplete line counts are a separate tally.
func notCapturedLine(in Input, now time.Time) string {
	gaps := listedGaps(in, now)
	if len(gaps) == 0 {
		return ""
	}
	var retried, stuck, action, judged int
	for _, g := range gaps {
		w := g.Weight()
		switch rank := gapRank(in, g, now); {
		case rank == 0:
			stuck += w
		case rank == 1:
			action += w
		case rank == 2:
			retried += w
		default:
			judged += w
		}
	}
	var parts []string
	for _, p := range []struct {
		n    int
		text string
	}{{retried, "retried on each run"}, {stuck, "stuck"}, {action, "need action"}} {
		if p.n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", p.n, p.text))
		}
	}
	if judged > 0 {
		if in.noRunRecord() {
			parts = append(parts, fmt.Sprintf("%d cannot be judged (%s)", judged, in.noRecordText()))
		} else {
			parts = append(parts, fmt.Sprintf("%d not tried on the last run", judged))
		}
	}
	line := fmt.Sprintf("Not captured: %s from the source (%s)", countGaps(gaps), strings.Join(parts, ", "))
	if in.ReportExists {
		line += " — " + in.ReportPath
	}
	return line
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
	Original  string `json:"original,omitempty"` // whether the original still exists (friction F12); omitted when nothing true can be said
	Runs      int    `json:"runs"`
	Attempted bool   `json:"attempted"`
	Stuck     bool   `json:"stuck"`
}

// JSONCaptureGaps is the capture_gaps facet: the ledger's tally (attempted
// fillable, terminal and info gaps, how many fillable are stuck, how many were
// not attempted on their store's last run, the oldest first observation) and up
// to 20 items.
type JSONCaptureGaps struct {
	Fillable       int        `json:"fillable"`
	Terminal       int        `json:"terminal"`
	Info           int        `json:"info"`
	Stuck          int        `json:"stuck"`
	NotAttempted   int        `json:"not_attempted"`
	OldestFirstRun *time.Time `json:"oldest_first_run"`
	// Deadline is the sentence for a user who is about to lose a Microsoft 365
	// mailbox, present while an attempted fillable gap is of a Microsoft 365 source
	// (friction F11); omitted otherwise.
	Deadline string        `json:"deadline,omitempty"`
	Items    []JSONGapItem `json:"items"`
}

// jsonCaptureGaps builds the capture_gaps facet, judging stuck at the time the
// Report was assessed (so the JSON and the reasons never disagree).
func jsonCaptureGaps(in Input, rep Report) *JSONCaptureGaps {
	now := rep.now
	if now.IsZero() {
		now = time.Now()
	}
	period := schedulePeriod(in)
	t := in.tally(now, period)
	doc := &JSONCaptureGaps{Fillable: t.Fillable, Terminal: t.Terminal, Info: t.Info, Stuck: t.Stuck, NotAttempted: t.NotAttempted, Items: []JSONGapItem{}}
	if !t.Oldest.IsZero() {
		o := t.Oldest.UTC()
		doc.OldestFirstRun = &o
	}
	for _, g := range in.Gaps {
		if g.Class == state.ClassFillable && in.attempted(g) && in.gapSourceKind(g) == state.SourceGraph {
			doc.Deadline = state.GapDeadlineRemedy
			break
		}
	}
	for i, g := range listedGaps(in, now) {
		if i == maxGapLines {
			break
		}
		attempted := in.attempted(g)
		stuck := attempted && state.GapStuck(g, now, period)
		remedy := state.GapRemedy(g.Reason, in.gapSourceKind(g), state.GapErrnoClass(g.Detail))
		if stuck {
			remedy = state.GapStuckRemedy(g.Reason, in.gapSourceKind(g), state.GapErrnoClass(g.Detail))
		}
		doc.Items = append(doc.Items, JSONGapItem{
			Source: in.gapSourceName(g), Folder: util.StripControl(g.Folder), Subject: util.StripControl(g.Subject),
			Date: gapDate(g), Scope: g.Scope, Reason: util.StripControl(g.Reason), Class: g.Class, Remedy: remedy,
			Original: state.GapOriginal(g.Reason, in.gapSourceKind(g)),
			Runs:     g.Runs, Attempted: attempted, Stuck: stuck,
		})
	}
	return doc
}
