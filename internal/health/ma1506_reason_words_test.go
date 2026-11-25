package health

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/state"
)

// reasonArchive is a folder holding a refused record with the reason reason and a source streak
// past the surface threshold, its first date Oct 12, 2026; encrypted writes the record as an
// encrypted archive's.
func reasonArchive(t *testing.T, reason string, encrypted bool) string {
	t.Helper()
	dir := t.TempDir()
	first := time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC)
	if err := state.WriteSourceStreak(dir, state.SourceStreak{ConsecutiveFails: 2, FirstUnreachable: first, LastUnreachable: first.Add(state.SurfaceAfter), LastKind: "local-source-unreachable"}); err != nil {
		t.Fatal(err)
	}
	at := first.Add(state.SurfaceAfter)
	if err := state.WriteLastRun(dir, state.LastRun{Status: state.RunFailed, Started: at, Finished: at, Error: "x", SourceReason: reason, Encrypted: encrypted}); err != nil {
		t.Fatal(err)
	}
	return dir
}

// statusLost is status's source_lost line for the archive at dir, read as status reads it.
func statusLost(t *testing.T, dir string) string {
	t.Helper()
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	in := healthyInput(now)
	in.Out = dir
	in.LastRun, in.LastRunState, _ = state.ReadLastRun(dir)
	for _, r := range Assess(in, now).Reasons {
		if strings.Contains(r, "Oct 12, 2026") { // the streak's first date: the source-loss line
			return strings.TrimPrefix(r, "WARN: ")
		}
	}
	return ""
}

// covers: MA-1506, MA-693, R18
// The later words for both stops (docs/design-per-mailbox-archives.md §11, revision 2.1's F-03): a
// source-lost archive whose last refused run kept a reason says, after 3 days, that reason's words in
// status (and the banner, from the same helper) in place of "reconnect the source", and at 30 days
// the pause says its own: moved, "Thunderbird stopped using this archive's mail folder on DATE: …";
// removed, "Thunderbird no longer has the account this archive backs up (since DATE). …". Positive
// twins first, each keeping today's words: a record with no reason; a planted reason outside the
// closed set; an encrypted archive's record, whose allow-list keeps no reason (its bytes gain no
// field).
func TestTheReasonWordsReplaceReconnectTheSource(t *testing.T) {
	const reconnect = "this archive's source has not been reachable since Oct 12, 2026 — reconnect the source (its mailbox or mail folder) to resume backups"
	if got := statusLost(t, reasonArchive(t, "", false)); got != reconnect {
		t.Fatalf("positive twin: a record with no reason keeps today's words: %q", got)
	}
	if got := statusLost(t, reasonArchive(t, "qz-planted", false)); got != reconnect {
		t.Errorf("positive twin: a planted reason outside the closed set keeps today's words: %q", got)
	}
	enc := reasonArchive(t, state.SourceReasonMoved, true)
	if data, _ := os.ReadFile(filepath.Join(enc, state.LastRunName)); bytes.Contains(data, []byte("source_reason")) {
		t.Errorf("an encrypted archive's record gained source_reason:\n%s", data)
	}
	if got := statusLost(t, enc); got != reconnect {
		t.Errorf("positive twin: an encrypted archive keeps today's words: %q", got)
	}
	if SourceReasonWordsAt(enc) != "" || SourceReasonPauseWordsAt(enc) != "" {
		t.Error("positive twin: an encrypted archive's banner and pause keep today's words")
	}

	// Amended 2026-11-25 (the friction review's F12; nothing weakened): the moved words also give the
	// run's own second way; they read "… click Make a new archive… and choose it again; then click
	// Pause …" before. The 30-day pause's words (an email's) read "Its mail stays here." before.
	moved := "Thunderbird stopped using this archive's mail folder on Oct 12, 2026: its account now keeps its mail in another folder. The mail saved so far stays here. To back that account up, click Make a new archive… and choose it again (or choose One archive for each mailbox and the same folder as before); then click Pause (make read-only) on this archive."
	removed := "Thunderbird no longer has the account this archive backs up (since Oct 12, 2026). The mail saved so far stays here. To stop its backups, click Pause (make read-only)."
	for reason, want := range map[string]string{state.SourceReasonMoved: moved, state.SourceReasonRemoved: removed} {
		dir := reasonArchive(t, reason, false)
		if got := statusLost(t, dir); got != want {
			t.Errorf("%s: status says\n%q\nwant\n%q", reason, got, want)
		}
		if got := SourceReasonWordsAt(dir); got != want {
			t.Errorf("%s: the banner's words %q", reason, got)
		}
	}

	// The 30-day pause's words.
	for reason, want := range map[string]string{
		state.SourceReasonMoved:   "MailArchive paused this archive's scheduled backup: Thunderbird has not used its mail folder since Oct 12, 2026. Its mail stays in the archive. To back that account up, click Make a new archive… and choose it again.",
		state.SourceReasonRemoved: "MailArchive paused this archive's scheduled backup: Thunderbird has not had its account since Oct 12, 2026. Its mail stays in the archive.",
		"":                        "",
	} {
		if got := SourceReasonPauseWordsAt(reasonArchive(t, reason, false)); got != want {
			t.Errorf("the pause for %q: %q, want %q", reason, got, want)
		}
	}
	// Below the 3 days the banner says nothing yet; the pause's words need only the first date.
	young := t.TempDir()
	if err := state.WriteSourceStreak(young, state.SourceStreak{ConsecutiveFails: 1, FirstUnreachable: time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC), LastUnreachable: time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC), LastKind: "local-source-unreachable"}); err != nil {
		t.Fatal(err)
	}
	if err := state.WriteLastRun(young, state.LastRun{Status: state.RunFailed, SourceReason: state.SourceReasonMoved}); err != nil {
		t.Fatal(err)
	}
	if SourceReasonWordsAt(young) != "" {
		t.Error("a streak below the threshold gives the banner no words")
	}
}
