package util

import "testing"

// covers: MA-813, R37
// The synced-destination check an unencrypted copy asks twice for (phase 3 §10, K5),
// one case per provider: macOS keeps every File Provider client's root under
// Library/CloudStorage and iCloud Drive's store under Library/Mobile Documents, so
// everything there is synced, named by its provider where the folder says which; the
// providers added beside the earlier five are each recognised by their root's name;
// a folder that only resembles one is not flagged.
func TestUnderCloudSyncKnowsTheMacFoldersAndEachProvider(t *testing.T) {
	t.Setenv("OneDrive", "")
	t.Setenv("OneDriveCommercial", "")
	t.Setenv("OneDriveConsumer", "")

	flagged := map[string]string{
		"/Users/u/Library/CloudStorage/GoogleDrive-u@example.com/Archive": "Google Drive",
		"/Users/u/Library/CloudStorage/OneDrive-Personal/Mail":            "OneDrive",
		"/Users/u/Library/CloudStorage/Dropbox/Mail":                      "Dropbox",
		"/Users/u/Library/CloudStorage/Box-Box/Mail":                      "Box",
		"/Users/u/Library/CloudStorage/SomeNewService/Mail":               "a cloud storage service",
		"/Users/u/Library/CloudStorage":                                   "a cloud storage service",
		"/Users/u/Library/Mobile Documents/com~apple~CloudDocs/Mail":      "iCloud Drive",
		`C:\Users\u\iCloudDrive\Mail`:                                     "iCloud Drive",
		"/home/u/Nextcloud/Mail":                                          "Nextcloud",
		"/home/u/ownCloud/Mail":                                           "ownCloud",
		"/Users/u/pCloud Drive/Mail":                                      "pCloud",
		`C:\Users\u\SynologyDrive\Mail`:                                   "Synology Drive",
	}
	for p, want := range flagged {
		if svc, ok := UnderCloudSync(p); !ok || svc != want {
			t.Errorf("UnderCloudSync(%q) = %q,%v; want %q,true", p, svc, ok, want)
		}
	}
	for _, p := range []string{
		"/Users/u/Library/Mail/V10",
		"/Users/u/Library/Containers/x",
		"/Users/u/Documents/CloudStorage/Mail",
		"/home/u/MyNextcloudNotes/x",
	} {
		if svc, ok := UnderCloudSync(p); ok {
			t.Errorf("UnderCloudSync(%q) wrongly flagged as %q", p, svc)
		}
	}
}

// covers: MA-813, R37
// "This folder may be synced" (phase 3 §10): on macOS the Desktop and Documents
// folders, and everything in them, may be kept in iCloud without the path saying so;
// another folder in the home folder is not, and no other system says it.
func TestMaySyncIsTheMacDesktopAndDocuments(t *testing.T) {
	home := "/Users/u"
	for _, p := range []string{"/Users/u/Desktop", "/Users/u/Documents/Mail copy", "/Users/u/desktop/x"} {
		if !maySync(p, home, "darwin") {
			t.Errorf("%q on macOS: want may be synced", p)
		}
	}
	for _, p := range []string{"/Users/u", "/Users/u/Mail", "/Users/u/DocumentsOld/x", "/Volumes/Backup/Documents"} {
		if maySync(p, home, "darwin") {
			t.Errorf("%q on macOS: wrongly said may be synced", p)
		}
	}
	if maySync("/home/u/Documents/x", "/home/u", "linux") || maySync(`C:\Users\u\Documents\x`, `C:\Users\u`, "windows") {
		t.Error("another system said its Documents folder may be synced")
	}
	if maySync("/Users/u/Desktop", "", "darwin") {
		t.Error("an unknown home folder said may be synced")
	}
}
