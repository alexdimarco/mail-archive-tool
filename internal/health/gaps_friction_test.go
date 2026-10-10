package health

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/state"
)

// FI-S1a friction review fixes (docs/review-fi-s1a-friction.md): what `status`
// and `status -json` now tell the actor. Existing assertions of MA-326 are
// untouched; these add to them.

// extraSource registers another store (token, id) with the same last run as "ann".
func extraSource(in *Input, token, id string) {
	in.GapSource[token] = id
	in.GapLastRun[token] = in.GapLastRun["ann"]
}

// gapListLines are the per-gap lines Summary prints, in order.
func gapListLines(in Input, rep Report) []string {
	var out []string
	for _, l := range Summary(in, rep) {
		if strings.HasPrefix(l, "  ") && (strings.Contains(l, "ann@contoso.com/") || strings.Contains(l, "/data/old.pst/") || strings.Contains(l, "/data/tb-mbox/")) {
			out = append(out, l)
		}
	}
	return out
}

// covers: MA-326, R18, S48
// F3: the 20-line cap and the JSON's 20 items are filled most severe first (stuck,
// terminal, fillable, then not attempted), so a stuck item is never counted but
// unlisted, and its line is marked. Within a class the ledger's order holds.
func TestStatusListsStuckThenTerminalFirstAndMarksStuck(t *testing.T) {
	var gaps []state.CaptureGap
	na := fetchFailed("graph:NA")
	na.Folder, na.LastRun = "Aaa", gapNow.Add(-40*24*time.Hour)
	gaps = append(gaps, na)
	for i := 0; i < 25; i++ {
		g := fetchFailed(fmt.Sprintf("graph:%02d", i))
		g.Folder = "Inbox"
		gaps = append(gaps, g)
	}
	term := state.CaptureGap{Folder: "Old", Locator: "folder:Old", Scope: state.GapScopeFolder, Reason: "folder-unreadable", Class: state.ClassTerminal}
	stuck := fetchFailed("graph:STUCK")
	stuck.Folder, stuck.Runs, stuck.FirstRun = "Zeta", 6, gapNow.Add(-10*24*time.Hour)
	gaps = append(gaps, term, stuck)
	in := gapInput(gaps...)
	rep := Assess(in, gapNow)

	lines := gapListLines(in, rep)
	assure.Reached(t, lines, "the gap lines")
	if len(lines) != 20 {
		t.Fatalf("%d gap lines, want 20", len(lines))
	}
	if !strings.Contains(lines[0], "Zeta") || !strings.Contains(lines[0], "stuck since 2026-08-23") {
		t.Errorf("first gap line %q; want the stuck item, marked 'stuck since 2026-08-23'", lines[0])
	}
	if !strings.Contains(lines[1], "Old") {
		t.Errorf("second gap line %q; want the terminal folder", lines[1])
	}
	for _, l := range lines[2:] {
		if strings.Contains(l, "Aaa") {
			t.Errorf("a not-attempted gap outranked a fillable one: %q", l)
		}
		if strings.Contains(l, "stuck since") {
			t.Errorf("only the stuck gap is marked: %q", l)
		}
	}
	doc := JSON(in, rep)
	if len(doc.CaptureGaps.Items) != 20 || !doc.CaptureGaps.Items[0].Stuck || doc.CaptureGaps.Items[0].Folder != "Zeta" || doc.CaptureGaps.Items[1].Class != state.ClassTerminal {
		t.Errorf("JSON items start %+v; want the stuck item, then the terminal one", doc.CaptureGaps.Items[:2])
	}
	for _, it := range doc.CaptureGaps.Items {
		if it.Folder == "Aaa" {
			t.Error("the not-attempted gap made the capped JSON list ahead of fillable ones")
		}
	}
}

// covers: MA-326, R18, S48
// F4: a WARN headline says each remedy once and labels it with the kind of source
// it is for. "retried" appears once on a fillable line whatever the reasons; a
// terminal line over two source kinds labels each remedy; the stuck line says the
// ITEMS were not captured (the run itself is not what failed).
func TestStatusHeadlinesSayEachRemedyOnceAndLabelTheSource(t *testing.T) {
	transport := fetchFailed("graph:T")
	transport.Reason = "fetch-transport"
	in := gapInput(fetchFailed("graph:A1"), transport)
	rep := Assess(in, gapNow)
	line := rep.Reasons[codeIndex(rep, "capture_gaps_fillable")]
	if n := strings.Count(line, "retried"); n != 1 {
		t.Errorf("'retried' appears %d times in %q; want once", n, line)
	}
	if !strings.Contains(line, "check the network") {
		t.Errorf("the transport remedy was lost: %q", line)
	}

	big := state.CaptureGap{Folder: "Inbox", Locator: "graph:BIG", Reason: "fetch-too-large", Class: state.ClassTerminal, Subject: "Big"}
	pst := state.CaptureGap{Token: "p", Folder: "Inbox", Locator: "pst-nid:u/9", Reason: "pst-message-unreadable", Class: state.ClassTerminal}
	in = gapInput(big, pst)
	extraSource(&in, "p", "path:/data/old.pst")
	rep = Assess(in, gapNow)
	line = rep.Reasons[codeIndex(rep, "capture_gaps_terminal")]
	a, b := strings.Index(line, "[Microsoft 365]"), strings.Index(line, "[.pst file]")
	c, d := strings.Index(line, "cannot hold a message that large"), strings.Index(line, "scanpst.exe")
	if !(a >= 0 && a < c && c < b && b < d) {
		t.Errorf("terminal line must label each remedy with its source kind, in order: %q", line)
	}
	if strings.Count(line, "scanpst.exe") != 1 || strings.Count(line, "cannot hold a message that large") != 1 {
		t.Errorf("a remedy is said more than once: %q", line)
	}

	stuck := fetchFailed("graph:S")
	stuck.Runs, stuck.FirstRun = 6, gapNow.Add(-10*24*time.Hour)
	in = gapInput(stuck)
	rep = Assess(in, gapNow)
	line = rep.Reasons[codeIndex(rep, "capture_gaps_stuck")]
	if !strings.Contains(line, "have failed on every run since 2026-08-23") || !strings.Contains(line, "the rest of the archive is unaffected") {
		t.Errorf("stuck line %q; want the existing words and that only these items are affected", line)
	}
}

// covers: MA-326, R18, S48
// F5: "+N more" counts what is behind it: when the entries left are not all single
// items (an overflow entry stands for thousands) it says entries AND items, and
// every capped list and terminal-only WARN names the report's path when it exists.
func TestStatusMoreLineCountsItemsAndNamesTheReport(t *testing.T) {
	var gaps []state.CaptureGap
	for i := 0; i < 25; i++ {
		g := fetchFailed(fmt.Sprintf("graph:%02d", i))
		g.Folder = "Inbox"
		gaps = append(gaps, g)
	}
	over := state.CaptureGap{Folder: "Zz", Scope: state.GapScopeOverflow, Locator: "overflow", Reason: "overflow", Class: state.ClassFillable, Skipped: 4000}
	gaps = append(gaps, over)
	in := gapInput(gaps...)
	in.ReportExists, in.ReportPath = true, "/a/attachments-report.tsv"
	rep := Assess(in, gapNow)
	more := ""
	for _, l := range Summary(in, rep) {
		if strings.Contains(l, "more") && strings.Contains(l, "see the report") {
			more = l
		}
	}
	assure.Reached(t, more, "the +N more line")
	for _, want := range []string{"+6 more entries", "4005 item(s)", "/a/attachments-report.tsv"} {
		if !strings.Contains(more, want) {
			t.Errorf("more line %q lacks %q", more, want)
		}
	}

	// A terminal-only archive names the report in its WARN too.
	term := state.CaptureGap{Folder: "Inbox", Locator: "graph:BIG", Reason: "fetch-too-large", Class: state.ClassTerminal}
	in = gapInput(term)
	in.ReportExists, in.ReportPath = true, "/a/attachments-report.tsv"
	rep = Assess(in, gapNow)
	if line := rep.Reasons[codeIndex(rep, "capture_gaps_terminal")]; !strings.Contains(line, "/a/attachments-report.tsv") {
		t.Errorf("a terminal-only WARN lacks the report path: %q", line)
	}
}

// covers: MA-326, R18, S48
// F6: the top block says what was not captured, so "Incomplete: none" and
// "Messages: N" are not the whole story: a "Not captured:" line sits above the
// posture with the counts and the report. A healthy archive prints none.
func TestStatusTopBlockHasANotCapturedLine(t *testing.T) {
	healthy := gapInput()
	for _, l := range Summary(healthy, Assess(healthy, gapNow)) {
		if strings.Contains(l, "Not captured") {
			t.Fatalf("a healthy archive prints %q", l)
		}
	}
	stuck := fetchFailed("graph:S")
	stuck.Runs, stuck.FirstRun = 6, gapNow.Add(-10*24*time.Hour)
	term := state.CaptureGap{Folder: "Inbox", Locator: "graph:BIG", Reason: "fetch-too-large", Class: state.ClassTerminal}
	info := state.CaptureGap{Folder: "Inbox", Locator: "ident:x", Reason: "refresh-failed", Class: state.ClassInfo}
	in := gapInput(fetchFailed("graph:A1"), stuck, term, info)
	in.ReportExists, in.ReportPath = true, "/a/attachments-report.tsv"
	lines := Summary(in, Assess(in, gapNow))
	at, posture, msgs := -1, -1, -1
	for i, l := range lines {
		switch {
		case strings.HasPrefix(l, "Not captured:"):
			at = i
		case strings.HasPrefix(l, "Posture:"):
			posture = i
		case strings.HasPrefix(l, "Messages:"):
			msgs = i
		}
	}
	if at < 0 || !(msgs < at && at < posture) {
		t.Fatalf("Not captured line at %d, Messages at %d, Posture at %d; want it between them:\n%s", at, msgs, posture, strings.Join(lines, "\n"))
	}
	for _, want := range []string{"3 item(s)", "1 stuck", "1 need action", "1 retried", "/a/attachments-report.tsv"} {
		if !strings.Contains(lines[at], want) {
			t.Errorf("Not captured line %q lacks %q", lines[at], want)
		}
	}
}

// covers: MA-326, R18, S48
// F11: a user about to lose a Microsoft 365 mailbox reads the deadline remedy in
// `status` and `status -json` from the first sighting, not only in the first run's
// log; a fillable gap of a source that is not Microsoft 365 does not carry it.
func TestStatusFillableGraphGapCarriesTheDeadlineRemedy(t *testing.T) {
	in := gapInput(fetchFailed("graph:A1"))
	rep := Assess(in, gapNow)
	line := rep.Reasons[codeIndex(rep, "capture_gaps_fillable")]
	if !strings.Contains(line, state.GapDeadlineRemedy) {
		t.Errorf("fillable Microsoft 365 line lacks the deadline remedy: %q", line)
	}
	if doc := JSON(in, rep); doc.CaptureGaps.Deadline != state.GapDeadlineRemedy {
		t.Errorf("JSON capture_gaps.deadline = %q", doc.CaptureGaps.Deadline)
	}

	local := state.CaptureGap{Token: "m", Folder: "Inbox", Locator: "file:Inbox/x", Reason: "file-unreadable", Class: state.ClassFillable}
	in = gapInput(local)
	extraSource(&in, "m", "path:/data/tb-mbox")
	rep = Assess(in, gapNow)
	if line := rep.Reasons[codeIndex(rep, "capture_gaps_fillable")]; strings.Contains(line, "Microsoft 365") || strings.Contains(line, "Outlook on the web") {
		t.Errorf("a local-store gap carries the Microsoft 365 deadline: %q", line)
	}
	if doc := JSON(in, rep); doc.CaptureGaps.Deadline != "" {
		t.Errorf("JSON deadline for a local gap: %q", doc.CaptureGaps.Deadline)
	}
}

// covers: MA-326, R18, S48
// F12: every missing item says whether the original still exists: still in the
// mailbox for Microsoft 365, the .pst is the only copy (keep it before repairing),
// still in the source folder for a local store; a deleted item says nothing of the
// kind. The line, the terminal WARN and the JSON item (`original`) agree.
func TestStatusSaysWhetherTheOriginalStillExists(t *testing.T) {
	big := state.CaptureGap{Folder: "Inbox", Locator: "graph:BIG", Reason: "fetch-too-large", Class: state.ClassTerminal, Subject: "Big"}
	pst := state.CaptureGap{Token: "p", Folder: "Inbox", Locator: "pst-nid:u/9", Reason: "pst-message-unreadable", Class: state.ClassTerminal}
	loc := state.CaptureGap{Token: "m", Folder: "Inbox", Locator: "file:Inbox/x", Reason: "file-unreadable", Class: state.ClassTerminal}
	gone := state.CaptureGap{Folder: "Inbox", Locator: "graph:GONE", Reason: "vanished-at-fetch", Class: state.ClassFillable}
	in := gapInput(big, pst, loc, gone)
	extraSource(&in, "p", "path:/data/old.pst")
	extraSource(&in, "m", "path:/data/tb-mbox")
	rep := Assess(in, gapNow)
	term := rep.Reasons[codeIndex(rep, "capture_gaps_terminal")]
	for _, want := range []string{"still in your mailbox", "only copy", "still in the source folder"} {
		if !strings.Contains(term, want) {
			t.Errorf("terminal WARN lacks %q: %q", want, term)
		}
	}
	lines := strings.Join(gapListLines(in, rep), "\n")
	for _, want := range []string{"still in your mailbox", "only copy", "still in the source folder"} {
		if !strings.Contains(lines, want) {
			t.Errorf("gap lines lack %q:\n%s", want, lines)
		}
	}
	got := map[string]string{}
	for _, it := range JSON(in, rep).CaptureGaps.Items {
		got[it.Reason] = it.Original
	}
	if !strings.Contains(got["fetch-too-large"], "mailbox") || !strings.Contains(got["pst-message-unreadable"], "only copy") || !strings.Contains(got["file-unreadable"], "source folder") {
		t.Errorf("JSON original per reason: %v", got)
	}
	if got["vanished-at-fetch"] != "" {
		t.Errorf("a deleted message claims its original exists: %q", got["vanished-at-fetch"])
	}
	if state.GapOriginal("pst-message-unreadable", state.SourceOutlookScratch) != "" || state.GapOriginal("not-a-message-file", state.SourceLocal) != "" {
		t.Error("the scratch .pst (its remedy already says so) or a non-message file got an original clause")
	}
}

// covers: MA-326, R18, S48
// F14: with no last-run record the ledger cannot be judged attempted or not, and
// `status` says so instead of "not in recent runs" (false: there is no run record):
// a terminal gap keeps its WARN (it needs action whatever the last run did), the
// rest read as a note that names the missing record, and the JSON agrees.
func TestStatusWithNoRunRecordSaysSoAndKeepsTheTerminalWarn(t *testing.T) {
	stuck := fetchFailed("graph:S")
	stuck.Runs, stuck.FirstRun = 6, gapNow.Add(-10*24*time.Hour)
	term := state.CaptureGap{Folder: "Inbox", Locator: "graph:BIG", Reason: "fetch-too-large", Class: state.ClassTerminal}
	in := gapInput(stuck, term)
	in.LastRunState, in.LastRun = state.LastRunAbsent, state.LastRun{}
	in.GapLastRun = map[string]time.Time{}
	rep := Assess(in, gapNow)
	if codeIndex(rep, "capture_gaps_terminal") < 0 {
		t.Fatalf("the terminal WARN vanished with the run record: %v", rep.Codes)
	}
	i := codeIndex(rep, "capture_gaps_not_attempted")
	if i < 0 || !strings.Contains(rep.Reasons[i], "no run record") || !strings.Contains(rep.Reasons[i], "1 item(s)") {
		t.Fatalf("note for the other gap: codes %v reasons %v; want one that names the missing run record", rep.Codes, rep.Reasons)
	}
	for _, l := range append(Summary(in, rep), rep.Reasons...) {
		if strings.Contains(l, "not in recent runs") {
			t.Errorf("false claim with no run record: %q", l)
		}
	}
	if doc := JSON(in, rep); doc.CaptureGaps.Terminal != 1 || doc.CaptureGaps.NotAttempted != 1 {
		t.Errorf("JSON terminal %d not_attempted %d; want 1 and 1", doc.CaptureGaps.Terminal, doc.CaptureGaps.NotAttempted)
	}

	// The same through Gather: an archive copied without .mailarchive-lastrun.json.
	out := t.TempDir()
	m, err := state.Load(filepath.Join(out, ".mailarchive-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	tok := m.Token("mailbox:ann@contoso.com", "ann@contoso.com")
	g := term
	g.Token = tok
	m.ObserveGap(g, time.Now().UTC().Add(-time.Hour))
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	real := Gather(out, "")
	if rep := Assess(real, time.Now()); codeIndex(rep, "capture_gaps_terminal") < 0 {
		t.Errorf("Gather with no run record: %v", rep.Codes)
	}
}

// covers: MA-326, R18, S48
// F7: an unparsed record is described once, as raw text: it is not also counted
// "source-empty (never fillable)" in the Incomplete line, and the messages that
// were only partly read are named (their pages), not only counted.
func TestStatusUnparsedIsNotSourceEmptyAndPartlyReadIsNamed(t *testing.T) {
	in := healthyInput(gapNow)
	in.Terminal, in.Unparsed, in.UnparsedTerminal = 1, 1, 1
	text := strings.Join(Summary(in, Assess(in, gapNow)), "\n")
	if strings.Contains(text, "source-empty") && strings.Contains(text, "1 source-empty") {
		t.Errorf("an unparsed record is counted source-empty:\n%s", text)
	}
	if !strings.Contains(text, "Incomplete: none") || !strings.Contains(text, "raw text") {
		t.Errorf("want 'Incomplete: none' and a line saying the message is kept as raw text:\n%s", text)
	}
	in.Terminal = 2 // a genuinely source-empty record beside it
	text = strings.Join(Summary(in, Assess(in, gapNow)), "\n")
	if !strings.Contains(text, "1 source-empty (never fillable)") {
		t.Errorf("the real source-empty record must still count:\n%s", text)
	}

	in = healthyInput(gapNow)
	in.Fillable, in.Unreadable, in.UnreadableOnly = 2, 2, 2
	in.PartlyRead = []string{"s/a.html", "s/b.html"}
	lines := Summary(in, Assess(in, gapNow))
	joined := strings.Join(lines, "\n")
	for _, want := range []string{"Partly read: 2 message(s)", "s/a.html", "s/b.html"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the partly-read list lacks %q:\n%s", want, joined)
		}
	}
	in.Unreadable, in.PartlyRead = 30, nil
	for i := 0; i < 30; i++ {
		in.PartlyRead = append(in.PartlyRead, fmt.Sprintf("s/%02d.html", i))
	}
	joined = strings.Join(Summary(in, Assess(in, gapNow)), "\n")
	if strings.Contains(joined, "s/29.html") || !strings.Contains(joined, "+25 more") {
		t.Errorf("the list is not capped at 5 with '+N more':\n%s", joined)
	}
}

// covers: MA-326, R18, S48
// F7 through Gather: the manifest's unparsed-only and partly-read records reach
// Input, with the pages named and control characters stripped.
func TestGatherReadsUnparsedAndPartlyReadRecords(t *testing.T) {
	out := t.TempDir()
	m, err := state.Load(filepath.Join(out, ".mailarchive-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	m.Add("k1", state.Record{Path: "s/a\x1b.html", Missing: []string{"unreadable:body"}})
	m.Add("k2", state.Record{Path: "s/b.html", Terminal: []string{"unparsed"}})
	m.Add("k3", state.Record{Path: "s/c.html", Terminal: []string{"x.pdf"}})
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	in := Gather(out, "")
	if in.Unparsed != 1 || in.UnparsedTerminal != 1 || in.Terminal != 2 {
		t.Errorf("unparsed %d unparsed-terminal %d terminal %d; want 1, 1, 2", in.Unparsed, in.UnparsedTerminal, in.Terminal)
	}
	if len(in.PartlyRead) != 1 || in.PartlyRead[0] != "s/a.html" {
		t.Errorf("partly read pages %q; want [s/a.html]", in.PartlyRead)
	}
	b, _ := json.Marshal(JSON(in, Assess(in, time.Now())))
	assure.Reached(t, b, "the JSON")
}
