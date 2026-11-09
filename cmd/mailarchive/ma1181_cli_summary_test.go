package main

import (
	"bytes"
	"log"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/tbcomplete"
)

// covers: MA-1181, MA-1198
// The backup's own command-line summary (completeness design §7's "Command line (stderr)" row,
// slice C3): after its counts, a "Thunderbird:" line with the run's lead, then each account's
// lines, from the run's verdicts in memory; names cleaned and capped (TC-7), so no control
// character a server named a folder with reaches the terminal. The exit code is unchanged (X1:
// a behind run is a success). With no schedule, what to do again is the command itself (the C3
// friction review's P3); the complete lead is "Backup complete." (the owner's ruling of
// 2026-11-08). Positive twin first: a run that read no Thunderbird account says nothing of it.
func TestTheRunSummarySaysWhatThunderbirdHasSoFar(t *testing.T) {
	text := func(r app.Result) string {
		var buf bytes.Buffer
		printSummaryFor(log.New(&buf, "", 0), r, false, false, t.TempDir(), "Done.", "darwin")
		return buf.String()
	}
	r := app.Result{Stats: export.Stats{Exported: 3}}
	if got := text(r); strings.Contains(got, "Thunderbird") {
		t.Fatalf("positive twin: %s", got)
	}
	caughtUp := tbcomplete.Result{Account: "imap.gmail.com", Name: "sam@gmail.com", StoreID: "g", Verdict: tbcomplete.CaughtUp, OnDisk: 9, Listed: 9, AsOf: time.Now().Unix()}
	r.Thunderbird = []tbcomplete.Result{caughtUp}
	if got := text(r); !strings.Contains(got, "Thunderbird: Backup complete.\n  Thunderbird has downloaded everything it lists for sam@gmail.com (9 messages).\n") {
		t.Errorf("caught up:\n%s", got)
	}
	r.Thunderbird = []tbcomplete.Result{
		caughtUp,
		{Account: "imap.\x1b]0;evil\x07.com", StoreID: "b", Verdict: tbcomplete.Behind, OnDisk: 1670, Listed: 14201,
			Folders: []tbcomplete.FolderCount{{Path: "IN\x1b[2JBOX", Listed: 10799, OnDisk: 1200}}},
	}
	got := text(r)
	for _, want := range []string{
		"Thunderbird: Backed up what Thunderbird has so far.\n",
		"  Thunderbird has downloaded everything it lists for sam@gmail.com (9 messages).\n",
		"  Thunderbird has downloaded about 1,670 of 14,201 messages in the Thunderbird account kept in the folder imap.]0;evil.com so far. Furthest behind: INBOX 1,200 of 10,799. Leave Thunderbird open (it downloads mostly while the computer is left alone). Run the same command again once Thunderbird has downloaded more, to add the rest.\n",
	} {
		if !strings.Contains(strings.ReplaceAll(got, "[2J", ""), want) {
			t.Errorf("the summary lacks %q:\n%s", want, got)
		}
	}
	if strings.ContainsAny(got, "\x1b\x07") {
		t.Errorf("a control character reaches the terminal:\n%q", got)
	}
}
