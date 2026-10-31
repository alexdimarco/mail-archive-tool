package wizard

import (
	"strings"
	"testing"
)

// covers: MA-939, S65
// The wizard's notices say what is true and where to look (the wording pass before 1.5.2).
// Positive twin first: the schedule confirmation still says the sync runs before each run.
// It now also says a run skips the sync while someone is using the computer, and that
// MailArchive warns after 3 days without a sync. A failed or recovered scheduled backup's
// notification names the archive's folder, not the schedule's hashed name, and points at
// MailArchive Desktop (with several archives, the wizard's health check shows only the last
// one it worked on). The auto-pause email says how long (30 days), that the archive is kept,
// and the real way back: Repair on the Scheduled backup card, never "re-enable".
func TestTheWizardsNoticesSayWhatIsTrue(t *testing.T) {
	if !strings.HasPrefix(scheduleSyncLine, "\nThunderbird: synced before each run.") {
		t.Fatalf("positive twin: the confirmation's sync line: %q", scheduleSyncLine)
	}
	if scheduleSyncLine != "\nThunderbird: synced before each run. A run skips the sync when Thunderbird is already open or someone is using the computer; it then saves the mail already on this computer, and a later run picks up the rest. After 3 days without a sync, MailArchive warns you." {
		t.Errorf("the confirmation's sync line: %q", scheduleSyncLine)
	}
	const out = "/Volumes/Backup/Mail"
	if got := failureNotice(out); got != "MailArchive: the scheduled backup of /Volumes/Backup/Mail failed. Open MailArchive Desktop to see why." {
		t.Errorf("the failure notice: %q", got)
	}
	if got := recoveredNotice(out); got != "MailArchive: scheduled backups of /Volumes/Backup/Mail are working again." {
		t.Errorf("the recovery notice: %q", got)
	}
	if AutoPauseWhy != "the mailbox or mail folder has been unreachable for 30 days, so its scheduled backup was stopped. Your archive is kept. Do this: reconnect the source, then in MailArchive Desktop click Repair on the archive's Scheduled backup card to start the schedule again." {
		t.Errorf("the auto-pause reason: %q", AutoPauseWhy)
	}
	for _, s := range []string{failureNotice(out), recoveredNotice(out), AutoPauseWhy} {
		if strings.Contains(s, "Mail Archive") || strings.Contains(s, "mailarchive-") || strings.Contains(s, "re-enable") {
			t.Errorf("a notice names the program two ways, a hashed schedule name, or \"re-enable\": %q", s)
		}
	}
}
