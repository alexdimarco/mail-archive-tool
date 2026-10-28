package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
)

// writeRunJobFor writes, in the job folder, the run-job file a wizard install for the folder
// spelled out would write (named for that spelling), and returns its path.
func writeRunJobFor(t *testing.T, out string) string {
	t.Helper()
	jp, err := job.PathFor(schedule.DefaultNameFor(out))
	if err != nil {
		t.Fatal(err)
	}
	if err := job.Write(jp, job.Job{Name: schedule.DefaultNameFor(out), Out: out, Mode: "incremental", Inputs: []string{filepath.Join(out, "Inbox")}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(jp) })
	return jp
}

// covers: MA-875, MA-874, R37
// The card's Remove deletes a run-job's job file only with the entry it names (the part A pass):
// a job file named for another spelling of this archive folder (here a link to it), whose entry
// is still installed, is kept, the entry left as it was, and the reply says the backup was not
// removed and how to remove it; so is one whose entry the scheduler can't answer about. Once that
// entry is gone (removed at the command line), Remove deletes the job file. The stand-in scheduler
// answers by entry name (Config.EntryState), so the decision a real computer makes — anything but
// a definite "not installed" keeps the file — is the one tested (the part A re-verification).
// Positive twin first: the job file named for this spelling goes with its entry.
func TestRemoveKeepsAJobFileWhoseEntryItDidNotRemove(t *testing.T) {
	isolateHome(t)
	target := filepath.Join(t.TempDir(), "Mail")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "mail-link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("this system can't make a link here: %v", err)
	}

	// The positive twin: the Desktop's own spelling.
	g := newA3Rig(t, link, "darwin")
	same := writeRunJobFor(t, link)
	g.fake.tasks[link] = true
	if body := postJSON(g.h, "/api/schedule-remove", g.csrf, `{}`).Body.String(); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("the positive twin: %s", body)
	}
	if _, err := os.Stat(same); !os.IsNotExist(err) || g.fake.tasks[link] {
		t.Fatalf("the positive twin: the entry and its job file did not go (job file err %v, tasks %v)", err, g.fake.tasks)
	}

	// The wizard installed for the other spelling: its job file is found by the folder scan.
	other := writeRunJobFor(t, target)
	g.fake.tasks = map[string]bool{target: true}
	g.fake.removed = nil
	if ev := schedule.BuildKindEvidence(link); ev.JobPath != other {
		t.Fatalf("fixture: the evidence does not find the other spelling's job file (%+v)", ev)
	}
	before := fileBytes(other)
	body := postJSON(g.h, "/api/schedule-remove", g.csrf, `{}`).Body.String()
	assure.Refused(t, refusedCode(body), said(body),
		assure.Names("was not removed", other, "mailarchive schedule -out "+link+" -name "+schedule.DefaultNameFor(target)+" -remove"),
		assure.NoSideEffect(func() bool { return string(fileBytes(other)) == string(before) && g.fake.tasks[target] }))

	// The scheduler can't answer about that entry (Windows' SchedulerUnavailable): the job file is
	// kept, as for an entry that is installed — only a definite "not installed" lets it go.
	g.fake.tasks = map[string]bool{}
	g.fake.unknown = true
	body = postJSON(g.h, "/api/schedule-remove", g.csrf, `{}`).Body.String()
	assure.Refused(t, refusedCode(body), said(body),
		assure.Names("was not removed", other),
		assure.NoSideEffect(func() bool { return string(fileBytes(other)) == string(before) }))
	g.fake.unknown = false

	// That entry removed at the command line: Remove now deletes the job file.
	if body := postJSON(g.h, "/api/schedule-remove", g.csrf, `{}`).Body.String(); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("with its entry gone: %s", body)
	}
	if _, err := os.Stat(other); !os.IsNotExist(err) {
		t.Errorf("with its entry gone, the job file was kept: %v", err)
	}
}
