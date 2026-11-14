package gmailanswer

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
)

// isolate points this computer's settings folder at a throw-away home.
func isolate(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	return home
}

// covers: MA-1292, R45
// The Gmail answer is kept on this computer for every archive, never in the archive (the B3
// friction review's F2 and F7). Positive twin first: an archive with no answer anywhere reads as
// no answer. With no job file, a Desktop answer (Keep) and a terminal Not now (KeepHere) are kept
// in <key>.gmail-answer beside the job files (owner-only, not a .json name, nothing written into
// the archive) and read back. With a job file, a Desktop answer is kept in the job file as B3
// kept it and the answer file is removed, so the two never disagree; a terminal Not now kept in
// the answer file still counts beside a job file that holds no answer. A damaged answer file, or
// one that is a link, reads as no answer.
func TestTheGmailAnswerIsKeptOnThisComputerForEveryArchive(t *testing.T) {
	home := isolate(t)
	out := filepath.Join(home, "archive")
	if err := os.MkdirAll(out, 0o700); err != nil {
		t.Fatal(err)
	}
	if a := Read(out); a.NotNow || len(a.Yes) != 0 {
		t.Fatalf("positive twin: no answer kept, yet %+v", a)
	}
	p, err := Path(out)
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasSuffix(p, ".json") || filepath.Base(p) != schedule.DefaultNameFor(out)+Suffix {
		t.Fatalf("the answer file is %s", p)
	}

	// No job file: the Desktop's answer goes to the answer file.
	if err := Keep(out, Answer{Yes: []string{"gmail-store"}}); err != nil {
		t.Fatal(err)
	}
	if a := Read(out); a.NotNow || !reflect.DeepEqual(a.Yes, []string{"gmail-store"}) {
		t.Fatalf("the Desktop's yes with no job file reads back as %+v", a)
	}
	if fi, err := os.Stat(p); err != nil || (runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600) {
		t.Fatalf("the answer file: %v %v", fi, err)
	}
	if entries, _ := os.ReadDir(out); len(entries) != 0 {
		t.Fatalf("keeping the answer wrote into the archive: %v", entries)
	}
	// A terminal Not now.
	if err := KeepHere(out, Answer{NotNow: true}); err != nil {
		t.Fatal(err)
	}
	if a := Read(out); !a.NotNow {
		t.Fatalf("a terminal Not now reads back as %+v", a)
	}

	// A job file with no answer: the terminal's Not now still counts beside it.
	jp, _ := job.PathFor(schedule.DefaultNameFor(out))
	if err := job.Write(jp, job.Job{Name: "a", Out: out, Mode: "incremental"}); err != nil {
		t.Fatal(err)
	}
	if a := Read(out); !a.NotNow {
		t.Fatal("a Not now kept in the answer file is lost beside a job file")
	}
	// The Desktop's yes now goes to the job file, and the answer file goes.
	if err := Keep(out, Answer{Yes: []string{"gmail-store"}}); err != nil {
		t.Fatal(err)
	}
	if j, err := job.Read(jp); err != nil || j.GmailNotNow || !reflect.DeepEqual(j.GmailYes, []string{"gmail-store"}) {
		t.Fatalf("the job file holds %+v (%v)", j, err)
	}
	if _, err := os.Lstat(p); !os.IsNotExist(err) {
		t.Fatalf("the answer file is still there beside the job file's answer: %v", err)
	}
	if a := Read(out); a.NotNow || !reflect.DeepEqual(a.Yes, []string{"gmail-store"}) {
		t.Fatalf("after the Desktop's yes the answer reads %+v", a)
	}

	// A damaged answer file, and a link, read as no answer.
	other := filepath.Join(home, "other")
	op, _ := Path(other)
	if err := os.WriteFile(op, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if a := Read(other); a.NotNow || len(a.Yes) != 0 {
		t.Fatalf("a damaged answer file reads as %+v", a)
	}
	if runtime.GOOS != "windows" {
		target := filepath.Join(home, "target")
		os.WriteFile(target, []byte(`{"version":1,"not_now":true}`), 0o600)
		os.Remove(op)
		if err := os.Symlink(target, op); err != nil {
			t.Fatal(err)
		}
		if a := Read(other); a.NotNow {
			t.Fatal("an answer file that is a link was followed")
		}
	}
}
