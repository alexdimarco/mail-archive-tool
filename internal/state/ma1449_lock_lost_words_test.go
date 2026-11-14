package state

import (
	"strings"
	"testing"
)

// covers: MA-1449, R18
// An encrypted archive's record, notice and alert say only its failure kind's fixed sentence. A
// lost lock's (lock_lost) now says what happened and what to do, as a plaintext archive's words
// do since 1.7.2 (the friction review's F7): the archive folder changed during the backup, why
// that can be, and "connect this archive's drive and run the backup again". It no longer blames
// another program alone. Positive twin first: another kind keeps its own sentence.
func TestALostLocksFixedSentenceSaysWhatToDo(t *testing.T) {
	if got := FailureText(FailArchiveBusy); got != "another backup of this archive was running" {
		t.Fatalf("positive twin: archive_busy says %q", got)
	}
	want := "the backup stopped because the archive folder changed during the backup (its drive was swapped or disconnected, the folder was moved, or another program changed MailArchive's lock file there) — connect this archive's drive and run the backup again"
	if got := FailureText(FailLockLost); got != want {
		t.Fatalf("lock_lost says %q, want %q", got, want)
	}
	if strings.Contains(FailureText(FailLockLost), "took over") {
		t.Fatal("lock_lost still says another program took over")
	}
}
