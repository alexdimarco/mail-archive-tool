package wizard

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ncruces/zenity"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
)

// covers: MA-956, R36
// run-job (wizard.RunHeadless, the scheduled local backup) and the emptied-folder guard
// (design-archive-delete §7): a folder of the person's files that an archive was deleted
// from is refused before the run's log is opened into it — nothing is written there, the
// refusal goes to the job's failure log beside the job file. Positive twins first: a first
// run into an empty folder (its own run log is not the person's file), and into one holding
// only the descriptor, still work.
func TestMA956_RunJobRefusesEmptiedFolder(t *testing.T) {
	old := notify
	notify = func(string, ...zenity.Option) error { return nil }
	defer func() { notify = old }()
	oldDir := failureDir
	fdir := t.TempDir()
	failureDir = func() (string, error) { return fdir, nil }
	defer func() { failureDir = oldDir }()

	dir := t.TempDir()
	mbox := filepath.Join(dir, "Inbox")
	const msg = "From - Mon Jan 01 00:00:00 2024\r\nFrom: Bob <bob@example.com>\r\nSubject: Hello\r\n" +
		"Date: Mon, 01 Jan 2024 10:00:00 +0000\r\nMessage-ID: <abc@example.com>\r\n\r\nplain body\r\n"
	if err := os.WriteFile(mbox, []byte(msg), 0o600); err != nil {
		t.Fatal(err)
	}
	jobFor := func(name, out string) string {
		p := filepath.Join(dir, name+".json")
		if err := job.Write(p, job.Job{Name: name, Out: out, Mode: "incremental", Inputs: []string{mbox}}); err != nil {
			t.Fatal(err)
		}
		return p
	}
	folder := func(name string) string {
		p := filepath.Join(dir, name)
		if err := os.Mkdir(p, 0o700); err != nil {
			t.Fatal(err)
		}
		return p
	}
	manifest := ".mailarchive-manifest.json"

	// Positive twins.
	empty := folder("empty")
	if code := RunHeadless(jobFor("mailarchive-empty", empty)); code != 0 {
		t.Fatalf("a first scheduled run into an empty folder exit = %d, want 0", code)
	}
	assure.Reached(t, fileThere(filepath.Join(empty, manifest)), "the first run's archive")
	desc := folder("desc")
	if err := os.WriteFile(filepath.Join(desc, schedule.DescriptorName), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := RunHeadless(jobFor("mailarchive-desc", desc)); code != 0 {
		t.Fatalf("a first scheduled run into a folder holding only the descriptor exit = %d, want 0", code)
	}

	// The refusal.
	kept := folder("kept")
	mine := []byte("my own notes\n")
	if err := os.WriteFile(filepath.Join(kept, "notes.txt"), mine, 0o600); err != nil {
		t.Fatal(err)
	}
	jp := jobFor("mailarchive-kept", kept)
	code := RunHeadless(jp)
	failLog, _ := os.ReadFile(filepath.Join(dir, "mailarchive-kept.jobfail.log"))
	rc := 0
	if code != 0 {
		rc = 1
	}
	assure.Refused(t, rc, string(failLog), assure.Code(1),
		assure.Names(kept, "holds no MailArchive archive", "remove its scheduled backup"),
		assure.NoSideEffect(func() bool {
			entries, err := os.ReadDir(kept)
			if err != nil {
				return false
			}
			var names []string
			for _, e := range entries {
				names = append(names, e.Name())
			}
			got, _ := os.ReadFile(filepath.Join(kept, "notes.txt"))
			return slices.Equal(names, []string{"notes.txt"}) && bytes.Equal(got, mine)
		}))
	if strings.Contains(string(failLog), "notes.txt") {
		t.Error("the refusal must not name the person's files")
	}
}

func fileThere(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
