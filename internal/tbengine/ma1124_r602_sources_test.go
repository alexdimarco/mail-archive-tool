package tbengine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/schedule"
)

// covers: MA-1124, R40, R41
// R6-02 (settled 2026-11-07, the lead's call): "this archive backs up this profile"
// (W-RECAPTURE) and the default profile come from what the archive itself records as its
// sources — its last run's inputs — and the schedule installed for it, as well as Desktop
// job files. Positive twin: an archive backed up from elsewhere has neither W-RECAPTURE
// nor the "cannot tell" line. Then, with no job file anywhere: an archive whose last
// backup read the profile's Local Folders warns, and so its default profile is another;
// one whose installed schedule names the profile warns; one backed up with -auto warns; an
// encrypted archive, whose record keeps no inputs, says it cannot tell.
func TestTheArchivesOwnRecordSaysItBacksUpTheProfile(t *testing.T) {
	elsewhere := plainArchive(t, true)
	pr := newProfile(t, "")
	twin := plan(t, Request{Out: elsewhere, Profile: pr.choice()})
	assure.Reached(t, !strings.Contains(codesOf(twin), "W-RECAPTURE"), fmt.Sprintf("an archive of another folder: no W-RECAPTURE: %s", codesOf(twin)))

	write(t, filepath.Join(pr.local, "Kept"), mboxOf("person mail one"))
	own := filepath.Join(t.TempDir(), "Profile backup")
	backup(t, pr.local, own, true, nil) // at the command line: no job file
	p := plan(t, Request{Out: own, Profile: pr.choice()})
	assure.Reached(t, p.Codes()[0] == "W-RECAPTURE" && p.Lines[0].Words == wRecapture, fmt.Sprintf("the archive's last run read this profile: W-RECAPTURE first: %s", codesOf(p)))

	base := t.TempDir()
	other := newProfile(t, "")
	profilesINI(t, base, []string{"Main", "Other"}, []string{pr.dir, other.dir})
	def := plan(t, Request{Out: own, Profile: ProfileChoice{Bases: []string{base}}})
	assure.Reached(t, def.Profile.Dir == other.dir, fmt.Sprintf("the default is the profile it does not back up: %s", def.Profile.Dir))

	sched := plainArchive(t, true)
	if err := schedule.WriteDescriptor(sched, schedule.Descriptor{Version: 1, Name: "nightly", Interval: "daily", At: "02:00",
		Job: []string{"-out", sched, "-mode", "incremental", "-input", pr.dir}}); err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(sched, ".mailarchive-lastrun.json")) // only the schedule says it
	ps := plan(t, Request{Out: sched, Profile: pr.choice()})
	assure.Reached(t, ps.Codes()[0] == "W-RECAPTURE" && ps.Lines[0].Words == wRecapture, fmt.Sprintf("an installed schedule's input: W-RECAPTURE: %s", codesOf(ps)))

	pa := plan(t, Request{Out: elsewhere, Profile: pr.choice(), Sources: &Sources{Auto: true, Known: true}})
	assure.Reached(t, pa.Codes()[0] == "W-RECAPTURE", fmt.Sprintf("a backup that finds its sources itself (-auto) backs up every profile: %s", codesOf(pa)))
	var s Sources
	s.add([]string{"-out", elsewhere, "-mode", "incremental", "-auto"})
	assure.Reached(t, s.Auto && s.Known, "a recorded -auto is read as such")

	enc, u := encryptedArchive(t)
	pe := plan(t, Request{Out: enc, Keys: u, Profile: pr.choice()})
	assure.Reached(t, pe.Codes()[0] == "W-RECAPTURE" && pe.Lines[0].Words == wRecaptureUnknown, fmt.Sprintf("an encrypted archive, whose record keeps no inputs: it cannot tell, and says so: %s", codesOf(pe)))
}
