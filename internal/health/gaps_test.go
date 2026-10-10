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

var gapNow = time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)

// gapInput is a healthy archive with one Microsoft 365 mailbox whose last run
// started at gapNow-1h; each gap is observed (attempted) by that run unless a
// test says otherwise.
func gapInput(gaps ...state.CaptureGap) Input {
	in := healthyInput(gapNow)
	last := gapNow.Add(-time.Hour)
	in.LastRun.Started = last
	in.GapLastRun = map[string]time.Time{"ann": last}
	in.GapSource = map[string]string{"ann": "mailbox:ann@contoso.com"}
	for i := range gaps {
		if gaps[i].Token == "" {
			gaps[i].Token = "ann"
		}
		if gaps[i].LastRun.IsZero() {
			gaps[i].LastRun = last
		}
		if gaps[i].FirstRun.IsZero() {
			gaps[i].FirstRun = last
		}
		if gaps[i].Runs == 0 {
			gaps[i].Runs = 1
		}
		if gaps[i].Scope == "" {
			gaps[i].Scope = state.GapScopeItem
		}
	}
	in.Gaps = gaps
	return in
}

func fetchFailed(loc string) state.CaptureGap {
	return state.CaptureGap{Folder: "Inbox", Locator: loc, Reason: "fetch-failed", Class: state.ClassFillable,
		Subject: "Quarterly report", Received: "2026-03-02T09:00:00Z", Detail: "500 ErrorMimeContentConversionFailed"}
}

func codeIndex(rep Report, code string) int {
	for i, c := range rep.Codes {
		if c == code {
			return i
		}
	}
	return -1
}

// covers: MA-326, R18, S48
// status's capture-gap rules (§4.10): a fillable gap is a WARN that says it is
// retried whenever the archive runs again, and says "automatically" only when a
// schedule is installed; the gap's remedy is in the line.
func TestStatusFillableGapIsWarnAndAutomaticallyOnlyWithASchedule(t *testing.T) {
	in := gapInput(fetchFailed("graph:A1"), fetchFailed("graph:A2"))
	rep := Assess(in, gapNow)
	i := codeIndex(rep, "capture_gaps_fillable")
	if rep.Posture != "WARN" || i < 0 {
		t.Fatalf("a fillable gap: posture %s codes %v; want WARN capture_gaps_fillable", rep.Posture, rep.Codes)
	}
	line := rep.Reasons[i]
	if !strings.Contains(line, "2 item(s)") || !strings.Contains(line, "retried automatically whenever the archive runs again") {
		t.Errorf("with a schedule installed the line must say 2 items and 'automatically': %q", line)
	}
	in.HasDescriptor = false
	line = Assess(in, gapNow).Reasons[codeIndex(Assess(in, gapNow), "capture_gaps_fillable")]
	if strings.Contains(line, "automatically") || !strings.Contains(line, "retried whenever the archive runs again") {
		t.Errorf("with no schedule the line must not promise 'automatically': %q", line)
	}
}

// covers: MA-326, R18, S48
// A gap attempted on several runs escalates to WARN capture_gaps_stuck (owner ruling
// OR5: Runs >= 3 and 72 h or two schedule periods), listed FIRST and never RED
// (FC18), with the Microsoft 365 remedy; only an attempted gap can be stuck, so a
// gap whose store or folder was not walked on its last run reads as a note, never
// stuck, whatever its Runs.
func TestStatusStuckIsWarnListedFirstNeverRed(t *testing.T) {
	stuck := fetchFailed("graph:STUCK")
	stuck.Runs, stuck.FirstRun = 6, gapNow.Add(-10*24*time.Hour)
	fresh := fetchFailed("graph:FRESH")
	fresh.Folder = "Sent"
	in := gapInput(stuck, fresh)
	rep := Assess(in, gapNow)
	if rep.Posture != "WARN" {
		t.Fatalf("a stuck gap made the posture %s, want WARN (never RED): %v", rep.Posture, rep.Reasons)
	}
	if i := codeIndex(rep, "capture_gaps_stuck"); i != 0 {
		t.Fatalf("capture_gaps_stuck is at position %d of %v; want it listed first", i, rep.Codes)
	}
	line := rep.Reasons[0]
	for _, want := range []string{"1 item(s) have failed on every run since 2026-08-23", "Microsoft 365 will not export this item", "save a copy from Outlook on the web", "stays listed while the item remains in the mailbox"} {
		if !strings.Contains(line, want) {
			t.Errorf("the stuck line lacks %q: %q", want, line)
		}
	}
	if i := codeIndex(rep, "capture_gaps_fillable"); i < 0 || !strings.Contains(rep.Reasons[i], "1 item(s)") {
		t.Errorf("the other, not-yet-stuck gap is still the fillable line: %v", rep.Reasons)
	}

	// Not attempted on its token's last run: a note, never stuck, never a WARN.
	in = gapInput(stuck)
	in.Gaps[0].LastRun = gapNow.Add(-30 * 24 * time.Hour)
	rep = Assess(in, gapNow)
	if rep.Posture != "GREEN" || codeIndex(rep, "capture_gaps_stuck") >= 0 || codeIndex(rep, "capture_gaps_fillable") >= 0 {
		t.Fatalf("a gap that was not attempted: posture %s codes %v; want GREEN and no stuck/fillable rule", rep.Posture, rep.Codes)
	}
	i := codeIndex(rep, "capture_gaps_not_attempted")
	if i < 0 {
		t.Fatalf("no capture_gaps_not_attempted note: %v", rep.Codes)
	}
	for _, want := range []string{"1 item(s)", "ann@contoso.com", "never captured", "last attempted 2026-08-03", "not in recent runs"} {
		if !strings.Contains(rep.Reasons[i], want) {
			t.Errorf("the not-attempted note lacks %q: %q", want, rep.Reasons[i])
		}
	}
	if strings.HasPrefix(rep.Reasons[i], "WARN") || strings.HasPrefix(rep.Reasons[i], "RED") {
		t.Errorf("the not-attempted gap must be a note: %q", rep.Reasons[i])
	}
}

// covers: MA-326, R18, S48
// A terminal gap is a WARN whose remedy is keyed by the source kind (§4.10): a
// .pst says to repair it in place with scanpst, a scratch .pst that -outlook
// rebuilds says to open the item in Outlook, never "repair the .pst".
func TestStatusTerminalGapRemedyIsKeyedBySourceKind(t *testing.T) {
	terminal := state.CaptureGap{Folder: "Inbox", Locator: "pst-nid:u/9", Reason: "pst-message-unreadable", Class: state.ClassTerminal, Detail: "node unreadable"}
	cases := []struct {
		source  string
		want    string
		forbids string
	}{
		{"path:/data/old.pst", "scanpst.exe", ""},
		{"path:/archive/_outlook-pst/ann.pst", "open it in Outlook", "scanpst"},
	}
	for _, c := range cases {
		in := gapInput(terminal)
		in.GapSource["ann"] = c.source
		rep := Assess(in, gapNow)
		i := codeIndex(rep, "capture_gaps_terminal")
		if rep.Posture != "WARN" || i < 0 {
			t.Fatalf("%s: posture %s codes %v; want WARN capture_gaps_terminal", c.source, rep.Posture, rep.Codes)
		}
		line := rep.Reasons[i]
		if !strings.Contains(line, "NOT in this archive") || !strings.Contains(line, c.want) || (c.forbids != "" && strings.Contains(line, c.forbids)) {
			t.Errorf("%s: terminal line %q; want 'NOT in this archive' and %q (not %q)", c.source, line, c.want, c.forbids)
		}
	}
}

// covers: MA-326, R18, S48
// A partly-read record is its own WARN (unreadable_messages) and is excluded from
// incomplete_content's text and count, while the JSON `fillable` keeps its meaning:
// every fillable record. Unparsed stubs are a note that moves no posture.
func TestStatusPartlyReadRecordsAreTheirOwnWarn(t *testing.T) {
	in := healthyInput(gapNow)
	in.Fillable, in.Unreadable, in.UnreadableOnly = 2, 2, 2
	rep := Assess(in, gapNow)
	if rep.Posture != "WARN" || codeIndex(rep, "unreadable_messages") < 0 || codeIndex(rep, "incomplete_content") >= 0 {
		t.Fatalf("two partly-read records: posture %s codes %v; want WARN unreadable_messages and no incomplete_content", rep.Posture, rep.Codes)
	}
	if line := rep.Reasons[codeIndex(rep, "unreadable_messages")]; !strings.Contains(line, "2 message(s) could only be partly read") || !strings.Contains(line, "scanpst.exe") {
		t.Errorf("unreadable_messages line %q; want the count and the in-place repair remedy", line)
	}
	if doc := JSON(in, rep); doc.Fillable != 2 || doc.Unreadable != 2 {
		t.Errorf("JSON fillable=%d unreadable=%d; want 2 and 2 (fillable keeps its meaning)", doc.Fillable, doc.Unreadable)
	}

	in.Fillable, in.Unreadable, in.UnreadableOnly = 3, 2, 2 // one record is also missing an ordinary item
	rep = Assess(in, gapNow)
	if line := rep.Reasons[codeIndex(rep, "incomplete_content")]; !strings.Contains(line, "1 message(s) still missing content") {
		t.Errorf("incomplete_content must count only the records that are not merely partly read: %q", line)
	}

	in = healthyInput(gapNow)
	in.Unparsed = 4
	rep = Assess(in, gapNow)
	i := codeIndex(rep, "unparsed_messages")
	if rep.Posture != "GREEN" || i < 0 || !strings.Contains(rep.Reasons[i], "re-examines them once") {
		t.Errorf("unparsed records: posture %s codes %v reasons %v; want a GREEN note that says the next run re-examines them once", rep.Posture, rep.Codes, rep.Reasons)
	}
}

// covers: MA-326, R18, S48
// S1a maps a partial run to today's failure: the last-run record of a partial run
// is `failed` with its one-line error, which status shows as run_failed RED with
// that text (loud, fail-closed, no X1 change).
func TestStatusAPartialRunIsRecordedAsFailedForNow(t *testing.T) {
	in := gapInput(fetchFailed("graph:A1"))
	in.LastRun.Status = state.RunFailed
	in.LastRun.Error = "partial: 1 item(s) not captured — see attachments-report.tsv"
	rep := Assess(in, gapNow)
	i := codeIndex(rep, "run_failed")
	if rep.Posture != "RED" || i < 0 || !strings.Contains(rep.Reasons[i], "partial: 1 item(s) not captured") {
		t.Errorf("a partial run recorded failed: posture %s reasons %v; want RED run_failed carrying the summary", rep.Posture, rep.Reasons)
	}
}

// covers: MA-326, R18, S48
// health.Summary lists at most 20 gaps after the reasons, each with its source id,
// folder, subject, date and remedy, control-stripped, and ends with "+N more";
// info gaps (nothing missing) are not listed.
func TestStatusSummaryListsAtMostTwentyGapLines(t *testing.T) {
	var gaps []state.CaptureGap
	for i := 0; i < 25; i++ {
		g := fetchFailed(fmt.Sprintf("graph:%02d", i))
		g.Subject = fmt.Sprintf("Subject %02d \x1b[31mred\x1b[0m\nforged line", i)
		gaps = append(gaps, g)
	}
	info := state.CaptureGap{Folder: "Inbox", Locator: "ident:x", Reason: "refresh-failed", Class: state.ClassInfo, Subject: "an info row"}
	gaps = append(gaps, info)
	in := gapInput(gaps...)
	lines := Summary(in, Assess(in, gapNow))
	var gapLines []string
	more := ""
	for _, l := range lines {
		switch {
		case strings.Contains(l, "ann@contoso.com/Inbox"):
			gapLines = append(gapLines, l)
		case strings.Contains(l, "more —"):
			more = l
		}
	}
	assure.Reached(t, gapLines, "the gap lines")
	if len(gapLines) != 20 || !strings.Contains(more, "+5 more — see the report") {
		t.Fatalf("%d gap lines and %q; want 20 and '+5 more — see the report'", len(gapLines), more)
	}
	first := gapLines[0]
	for _, want := range []string{`"Subject 00`, "(2026-03-02)", "Microsoft 365 returned an error", "retried when you run again"} {
		if !strings.Contains(first, want) {
			t.Errorf("a gap line lacks %q: %q", want, first)
		}
	}
	for _, l := range lines {
		if strings.ContainsAny(l, "\x1b") || strings.Contains(l, "an info row") {
			t.Errorf("a control character or an info gap reached the summary: %q", l)
		}
	}
	for _, l := range lines {
		if l == "forged line" || strings.HasPrefix(l, "forged line") {
			t.Errorf("a newline in a subject forged a summary line: %q", l)
		}
	}
}

// covers: MA-326, R18, S48
// The JSON keys are additive and the version stays 2 (owner ruling OR1):
// capture_gaps {fillable, terminal, info, stuck, not_attempted, oldest_first_run,
// items[]} with items bounded to 20, unreadable, and last_run's gap counts
// including gaps_new.
func TestStatusJSONGapKeys(t *testing.T) {
	var gaps []state.CaptureGap
	for i := 0; i < 24; i++ {
		gaps = append(gaps, fetchFailed(fmt.Sprintf("graph:%02d", i)))
	}
	term := state.CaptureGap{Folder: "Old", Locator: "folder:Old", Scope: state.GapScopeFolder, Reason: "folder-unreadable", Class: state.ClassTerminal}
	notAtt := fetchFailed("graph:NA")
	notAtt.LastRun = gapNow.Add(-40 * 24 * time.Hour)
	gaps = append(gaps, term, notAtt)
	in := gapInput(gaps...)
	in.Unreadable = 3
	in.LastRun.GapsNew, in.LastRun.GapsObserved = 2, 25
	rep := Assess(in, gapNow)
	data, err := json.Marshal(JSON(in, rep))
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["version"] != float64(2) || JSONVersion != 2 {
		t.Errorf("JSON version %v (const %d), want 2", doc["version"], JSONVersion)
	}
	cg, ok := doc["capture_gaps"].(map[string]any)
	if !ok {
		t.Fatalf("no capture_gaps object in %s", data)
	}
	want := map[string]float64{"fillable": 24, "terminal": 1, "info": 0, "stuck": 0, "not_attempted": 1}
	for k, v := range want {
		if cg[k] != v {
			t.Errorf("capture_gaps.%s = %v, want %v", k, cg[k], v)
		}
	}
	if cg["oldest_first_run"] == nil {
		t.Error("capture_gaps.oldest_first_run is missing")
	}
	items, _ := cg["items"].([]any)
	if len(items) != 20 {
		t.Errorf("capture_gaps.items has %d entries, want 20 (bounded)", len(items))
	} else if it, _ := items[0].(map[string]any); it["source"] != "ann@contoso.com" || it["reason"] == nil || it["remedy"] == nil || it["folder"] == nil {
		t.Errorf("an item lacks its source, folder, reason or remedy: %v", items[0])
	}
	if doc["unreadable"] != float64(3) {
		t.Errorf("unreadable = %v, want 3", doc["unreadable"])
	}
	lr, _ := doc["last_run"].(map[string]any)
	if lr["gaps_new"] != float64(2) || lr["gaps_observed"] != float64(25) {
		t.Errorf("last_run gap counts = %v; want gaps_new 2 and gaps_observed 25", lr)
	}
}

// covers: MA-326, R18, S48
// Gather reads the ledger, the stores' source ids and each store's last run from
// disk, so the attempted split holds end to end: a gap observed by the run the
// last-run record names is attempted (a WARN); the same gap after a later run that
// did not touch it is not (a note).
func TestGatherReadsTheLedgerAndTheAttemptedSplit(t *testing.T) {
	out := t.TempDir()
	m, err := state.Load(filepath.Join(out, ".mailarchive-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	tok := m.Token("mailbox:ann@contoso.com", "ann@contoso.com")
	runAt := time.Now().UTC().Add(-time.Hour)
	g := fetchFailed("graph:A1")
	g.Token = tok
	m.ObserveGap(g, runAt)
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	if err := state.WriteLastRun(out, state.LastRun{Status: state.RunOK, Started: runAt, Finished: runAt.Add(time.Minute), PID: 1}); err != nil {
		t.Fatal(err)
	}
	in := Gather(out, "")
	assure.Reached(t, in.Gaps, "the gaps Gather read")
	if in.GapSource[tok] != "mailbox:ann@contoso.com" || !in.GapLastRun[tok].Equal(runAt) {
		t.Fatalf("Gather: source %q last run %v; want the store id and the last run's start", in.GapSource[tok], in.GapLastRun[tok])
	}
	if rep := Assess(in, time.Now()); codeIndex(rep, "capture_gaps_fillable") < 0 {
		t.Errorf("the gap the last run observed is not a WARN: %v", rep.Codes)
	}
	later := runAt.Add(30 * time.Minute)
	if err := state.WriteLastRun(out, state.LastRun{Status: state.RunOK, Started: later, Finished: later.Add(time.Minute), PID: 1}); err != nil {
		t.Fatal(err)
	}
	rep := Assess(Gather(out, ""), time.Now())
	if codeIndex(rep, "capture_gaps_fillable") >= 0 || codeIndex(rep, "capture_gaps_not_attempted") < 0 {
		t.Errorf("after a later run that did not observe it the gap must be not attempted: %v", rep.Codes)
	}
}

// covers: MA-326, R18, S48
// A stuck Microsoft 365 gap with no listing Message-ID also says that deleted
// messages in its folder are not being detected, and how to resume deletion
// tracking (§4.10, FC23(4)).
func TestStatusStuckNoMessageIDGapExplainsDeletionTracking(t *testing.T) {
	t.Skip("MA-326: the ledger does not record that a Graph fault had no listing Message-ID (CaptureGap has no field for it, and the design names none), so status cannot tell when the sweep sentence applies; closes when FI-S3 records it and the design says where")
}

// covers: MA-326, R18, S48
// A last run with partial: true shows the partial line, not run_failed (owner
// rulings OR1, OR2).
func TestStatusPartialFlagShowsThePartialLine(t *testing.T) {
	t.Skip("MA-326: the partial flag and its status line are FI-S1b's; S1a records a partial run as failed (§4.9); closes when FI-S1b lands")
}

// covers: MA-326, R18, S48
// The desktop Status & health view shows the same reasons and gap lines WITHOUT
// subjects until the dashboard read guard lands (X10).
func TestDesktopStatusShowsGapLinesWithoutSubjects(t *testing.T) {
	t.Skip("MA-326: the desktop view's mapping (no subject on the GET page before the read guard, F3/E0) is FI-S5's; closes when FI-S5 lands")
}
