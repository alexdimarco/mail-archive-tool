package app

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/state"
)

const (
	reportName       = "attachments-report.tsv"
	legacyReportName = "attachments-report-legacy.tsv"
)

// writeReport regenerates <out>/attachments-report.tsv from the manifest (R1):
// one row per fillable or terminal gap and per unresolved inline reference,
// classed and in a stable order, then one row per capture-gap ledger entry (an
// item or folder that left no record, with its store's source id), so nothing
// found by an earlier run is lost and a filled gap disappears. With no record
// issue and an empty ledger the file is removed. On the
// first run over a migrated (version-1) manifest, a report left by the old
// per-run writer is preserved as attachments-report-legacy.tsv. It returns the
// report path ("" when there is nothing to report) and the row count.
func writeReport(out string, manifest *state.Manifest, migrated bool, logger *log.Logger) (string, int) {
	path := filepath.Join(out, reportName)
	if migrated {
		legacy := filepath.Join(out, legacyReportName)
		if _, err := os.Stat(path); err == nil {
			if _, lerr := os.Stat(legacy); errors.Is(lerr, fs.ErrNotExist) {
				if rerr := os.Rename(path, legacy); rerr == nil {
					logger.Printf("kept the earlier verification report as %s", legacy)
				}
			}
		}
	}

	// The report is written when there is something to say: a record with an issue,
	// or a gap in the capture-gap ledger — a ledger-only archive (the common F9
	// case: items that failed and left no record) gets its report too (FC24).
	issues := manifest.Issues()
	gaps := manifest.CaptureGapsSorted()
	if len(issues) == 0 && len(gaps) == 0 {
		os.Remove(path)
		return "", 0
	}

	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	fmt.Fprintln(w, "kind\tclass\tfolder\tdate\tsubject\tdetail\tpath")
	rows := 0
	emit := func(kind, class string, r state.Record, detail string) {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			kind, class, tsv(r.Folder), r.Date, tsv(r.Subject), tsv(detail), r.Path)
		rows++
	}
	for _, r := range issues {
		for _, item := range r.Missing {
			emit(gapKind(item), state.RecordGapClass(item, false), r, gapDetail(item))
		}
		for _, item := range r.Terminal {
			emit(gapKind(item), state.RecordGapClass(item, true), r, gapDetail(item))
		}
		for _, cid := range r.Unresolved {
			emit("unresolved-inline-image", "info", r, cid)
		}
		if r.BodyFallback != "" {
			emit("html-shown-as-source", "info", r, r.BodyFallback)
		}
	}
	rows += writeGapRows(w, out, manifest, gaps)
	w.Flush()
	if err := export.WriteFileAtomic(path, buf.Bytes()); err != nil {
		logger.Printf("warning: could not write verification report: %v", err)
		return "", rows
	}
	return path, rows
}

func gapKind(item string) string {
	switch item {
	case state.MissingBody:
		return "missing-body"
	case state.UnknownSentinel:
		return "unknown"
	case state.LabelUnreadableBody:
		return "unreadable-body"
	case state.LabelUnreadableAttachments:
		return "unreadable-attachments"
	case state.LabelUnparsed:
		return "unparsed-message"
	case state.LabelSuperseded:
		return "superseded"
	default:
		return "empty-attachment"
	}
}

// writeGapRows writes one row per capture-gap ledger entry after the record rows
// (§4.10): kind = the reason, class, folder, date (the item's own, else the day
// the gap was first seen), subject, a detail that reads locator · detail · runs
// since first observation (with the number of items a folder or overflow row did
// not archive, and "not attempted since" for a gap its store's last run did not
// attempt, FC14) and, in the path column, the store's SOURCE ID — the mailbox
// address or the input path, from the reverse of Stores (FC22). The order is
// source, folder, reason, locator. Every field goes through tsv(): the ledger is
// untrusted on read, and a row is one line. It returns how many rows it wrote.
func writeGapRows(w *bufio.Writer, out string, manifest *state.Manifest, gaps []state.CaptureGap) int {
	if len(gaps) == 0 {
		return 0
	}
	lastRun, lrState, _ := state.ReadLastRun(out)
	type row struct {
		source string
		g      state.CaptureGap
	}
	sources := map[string]string{}
	rowsOf := make([]row, 0, len(gaps))
	for _, g := range gaps {
		src, ok := sources[g.Token]
		if !ok {
			src = state.SourceDisplay(manifest.GapSourceID(g.Token))
			if src == "" {
				src = g.Token
			}
			sources[g.Token] = src
		}
		rowsOf = append(rowsOf, row{src, g})
	}
	sort.SliceStable(rowsOf, func(i, j int) bool {
		a, b := rowsOf[i], rowsOf[j]
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
	for _, r := range rowsOf {
		g := r.g
		date := g.Received
		if date == "" {
			date = g.FirstRun.UTC().Format(time.RFC3339)
		}
		parts := []string{g.Locator}
		if g.Detail != "" {
			parts = append(parts, g.Detail)
		}
		switch {
		case g.Scope == state.GapScopeOverflow:
			parts = append(parts, fmt.Sprintf("%d item(s) in this folder not archived", g.Skipped))
		case g.Scope == state.GapScopeFolder && g.Skipped > 0:
			parts = append(parts, fmt.Sprintf("%d item(s) in this folder not archived", g.Skipped))
		}
		parts = append(parts, fmt.Sprintf("%d run(s) since %s", g.Runs, g.FirstRun.UTC().Format("2006-01-02")))
		if lrState == state.LastRunPresent && !g.LastRun.Equal(lastRun.Started) {
			parts = append(parts, "not attempted since "+g.LastRun.UTC().Format(time.RFC3339))
		}
		// Why, and what to do, in the words every surface shares (X10), with whether
		// the original still exists: an inheritor with no tool reads this and nothing
		// else (friction F10). A fillable Microsoft 365 row adds the deadline.
		kind := state.SourceKind(manifest.GapSourceID(g.Token))
		why := state.GapWhy(g.Reason)
		if o := state.GapOriginal(g.Reason, kind); o != "" {
			why += " (" + o + ")"
		}
		remedy := state.GapRemedy(g.Reason, kind, state.GapErrnoClass(g.Detail))
		if g.Class == state.ClassFillable && kind == state.SourceGraph {
			remedy += ". " + state.GapDeadlineRemedy
		}
		parts = append(parts, "why: "+why, "what to do: "+remedy)
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			tsv(g.Reason), tsv(g.Class), tsv(g.Folder), tsv(date), tsv(g.Subject), tsv(strings.Join(parts, " · ")), tsv(r.source))
	}
	return len(rowsOf)
}

// gapDetail is the human-readable detail column for a gap. The body gap is
// stored as the bare token "body"; the report shows "(message body)" so the row
// reads for a person, while attachment gaps keep their file name verbatim.
func gapDetail(item string) string {
	if item == state.MissingBody {
		return "(message body)"
	}
	return item
}

// tsv keeps one report row on one line: every control character (tab, LF,
// CR, and the rest of C0/C1) in an untrusted field becomes a space.
func tsv(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			return ' '
		}
		return r
	}, s)
}
