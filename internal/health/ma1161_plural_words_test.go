package health

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/state"
)

// covers: MA-1161, R18, S48
// Every line status and Status & health print counts in proper singular and plural, the verb and
// pronoun agreeing ("1 message is kept … read it", "1 unreadable line — that event is skipped"),
// never "(s)" (the polish-161 friction walk's N3), and a line says what it adds in one bracket,
// never two in a row: a folder's gap line "(the original is untouched; 3 messages not read)", a
// remedy "(the original is still in your mailbox; +1 other cause: see the list below)". Positive
// twin first: the same lines for two things still count both.
func TestStatusLinesCountInProperSingularAndPlural(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	when := now.Add(-time.Hour)
	cases := []struct {
		name      string
		set       func(in *Input, n int)
		one, many string
	}{
		{"index missing", func(in *Input, n int) { in.HasIndex, in.Indexed, in.Messages = false, 0, n },
			"although the manifest records 1 message —", "although the manifest records 2 messages —"},
		{"verify unrecorded", func(in *Input, n int) {
			in.LastVerifyState = state.LastVerifyPresent
			in.LastVerify = state.LastVerify{Status: state.VerifyDone, Started: when, Finished: &when, Unrecorded: n, ExitCode: 2}
		}, "left 1 file with no recorded fixity — run `mailarchive verify -record -out \"/a\"` to baseline it", "left 2 files with no recorded fixity — run `mailarchive verify -record -out \"/a\"` to baseline them"},
		{"history corrupt", func(in *Input, n int) { in.History = state.HistoryStat{Exists: true, Runs: n, Events: n, BadLines: n} },
			"has 1 unreadable line — that event is skipped", "has 2 unreadable lines — those events are skipped"},
		{"history line", func(in *Input, n int) { in.History = state.HistoryStat{Exists: true, Runs: n, Events: n, BadLines: n} },
			"History:    1 run, 1 event — 1 unreadable line, go-back partial", "History:    2 runs, 2 events — 2 unreadable lines, go-back partial"},
		{"history clean", func(in *Input, n int) { in.History = state.HistoryStat{Exists: true, Runs: n, Events: n} },
			"History:    1 run, 1 event recorded", "History:    2 runs, 2 events recorded"},
		{"history torn", func(in *Input, n int) {
			in.History = state.HistoryStat{Exists: true, Runs: n, Events: n, TornTail: true}
		},
			"History:    1 run, 1 event — torn tail", "History:    2 runs, 2 events — torn tail"},
		{"partly read", func(in *Input, n int) { in.Fillable, in.Unreadable, in.UnreadableOnly = n, n, n },
			"WARN: 1 message could only be partly read from the source", "WARN: 2 messages could only be partly read from the source"},
		{"partly read summary", func(in *Input, n int) { in.Fillable, in.Unreadable, in.UnreadableOnly = n, n, n },
			"Partly read: 1 message could only be partly read", "Partly read: 2 messages could only be partly read"},
		{"unparsed", func(in *Input, n int) { in.Unparsed = n },
			"1 message could not be parsed and is kept as raw text — the original bytes are in the .eml when the archive is made with -raw; after upgrading, the next run re-examines it once",
			"2 messages could not be parsed and are kept as raw text — the original bytes are in the .eml when the archive is made with -raw; after upgrading, the next run re-examines them once"},
		{"unparsed summary", func(in *Input, n int) { in.Unparsed = n },
			"Raw text:   1 message could not be parsed and is kept as raw text", "Raw text:   2 messages could not be parsed and are kept as raw text"},
		{"undecodable", func(in *Input, n int) { in.Undecodable = n },
			"1 message has parts this version cannot decode", "2 messages have parts this version cannot decode"},
		{"fallback", func(in *Input, n int) { in.FidelityFallback = n },
			"1 message is kept as an earlier version read it, because this version could not decode it in full — nothing is lost",
			"2 messages are kept as an earlier version read them, because this version could not decode them in full — nothing is lost"},
		{"fallback summary", func(in *Input, n int) { in.FidelityFallback = n },
			"Fallback:   1 message is kept as an earlier version read it, because this version could not decode it in full; nothing is lost",
			"Fallback:   2 messages are kept as an earlier version read them, because this version could not decode them in full; nothing is lost"},
		{"shown as source", func(in *Input, n int) { in.ShownAsSource = map[string]int{"unparseable": n} },
			"1 message is shown as plain text instead of formatted pages", "2 messages are shown as plain text instead of formatted pages"},
		{"cut short", func(in *Input, n int) {
			in.Fillable, in.Undecodable, in.FillableParseGap, in.FillableOnlyParseGap = n, n, n, n
		},
			"1 message still missing content: cut short, or with parts that could not be told apart, in the file it was read from — replace the mailbox file with a complete export from your mail app (an IMAP cache: download the message for offline use), then re-run; incremental fills it.",
			"2 messages still missing content: cut short, or with parts that could not be told apart, in the file they were read from — replace the mailbox file with a complete export from your mail app (an IMAP cache: download the messages for offline use), then re-run; incremental fills them."},
	}
	lines := func(in Input) string {
		rep := Assess(in, now)
		return strings.Join(append(Summary(in, rep), rep.Reasons...), "\n")
	}
	var all []string
	for _, n := range []int{2, 1} { // the positive twin, two of each, first
		for _, c := range cases {
			in := healthyInput(now)
			c.set(&in, n)
			got, want := lines(in), c.many
			if n == 1 {
				want = c.one
			}
			if !strings.Contains(got, want) {
				t.Errorf("%s, %d: lacks %q:\n%s", c.name, n, want, got)
			}
			all = append(all, got)
		}
		if t.Failed() {
			t.FailNow()
		}
	}

	// One bracket, never two in a row: a folder's gap line, and a remedy that also says where the
	// original is and that other causes are listed.
	folder := state.CaptureGap{Folder: "Receipts", Locator: "folder:Receipts", Scope: state.GapScopeFolder, Reason: "folder-unwritable", Class: state.ClassTerminal, Skipped: 3}
	big := func(loc string) state.CaptureGap {
		g := fetchFailed(loc)
		g.Reason, g.Class = "fetch-too-large", state.ClassTerminal
		return g
	}
	in := gapInput(folder, big("graph:B1"), big("graph:B2"))
	gaps := lines(in)
	for _, want := range []string{"(the original is untouched; 3 messages not read)", "(the original is still in your mailbox; +1 other cause: see the list below)"} {
		if !strings.Contains(gaps, want) {
			t.Errorf("the gap lines lack %q:\n%s", want, gaps)
		}
	}
	all = append(all, gaps)

	// The line after a capped list: one entry left is "1 more entry".
	var many []state.CaptureGap
	for i := 0; i < maxGapLines; i++ {
		many = append(many, fetchFailed(fmt.Sprintf("graph:M%d", i)))
	}
	many = append(many, state.CaptureGap{Folder: "Big", Locator: "overflow:Big", Scope: state.GapScopeOverflow, Reason: "fetch-failed", Class: state.ClassFillable, Skipped: 4005})
	capped := lines(gapInput(many...))
	if !strings.Contains(capped, "+1 more entry (4,005 messages)") {
		t.Errorf("the capped list's last line:\n%s", capped)
	}
	all = append(all, capped)

	var bad []string
	lineCount := 0
	for _, l := range strings.Split(strings.Join(all, "\n"), "\n") {
		lineCount++
		if strings.Contains(l, "(s)") || strings.Contains(l, ") (") || strings.Contains(l, "1 more entries") {
			bad = append(bad, l)
		}
	}
	assure.Reached(t, lineCount > len(cases) && len(bad) == 0, "no (s), no two brackets in a row, in any line; these are:\n"+strings.Join(bad, "\n"))
}
