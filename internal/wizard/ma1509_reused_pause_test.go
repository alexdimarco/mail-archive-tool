package wizard

import (
	"testing"
	"time"

	"mail-archive-tool/internal/health"
	"mail-archive-tool/internal/state"
)

// covers: MA-1509, MA-871, R18
// The 30-day pause of an archive whose last refused run kept source_reason reused (docs/design-per-
// mailbox-archives.md revision 3, INS-03) says the reason's own pause words, in its email and its log
// line (AutoPauseWhyFor, the one place both read), in place of "reconnect the source, then … click
// Repair". Positive twin first: a stop with no reason keeps today's AutoPauseWhy.
func TestThePauseSaysTheReusedWords(t *testing.T) {
	first := time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC)
	archive := func(reason string) string {
		t.Helper()
		dir := t.TempDir()
		if err := state.WriteSourceStreak(dir, state.SourceStreak{ConsecutiveFails: 30, FirstUnreachable: first, LastUnreachable: first.Add(state.AutoPauseAfter), LastKind: "local-source-unreachable"}); err != nil {
			t.Fatal(err)
		}
		if err := state.WriteLastRun(dir, state.LastRun{Status: state.RunFailed, Started: first, Finished: first, Error: "x", SourceReason: reason}); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	if got := AutoPauseWhyFor(archive("")); got != AutoPauseWhy {
		t.Fatalf("positive twin: a stop with no reason pauses in today's words: %q", got)
	}
	dir := archive(state.SourceReasonReused)
	// Restated 2026-11-25 (the re-verify of rounds A and B, RF-02: the pause email named no next step),
	// beside the old text, kept here: "MailArchive paused this archive's scheduled backup: Thunderbird
	// has used its mail folder for a different account since Oct 12, 2026. Its mail stays in the
	// archive." It now goes on with the next step, compared whole.
	want := "MailArchive paused this archive's scheduled backup: Thunderbird has used its mail folder for a different account since Oct 12, 2026. Its mail stays in the archive. To back up the account that uses it now, click Make a new archive… in MailArchive Desktop and choose it."
	if got := AutoPauseWhyFor(dir); got != want || got != health.SourceReasonPauseWordsAt(dir) {
		t.Errorf("the reused archive's pause: %q, want %q", got, want)
	}
}
