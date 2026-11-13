package job

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// covers: MA-1236, R45
// The person's "Not now" to the Gmail switch (Q-B) is kept on this computer, in the archive's job
// file, never in the archive: it round-trips, and a job file written before it reads as not
// answered. Positive twin first: a job with no answer writes no such field.
func TestTheGmailNotNowIsKeptOnThisComputer(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "plain.json")
	if err := Write(plain, Job{Name: "plain", Out: "/archive", Mode: "incremental"}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(plain)
	if strings.Contains(string(data), "gmail_not_now") {
		t.Fatalf("positive twin: a job with no answer wrote the field: %s", data)
	}
	if j, err := Read(plain); err != nil || j.GmailNotNow {
		t.Fatalf("a job with no answer reads as Not now: %v", err)
	}
	path := filepath.Join(dir, "asked.json")
	if err := Write(path, Job{Name: "asked", Out: "/archive", Mode: "incremental", GmailNotNow: true}); err != nil {
		t.Fatal(err)
	}
	j, err := Read(path)
	if err != nil || !j.GmailNotNow {
		t.Fatalf("Not now did not round-trip: %v %+v", err, j)
	}
}
