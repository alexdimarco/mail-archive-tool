package util

import "testing"

// covers: MA-117, R18, S29
// UnderCloudSync flags a OneDrive-synced archive two ways: a path segment equal
// to or beginning with "OneDrive" (so "OneDrive - Company" and
// "OneDriveCommercial" match, case-insensitively), and a path under one of the
// OneDrive* environment roots the client sets. A plainly local path is not
// flagged, and the returned service names the sync provider.
func TestUnderCloudSync(t *testing.T) {
	// No env roots for the segment cases.
	t.Setenv("OneDrive", "")
	t.Setenv("OneDriveCommercial", "")
	t.Setenv("OneDriveConsumer", "")

	seg := map[string]bool{
		"/home/u/OneDrive/Mail":              true,
		"/home/u/OneDrive - Company/Archive": true,
		"/home/u/onedrive/lower/Mail":        true,
		`C:\Users\u\OneDriveCommercial\x`:    true,
		"/home/u/Documents/Mail":             false,
		"/home/u/one/drive/Mail":             false,
	}
	for p, want := range seg {
		svc, ok := UnderCloudSync(p)
		if ok != want {
			t.Errorf("UnderCloudSync(%q) ok=%v, want %v", p, ok, want)
		}
		if ok && svc != "OneDrive" {
			t.Errorf("UnderCloudSync(%q) service=%q, want OneDrive", p, svc)
		}
	}

	// Env-root detection: a path under $OneDrive is flagged with no OneDrive
	// segment in it; a sibling path is not.
	t.Setenv("OneDrive", "/mnt/cloud/od-root")
	if svc, ok := UnderCloudSync("/mnt/cloud/od-root/Mail/Archive"); !ok || svc != "OneDrive" {
		t.Errorf("path under $OneDrive not detected: ok=%v svc=%q", ok, svc)
	}
	if _, ok := UnderCloudSync("/mnt/cloud/other/Mail"); ok {
		t.Errorf("a path outside $OneDrive was wrongly flagged")
	}
}
