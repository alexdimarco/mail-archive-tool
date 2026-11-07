package tbengine

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/state"
)

// covers: MA-1124, R40, S70
// R7-03 (settled 2026-11-07, the lead's call): an encrypted archive's folder is the party
// its storage is kept from, and its plaintext schedule descriptor and last-run record are
// files anyone who can write that folder can plant (an encrypted archive is never
// scheduled, O5, and its record keeps no inputs). So for an encrypted archive they are not
// read when deciding W-RECAPTURE and the default profile: only this computer's own
// sources (Desktop job files). A descriptor naming another input no longer hides the
// "cannot tell" line, and a planted -auto no longer warns of recapture for every profile.
// Positive twin: the same two files planted in a plain archive's folder are read, as
// R6-02 settled (its own sources).
func TestPlantedSourcesInAnEncryptedArchiveAreNotRead(t *testing.T) {
	pr := newProfile(t, "")
	plant := func(out string, desc []string, auto bool) {
		t.Helper()
		if desc != nil {
			if err := schedule.WriteDescriptor(out, schedule.Descriptor{Version: 1, Name: "nightly", Interval: "daily", At: "02:00", Job: desc}); err != nil {
				t.Fatal(err)
			}
		}
		if auto {
			if err := state.WriteLastRun(out, state.LastRun{Status: "ok", Started: time.Now(), Finished: time.Now(), Job: []string{"-out", out, "-auto"}}); err != nil {
				t.Fatal(err)
			}
		}
	}
	elsewhere := t.TempDir()

	plain := plainArchive(t, true)
	plant(plain, []string{"-out", plain, "-input", elsewhere}, false)
	pp := plan(t, Request{Out: plain, Profile: pr.choice()})
	assure.Reached(t, !strings.Contains(codesOf(pp), "W-RECAPTURE"), fmt.Sprintf("twin: a plain archive's descriptor is read (another input: no recapture line): %s", codesOf(pp)))
	plant(plain, nil, true)
	pa := plan(t, Request{Out: plain, Profile: pr.choice()})
	assure.Reached(t, pa.Codes()[0] == "W-RECAPTURE" && pa.Lines[0].Words == wRecapture, fmt.Sprintf("twin: a plain archive's last run is read (-auto: W-RECAPTURE): %s", codesOf(pa)))

	enc, u := encryptedArchive(t)
	plant(enc, []string{"-out", enc, "-input", elsewhere}, false)
	pe := plan(t, Request{Out: enc, Keys: u, Profile: pr.choice()})
	assure.Reached(t, pe.Codes()[0] == "W-RECAPTURE" && pe.Lines[0].Words == wRecaptureUnknown, fmt.Sprintf("a descriptor planted in an encrypted archive is not read: it still cannot tell: %s", codesOf(pe)))
	plant(enc, nil, true)
	pe2 := plan(t, Request{Out: enc, Keys: u, Profile: pr.choice()})
	assure.Reached(t, pe2.Codes()[0] == "W-RECAPTURE" && pe2.Lines[0].Words == wRecaptureUnknown, fmt.Sprintf("a last run planted in an encrypted archive is not read: no recapture warning, it cannot tell: %s", codesOf(pe2)))
	misread := ArchiveSources(enc, false) // a caller that read the folder as a plain archive's
	pe3 := plan(t, Request{Out: enc, Keys: u, Profile: pr.choice(), Sources: &misread})
	assure.Reached(t, misread.Auto && pe3.Lines[0].Words == wRecaptureUnknown, fmt.Sprintf("the plan reads an encrypted archive's sources itself, whatever the caller passed: %s", codesOf(pe3)))
	s := ArchiveSources(enc, true)
	assure.Reached(t, !s.Known && !s.Auto && len(s.Paths) == 0, fmt.Sprintf("ArchiveSources of an encrypted archive reads neither planted file: %+v", s))
	sp := ArchiveSources(plain, false)
	assure.Reached(t, sp.Known && sp.Auto, fmt.Sprintf("twin: ArchiveSources of a plain archive reads them: %+v", sp))
}
