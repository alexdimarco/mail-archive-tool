package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/state"
)

// summaryFixture is a manifest with a Microsoft 365 mailbox, a .pst and a local
// maildir store, and the run's timestamp.
func summaryFixture(t *testing.T) (m *state.Manifest, mailbox, pst, local string, runAt time.Time) {
	t.Helper()
	m, err := state.Load(filepath.Join(tmpDir(t), ".mailarchive-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	return m, m.Token("mailbox:ann@contoso.com", "ann@contoso.com"), m.Token("path:/data/old.pst", "Old"),
		m.Token("path:/mail/Inbox", "Inbox"), time.Now().UTC().Truncate(time.Second)
}

// covers: MA-328, R18, R12, S48
// summarizeGaps counts what THIS run observed (fillable and terminal gaps feed
// partial; an info gap is nothing missing; a gap the run did not touch is not
// attempted) and words each line with the store's source id, the folder, the
// subject and date, why, and the remedy keyed by source kind and errno. The
// deadline remedy shows at the FIRST sighting of a fillable Microsoft 365 gap and
// not after; a destination-side reason names the archive location, never "read".
func TestSummarizeGapsWordsTheNotCapturedBlock(t *testing.T) {
	m, mailbox, pst, local, runAt := summaryFixture(t)
	old := runAt.Add(-72 * time.Hour)
	m.ObserveGap(state.CaptureGap{Token: mailbox, Folder: "Inbox", Scope: state.GapScopeItem, Locator: "graph:NEW", Reason: "fetch-failed", Class: state.ClassFillable,
		Subject: "Quarterly report", Received: "2026-03-02T09:00:00Z", Detail: "500 ErrorMimeContentConversionFailed"}, runAt)
	m.ObserveGap(state.CaptureGap{Token: mailbox, Folder: "Inbox", Scope: state.GapScopeItem, Locator: "graph:OLD", Reason: "fetch-failed", Class: state.ClassFillable,
		Subject: "Older report", Received: "2026-02-02T09:00:00Z"}, old)
	m.ObserveGap(state.CaptureGap{Token: mailbox, Folder: "Inbox", Scope: state.GapScopeItem, Locator: "graph:OLD", Reason: "fetch-failed", Class: state.ClassFillable}, runAt)
	m.ObserveGap(state.CaptureGap{Token: pst, Folder: "Projects/2009", Scope: state.GapScopeFolder, Locator: "folder:Projects/2009", Reason: "folder-unreadable", Class: state.ClassTerminal}, runAt)
	m.ObserveGap(state.CaptureGap{Token: local, Folder: "長いフォルダ名", Scope: state.GapScopeFolder, Locator: "folder:long", Reason: "folder-unwritable", Class: state.ClassFillable,
		Detail: "mkdir /archive/x: file name too long", Skipped: 3}, runAt)
	m.ObserveGap(state.CaptureGap{Token: mailbox, Folder: "Inbox", Scope: state.GapScopeItem, Locator: "ident:I", Reason: "refresh-failed", Class: state.ClassInfo}, runAt)
	m.ObserveGap(state.CaptureGap{Token: mailbox, Folder: "Sent", Scope: state.GapScopeItem, Locator: "graph:SKIPPED", Reason: "fetch-failed", Class: state.ClassFillable}, old)

	g := summarizeGaps(m, "/archive", runAt, runAt, 24*time.Hour)
	if g.Observed != 4 || g.Fillable != 3 || g.Terminal != 1 || g.Info != 1 || g.NotAttempted != 1 || g.ItemGaps != 2 || g.FolderGaps != 2 || g.New != 3 {
		t.Errorf("counts = %+v; want observed 4 (fillable 3, terminal 1), info 1, not-attempted 1, 2 items and 2 folders, 3 new (all but graph:OLD)", g)
	}
	if len(g.Lines) != 4 {
		t.Fatalf("%d lines, want 4 (the info and not-attempted gaps are not listed): %q", len(g.Lines), g.Lines)
	}
	newLine := assure.Reached(t, findLine(g.Lines, "Quarterly report"), "the new gap's line")
	for _, want := range []string{"ann@contoso.com/Inbox:", `"Quarterly report"`, "(2026-03-02)", "Microsoft 365 returned an error", "500 ErrorMimeContentConversionFailed", "retried when you run again", state.GapDeadlineRemedy} {
		if !strings.Contains(newLine, want) {
			t.Errorf("the new gap's line lacks %q:\n%s", want, newLine)
		}
	}
	oldLine := assure.Reached(t, findLine(g.Lines, "Older report"), "the old gap's line")
	if strings.Contains(oldLine, state.GapDeadlineRemedy) {
		t.Errorf("the deadline remedy shows only at the first sighting, but an older gap carries it:\n%s", oldLine)
	}
	if l := findLine(g.Lines, "old.pst"); !strings.Contains(l, "scanpst.exe") || !strings.HasPrefix(strings.TrimSpace(l), "/data/old.pst/Projects/2009:") {
		t.Errorf("the .pst folder's line must lead with the input path and give the in-place repair:\n%s", l)
	}
	dest := assure.Reached(t, findLine(g.Lines, "長いフォルダ名"), "the unwritable folder's line")
	for _, want := range []string{"/mail/Inbox/長いフォルダ名:", "could not be written to /archive", "file name too long", "rename it in your mail app"} {
		if !strings.Contains(dest, want) {
			t.Errorf("the destination-side line lacks %q:\n%s", want, dest)
		}
	}
	if strings.Contains(dest, "read") {
		t.Errorf("a destination-side reason must name the archive location, never 'read':\n%s", dest)
	}

	// Partial: a fillable or terminal gap observed this run, or a partly-read
	// record; an info gap alone is not.
	if !(Result{Gaps: g}).partial() {
		t.Error("a run that observed fillable and terminal gaps is not partial")
	}
	infoOnly := GapSummary{Info: 2}
	if (Result{Gaps: infoOnly}).partial() {
		t.Error("an info gap made the run partial; it is nothing missing")
	}
	if !(Result{Stats: export.Stats{Unreadable: 1}}).partial() || (Result{}).partial() {
		t.Error("a partly-read record makes a run partial; a run with nothing does not")
	}
}

func findLine(lines []string, needle string) string {
	for _, l := range lines {
		if strings.Contains(l, needle) {
			return l
		}
	}
	return ""
}

// covers: MA-328, R18, R12, S48
// The block lists at most 20 gaps and then says "+N more — see the report".
func TestSummarizeGapsCapsTheListAtTwenty(t *testing.T) {
	m, mailbox, _, _, runAt := summaryFixture(t)
	for i := 0; i < 25; i++ {
		m.ObserveGap(state.CaptureGap{Token: mailbox, Folder: "Inbox", Scope: state.GapScopeItem, Locator: fmt.Sprintf("graph:%02d", i), Reason: "fetch-failed", Class: state.ClassFillable, Subject: fmt.Sprintf("Subject %02d", i)}, runAt)
	}
	g := summarizeGaps(m, "/archive", runAt, runAt, 24*time.Hour)
	if g.Observed != 25 || len(g.Lines) != 21 || g.Lines[20] != "  +5 more — see the report" {
		t.Errorf("observed %d, %d lines, last %q; want 25, 21 and '  +5 more — see the report'", g.Observed, len(g.Lines), g.Lines[len(g.Lines)-1])
	}
}

// covers: MA-328, R18, R12, S48
// A partial run is a typed error that the run's caller can find with errors.Is, is
// never mistaken for a cancel, reads as the one line the last-run record and the
// CLI show, and is recorded as today's failure (S1a: exit 1, last-run failed,
// status run_failed RED) with its gap counts and the attention sidecar.
func TestPartialRunIsATypedFailure(t *testing.T) {
	g := GapSummary{Observed: 4, New: 2, Fillable: 3, Terminal: 1, ItemGaps: 3, FolderGaps: 1}
	err := partialOutcome(Result{Gaps: g, Partial: true, ReportPath: "/a/attachments-report.tsv"})
	var pe *PartialError
	if !errors.Is(err, ErrPartial) || !errors.As(err, &pe) || errors.Is(err, context.Canceled) {
		t.Fatalf("partialOutcome = %v; want a *PartialError matching ErrPartial and not context.Canceled", err)
	}
	if got, want := err.Error(), "partial: 3 item(s) and 1 folder(s) not captured — see attachments-report.tsv"; got != want {
		t.Errorf("error text %q, want %q", got, want)
	}
	if pe.ReportPath != "/a/attachments-report.tsv" || pe.Summary.New != 2 {
		t.Errorf("PartialError = %+v", pe)
	}
	if partialOutcome(Result{Gaps: GapSummary{Info: 1}}) != nil {
		t.Error("a run with nothing missing returned a partial error")
	}
	if got := partialOutcome(Result{Partial: true, Stats: export.Stats{Unreadable: 2}}).Error(); got != "partial: 2 message(s) only partly read — see attachments-report.tsv" {
		t.Errorf("partly-read-only error text %q", got)
	}

	// recordRun finalizes it as failed with the one-line error and the gap counts,
	// and drops the attention sidecar.
	out := tmpDir(t)
	opts := Options{Out: out}
	lr := beginRun(opts)
	res := Result{Gaps: g, Partial: true, Stats: export.Stats{Unreadable: 1}}
	runErr := error(&PartialError{Summary: g, ReportPath: "/a/attachments-report.tsv"})
	recordRun(opts, lr, &res, &runErr, log.New(&bytes.Buffer{}, "", 0))
	rec, st, rerr := state.ReadLastRun(out)
	if rerr != nil || st != state.LastRunPresent {
		t.Fatalf("no last-run record: %v", rerr)
	}
	if rec.Status != state.RunFailed || !strings.HasPrefix(rec.Error, "partial: 3 item(s) and 1 folder(s) not captured") {
		t.Errorf("a partial run recorded %q %q; want failed with the one-line summary (S1a maps partial to failure)", rec.Status, rec.Error)
	}
	if rec.GapsObserved != 4 || rec.GapsNew != 2 || rec.GapsFillable != 3 || rec.GapsTerminal != 1 || rec.Unreadable != 1 {
		t.Errorf("gap counts in the record: %+v", rec)
	}
	if _, err := os.Stat(filepath.Join(out, state.AttentionName)); err != nil {
		t.Errorf("a partial run (recorded failed) left no attention sidecar: %v", err)
	}
}

// covers: MA-328, R18, R12, S48
// Run puts the run's own timestamp on the exporter and in the summary, so a gap
// observed by the run is attempted and counted; a healthy run is not partial.
func TestHealthyRunIsNotPartialAndSummarisesNothing(t *testing.T) {
	src := filepath.Join(tmpDir(t), "Inbox")
	maildirMessages(t, src, 2)
	out := tmpDir(t)
	res, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Mode: export.Incremental}, log.New(&bytes.Buffer{}, "", 0), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Partial || res.Gaps.Observed != 0 || len(res.Gaps.Lines) != 0 {
		t.Errorf("a healthy run: partial %v gaps %+v", res.Partial, res.Gaps)
	}
	rec, _, _ := state.ReadLastRun(out)
	if rec.Status != state.RunOK || rec.GapsObserved != 0 {
		t.Errorf("last run %+v", rec)
	}
}

// covers: MA-328, R18, R12, S48
// A .pst that fails to open appends the in-place repair remedy to the run log's
// error line (extends MA-282): repair it in place, or put the repaired copy at the
// SAME path, because a copy from another path starts a separate archive tree. A
// .ost keeps its own remedy and any other source gets none.
func TestPSTOpenFailureNamesTheInPlaceRepair(t *testing.T) {
	dir := tmpDir(t)
	bad := filepath.Join(dir, "old.pst")
	if err := os.WriteFile(bad, []byte("this is not a PST file"), 0o644); err != nil {
		t.Fatal(err)
	}
	var logbuf bytes.Buffer
	_, err := Run(context.Background(), Options{Inputs: []string{bad}, Out: tmpDir(t), Mode: export.Incremental}, log.New(&logbuf, "", 0), nil)
	if err == nil {
		t.Fatal("a corrupt .pst did not fail the run")
	}
	text := logbuf.String()
	for _, want := range []string{"error: " + bad, "scanpst.exe", "in place", "same path", "separate archive tree"} {
		if !strings.Contains(text, want) {
			t.Errorf("the run log lacks %q:\n%s", want, text)
		}
	}
	if got := dataFileRemedy("x.ost"); got != ostOpenRemedy("x.ost") || got == "" {
		t.Errorf("a .ost keeps its own remedy: %q", got)
	}
	if dataFileRemedy("inbox.mbox") != "" {
		t.Error("an mbox source got a .pst remedy")
	}
	if !strings.Contains(GapRemedy("pst-message-unreadable", state.SourcePST, ""), "scanpst.exe") {
		t.Error("app.GapRemedy does not give the .pst remedy")
	}
}
