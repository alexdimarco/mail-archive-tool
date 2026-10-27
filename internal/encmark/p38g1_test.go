package encmark

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-850, R37
// Each recorded-folder case says what happened and what to do with the folder
// named once — in the command to type where there is one; a folder gone asks first
// whether its drive is plugged in (P3-8 friction review, B12).
func TestEachCaseNamesTheFolderOnceWithItsNextStep(t *testing.T) {
	const folder = "/Volumes/Backup/Mail Archive"
	for c, next := range map[Case]string{
		Link:        "mailarchive forget-archive -out",
		Other:       "mailarchive forget-archive -out",
		Foreign:     "mailarchive forget-archive -out",
		Gone:        "mailarchive forget-archive -out",
		KeysMissing: "mailarchive restore-key-file -out",
		Unproven:    "run a backup into it at a terminal",
	} {
		w := Wording(c, folder)
		if n := strings.Count(w, folder); n != 1 || !strings.Contains(w, next) {
			t.Errorf("case %v names the folder %d times (want once) or lacks %q: %s", c, n, next, w)
		}
	}
	assure.Reached(t, strings.HasPrefix(Wording(Gone, folder), "Is the drive plugged in?"), "a gone folder asks for the drive first")
}

// covers: MA-850, R37
// Case L — a recorded archive's parent folder swapped for a link to where the
// archive now is (the Windows lab's junction swap) — is told as a link, never as an
// archive removed or gone, so forget-archive is not offered as if the archive were
// lost (B12). Positive twin: the archive in place reads as itself.
func TestALinkOnTheWayIsNotWordedAsRemoved(t *testing.T) {
	out, _ := archive(t)
	if r, err := At(out); err != nil || r.Case != None {
		t.Fatalf("positive twin: the archive in place: %+v %v", r, err)
	}
	parent := filepath.Dir(out)
	moved := parent + "-moved"
	if err := os.Rename(parent, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(moved, parent); err != nil {
		t.Skip("no symlinks here:", err)
	}
	r, err := At(out)
	if err != nil || r.Case != Link {
		t.Fatalf("a parent swapped for a link: want case L, got %+v %v", r, err)
	}
	assure.Refused(t, 1, r.Why, assure.Code(1), assure.Names("now a link", "was not removed", "use it where it is now"),
		assure.Forbid("used to hold", "Is the drive plugged in?", "removed the archive on purpose"))
}
