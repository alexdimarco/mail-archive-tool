package app

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/outlookcom"
	"mail-archive-tool/internal/state"
)

// covers: MA-320, R1, R6, R10, R5, R12, S46
// The `-outlook` export-skip mapping (FC28), by unit test on every OS (the Windows
// plus classic Outlook case is the L row MA-504, pending): a folder whose CopyTo failed
// and an account Outlook could not export are each an outlook-export-skipped folder
// gap under the token of the PST they belong to — fillable, locator
// outlook:<account>/<folder> — so the run is partial; an account that is already a
// .pst is an info outlook-account-skipped gap naming the path to archive directly,
// and alone it is no reason for a partial run. A later run whose export carries the
// folder closes its gap when that PST's walk completes, and the one whose PST was
// never walked stays listed. (The skipped folder is one the fixture PST holds, Drafts:
// the next run's export carries it only if the walked file has it, S2A-AI4 — a file
// without the folder, walked without the skip, no longer closes the gap.)
func TestOutlookExportSkipsBecomeGaps(t *testing.T) {
	data := pstFixtureData(t)
	dir := tmpDir(t)
	pstDir := filepath.Join(dir, "_outlook-pst")
	ann := writeData(t, pstDir, "ann@contoso.com.pst", data)
	if got := outlookcom.PSTPath(pstDir, "ann@contoso.com"); got != ann {
		t.Fatalf("PSTPath = %q; want %q", got, ann)
	}
	skips := []outlookcom.Skip{
		{Account: "ann@contoso.com", Folder: "Drafts", Path: ann, Reason: "CopyTo: 0x80040119"},
		{Account: "bob@contoso.com", Path: outlookcom.PSTPath(pstDir, "bob@contoso.com"), Reason: "could not create the PST"},
		{Account: "Old data", Path: "/data/old.pst", AlreadyPST: true, Reason: "already a .pst file"},
	}

	// The mapping alone.
	gaps := outlookGaps(skips)
	if len(gaps) != 3 {
		t.Fatalf("%d gaps; want 3", len(gaps))
	}
	if g := gaps[0].gap; g.Reason != "outlook-export-skipped" || g.Class != state.ClassFillable || g.Scope != state.GapScopeFolder || g.Folder != "Drafts" || g.Locator != "outlook:ann@contoso.com/Drafts" {
		t.Errorf("folder skip -> %+v", g)
	}
	if g := gaps[1].gap; g.Reason != "outlook-export-skipped" || g.Class != state.ClassFillable || g.Folder != "" || g.Locator != "outlook:bob@contoso.com/" {
		t.Errorf("account skip -> %+v", g)
	}
	if g := gaps[2].gap; g.Reason != "outlook-account-skipped" || g.Class != state.ClassInfo || !strings.Contains(g.Detail, "/data/old.pst") {
		t.Errorf("already-a-.pst -> %+v", g)
	}

	// Through a run: the folder and account skips make it partial.
	out := tmpDir(t)
	opts := Options{Inputs: []string{ann}, Out: out, Mode: export.Incremental, OutlookSkips: skips}
	res, err := Run(context.Background(), opts, discard(), nil)
	if !errors.Is(err, ErrPartial) || !res.Partial {
		t.Fatalf("run with skips: err %v, partial %v; want ErrPartial", err, res.Partial)
	}
	if loadArchive(t, out).Len() != 17 {
		t.Errorf("%d records; the PST that was exported is archived whole", loadArchive(t, out).Len())
	}
	got := ledgerOf(t, out)
	byReason := map[string][]state.CaptureGap{}
	for _, g := range got {
		byReason[g.Reason] = append(byReason[g.Reason], g)
	}
	if len(byReason["outlook-export-skipped"]) != 2 || len(byReason["outlook-account-skipped"]) != 1 || len(got) != 3 {
		t.Fatalf("ledger %+v; want two outlook-export-skipped and one outlook-account-skipped", got)
	}
	if res.Gaps.Fillable != 2 || res.Gaps.Info != 1 || res.Gaps.Observed != 2 || res.Gaps.FolderGaps != 2 {
		t.Errorf("summary %+v; want the two fillable folder gaps observed and the info one apart", res.Gaps)
	}
	if !strings.Contains(strings.Join(res.Gaps.Lines, "\n"), "open the folder in Outlook, then run again") {
		t.Errorf("summary lines %q lack the remedy", res.Gaps.Lines)
	}
	tokens := map[string]bool{}
	for _, g := range got {
		tokens[g.Token] = true
	}
	if len(tokens) != 3 {
		t.Errorf("%d tokens for the three stores; each gap sits under its own PST's token", len(tokens))
	}

	// The info gap alone is no partial run.
	clean := tmpDir(t)
	res, err = Run(context.Background(), Options{Inputs: []string{ann}, Out: clean, OutlookSkips: skips[2:]}, discard(), nil)
	if err != nil || res.Partial || len(ledgerOf(t, clean)) != 1 {
		t.Errorf("only the already-a-.pst account: err %v, partial %v, %d gaps; want nil, false, 1 info gap", err, res.Partial, len(ledgerOf(t, clean)))
	}

	// The next run's export carries the folder: its gap closes when ann's PST is walked,
	// bob's (never walked) and the info one are retained.
	res, err = Run(context.Background(), Options{Inputs: []string{ann}, Out: out, Mode: export.Incremental}, discard(), nil)
	if err != nil || res.Partial {
		t.Fatalf("next run: err %v, partial %v", err, res.Partial)
	}
	left := ledgerOf(t, out)
	if len(left) != 2 || res.Gaps.ClosedGone != 1 {
		t.Errorf("after the export carried the folder: %d gaps, closed %d; want the folder gap closed and bob's account and the info gap kept", len(left), res.Gaps.ClosedGone)
	}
	for _, g := range left {
		if strings.Contains(g.Locator, "Drafts") {
			t.Errorf("the carried folder's gap %+v is still listed", g)
		}
	}
	assure.Reached(t, left, "the gaps of the PSTs never walked")
}
