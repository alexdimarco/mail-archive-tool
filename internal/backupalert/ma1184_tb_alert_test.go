package backupalert

import (
	"bytes"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/alert"
	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/vault"
)

// tbRig is streakRig with a plaintext archive folder holding a real behind-streak record of one
// account 3 days into its streak, and the notice a run would give for it.
type tbRig struct {
	*streakRig
	out    string
	notice state.TBNotice
}

func newTBRig(t *testing.T, alertJSON string, stopped bool) *tbRig {
	t.Helper()
	g := newStreakRig(t, alertJSON)
	out := filepath.Join(t.TempDir(), "archive")
	if err := os.MkdirAll(out, 0o700); err != nil {
		t.Fatal(err)
	}
	t0 := time.Date(2026, 11, 1, 2, 0, 0, 0, time.UTC)
	x := int64(500)
	if stopped {
		x = 100
	}
	s := state.TBStreak{}.StartRun(t0).Behind("k1", "imap.qzc5.example", 100, t0)
	if stopped { // stopped needs 3 backups without progress across 3 days (FALSE-01's rule)
		for _, h := range []time.Duration{25, 49} {
			s = s.StartRun(t0.Add(h*time.Hour)).Behind("k1", "imap.qzc5.example", x, t0.Add(h*time.Hour))
		}
	}
	s = s.StartRun(t0.Add(73*time.Hour)).Behind("k1", "imap.qzc5.example", x, t0.Add(73*time.Hour)).Named()
	if err := state.WriteTBStreak(out, s); err != nil {
		t.Fatal(err)
	}
	a, _ := s.Account("k1")
	if due, st := a.Due(); !due || st != stopped {
		t.Fatalf("the rig's streak is not due as asked: %+v", a)
	}
	n := state.TBNotice{Stopped: stopped, RecordID: s.RecordID, Store: "k1", Serial: a.Serial, Account: a.Account, OnDisk: x, Listed: 1000,
		FirstBehind: a.FirstBehind, LastBehind: a.LastBehind, LastGrew: a.LastGrew}
	return &tbRig{streakRig: g, out: out, notice: n}
}

func (g *tbRig) report(deferred bool, logger *log.Logger) bool {
	return ReportTBBehind(g.settings, nil, g.out, []state.TBNotice{g.notice}, deferred, logger)
}

func (g *tbRig) flags() (still, stopped bool) {
	g.t.Helper()
	s, err := state.ReadTBStreak(g.out)
	if err != nil {
		g.t.Fatal(err)
	}
	a, _ := s.Account("k1")
	return a.StillSent, a.StoppedSent
}

// covers: MA-1184, MA-1188
// The scheduled run's Thunderbird download email (slice C5): a due notice sends one email, from the
// state words, and only then is the streak's flag set; a delivery that fails sets nothing and logs
// why, so the next due run tries again; with alerts off, or on without the failure option, nothing
// is sent and the flag is set at once (as the sync email does), so turning alerts on later in the
// same streak sends nothing for it; a run that already sent an email (deferred) sends nothing and
// sets nothing. Positive twin first: the still-downloading email goes and its flag is set.
func TestTheThunderbirdDownloadEmailGoesThenIsMarked(t *testing.T) {
	g := newTBRig(t, alertsOnFailure, false)
	var buf bytes.Buffer
	if !g.report(false, log.New(&buf, "", 0)) || len(g.sent) != 1 || g.sent[0].Result != ResultTBBehind || g.sent[0].TB == nil {
		t.Fatalf("positive twin: sent %q", g.results())
	}
	e := state.TBNoticeEmail([]state.TBNotice{g.notice}, time.Now())
	if tb := g.sent[0].TB; tb.Stopped != e.Stopped || tb.Accounts != e.Accounts || strings.Join(tb.Lines, "|") != strings.Join(e.Lines, "|") ||
		strings.Join(tb.Remedy, "|") != strings.Join(e.Remedy, "|") || tb.Which != e.Which || tb.Which == "" || g.sent[0].Archive != g.out {
		t.Errorf("the email's words: %+v", tb)
	}
	if still, st := g.flags(); !still || st {
		t.Fatalf("after delivery the flags are still %v stopped %v", still, st)
	}
	if !strings.Contains(buf.String(), "still downloading email was sent to") {
		t.Errorf("the log: %q", buf.String())
	}

	// The stopped one sets both flags.
	s := newTBRig(t, alertsOnFailure, true)
	if !s.report(false, nil) || len(s.sent) != 1 || !s.sent[0].TB.Stopped {
		t.Fatalf("the stopped email: %+v", s.sent)
	}
	if still, st := s.flags(); !still || !st {
		t.Errorf("after the stopped email the flags are %v %v", still, st)
	}

	// A failed delivery: nothing marked, logged; the next due run is delivered.
	f := newTBRig(t, alertsOnFailure, false)
	f.fail = []error{errors.New("the mail server did not answer")}
	buf.Reset()
	if f.report(false, log.New(&buf, "", 0)) || len(f.sent) != 0 || !strings.Contains(buf.String(), "could not be sent") {
		t.Fatalf("a failed delivery: sent %q, log %q", f.results(), buf.String())
	}
	if still, _ := f.flags(); still {
		t.Fatal("a failed delivery marked the streak")
	}
	if !f.report(false, nil) || len(f.sent) != 1 {
		t.Fatalf("the next due run did not try again: %q", f.results())
	}

	// Deferred: this run already sent an email; nothing sent, nothing marked.
	d := newTBRig(t, alertsOnFailure, false)
	buf.Reset()
	if d.report(true, log.New(&buf, "", 0)) || len(d.sent) != 0 || !strings.Contains(buf.String(), "waits for the next backup") {
		t.Fatalf("deferred: sent %q, log %q", d.results(), buf.String())
	}
	if still, _ := d.flags(); still {
		t.Fatal("a deferred email marked the streak")
	}

	// Alerts off, or on without the failure option: nothing sent, marked at once.
	for _, cfg := range []string{`{"alertEnabled":false}`, `{"alertEnabled":true,"alertOnFailure":false,"alertOnPause":true,"alertHost":"smtp.example.org","alertPort":587,"alertFrom":"b@example.org","alertTo":"t@example.org"}`} {
		o := newTBRig(t, cfg, false)
		if o.report(false, nil) || len(o.sent) != 0 {
			t.Fatalf("%s: sent %q", cfg, o.results())
		}
		if still, _ := o.flags(); !still {
			t.Errorf("%s: the streak was not marked at once", cfg)
		}
	}
}

// covers: MA-1184
// The flag is set under the archive's lock: while another run holds it, the email still goes but
// nothing is marked, and the log says the next backup may send it again. No notice, nothing.
// Positive twin first: with the lock free, the flag is set.
func TestTheMarkWaitsForTheArchiveLock(t *testing.T) {
	g := newTBRig(t, alertsOnFailure, false)
	if !g.report(false, nil) {
		t.Fatal("positive twin: not sent")
	}
	if still, _ := g.flags(); !still {
		t.Fatal("positive twin: not marked")
	}
	h := newTBRig(t, alertsOnFailure, false)
	held, err := lockfile.AcquireAs(filepath.Join(h.out, lockfile.Name), "export")
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	sent := h.report(false, log.New(&buf, "", 0))
	held.Release()
	if !sent || len(h.sent) != 1 || !strings.Contains(buf.String(), "may send it again") {
		t.Fatalf("a held lock: sent %v %q, log %q", sent, h.results(), buf.String())
	}
	if still, _ := h.flags(); still {
		t.Error("marked under another run's lock")
	}
	if ReportTBBehind(h.settings, nil, h.out, nil, false, nil) || len(h.sent) != 1 {
		t.Errorf("no notice sent %q", h.results())
	}
}

// covers: MA-1188
// An encrypted archive gets no completeness email at all (O1, §9.3): even handed a notice, nothing
// is sent and nothing is written beside it. Positive twin first: the same notice for a plaintext
// archive sends.
func TestAnEncryptedArchiveGetsNoDownloadEmail(t *testing.T) {
	g := newTBRig(t, alertsOnFailure, false)
	if !g.report(false, nil) {
		t.Fatal("positive twin: not sent")
	}
	e := newTBRig(t, alertsOnFailure, false)
	enc := t.TempDir()
	p, err := vault.PrepareCreate(enc, "tb alert test passphrase")
	if err != nil {
		t.Fatal(err)
	}
	k, err := p.Commit()
	if err != nil {
		t.Fatal(err)
	}
	k.Destroy()
	if !app.ArchiveEncrypted(enc) {
		t.Fatal("the archive is not encrypted")
	}
	if ReportTBBehind(e.settings, nil, enc, []state.TBNotice{e.notice}, false, nil) || len(e.sent) != 0 {
		t.Errorf("an encrypted archive sent %q", e.results())
	}
	if _, err := os.Lstat(filepath.Join(enc, state.TBStreakName)); !os.IsNotExist(err) {
		t.Errorf("an encrypted archive holds %s (%v)", state.TBStreakName, err)
	}
	if ev := g.sent[0]; ev.TB == nil || ev.Result != alert.ResultTBBehind {
		t.Errorf("positive twin's event: %+v", ev)
	}
}
