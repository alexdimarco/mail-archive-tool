package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
)

// covers: MA-803, R37
// The standing red and the last-checked time (the P3-2b pass, REC-01, REC-06) stay
// within the allow-list: written with times, status, outcome and tier only, read
// back as written; a standing verdict that is not a finished red found with the key,
// or either field on a plaintext record, reads as unreadable. StandingRed finds the
// red at the top or beneath, and CheckedAt is the later of the two times.
func TestTheStandingRedStaysMinimised(t *testing.T) {
	out := t.TempDir()
	at := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	later := at.Add(48 * time.Hour)
	red := LastVerify{Status: VerifyDone, Started: at, Finished: &at, Encrypted: true, Tier: TierFull, Outcome: OutcomeDamaged, Missing: 4}
	top := LastVerify{Status: VerifyDone, Started: later, Finished: &later, Encrypted: true, Tier: TierKeyless, Outcome: OutcomeUnfinishedSave, Standing: &red}
	if err := WriteLastVerify(out, top); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(out, LastVerifyName))
	v, st, err := ReadLastVerify(out)
	if err != nil || st != LastVerifyPresent || v.Standing == nil || v.Standing.Outcome != OutcomeDamaged || strings.Contains(string(b), "missing") {
		t.Fatalf("written minimised and read back: %v %v\n%s", st, err, b)
	}
	if s := StandingRed(&v); s == nil || s.Outcome != OutcomeDamaged || StandingRed(&red) == nil || StandingRed(&top) == nil {
		t.Fatal("StandingRed finds the red beneath and at the top")
	}
	kept := red
	kept.LastChecked = &later
	if !kept.CheckedAt().Equal(later) || !red.CheckedAt().Equal(at) {
		t.Fatal("CheckedAt is the later time")
	}

	for name, body := range map[string]string{
		"a standing amber":          `{"version":1,"status":"done","started":"2026-10-25T00:00:00Z","finished":"2026-10-25T00:00:00Z","outcome":"intact","encrypted":true,"standing":{"version":1,"status":"done","started":"2026-10-25T00:00:00Z","finished":"2026-10-25T00:00:00Z","outcome":"not_attested","encrypted":true,"tier":2}}`,
		"a standing keyless red":    `{"version":1,"status":"done","started":"2026-10-25T00:00:00Z","finished":"2026-10-25T00:00:00Z","outcome":"intact","encrypted":true,"standing":{"version":1,"status":"done","started":"2026-10-25T00:00:00Z","finished":"2026-10-25T00:00:00Z","outcome":"damaged","encrypted":true,"tier":1}}`,
		"standing on plaintext":     `{"version":1,"status":"done","started":"2026-10-25T00:00:00Z","finished":"2026-10-25T00:00:00Z","attested":true,"standing":{"version":1,"status":"done","started":"2026-10-25T00:00:00Z","finished":"2026-10-25T00:00:00Z","outcome":"damaged","encrypted":true,"tier":2}}`,
		"last_checked on plaintext": `{"version":1,"status":"done","started":"2026-10-25T00:00:00Z","finished":"2026-10-25T00:00:00Z","attested":true,"last_checked":"2026-10-26T00:00:00Z"}`,
	} {
		os.WriteFile(filepath.Join(out, LastVerifyName), []byte(body), 0o600)
		if _, st, err := ReadLastVerify(out); st != LastVerifyUnreadable || err == nil {
			t.Fatalf("%s: %v %v", name, st, err)
		}
	}
	assure.Reached(t, true, "every planted shape refused")
}
