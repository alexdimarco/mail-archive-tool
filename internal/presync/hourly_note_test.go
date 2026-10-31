package presync

import (
	"fmt"
	"strings"
	"testing"
)

// covers: MA-936
// The hourly note's "up to 20 minutes" is the wait's own limit (owner ruling 5 of
// 2026-10-31): HourlySyncNote names defMaxWait in minutes, so a change to the limit
// cannot leave the note saying another number. Positive twin first: the note names the
// limit; a note naming another number would not.
func TestTheHourlyNoteNamesTheWaitLimit(t *testing.T) {
	want := fmt.Sprintf("up to %d minutes", int(defMaxWait.Minutes()))
	if !strings.Contains(HourlySyncNote, want) {
		t.Fatalf("HourlySyncNote %q does not say %q (defMaxWait %v)", HourlySyncNote, want, defMaxWait)
	}
	if other := strings.Replace(HourlySyncNote, want, "up to 30 minutes", 1); strings.Contains(other, want) {
		t.Error("twin: a note naming another number still matched")
	}
}
