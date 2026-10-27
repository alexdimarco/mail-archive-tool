package main

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
)

// covers: MA-830, R37
// A copy that left a message out exits 3, partial (FID-07): the copy is finished, the
// message is listed on the terminal ("left out:") and in the problems file inside
// the copy; and the plan says what Ctrl-C does to a copy and that MailArchive does
// not keep track of a stopped one.
func TestDecryptExitsPartialAndListsWhatItLeftOut(t *testing.T) {
	out, _ := backedUpArchive(t)
	var gone string
	filepath.WalkDir(filepath.Join(out, export.UnitsDir), func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() && gone == "" {
			gone = p
		}
		return nil
	})
	if gone == "" || os.Remove(gone) != nil {
		t.Fatal("no unit to take away")
	}
	parent := t.TempDir()
	var stdout, stderr bytes.Buffer
	a := &scripted{isAttended: true, answers: []string{"y", flowPassword}}
	err := decryptWith(a, out, parent, "", noKeyFlags(), decryptDay, &stdout, &stderr)
	var ex *exitError
	dest := filepath.Join(parent, filepath.Base(out)+"-unencrypted-2026-10-27")
	problems, perr := os.ReadFile(filepath.Join(dest, app.CopyProblemsName))
	_, marker := os.Stat(filepath.Join(dest, app.CopyMarkerName))
	assure.Reached(t, errors.As(err, &ex) && ex.code == 3 && strings.Contains(stderr.String(), "left out: ") && perr == nil && len(problems) > 0 &&
		os.IsNotExist(marker) && strings.Contains(stdout.String(), "Ctrl-C stops the copy") && strings.Contains(stdout.String(), "does not keep track"),
		"exit 3, listed: "+stderr.String())
}

// covers: MA-830, R37
// A copy with no search index is partial too (FID-07, a §16 call).
func TestACopyWithNoIndexIsPartial(t *testing.T) {
	assure.Reached(t, app.DecryptReport{Index: "none"}.ExitCode() == 3 && app.DecryptReport{Index: "rebuilt"}.ExitCode() == 0, "no index exits 3; a rebuilt one 0")
}
