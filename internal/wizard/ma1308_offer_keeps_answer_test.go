package wizard

import (
	"path/filepath"
	"reflect"
	"testing"

	"mail-archive-tool/internal/gmailanswer"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
)

// covers: MA-1308, R45
// The wizard's "Yes — every day" offer (installOffered, which the Desktop's Add another archive
// reaches) writes the archive's job file afresh from the wizard's answers; it keeps the Gmail
// answer the job file there held (the B3 adversarial pass's A7), so a Not now is not forgotten and
// a yes still waits. Positive twin first: an archive with no answer gets a job file with none.
func TestTheScheduleOfferKeepsTheGmailAnswer(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	countInstalls(t)
	exe := filepath.Join(t.TempDir(), "mailarchive")

	plain := filepath.Join(home, "plain")
	lastRunWithID(t, plain)
	if _, err := installOffered(wizardChoice{out: plain, auto: true}, schedule.Daily, "02:00", exe, ""); err != nil {
		t.Fatal(err)
	}
	pj, _ := job.PathFor(schedule.DefaultNameFor(plain))
	if j, err := job.Read(pj); err != nil || j.GmailNotNow || len(j.GmailYes) != 0 || j.GmailArchive != "" {
		t.Fatalf("positive twin: %+v (%v)", j, err)
	}

	out := filepath.Join(home, "gmail")
	id := lastRunWithID(t, out)
	jp, _ := job.PathFor(schedule.DefaultNameFor(out))
	if err := job.Write(jp, job.Job{Name: schedule.DefaultNameFor(out), Out: out, Mode: "incremental", GmailNotNow: true, GmailYes: []string{"imap.gmail.com"}, GmailArchive: id}); err != nil {
		t.Fatal(err)
	}
	if _, err := installOffered(wizardChoice{out: out, auto: true}, schedule.Daily, "02:00", exe, ""); err != nil {
		t.Fatal(err)
	}
	j, err := job.Read(jp)
	if err != nil || !j.GmailNotNow || !reflect.DeepEqual(j.GmailYes, []string{"imap.gmail.com"}) || j.GmailArchive != id || !j.Auto {
		t.Fatalf("the offer's job file dropped the Gmail answer: %+v (%v)", j, err)
	}
	if a := gmailanswer.Read(out); !a.NotNow || !reflect.DeepEqual(a.Yes, []string{"imap.gmail.com"}) {
		t.Fatalf("after the offer the answer reads %+v", a)
	}
}
