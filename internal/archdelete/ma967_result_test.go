package archdelete

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const unrecA = "Mailbox/Inbox/2020-01-01_0000_old_00aa00aa.html"

// covers: MA-967, R36
func TestUnrecordedCopiesAreKeptUnlessTickedAndThenOnlyTheListedOnesGo(t *testing.T) {
	// Unticked (the default): kept, and the result never says only "Deleted".
	dir := plainBesidePersonsFiles(t)
	writeFile(t, filepath.Join(dir, filepath.FromSlash(unrecA)), "an older copy")
	r := newRig(t, dir)
	res := r.run(t, false)
	if res.Status != StatusComplete || !exists(filepath.Join(dir, filepath.FromSlash(unrecA))) ||
		!strings.Contains(res.Lines[0], "The folder is kept: 4 files are left in it: 3 files of yours and 1 mail copy the archive did not record.") || !contains(res.Kept, unrecA) { // counted once (MA-1028, B7)
		t.Fatalf("unticked: %s (kept %q)", res.Message(), res.Kept)
	}

	// Ticked: exactly the listed copies go; the person's other files stay; a folder
	// holding only the copy and a .DS_Store is not kept for the litter alone.
	dir = plainBesidePersonsFiles(t)
	only := "Other/2020-01-02_0000_x_11bb11bb.eml"
	writeManifest(t, dir, 6, map[string]string{"path:/mail/Mailbox.pst": "Mailbox", "path:/mail/Other.pst": "Other"}, []rec{
		{Path: stemA + ".html"}, {Path: stemB + ".html"},
	})
	writeFile(t, filepath.Join(dir, filepath.FromSlash(unrecA)), "an older copy")
	writeFile(t, filepath.Join(dir, filepath.FromSlash(only)), "a copy")
	writeFile(t, filepath.Join(dir, "Other", ".DS_Store"), "litter")
	r = newRig(t, dir)
	res = r.run(t, true)
	if res.Status != StatusComplete {
		t.Fatalf("ticked: %s", res.Message())
	}
	for _, gone := range []string{unrecA, only, "Other"} {
		if exists(filepath.Join(dir, filepath.FromSlash(gone))) {
			t.Errorf("%s must be gone", gone)
		}
	}
	for _, kept := range []string{"taxes.pdf", "Photos/beach.jpg", "Mailbox/Inbox/notes.txt"} {
		if !exists(filepath.Join(dir, filepath.FromSlash(kept))) {
			t.Errorf("the person's %s must be kept", kept)
		}
	}
}

// covers: MA-967, R36
func TestATickedFileThatCannotBeRemovedIsNotFullyDeletedWithTheByHandRemedy(t *testing.T) {
	dir := plainBesidePersonsFiles(t)
	writeFile(t, filepath.Join(dir, filepath.FromSlash(unrecA)), "an older copy")
	r := newRig(t, dir)
	faultOn(t, nil, func(rel string) error {
		if rel == unrecA {
			return inUseErr(rel)
		}
		return nil
	})
	res := r.run(t, true)
	want := "Not fully deleted. 1 file you chose to delete could not be removed: " + unrecA + ". MailArchive no longer recognises this folder as an archive: delete it by hand." // one file (MA-1028, B7)
	if res.Status != StatusPartial || res.ExitCode() != 3 || res.Lines[0] != want {
		t.Fatalf("got:\n%s\nwant first line %q", res.Message(), want)
	}
	if strings.Contains(res.Message(), "click Delete again") || res.Unfinished {
		t.Fatal("a ticked file's failure never offers Delete again and never marks the row unfinished")
	}
	if exists(filepath.Join(dir, manifestName)) || len(r.rows) != 1 {
		t.Fatal("the manifest is not kept for a ticked file, and step 9 removes the row")
	}
	if ev := events(logLines(t, r.x.Log)); ev[len(ev)-1] != "finished partial" {
		t.Fatalf("log %q", ev)
	}
}

// deniedOneAtRoot is the denied remedy for one file in the archive's own folder: on macOS and
// Linux it names that folder (amended 2026-11-02, the 1.5.3 friction review's B6), on Windows
// none.
func deniedOneAtRoot() string {
	r := strings.Replace(deniedRemedy, "delete these by hand", "delete it by hand", 1)
	if deniedNamesFolders {
		return "The system did not allow it in the archive's folder (a locked file, a folder you may not change, or a read-only drive): unlock it and click Delete again, or delete it by hand."
	}
	return r
}

// covers: MA-967, R36
func TestEachLeftoverGetsTheRemedyOfItsErrorType(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    func(string) error
		remedy string
	}{
		{"in use", inUseErr, "Close Outlook or any program using it, then click Delete again."}, // one file (MA-1028, B7)
		{"denied", deniedErr, deniedOneAtRoot()}, // one file (MA-1028, B7), in the archive's folder (B6)
		{"device gone", goneErr, "Reconnect the drive, then click Delete again."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := plainBesidePersonsFiles(t)
			r := newRig(t, dir)
			faultOn(t, nil, func(rel string) error {
				if rel == "search.db" {
					return tc.err(rel)
				}
				return nil
			})
			res := r.run(t, false)
			if res.Status != StatusPartial || !strings.Contains(res.Lines[0], "search.db") || !strings.HasSuffix(res.Lines[0], tc.remedy) {
				t.Fatalf("%s", res.Message())
			}
			if len(res.NotRemoved) != 1 || len(res.Kept) != 3 {
				t.Fatalf("leftovers: not removed %v, kept %q", res.NotRemoved, res.Kept)
			}
		})
	}
}

// covers: MA-967, R36
func TestTheThreeKindsOfLeftoverAndTheCompletionRule(t *testing.T) {
	// The person's files (c) never block completion; litter (b) is removed, also from
	// a store folder; a name the archive recorded (a) does.
	dir := plainBesidePersonsFiles(t)
	r := newRig(t, dir)
	res := r.run(t, false)
	if res.Status != StatusComplete || res.LitterRemoved != 2 || len(res.Kept) != 3 || len(res.NotRemoved) != 0 {
		t.Fatalf("%s (litter %d kept %q)", res.Message(), res.LitterRemoved, res.Kept)
	}
	if exists(filepath.Join(dir, "Mailbox", "Inbox", ".DS_Store")) {
		t.Fatal("litter in a store folder is removed")
	}
	// A record shows each kind.
	text := string(res.Record)
	for _, want := range []string{"Status: complete\n", "Left (could not be removed): 0\n", "Kept (not the archive's): 3\n", "Litter removed: 2\n"} {
		if !strings.Contains(text, want) {
			t.Errorf("record lacks %q:\n%s", want, text)
		}
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatal("a folder holding the person's files is kept")
	}
}
