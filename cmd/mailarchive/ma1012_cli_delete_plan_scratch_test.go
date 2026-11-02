package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/lockfile"
)

// covers: MA-1012, R36
// delete-archive's plan lists the encrypted archive's scratch folder (design-archive-delete §3.1
// [C7]; slice 3a's conformance D1): a reader another program holds — serve or search — refuses
// at the plan, before the name is asked and before deletions.log is written. The command line has
// no page, so no reader is excused. Positive twin first: with no reader, the plan is made and
// saved.
func TestDeleteArchiveRefusesAReaderHeldElsewhereAtThePlan(t *testing.T) {
	e := newCLIDel(t)
	flowRecord(t)
	out := filepath.Join(t.TempDir(), "Sealed archive")
	mk := &scripted{isAttended: true, answers: []string{flowPassword, flowPassword, "", "", keyAnswer}}
	u, err := createEncrypted(mk, out, noKeyFlags(), false, nil)
	if err != nil {
		t.Fatalf("make the archive: %v\n%s", err, mk.transcript())
	}
	u.Destroy()
	e.list(out)
	scratch, err := app.ArchiveScratchFor(out, recordedAt(t, out))
	if err != nil {
		t.Fatal(err)
	}

	planOut := filepath.Join(t.TempDir(), "plan.txt")
	if err := deleteArchive(&scripted{isAttended: true}, out, planOut, "", io.Discard); err != nil {
		t.Fatalf("positive twin: the plan with no reader: %v", err)
	}
	assure.Reached(t, exists(planOut), "the saved plan")

	name := filepath.Join(scratch, "index", fmt.Sprintf("reader-%d-1.db", os.Getpid()+1))
	if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(name, []byte("index"), 0o600)
	os.WriteFile(name+".lock", nil, 0o600)
	f, err := lockfile.HoldFile(name + ".lock")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	a := &scripted{isAttended: true, answers: []string{"Sealed archive"}}
	err = deleteArchive(a, out, "", "", io.Discard)
	assure.Refused(t, exitCode(err), errText(err), assure.Code(1),
		assure.Names("Close MailArchive serve, search or other MailArchive windows for this archive first."),
		assure.NoSideEffect(func() bool {
			return exists(out) && e.listed(out) && e.logText() == "" && !strings.Contains(strings.Join(a.prompts, "\n"), "Type the folder's name")
		}))
}
