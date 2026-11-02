package archdelete

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/state"
)

// covers: MA-1038, R36
// The mail folders named inside a store are found whatever the case of the store's spelling where
// the file system ignores case (Windows), and only there (the settle pass's LOW-7: no test caught
// the folding removed). Positive twin first: the same spelling is found on every platform.
func TestStoreFoldersFoldCaseWhereTheFileSystemDoes(t *testing.T) {
	m := &state.Manifest{Entries: map[string]state.Record{
		"k1": {Path: "mbox/2024-03-01_0900_a_11111111.html", Folder: "Inbox"},
		"k2": {Path: "MBOX/2024-03-01_0901_b_22222222.html", Folder: "Sent"},
	}}
	// Positive twin: the store's own spelling, on Linux.
	if got := storeFolders(m, []string{"mbox"}, "linux"); len(got) != 1 || strings.Join(got[0], ",") != "Inbox" {
		t.Fatalf("the twin: %q", got)
	}
	if got := storeFolders(m, []string{"mbox"}, "windows"); len(got) != 1 || strings.Join(got[0], ",") != "Inbox,Sent" {
		t.Fatalf("windows, the store spelled otherwise by a record: %q", got)
	}
}

// covers: MA-1038, R36
// The names said on screen have their control characters stripped, so a file named with a
// terminal's escape cannot redraw the terminal (the settle pass's LOW-7: no test caught the
// stripping removed). Positive twin first: a plain name is said as it is.
func TestNamesAndMoreStripsControlCharacters(t *testing.T) {
	if got := namesAndMore([]string{"notes.txt"}); got != "notes.txt" {
		t.Fatalf("the twin: %q", got)
	}
	got := namesAndMore([]string{"a\x1b[2Jb.txt", "c\r\nd.txt"})
	if strings.ContainsAny(got, "\x1b\r\n") || !strings.Contains(got, "b.txt") || !strings.Contains(got, "d.txt") {
		t.Fatalf("control characters said: %q", got)
	}
}
