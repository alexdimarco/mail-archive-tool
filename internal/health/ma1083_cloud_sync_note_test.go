package health

import (
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/util"
)

// covers: MA-1083
// An archive in a cloud-sync folder is a "Keep it local" WARN (owner's ruling of
// 2026-11-13, superseding the 2026-11-05 information line): the cloud_sync reason
// is marked WARN, moves an otherwise healthy archive's posture to WARN, names the
// sync app, gives the reason (many small files, lost without any error) and the
// remedy (a folder on this computer the app does not sync, backed up separately).
// The positive twin: the same archive outside a synced folder has no such line and
// stays GREEN.
func TestACloudSyncFolderIsAKeepItLocalWarning(t *testing.T) {
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	in := healthyInput(now)
	in.OutUnderSync = "Nextcloud"
	rep := Assess(in, now)

	if rep.Posture != "WARN" {
		t.Errorf("a synced folder left the posture %s; want WARN (keep it local):\n%s", rep.Posture, strings.Join(rep.Reasons, "\n"))
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
	if !strings.HasPrefix(line, "WARN:") {
		t.Errorf("the synced-folder line is not marked WARN: %q", line)
	}
	for _, want := range []string{
		"Keep it local: this archive is in Nextcloud's synced folder.",
		"A backup writes thousands of small files in a few minutes, and sync apps can lose some of them while they catch up — files go missing from the archive without any error.",
		"Move the archive to a folder on this computer that Nextcloud does not sync (Settings → Archive location, or mailarchive with a local -out).",
		"If you want a copy off this computer, back up that local folder separately.",
	} {
		if !strings.Contains(line, want) {
			t.Errorf("the synced-folder line does not say %q:\n%q", want, line)
		}
	}

	// The positive twin: the same archive outside a synced folder has no such line.
	in.OutUnderSync = ""
	twin := Assess(in, now)
	for _, c := range twin.Codes {
		if c == "cloud_sync" {
			t.Errorf("an archive outside a synced folder got a cloud_sync line")
		}
	}
	if twin.Posture != "GREEN" {
		t.Errorf("an archive outside a synced folder is %s; want GREEN", twin.Posture)
	}
}

// covers: MA-1083
// The words read correctly for every sync app the detection names, and when it cannot
// name the app (an unknown Library/CloudStorage provider) they turn generic — never a
// blank or a wrong name (owner, 2026-11-13). Both the status line and the short
// schedule-time note.
func TestKeepItLocalWordsFitEverySyncApp(t *testing.T) {
	for _, svc := range []string{"Nextcloud", "OneDrive", "iCloud Drive", "Dropbox", "Google Drive"} {
		for _, txt := range []string{KeepItLocalText(svc), KeepItLocalShort(svc)} {
			for _, want := range []string{
				"Keep it local: this archive is in " + svc + "'s synced folder",
				"folder on this computer that " + svc + " does not sync",
				"without any error",
			} {
				if !strings.Contains(txt, want) {
					t.Errorf("%s: missing %q:\n%q", svc, want, txt)
				}
			}
		}
	}

	for _, unknown := range []string{util.UnknownCloudService, ""} {
		for _, txt := range []string{KeepItLocalText(unknown), KeepItLocalShort(unknown)} {
			for _, want := range []string{
				"Keep it local: this archive is in a folder a cloud sync app keeps in step with an online copy",
				"folder on this computer that the sync app does not sync",
				"without any error",
			} {
				if !strings.Contains(txt, want) {
					t.Errorf("unknown provider %q: missing %q:\n%q", unknown, want, txt)
				}
			}
			if strings.Contains(txt, "'s synced folder") || strings.Contains(txt, "cloud storage service") {
				t.Errorf("unknown provider %q named a service: %q", unknown, txt)
			}
		}
	}

	// The detection's own name for an unknown CloudStorage provider reaches the fallback.
	svc, ok := util.UnderCloudSync("/Users/x/Library/CloudStorage/SomeNewApp-account/Mail")
	if !ok || svc != util.UnknownCloudService {
		t.Fatalf("an unknown CloudStorage provider = %q,%v; want %q,true", svc, ok, util.UnknownCloudService)
	}
	if !strings.Contains(KeepItLocalText(svc), "the sync app does not sync") {
		t.Errorf("the unknown CloudStorage provider did not get the generic words: %q", KeepItLocalText(svc))
	}
}
