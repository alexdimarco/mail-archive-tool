package presync

import (
	"testing"
	"time"
)

// covers: MA-554
// The "ours" ownership predicate (design 4.4.3): a candidate is ours only if its
// executable equals the one we launched, its command line carries
// -profile <canonical profile>, its pid was not in the pre-launch snapshot, and
// its start time is at or after launchAt-2s. A candidate with no start time is
// never ours (identity-unavailable); a pre-launch start is the already-running
// race; and ambiguity (two matches) yields no match, so nothing is ever quit on
// a guess.
func TestPresyncOwnership(t *testing.T) {
	launch := time.Date(2026, 10, 22, 2, 0, 0, 0, time.UTC)
	lc := launchCtx{
		app:      "/Applications/Thunderbird.app/Contents/MacOS/thunderbird",
		profile:  "/Users/x/profile.default",
		launchAt: launch,
		snapshot: map[int]bool{100: true}, // pid 100 was already running at S4
	}
	ours := proc{pid: 200, exe: lc.app, start: launch.Add(1 * time.Second), args: "thunderbird -profile /Users/x/profile.default"}

	t.Run("exact single match is ours", func(t *testing.T) {
		if !candidateIsOurs(ours, lc) {
			t.Errorf("the launched process should be ours")
		}
		if m, ok := findOurs([]proc{ours}, lc); !ok || m.pid != 200 {
			t.Errorf("findOurs should return the single match")
		}
	})
	t.Run("different executable is not ours", func(t *testing.T) {
		c := ours
		c.exe = "/opt/thunderbird/thunderbird"
		if candidateIsOurs(c, lc) {
			t.Errorf("a different executable must not match")
		}
	})
	t.Run("missing or wrong -profile is not ours", func(t *testing.T) {
		c := ours
		c.args = "thunderbird" // no -profile
		if candidateIsOurs(c, lc) {
			t.Errorf("no -profile argument must not match")
		}
		c.args = "thunderbird -profile /Users/x/other"
		if candidateIsOurs(c, lc) {
			t.Errorf("a different profile must not match")
		}
	})
	t.Run("a pid from the pre-launch snapshot is not ours", func(t *testing.T) {
		c := ours
		c.pid = 100 // in the S4 snapshot
		if candidateIsOurs(c, lc) {
			t.Errorf("a process already running before the launch must not match")
		}
	})
	t.Run("a start before launchAt-2s is the already-running race", func(t *testing.T) {
		c := ours
		c.start = launch.Add(-3 * time.Second)
		if candidateIsOurs(c, lc) {
			t.Errorf("a pre-launch start must not match")
		}
		c.start = launch.Add(-2 * time.Second) // the boundary is allowed
		if !candidateIsOurs(c, lc) {
			t.Errorf("the -2s boundary should match")
		}
	})
	t.Run("no start time is never ours (identity-unavailable)", func(t *testing.T) {
		c := ours
		c.start = time.Time{}
		if candidateIsOurs(c, lc) {
			t.Errorf("a candidate with no start time must never be ours")
		}
	})
	t.Run("two matching candidates yield no match", func(t *testing.T) {
		twin := ours
		twin.pid = 201
		if _, ok := findOurs([]proc{ours, twin}, lc); ok {
			t.Errorf("ambiguity (two matches) must yield no match, so nothing is quit")
		}
	})
}
