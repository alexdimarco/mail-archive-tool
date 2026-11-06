package health

import (
	"strings"
	"testing"
	"time"
)

// covers: MA-1083
// An archive the person chose to keep in a cloud-sync folder is not a warning (owner's
// ruling of 2026-11-05): the reason is an information line that names the service and
// what a non-synced folder would change, carries the cloud_sync code, and leaves the
// posture of an otherwise healthy archive GREEN.
func TestACloudSyncFolderIsAnInformationLineNotAWarning(t *testing.T) {
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	in := healthyInput(now)
	in.OutUnderSync = "Nextcloud"
	rep := Assess(in, now)

	if rep.Posture != "GREEN" {
		t.Errorf("a synced folder made the posture %s; want GREEN (information only):\n%s", rep.Posture, strings.Join(rep.Reasons, "\n"))
	}
	var line string
	for i, c := range rep.Codes {
		if c == "cloud_sync" {
			line = rep.Reasons[i]
		}
	}
	if line == "" {
		t.Fatalf("no cloud_sync reason: codes %v", rep.Codes)
	}
	if strings.HasPrefix(line, "WARN:") || strings.HasPrefix(line, "RED:") {
		t.Errorf("the synced-folder line is still marked as a problem: %q", line)
	}
	for _, want := range []string{"Nextcloud", "synced folder", "non-synced folder"} {
		if !strings.Contains(line, want) {
			t.Errorf("the synced-folder line does not say %q: %q", want, line)
		}
	}

	// The positive twin: the same archive outside a synced folder has no such line.
	in.OutUnderSync = ""
	for _, c := range Assess(in, now).Codes {
		if c == "cloud_sync" {
			t.Errorf("an archive outside a synced folder got a cloud_sync line")
		}
	}
}
