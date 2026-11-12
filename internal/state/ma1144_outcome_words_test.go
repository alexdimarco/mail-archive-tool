package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
)

// covers: MA-1144, R18, S48
// A partial run's words, by kind (design-plain-failure-words §2; the owner's report of
// 2026-11-05: "Failed — partial: 4 item(s) not captured — see attachments-report.tsv"). Each
// capture gap is put under one kind by its reason and class; the words say what happened
// first, then the remedy, one sentence per kind and the most serious first; the remedy of a
// mistake in MailArchive and of a write to the archive is the gap table's own, so the report's
// row, status's gap line and the run's words read the same; the report is named only as the
// details, never as the only explanation. Positive twin first: the owner's run, 4 export-panic
// gaps, says they are MailArchive's mistake, that the mail is fine, and how to report it.
func TestAPartialRunSaysWhatHappenedByKind(t *testing.T) {
	var p PartialCounts
	for i := 0; i < 4; i++ {
		p.Add(CaptureGap{Reason: "export-panic", Class: ClassTerminal, Scope: GapScopeItem})
	}
	want := "Not everything was saved. 4 messages could not be saved because of a mistake in MailArchive — your mail is fine (the original is untouched); please report it at " +
		ReportAddress + ". Details: attachments-report.tsv in the archive folder."
	if got := PartialText(p, true); got != want {
		t.Fatalf("the owner's run:\n got %q\nwant %q", got, want)
	}

	for _, c := range []struct{ reason, class, kind string }{
		{"export-panic", ClassTerminal, KindMistake},
		{"export-failed", ClassFillable, KindArchiveWrite},
		{"folder-unwritable", ClassFillable, KindArchiveWrite},
		{"fetch-failed", ClassFillable, KindRetried},
		{"file-unreadable", ClassFillable, KindRetried},
		{"folder-unreadable", ClassFillable, KindRetried},
		{"fetch-too-large", ClassTerminal, KindSource},
		{"pst-message-unreadable", ClassTerminal, KindSource},
		{"refresh-failed", ClassInfo, ""},
		{"not-a-message-file", ClassInfo, ""},
	} {
		if got := PartialKindOf(c.reason, c.class); got != c.kind {
			t.Errorf("PartialKindOf(%q, %q) = %q, want %q", c.reason, c.class, got, c.kind)
		}
	}

	var mix PartialCounts
	mix.Add(CaptureGap{Reason: "fetch-failed", Class: ClassFillable, Scope: GapScopeOverflow, Skipped: 420})
	mix.Add(CaptureGap{Reason: "folder-unreadable", Class: ClassFillable, Scope: GapScopeFolder})
	mix.Add(CaptureGap{Reason: "export-failed", Class: ClassFillable, Scope: GapScopeItem})
	mix.Add(CaptureGap{Reason: "fetch-too-large", Class: ClassTerminal, Scope: GapScopeItem})
	mix.Add(CaptureGap{Reason: "export-panic", Class: ClassTerminal, Scope: GapScopeItem})
	mix.Add(CaptureGap{Reason: "refresh-failed", Class: ClassInfo, Scope: GapScopeItem})
	mix.PartlyRead = 2
	got := PartialSentences(mix)
	wantS := []string{
		"1 message could not be saved because of a mistake in MailArchive — " + MistakeRemedy + ".",
		"1 message could not be written to the archive — " + GapRemedy("export-failed", "", "") + ".",
		"1 message cannot be read from your mail source as it stands (for example a damaged file) — " + SourceRemedy + ".",
		"420 messages and 1 folder could not be read from your mail this time — " + RetryRemedy + ".",
		"2 messages could only be partly read from your mail — " + PartlyReadRemedy + ".",
	}
	if strings.Join(got, "\n") != strings.Join(wantS, "\n") {
		t.Errorf("by kind, most serious first:\n got %q\nwant %q", got, wantS)
	}
	// The remedy is the same everywhere: the gap table's own for these reasons.
	assure.Reached(t, GapRemedy("export-panic", SourceLocal, "") == MistakeRemedy && GapRemedy("export-panic", SourceGraph, "") == MistakeRemedy,
		"the gap table says the mistake's remedy in the same words")
	// The report is the details, never the only explanation; no bare code reaches a person.
	for _, s := range append(got, PartialText(mix, false)) {
		for _, gone := range []string{"attachments-report.tsv", "item(s)", "partial:", "export-panic", "not captured"} {
			if strings.Contains(s, gone) {
				t.Errorf("%q carries %q", s, gone)
			}
		}
	}
	if PartialText(mix, false) != PartialLead+" "+strings.Join(wantS, " ") {
		t.Errorf("an archive with no report names none: %q", PartialText(mix, false))
	}
	// Nothing counted is nothing said; an info gap is not a problem.
	var info PartialCounts
	info.Add(CaptureGap{Reason: "refresh-failed", Class: ClassInfo})
	if !info.Empty() || len(PartialSentences(info)) != 0 {
		t.Errorf("an info gap was counted: %+v", info)
	}
	// What a record from elsewhere counts under no kind is still said, with a remedy.
	if s := PartialSentences(PartialCounts{Other: PartialKind{Messages: 3}}); len(s) != 1 || s[0] != "3 messages could not be saved — "+SourceRemedy+"." {
		t.Errorf("an unknown kind: %q", s)
	}
}

// covers: MA-1149, MA-1157, R18, S29
// Messages the mail app has not downloaded in full yet (fillable records: Thunderbird still
// downloading) are said as that, with the remedy that later backups add them — one sentence,
// singular and plural, the digits grouped. Amended for 1.6.2 (the 1.6.1 friction review's P7,
// MA-1157): the sentence names Thunderbird only when the run read Thunderbird's store.
func TestMessagesNotDownloadedYetAreSaidAsThat(t *testing.T) {
	if got, want := NotDownloadedSentence(1, FromThunderbird), "1 message is not on this computer in full: Thunderbird has not downloaded it yet. Later backups add it once it has; opening it in Thunderbird makes it download it sooner."; got != want {
		t.Errorf("one:\n got %q\nwant %q", got, want)
	}
	got := NotDownloadedSentence(1420, FromThunderbird)
	assure.Reached(t, strings.HasPrefix(got, "1,420 messages are not on this computer in full") && strings.Contains(got, "Later backups add them once it has"),
		"many, grouped: "+got)
}

// covers: MA-1145, R18, S48
// What a partial run could not save is recorded by kind (additive): the last-run record and
// its history entry carry "partial", with the counts and nothing else; the status stays
// "failed" (the S1a mapping and every code that keys on it are unchanged); a record without
// it (a run that saved everything, an older one) reads as none; an encrypted archive's record
// and history keep only their allow-list, so no count is written beside it. The history entry
// also carries how many messages are not downloaded in full yet. Positive twin first: a
// plaintext partial run's record carries the kinds.
func TestAPartialRunIsRecordedByKind(t *testing.T) {
	out := t.TempDir()
	p := &PartialCounts{Mistake: PartialKind{Messages: 4}, Retried: PartialKind{Folders: 1}}
	lr := LastRun{Status: RunFailed, Started: time.Now().UTC(), Finished: time.Now().UTC(), Error: PartialText(*p, true), Partial: p, Fillable: 420, GapsObserved: 5, GapsTerminal: 4}
	if err := WriteLastRun(out, lr); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(out, LastRunName))
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["status"] != RunFailed || doc["gaps_observed"] != float64(5) {
		t.Errorf("the codes changed: %s", raw)
	}
	part, _ := doc["partial"].(map[string]any)
	mistake, _ := part["mailarchive_mistake"].(map[string]any)
	retried, _ := part["retried"].(map[string]any)
	if mistake["messages"] != float64(4) || retried["folders"] != float64(1) || len(part) != 2 {
		t.Errorf("the record's partial: %s", raw)
	}
	back, st, err := ReadLastRun(out)
	if st != LastRunPresent || err != nil || back.Partial == nil || *back.Partial != *p {
		t.Errorf("read back: %+v %v %v", back.Partial, st, err)
	}
	e := RunHistoryEntryFrom(back)
	if e.Status != RunFailed || e.Partial == nil || *e.Partial != *p || e.Fillable != 420 {
		t.Errorf("the history entry: %+v", e)
	}
	if err := AppendRunHistory(out, e); err != nil {
		t.Fatal(err)
	}
	h, _ := ReadRunHistory(out)
	if len(h) != 1 || h[0].Partial == nil || h[0].Partial.Mistake.Messages != 4 || h[0].Fillable != 420 {
		t.Errorf("the history read back: %+v", h)
	}

	// A record without it reads as none.
	if err := os.WriteFile(filepath.Join(out, LastRunName), []byte(`{"version":1,"status":"failed","error":"partial: 1 item(s) not captured — see attachments-report.tsv"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if old, _, _ := ReadLastRun(out); old.Partial != nil || RunHistoryEntryFrom(old).Partial != nil {
		t.Errorf("an older record reads with partial counts: %+v", old.Partial)
	}
	// A planted record's negative count is not a count: nothing is said from it.
	if err := os.WriteFile(filepath.Join(out, LastRunName), []byte(`{"version":1,"status":"failed","partial":{"mailarchive_mistake":{"messages":-3}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if planted, _, _ := ReadLastRun(out); planted.Partial != nil {
		t.Errorf("a planted negative count was kept: %+v", planted.Partial)
	}

	// An encrypted archive keeps none of it.
	enc := t.TempDir()
	if err := WriteLastRun(enc, LastRun{Status: RunFailed, Encrypted: true, ErrorKind: FailPartial, Partial: p, Fillable: 9}); err != nil {
		t.Fatal(err)
	}
	encRaw, _ := os.ReadFile(filepath.Join(enc, LastRunName))
	if strings.Contains(string(encRaw), "partial\":{") || strings.Contains(string(encRaw), "mailarchive_mistake") || strings.Contains(string(encRaw), "fillable") {
		t.Errorf("an encrypted record carries counts: %s", encRaw)
	}
	if m := RunHistoryEntryFrom(LastRun{Status: RunFailed, Encrypted: true, ErrorKind: FailPartial, Partial: p, Fillable: 9}); m.Partial != nil || m.Fillable != 0 {
		t.Errorf("an encrypted history entry carries counts: %+v", m)
	}
}

// covers: MA-1146, R18, S48
// BACKUP-NEEDS-ATTENTION.txt after a partial run says what happened first — the backup did
// not save everything, and the rest was saved — and carries the run's words, never "FAILED".
// Positive twin first: a run that failed outright still says FAILED with its reason.
func TestTheAttentionNoticeOfAPartialRunSaysWhatHappened(t *testing.T) {
	out := t.TempDir()
	if err := WriteLastRun(out, LastRun{Status: RunFailed, Error: "the archive drive is full"}); err != nil {
		t.Fatal(err)
	}
	failed, _ := os.ReadFile(filepath.Join(out, AttentionName))
	if !strings.HasPrefix(string(failed), "This email backup FAILED and needs your attention.") || !strings.Contains(string(failed), "the archive drive is full") {
		t.Fatalf("the positive twin: %s", failed)
	}
	p := &PartialCounts{Mistake: PartialKind{Messages: 4}}
	if err := WriteLastRun(out, LastRun{Status: RunFailed, Error: PartialText(*p, true), Partial: p}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(out, AttentionName))
	s := string(b)
	assure.Reached(t, strings.HasPrefix(s, "This email backup did not save everything.") && strings.Contains(s, "Everything else it found was saved") &&
		strings.Contains(s, "4 messages could not be saved because of a mistake in MailArchive") && strings.Contains(s, ReportAddress) && !strings.Contains(s, "FAILED"),
		"the partial run's notice: "+s)
}
