package wizard

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ncruces/zenity"
)

// covers: MA-1494, MA-1495, MA-1496, S90
// RV-02 (the re-verify of rounds A and B, MEDIUM): on a Windows drive that keeps no access lists (FAT
// or exFAT, as on many USB sticks) the owner-only restriction cannot be taken and Windows reports every
// folder as owned by everyone, so the plan never looked at the set's own archives and every re-run made
// "NAME (2)" for each mailbox, each with its own schedule. The plan now asks once, through the parent's
// open handle, whether its volume keeps access lists (setNoAccessLists: GetVolumeInformationByHandleW
// and FILE_PERSISTENT_ACLS on Windows, never on unix); where it does not, a child is judged by "a real
// folder, not a link", never by its owner, and the plan's line for what it did not look at names links
// only. The seam is driven here (this Mac's drives keep owners), with the set's own folders made
// writable by others, as Windows reports every folder on such a drive. Positive twins first: the
// person's own folders are brought up to date; on a drive that keeps access lists, folders others can
// change are left unread and named so (INS-01, kept).
func TestASetOnADriveWithNoAccessListsBringsItsOwnArchivesUpToDate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("folder modes do not stand in for an access list here; the Windows lab's exFAT stick does (RV-02)")
	}
	ownSettings(t)
	rows, _ := setMail(t)
	parent := setFresh(t)
	(&setScheduler{}).install(t)
	s := &flowScript{folders: []string{parent}, summary: zenity.ErrCanceled}
	s.install(t)
	if made, err := makeSet(rows, "darwin"); err != nil || len(made) != len(rows) {
		t.Fatalf("the set: %+v %v", made, err)
	}
	real := setNoAccessLists
	t.Cleanup(func() { setNoAccessLists = real })
	parentFI, err := os.Stat(parent)
	if err != nil {
		t.Fatal(err)
	}
	var asked []os.FileInfo
	drive := func(noACL bool) {
		setNoAccessLists = func(f *os.File) bool {
			fi, err := f.Stat()
			if err != nil {
				t.Fatal(err)
			}
			asked = append(asked, fi)
			return noACL
		}
	}
	// upToDate reports whether p brings every archive of the set up to date and makes nothing new.
	upToDate := func(p setPlan) bool {
		for _, it := range p.items {
			if it.outcome == outNew || strings.Contains(it.folder, " (2)") {
				return false
			}
		}
		return true
	}

	// Positive twin: the person's own folders, on a drive with access lists.
	drive(false)
	if p, err := planSet(parent, rows, "darwin"); err != nil || !upToDate(p) || len(p.unlooked) != 0 {
		t.Fatalf("positive twin: the person's own folders are brought up to date: %v %q", err, outcomesOf(p))
	}

	// Every folder of the set writable by others, as Windows reports them on such a drive, and a link.
	for _, r := range rows {
		if err := os.Chmod(filepath.Join(parent, r.name), 0o775); err != nil {
			t.Fatal(err)
		}
	}
	elsewhere := archiveHolding(t, filepath.Join(t.TempDir(), "ann elsewhere"), idOf(rows[0]))
	setLinkTo(t, elsewhere, filepath.Join(parent, "Notes link"))

	// Positive twin of the rule: on a drive that keeps access lists, folders others can change are
	// never read and are named so (INS-01).
	p, err := planSet(parent, rows, "darwin")
	if err != nil || upToDate(p) || p.items[0].folder != rows[0].name+" (2)" ||
		!strings.Contains(setPlanText(p), setUnlookedMany+"Local Folders, Notes link, "+rows[0].name) {
		t.Fatalf("positive twin: on a drive with access lists a folder others can change is not read: %v %q\n%s", err, outcomesOf(p), setPlanText(p))
	}

	// A drive that keeps no access lists: every real folder is read whatever its owner; the link never.
	asked = nil
	drive(true)
	p, err = planSet(parent, rows, "darwin")
	text := setPlanText(p)
	if err != nil || !upToDate(p) || p.items[0].outcome != outContinue || p.items[0].folder != rows[0].name {
		t.Errorf("RV-02: on a drive with no access lists the set's own archives are brought up to date: %v %q\n%s", err, outcomesOf(p), text)
	}
	if !strings.Contains(text, "\n"+setUnlookedLinkOne+"Notes link") || strings.Contains(text, "another account owns or can change") {
		t.Errorf("RV-02: the plan names the link only, never \"another account owns or can change\":\n%s", text)
	}
	if len(asked) != 1 || !os.SameFile(asked[0], parentFI) {
		t.Errorf("the volume is asked once, through the parent's own handle: %d asks", len(asked))
	}

	// The re-run made: each archive brought up to date in its own folder, no "(2)" made.
	s = &flowScript{folders: []string{parent}, summary: zenity.ErrCanceled}
	s.install(t)
	made, err := makeSet(rows, "darwin")
	if err != nil || len(made) != 4 {
		t.Fatalf("the re-run: %+v %v", made, err)
	}
	for i, m := range made {
		if m.New || m.Outcome != MadeFinished || m.Folder != filepath.Join(parent, rows[i].name) {
			t.Errorf("RV-02: %s was not brought up to date in its own folder: %+v", rows[i].name, m)
		}
	}
	if ents, _ := os.ReadDir(parent); len(ents) != len(rows)+1 {
		t.Errorf("the re-run made a folder: %d entries in the parent", len(ents))
	}

	// Two links: the line's form for more than one.
	if err := os.Remove(filepath.Join(parent, "Notes link")); err != nil {
		t.Fatal(err)
	}
	setLinkTo(t, t.TempDir(), filepath.Join(parent, "A"))
	setLinkTo(t, t.TempDir(), filepath.Join(parent, "B"))
	if p, err = planSet(parent, rows, "darwin"); err != nil || !strings.Contains(setPlanText(p), "\n"+setUnlookedLinkMany+"A, B") {
		t.Errorf("the line for two links: %v\n%s", err, setPlanText(p))
	}
}
