package health

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/state"
)

// covers: MA-1160, R18, S29
// status's and Status & health's line for messages not in the archive in full (incomplete_content)
// is worded by where the last run read its mail, in the sentence Run now and Recent backups say
// (the polish-161 friction walk's N2: "1 message(s) still missing content — download for offline
// use in your mail app" was said for a plain mail file): Thunderbird's store names Thunderbird, a
// mail file says the file holds only part of them and names no mail app, a kind not known blames
// nobody; the count agrees with its noun and verb. Positive twin first: a Thunderbird store's line
// is the sentence Run now says, the report named after it.
func TestStatusSaysWhereTheIncompleteMailCameFrom(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	warn := func(in Input) string {
		rep := Assess(in, now)
		if i := codeIndex(rep, "incomplete_content"); i >= 0 {
			return rep.Reasons[i]
		}
		return ""
	}

	tb := healthyInput(now)
	tb.Fillable, tb.LastRun.FillableFrom = 3, state.FromThunderbird
	tb.ReportExists, tb.ReportPath = true, "/a/attachments-report.tsv"
	want := "WARN: " + strings.TrimSuffix(state.NotDownloadedSentence(3, state.FromThunderbird), ".") + " (see /a/attachments-report.tsv)"
	if got := warn(tb); got != want || !strings.Contains(got, "Thunderbird has not downloaded them yet") {
		t.Fatalf("the positive twin: a Thunderbird store's line\n got %q\nwant %q", got, want)
	}

	file := healthyInput(now)
	file.Fillable, file.LastRun.FillableFrom = 1, state.FromMailFile
	one := warn(file)
	if !strings.Contains(one, "1 message is not in the archive in full: the mail file holds only part of this message; if it is a copy, copy it again from its mail program") {
		t.Errorf("a mail file's line: %q", one)
	}

	unknown := healthyInput(now)
	unknown.Fillable = 2 // an older record names no kind
	neutral := warn(unknown)
	if !strings.Contains(neutral, strings.TrimSuffix(state.NotDownloadedSentence(2, ""), ".")) {
		t.Errorf("a kind not known: %q", neutral)
	}

	mixed := healthyInput(now)
	mixed.Fillable, mixed.FillableParseGap, mixed.FillableOnlyParseGap, mixed.LastRun.FillableFrom = 3, 1, 1, state.FromMailFile
	mix := warn(mixed)
	if !strings.Contains(mix, "the mail file holds only part of these messages") || !strings.Contains(mix, "1 of them is cut short") {
		t.Errorf("a mix keeps the cut-file remedy beside the mail file's words: %q", mix)
	}

	for _, l := range []string{one, neutral, mix} {
		if strings.Contains(l, "download for offline use") || strings.Contains(l, "mail app has not downloaded") || strings.Contains(l, "Thunderbird") || strings.Contains(l, "(s)") {
			t.Errorf("a line that is not a mail app's store blames one, or counts in (s): %q", l)
		}
	}

	// The real path: Gather reads the kind from the last-run record the run wrote.
	out := t.TempDir()
	m, err := state.Load(filepath.Join(out, ".mailarchive-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	m.Add("k1", state.Record{Path: "s/a.html", Missing: []string{"body"}})
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	if err := state.WriteLastRun(out, state.LastRun{Status: state.RunOK, Started: now.Add(-time.Minute), Finished: now, Fillable: 1, FillableFrom: state.FromMailFile}); err != nil {
		t.Fatal(err)
	}
	real := warn(Gather(out, ""))
	assure.Reached(t, strings.Contains(real, "the mail file holds only part of this message") && !strings.Contains(real, "download for offline use"),
		"status of a real archive read from a mail file: "+real)
}
