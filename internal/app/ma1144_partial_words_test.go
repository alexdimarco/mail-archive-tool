package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/state"
)

// covers: MA-1144, MA-1145, R18, S48
// The owner's report of 2026-11-05, end to end on a real run: two messages a mistake in
// MailArchive could not save (export-panic) make the run partial, and its words say so in
// plain words — that the mail is fine and how to report it — with the report named only as
// the details; the last-run record and the history entry carry the kinds; every code stays as
// it was (status failed, the gap counts, ErrPartial, the partial failure kind). Positive twin
// first: the same source with no fault saves everything and records no partial.
func TestAPartialRunSaysItsKindsInPlainWords(t *testing.T) {
	src := filepath.Join(tmpDir(t), "Inbox")
	maildirMessages(t, src, 4)
	clean := tmpDir(t)
	if _, err := runLocal(t, clean, src); err != nil {
		t.Fatalf("the positive twin: %v", err)
	}
	if lr, _, _ := state.ReadLastRun(clean); lr.Status != state.RunOK || lr.Partial != nil {
		t.Fatalf("the positive twin's record: %+v", lr)
	}

	out := tmpDir(t)
	seen := 0
	exporterSetup = func(e *export.Exporter) {
		e.BeforeWrite = func(path string) error {
			if strings.HasSuffix(path, ".html") {
				if seen++; seen == 2 || seen == 3 {
					panic("renderer fault")
				}
			}
			return nil
		}
	}
	t.Cleanup(func() { exporterSetup = nil })
	_, err := runLocal(t, out, src)
	if !errors.Is(err, ErrPartial) || FailureKind(err) != state.FailPartial {
		t.Fatalf("run err = %v; want a partial run", err)
	}
	want := "Not everything was saved. 2 messages could not be saved because of a mistake in MailArchive — " + state.MistakeRemedy + ". " + state.PartialDetails
	if err.Error() != want {
		t.Errorf("the run's words:\n got %q\nwant %q", err.Error(), want)
	}
	lr, _, _ := state.ReadLastRun(out)
	if lr.Status != state.RunFailed || lr.Error != want || lr.GapsObserved != 2 || lr.GapsTerminal != 2 ||
		lr.Partial == nil || *lr.Partial != (state.PartialCounts{Mistake: state.PartialKind{Messages: 2}}) {
		t.Errorf("the record: status %q error %q gaps %d/%d partial %+v", lr.Status, lr.Error, lr.GapsObserved, lr.GapsTerminal, lr.Partial)
	}
	h, _ := state.ReadRunHistory(out)
	assure.Reached(t, len(h) == 1 && h[0].Status == state.RunFailed && h[0].Partial != nil && h[0].Partial.Mistake.Messages == 2,
		"the history entry carries the kinds")
}

// covers: MA-1144, R18, S48
// PartialError's words for every mix of kinds the run's summary counts: the most serious
// first, the partly-read count last, a gap counted with no kind still said; an encrypted
// archive's words name MailArchive, never the report it does not have.
func TestPartialErrorWordsEveryKind(t *testing.T) {
	sum := GapSummary{ItemGaps: 6, FolderGaps: 1, Kinds: state.PartialCounts{
		Mistake: state.PartialKind{Messages: 1}, ArchiveWrite: state.PartialKind{Folders: 1},
		Retried: state.PartialKind{Messages: 3}, Source: state.PartialKind{Messages: 1},
	}}
	e := &PartialError{Summary: sum, Unreadable: 2, ReportPath: "/a/attachments-report.tsv"}
	s := e.Error()
	order := []string{"1 message could not be saved because of a mistake in MailArchive", "1 folder could not be written to the archive",
		"1 message cannot be read from your mail source", "3 messages could not be read from your mail this time", "2 messages could only be partly read", state.PartialDetails}
	at := 0
	for _, w := range order {
		i := strings.Index(s, w)
		if i < at {
			t.Fatalf("%q not in order in %q", w, s)
		}
		at = i
	}
	// A gap the summary counted under no kind (a summary from elsewhere) is still said.
	if s := (&PartialError{Summary: GapSummary{ItemGaps: 1}, ReportPath: "/a/r"}).Error(); !strings.Contains(s, "1 message could not be saved — "+state.SourceRemedy) || !strings.Contains(s, state.PartialDetails) {
		t.Errorf("an uncounted gap: %q", s)
	}
	enc := (&PartialError{Summary: sum, Encrypted: true}).Error()
	assure.Reached(t, strings.HasPrefix(enc, state.PartialLead) && !strings.Contains(enc, "attachments-report.tsv") && strings.Contains(enc, "Open the archive in MailArchive to see which."),
		"the encrypted words: "+enc)
}

// covers: MA-1147, R18, S29
// A backup refused because another run holds the archive is a skip, not a failure (the owner's
// report of 2026-11-05: Run now said "Backup failed: another backup or check is using this
// archive … holding its lock file .mailarchive.lock" while the 20:00 backup was simply still
// running). Its words say what is running and since when, that this backup was skipped and
// nothing is wrong, and to try again when it finishes — said once, naming the lock file (S25)
// but none of the lock's diagnostics, never "failed"; it is still a busy refusal to every code (ErrHeld, the busy
// failure kind) and writes nothing. A holder that started more than a day ago may be stuck, and
// the words say so and what to do. Positive twin first: the backup runs once the other lets go.
func TestABackupMetByARunningOneIsSkippedNotFailed(t *testing.T) {
	src := filepath.Join(tmpDir(t), "Inbox")
	maildirMessages(t, src, 1)
	out := tmpDir(t)
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Index: true}, discard(), nil); err != nil {
		t.Fatalf("the positive twin: %v", err)
	}
	before, _ := os.ReadFile(filepath.Join(out, state.LastRunName))
	held, err := lockfile.AcquireAs(filepath.Join(out, lockfile.Name), "export")
	if err != nil {
		t.Fatal(err)
	}
	_, err = Run(context.Background(), Options{Inputs: []string{src}, Out: out, Index: true}, discard(), nil)
	held.Release()
	if !errors.Is(err, lockfile.ErrHeld) || FailureKind(err) != state.FailArchiveBusy {
		t.Fatalf("want a busy refusal, got %v", err)
	}
	msg := err.Error()
	assure.Refused(t, 1, msg, assure.Code(1),
		assure.Names("Another backup of this archive is already running (started ", ") — this backup was skipped; nothing is wrong.", "Try again when it finishes",
			"in use (its lock file, "+lockfile.Name+")"),
		assure.Forbid("failed", "Failed", "pid=", "host=", "nonce", "held by", out),
		assure.NoSideEffect(func() bool {
			after, _ := os.ReadFile(filepath.Join(out, state.LastRunName))
			return string(after) == string(before)
		}))
	if words, ok := BusySkipped(err); !ok || words != msg {
		t.Errorf("BusySkipped = %q, %v; want the refusal's own words", words, ok)
	}
	if _, ok := BusySkipped(errors.New("the disk is full")); ok {
		t.Error("an error that is not a busy archive was told as a skip")
	}

	// By what holds it; a holder from more than a day ago may be stuck.
	lock := filepath.Join(t.TempDir(), lockfile.Name)
	now := time.Date(2026, 11, 5, 21, 0, 0, 0, time.Local)
	for verb, lead := range map[string]string{
		"export": "Another backup of this archive is already running", "graph": "A Microsoft 365 backup into this archive is already running",
		"verify": "A check of this archive (verify) is running", "rebuild": "A rebuild of this archive's search index is running",
		RestoreThunderbirdVerb: "A restore of this archive into Thunderbird is running", "": "Another MailArchive task is using this archive",
	} {
		line := "pid=1 started=" + now.Add(-time.Hour).Format(time.RFC3339) + " host=h"
		if verb != "" {
			line += " verb=" + verb
		}
		if err := os.WriteFile(lock, []byte(line+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if w := busyWords(lock, now); !strings.HasPrefix(w, lead+" (started 20:00 on 2026-11-05) — this backup was skipped; nothing is wrong.") {
			t.Errorf("verb %q: %q", verb, w)
		}
	}
	// The holder is this computer ("h"), as when this test was written before MA-1153 named another
	// one: its words say "restart the computer".
	prevHost := thisHost
	thisHost = func() string { return "h" }
	t.Cleanup(func() { thisHost = prevHost })
	if err := os.WriteFile(lock, []byte("pid=1 started="+now.Add(-30*time.Hour).Format(time.RFC3339)+" host=h verb=export\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stuck := busyWords(lock, now)
	if strings.Contains(stuck, "nothing is wrong") || !strings.Contains(stuck, "started more than a day ago") || !strings.Contains(stuck, "so it may be stuck") || !strings.Contains(stuck, "restart the computer") {
		t.Errorf("a holder from more than a day ago: %q", stuck)
	}
}
