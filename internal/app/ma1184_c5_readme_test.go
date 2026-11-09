package app

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/state"
)

// covers: MA-1184
// The archive's README.txt names the behind-streak record (slice C5; §13's C2 note: "the archive
// README's line for the new file"), as it names every housekeeping file MailArchive writes into the
// archive's top folder, in plain words. The encrypted archive's note names no file (it keeps none).
// Positive twin first: the presync streak's line is there.
func TestTheReadmeNamesTheBehindStreakRecord(t *testing.T) {
	text := strings.Join(strings.Fields(archiveReadme), " ")
	if !strings.Contains(text, ".mailarchive-syncstreak.json how many scheduled backups") {
		t.Fatal("positive twin: the presync streak's line is gone")
	}
	want := state.TBStreakName + ` how long Thunderbird has been downloading each account's mail, and whether its emails were sent (only for an archive of Thunderbird mail; used for the 3-day "still downloading" and "stopped downloading" emails)`
	if !strings.Contains(text, want) {
		t.Errorf("README.txt lacks %q", want)
	}
	if strings.Contains(EncryptedNote(), "tbstreak") {
		t.Error("the encrypted note names the behind-streak record")
	}
}
