package app

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/util"
)

// GapRemedy is what to do about a capture gap, keyed by its reason, the kind of
// source it came from (state.SourceGraph, SourcePST, SourceOutlookScratch,
// SourceLocal) and the errno class of its cause (state.ErrnoPerm, ErrnoIO,
// ErrnoSpace, ErrnoTooLong; "" for any other). It is the ONE table the run
// summary, `status` and the report share, exported so the extraction workspace's
// copy export words the same Microsoft error the same way (FC20, FC23(6)); a
// reason that is the archive's destination ("could not be written") names the
// archive location, never "read". The table itself lives in internal/state,
// below both this package and health, which must not import app.
func GapRemedy(reason, sourceKind, errnoClass string) string {
	return state.GapRemedy(reason, sourceKind, errnoClass)
}

// gapBannerCount is the count the root index.html is generated with and stamped
// with (§4.10, FC24): what the manifest says the source held that this archive
// may not — ledger gaps that are missing (an overflow entry for the items it
// counts), partly-read records and unparsed records. Every pages.Generate caller
// passes it, so a run, a Graph run, reindex and rebuild agree.
func gapBannerCount(m *state.Manifest) int {
	return m.MissingGapCount() + m.UnreadableCount() + m.UnparsedCount()
}

// maxSummaryGapLines bounds the "Not captured" list (the rest is "+N more").
const maxSummaryGapLines = 20

// GapSummary is what a run says about the capture-gap ledger (§4.2): what it
// observed (the fillable and terminal gaps that feed partial; Info is nothing
// missing), how many of those were new this run, how many earlier gaps it closed
// and how, how many fillable gaps are stuck, how many gaps its walk did not
// attempt, and the per-gap lines of the summary. Items and folders are counted
// apart (FC23(3)). Recovered and ClosedGone are the producers' to count (the
// walkers that close a gap: S2a, S3).
type GapSummary struct {
	Observed, New, Recovered, ClosedGone, Fillable, Terminal, Info, Stuck, NotAttempted, ItemGaps, FolderGaps int
	Lines                                                                                                     []string
}

// summarizeGaps reads this run's observations out of the manifest: an entry whose
// LastRun is runAt was observed (attempted) by this run; any other is not
// attempted. out is the archive location, named in the lines of a reason that is
// the destination's fault; period is the schedule period the stuck threshold
// counts two of.
func summarizeGaps(m *state.Manifest, out string, runAt, now time.Time, period time.Duration) GapSummary {
	gaps := m.CaptureGapsSorted()
	var s GapSummary
	if len(gaps) == 0 {
		return s
	}
	last := make(map[string]time.Time, 4)
	for _, g := range gaps {
		last[g.Token] = runAt
	}
	t := state.TallyGaps(gaps, last, now, period)
	s.Fillable, s.Terminal, s.Info, s.Stuck, s.NotAttempted = t.Fillable, t.Terminal, t.Info, t.Stuck, t.NotAttempted
	s.Observed = t.Fillable + t.Terminal

	type row struct {
		source, kind string
		g            state.CaptureGap
	}
	sources, kinds := map[string]string{}, map[string]string{}
	var obs []row
	for _, g := range gaps {
		if !g.LastRun.Equal(runAt) || (g.Class != state.ClassFillable && g.Class != state.ClassTerminal) {
			continue
		}
		src, ok := sources[g.Token]
		if !ok {
			id := m.GapSourceID(g.Token)
			src, kinds[g.Token] = state.SourceDisplay(id), state.SourceKind(id)
			if src == "" {
				src = g.Token
			}
			sources[g.Token] = src
		}
		obs = append(obs, row{src, kinds[g.Token], g})
		if g.Scope == state.GapScopeFolder {
			s.FolderGaps++
		} else {
			s.ItemGaps += g.Weight()
		}
		if g.FirstRun.Equal(runAt) {
			s.New += g.Weight()
		}
	}
	sort.SliceStable(obs, func(i, j int) bool {
		a, b := obs[i], obs[j]
		switch {
		case a.source != b.source:
			return a.source < b.source
		case a.g.Folder != b.g.Folder:
			return a.g.Folder < b.g.Folder
		case a.g.Reason != b.g.Reason:
			return a.g.Reason < b.g.Reason
		}
		return a.g.Locator < b.g.Locator
	})
	for i, r := range obs {
		if i == maxSummaryGapLines {
			s.Lines = append(s.Lines, summaryMoreLine(obs[maxSummaryGapLines:], func(r row) state.CaptureGap { return r.g }))
			break
		}
		s.Lines = append(s.Lines, gapSummaryLine(r.g, r.source, r.kind, out, runAt))
	}
	return s
}

// summaryMoreLine is the line after the capped list, in the words `status` uses
// (friction F5): "+N more" when every entry left is a single item, else it counts
// what the entries stand for (an overflow entry is thousands of items, a folder is
// not an item). The head line above it names the report's path.
func summaryMoreLine[T any](rest []T, gap func(T) state.CaptureGap) string {
	items, folders := 0, 0
	for _, r := range rest {
		g := gap(r)
		if g.Scope == state.GapScopeFolder {
			folders++
		} else {
			items += g.Weight()
		}
	}
	if folders == 0 && items == len(rest) {
		return fmt.Sprintf("  +%d more — see the report", len(rest))
	}
	what := fmt.Sprintf("%d item(s)", items)
	if folders > 0 && items > 0 {
		what = fmt.Sprintf("%d item(s) and %d folder(s)", items, folders)
	} else if folders > 0 {
		what = fmt.Sprintf("%d folder(s)", folders)
	}
	return fmt.Sprintf("  +%d more entries (%s) — see the report", len(rest), what)
}

// destinationWhy words a reason that is the archive location's fault: it names
// the location, never "could not be read" (FC20). "" for any other reason.
func destinationWhy(g state.CaptureGap, out string) string {
	switch g.Reason {
	case "folder-unwritable":
		return "the folder could not be written to " + out
	case "export-failed":
		if state.GapErrnoClass(g.Detail) != "" {
			return "the message could not be written to " + out
		}
	}
	return ""
}

// causeOf is the short cause a line quotes: the clause after the last ": " of an
// error text ("mkdir /x: file name too long" reads "file name too long"),
// control-stripped and bounded.
func causeOf(detail string) string {
	d := util.StripControl(detail)
	if i := strings.LastIndex(d, ": "); i >= 0 {
		d = d[i+2:]
	}
	if r := []rune(d); len(r) > 100 {
		d = string(r[:100]) + "…"
	}
	return d
}

// gapSummaryLine is one line of the "Not captured" block (§4.10): the store's
// source id and folder, then for an item its subject and date, why, the cause,
// and what to do about it. A fillable Microsoft 365 gap seen for the first time
// adds the deadline remedy (FC23(1)): a user about to lose the mailbox need not
// wait for the stuck threshold to learn there is a way to save the item.
func gapSummaryLine(g state.CaptureGap, source, kind, out string, runAt time.Time) string {
	where := source
	if g.Folder != "" {
		where += "/" + util.StripControl(g.Folder)
	}
	why := destinationWhy(g, out)
	if why == "" {
		why = state.GapWhy(g.Reason)
	}
	cause := ""
	if c := causeOf(g.Detail); c != "" {
		cause = " (" + c + ")"
	}
	remedy := state.GapRemedy(g.Reason, kind, state.GapErrnoClass(g.Detail))
	if g.Class == state.ClassFillable && kind == state.SourceGraph && g.FirstRun.Equal(runAt) {
		remedy += ". " + state.GapDeadlineRemedy
	}
	var b strings.Builder
	b.WriteString("  " + where + ": ")
	switch g.Scope {
	case state.GapScopeFolder:
		b.WriteString(why + cause + " — " + remedy)
	case state.GapScopeOverflow:
		fmt.Fprintf(&b, "%d more item(s): %s — %s", g.Skipped, why, remedy)
	default:
		if g.Subject != "" {
			fmt.Fprintf(&b, "%q", util.StripControl(g.Subject))
		} else {
			b.WriteString("(an item with no subject)")
		}
		if t, err := time.Parse(time.RFC3339, g.Received); err == nil {
			b.WriteString(" (" + t.UTC().Format("2006-01-02") + ")")
		}
		b.WriteString(" — " + why + cause + "; " + remedy)
	}
	return b.String()
}

// partial reports whether the run observed anything it could not capture: a
// fillable or terminal gap in the ledger, or a partly-read record written or
// re-examined (§4.1). Unparsed stubs and info gaps do not make a run partial.
func (r Result) partial() bool { return r.Gaps.Observed > 0 || r.Stats.Unreadable > 0 }

// ErrPartial matches (errors.Is) the error of a run that finished its walk but
// observed an item or folder it could not capture, with no run-level failure and
// no cancel (§4.9). S1a maps it to today's failure: every caller that has not
// been updated treats it as an error (exit 1, last-run failed, status run_failed
// RED), never as success; FI-S1b adds the `partial` flag and the partial wording.
var ErrPartial = errors.New("partial run")

// PartialError is the error of a partial run: what it could not capture and the
// report that lists it.
type PartialError struct {
	Summary    GapSummary
	ReportPath string
	Unreadable int // partly-read records the run wrote or re-examined
	// Encrypted says the archive is an encrypted one, which keeps no report: the
	// text points at MailArchive instead of attachments-report.tsv.
	Encrypted bool
}

func (e *PartialError) Error() string {
	var what []string
	if e.Summary.ItemGaps > 0 {
		what = append(what, fmt.Sprintf("%d item(s)", e.Summary.ItemGaps))
	}
	if e.Summary.FolderGaps > 0 {
		what = append(what, fmt.Sprintf("%d folder(s)", e.Summary.FolderGaps))
	}
	text := "partial:"
	if len(what) > 0 {
		text += " " + strings.Join(what, " and ") + " not captured"
	}
	if e.Unreadable > 0 {
		if len(what) > 0 {
			text += ";"
		}
		text += fmt.Sprintf(" %d message(s) only partly read", e.Unreadable)
	}
	if e.Encrypted {
		return text + " — open the archive in MailArchive to see which"
	}
	return text + " — see attachments-report.tsv"
}

// Is makes errors.Is(err, ErrPartial) true for a PartialError.
func (e *PartialError) Is(target error) bool { return target == ErrPartial }

// partialOutcome is the error a run that finished without a run-level failure
// returns: a *PartialError when it observed something it could not capture, else
// nil. A run-level failure and a cancel never reach it — they return their own
// error first, so precedence is failure over partial over complete (§4.9).
func partialOutcome(r Result) error {
	if !r.Partial {
		return nil
	}
	return &PartialError{Summary: r.Gaps, ReportPath: r.ReportPath, Unreadable: r.Stats.Unreadable, Encrypted: r.encrypted}
}
