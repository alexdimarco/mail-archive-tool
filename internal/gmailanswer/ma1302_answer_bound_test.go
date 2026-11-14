package gmailanswer

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/state"
)

// covers: MA-1302, R45
// Every answer kept on this computer names the archive it was given for (the B3 adversarial
// pass's A1, turned from its probe TestB3AdvAnswerHasNoArchiveIdentity). Positive twin first: the
// answer given for archive A reads back for A, from the answer file and from the job file. When
// another archive B (its own id) is put in the same folder, neither reads for B; an answer naming
// no archive is refused, with nothing written; a yes is spent once a backup that ended ok found
// its store switched on (Read leaves it out, Settle removes it and an answer left empty goes), and
// a Not now is left as it is. A job file written afresh carries the answer the one it replaces
// held (CarryInto, A7).
func TestAnAnswerIsReadOnlyForTheArchiveItWasGivenFor(t *testing.T) {
	home := isolate(t)
	out := filepath.Join(home, "archive")
	idA := backedUp(t, out)
	if err := KeepHere(out, Answer{Archive: idA, Yes: []string{"imap.gmail.com"}}); err != nil {
		t.Fatal(err)
	}
	if a := Read(out); a.Archive != idA || !reflect.DeepEqual(a.Yes, []string{"imap.gmail.com"}) {
		t.Fatalf("positive twin: A's yes reads back as %+v", a)
	}
	jp, _ := job.PathFor(schedule.DefaultNameFor(out))
	if err := job.Write(jp, job.Job{Name: "a", Out: out, Mode: "incremental", GmailNotNow: true, GmailArchive: idA}); err != nil {
		t.Fatal(err)
	}
	if a := Read(out); !a.NotNow {
		t.Fatalf("positive twin: A's Not now in its job file reads back as %+v", a)
	}

	// Archive B in the same folder: neither answer is read for it.
	if err := os.RemoveAll(out); err != nil {
		t.Fatal(err)
	}
	idB := backedUp(t, out)
	if idB == idA {
		t.Fatal("two archives were given one id")
	}
	if a := Read(out); a.NotNow || len(a.Yes) != 0 || a.Archive != idB {
		t.Fatalf("the answers given for A are read for B: %+v", a)
	}

	// An answer naming no archive is refused, and nothing is written.
	p, _ := Path(out)
	before, _ := os.ReadFile(p)
	err := KeepHere(out, Answer{Yes: []string{"imap.gmail.com"}})
	msg, rc := "", 0
	if err != nil {
		msg = err.Error()
	}
	if errors.Is(err, ErrNoArchiveID) {
		rc = 1 // the typed refusal
	}
	assure.Refused(t, rc, msg, assure.Code(1), assure.Names("no record of a backup", "back it up once"),
		assure.NoSideEffect(func() bool { after, _ := os.ReadFile(p); return string(after) == string(before) }))

	// Spent: B's yes, once B's last backup found the store switched on.
	os.Remove(jp)
	if err := KeepHere(out, Answer{Archive: idB, Yes: []string{"imap.gmail.com", "second"}}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := state.WriteLastRun(out, state.LastRun{Status: state.RunOK, Started: now, Finished: now, GmailOn: []string{"imap.gmail.com"}}); err != nil {
		t.Fatal(err)
	}
	if state.ArchiveID(out) != idB {
		t.Fatal("a later record did not carry the archive's id")
	}
	if a := Read(out); !reflect.DeepEqual(a.Yes, []string{"second"}) {
		t.Fatalf("a spent yes still reads: %+v", a)
	}
	if err := Settle(out); err != nil {
		t.Fatal(err)
	}
	if f := readFile(p); !reflect.DeepEqual(f.Yes, []string{"second"}) || f.Archive != idB {
		t.Fatalf("Settle kept %+v", f)
	}
	if err := state.WriteLastRun(out, state.LastRun{Status: state.RunOK, Started: now, Finished: now, GmailOn: []string{"imap.gmail.com", "second"}}); err != nil {
		t.Fatal(err)
	}
	if err := Settle(out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(p); err == nil {
		t.Fatal("an answer left with nothing in it was not removed")
	}
	// A Not now is not spent.
	if err := KeepHere(out, Answer{Archive: idB, NotNow: true}); err != nil {
		t.Fatal(err)
	}
	if err := Settle(out); err != nil || !Read(out).NotNow {
		t.Fatalf("Settle took a Not now away (%v)", err)
	}

	// A job file written afresh carries the answer of the one it replaces (A7).
	if err := job.Write(jp, job.Job{Name: "a", Out: out, Mode: "incremental", GmailYes: []string{"x"}, GmailArchive: idB}); err != nil {
		t.Fatal(err)
	}
	fresh := job.Job{Name: "a", Out: out, Mode: "incremental"}
	CarryInto(out, &fresh)
	if fresh.GmailArchive != idB || !reflect.DeepEqual(fresh.GmailYes, []string{"x"}) {
		t.Fatalf("a job file written afresh dropped the answer: %+v", fresh)
	}
}

// covers: MA-1305, R45
// The answer file is found for the folder whatever its spelling, as this computer's file system
// tells folders apart (the B3 adversarial pass's A3, turned from its probe
// TestB3AdvAnswerKeyFollowsSpellingNotFolder): on macOS and Windows letter case is ignored, and a
// link to the folder finds the folder's answer. Positive twin first: the spelling the answer was
// kept under finds it, and a different folder does not.
func TestTheAnswerIsFoundForEverySpellingOfTheFolder(t *testing.T) {
	home := isolate(t)
	out := filepath.Join(home, "Archive")
	id := backedUp(t, out)
	if err := KeepHere(out, Answer{Archive: id, NotNow: true}); err != nil {
		t.Fatal(err)
	}
	if !Read(out).NotNow {
		t.Fatal("positive twin: the answer is not found under its own spelling")
	}
	if Read(filepath.Join(home, "elsewhere")).NotNow {
		t.Fatal("positive twin: another folder reads this folder's answer")
	}
	other := filepath.Join(home, "ARCHIVE")
	a, err1 := os.Stat(out)
	b, err2 := os.Stat(other)
	if err1 == nil && err2 == nil && os.SameFile(a, b) {
		if !Read(other).NotNow {
			t.Fatalf("the Not now kept for %s is not found for the same folder spelled %s", out, other)
		}
	}
	link := filepath.Join(home, "link")
	if err := os.Symlink(out, link); err == nil {
		if !Read(link).NotNow {
			t.Fatal("a link to the folder does not find the folder's answer")
		}
	}
}
