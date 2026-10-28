package app

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/util"
)

// covers: MA-854, R37
// A file identity whose pin did not take is refused where it is used (the P3-8 fixes'
// pass, WINPATH-1): on Windows its identity would be read only at the comparison, from
// whatever is at the path by then, so a file swapped in would match. The program's own
// file then has no identity and no self-copy is made from it; the program folder is
// not held; a program copy judged is unreadable this run (never a change, R4); an
// archive file being exported is refused as replaced. A pin fails for real only in a
// race on Windows, so it is injected here. Positive twin: each with the pin taken.
func TestAnIdentityWhosePinDidNotTakeIsRefused(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "mailarchive.exe")
	body := []byte("the program as it started")
	os.WriteFile(exe, body, 0o755)
	progDir := filepath.Join(dir, container.ProgramDir)
	os.Mkdir(progDir, 0o755)
	os.WriteFile(filepath.Join(progDir, "copy"), body, 0o644)
	sum := sha256.Sum256(body)
	copyFile := container.ProgramFile{Name: "copy", Size: int64(len(body)), SHA256: hex.EncodeToString(sum[:])}
	os.WriteFile(filepath.Join(dir, "index.html"), body, 0o644)
	arch, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer arch.Close()
	stand := func() (string, error) { return exe, nil }
	orig := selfExecutable
	defer func() { selfExecutable = orig }()

	// Positive twin: every pin takes.
	self := selfFile(stand)
	selfExecutable = func() (string, os.FileInfo) { return exe, self.fi }
	src, serr := selfSource()
	held, herr := openProgramDir(progDir, false)
	if herr != nil {
		t.Fatalf("positive twin: the program folder is held: %v", herr)
	}
	j, _ := held.judgeOne(copyFile)
	f, _, ferr := (&ArchiveSource{}).openGatedIn(arch, "index.html")
	if ferr == nil {
		f.Close()
	}
	if self.fi == nil || serr != nil || src != exe || j != jOK || ferr != nil {
		t.Fatalf("positive twin: self %v (%v), judged %v, archive file %v", self.fi != nil, serr, j, ferr)
	}

	restore := util.FailPinsForTest()
	defer restore()
	self = selfFile(stand)
	selfExecutable = func() (string, os.FileInfo) { return exe, self.fi }
	_, serr = selfSource()
	msg := ""
	if serr != nil {
		msg = serr.Error()
	}
	code := 0
	if self.fi == nil && serr != nil {
		code = 1
	}
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("this program's own file"))

	_, herr = openProgramDir(progDir, false)
	j, _ = held.judgeOne(copyFile)
	held.close()
	_, _, ferr = (&ArchiveSource{}).openGatedIn(arch, "index.html")
	var afe *ArchiveFileError
	assure.Reached(t, herr != nil && j == jUnreadable && errors.As(ferr, &afe) && afe.Kind == AFChanged,
		"the folder not held, the copy unreadable, the archive file refused as replaced")
}
