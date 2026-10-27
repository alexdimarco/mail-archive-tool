package app

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/state"
)

// covers: MA-848, R37
// verify -full while an update's intent stands takes either side of it as the program's
// own (R5, R7; the PG4 pass, DUR-2): the note written for the update, a copy already
// replaced by its incoming version and a copy whose removal was under way, gone, tell
// nothing but the unfinished update. A file no entry names is told, by name, whether
// copies are kept or not (COR-3); with copies switched off, the copies still sealed are
// still compared.
func TestVerifyFullTakesAnUnfinishedUpdateAsItsOwn(t *testing.T) {
	out := sealedArchive(t)
	dir := filepath.Join(out, container.ProgramDir)
	os.MkdirAll(dir, 0o755)
	entry := func(name, plat, version string, body []byte) container.ProgramFile {
		s := sha256.Sum256(body)
		return container.ProgramFile{Name: name, SHA256: hex.EncodeToString(s[:]), Size: int64(len(body)), Origin: container.OriginRelease, Version: version, Platform: plat, Reads: 1}
	}
	oldNote, newNote := []byte("the note for v1.5.0"), []byte("the note for v1.6.0")
	os.WriteFile(filepath.Join(out, "README.txt"), newNote, 0o644)
	os.WriteFile(filepath.Join(dir, "MailArchive-macos.zip"), []byte("mac v1.6.0"), 0o644) // already replaced
	keep := true
	set := &container.ProgramSet{Format: container.ProgramFormat, Keep: &keep, State: container.ProgramIntent, Version: "v1.5.0",
		Files: []container.ProgramFile{entry("MailArchive-macos.zip", "macos", "v1.5.0", []byte("mac v1.5.0")),
			entry("SHA256SUMS-v1.5.0", container.PlatformChecksums, "v1.5.0", []byte("sums"))},
		Incoming: []container.ProgramFile{entry("MailArchive-macos.zip", "macos", "v1.6.0", []byte("mac v1.6.0"))},
		Removing: []string{"SHA256SUMS-v1.5.0"}, // already removed
		Note:     &container.ProgramNote{Current: NoteDigest(oldNote), Incoming: NoteDigest(newNote)}}
	verify := func() []string {
		rep, err := VerifySealed(out, VerifyOptions{}, discard(), nil, &fakeSealed{m: oneRecord(t), listOK: true, program: set, note: NoteDigest(oldNote)})
		if err != nil {
			t.Fatal(err)
		}
		return rep.ProgramFindings
	}
	during := verify()
	if len(during) != 1 || !strings.Contains(during[0], "did not finish") {
		t.Fatalf("an unfinished update is the program's own: %v", during)
	}
	os.WriteFile(filepath.Join(dir, "mailarchive-windows-amd64.zip"), []byte("planted"), 0o644)
	planted := strings.Join(verify(), " | ")
	off := false
	os.WriteFile(filepath.Join(dir, "MailArchive-macos.zip"), []byte("swapped"), 0o644)
	set = &container.ProgramSet{Format: container.ProgramFormat, Keep: &off, State: container.ProgramLanded, Version: "v1.6.0",
		Files: []container.ProgramFile{entry("MailArchive-macos.zip", "macos", "v1.6.0", []byte("mac v1.6.0"))}}
	os.WriteFile(filepath.Join(out, "README.txt"), oldNote, 0o644)
	switchedOff := strings.Join(verify(), " | ")
	assure.Reached(t, strings.Contains(planted, "mailarchive-windows-amd64.zip, which MailArchive did not put there") &&
		strings.Contains(switchedOff, "Mac was changed") && strings.Contains(switchedOff, "did not put there"),
		"planted: "+planted+"\nswitched off: "+switchedOff)
}

// covers: MA-847, R37
// verify -full compares the program folder and the note with what the record seals
// (program design §6, C18), read-only: a changed copy, a missing one, something in a
// copy's place and a note not the last backup's are each told as a warning, and the
// verdict on the mail is unchanged. The positive twin: a folder as sealed tells nothing.
func TestVerifyFullComparesTheProgramFolder(t *testing.T) {
	out := sealedArchive(t)
	dir := filepath.Join(out, container.ProgramDir)
	os.MkdirAll(dir, 0o755)
	file := func(name, plat string, body []byte) container.ProgramFile {
		os.WriteFile(filepath.Join(dir, name), body, 0o644)
		s := sha256.Sum256(body)
		return container.ProgramFile{Name: name, SHA256: hex.EncodeToString(s[:]), Size: int64(len(body)), Origin: container.OriginRelease, Version: "v1.5.0", Platform: plat, Reads: 1}
	}
	note := []byte("the note")
	os.WriteFile(filepath.Join(out, "README.txt"), note, 0o644)
	keep := true
	set := &container.ProgramSet{Format: container.ProgramFormat, Keep: &keep, State: container.ProgramLanded, Version: "v1.5.0", Files: []container.ProgramFile{
		file("MailArchive-macos.zip", "macos", []byte("mac")), file("mailarchive-linux-amd64", "linux-amd64", []byte("linux")),
		file("mailarchive-linux-arm64", "linux-arm64", []byte("arm")),
	}}
	verify := func() Report {
		rep, err := VerifySealed(out, VerifyOptions{}, discard(), nil, &fakeSealed{m: oneRecord(t), listOK: true, program: set, note: NoteDigest(note)})
		if err != nil {
			t.Fatal(err)
		}
		return rep
	}
	clean := verify()
	if len(clean.ProgramFindings) != 0 {
		t.Fatalf("positive twin: %v", clean.ProgramFindings)
	}
	os.WriteFile(filepath.Join(dir, "MailArchive-macos.zip"), []byte("cam"), 0o644)
	os.Remove(filepath.Join(dir, "mailarchive-linux-amd64"))
	os.Remove(filepath.Join(dir, "mailarchive-linux-arm64"))
	os.Mkdir(filepath.Join(dir, "mailarchive-linux-arm64"), 0o755)
	os.WriteFile(filepath.Join(out, "README.txt"), []byte("go to evil.example"), 0o644)
	after := snapshotTree(t, dir)
	rep := verify()
	all := strings.Join(rep.ProgramFindings, " | ")
	summary := strings.Join(VerifySummary(rep), "\n")
	untouched := sameTree(after, snapshotTree(t, dir))
	// An update left standing, and a program folder that is a link, not a plain folder.
	set.State = container.ProgramIntent
	intent := strings.Join(verify().ProgramFindings, " | ")
	os.Rename(dir, dir+"-elsewhere")
	os.Symlink(dir+"-elsewhere", dir)
	linked := strings.Join(verify().ProgramFindings, " | ")
	assure.Reached(t, strings.Contains(all, "Mac was changed") && strings.Contains(all, "Linux (x86-64) is missing") &&
		strings.Contains(all, "stands where the copy for Linux (ARM)") && strings.Contains(all, "README.txt") &&
		rep.Outcome == clean.Outcome && rep.Outcome != state.OutcomeDamaged && untouched &&
		strings.Contains(summary, "copies of MailArchive") && strings.Contains(intent, "did not finish") &&
		strings.Contains(linked, "not a plain folder"), "told, unchanged:\n"+summary+"\n"+intent+"\n"+linked)
}
