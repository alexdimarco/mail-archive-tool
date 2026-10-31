package presync

import (
	"fmt"
	"strings"
	"testing"
)

// covers: MA-936
// The hourly note's "about 20 minutes" is the wait's own limit (owner ruling 5 of
// 2026-10-31): HourlySyncNote names defMaxWait in minutes, so a change to the limit
// cannot leave the note saying another number. It says "about", never "up to"
// (review finding behaviour-3): the limit counts only the wait for mail while the
// computer is awake, and Thunderbird is also open for its start (up to defAwaitUp)
// and its quit (up to defQuitGrace), so "up to" would understate it. Positive twin
// first: the note names the limit; a note naming another number would not.
func TestTheHourlyNoteNamesTheWaitLimit(t *testing.T) {
	want := fmt.Sprintf("about %d minutes", int(defMaxWait.Minutes()))
	if !strings.Contains(HourlySyncNote, want) {
		t.Fatalf("HourlySyncNote %q does not say %q (defMaxWait %v)", HourlySyncNote, want, defMaxWait)
	}
	if other := strings.Replace(HourlySyncNote, want, "about 30 minutes", 1); strings.Contains(other, want) {
		t.Error("twin: a note naming another number still matched")
	}
	if strings.Contains(HourlySyncNote, "up to") {
		t.Errorf("HourlySyncNote %q says \"up to\": Thunderbird is open longer than the wait (its start and quit, and any sleep)", HourlySyncNote)
	}
}
