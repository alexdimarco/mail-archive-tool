package health

import (
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/state"
)

// covers: MA-1154, R18, S48
// status's capture-gap lines (and Status & health's, the same lines) count in the words a
// backup's outcome is told in (the 1.6.1 friction review's P4: "Not captured: 1 folder(s) …"
// and "WARN: 1 folder(s) could not be captured …" sat beside "did not save everything"):
// messages and folders, the digits grouped, the verb agreeing ("1 message has", "2 messages
// have"), a folder's unread messages and an overflow entry's count likewise — never "item(s)"
// or "folder(s)". Positive twin first: the counts are still the ledger's, items and folders
// apart.
func TestStatusGapLinesCountInPlainWords(t *testing.T) {
	two := gapInput(fetchFailed("graph:A1"), fetchFailed("graph:A2"))
	if w, one := countGapsN(two.Gaps); w != "2 messages" || one {
		t.Fatalf("the positive twin: %q %v", w, one)
	}
	folder := state.CaptureGap{Folder: "Receipts", Locator: "folder:Receipts", Scope: state.GapScopeFolder, Reason: "folder-unwritable", Class: state.ClassTerminal, Skipped: 3}
	over := state.CaptureGap{Folder: "Big", Locator: "overflow:Big", Scope: state.GapScopeOverflow, Reason: "fetch-failed", Class: state.ClassFillable, Skipped: 4005}
	stuck := fetchFailed("graph:S")
	stuck.Runs, stuck.FirstRun = 6, gapNow.Add(-10*24*time.Hour)
	for _, c := range []struct {
		gaps []state.CaptureGap
		want string
		one  bool
	}{
		{[]state.CaptureGap{folder}, "1 folder", true},
		{[]state.CaptureGap{folder, fetchFailed("graph:A")}, "1 message and 1 folder", false},
		{[]state.CaptureGap{over}, "4,005 messages", false},
	} {
		if w, one := countGapsN(c.gaps); w != c.want || one != c.one {
			t.Errorf("countGapsN = %q %v, want %q %v", w, one, c.want, c.one)
		}
	}

	in := gapInput(stuck, folder, over)
	in.ReportExists, in.ReportPath = true, "/a/attachments-report.tsv"
	rep := Assess(in, gapNow)
	lines := append(Summary(in, rep), rep.Reasons...)
	all := strings.Join(lines, "\n")
	for _, want := range []string{
		"1 message has failed on every run since 2026-08-23 (each run could not capture it;",
		"4,005 messages could not be captured on the last run; they are retried",
		"1 folder is NOT in this archive and will not be without action",
		"(3 messages not read)",
		"4,005 messages more: ",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("status lacks %q:\n%s", want, all)
		}
	}
	one := gapInput(fetchFailed("graph:ONE"))
	if i := codeIndex(Assess(one, gapNow), "capture_gaps_fillable"); i < 0 || !strings.Contains(Assess(one, gapNow).Reasons[i], "1 message could not be captured on the last run; it is retried") {
		t.Errorf("one fillable message: %v", Assess(one, gapNow).Reasons)
	}
	assure.Reached(t, !strings.Contains(all, "item(s)") && !strings.Contains(all, "folder(s)"), "no item(s) or folder(s) in status's gap lines:\n"+all)
}
