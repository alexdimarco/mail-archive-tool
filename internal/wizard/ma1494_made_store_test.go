package wizard

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ncruces/zenity"

	"mail-archive-tool/internal/job"
)

// unfinishedSetFolder makes name in parent a folder a set made and never finished: no manifest, and
// one message page already copied into it (stray, its path).
func unfinishedSetFolder(t *testing.T, parent, name string) (dir, stray string) {
	t.Helper()
	dir = filepath.Join(parent, name)
	stray = filepath.Join(dir, "INBOX", "2025-03-03 a private page.html")
	if err := os.MkdirAll(filepath.Dir(stray), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stray, []byte("mail copied before the crash"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, stray
}

// covers: MA-1494, MA-1500, S90
// An unfinished folder is continued only for its own store (the adversarial pass's OUT-03 = CON-07
// and OUT-04; the lead's fold). The made-list's line now keeps the mailbox's store id beside its
// name, and §7's unfinished-folder rule continues a folder only for that store: another mailbox that
// has the name now (an account renamed, a second profile listed first) gets a new folder, and the mail
// copied for the first never ends up in its archive. A line written before, with no store id, is read
// as it was and matched by the name. Names are compared in composed form (a drive that lists names
// decomposed, as HFS+ does, still continues the folder), and the line's parent by identity when its
// spelling differs (letter case). Positive twin first: the mailbox the folder was made for continues
// it, whatever its name now. A new set folder's line keeps its store id.
func TestAnUnfinishedFolderIsContinuedOnlyForItsOwnStore(t *testing.T) {
	ownSettings(t)
	rows, _ := setMail(t)
	f, err := job.MadeListFile()
	if err != nil {
		t.Fatal(err)
	}

	// Positive twin: the line keeps ann's store, and ann's row continues the folder, also renamed.
	p := setFresh(t)
	u, stray := unfinishedSetFolder(t, p, rows[0].name)
	if err := job.RecordMadeStore(f, u, rows[0].name, idOf(rows[0])); err != nil {
		t.Fatal(err)
	}
	ann := rows[0]
	ann.name = "ann renamed in Thunderbird"
	plan, err := planSet(p, []setRow{ann}, "darwin")
	if err != nil || !reflect.DeepEqual(outcomesOf(plan), []string{"ann renamed in Thunderbird: unfinished " + rows[0].name}) {
		t.Fatalf("positive twin: %q %v", outcomesOf(plan), err)
	}

	// Another mailbox that has the name now: a new folder, never the one made for ann, and its
	// finished archive holds none of ann's mail.
	bob := rows[1]
	bob.name = rows[0].name
	plan, err = planSet(p, []setRow{bob}, "darwin")
	if err != nil || !reflect.DeepEqual(outcomesOf(plan), []string{rows[0].name + ": new " + rows[0].name + " (2)"}) {
		t.Fatalf("another mailbox with the name continues the folder made for ann: %q %v", outcomesOf(plan), err)
	}
	before := treeOf(t, u)
	(&setScheduler{}).install(t)
	s := &flowScript{folders: []string{p}, summary: zenity.ErrCanceled}
	s.install(t)
	ch, err := chooseSet([]setRow{bob}, "darwin")
	if err != nil {
		t.Fatal(err)
	}
	made, err := runSet(ch, "darwin")
	if err != nil || len(made) != 1 || made[0].Folder != u+" (2)" || !holdsOnly(made[0].Folder, rows[1]) {
		t.Fatalf("bob's archive: %+v %v", made, err)
	}
	if !reflect.DeepEqual(treeOf(t, u), before) {
		t.Errorf("the folder made for ann was written into")
	}
	if _, err := os.Stat(filepath.Join(made[0].Folder, "INBOX", filepath.Base(stray))); err == nil {
		t.Errorf("ann's copied mail is in bob's archive")
	}
	data, err := os.ReadFile(f)
	if err != nil || !bytes.Contains(data, []byte(made[0].Folder+"\t"+rows[0].name+"\t"+idOf(rows[1])+"\n")) {
		t.Errorf("a new set folder's line does not keep its store id:\n%s", data)
	}

	// A line written before (two fields): read as it was, and matched by the name.
	q := setFresh(t)
	u2, _ := unfinishedSetFolder(t, q, rows[2].name)
	if err := job.RecordMade(f, u2, rows[2].name); err != nil {
		t.Fatal(err)
	}
	if plan, err = planSet(q, []setRow{rows[2]}, "darwin"); err != nil || !reflect.DeepEqual(outcomesOf(plan), []string{rows[2].name + ": unfinished " + rows[2].name}) {
		t.Errorf("a two-field line: %q %v", outcomesOf(plan), err)
	}

	// The parent spelled with other letter case: the same folder by identity where the drive ignores
	// case (macOS and Windows by default), another folder where it does not.
	c := setFresh(t)
	u3, _ := unfinishedSetFolder(t, c, rows[1].name)
	alt := filepath.Join(filepath.Dir(c), strings.ToUpper(filepath.Base(c)))
	if err := job.RecordMadeStore(f, filepath.Join(alt, rows[1].name), rows[1].name, idOf(rows[1])); err != nil {
		t.Fatal(err)
	}
	want := rows[1].name + ": new " + rows[1].name + " (2)"
	if afi, err := os.Stat(alt); err == nil {
		if cfi, _ := os.Stat(c); os.SameFile(afi, cfi) {
			want = rows[1].name + ": unfinished " + rows[1].name
		}
	}
	if plan, err = planSet(c, []setRow{rows[1]}, "darwin"); err != nil || !reflect.DeepEqual(outcomesOf(plan), []string{want}) {
		t.Errorf("the parent spelled another way: %q, want %q (%v; %s)", outcomesOf(plan), want, err, u3)
	}

	// A name listed decomposed (as HFS+ lists it) and recorded composed.
	n := setFresh(t)
	nfc, nfd := "josé@qz-a.example", "josé@qz-a.example"
	if err := os.Mkdir(filepath.Join(n, nfd), 0o700); err != nil {
		t.Fatal(err)
	}
	if ents, _ := os.ReadDir(n); len(ents) != 1 || ents[0].Name() != nfd {
		t.Logf("this drive does not keep a decomposed name (%q): that part is the lab's", ents)
		return
	}
	if err := job.RecordMadeStore(f, filepath.Join(n, nfc), nfc, idOf(rows[0])); err != nil {
		t.Fatal(err)
	}
	r := rows[0]
	r.name = nfc
	if plan, err = planSet(n, []setRow{r}, "darwin"); err != nil || !reflect.DeepEqual(outcomesOf(plan), []string{nfc + ": unfinished " + nfd}) {
		t.Errorf("an unfinished folder listed decomposed is not continued: %q %v", outcomesOf(plan), err)
	}
}
