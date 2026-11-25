package health

import (
	"testing"

	"mail-archive-tool/internal/state"
)

// covers: MA-1509, MA-693, R18
// The reused reason's later words (docs/design-per-mailbox-archives.md revision 3: "Status, the
// banner and the 30-day pause get this reason's words as F-03 gives the others"): a source-lost
// archive whose last refused run kept source_reason reused says, after 3 days, in status and the
// banner (one helper), "Thunderbird now uses this archive's mail folder for a different account (since
// DATE), so its backups stopped. …" in place of "reconnect the source", and at 30 days the pause says
// "MailArchive paused this archive's scheduled backup: Thunderbird has used its mail folder for a
// different account since DATE. …". The code is read back as one of the closed set. Positive twin
// first: a record with no reason keeps today's words.
func TestTheReusedReasonsWords(t *testing.T) {
	const reconnect = "this archive's source has not been reachable since Oct 12, 2026 — reconnect the source (its mailbox or mail folder) to resume backups"
	if got := statusLost(t, reasonArchive(t, "", false)); got != reconnect {
		t.Fatalf("positive twin: a record with no reason keeps today's words: %q", got)
	}
	dir := reasonArchive(t, state.SourceReasonReused, false)
	if lr, _, _ := state.ReadLastRun(dir); lr.SourceReason != state.SourceReasonReused {
		t.Fatalf("reused is read back as one of the closed set: %q", lr.SourceReason)
	}
	want := "Thunderbird now uses this archive's mail folder for a different account (since Oct 12, 2026), so its backups stopped. The mail saved so far stays here. To back up that account, click Make a new archive… and choose it; to stop this archive's backups, click Pause (make read-only)."
	if got := statusLost(t, dir); got != want {
		t.Errorf("status says\n%q\nwant\n%q", got, want)
	}
	if got := SourceReasonWordsAt(dir); got != want {
		t.Errorf("the banner's words %q", got)
	}
	if got, want := SourceReasonPauseWordsAt(dir), "MailArchive paused this archive's scheduled backup: Thunderbird has used its mail folder for a different account since Oct 12, 2026. Its mail stays in the archive."; got != want {
		t.Errorf("the pause's words %q, want %q", got, want)
	}
	if enc := reasonArchive(t, state.SourceReasonReused, true); SourceReasonWordsAt(enc) != "" || statusLost(t, enc) != reconnect {
		t.Error("an encrypted archive keeps today's words: its record keeps no reason")
	}
}
