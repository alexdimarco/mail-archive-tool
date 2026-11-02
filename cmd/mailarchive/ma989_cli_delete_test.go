package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/schedule"
)

// covers: MA-989, R36, R12
// delete-archive at a terminal (design-archive-delete §3.3, §3.1, §4.3, §8, §8.1): the plan on
// stderr, §4.3's question only when it applies, the folder's name typed; the result and then the
// whole deletion record on stdout; exit 0; a name that does not match deletes nothing.
func TestDeleteArchiveDeletesAPlainArchiveAtTheTerminal(t *testing.T) {
	e := newCLIDel(t)
	dir := e.plainArchive("Archive B")
	store := storeFolder(t, dir)
	entry := schedule.DefaultNameFor(dir)
	e.sched.installed[entry] = true
	notes, _ := os.ReadFile(filepath.Join(dir, "notes.txt"))

	// Positive twin: a complete delete.
	a := &scripted{isAttended: true, answers: []string{"Archive B"}}
	var out bytes.Buffer
	if err := deleteArchive(a, dir, "", "", &out); err != nil {
		t.Fatalf("delete-archive: %v\n%s\n%s", err, a.transcript(), out.String())
	}
	plan := strings.Join(a.said, "\n")
	for _, want := range []string{
		dir, "mail folder 1: " + store, entry, "What Delete does not reach:", "If you delete it, a line is added to this computer's deletion log (" + e.logPath() + ")", // MA-1028: B5, B12
	} {
		if !strings.Contains(plan, want) {
			t.Fatalf("the plan does not say %q:\n%s", want, plan)
		}
	}
	assure.Reached(t, prompted(a, "Type the folder's name, Archive B, to delete it"), "the name asked for")
	if prompted(a, "for retention") {
		t.Fatalf("§4.3's question was asked with no unrecorded mail copy:\n%s", a.transcript())
	}
	res := out.String()
	for _, want := range []string{
		"Deleted.", "Stopped the scheduled backup " + entry, "Removed from Your archives", "Recorded in deletions.log",
		"Deletion record (copy it now: MailArchive does not keep a copy)", "Status: complete",
	} {
		if !strings.Contains(res, want) {
			t.Fatalf("the result does not say %q:\n%s", want, res)
		}
	}
	for _, never := range []string{"Save this record now", "locked on this page"} {
		if strings.Contains(res, never) {
			t.Fatalf("the command line's result says the Desktop's %q:\n%s", never, res)
		}
	}
	if strings.Index(res, "Recorded in deletions.log") > strings.Index(res, "Deletion record (copy it now") {
		t.Fatalf("the record is not printed after the result:\n%s", res)
	}
	if !recordDigestOK(recordIn(res), e.logText()) {
		t.Fatalf("the printed record's digest is not its own or not the finished line's:\n%s\n%s", res, e.logText())
	}
	if _, err := os.Stat(filepath.Join(dir, ".mailarchive-manifest.json")); !os.IsNotExist(err) {
		t.Fatalf("the manifest is still there: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "notes.txt")); !bytes.Equal(got, notes) {
		t.Fatal("the person's own file was changed")
	}
	if e.listed(dir) {
		t.Fatal("the row is still in Your archives")
	}
	if r := e.sched.removedNames(); len(r) != 1 || r[0] != entry {
		t.Fatalf("the scheduler removed %v, want [%s]", r, entry)
	}

	// A name that does not match (another case): nothing deleted, nothing logged.
	c := e.plainArchive("Archive C")
	beforeC, logBefore := tree(t, c), e.logText()
	ac := &scripted{isAttended: true, answers: []string{"archive c", "archive c", "archive c"}} // three tries (MA-1028, B10)
	var outC bytes.Buffer
	err := deleteArchive(ac, c, "", "", &outC)
	assure.Refused(t, exitCode(err), errText(err), assure.Code(1),
		assure.Names("does not match", "Archive C", "nothing was deleted"),
		assure.NoSideEffect(func() bool { return sameTree(beforeC, tree(t, c)) && e.listed(c) && e.logText() == logBefore }))
	if outC.Len() != 0 {
		t.Fatalf("a refused delete printed a result:\n%s", outC.String())
	}
	// Asked three times, each wrong name said, nothing deleted between (MA-1028, B10, B8).
	if said := strings.Join(ac.said, "\n"); len(ac.prompts) != 3 || !prompted(ac, "Type the folder's name, Archive C, to delete it (capital letters count): ") ||
		!strings.Contains(said, "That is not the folder's name, Archive C (capital letters count). Nothing was deleted; 2 tries left.") ||
		!strings.Contains(said, "Nothing was deleted; 1 try left.") || !strings.Contains(errText(err), "Type it exactly as shown (capital letters count).") {
		t.Fatalf("the tries: %d prompts\n%s\n%v", len(ac.prompts), ac.transcript(), err)
	}

	// §4.3: an unrecorded mail copy is asked about, default no, and kept.
	d := e.plainArchive("Archive D")
	stray := filepath.Join(mailFolder(t, d), "old copy.eml")
	if err := os.WriteFile(stray, []byte("From: x\r\n\r\nold\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ad := &scripted{isAttended: true, answers: []string{"", "Archive D"}}
	var outD bytes.Buffer
	if err := deleteArchive(ad, d, "", "", &outD); err != nil {
		t.Fatalf("delete-archive with an unrecorded copy: %v\n%s\n%s", err, ad.transcript(), outD.String())
	}
	assure.Reached(t, prompted(ad, "Also delete this 1 file (for retention)?"), "§4.3's question") // one file (MA-1028, B7)
	if !strings.Contains(strings.Join(ad.said, "\n"), "old copy.eml") {
		t.Fatalf("the plan does not name the unrecorded copy:\n%s", ad.transcript())
	}
	if _, err := os.Stat(stray); err != nil {
		t.Fatalf("the unrecorded copy was removed though the answer was no: %v", err)
	}
	if !strings.Contains(outD.String(), "The folder is kept: 2 files are left in it: 1 file of yours (notes.txt) and 1 mail copy the archive did not record.") { // counted once (MA-1028, B7), yours named (MA-1038, N7)
		t.Fatalf("the result does not say the copy was kept:\n%s", outD.String())
	}
}
