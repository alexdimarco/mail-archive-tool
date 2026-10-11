package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/lockfile"
)

// covers: MA-510, R29, R5, R20, S25
// A capture that is still running when the export finds a record's file rewritten (E4-AI2): the
// exporter writes a record's files at once but saves the manifest only at its checkpoints, so for a
// while the manifest still has the old checksum. The export is told apart by the archive's lock, which a
// capture holds: with it held, a file that does not match its recorded checksum, or one that is missing
// (a re-capture removes the zip before it writes the page again), is "changed in the archive during
// export" and retryable, where it used to be "modified or damaged: run Verify" or "removed from the
// archive after you selected it", both terminal and so not offered again by "Export failed messages
// again". The positive twins: with no capture running, the same files are told as before.
func TestArchiveExportBesideARunningCaptureIsChangedNotDamaged(t *testing.T) {
	for _, c := range []struct {
		name      string
		capturing bool
		damage    func(t *testing.T, a *ceArchive)
		class     string
		reason    string
	}{
		{"a file that no longer matches, no capture", false, flipEML, "terminal", "doesn't match the checksum the archive recorded"},
		{"a file that no longer matches, a capture holds the lock", true, flipEML, "retryable", "changed in the archive during export"},
		{"a file that is gone, no capture", false, removeEML, "terminal", "removed from the archive after you selected it"},
		{"a file that is gone, a capture holds the lock", true, removeEML, "retryable", "changed in the archive during export"},
	} {
		t.Run(c.name, func(t *testing.T) {
			a := buildCEArchive(t)
			os.Remove(filepath.Join(a.dir, lockfile.Name)) // the captures that built it are over
			e := newCEEnv(t)
			s := a.open()
			hello, second := a.key("hello"), a.key("second")
			p := a.plan(e, s, []string{hello, second})
			if c.capturing {
				lk, err := lockfile.AcquireAs(filepath.Join(a.dir, lockfile.Name), "graph")
				if err != nil {
					t.Fatal(err)
				}
				defer lk.Release()
			}
			c.damage(t, a)
			res := a.run(e, p, s)
			man := manifestOf(t, res.Root)
			f := failureListed(man, hello)
			if f == nil || f["class"] != c.class || !strings.Contains(f["reason"].(string), c.reason) {
				t.Fatalf("hello's failure = %v, want class %s and %q", f, c.class, c.reason)
			}
			if entryOf(t, man, second)["status"] != "exported" {
				t.Errorf("the message beside it was not exported: %v", entryOf(t, man, second))
			}
			in, err := e.RetryInput(res.RunID, "failed", "")
			if c.class == "retryable" && (err != nil || len(in.Messages) != 1 || in.Messages[0].SourceID != hello) {
				t.Errorf("Export failed messages again after a change under a capture: %v %v, want the message offered", in, err)
			}
			if c.class == "terminal" && err == nil {
				t.Errorf("a terminal failure was offered again: %v", in)
			}
			if _, lerr := os.Lstat(filepath.Join(a.dir, lockfile.Name)); c.capturing == false && lerr == nil {
				t.Errorf("the export made %s: an archive export never creates the lock", lockfile.Name)
			}
		})
	}
}

func flipEML(t *testing.T, a *ceArchive) {
	t.Helper()
	_, eml, _ := a.files("hello")
	b := a.read(eml)
	b[len(b)-3] ^= 0x20
	if err := os.WriteFile(eml, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func removeEML(t *testing.T, a *ceArchive) {
	t.Helper()
	_, eml, _ := a.files("hello")
	if err := os.Remove(eml); err != nil {
		t.Fatal(err)
	}
}
