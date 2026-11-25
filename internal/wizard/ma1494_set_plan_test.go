package wizard

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/seal"
	"mail-archive-tool/internal/state"
)

// setFixture is the set of a profile shaped like the owner's (its rows in the list's order:
// qz.student@mail.qzuni.example, qzsam.walker@gmail.com, qzsam.work@gmail.com, qzsam@qzname.example,
// qzwork@qzwork.example, Local Folders), with extra stores beside them, and the rows by name.
func setFixture(t *testing.T, extra ...pickStore) ([]setRow, map[string]setRow) {
	t.Helper()
	stores := append(append([]pickStore(nil), ownerStores...), extra...)
	named := pickProfile(t, t.TempDir(), "a1b2c3d4.default-release", ownerShaped(true), stores...)
	rows := setRows(listRows(gatherPickRows(storesOf(named, stores), time.Now().Add(time.Minute), sizeEntryMax), "darwin"), "darwin")
	by := map[string]setRow{}
	for _, r := range rows {
		by[r.name] = r
	}
	return rows, by
}

// idOf is the store id a backup of r's mailbox writes in its archive's manifest.
func idOf(r setRow) string { return state.PathSourceID(r.path) }

// archiveHolding makes dir (0700) a plaintext archive whose manifest holds the store ids.
func archiveHolding(t *testing.T, dir string, ids ...string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	stores := map[string]string{}
	for i, id := range ids {
		stores[id] = fmt.Sprintf("t%d", i)
	}
	b, err := json.Marshal(map[string]any{"version": 1, "stores": stores})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".mailarchive-manifest.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// setJobFile writes the job file of the archive folder out (raw bytes when body is not nil).
func setJobFile(t *testing.T, out string, j job.Job, body []byte) {
	t.Helper()
	p, err := job.PathFor(schedule.DefaultNameFor(out))
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, body, 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	j.Out = out
	if err := job.Write(p, j); err != nil {
		t.Fatal(err)
	}
}

// outcomesOf says each item of p as "name: outcome folder", for a comparison that reads.
func outcomesOf(p setPlan) []string {
	var out []string
	for _, it := range p.items {
		s := it.row.name + ": "
		switch it.outcome {
		case outNew:
			s += "new " + it.folder
		case outContinue:
			s += "continue " + it.folder
		case outUnfinished:
			s += "unfinished " + it.folder
		case outPaused:
			s += "paused " + it.folder
		case outKept:
			s += fmt.Sprintf("kept %s exact=%v", it.folder, it.exact)
		case outNotMade:
			s += "not made, " + it.why
		}
		if len(it.also) > 0 {
			s += " also " + strings.Join(it.also, ",")
		}
		for _, c := range it.reused {
			s += fmt.Sprintf(" reused %s paused=%v", c.name, c.paused)
		}
		if it.jobUnreadable {
			s += " (job unreadable)"
		}
		out = append(out, s)
	}
	return out
}

// covers: MA-1494
// A folder already there (§7): each mailbox's archive is found by what it holds, never by its name.
// Positive twin first: an empty parent plans a new folder for every mailbox, named by §4. Then a
// re-run: the same mailbox's archive is continued whatever its name (an account renamed since, a
// numbered twin: the first by name continued, the other left), a combined archive at a mailbox's
// name is not continued and its name is taken (the next free number), a link at a name is never
// read nor written into, a folder writable by others neither, and an archive whose mailbox is not
// found now is listed. Its archive's job file: one with auto-discovery on is not this mailbox's
// archive; one with this one input is; one that cannot be read is continued and marked (F-11). A
// paused archive is left as it is. A folder the wizard made but never finished, named in the
// made-list for this mailbox in this parent, is continued; one the list does not name is not.
func TestASetsPlanFindsEachMailboxsArchiveByWhatItHolds(t *testing.T) {
	ownSettings(t)
	rows, by := setFixture(t)
	empty := t.TempDir()
	p, err := planSet(empty, rows, "darwin")
	want := []string{
		"qz.student@mail.qzuni.example: new qz.student@mail.qzuni.example",
		"qzsam.walker@gmail.com: new qzsam.walker@gmail.com",
		"qzsam.work@gmail.com: new qzsam.work@gmail.com",
		"qzsam@qzname.example: new qzsam@qzname.example",
		"qzwork@qzwork.example: new qzwork@qzwork.example",
		"Local Folders: new Local Folders",
	}
	if err != nil || !reflect.DeepEqual(outcomesOf(p), want) || p.unread != nil || p.notFound != nil || p.more {
		t.Fatalf("positive twin: an empty parent: %v\n got %q\nwant %q", err, outcomesOf(p), want)
	}
	if entries, _ := os.ReadDir(empty); len(entries) != 0 {
		t.Fatalf("planning wrote in the parent: %v", entries)
	}

	// Amended 2026-11-25 (round D; nothing weakened): the re-run, whose fixture holds a symbolic link,
	// is its own subtest (symlinkOrSkip), so where a link cannot be made (an ordinary Windows user, the
	// Windows lab of MA-1503) it alone is skipped, with the reason, and the parts below still run.
	t.Run("a re-run", func(t *testing.T) {
		parent := t.TempDir()
		archiveHolding(t, filepath.Join(parent, "qzsam.walker@gmail.com"), idOf(by["qzsam.walker@gmail.com"]))
		archiveHolding(t, filepath.Join(parent, "old-name@qzuni.example"), idOf(by["qz.student@mail.qzuni.example"]))
		archiveHolding(t, filepath.Join(parent, "qzsam.work@gmail.com"), idOf(by["qzsam.work@gmail.com"]), idOf(by["qzsam@qzname.example"]))
		archiveHolding(t, filepath.Join(parent, "qzwork@qzwork.example"), idOf(by["qzwork@qzwork.example"]))
		archiveHolding(t, filepath.Join(parent, "qzwork@qzwork.example (2)"), idOf(by["qzwork@qzwork.example"]))
		archiveHolding(t, filepath.Join(parent, "sam@gone.example"), "path:/nowhere/ImapMail/imap.gone.example")
		elsewhere := archiveHolding(t, filepath.Join(t.TempDir(), "elsewhere"), idOf(by["Local Folders"]))
		symlinkOrSkip(t, elsewhere, filepath.Join(parent, "Local Folders"))
		before := treeOf(t, parent)
		p, err := planSet(parent, rows, "darwin")
		want := []string{
			"qz.student@mail.qzuni.example: continue old-name@qzuni.example",
			"qzsam.walker@gmail.com: continue qzsam.walker@gmail.com",
			"qzsam.work@gmail.com: new qzsam.work@gmail.com (2)",
			"qzsam@qzname.example: new qzsam@qzname.example",
			"qzwork@qzwork.example: continue qzwork@qzwork.example also qzwork@qzwork.example (2)",
			"Local Folders: new Local Folders (2)",
		}
		if err != nil || !reflect.DeepEqual(outcomesOf(p), want) {
			t.Fatalf("a re-run: %v\n got %q\nwant %q", err, outcomesOf(p), want)
		}
		if !reflect.DeepEqual(p.notFound, []string{"sam@gone.example"}) || p.unread != nil {
			t.Errorf("not found now: %q, unread %q", p.notFound, p.unread)
		}
		if !reflect.DeepEqual(treeOf(t, parent), before) {
			t.Errorf("planning changed the parent")
		}
	})

	// Writable by others: never read, so its name is taken and its mailbox gets the next number.
	if runtime.GOOS != "windows" {
		open := t.TempDir()
		lf := archiveHolding(t, filepath.Join(open, "Local Folders"), idOf(by["Local Folders"]))
		if err := os.Chmod(lf, 0o777); err != nil {
			t.Fatal(err)
		}
		p, err = planSet(open, rows, "darwin")
		if err != nil || outcomesOf(p)[5] != "Local Folders: new Local Folders (2)" {
			t.Errorf("a folder writable by others is never written into: %v %q", err, outcomesOf(p))
		}
	}

	// Job files, a paused archive, an unfinished folder.
	jobs := t.TempDir()
	student := archiveHolding(t, filepath.Join(jobs, "s"), idOf(by["qz.student@mail.qzuni.example"]))
	setJobFile(t, student, job.Job{Auto: true, Inputs: []string{by["qz.student@mail.qzuni.example"].path}, Mode: "incremental"}, nil)
	walker := archiveHolding(t, filepath.Join(jobs, "w"), idOf(by["qzsam.walker@gmail.com"]))
	if err := seal.Seal(walker, "test"); err != nil {
		t.Fatal(err)
	}
	work := archiveHolding(t, filepath.Join(jobs, "k"), idOf(by["qzsam.work@gmail.com"]))
	setJobFile(t, work, job.Job{}, []byte("{not a job"))
	qzname := archiveHolding(t, filepath.Join(jobs, "n"), idOf(by["qzsam@qzname.example"]))
	setJobFile(t, qzname, job.Job{Inputs: []string{by["qzsam@qzname.example"].path}, Mode: "incremental"}, nil)
	for _, d := range []string{"qzwork@qzwork.example", "Local Folders"} {
		if err := os.Mkdir(filepath.Join(jobs, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	mf, _ := job.MadeListFile()
	if err := job.RecordMade(mf, filepath.Join(jobs, "qzwork@qzwork.example"), "qzwork@qzwork.example"); err != nil {
		t.Fatal(err)
	}
	p, err = planSet(jobs, rows, "darwin")
	want = []string{
		"qz.student@mail.qzuni.example: new qz.student@mail.qzuni.example",
		"qzsam.walker@gmail.com: paused w",
		"qzsam.work@gmail.com: continue k (job unreadable)",
		"qzsam@qzname.example: continue n",
		"qzwork@qzwork.example: unfinished qzwork@qzwork.example",
		"Local Folders: new Local Folders (2)",
	}
	if err != nil || !reflect.DeepEqual(outcomesOf(p), want) {
		t.Fatalf("job files, paused, unfinished:%v\n got %q\nwant %q", err, outcomesOf(p), want)
	}
	assure.Reached(t, p.items[4].ask, "an unfinished folder of an IMAP account is asked about its download")
}

// covers: MA-1494
// A folder already there, continued: a child that cannot be read now is listed, and while it is
// there a mailbox with no matching archive gets no new archive (F-07), while one whose archive is
// found is still continued. A saved-once row (a folder no account uses now) already kept is left as
// it is, checked before "its archive" (F-05): with Pause advice when that archive holds exactly its
// one store, none in a combined archive. A folder whose delete did not finish is not continued, with
// the delete's words. A folder owned by someone else is never read nor written into. More than 500
// folders: the others are left out and the plan says so. Positive twin first: the same parent,
// readable, plans a new archive for each mailbox it does not hold.
func TestASetsPlanLeavesWhatItCannotReadOrMustNotTouch(t *testing.T) {
	ownSettings(t)
	rows, by := setFixture(t, pickStore{"ImapMail/imap.old.example", 3000})
	old := by["imap.old.example (not in Thunderbird now)"]
	if !old.once || !old.make {
		t.Fatalf("the fixture's leftover must be saved once: %+v", old)
	}
	parent := t.TempDir()
	archiveHolding(t, filepath.Join(parent, "qzsam.walker@gmail.com"), idOf(by["qzsam.walker@gmail.com"]))
	shut := filepath.Join(parent, "shut")
	if err := os.Mkdir(shut, 0o700); err != nil {
		t.Fatal(err)
	}
	p, err := planSet(parent, rows, "darwin")
	if err != nil || p.unread != nil || p.items[0].outcome != outNew || p.items[1].outcome != outContinue {
		t.Fatalf("positive twin: readable: %v %q", err, outcomesOf(p))
	}
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		if err := os.Chmod(shut, 0); err != nil {
			t.Fatal(err)
		}
		// Someone else's folder that this user cannot open is never read, so it holds nothing back
		// (the user stood in, as fault injection).
		mine := setUID
		setUID = func() int { return os.Geteuid() + 1 }
		p, err = planSet(parent, rows, "darwin")
		setUID = mine
		if err != nil || p.unread != nil || p.items[0].outcome != outNew {
			t.Errorf("someone else's folder is not cannot-read-now: %v %q %q", err, p.unread, outcomesOf(p))
		}
		p, err = planSet(parent, rows, "darwin")
		os.Chmod(shut, 0o700)
		got := outcomesOf(p)
		if err != nil || !reflect.DeepEqual(p.unread, []string{"shut"}) || got[1] != "qzsam.walker@gmail.com: continue qzsam.walker@gmail.com" {
			t.Fatalf("a child that cannot be read now: %v %q %q", err, p.unread, got)
		}
		for i, g := range got {
			if i != 1 && g != p.items[i].row.name+": not made, unread" {
				t.Errorf("no new archive while a child cannot be read: %q", g)
			}
		}
	}

	// Saved once, already kept: exactly its store (Pause advice), or in a combined archive (none).
	kept := t.TempDir()
	archiveHolding(t, filepath.Join(kept, "Old account"), idOf(old))
	archiveHolding(t, filepath.Join(kept, "everything"), idOf(old), idOf(by["Local Folders"]))
	p, err = planSet(kept, rows, "darwin")
	if err != nil || outcomesOf(p)[6] != "imap.old.example (not in Thunderbird now): kept Old account exact=true" {
		t.Errorf("a leftover's own archive is kept as it is, before its-archive: %v %q", err, outcomesOf(p))
	}
	combined := t.TempDir()
	archiveHolding(t, filepath.Join(combined, "everything"), idOf(old), idOf(by["Local Folders"]))
	p, err = planSet(combined, rows, "darwin")
	if err != nil || outcomesOf(p)[6] != "imap.old.example (not in Thunderbird now): kept everything exact=false" || p.items[5].outcome != outNew {
		t.Errorf("a leftover kept in a combined archive, no Pause advice; Local Folders gets its own: %v %q", err, outcomesOf(p))
	}

	// A folder whose delete did not finish.
	del := t.TempDir()
	wd := archiveHolding(t, filepath.Join(del, "qzsam.walker@gmail.com"), idOf(by["qzsam.walker@gmail.com"]))
	ma1048Plant(t, wd)
	p, err = planSet(del, rows, "darwin")
	it := p.items[1]
	code := 0
	if it.outcome == outNotMade && it.why == notMadeDelete {
		code = 1
	}
	assure.Refused(t, code, it.delWhy, assure.Code(1), assure.Names("Delete not finished", "To finish it, click Delete", wd))

	// Owned by someone else (the user the wizard runs as stood in, as fault injection).
	if runtime.GOOS != "windows" {
		mine := setUID
		setUID = func() int { return os.Geteuid() + 1 }
		p, err = planSet(del, rows, "darwin")
		setUID = mine
		if err != nil || outcomesOf(p)[1] != "qzsam.walker@gmail.com: new qzsam.walker@gmail.com (2)" {
			t.Errorf("a folder owned by someone else is never read nor written into: %v %q", err, outcomesOf(p))
		}
	}

	// More than 500 folders: the first 500 by name are looked at.
	many := t.TempDir()
	for i := 0; i <= setChildMax; i++ {
		if err := os.Mkdir(filepath.Join(many, fmt.Sprintf("f%04d", i)), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	p, err = planSet(many, rows, "darwin")
	if err != nil || !p.more {
		t.Errorf("more than 500 folders: %v more=%v", err, p.more)
	}
}
