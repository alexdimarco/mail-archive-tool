package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/container"
)

// covers: MA-845, R37
// The note names only copies on the drive (NOTE-03), and gives the run steps each kept
// kind needs and no other (NOTE-04): a Mac app to unzip and open, the Windows zip to
// unzip, a bare program to make runnable — a Linux release copy included.
func TestTheNoteNamesWhatIsThereAndHowToRunIt(t *testing.T) {
	keep := true
	set := func(files ...container.ProgramFile) string {
		return programSection(&container.ProgramSet{Format: container.ProgramFormat, Keep: &keep, Files: files})
	}
	mac := container.ProgramFile{Name: "MailArchive-macos.zip", Origin: container.OriginRelease, Version: "v1.5.0", Platform: "macos"}
	lx := container.ProgramFile{Name: "mailarchive-linux-amd64", Origin: container.OriginRelease, Version: "v1.5.0", Platform: "linux-amd64"}
	missing := lx
	missing.MissingRuns = 1
	onlyMac := set(mac)
	withLinux := set(mac, lx)
	gone := set(mac, missing)
	assure.Reached(t, strings.Contains(onlyMac, "unzip it and open the app") && !strings.Contains(onlyMac, "chmod +x") &&
		strings.Contains(withLinux, "chmod +x") && !strings.Contains(gone, "Linux (x86-64)") && !strings.Contains(gone, "chmod +x"),
		"what is there, and its steps")
}

// covers: MA-845, R37
// The note's version is a release's or "a development build" — never a pseudo-version
// that reads like a release (NOTE-07).
func TestTheNotesVersionIsAReleaseOrADevelopmentBuild(t *testing.T) {
	oi := programIdentity
	defer func() { programIdentity = oi }()
	programIdentity = func() (string, bool) { return "v1.5.1-0.20261027143848-6e51f709870d+dirty", false }
	dev := EncryptedNote()
	programIdentity = func() (string, bool) { return "v1.5.0", true }
	rel := EncryptedNote()
	assure.Reached(t, strings.Contains(dev, "written by MailArchive (a development build)") && !strings.Contains(dev, "v1.5.1") &&
		strings.Contains(rel, "written by MailArchive v1.5.0"), "the version line")
}

// covers: MA-845, R37
// A set a newer MailArchive keeps leaves its note as it is; a note changed meanwhile is
// replaced by one that says copies may be kept, with the offline warning (NOTE-06).
func TestANewerProgramsNoteIsLeftAsItIs(t *testing.T) {
	asDevBuild(t)
	isolatedCache(t)
	out, key := encryptedOut(t)
	backup(t, out, key, ProgramCopiesOn)
	m, s, err := container.LoadManifest(out, container.LoadOptions{Key: key})
	if err != nil {
		t.Fatal(err)
	}
	s.SetProgramRawForTest([]byte(`{"format":2,"keep":true}`))
	m.Save()
	before, _ := os.ReadFile(filepath.Join(out, "README.txt"))
	backup(t, out, key, ProgramCopiesUnset)
	after, _ := os.ReadFile(filepath.Join(out, "README.txt"))
	if string(after) != string(before) {
		t.Fatal("a newer program's note was rewritten")
	}
	os.WriteFile(filepath.Join(out, "README.txt"), []byte("changed"), 0o644)
	backup(t, out, key, ProgramCopiesUnset)
	after, _ = os.ReadFile(filepath.Join(out, "README.txt"))
	assure.Reached(t, strings.Contains(string(after), "by\na newer MailArchive") && strings.Contains(string(after), "cannot be checked"), "replaced with the unread section")
}
