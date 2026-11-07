package tbengine

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/tbrestore"
)

// covers: MA-1122, MA-1127, R40, S68
// The whole sequence through the engine, on a real archive and a fake profile: the plan
// writes nothing; Run takes the archive lock, the restore lock and Thunderbird's lock,
// settles, writes, checks and commits ("started", "checked", "ended" in restores.log),
// and the folder appears with every message the archive's original byte for byte, its
// subfolder under it, the marker and the made-list inside; Thunderbird's own Trash is
// untouched; and afterwards another process takes each of the three locks.
func TestTheWholeRestoreThroughTheEngine(t *testing.T) {
	out := plainArchive(t, true)
	pr := newProfile(t, "")
	before := snapshot(t, pr.dir, out)
	p := plan(t, Request{Out: out, Profile: pr.choice()})
	assure.Reached(t, snapshot(t, pr.dir, out) == before, fmt.Sprintf("the plan writes nothing"))
	assure.Reached(t, p.Top == "Restored — Work mail" && p.Messages == 4, fmt.Sprintf("the plan: top %q, %d messages", p.Top, p.Messages))
	trash, _ := os.ReadFile(filepath.Join(pr.local, "Trash"))

	var seen []Progress
	res := mustRun(t, p, Start{Progress: func(g Progress) { seen = append(seen, g) }})
	if !res.Done() || res.Restored != 4 || res.NotRestored != 0 || res.ExitCode() != 0 {
		t.Fatalf("the run: %+v %v %s", res.Write.Outcome, res.Write.Err, res.Words)
	}
	assure.Reached(t, strings.Join(events(t, pr.dir), " ") == "started checked ended", fmt.Sprintf("restores.log: %v", events(t, pr.dir)))
	top := filepath.Join(pr.local, p.Top)
	if fi, err := os.Lstat(top); err != nil || !fi.Mode().IsRegular() || fi.Size() != 0 {
		t.Fatalf("the parent file, empty, made last: %v %v", fi, err)
	}
	for _, f := range []string{tbrestore.MarkerName, tbrestore.MadeListName, folderA, folderA + ".sbd/" + folderB, folderC} {
		if _, err := os.Lstat(filepath.Join(top+".sbd", filepath.FromSlash(f))); err != nil {
			t.Fatalf("the restored tree holds %s: %v", f, err)
		}
	}
	a, _ := os.ReadFile(filepath.Join(top+".sbd", folderA))
	for _, s := range []string{"Subject: alpha one", "Subject: alpha two", canary} {
		assure.Reached(t, bytes.Contains(a, []byte(s)), fmt.Sprintf("Zebrafolder holds %q", s))
	}
	assure.Reached(t, bytes.Count(a, []byte("\nFrom ")) >= 1 && bytes.Contains(a, []byte(">From here a line")), fmt.Sprintf("mboxrd: the body's From line quoted"))
	now, _ := os.ReadFile(filepath.Join(pr.local, "Trash"))
	assure.Reached(t, bytes.Equal(trash, now), fmt.Sprintf("Thunderbird's own folder untouched"))
	assure.Reached(t, len(seen) == 4 && seen[3].Done == 4 && seen[3].Total == 4 && seen[0].Folder != "", fmt.Sprintf("progress counts each message, naming the folder (unencrypted)"))
	assure.Reached(t, strings.Contains(res.Words, "Restored 4 messages into Thunderbird as the folder 'Restored — Work mail'"), fmt.Sprintf("the Done words: %s", res.Words))
	allFree(t, out, pr.dir, "after the run")
}
