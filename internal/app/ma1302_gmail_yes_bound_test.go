package app

import (
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/gmailanswer"
	"mail-archive-tool/internal/state"
)

// covers: MA-1302, R45
// A yes kept on this computer switches only the archive it was given for (the B3 adversarial
// pass's A1, turned from its probe TestB3AdvAYesKeptForOneArchiveSwitchesAnother). The yes names
// the archive by the id its last-run record carries; the run applies it only when the archive it
// holds the lock of carries that id, and the yes is spent once a backup has switched its stores on.
// Positive twin first: the yes given on A's card switches A at A's next scheduled run, and is then
// gone. Then A is removed and another existing archive B, of another Gmail profile, is put in the
// same folder: the yes this computer keeps for the folder is not read for B (Read), and even
// handed to the run as it was given for A (a caller that read it before the folder changed), B
// stays version 6, nothing switched on, and its question waits.
func TestAYesKeptForOneArchiveNeverSwitchesAnother(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))

	// Positive twin: A's yes switches A, and is spent.
	storeA, outA := existingGmailArchive(t)
	res := runTB(t, Options{Inputs: []string{storeA}, Out: outA, Unattended: true})
	if !res.Gmail.Waiting() {
		t.Fatalf("A's question does not wait: %+v", res.Gmail)
	}
	idA := state.ArchiveID(outA)
	if idA == "" {
		t.Fatal("A's run gave the archive no id")
	}
	if err := gmailanswer.Keep(outA, gmailanswer.Answer{Archive: idA, Yes: res.Gmail.Off}); err != nil {
		t.Fatal(err)
	}
	kept := gmailanswer.Read(outA)
	runTB(t, Options{Inputs: []string{storeA}, Out: outA, Unattended: true, GmailYesStores: kept.Yes, GmailYesFor: kept.Archive})
	if v, on := manifestOnDisk(t, outA); v != 7 || len(on) == 0 {
		t.Fatalf("positive twin: A's own yes did not switch A (version %d, on %v)", v, on)
	}
	if state.ArchiveID(outA) != idA {
		t.Fatal("A's id changed across its own runs")
	}
	if err := gmailanswer.Settle(outA); err != nil {
		t.Fatal(err)
	}
	if p, _ := gmailanswer.Path(outA); fileThere(p) || len(gmailanswer.Read(outA).Yes) != 0 {
		t.Fatal("the yes A's backup applied is still kept on this computer")
	}

	// The same folder, then another archive: A's yes waits again (as a second card answer)...
	storeA2, outA2 := existingGmailArchive(t)
	res = runTB(t, Options{Inputs: []string{storeA2}, Out: outA2, Unattended: true})
	idA2 := state.ArchiveID(outA2)
	if err := gmailanswer.Keep(outA2, gmailanswer.Answer{Archive: idA2, Yes: res.Gmail.Off}); err != nil {
		t.Fatal(err)
	}
	stale := gmailanswer.Read(outA2) // what a caller read before the folder changed
	// ...and A2 is removed and B, another existing archive, is put in its folder.
	storeB, outB := existingGmailArchive(t)
	if err := os.RemoveAll(outA2); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(outB, outA2); err != nil {
		t.Fatal(err)
	}
	if k := gmailanswer.Read(outA2); len(k.Yes) != 0 || k.NotNow {
		t.Fatalf("the yes given for A2 is read for B in the same folder: %+v", k)
	}
	res = runTB(t, Options{Inputs: []string{storeB}, Out: outA2, Unattended: true, GmailYesStores: stale.Yes, GmailYesFor: stale.Archive})
	if v, on := manifestOnDisk(t, outA2); v != 6 || len(on) != 0 {
		t.Fatalf("archive B was switched to the 1.8-only format (version %d, on %v) by the yes given for A2", v, on)
	}
	if !res.Gmail.Waiting() {
		t.Fatalf("B's own question does not wait: %+v", res.Gmail)
	}
}

func fileThere(p string) bool {
	_, err := os.Lstat(p)
	return p != "" && err == nil
}
