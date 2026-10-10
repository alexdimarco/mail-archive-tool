package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

// covers: MA-327, R1, R7, R19, S48
// F10: the inheritor's way from the root page to a remedy has no dead end. The
// banner (its words unchanged) is followed by a link to the report and its count,
// and names `mailarchive status -out .`; each ledger row of the report ends with
// why it happened and what to do (the header is unchanged), including whether the
// original still exists; README.txt names the same command.
func TestInheritorPathFromBannerToRemedy(t *testing.T) {
	const banner = "Some items from the original mail source may not be in this archive — see attachments-report.tsv"
	src := filepath.Join(tmpDir(t), "Inbox")
	maildirMessages(t, src, 2)
	out := tmpDir(t)
	logger := log.New(io.Discard, "", 0)
	opts := Options{Inputs: []string{src}, Out: out, Mode: export.Incremental, Index: true, Pages: true}
	if _, err := Run(context.Background(), opts, logger, nil); err != nil {
		t.Fatal(err)
	}
	mpath := filepath.Join(out, ".mailarchive-manifest.json")
	m, err := state.Load(mpath)
	if err != nil {
		t.Fatal(err)
	}
	var tok string
	for _, tk := range m.Stores {
		tok = tk
	}
	m.ObserveGap(state.CaptureGap{Token: tok, Folder: "Inbox", Scope: state.GapScopeItem, Locator: "file:Inbox/x", Reason: "file-unreadable", Class: state.ClassTerminal,
		Subject: "Lost", Detail: "open x: permission denied"}, time.Now().UTC().Add(-time.Hour))
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Reindex(out, logger); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(out, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	page := string(b)
	for _, want := range []string{banner, `href="attachments-report.tsv"`, "1 item(s)", "mailarchive status -out ."} {
		if !strings.Contains(page, want) {
			t.Errorf("the root page lacks %q", want)
		}
	}

	path, rows := writeReport(out, m, false, logger)
	if rows != 1 {
		t.Fatalf("%d report rows, want 1", rows)
	}
	lines := readLines(t, path)
	if lines[0] != "kind\tclass\tfolder\tdate\tsubject\tdetail\tpath" {
		t.Errorf("the report header changed: %q", lines[0])
	}
	detail := strings.Split(lines[1], "\t")[5]
	for _, want := range []string{"why: the message file could not be read", "still in the source folder", "what to do: fix the permissions on the file"} {
		if !strings.Contains(detail, want) {
			t.Errorf("the row's detail lacks %q: %q", want, detail)
		}
	}
	if !strings.Contains(strings.Join(strings.Fields(archiveReadme), " "), `"mailarchive status -out ."`) {
		t.Error("README.txt does not name `mailarchive status -out .`")
	}
}

// covers: MA-327, R1, R7, R19, S48
// F9: verify's "Not in this archive" line drops a zero part ("0 folder(s)"), says
// how many of the missing items are retried and how many need action (and where
// to read what to do), and says that "attested" means the files held are intact,
// not that the archive is complete. The existing line's words, the verdict and the
// exit code stay (TestVerifyPrintsTheNotInThisArchiveLine); `verify -json` gains
// the split additively; a healthy archive's output has none of it.
func TestVerifyNotInThisArchiveSplitsRetryableFromNeedsAction(t *testing.T) {
	src := filepath.Join(tmpDir(t), "Inbox")
	maildirMessages(t, src, 2)
	out := tmpDir(t)
	logger := log.New(io.Discard, "", 0)
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Mode: export.Incremental}, logger, nil); err != nil {
		t.Fatal(err)
	}
	healthy, err := Verify(out, VerifyOptions{}, logger, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range VerifySummary(healthy) {
		if strings.Contains(l, "Not in this archive") || strings.Contains(l, "complete") {
			t.Fatalf("a healthy archive's verify mentions gaps: %q", l)
		}
	}

	mpath := filepath.Join(out, ".mailarchive-manifest.json")
	m, _ := state.Load(mpath)
	var tok string
	for _, tk := range m.Stores {
		tok = tk
	}
	run := time.Now().UTC().Add(-time.Hour)
	m.ObserveGap(state.CaptureGap{Token: tok, Folder: "Inbox", Scope: state.GapScopeItem, Locator: "file:a", Reason: "file-unreadable", Class: state.ClassFillable}, run)
	m.ObserveGap(state.CaptureGap{Token: tok, Folder: "Inbox", Scope: state.GapScopeItem, Locator: "file:b", Reason: "file-unreadable", Class: state.ClassTerminal}, run)
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	rep, err := Verify(out, VerifyOptions{}, logger, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rep.ExitCode() != 0 || !rep.Attested() || rep.GapRetryable != 1 || rep.GapNeedAction != 1 {
		t.Fatalf("exit %d attested %v retryable %d need-action %d; want 0, true, 1, 1", rep.ExitCode(), rep.Attested(), rep.GapRetryable, rep.GapNeedAction)
	}
	lines := VerifySummary(rep)
	joined := strings.Join(lines, "\n")
	if strings.Contains(joined, "0 folder(s)") {
		t.Errorf("a zero part is printed:\n%s", joined)
	}
	for _, want := range []string{"Not in this archive: 2 item(s) from the source could not be captured", "1 retried whenever the archive runs again", "1 need action", "mailarchive status -out", "means the files this archive holds are intact, not that the archive is complete"} {
		if !strings.Contains(joined, want) {
			t.Errorf("verify's summary lacks %q:\n%s", want, joined)
		}
	}
	at, verdict := -1, -1
	for i, l := range lines {
		if strings.Contains(l, "not that the archive is complete") {
			at = i
		}
		if strings.HasPrefix(l, "attested:") {
			verdict = i
		}
	}
	if at < 0 || verdict < 0 || at > verdict {
		t.Errorf("the 'not complete' sentence (line %d) must come before the verdict (line %d)", at, verdict)
	}
	data, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"gap_retryable":1`) || !strings.Contains(string(data), `"gap_need_action":1`) {
		t.Errorf("verify -json lacks the split: %s", data)
	}
}

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
	if !strings.Contains(logText, "separate archive tree") {
		t.Errorf("run log lost its full repair line: %s", logText)
	}
	if strings.Contains(e, "separate archive tree") {
		t.Errorf("the returned error (printed twice more) repeats the long remedy: %q", e)
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
