package main

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/tbrestore"
)

// covers: MA-1129, R40, R33
// W-WAIT ("While this runs, leave Thunderbird closed …") is said once at a terminal: as
// the plan's last line, never again as the run starts (the MA-1095 lab's wording note L1,
// settled 2026-11-07) — with -yes and when the person answers yes.
func TestTheWaitWordsAreSaidOnceAtTheTerminal(t *testing.T) {
	e := newTBCLI(t, "")
	code, stdout, stderr := e.run(&scripted{}, nil, "-out", e.archive, "-profile", e.profile, "-yes")
	if code != 0 || !e.restored() || !strings.Contains(stdout, "Restored 2 messages") {
		t.Fatalf("-yes restores: %d %q %q", code, stdout, stderr)
	}
	if n := strings.Count(stderr, tbrestore.WaitWords()); n != 1 {
		t.Fatalf("-yes: W-WAIT said %d times, want once: %q", n, stderr)
	}

	e = newTBCLI(t, "")
	a := &scripted{isAttended: true, answers: []string{"y"}}
	code, _, stderr = e.run(a, nil, "-out", e.archive, "-profile", e.profile)
	if code != 0 || !e.restored() {
		t.Fatalf("at a terminal, yes restores: %d %q", code, stderr)
	}
	if n := strings.Count(stderr, tbrestore.WaitWords()); n != 1 {
		t.Fatalf("at a terminal: W-WAIT said %d times, want once: %q", n, stderr)
	}
}
