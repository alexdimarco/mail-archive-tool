package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/state"
)

// FI-S1a friction review fixes (docs/review-fi-s1a-friction.md): the app-level
// ones. Each test asserts what the actor now sees.

// covers: MA-328, R18, R12, S48
// F5: the run summary's "+N more" counts what is behind it, in `status`'s words: an
// overflow entry standing for 4000 items is not "1 more". With only single items
// left it keeps its plain form.
func TestSummaryMoreLineCountsItemsNotEntries(t *testing.T) {
	m, mailbox, _, _, runAt := summaryFixture(t)
	for i := 0; i < 25; i++ {
		m.ObserveGap(state.CaptureGap{Token: mailbox, Folder: "Inbox", Scope: state.GapScopeItem, Locator: fmt.Sprintf("graph:%02d", i), Reason: "fetch-failed", Class: state.ClassFillable, Subject: fmt.Sprintf("S%02d", i)}, runAt)
	}
	// The ledger makes an overflow entry itself when a lifetime cap is hit; put one
	// in directly rather than fill a cap.
	m.CaptureGaps[state.GapKey(mailbox, state.GapScopeOverflow, "overflow:Zz")] = state.CaptureGap{Token: mailbox, Folder: "Zz", Scope: state.GapScopeOverflow,
		Locator: "overflow:Zz", Reason: "overflow", Class: state.ClassFillable, Skipped: 4000, FirstRun: runAt, LastRun: runAt, Runs: 1}
	g := summarizeGaps(m, "/archive", runAt, runAt, 24*time.Hour)
	if len(g.Lines) != 21 || g.Lines[20] != "  +6 more entries (4005 item(s)) — see the report" {
		t.Errorf("%d lines, last %q; want 21 and '  +6 more entries (4005 item(s)) — see the report'", len(g.Lines), g.Lines[len(g.Lines)-1])
	}
}

// covers: MA-328, R18, S48
// F1: the .pst open remedy follows the cause. A permission or in-use error says to
// fix the permissions or close Outlook, never "a damaged .pst can usually be
// repaired" (a repair tool cannot help a file it cannot open); a parse or format
// error keeps the in-place repair (the positive twin).
func TestPSTOpenRemedyFollowsTheCause(t *testing.T) {
	repair := func(s string) bool { return strings.Contains(s, "scanpst.exe") && strings.Contains(s, "damaged .pst") }
	parse := dataFileRemedyFor("a.pst", errors.New("unsupported format type"))
	if !repair(parse) {
		t.Fatalf("positive twin: a format error lost the repair advice: %q", parse)
	}
	for name, err := range map[string]error{
		"permission denied": &fs.PathError{Op: "open", Path: "a.pst", Err: fs.ErrPermission},
		"errno text":        errors.New("open a.pst: permission denied"),
		"windows access":    errors.New("open a.pst: Access is denied."),
		"in use on windows": errors.New("open a.pst: The process cannot access the file because it is being used by another process."),
		"sharing violation": errors.New("open a.pst: sharing violation"),
	} {
		got := dataFileRemedyFor("a.pst", err)
		if repair(got) || got == "" {
			t.Errorf("%s: %q must not offer the repair, and must say something", name, got)
		}
		if !strings.Contains(got, "permission") && !strings.Contains(got, "close Outlook") {
			t.Errorf("%s: %q names neither the permissions nor Outlook", name, got)
		}
	}
	if got := dataFileRemedyFor("a.pst", errors.New("open a.pst: no such file or directory")); repair(got) || !strings.Contains(got, "path") {
		t.Errorf("a missing .pst: %q; want the path to be checked, not a repair", got)
	}
	if got := dataFileRemedyFor("x.ost", errors.New("unsupported format type")); got != ostOpenRemedy("x.ost") {
		t.Errorf("an .ost keeps its own remedy: %q", got)
	}
	if dataFileRemedyFor("inbox.mbox", errors.New("permission denied")) != "" {
		t.Error("an mbox source got a .pst remedy")
	}
}

// covers: MA-328, R18, S48
// F1 and F2 end to end: a .pst that cannot be opened (mode 000) fails the run, and
// the returned error, the last-run record and the attention file all name the file
// and the remedy for THAT cause (fix the permissions), so `status` and the sidecar
// are not a dead end; the run log does not call the file damaged. A junk .pst
// (positive twin) carries the repair advice into the same surfaces.
func TestUnopenablePSTNamesFileAndRemedyOnEverySurface(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("a mode-000 file is still readable here")
	}
	dir := tmpDir(t)
	junk := filepath.Join(dir, "junk.pst")
	locked := filepath.Join(dir, "locked.pst")
	for _, p := range []string{junk, locked} {
		if err := os.WriteFile(p, []byte("this is not a PST file"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o644) })

	run := func(in string) (string, string, string) {
		out := tmpDir(t)
		var logbuf bytes.Buffer
		_, err := Run(context.Background(), Options{Inputs: []string{in}, Out: out, Mode: export.Incremental}, log.New(&logbuf, "", 0), nil)
		assure.Reached(t, err, "the run error")
		rec, st, rerr := state.ReadLastRun(out)
		if rerr != nil || st != state.LastRunPresent {
			t.Fatalf("no last-run record: %v", rerr)
		}
		if rec.Status != state.RunFailed || rec.Error != err.Error() {
			t.Errorf("record %q %q, error %q", rec.Status, rec.Error, err)
		}
		side, _ := os.ReadFile(filepath.Join(out, state.AttentionName))
		return err.Error(), logbuf.String(), string(side)
	}

	e, logText, side := run(junk)
	for _, s := range []struct{ where, text string }{{"error", e}, {"sidecar", side}} {
		if !strings.Contains(s.text, junk) || !strings.Contains(s.text, "scanpst.exe") {
			t.Errorf("junk .pst, %s lacks the file or the repair: %q", s.where, s.text)
		}
	}
	if !strings.Contains(logText, "scanpst.exe") {
		t.Errorf("run log lost its repair line: %s", logText)
	}

	e, logText, side = run(locked)
	for _, s := range []struct{ where, text string }{{"error", e}, {"sidecar", side}} {
		if !strings.Contains(s.text, locked) || !strings.Contains(s.text, "permission") {
			t.Errorf("locked .pst, %s lacks the file or the permission remedy: %q", s.where, s.text)
		}
		if strings.Contains(s.text, "scanpst") || strings.Contains(s.text, "damaged .pst") {
			t.Errorf("locked .pst, %s offers a repair for a file that is not damaged: %q", s.where, s.text)
		}
	}
	if strings.Contains(logText, "damaged") || strings.Contains(logText, "scanpst") {
		t.Errorf("run log calls a permission error damage: %s", logText)
	}
	if strings.ContainsAny(e, "\r\n") {
		t.Errorf("the run error must be one line: %q", e)
	}
	if !strings.Contains(e, "1 file(s) failed") {
		t.Errorf("the count is gone from %q", e)
	}
	_ = state.RunFailed
}
