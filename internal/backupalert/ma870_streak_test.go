package backupalert

import (
	"bytes"
	"errors"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/alert"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/uninstall"
)

// streakRig is a settings file with the alert options given, a recording sender (a queued
// error is a send that fails), and one archive's key.
type streakRig struct {
	t        *testing.T
	settings string
	key      string
	sent     []alert.Event
	fail     []error
}

const alertsOnFailure = `{"alertEnabled":true,"alertOnFailure":true,"alertOnPause":true,"alertHost":"smtp.example.org","alertPort":587,"alertFrom":"b@example.org","alertTo":"t@example.org"}`

func newStreakRig(t *testing.T, alertJSON string) *streakRig {
	t.Helper()
	g := &streakRig{t: t, settings: filepath.Join(t.TempDir(), "desktop-settings.json"), key: KeyFor(filepath.Join(t.TempDir(), "archive"))}
	if err := os.WriteFile(g.settings, []byte(alertJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	restore := SetSenderForTest(func(_ alert.Config, _ string, ev alert.Event) error {
		if len(g.fail) > 0 {
			e := g.fail[0]
			g.fail = g.fail[1:]
			if e != nil {
				return e
			}
		}
		g.sent = append(g.sent, ev)
		return nil
	})
	t.Cleanup(restore)
	return g
}

func (g *streakRig) report(result string) Outcome {
	var err error
	if result == ResultFailed {
		err = errors.New("the backup drive is not there")
	}
	return Report(g.settings, nil, g.key, "/archive", result, err, 0, nil)
}

func (g *streakRig) results() string {
	var r []string
	for _, ev := range g.sent {
		r = append(r, ev.Result)
	}
	return strings.Join(r, ",")
}

func (g *streakRig) path() string { return StreakPath(g.settings, g.key) }

// covers: MA-870, R37
// Two failed runs in a row send one email; the first success after them sends one recovery email
// ("working again", under the failure option) and ends the streak, and a later failure sends
// again. Positive twin first: the first failure sends. The Desktop notice follows the same
// streak: Notify on the first failure only, Recovered on the first success after.
func TestTwoFailuresSendOneEmailAndTheFirstSuccessOneRecovery(t *testing.T) {
	g := newStreakRig(t, alertsOnFailure)
	if o := g.report(ResultFailed); !o.Notify || g.results() != "failed" {
		t.Fatalf("the first failure: notify=%v sent %q", o.Notify, g.results())
	}
	if o := g.report(ResultFailed); o.Notify || g.results() != "failed" {
		t.Fatalf("the second failure in a row: notify=%v sent %q, want one email", o.Notify, g.results())
	}
	if o := g.report(ResultOK); !o.Recovered || g.results() != "failed,recovered" {
		t.Fatalf("the first success after: recovered=%v sent %q", o.Recovered, g.results())
	}
	if o := g.report(ResultOK); o.Recovered || g.results() != "failed,recovered" {
		t.Fatalf("a second success sends nothing (success emails off): sent %q", g.results())
	}
	if _, err := os.Lstat(g.path()); !os.IsNotExist(err) {
		t.Fatalf("the success left the alert record behind: %v", err)
	}
	if o := g.report(ResultFailed); !o.Notify || g.results() != "failed,recovered,failed" {
		t.Fatalf("a failure after the recovery sends again: sent %q", g.results())
	}
	msg := string((alert.Config{From: "b@example.org", To: "t@example.org"}).Message(alert.Event{Result: ResultRecovered, Archive: "/archive"}))
	if !strings.Contains(msg, "Subject: MailArchive backups are working again: /archive") || !strings.Contains(msg, "WORKING AGAIN") {
		t.Errorf("the recovery email does not say backups are working again:\n%s", msg)
	}
}

// covers: MA-870, R37
// The streak is kept with success emails off (the default) and with alerts off altogether: fail,
// succeed, fail sends two failure emails; with alerts off nothing is sent but the streak is still
// kept, so the Desktop notice goes once per failing streak and once on recovery.
func TestTheStreakIsKeptWithSuccessEmailsOffAndWithAlertsOff(t *testing.T) {
	g := newStreakRig(t, alertsOnFailure)
	g.report(ResultFailed)
	g.report(ResultOK)
	g.report(ResultFailed)
	if n := strings.Count(g.results(), "failed"); n != 2 {
		t.Fatalf("fail, succeed, fail with success emails off sent %q, want two failure emails", g.results())
	}

	off := newStreakRig(t, `{"alertEnabled":false}`)
	if o := off.report(ResultFailed); !o.Notify {
		t.Fatal("alerts off: the first failure must still raise the notice")
	}
	if o := off.report(ResultFailed); o.Notify {
		t.Fatal("alerts off: the second failure in a row raised a second notice")
	}
	if o := off.report(ResultOK); !o.Recovered {
		t.Fatal("alerts off: the success after a failing streak is not a recovery")
	}
	if len(off.sent) != 0 {
		t.Fatalf("alerts off sent %q", off.results())
	}
}

// covers: MA-870, R37
// A send that fails on the first failed run does not mark the streak failing, so the second
// failed run tries again and is delivered; the third sends nothing. The Desktop notice went with
// the first failure and is not raised again (MA-879; amended 2026-10-28, owner-accepted: until
// then the retry raised the notice again).
func TestASendFailureOnTheFirstFailedRunIsTriedAgain(t *testing.T) {
	g := newStreakRig(t, alertsOnFailure)
	g.fail = []error{errors.New("the mail server did not answer")}
	var buf bytes.Buffer
	o := Report(g.settings, nil, g.key, "/archive", ResultFailed, errors.New("x"), 0, log.New(&buf, "", 0))
	if !o.Notify || len(g.sent) != 0 || !strings.Contains(buf.String(), "could not be sent") {
		t.Fatalf("the first failure's send failed: notify=%v sent %q, log %q", o.Notify, g.results(), buf.String())
	}
	if o := g.report(ResultFailed); o.Notify || g.results() != "failed" {
		t.Fatalf("the second failure must try the email again and be delivered, with no second notice: notify=%v sent %q", o.Notify, g.results())
	}
	if g.report(ResultFailed); g.results() != "failed" {
		t.Fatalf("the third failure sent again: %q", g.results())
	}
}

// covers: MA-870, R37
// Remove clears the streak, so remove, re-install, then fail sends; auto-pause clears it too, and
// its email goes whatever the streak said.
func TestRemoveAndAutoPauseClearTheStreak(t *testing.T) {
	g := newStreakRig(t, alertsOnFailure)
	g.report(ResultFailed)
	if err := ClearStreak(g.settings, g.key); err != nil {
		t.Fatal(err)
	}
	if g.report(ResultFailed); g.results() != "failed,failed" {
		t.Fatalf("remove, re-install, then fail sent %q, want a second failure email", g.results())
	}
	RecordAutoPause(g.settings, nil, g.key, "/archive", errors.New("unreachable for too long"), nil)
	if g.results() != "failed,failed,paused" {
		t.Fatalf("the auto-pause email did not go: %q", g.results())
	}
	if _, err := os.Lstat(g.path()); !os.IsNotExist(err) {
		t.Fatal("auto-pause left the failing streak behind")
	}
}

// covers: MA-870, R37
// An alert record that can't be read fails loud: the failure sends and the record is rewritten,
// whole and owner-only. One that can't be written at all leaves every failure sending, and the
// log says why.
func TestAnUnreadableAlertRecordSendsAndIsRewritten(t *testing.T) {
	g := newStreakRig(t, alertsOnFailure)
	if err := os.WriteFile(g.path(), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if g.report(ResultFailed); g.results() != "failed" {
		t.Fatalf("a damaged record must send: %q", g.results())
	}
	r, err := readStreak(g.path())
	if err != nil || !r.Failing {
		t.Fatalf("the damaged record was not rewritten: %+v %v", r, err)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(g.path()); fi.Mode().Perm() != 0o600 {
			t.Errorf("the rewritten record is %v, want 0600", fi.Mode().Perm())
		}
	}

	w := newStreakRig(t, alertsOnFailure)
	if err := os.MkdirAll(filepath.Join(w.path(), "in-the-way"), 0o700); err != nil { // a folder where the record goes
		t.Fatal(err)
	}
	var buf bytes.Buffer
	for i := 0; i < 2; i++ {
		Report(w.settings, nil, w.key, "/archive", ResultFailed, errors.New("x"), 0, log.New(&buf, "", 0))
	}
	if w.results() != "failed,failed" || !strings.Contains(buf.String(), "could not be written") {
		t.Fatalf("an unwritable record: sent %q, log %q", w.results(), buf.String())
	}
}

// covers: MA-870, R37
// The key is the one Install and Remove compute for a Desktop- or wizard-made schedule: the
// archive folder's DefaultNameFor; a run whose job file can't be read keys by the job file's own
// name, which for those jobs is the same key (rev4.1). A name that is no schedule name keys
// nothing. The record sits beside the settings file, is never named .json (the job folder's scan
// reads every .json there as a job), and uninstall removes it.
func TestTheStreakKeyIsTheSchedulesNameAndUninstallRemovesIt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(os.Getenv("HOME"), ".config"))
	t.Setenv("AppData", filepath.Join(os.Getenv("HOME"), "AppData"))
	out := filepath.Join(t.TempDir(), "archive")
	if KeyFor(out) != schedule.DefaultNameFor(out) || KeyFor("") != "" {
		t.Fatalf("KeyFor(%s) = %q", out, KeyFor(out))
	}
	jp, err := job.PathFor(schedule.DefaultNameFor(out))
	if err != nil {
		t.Fatal(err)
	}
	if KeyForJobFile(jp) != KeyFor(out) {
		t.Fatalf("the job file's key %q is not the archive's %q", KeyForJobFile(jp), KeyFor(out))
	}
	if KeyForJobFile(filepath.Join(t.TempDir(), "../..;rm.json")) != "" {
		t.Error("a job file named outside the schedule names keyed a record")
	}
	settings := filepath.Join(t.TempDir(), "desktop-settings.json")
	p := StreakPath(settings, KeyFor(out))
	if filepath.Dir(p) != filepath.Dir(settings) || strings.HasSuffix(p, ".json") || !strings.HasSuffix(p, StreakSuffix) {
		t.Fatalf("the record is at %s", p)
	}
	if err := writeStreak(p, streakRecord{Failing: true}); err != nil {
		t.Fatal(err)
	}
	targets, err := uninstall.ConfigTargets(filepath.Dir(settings), nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tg := range targets {
		found = found || tg == p
	}
	if !found {
		t.Errorf("uninstall does not remove the alert record %s (targets %v)", p, targets)
	}
}
