package util

import "testing"

// covers: MA-160, R18, S29
// UnderCloudSync recognises the common consumer cloud-sync roots beyond
// OneDrive — Dropbox, Google Drive ("My Drive"), iCloud Drive and Box — by a
// path segment, returning the matched service name; a folder that merely
// contains one of those words as part of a larger segment is not flagged.
func TestUnderCloudSyncServices(t *testing.T) {
	t.Setenv("OneDrive", "")
	t.Setenv("OneDriveCommercial", "")
	t.Setenv("OneDriveConsumer", "")

	flagged := map[string]string{
		"/home/u/Dropbox/Mail":         "Dropbox",
		"/home/u/My Drive/Mail":        "Google Drive",
		"/home/u/Google Drive/Archive": "Google Drive",
		"/home/u/iCloud Drive/Mail":    "iCloud Drive",
		"/home/u/Box/Mail":             "Box",
		`C:\Users\u\Dropbox\x`:         "Dropbox",
	}
	for p, want := range flagged {
		svc, ok := UnderCloudSync(p)
		if !ok || svc != want {
			t.Errorf("UnderCloudSync(%q) = %q,%v; want %q,true", p, svc, ok, want)
		}
	}

	notFlagged := []string{
		"/home/u/Documents/Mail",
		"/home/u/one/drive/Mail",
		"/home/u/Inbox/Mail",
		"/home/u/MyDropboxNotes/x",
	}
	for _, p := range notFlagged {
		if svc, ok := UnderCloudSync(p); ok {
			t.Errorf("UnderCloudSync(%q) wrongly flagged as %q", p, svc)
		}
	}
}
