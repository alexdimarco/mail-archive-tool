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
