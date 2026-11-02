package archdelete

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/lockfile"
)

// heldReader writes a reader's scratch index for pid in scratch and holds its lock, as a live
// reader does; the returned func lets it go.
func heldReader(t *testing.T, scratch string, pid int) func() {
	t.Helper()
	name := fmt.Sprintf("reader-%d-1.db", pid)
	writeFile(t, filepath.Join(scratch, "index", name), "index")
	writeFile(t, filepath.Join(scratch, "index", name+".lock"), "")
	f, err := lockfile.HoldFile(filepath.Join(scratch, "index", name+".lock"))
	if err != nil {
		t.Fatal(err)
	}
	return func() { f.Close() }
}

// covers: MA-1012, R36
// The plan lists an encrypted archive's scratch folders (design-archive-delete §3.1 [C7,
// R3:NF-C7-OWN-READER]; slice 3a's conformance D1): a reader another program holds, or a file
// MailArchive did not put there, refuses at the plan in §7's words — before any started line and
// before the page's open archive is locked — and a reader of this process is excused only while
// the page has the archive open (step 1 closes it). Positive twins first: a scratch folder of the
// archive's own files is planned; this process's reader with the archive open here is planned.
func TestThePlanRefusesAReaderHeldElsewhereButNotThisPagesOwn(t *testing.T) {
	dir := encryptedClean(t)
	root := t.TempDir()
	scratch := scratchFor(root, dir, testArchiveID)
	deps := testDeps()
	deps.Record = recordedAt(dir)
	deps.ScratchDir = func(spelling, id string) (string, error) { return scratchFor(root, spelling, id), nil }
	openHere := false
	deps.OpenHere = func(string) bool { return openHere }

	writeFile(t, filepath.Join(scratch, "snap", ".mailarchive-snapshot-1"), "x")
	mustPlan(t, dir, deps)
	release := heldReader(t, scratch, os.Getpid())
	openHere = true
	mustPlan(t, dir, deps) // this page's own reader, the archive open here
	release()
	os.RemoveAll(filepath.Join(scratch, "index"))

	for _, c := range []struct {
		name  string
		setup func() func()
		open  bool
		want  Case
		says  string
	}{
		{"a reader of another program", func() func() { return heldReader(t, scratch, os.Getpid()+1) }, true, CaseReader, msgReaderOpen},
		{"this process's reader, the archive not open here", func() func() { return heldReader(t, scratch, os.Getpid()) }, false, CaseReader, msgReaderOpen},
		{"a file MailArchive did not put there", func() func() {
			writeFile(t, filepath.Join(scratch, "notes.txt"), "the person's")
			return func() { os.Remove(filepath.Join(scratch, "notes.txt")) }
		}, true, CaseScratch, "MailArchive's working folder for this archive holds files it did not put there: " + scratch},
	} {
		t.Run(c.name, func(t *testing.T) {
			done := c.setup()
			defer done()
			defer os.RemoveAll(filepath.Join(scratch, "index"))
			openHere = c.open
			before := statTree(t, dir)
			rc, msg, pc := refusalOf(t, dir, deps)
			if pc != c.want {
				t.Fatalf("the plan says %s %q; want the %s refusal", pc, msg, c.want)
			}
			assure.Refused(t, rc, msg, assure.Code(1), assure.Names(c.says),
				assure.NoSideEffect(func() bool { return sameTree(before, statTree(t, dir)) && exists(scratch) }))
		})
	}
}
