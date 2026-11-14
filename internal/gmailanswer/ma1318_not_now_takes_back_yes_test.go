package gmailanswer

import (
	"path/filepath"
	"reflect"
	"testing"

	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
)

// covers: MA-1318, R45
// Not now takes back a yes still waiting for the same archive (the B3 adversarial pass, round 2's
// G2): the person's latest answer is the one kept. A Desktop Not now (Keep), with or without a job
// file, keeps no yes beside it; a terminal Not now (NotNowHere) is kept in the answer file and takes
// the waiting yes out of the archive's job file too, so the next scheduled backup switches nothing.
// Positive twin first: a yes alone is kept and read back, and a Not now for another archive leaves
// this one's yes.
func TestNotNowTakesBackAWaitingYes(t *testing.T) {
	home := isolate(t)
	out := filepath.Join(home, "archive")
	id := backedUp(t, out)
	if err := Keep(out, Answer{Archive: id, Yes: []string{"imap.gmail.com"}}); err != nil {
		t.Fatal(err)
	}
	if a := Read(out); !reflect.DeepEqual(a.Yes, []string{"imap.gmail.com"}) || a.NotNow {
		t.Fatalf("positive twin: the yes reads back as %+v", a)
	}

	// The Desktop's Not now, answer file only (no job file).
	if err := Keep(out, Answer{Archive: id, NotNow: true, Yes: []string{"imap.gmail.com"}}); err != nil {
		t.Fatal(err)
	}
	if a := Read(out); !a.NotNow || len(a.Yes) != 0 {
		t.Fatalf("after Not now (no job file) the answer reads %+v: a yes still waits", a)
	}

	// The Desktop's Not now with a job file.
	jp, _ := job.PathFor(schedule.DefaultNameFor(out))
	if err := job.Write(jp, job.Job{Name: "a", Out: out, Mode: "incremental", GmailYes: []string{"imap.gmail.com"}, GmailArchive: id}); err != nil {
		t.Fatal(err)
	}
	if err := Keep(out, Answer{Archive: id, NotNow: true, Yes: []string{"imap.gmail.com"}}); err != nil {
		t.Fatal(err)
	}
	if j, _ := job.Read(jp); !j.GmailNotNow || len(j.GmailYes) != 0 {
		t.Fatalf("after Not now the job file keeps %+v", j)
	}

	// A terminal Not now: the answer file has the Not now, and the job file's yes is taken back.
	if err := job.Write(jp, job.Job{Name: "a", Out: out, Mode: "incremental", GmailYes: []string{"imap.gmail.com"}, GmailArchive: id}); err != nil {
		t.Fatal(err)
	}
	if err := NotNowHere(out, id); err != nil {
		t.Fatal(err)
	}
	if a := Read(out); !a.NotNow || len(a.Yes) != 0 {
		t.Fatalf("after a terminal Not now the answer reads %+v: the job file's yes still waits", a)
	}
	if j, _ := job.Read(jp); len(j.GmailYes) != 0 || j.Mode != "incremental" || j.Out != out {
		t.Fatalf("the terminal's Not now left the job file as %+v", j)
	}

	// A Not now for another archive (a yes kept for this one in the job file) takes nothing back.
	if err := job.Write(jp, job.Job{Name: "a", Out: out, Mode: "incremental", GmailYes: []string{"imap.gmail.com"}, GmailArchive: id}); err != nil {
		t.Fatal(err)
	}
	if err := NotNowHere(out, "0123456789abcdef0123456789abcdef"); err != nil {
		t.Fatal(err)
	}
	if j, _ := job.Read(jp); !reflect.DeepEqual(j.GmailYes, []string{"imap.gmail.com"}) || j.GmailArchive != id {
		t.Fatalf("a Not now for another archive took this one's yes: %+v", j)
	}
}
