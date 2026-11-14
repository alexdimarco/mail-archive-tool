package state

import "testing"

// covers: MA-1299, R45
// The Gmail views show Gmail's own folder names, "All Mail" and "Sent Mail", never the internal
// path "[Gmail]/All Mail" (the B3 friction review's F11). Positive twin first: a folder that is not
// under "[Gmail]/" (or the UK accounts' "[Google Mail]/") is shown as it is, and so is "[Gmail]"
// alone; only the leading segment is dropped.
func TestGmailFoldersAreShownByTheirOwnNames(t *testing.T) {
	for in, want := range map[string]string{
		"INBOX":                   "INBOX",
		"Receipts/2024":           "Receipts/2024",
		"[Gmail]":                 "[Gmail]",
		"Work/[Gmail]/x":          "Work/[Gmail]/x",
		"[Gmail]/All Mail":        "All Mail",
		"[Gmail]/Sent Mail":       "Sent Mail",
		"[Google Mail]/Sent Mail": "Sent Mail",
	} {
		if got := ShownFolder(in); got != want {
			t.Errorf("ShownFolder(%q) = %q, want %q", in, got, want)
		}
	}
}
