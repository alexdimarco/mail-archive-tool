package wizard

import (
	"testing"

	"github.com/ncruces/zenity"

	"mail-archive-tool/internal/assure"
)

// covers: MA-952
// A scheduled backup's notices (it failed; it works again) are titled "MailArchive
// backup", not "MailArchive wizard": nobody opened the wizard when a backup runs by
// itself (the adversarial review of ruling B, 2026-10-31, finding 1). Positive twin
// first: the wizard's own windows keep the title "MailArchive wizard", and both notices
// still reach the notification seam with their words.
func TestScheduledBackupNoticesAreNotTitledWizard(t *testing.T) {
	if appTitle != "MailArchive wizard" {
		t.Fatalf("positive twin: the wizard's windows are titled %q, want MailArchive wizard", appTitle)
	}

	// The default path still raises the notice through notify, with its words.
	var texts []string
	oldNotify := notify
	notify = func(text string, _ ...zenity.Option) error { texts = append(texts, text); return nil }
	t.Cleanup(func() { notify = oldNotify })
	notifyFailure("/mail/archive")
	notifyRecovered("/mail/archive")
	texts = assure.Reached(t, texts, "the two notices")
	if len(texts) != 2 || texts[0] != failureNotice("/mail/archive") || texts[1] != recoveredNotice("/mail/archive") {
		t.Fatalf("the notices did not reach notify with their words: %q", texts)
	}

	// Their title.
	var titles []string
	oldSend := sendNotice
	sendNotice = func(title, _ string) { titles = append(titles, title) }
	t.Cleanup(func() { sendNotice = oldSend })
	notifyFailure("/mail/archive")
	notifyRecovered("/mail/archive")
	titles = assure.Reached(t, titles, "the notices' titles")
	if len(titles) != 2 {
		t.Fatalf("want 2 notices, got %d", len(titles))
	}
	for i, got := range titles {
		if got != "MailArchive backup" {
			t.Errorf("notice %d is titled %q, want MailArchive backup", i, got)
		}
	}
}
