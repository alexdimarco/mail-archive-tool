package archdelete

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// covers: MA-1038, R36
// The person's own files Delete keeps are named on screen, not only counted (the 1.5.3 re-walk's
// N7): the plan's "Kept (not the archive's): NAMES" line, which the terminal shows as the panel
// does, and the result's "The folder is kept: …" sentence, the first ten then "and N more".
// Positive twin first: an archive with nothing of the person's has no kept line and its folder
// goes.
func TestTheKeptFilesAreNamedInThePlanAndTheResult(t *testing.T) {
	twin := newRig(t, smallArchive(t, t.TempDir()))
	if p := twin.plan(t); p.KeptLine() != "" {
		t.Fatalf("the twin keeps %q", p.KeptLine())
	}
	if res := twin.run(t, false); !strings.HasSuffix(res.Lines[0], "The folder is gone.") {
		t.Fatalf("the twin's result: %s", res.Message())
	}

	dir := smallArchive(t, t.TempDir())
	writeFile(t, filepath.Join(dir, "my-notes.txt"), "mine")
	r := newRig(t, dir)
	p := r.plan(t)
	if got := p.KeptLine(); got != "Kept (not the archive's): my-notes.txt" {
		t.Fatalf("kept line: %q", got)
	}
	res := Execute(p, p.Name, false, r.x)
	if res.Status != StatusComplete || !strings.HasSuffix(res.Lines[0], "The folder is kept: it holds 1 file that is not the archive's: my-notes.txt.") {
		t.Fatalf("result: %s", res.Message())
	}

	// Twelve: the first ten, then "and 2 more"; the sentence with mail copies names yours.
	var names []string
	for i := 1; i <= 12; i++ {
		names = append(names, fmt.Sprintf("k%02d.txt", i))
	}
	ten := strings.Join(names[:10], ", ")
	for _, c := range []struct {
		kept, copies int
		yours        []string
		want         string
	}{
		{12, 0, names, "The folder is kept: it holds 12 files that are not the archive's: " + ten + " and 2 more."},
		{3, 1, names[:2], "The folder is kept: 3 files are left in it: 2 files of yours (k01.txt, k02.txt) and 1 mail copy the archive did not record."},
		{1, 1, nil, "The folder is kept: 1 file is left in it: 1 mail copy the archive did not record."},
	} {
		if got := keptSentence(c.kept, c.copies, c.yours); got != c.want {
			t.Errorf("keptSentence(%d, %d, %d names) = %q, want %q", c.kept, c.copies, len(c.yours), got, c.want)
		}
	}
	p.Kept = names
	if got := p.KeptLine(); got != "Kept (not the archive's): "+ten+" and 2 more" {
		t.Errorf("twelve kept: %q", got)
	}
}

// covers: MA-1038, R36
// The files "of yours" the result counts are the ones it names: the plan's kept list. Operating-
// system litter in a folder only an unticked mail copy passes through goes with the copies and is
// never counted as the person's (the settle pass's MED-2: a .DS_Store beside an unticked copy made
// "4 files of yours" of three named). Positive twin first: the same folder with no litter beside
// the copy says the same sentence.
func TestTheKeptCountIsTheNamedFiles(t *testing.T) {
	build := func(litter bool) (*DeletePlan, *Result) {
		dir := plainBesidePersonsFiles(t)
		writeManifest(t, dir, 6, map[string]string{"path:/mail/Mailbox.pst": "Mailbox", "path:/mail/Other.pst": "Other"}, []rec{
			{Path: stemA + ".html"}, {Path: stemB + ".html"},
		})
		writeFile(t, filepath.Join(dir, "Other", "2020-01-02_0000_x_11bb11bb.eml"), "a copy")
		if litter {
			writeFile(t, filepath.Join(dir, "Other", ".DS_Store"), "litter")
		}
		r := newRig(t, dir)
		p := r.plan(t)
		return p, Execute(p, p.Name, false, r.x)
	}
	sentence := func(p *DeletePlan) string {
		return fmt.Sprintf("The folder is kept: %d files are left in it: 3 files of yours (Mailbox/Inbox/notes.txt, Photos, taxes.pdf) and %s.",
			3+len(p.Unrecorded), copyWords(len(p.Unrecorded)))
	}
	tp, twin := build(false)
	if len(tp.UnrecordedLitter) != 0 || len(tp.Kept) != 3 || !strings.HasSuffix(twin.Lines[0], sentence(tp)) {
		t.Fatalf("the twin (kept %q): %s", tp.Kept, twin.Lines[0])
	}
	p, res := build(true)
	if len(p.UnrecordedLitter) != 1 {
		t.Fatalf("fixture: no litter beside the copy: %q", p.UnrecordedLitter)
	}
	if !strings.HasSuffix(res.Lines[0], sentence(p)) {
		t.Fatalf("with litter beside the unticked copy, want %q:\n%s", sentence(p), res.Lines[0])
	}
}

// manifestWithFolders writes a manifest whose records sit in stores with the mail folders given
// ({store, folder, stem}), and each record's page.
func manifestWithFolders(t *testing.T, dir string, recs [][3]string) {
	t.Helper()
	entries := map[string]map[string]any{}
	stores := map[string]string{}
	for i, r := range recs {
		path := r[0] + "/" + r[2] + ".html"
		entries[fmt.Sprintf("%s\x00%s\x00id%d", r[0], r[1], i)] = map[string]any{"path": path, "folder": r[1], "exported_at": "2024-03-01T09:00:00Z"}
		stores["path:/"+r[0]] = r[0]
		writeFile(t, filepath.Join(dir, filepath.FromSlash(path)), "page")
	}
	e, _ := json.Marshal(entries)
	s, _ := json.Marshal(stores)
	writeFile(t, filepath.Join(dir, manifestName), fmt.Sprintf(`{"version":6,"entries":%s,"stores":%s}`, e, s))
}

// covers: MA-1038, R36
// Each mail folder on screen names the mail folders the archive recorded inside it when they are
// named otherwise (the 1.5.3 re-walk's N16: a Thunderbird source folder named "mbox" showed
// "mail folder 1: mbox", with no Inbox), and the saved plan and the record say "mail folder N/",
// never "store N/", and still name no folder below it [O2-01]. Positive twin first: a store named
// as its only mail folder is named once.
func TestTheMailFoldersAreNamedAsThePersonKnowsThem(t *testing.T) {
	twin := t.TempDir()
	manifestWithFolders(t, twin, [][3]string{{"Inbox", "Inbox", "2024-03-01_0900_a_11111111"}})
	if got := mustPlan(t, twin, testDeps()).StoreLabel(0); got != "Inbox" {
		t.Fatalf("the twin: %q", got)
	}

	dir := t.TempDir()
	manifestWithFolders(t, dir, [][3]string{
		{"mbox", "Inbox", "2024-03-01_0900_a_11111111"},
		{"mbox", "Sent", "2024-03-01_0901_b_22222222"},
		{"mbox", "Inbox", "2024-03-01_0902_c_33333333"},
	})
	writeFile(t, filepath.Join(dir, "mbox", "2019-01-01_0000_old_99999999.html"), "an unrecorded copy")
	p := mustPlan(t, dir, testDeps())
	if got := p.StoreLabel(0); got != "mbox (Inbox, Sent)" {
		t.Fatalf("label: %q", got)
	}
	saved := string(RecordText(RecordInput{Status: StatusPlanned, When: time.Now(), Plan: p}))
	if !strings.Contains(saved, "Mail copy not recorded: mail folder 1/2019-01-01_0000_…_99999999.html\n") || strings.Contains(saved, "store 1") ||
		strings.Contains(saved, "Sent") {
		t.Fatalf("saved plan:\n%s", saved)
	}
}

// covers: MA-1038, R36
// The result says where a delete stopped in words, never the design's step numbers (the 1.5.3
// re-walk's N20): "stopped before step 4" became "stopped before it updated MailArchive's
// settings", and a re-run of a folder an earlier delete removed says what it did not do, not
// "Steps 1 and 5 to 7 … were skipped". Positive twin first: a delete that is not stopped says
// neither.
func TestWhereADeleteStoppedIsSaidInWords(t *testing.T) {
	twin := newRig(t, smallArchive(t, t.TempDir()))
	if res := twin.run(t, false); res.Status != StatusComplete || strings.Contains(res.Message(), "The delete stopped") || strings.Contains(res.Message(), "step") {
		t.Fatalf("the twin: %s", res.Message())
	}

	for _, c := range []struct {
		step int
		want string
	}{
		{4, "A MailArchive Desktop is running. The delete stopped before it updated MailArchive's settings. Steps done: "},
		{8, "A MailArchive Desktop is running. The delete stopped before its last steps (this computer's record of the archive, and its row in Your archives). Steps done: "},
		{9, "A MailArchive Desktop is running. The delete stopped before it removed the archive's row from Your archives. Steps done: "},
	} {
		r := newRig(t, smallArchive(t, t.TempDir()))
		r.x.BeforeStep = func(step int) error {
			if step == c.step {
				return errors.New("A MailArchive Desktop is running")
			}
			return nil
		}
		res := r.run(t, false)
		if res.Status != StatusPartial || !strings.HasPrefix(res.Lines[0], c.want) || strings.Contains(res.Message(), "step ") {
			t.Errorf("a stop before step %d: %s", c.step, res.Message())
		}
	}

	r := newRig(t, smallArchive(t, t.TempDir()))
	stopBefore8(t, r)
	res := r.run(t, false)
	if res.Status != StatusComplete || !hasLine(res, "This delete did not lock the archive, empty MailArchive's working folder for it or remove any files: an earlier delete had already removed the folder.") ||
		strings.Contains(res.Message(), "Steps 1") {
		t.Fatalf("the folder-gone re-run: %s", res.Message())
	}
}
