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
	"time"

	"mail-archive-tool/internal/alert"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/uninstall"
)

// syncRig is streakRig with a plaintext archive folder whose sync streak the test plants
// (the archive's own record, a real file), keyed as the scheduled run keys it.
type syncRig struct {
	*streakRig
	out string
}

func newSyncRig(t *testing.T, alertJSON string) *syncRig {
	t.Helper()
	g := newStreakRig(t, alertJSON)
	out := filepath.Join(t.TempDir(), "archive")
	if err := os.MkdirAll(out, 0o700); err != nil {
		t.Fatal(err)
	}
	g.key = KeyFor(out)
	return &syncRig{streakRig: g, out: out}
}

func (g *syncRig) plant(s state.SyncStreak) {
	g.t.Helper()
	if err := state.WriteSyncStreak(g.out, s); err != nil {
		g.t.Fatal(err)
	}
}

func (g *syncRig) tell(deferred bool) {
	ReportSyncStale(g.settings, nil, g.key, g.out, deferred, nil)
}

func (g *syncRig) count() int { return strings.Count(g.results(), ResultSyncStale) }

// syncT0 is a plausible time (after state.MinPlausibleTime) the planted streaks start at.
var syncT0 = time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC)

// staleFrom is a streak of two missed scheduled runs four days apart, from t0: stale.
func staleFrom(s state.SyncStreak, t0 time.Time, name string) state.SyncStreak {
	return s.Miss(name, t0).Miss(name, t0.Add(4*24*time.Hour))
}

// covers: MA-910, S65
// The sync email goes once per streak (design-presync-attended §5.5, C-35): positive
// twin first, a stale streak sends one email; the next stale run of the same streak
// (same Serial) sends nothing, and the told-file holds that Serial. A streak that is not
// stale sends nothing. After Thunderbird syncs, the next stale streak (a new Serial)
// sends again.
func TestTheSyncEmailGoesOncePerStreak(t *testing.T) {
	g := newSyncRig(t, alertsOnFailure)
	s := staleFrom(state.SyncStreak{}, syncT0, "owner-active-skipped")
	g.plant(s)
	g.tell(false)
	if g.count() != 1 {
		t.Fatalf("positive twin: a stale streak sent %q, want one sync email", g.results())
	}
	told, err := readSyncAlert(SyncAlertPath(g.settings, g.key))
	if err != nil || told.ToldFor != s.Serial || s.Serial != 1 {
		t.Fatalf("the told-file: %+v (%v), want ToldFor %d", told, err, s.Serial)
	}
	g.plant(s.Miss("owner-active-skipped", syncT0.Add(5*24*time.Hour)))
	g.tell(false)
	if g.count() != 1 {
		t.Fatalf("the next stale run of the same streak sent again: %q", g.results())
	}

	next := staleFrom(s.Synced(syncT0.Add(6*24*time.Hour)), syncT0.Add(7*24*time.Hour), "app-not-found")
	if next.Serial != 2 {
		t.Fatalf("the next streak's serial = %d", next.Serial)
	}
	g.plant(next)
	g.tell(false)
	if g.count() != 2 {
		t.Fatalf("a new streak after a sync did not send: %q", g.results())
	}

	// Its own rig (and recording sender) last, since a new rig replaces the sender.
	fresh := newSyncRig(t, alertsOnFailure)
	fresh.plant(state.SyncStreak{}.Miss("owner-active-skipped", syncT0).Miss("owner-active-skipped", syncT0.Add(time.Hour)))
	fresh.tell(false)
	if len(fresh.sent) != 0 {
		t.Fatalf("a streak that is not stale sent %q", fresh.results())
	}
}

// covers: MA-910, S65
// The told-file names the sync record as well as the Serial (the gate-2 adversarial pass
// after AS5 and AS6): a record removed or damaged starts again at Serial 1 while the
// told-file stays, and its streak is a new one. Positive twin first: a stale streak of
// one record sends, and the told-file holds its Serial and RecordID; the same streak again
// sends nothing; a stale streak at the same Serial in a record named otherwise sends.
func TestTheToldFileNamesTheRecordToo(t *testing.T) {
	g := newSyncRig(t, alertsOnFailure)
	a := staleFrom(state.SyncStreak{}, syncT0, "owner-active-skipped")
	a.RecordID = "RecordA"
	g.plant(a)
	g.tell(false)
	if g.count() != 1 {
		t.Fatalf("positive twin: a stale streak sent %q", g.results())
	}
	told, err := readSyncAlert(SyncAlertPath(g.settings, g.key))
	if err != nil || told.ToldFor != 1 || told.ToldRecord != "RecordA" {
		t.Fatalf("the told-file: %+v (%v), want serial 1 of RecordA", told, err)
	}
	g.tell(false)
	if g.count() != 1 {
		t.Fatalf("the same streak sent again: %q", g.results())
	}

	b := staleFrom(state.SyncStreak{}, syncT0.Add(10*24*time.Hour), "app-not-found")
	b.RecordID = "RecordB"
	if b.Serial != a.Serial {
		t.Fatalf("the restarted record's serial is %d, want %d", b.Serial, a.Serial)
	}
	g.plant(b)
	g.tell(false)
	if g.count() != 2 {
		t.Fatalf("a restarted record's stale streak (same Serial) was read as told: %q", g.results())
	}
	g.tell(false)
	if g.count() != 2 {
		t.Fatalf("the restarted record's streak was told twice: %q", g.results())
	}
}

// covers: MA-910, S65
// With alerts off, or on without the failure option, nothing is sent and the streak is
// marked told at once (as Report does), so turning alerts on later in the same streak
// sends nothing for it; the next streak sends.
func TestWithAlertsOffTheSyncStreakIsMarkedToldAtOnce(t *testing.T) {
	for _, cfg := range []string{`{"alertEnabled":false}`, `{"alertEnabled":true,"alertOnFailure":false,"alertOnPause":true,"alertHost":"smtp.example.org","alertPort":587,"alertFrom":"b@example.org","alertTo":"t@example.org"}`} {
		g := newSyncRig(t, cfg)
		s := staleFrom(state.SyncStreak{}, syncT0, "owner-active-skipped")
		g.plant(s)
		g.tell(false)
		if len(g.sent) != 0 {
			t.Fatalf("%s: sent %q", cfg, g.results())
		}
		told, err := readSyncAlert(SyncAlertPath(g.settings, g.key))
		if err != nil || told.ToldFor != s.Serial {
			t.Fatalf("%s: the streak was not marked told: %+v (%v)", cfg, told, err)
		}
		if err := os.WriteFile(g.settings, []byte(alertsOnFailure), 0o600); err != nil {
			t.Fatal(err)
		}
		g.tell(false)
		if len(g.sent) != 0 {
			t.Fatalf("%s: alerts turned on in the same streak sent %q", cfg, g.results())
		}
		g.plant(staleFrom(s.Synced(syncT0.Add(5*24*time.Hour)), syncT0.Add(6*24*time.Hour), "owner-active-skipped"))
		g.tell(false)
		if g.count() != 1 {
			t.Fatalf("%s: the next streak did not send: %q", cfg, g.results())
		}
	}
}

// covers: MA-910, S65
// The told-file is written only after the email was delivered (C-35): a delivery that
// fails leaves the streak untold and logs why, so the next stale run tries again and is
// delivered; the run after sends nothing.
func TestAFailedSyncEmailIsTriedAgain(t *testing.T) {
	g := newSyncRig(t, alertsOnFailure)
	g.plant(staleFrom(state.SyncStreak{}, syncT0, "owner-active-skipped"))
	g.fail = []error{errors.New("the mail server did not answer")}
	var buf bytes.Buffer
	ReportSyncStale(g.settings, nil, g.key, g.out, false, log.New(&buf, "", 0))
	if len(g.sent) != 0 || !strings.Contains(buf.String(), "could not be sent") {
		t.Fatalf("the failed delivery: sent %q, log %q", g.results(), buf.String())
	}
	if _, err := os.Lstat(SyncAlertPath(g.settings, g.key)); !os.IsNotExist(err) {
		t.Fatalf("a failed delivery marked the streak told (%v)", err)
	}
	g.tell(false)
	if g.count() != 1 {
		t.Fatalf("the next stale run did not try again: %q", g.results())
	}
	g.tell(false)
	if g.count() != 1 {
		t.Fatalf("the run after the delivery sent again: %q", g.results())
	}
}

// covers: MA-910, S65
// One email per run (§5.5, §5.6): a run that sends the recovery email defers the sync
// email — nothing sent, nothing marked told — and the next stale run sends it. Report
// says when it sent the recovery email: after a failing streak whose email went, with the
// failure option on; never for a plain success, nor with alerts off.
func TestTheRecoveryEmailDefersTheSyncEmail(t *testing.T) {
	g := newSyncRig(t, alertsOnFailure)
	g.plant(staleFrom(state.SyncStreak{}, syncT0, "owner-active-skipped"))
	g.report(ResultFailed)
	o := g.report(ResultOK)
	if !o.RecoveryEmailed || g.results() != "failed,recovered" {
		t.Fatalf("positive twin: the recovery: %+v, sent %q", o, g.results())
	}
	g.tell(o.RecoveryEmailed)
	if g.results() != "failed,recovered" {
		t.Fatalf("the sync email went with the recovery email: %q", g.results())
	}
	if _, err := os.Lstat(SyncAlertPath(g.settings, g.key)); !os.IsNotExist(err) {
		t.Fatalf("a deferred sync email marked the streak told (%v)", err)
	}
	o = g.report(ResultOK)
	if o.RecoveryEmailed {
		t.Fatalf("a plain success says it sent the recovery email: %+v", o)
	}
	g.tell(o.RecoveryEmailed)
	if g.results() != "failed,recovered,"+ResultSyncStale {
		t.Fatalf("the next stale run did not send the deferred sync email: %q", g.results())
	}

	off := newSyncRig(t, `{"alertEnabled":false}`)
	off.report(ResultFailed)
	if o := off.report(ResultOK); !o.Recovered || o.RecoveryEmailed {
		t.Fatalf("alerts off: the recovery says it emailed: %+v", o)
	}
}

// covers: MA-910, S65
// A clock restart inside one streak sends no second email (C-35): the clock going back
// restarts FirstMissed but keeps Serial, so when the restarted streak is stale again it is
// the same streak, already told.
func TestAClockRestartInsideOneStreakSendsNoSecondEmail(t *testing.T) {
	g := newSyncRig(t, alertsOnFailure)
	s := staleFrom(state.SyncStreak{}, syncT0, "owner-active-skipped")
	g.plant(s)
	g.tell(false)
	if g.count() != 1 {
		t.Fatalf("positive twin: %q", g.results())
	}
	back := syncT0.Add(-10 * 24 * time.Hour) // the clock went back past the first miss
	s = s.Miss("owner-active-skipped", back)
	if s.Stale() || !s.FirstMissed.Equal(back) {
		t.Fatalf("the clock restart: %+v", s)
	}
	s = s.Miss("owner-active-skipped", back.Add(4*24*time.Hour))
	if !s.Stale() || s.Serial != 1 {
		t.Fatalf("the restarted streak: %+v", s)
	}
	g.plant(s)
	g.tell(false)
	if g.count() != 1 {
		t.Fatalf("a clock restart inside one streak sent a second email: %q", g.results())
	}
}

// covers: MA-910, S65
// A told-file that cannot be read fails loud (§5.5): the email sends and the file is
// rewritten, whole and owner-only, so the next run sends nothing. A streak that cannot be
// read sends nothing and logs why.
func TestAnUnreadableToldFileSendsAndIsRewritten(t *testing.T) {
	g := newSyncRig(t, alertsOnFailure)
	s := staleFrom(state.SyncStreak{}, syncT0, "owner-active-skipped")
	g.plant(s)
	p := SyncAlertPath(g.settings, g.key)
	if err := os.WriteFile(p, []byte(`{"told_for":`), 0o600); err != nil {
		t.Fatal(err)
	}
	g.tell(false)
	if g.count() != 1 {
		t.Fatalf("a damaged told-file must send: %q", g.results())
	}
	told, err := readSyncAlert(p)
	if err != nil || told.ToldFor != s.Serial {
		t.Fatalf("the damaged told-file was not rewritten: %+v (%v)", told, err)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
			t.Errorf("the rewritten told-file is %v, want 0600", fi.Mode().Perm())
		}
	}
	g.tell(false)
	if g.count() != 1 {
		t.Fatalf("the rewritten told-file did not hold: %q", g.results())
	}

	bad := newSyncRig(t, alertsOnFailure)
	if err := os.WriteFile(filepath.Join(bad.out, state.SyncStreakName), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" { // a record others can read is refused (ReadSecureFile)
		var buf bytes.Buffer
		ReportSyncStale(bad.settings, nil, bad.key, bad.out, false, log.New(&buf, "", 0))
		if len(bad.sent) != 0 || !strings.Contains(buf.String(), "Thunderbird sync record") {
			t.Fatalf("an unreadable streak: sent %q, log %q", bad.results(), buf.String())
		}
	}
}

// covers: MA-910, S65
// The words come only from the closed maps (C-37): the subject and body are §5.5's, with
// the date, the count, the phrase of presync.TagPhrase and the remedy of §5.4's table in the
// Desktop's words. A hostile last name — markup, a header break, a raw name — is never
// echoed: it reads as the generic phrase and the generic remedy, and adds no header.
func TestTheSyncEmailWordsComeFromTheClosedMaps(t *testing.T) {
	g := newSyncRig(t, alertsOnFailure)
	s := staleFrom(state.SyncStreak{}, syncT0, "not-set-up")
	g.plant(s)
	g.tell(false)
	if g.count() != 1 {
		t.Fatalf("positive twin: %q", g.results())
	}
	ev := g.sent[0]
	msg := string((alert.Config{From: "b@example.org", To: "t@example.org"}).Message(ev))
	date := syncT0.Local().Format("Jan 2, 2006")
	for _, want := range []string{
		"Subject: Mail Archive: Thunderbird not synced for 3 days — " + g.out + "\r\n",
		"The scheduled backups of " + g.out + " on ",
		" still ran, but Thunderbird has not been synced before them since " + date + " (2 backups in a row).",
		"Mail that had only reached the mail server, not this computer, may be missing from the archive.",
		"The last time: Thunderbird not synced: the sync could not be prepared.",
		"Do this: The sync could not be prepared on this computer. Click Repair on the Scheduled backup card.",
		"You will get this email once, until Thunderbird syncs again.",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("the sync email lacks %q:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "not-set-up") {
		t.Errorf("the sync email echoes the outcome's name:\n%s", msg)
	}

	hostile := newSyncRig(t, alertsOnFailure)
	const evil = "<b>pwn</b>\r\nBcc: victim@example.org"
	hostile.plant(staleFrom(state.SyncStreak{}, syncT0, evil))
	hostile.tell(false)
	if hostile.count() != 1 {
		t.Fatalf("a hostile streak sent %q", hostile.results())
	}
	hmsg := string((alert.Config{From: "b@example.org", To: "t@example.org"}).Message(hostile.sent[0]))
	for _, bad := range []string{"<b>", "pwn", "Bcc:", "victim@"} {
		if strings.Contains(hmsg, bad) {
			t.Errorf("the hostile name reached the email (%q):\n%s", bad, hmsg)
		}
	}
	if !strings.Contains(hmsg, "The last time: Thunderbird not synced.") {
		t.Errorf("a hostile name does not read as the generic phrase:\n%s", hmsg)
	}
	if !strings.Contains(hmsg, "Do this: To back up the mail now, open Thunderbird") && !strings.Contains(hmsg, "Do this: Click Run now on the Scheduled backup card to sync while you watch.") {
		t.Errorf("a hostile name does not get the generic remedy:\n%s", hmsg)
	}
}

// covers: MA-910, S65
// The told-file sits beside the settings file as <key>.sync-alert, never .json; a
// schedule's create and Remove (ClearStreak) remove it with the alert streak (C-40); and
// uninstall removes it (its suffix and this package's agree, checked by behaviour).
func TestTheSyncAlertFileIsClearedAndUninstalled(t *testing.T) {
	g := newSyncRig(t, alertsOnFailure)
	g.plant(staleFrom(state.SyncStreak{}, syncT0, "owner-active-skipped"))
	g.tell(false)
	p := SyncAlertPath(g.settings, g.key)
	if filepath.Dir(p) != filepath.Dir(g.settings) || strings.HasSuffix(p, ".json") || !strings.HasSuffix(p, SyncAlertSuffix) {
		t.Fatalf("the told-file is at %s", p)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("positive twin: no told-file (%v)", err)
	}
	targets, err := uninstall.ConfigTargets(filepath.Dir(g.settings), nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tg := range targets {
		found = found || tg == p
	}
	if !found {
		t.Errorf("uninstall does not remove the told-file %s (targets %v)", p, targets)
	}
	if err := ClearStreak(g.settings, g.key); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(p); !os.IsNotExist(err) {
		t.Fatalf("ClearStreak left the told-file (%v)", err)
	}
	g.tell(false)
	if g.count() != 2 {
		t.Fatalf("after Remove and a re-create, the stale streak did not send: %q", g.results())
	}
}
