package wizard

import (
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/tbcomplete"
)

// Slice C3's words on the wizard's end summary, Run now's note (plaintext and encrypted) and the
// wizard's wait (docs/design-thunderbird-completeness.md §7, its M8, MF1 and wizard-lead folds).
// The run's in-memory verdicts are the source: never a record (O1).

func c3CaughtUp(name string, n int64) tbcomplete.Result {
	return tbcomplete.Result{Account: name, StoreID: "id-" + name, Verdict: tbcomplete.CaughtUp, OnDisk: n, Listed: n,
		Tolerance: tbcomplete.Tolerance(n), AsOf: time.Now().Add(-time.Hour).Unix(),
		Folders: []tbcomplete.FolderCount{{Path: "INBOX", Listed: n, OnDisk: n}}}
}

func c3Behind() tbcomplete.Result {
	return tbcomplete.Result{Account: "imap.example.com", StoreID: "id-b", Verdict: tbcomplete.Behind, OnDisk: 1670, Listed: 14201,
		Missing: 12531, Tolerance: 28, AsOf: time.Now().Add(-time.Hour).Unix(),
		Folders: []tbcomplete.FolderCount{{Path: "INBOX", Listed: 10799, OnDisk: 1200}, {Path: "Sent", Listed: 3402, OnDisk: 470}}}
}

func c3Unknown() tbcomplete.Result {
	return tbcomplete.Result{Account: "imap.example.com", StoreID: "id-u", Verdict: tbcomplete.Unknown, Reason: tbcomplete.ReasonCacheUnreadable}
}

const (
	c3BehindHead      = "Thunderbird has downloaded about 1,670 of 14,201 messages in imap.example.com so far. Furthest behind: INBOX 1,200 of 10,799; Sent 470 of 3,402. Leave Thunderbird open (it downloads mostly while the computer is left alone)"
	c3BehindScheduled = c3BehindHead + "; later backups add the rest."
	c3BehindOneOff    = c3BehindHead + ". Run the backup again once Thunderbird has downloaded more, to add the rest."
	c3UnknownLine     = "MailArchive cannot tell whether Thunderbird has downloaded all of imap.example.com, because it could not read Thunderbird's list of folders."
)

// covers: MA-1181
// The wizard's end summary leads by the run's weakest account: "Export complete." only when every
// Thunderbird account may say complete, or the run read none (today's words, the positive twin);
// "Backed up what Thunderbird has so far." with both numbers when one is behind; "Backup
// finished." and the one sentence why when MailArchive cannot tell. A one-off wizard export has
// no schedule, so it asks for the backup to be run again (TC-17).
func TestTheWizardSummaryLeadsByTheWeakestAccount(t *testing.T) {
	stats := export.Stats{Exported: 3}
	if got := exportSummary("/archive", false, app.Result{Stats: stats}); !strings.HasPrefix(got, "Export complete.\n\nNewly downloaded this run: 0\n") {
		t.Fatalf("positive twin: no Thunderbird account keeps today's words:\n%s", got)
	}
	owner := app.Result{Stats: stats, Thunderbird: []tbcomplete.Result{c3CaughtUp("imap.gmail.com", 238169), c3CaughtUp("imap.example.com", 3402)}}
	if got := exportSummary("/archive", false, owner); !strings.HasPrefix(got, "Export complete.\n\n"+
		"Thunderbird has downloaded everything it lists for imap.gmail.com (238,169 messages).\n"+
		"Thunderbird has downloaded everything it lists for imap.example.com (3,402 messages).\n\nNewly downloaded this run: 0\n") {
		t.Errorf("the owner-shaped run:\n%s", got)
	}
	behind := app.Result{Stats: stats, Thunderbird: []tbcomplete.Result{c3CaughtUp("imap.gmail.com", 238169), c3Behind()}}
	got := exportSummary("/archive", false, behind)
	if !strings.HasPrefix(got, "Backed up what Thunderbird has so far.\n\n") || !strings.Contains(got, c3BehindOneOff+"\n") {
		t.Errorf("one account behind:\n%s", got)
	}
	got = exportSummary("/archive", false, app.Result{Stats: stats, Thunderbird: []tbcomplete.Result{c3CaughtUp("imap.gmail.com", 9), c3Unknown()}})
	if !strings.HasPrefix(got, "Backup finished.\n\n") || !strings.Contains(got, c3UnknownLine+"\n") {
		t.Errorf("caught up beside unknown:\n%s", got)
	}
	for _, s := range []string{exportSummary("/archive", false, behind), got} {
		if strings.Contains(s, "Export complete") {
			t.Errorf("a summary that may not say complete does:\n%s", s)
		}
	}
}

// covers: MA-1181
// Run now's note leads the same way, in its own form ("Backup finished: …", never "complete"
// for a plaintext archive): "Backed up what Thunderbird has so far: …" when one account is
// behind, with "later backups add the rest" only when a schedule will run them (TC-17); the lines
// come after the first line. Positive twin first: no Thunderbird account keeps today's words.
func TestRunNowSaysWhatThunderbirdHasSoFar(t *testing.T) {
	r := app.Result{Stats: export.Stats{Exported: 3}, ManifestBefore: 10, ManifestSize: 13}
	if got := runNowSummary(r); got != "Backup finished: 3 new messages archived." {
		t.Fatalf("positive twin: %q", got)
	}
	r.Thunderbird = []tbcomplete.Result{c3Behind()}
	if got := runNowSummaryFor(r, true); got != "Backed up what Thunderbird has so far: 3 new messages archived.\n"+c3BehindScheduled {
		t.Errorf("behind, scheduled:\n%q", got)
	}
	if got := runNowSummaryFor(r, false); got != "Backed up what Thunderbird has so far: 3 new messages archived.\n"+c3BehindOneOff {
		t.Errorf("behind, Run now only:\n%q", got)
	}
	r.Thunderbird = []tbcomplete.Result{c3Unknown()}
	if got := runNowSummaryFor(r, true); got != "Backup finished: 3 new messages archived.\n"+c3UnknownLine {
		t.Errorf("unknown:\n%q", got)
	}
	r.Thunderbird = []tbcomplete.Result{c3CaughtUp("imap.gmail.com", 9)}
	if got := runNowSummaryFor(r, true); got != "Backup finished: 3 new messages archived.\nThunderbird has downloaded everything it lists for imap.gmail.com (9 messages)." {
		t.Errorf("caught up:\n%q", got)
	}
}

// covers: MA-1186, MA-1181
// An encrypted archive's Run now note obeys R42 (M8, MF1): it begins "Backup complete:" only
// when every Thunderbird account of the run may say complete, or the run read none (the positive
// twin, today's words); "Backed up what Thunderbird has so far:" when one is behind; "Backup
// finished:" when one cannot be told, an Exchange account (account_type) and a store with no
// matched account (no_match) included. The lines come from the run's result in memory; then the
// index-behind sentence, as today.
func TestTheEncryptedNoteNeverSaysCompleteOverAGap(t *testing.T) {
	r := app.Result{Stats: export.Stats{Exported: 3}}
	sealed := "3 message(s) added or updated. They are sealed in the encrypted archive; open it in MailArchive Desktop to read them."
	if got := encryptedSummary(r); got != "Backup complete: "+sealed {
		t.Fatalf("positive twin: %q", got)
	}
	r.Thunderbird = []tbcomplete.Result{c3CaughtUp("imap.gmail.com", 9)}
	if got := encryptedSummaryFor(r, true); got != "Backup complete: "+sealed+"\nThunderbird has downloaded everything it lists for imap.gmail.com (9 messages)." {
		t.Errorf("caught up, nothing missing: %q", got)
	}
	r.Thunderbird, r.IndexBehind = []tbcomplete.Result{c3Behind()}, true
	got := encryptedSummaryFor(r, true)
	if !strings.HasPrefix(got, "Backed up what Thunderbird has so far: "+sealed+"\n"+c3BehindScheduled+"\n") ||
		!strings.HasSuffix(got, "\nThe search index is missing some messages; the next backup, or Rebuild search index, brings it up to date.") {
		t.Errorf("behind:\n%q", got)
	}
	for _, reason := range []tbcomplete.Reason{tbcomplete.ReasonCacheUnreadable, tbcomplete.ReasonAccountType, tbcomplete.ReasonNoMatch} {
		u := c3Unknown()
		u.Reason = reason
		r := app.Result{Stats: export.Stats{Exported: 3}, Thunderbird: []tbcomplete.Result{c3CaughtUp("imap.gmail.com", 9), u}}
		if got := encryptedSummaryFor(r, true); !strings.HasPrefix(got, "Backup finished: "+sealed+"\n") || strings.Contains(got, "complete") {
			t.Errorf("%v: %q", reason, got)
		}
	}
}

// covers: MA-1181
// The wizard's wait no longer says the download is complete when the files stop growing
// (§7's surfaces table): Thunderbird pauses for long stretches.
func TestTheWizardWaitSaysPausedNotComplete(t *testing.T) {
	if syncPausedText != "The download has paused — starting the backup of what Thunderbird has so far…" {
		t.Errorf("the wait's words: %q", syncPausedText)
	}
	if strings.Contains(syncPausedText, "complete") {
		t.Errorf("the wait says complete: %q", syncPausedText)
	}
}

// covers: MA-1198
// Hostile folder and account names (TC-7) reach the wizard's end summary and the Run now note
// cleaned and capped. Positive twin first: ordinary names as they are.
func TestHostileNamesInTheWizardAndRunNow(t *testing.T) {
	b := c3Behind()
	r := app.Result{Stats: export.Stats{Exported: 1}, Thunderbird: []tbcomplete.Result{b}}
	if s := exportSummary("/archive", false, r) + runNowSummaryFor(r, true); !strings.Contains(s, "INBOX 1,200 of 10,799") {
		t.Fatalf("positive twin:\n%s", s)
	}
	b.Account = "imap.\u202eevil\x1b.com"
	b.Folders = []tbcomplete.FolderCount{{Path: "IN\u202eBOX\x07" + strings.Repeat("y", 10*1024), Listed: 9, OnDisk: 1}}
	b.NotKept = []tbcomplete.FolderCount{{Path: "\u2066Pro\u200bjects\x00", Listed: 3}}
	r.Thunderbird = []tbcomplete.Result{b}
	for name, s := range map[string]string{"the end summary": exportSummary("/archive", false, r), "Run now": runNowSummaryFor(r, true), "encrypted Run now": encryptedSummaryFor(r, true)} {
		for _, bad := range []string{"\u202e", "\x1b", "\x07", "\u2066", "\u200b", "\x00", strings.Repeat("y", 80)} {
			if strings.Contains(s, bad) {
				t.Errorf("%s holds %q", name, bad)
			}
		}
		for _, good := range []string{"imap.evil.com", "INBOX" + strings.Repeat("y", 75) + "… 1 of 9", "Projects (3 messages)"} {
			if !strings.Contains(s, good) {
				t.Errorf("%s misses %q:\n%s", name, good, s)
			}
		}
	}
}
