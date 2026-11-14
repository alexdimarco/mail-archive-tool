package app

import (
	"bytes"
	"context"
	"log"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/lockfile"
)

// covers: MA-1293, R45
// The B3 friction review's F7, proved: the Gmail question is asked inside the backup, after the
// archive's lock is taken, so while it waits for an answer the lock is held and any other backup
// of the archive (a scheduled one) is kept out. That is why the terminal's question has a time
// limit (cmd/mailarchive's TestTheTerminalQuestionHasATimeLimitAndANotNowIsRemembered): once the
// question returns Not now the run goes on, finishes and lets the lock go. And F13: a yes typed at
// the terminal is said once, by the terminal; the run's log says "you said yes" only for a yes
// given before the run (-gmail-folders, or the card's). Positive twin first: -gmail-folders's yes
// is logged.
func TestTheGmailQuestionIsAskedWhileTheArchiveIsLocked(t *testing.T) {
	// Positive twin: the yes given before the run is logged.
	store, out := existingGmailArchive(t)
	var logged bytes.Buffer
	opts := Options{Inputs: []string{store}, Out: out, GmailYes: true, Mode: export.Incremental}
	if _, err := Run(context.Background(), opts, log.New(&logged, "", 0), nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logged.String(), "Gmail: you said yes") {
		t.Fatalf("positive twin: -gmail-folders's yes is not logged: %q", logged.String())
	}

	// The question waits while the lock is held.
	store, out = existingGmailArchive(t)
	lockPath := filepath.Join(out, lockfile.Name)
	heldWhileAsking := false
	opts = Options{Inputs: []string{store}, Out: out, Mode: export.Incremental, AskGmail: func(string) bool {
		heldWhileAsking = lockfile.Held(lockPath)
		return false // what the terminal returns once its time limit passes
	}}
	if _, err := Run(context.Background(), opts, discard(), nil); err != nil {
		t.Fatal(err)
	}
	if !heldWhileAsking {
		t.Fatal("the archive's lock was not held while the question waited (the review's F7 finding no longer holds; revisit the time limit)")
	}
	if lockfile.Held(lockPath) {
		t.Fatal("the lock is still held after the run answered Not now")
	}

	// A yes typed at the terminal: the run does not log a second "you said yes".
	logged.Reset()
	opts = Options{Inputs: []string{store}, Out: out, Mode: export.Incremental, AskGmail: func(string) bool { return true }}
	if _, err := Run(context.Background(), opts, log.New(&logged, "", 0), nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(logged.String(), "you said yes") {
		t.Fatalf("a yes typed at the terminal is said twice: %q", logged.String())
	}
}
