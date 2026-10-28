package wizard

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ncruces/zenity"

	"mail-archive-tool/internal/alert"
	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/backupalert"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/state"
)

// localRunRig is a real local archive made from a real mbox, the alert configuration in a
// settings file of the test's own, a recording sender (a queued error is a send that fails)
// and a recording Desktop notice. No real schedule, notice or email; the home folder is the
// package's throwaway one.
type localRunRig struct {
	t        *testing.T
	dir      string
	out      string
	mbox     string
	settings string
	sent     []alert.Event
	sendErrs []error
	notices  []string
}

const localAlerts = `{"alertEnabled":true,"alertOnFailure":true,"alertOnPause":true,"alertHost":"smtp.example.org","alertPort":587,"alertFrom":"b@example.org","alertTo":"t@example.org"}`

func newLocalRunRig(t *testing.T) *localRunRig {
	t.Helper()
	g := &localRunRig{t: t, dir: t.TempDir()}
	oldNotify := notify
	notify = func(msg string, _ ...zenity.Option) error { g.notices = append(g.notices, msg); return nil }
	t.Cleanup(func() { notify = oldNotify })
	oldDir := failureDir
	fdir := t.TempDir()
	failureDir = func() (string, error) { return fdir, nil }
	t.Cleanup(func() { failureDir = oldDir })
	g.settings = filepath.Join(t.TempDir(), "desktop-settings.json")
	if err := os.WriteFile(g.settings, []byte(localAlerts), 0o600); err != nil {
		t.Fatal(err)
	}
	oldPath := backupalert.DefaultSettingsPath
	backupalert.DefaultSettingsPath = func() string { return g.settings }
	t.Cleanup(func() { backupalert.DefaultSettingsPath = oldPath })
	restore := backupalert.SetSenderForTest(func(_ alert.Config, _ string, ev alert.Event) error {
		if len(g.sendErrs) > 0 {
			e := g.sendErrs[0]
			g.sendErrs = g.sendErrs[1:]
			if e != nil {
				return e
			}
		}
		g.sent = append(g.sent, ev)
		return nil
	})
	t.Cleanup(restore)

	g.out = filepath.Join(g.dir, "archive")
	g.mbox = filepath.Join(g.dir, "Inbox")
	const msg = "From - Mon Jan 01 00:00:00 2024\r\nFrom: Bob <bob@example.com>\r\nTo: Me <me@example.com>\r\n" +
		"Subject: Hello\r\nDate: Mon, 01 Jan 2024 10:00:00 +0000\r\nMessage-ID: <abc@example.com>\r\n\r\nplain body\r\n"
	if err := os.WriteFile(g.mbox, []byte(msg), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := app.Run(context.Background(), app.Options{Inputs: []string{g.mbox}, Out: g.out, Mode: export.Incremental, Index: true, Pages: true}, log.New(io.Discard, "", 0), nil); err != nil {
		t.Fatal(err)
	}
	return g
}

// jobAt writes a job for this archive at path, reading inputs.
func (g *localRunRig) jobAt(path string, inputs ...string) string {
	g.t.Helper()
	if err := job.Write(path, job.Job{Name: jobFileName(path), Out: g.out, Mode: "incremental", Inputs: inputs}); err != nil {
		g.t.Fatal(err)
	}
	return path
}

func (g *localRunRig) results() string {
	var r []string
	for _, ev := range g.sent {
		r = append(r, ev.Result)
	}
	return strings.Join(r, ",")
}

// covers: MA-870, R37
// The local scheduled job (RunHeadless) keeps the alert streak with success emails off: two
// failed runs in a row send one email and raise one Desktop notice; the first success after sends
// one recovery email and one "working again" notice; fail, succeed, fail sends two failure
// emails. Positive twin first: the first failure sends and notifies.
func TestTheLocalJobEmailsAndNotifiesOncePerFailingStreak(t *testing.T) {
	g := newLocalRunRig(t)
	missing := filepath.Join(g.dir, "gone-Inbox")
	failJob := g.jobAt(filepath.Join(g.dir, "fail.json"), missing)
	okJob := g.jobAt(filepath.Join(g.dir, "ok.json"), g.mbox)

	if RunHeadless(failJob) != 1 || g.results() != "failed" || len(g.notices) != 1 {
		t.Fatalf("the first failure: sent %q, notices %v", g.results(), g.notices)
	}
	if RunHeadless(failJob) != 1 || g.results() != "failed" || len(g.notices) != 1 {
		t.Fatalf("the second failure in a row: sent %q, notices %v; want one of each", g.results(), g.notices)
	}
	if RunHeadless(okJob) != 0 || g.results() != "failed,recovered" || len(g.notices) != 2 || !strings.Contains(g.notices[1], "working again") {
		t.Fatalf("the first success after: sent %q, notices %v", g.results(), g.notices)
	}
	if RunHeadless(failJob) != 1 || strings.Count(g.results(), "failed") != 2 || len(g.notices) != 3 {
		t.Fatalf("fail, succeed, fail: sent %q, notices %v; want two failure emails", g.results(), g.notices)
	}
}

// covers: MA-870, R37
// A send that fails on the first failed run is tried again at the second, which is delivered;
// the third sends nothing.
func TestTheLocalJobTriesAnUndeliveredAlertAgain(t *testing.T) {
	g := newLocalRunRig(t)
	failJob := g.jobAt(filepath.Join(g.dir, "fail.json"), filepath.Join(g.dir, "gone-Inbox"))
	g.sendErrs = []error{io.ErrUnexpectedEOF}
	RunHeadless(failJob)
	if len(g.sent) != 0 {
		t.Fatalf("the first send failed, yet %q", g.results())
	}
	RunHeadless(failJob)
	RunHeadless(failJob)
	if g.results() != "failed" {
		t.Fatalf("after an undelivered first alert: sent %q, want one delivered failure", g.results())
	}
}

// covers: MA-870, R37
// Every early failure exit of RunHeadless goes through the streak: a job file that can't be read
// (keyed by its own name, which for a Desktop- or wizard-made job is the archive's key, so the
// run's later exits for the same archive add nothing to its streak), an archive folder that is
// not there (the drive unplugged: once, not every hour), a run log that can't open, and the
// Outlook app's export.
func TestEveryEarlyExitOfTheLocalJobGoesThroughTheStreak(t *testing.T) {
	g := newLocalRunRig(t)
	jp, err := job.PathFor(schedule.DefaultNameFor(g.out))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(jp) })
	if err := os.MkdirAll(filepath.Dir(jp), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(jp, []byte("{damaged"), 0o600); err != nil {
		t.Fatal(err)
	}
	if RunHeadless(jp) != 1 || g.results() != "failed" {
		t.Fatalf("an unreadable job file: sent %q", g.results())
	}
	if _, err := os.Stat(backupalert.StreakPath(g.settings, backupalert.KeyFor(g.out))); err != nil {
		t.Fatalf("the unreadable job file's streak is not the archive's: %v", err)
	}
	g.jobAt(jp, filepath.Join(g.dir, "gone-Inbox")) // the same archive's job, its source gone
	if RunHeadless(jp) != 1 || g.results() != "failed" {
		t.Fatalf("the same archive's next failure sent again (another key?): %q", g.results())
	}

	// The archive's own drive unplugged: the source streak inside it is never written, but the
	// alert streak (owner side) still sends once, not at every run.
	g.sent = nil
	gone := filepath.Join(g.dir, "unplugged", "archive")
	away := filepath.Join(g.dir, "away.json")
	if err := job.Write(away, job.Job{Name: "away", Out: gone, Mode: "incremental", Inputs: []string{g.mbox}}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		RunHeadless(away)
	}
	if g.results() != "failed" {
		t.Fatalf("three runs with the drive unplugged sent %q, want one", g.results())
	}

	// A run log that can't open.
	g.sent = nil
	logOut := filepath.Join(g.dir, "logless")
	if err := os.MkdirAll(filepath.Join(logOut, "mailarchive.log"), 0o755); err != nil {
		t.Fatal(err)
	}
	nolog := filepath.Join(g.dir, "nolog.json")
	if err := job.Write(nolog, job.Job{Name: "nolog", Out: logOut, Mode: "incremental", Inputs: []string{g.mbox}}); err != nil {
		t.Fatal(err)
	}
	if RunHeadless(nolog) != 1 || RunHeadless(nolog) != 1 || g.results() != "failed" {
		t.Fatalf("a run log that can't open: sent %q, want one", g.results())
	}

	// The Outlook app's export (it fails off Windows; on Windows it would drive a real Outlook).
	if runtime.GOOS != "windows" {
		g.sent = nil
		olOut := filepath.Join(g.dir, "outlook")
		if err := os.MkdirAll(olOut, 0o755); err != nil {
			t.Fatal(err)
		}
		ol := filepath.Join(g.dir, "outlook.json")
		if err := job.Write(ol, job.Job{Name: "outlook", Out: olOut, Mode: "incremental", Outlook: true}); err != nil {
			t.Fatal(err)
		}
		if RunHeadless(ol) != 1 || RunHeadless(ol) != 1 || g.results() != "failed" {
			t.Fatalf("the Outlook export's failure: sent %q, want one", g.results())
		}
	}
}

// covers: MA-871, R37
// RunHeadless auto-pauses like the capture: a run whose source is gone moves the archive's source
// streak (the refused run is recorded), and once the unreachable runs span 30 days the schedule's
// own bound entry is removed, the descriptor kept, the archive not sealed, the auto-pause email
// sent and the alert streak cleared. Positive twin first: spanning 29 days, nothing is removed.
func TestTheLocalJobAutoPausesLikeTheCapture(t *testing.T) {
	g := newLocalRunRig(t)
	name := schedule.DefaultNameFor(g.out)
	rec := recordRemovals(t, name) // the entry is installed
	if err := schedule.WriteDescriptor(g.out, schedule.Descriptor{Name: name, Interval: "hourly", At: "00:10", Job: []string{"mailarchive", "-out", g.out}}); err != nil {
		t.Fatal(err)
	}
	failJob := g.jobAt(filepath.Join(g.dir, "fail.json"), filepath.Join(g.dir, "gone-Inbox"))

	seed := func(daysAgo int) {
		then := time.Now().UTC().Add(-time.Duration(daysAgo) * 24 * time.Hour)
		if err := state.WriteSourceStreak(g.out, state.SourceStreak{ConsecutiveFails: 1, FirstUnreachable: then, LastUnreachable: then}); err != nil {
			t.Fatal(err)
		}
	}
	seed(29)
	RunHeadless(failJob)
	s, _ := state.ReadSourceStreak(g.out)
	if s.ConsecutiveFails != 2 || len(*rec) != 0 || strings.Contains(g.results(), "paused") {
		t.Fatalf("29 days: streak %+v, removed %v, sent %q", s, *rec, g.results())
	}

	seed(31)
	RunHeadless(failJob)
	if len(*rec) != 1 || (*rec)[0] != name {
		t.Fatalf("31 days: the local job did not auto-pause its own entry: removed %v", *rec)
	}
	if !strings.HasSuffix(g.results(), "paused") {
		t.Errorf("the auto-pause email did not go: %q", g.results())
	}
	if _, err := schedule.ReadDescriptor(g.out); err != nil {
		t.Error("auto-pause must keep the descriptor")
	}
	if _, err := os.Lstat(backupalert.StreakPath(g.settings, backupalert.KeyFor(g.out))); !os.IsNotExist(err) {
		t.Error("auto-pause left the alert streak behind")
	}
}

// covers: MA-859, R37
// A scheduled start while another holder has the archive lock, which started under 24 hours ago,
// exits quietly naming that holder: exit 0, no email, no notice, no alert streak, no run history
// entry. Past 24 hours it counts as a failure, alerted once. Positive twin first: with the lock
// free the job runs.
func TestAStartWhileAnotherHolderRunsSkipsQuietlyForADay(t *testing.T) {
	g := newLocalRunRig(t)
	okJob := g.jobAt(filepath.Join(g.dir, "ok.json"), g.mbox)
	if RunHeadless(okJob) != 0 {
		t.Fatal("the positive twin: the job did not run")
	}
	hist, _ := state.ReadRunHistory(g.out)
	lockPath := filepath.Join(g.out, lockfile.Name)
	held, err := lockfile.AcquireAs(lockPath, "verify")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { held.Release() })

	if rc := RunHeadless(okJob); rc != 0 {
		t.Fatalf("a start while a verify holds the archive exited %d, want 0 (quiet)", rc)
	}
	if len(g.sent) != 0 || len(g.notices) != 0 {
		t.Fatalf("the quiet skip sent %q and notices %v", g.results(), g.notices)
	}
	if _, err := os.Lstat(backupalert.StreakPath(g.settings, backupalert.KeyFor(g.out))); !os.IsNotExist(err) {
		t.Error("the quiet skip wrote an alert streak")
	}
	if h, _ := state.ReadRunHistory(g.out); len(h) != len(hist) {
		t.Errorf("the quiet skip added a run history entry: %d -> %d", len(hist), len(h))
	}
	logText, _ := os.ReadFile(filepath.Join(g.out, "mailarchive.log"))
	if !strings.Contains(string(logText), "skipped: a check of this archive (verify) started at") {
		t.Errorf("the skip's line does not name the holder:\n%s", logText)
	}

	if runtime.GOOS == "windows" {
		return // a held lock's bytes can't be rewritten by another handle there
	}
	old := time.Now().Add(-25 * time.Hour).UTC().Format(time.RFC3339)
	if err := os.WriteFile(lockPath, []byte("pid=1 started="+old+" host=h nonce=n verb=export\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if RunHeadless(okJob) != 1 || RunHeadless(okJob) != 1 {
		t.Fatal("a holder started over 24 hours ago must count as a failure")
	}
	if g.results() != "failed" || len(g.notices) != 1 {
		t.Fatalf("past 24 hours: sent %q, notices %v; want one of each", g.results(), g.notices)
	}
}

// covers: MA-859, R37
// The job's failure log (<name>.jobfail.log, beside the job file) stays under its cap: past it
// the log moves to <name>.older.jobfail.log, one older copy, and a new one starts.
func TestTheJobFailureLogStaysUnderItsCap(t *testing.T) {
	old := jobFailLogCap
	jobFailLogCap = 400
	t.Cleanup(func() { jobFailLogCap = old })
	fdir := t.TempDir()
	oldDir := failureDir
	failureDir = func() (string, error) { return fdir, nil }
	t.Cleanup(func() { failureDir = oldDir })
	dir := t.TempDir()
	path := filepath.Join(dir, "mailarchive-x.json")
	for i := 0; i < 60; i++ {
		jobFail(path, io.ErrUnexpectedEOF)
	}
	for _, name := range []string{"mailarchive-x.jobfail.log", "mailarchive-x.older.jobfail.log"} {
		fi, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if fi.Size() > jobFailLogCap {
			t.Errorf("%s is %d bytes, over its %d-byte cap", name, fi.Size(), jobFailLogCap)
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Errorf("the failure log kept more than one older copy: %v", entries)
	}
}
