package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"mail-archive-tool/internal/gmailanswer"
	"mail-archive-tool/internal/state"
)

// covers: MA-1311, R45
// `status`'s Gmail lines come from one source for its text, `status -json` and the Desktop's
// Status & health view (the B3 UX walk's U7: the waiting line is a reason with the code
// gmail_waiting, the count line one with gmail_counts). A question nobody answered at a terminal
// (the store recorded as held, with no Not now kept here) still says the question waits (U4); a
// Not now kept here silences it; and an archive at version 8 (another computer tidying it) says
// only the tidy-up WARN, never "back up once at a terminal" (U3). Positive twin first: an archive
// with no Gmail store has no Gmail line and no Gmail code.
func TestStatusGmailLinesComeFromOneSource(t *testing.T) {
	src := filepath.Join(t.TempDir(), "Inbox")
	writeMaildirMsg(t, src, "1.eml", sampleMsg)
	dir := filepath.Join(t.TempDir(), "archive")
	if code, stderr := runCLI("-out", dir, "-input", src); code != 0 {
		t.Fatalf("the run: exit %d, %s", code, stderr)
	}
	codes := func() []string {
		t.Helper()
		got, code := statusOf(t, dir, "-json")
		var doc struct {
			Codes []string `json:"reason_codes"`
		}
		if code != 0 || json.Unmarshal([]byte(got), &doc) != nil {
			t.Fatalf("status -json (exit %d): %s", code, got)
		}
		return doc.Codes
	}
	line := state.GmailWaitingLine("imap.gmail.com")
	if got, _ := statusOf(t, dir); strings.Contains(got, "Gmail:") || slices.Contains(codes(), "gmail_waiting") {
		t.Fatalf("positive twin: Gmail lines with no Gmail store (codes %v):\n%s", codes(), got)
	}

	// An unanswered terminal question: the run keeps the store as held.
	lr, _, _ := state.ReadLastRun(dir)
	lr.GmailHeld = []string{"imap.gmail.com"}
	if err := state.WriteLastRun(dir, lr); err != nil {
		t.Fatal(err)
	}
	if got, _ := statusOf(t, dir); !strings.Contains(got, line) {
		t.Fatalf("status says nothing after an unanswered question:\n%s", got)
	}
	if !slices.Contains(codes(), "gmail_waiting") {
		t.Fatalf("status -json has no gmail_waiting code: %v", codes())
	}
	if err := gmailanswer.KeepHere(dir, gmailanswer.Answer{Archive: lr.Archive, NotNow: true}); err != nil {
		t.Fatal(err)
	}
	if got, _ := statusOf(t, dir); strings.Contains(got, line) {
		t.Fatalf("status says the question waits after a Not now kept here:\n%s", got)
	}
	p, _ := gmailanswer.Path(dir)
	os.Remove(p)

	// Version 8: the tidy-up WARN only.
	lr.GmailHeld, lr.GmailOff = nil, []string{"imap.gmail.com"}
	if err := state.WriteLastRun(dir, lr); err != nil {
		t.Fatal(err)
	}
	if got, _ := statusOf(t, dir); !strings.Contains(got, line) {
		t.Fatalf("positive twin: the question waits at version 6:\n%s", got)
	}
	mp := filepath.Join(dir, ".mailarchive-manifest.json")
	b, err := os.ReadFile(mp)
	if err != nil {
		t.Fatal(err)
	}
	stamp := regexp.MustCompile(`"version":\s*\d+`)
	if err := os.WriteFile(mp, []byte(stamp.ReplaceAllString(string(b), `"version": 8`)), 0o600); err != nil {
		t.Fatal(err)
	}
	got, _ := statusOf(t, dir)
	if !strings.Contains(got, "manifest") && !strings.Contains(got, "tidying") {
		t.Fatalf("version 8 is not said:\n%s", got)
	}
	if strings.Contains(got, line) || strings.Contains(got, "back up once at a terminal") {
		t.Fatalf("a version-8 archive's status still says back up at a terminal:\n%s", got)
	}
}
