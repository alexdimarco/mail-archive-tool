package app

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/gmailanswer"
	"mail-archive-tool/internal/state"
)

// ma1316Settings gives the test its own settings folder, where the kept Gmail answer lives.
func ma1316Settings(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
}

// ma1316OtherArchive is a second existing archive of the same Thunderbird store (the second drive
// of a rotation): version 6, nothing switched on, its own id.
func ma1316OtherArchive(t *testing.T, store string) string {
	t.Helper()
	out := tmpDir(t)
	gmailDetectOff = true
	runTB(t, Options{Inputs: []string{store}, Out: out})
	gmailDetectOff = false
	return out
}

// ma1316SetVersion rewrites out's manifest's version stamp (8: another computer tidying it).
func ma1316SetVersion(t *testing.T, out string, v int) {
	t.Helper()
	p := filepath.Join(out, ".mailarchive-manifest.json")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	m["version"] = v
	if b, err = json.Marshal(m); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// ma1316YesFor is archive A, an existing Gmail archive whose question waits, with the card's yes
// kept for it; and the options of A's next scheduled run, read as optsForJob reads them.
func ma1316YesFor(t *testing.T) (storeA, outA, idA string, opts Options) {
	t.Helper()
	storeA, outA = existingGmailArchive(t)
	res := runTB(t, Options{Inputs: []string{storeA}, Out: outA, Unattended: true})
	idA = state.ArchiveID(outA)
	if !res.Gmail.Waiting() || idA == "" {
		t.Fatalf("A's question does not wait: %+v (id %q)", res.Gmail, idA)
	}
	if err := gmailanswer.Keep(outA, gmailanswer.Answer{Archive: idA, Yes: res.Gmail.Off}); err != nil {
		t.Fatal(err)
	}
	kept := gmailanswer.Read(outA)
	assure.Reached(t, kept.Yes, "the yes kept for A")
	opts = Options{Inputs: []string{storeA}, Out: outA, Unattended: true, Mode: export.Incremental,
		GmailYesStores: kept.Yes, GmailYesFor: kept.Archive, GmailNotNow: kept.NotNow}
	return storeA, outA, idA, opts
}

// ma1316Record is the archive's last-run record's bytes.
func ma1316Record(t *testing.T, out string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(out, state.LastRunName))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// covers: MA-1316, R45, R5
// The B3 adversarial pass, round 2's G1: the folder's archive is replaced while the run waits in
// its Thunderbird pre-sync (a drive of a rotation swapped at the same path, here a folder renamed).
// The run checks again, once the manifest has loaded and under its lock, that the folder still
// holds the archive it began with: it does not, so the run is refused with words that say so, the
// yes kept for A is not applied to B, and nothing is written into B (its record keeps B's own id).
// Positive twin first: without the swap the same run switches A.
func TestAYesIsNotAppliedWhenTheFolderChangesDuringThePresync(t *testing.T) {
	ma1316Settings(t)
	_, outA, _, twin := ma1316YesFor(t)
	twin.BeforeRead = func(context.Context, []string) BeforeReadResult { return BeforeReadResult{} }
	if _, err := Run(context.Background(), twin, discard(), nil); err != nil {
		t.Fatalf("positive twin: %v", err)
	}
	if v, on := manifestOnDisk(t, outA); v != 7 || len(on) == 0 {
		t.Fatalf("positive twin: the yes was not applied to A (version %d, on %v)", v, on)
	}

	if runtime.GOOS == "windows" {
		t.Skip("Windows will not rename a folder whose lock file is open: the drive-letter form is TestAYesIsNotAppliedWhenTheDriveLetterChangesDuringThePresync")
	}
	storeA, outA, idA, opts := ma1316YesFor(t)
	outB := ma1316OtherArchive(t, storeA)
	idB := state.ArchiveID(outB)
	if idB == "" || idB == idA {
		t.Fatalf("B's id %q (A's %q)", idB, idA)
	}
	recB := ma1316Record(t, outB)
	aside := outA + "-drive1"
	opts.BeforeRead = func(context.Context, []string) BeforeReadResult {
		// While the pre-sync waits for Thunderbird: drive 1 out, drive 2 in, at the same path.
		if err := os.Rename(outA, aside); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(outB, outA); err != nil {
			t.Fatal(err)
		}
		return BeforeReadResult{}
	}
	_, err := Run(context.Background(), opts, discard(), nil)
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	rc := 0
	if FailureKind(err) == state.FailLockLost {
		rc = 2
	}
	assure.Refused(t, rc, msg, assure.Names(outA, "another archive", "nothing was written", "Run the backup again"),
		assure.NoSideEffect(func() bool {
			v, on := manifestOnDisk(t, outA)
			return v == 6 && len(on) == 0 && ma1316Record(t, outA) == recB && state.ArchiveID(outA) == idB
		}))
	if va, ona := manifestOnDisk(t, aside); va != 6 || len(ona) != 0 {
		t.Errorf("A, moved aside, was switched by a run that no longer held it (version %d, on %v)", va, ona)
	}
}

// covers: MA-1316, R45, R5
// G1's Windows form: two drives used in turn at one letter. subst stands in for the drive letter:
// Q: names drive 1's folder, and while A's run waits in its pre-sync, Q: is made to name drive 2's
// folder instead (the lock file's handle stays open on drive 1, as when a USB drive is pulled and
// another takes its letter). The run is refused and drive 2's archive is left as it was. Positive
// twin: the TestAYesIsNotAppliedWhenTheFolderChangesDuringThePresync twin, which runs everywhere.
func TestAYesIsNotAppliedWhenTheDriveLetterChangesDuringThePresync(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows only (subst names a drive letter)")
	}
	const letter = "Q:"
	if _, err := os.Stat(letter + `\`); err == nil {
		t.Skip(letter + " is in use on this computer")
	}
	ma1316Settings(t)
	drive1, drive2 := t.TempDir(), t.TempDir()
	subst := func(args ...string) {
		if out, err := exec.Command("subst", args...).CombinedOutput(); err != nil {
			t.Fatalf("subst %v: %v %s", args, err, out)
		}
	}
	subst(letter, drive1)
	t.Cleanup(func() { _ = exec.Command("subst", letter, "/D").Run() })
	out := letter + `\MailArchive`
	_, store := imapProfile(t, "imap.gmail.com", map[string]string{"is_gmail": "true"}, gmailFolders(6)...)
	gmailDetectOff = true
	runTB(t, Options{Inputs: []string{store}, Out: out})
	gmailDetectOff = false
	res := runTB(t, Options{Inputs: []string{store}, Out: out, Unattended: true})
	idA := state.ArchiveID(out)
	if !res.Gmail.Waiting() || idA == "" {
		t.Fatalf("A's question does not wait: %+v (id %q)", res.Gmail, idA)
	}
	if err := gmailanswer.Keep(out, gmailanswer.Answer{Archive: idA, Yes: res.Gmail.Off}); err != nil {
		t.Fatal(err)
	}
	outB := filepath.Join(drive2, "MailArchive")
	gmailDetectOff = true
	runTB(t, Options{Inputs: []string{store}, Out: outB})
	gmailDetectOff = false
	idB := state.ArchiveID(outB)
	recB := ma1316Record(t, outB)
	kept := gmailanswer.Read(out)
	opts := Options{Inputs: []string{store}, Out: out, Unattended: true, Mode: export.Incremental,
		GmailYesStores: kept.Yes, GmailYesFor: kept.Archive, GmailNotNow: kept.NotNow}
	opts.BeforeRead = func(context.Context, []string) BeforeReadResult {
		subst(letter, "/D")
		subst(letter, drive2)
		return BeforeReadResult{}
	}
	_, err := Run(context.Background(), opts, discard(), nil)
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	rc := 0
	if FailureKind(err) == state.FailLockLost {
		rc = 2
	}
	assure.Refused(t, rc, msg, assure.Names("another archive", "Run the backup again"),
		assure.NoSideEffect(func() bool {
			v, on := manifestOnDisk(t, outB)
			return v == 6 && len(on) == 0 && ma1316Record(t, outB) == recB && state.ArchiveID(outB) == idB
		}))
}

// covers: MA-1317, R45, R5
// G1 on the version-8 skip path: the archive swapped in while the pre-sync waits is at version 8
// (another computer tidying it). The skip's put-back writes the record it kept before the run
// only while the folder still holds the archive it began with: here it does not, so B's record is
// left exactly as it was (B's own id), and B's next run never reads A's yes as its own. Positive
// twin first: without the swap, the skip puts A's own record back as it was.
func TestATidyUpSkipNeverPutsOneArchivesRecordIntoAnother(t *testing.T) {
	ma1316Settings(t)
	storeA, outA, idA, opts := ma1316YesFor(t)
	ma1316SetVersion(t, outA, 8)
	recA := ma1316Record(t, outA)
	twin := opts
	twin.GmailYesStores, twin.GmailYesFor = nil, ""
	if _, err := Run(context.Background(), twin, discard(), nil); !TidyingElsewhere(err) || ma1316Record(t, outA) != recA {
		t.Fatalf("positive twin: the skip did not put A's record back as it was (%v)", err)
	}
	ma1316SetVersion(t, outA, 6)

	outB := ma1316OtherArchive(t, storeA)
	idB := state.ArchiveID(outB)
	ma1316SetVersion(t, outB, 8)
	recB := ma1316Record(t, outB)
	aside := outA + "-drive1"
	opts.BeforeRead = func(context.Context, []string) BeforeReadResult {
		if err := os.Rename(outA, aside); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(outB, outA); err != nil {
			t.Fatal(err)
		}
		return BeforeReadResult{}
	}
	if runtime.GOOS == "windows" {
		t.Skip("Windows will not rename a folder whose lock file is open; the put-back's check is the same one TestAYesIsNotAppliedWhenTheDriveLetterChangesDuringThePresync proves there")
	}
	_, err := Run(context.Background(), opts, discard(), nil)
	if !TidyingElsewhere(err) {
		t.Fatalf("the run met B's tidy-up: %v", err)
	}
	if got := state.ArchiveID(outA); got != idB || ma1316Record(t, outA) != recB {
		t.Fatalf("the skip wrote into B's folder: its record's id is %q (A %q, B %q)", got, idA, idB)
	}
	// The other computer finishes B's tidy-up (the stamp back to what this build reads): what this
	// computer reads for the folder now is not A's yes, and B's next run leaves it at version 6.
	ma1316SetVersion(t, outA, 6)
	kept := gmailanswer.Read(outA)
	if len(kept.Yes) != 0 {
		t.Fatalf("A's yes is read for B: %+v", kept)
	}
	runTB(t, Options{Inputs: []string{storeA}, Out: outA, Unattended: true, GmailYesStores: kept.Yes, GmailYesFor: kept.Archive, GmailNotNow: kept.NotNow})
	if v, on := manifestOnDisk(t, outA); v != 6 || len(on) != 0 {
		t.Fatalf("B was switched (version %d, on %v) by the yes given for A", v, on)
	}
}
