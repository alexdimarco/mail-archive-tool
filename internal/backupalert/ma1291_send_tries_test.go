package backupalert

import (
	"bytes"
	"errors"
	"log"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"mail-archive-tool/internal/alert"
	"mail-archive-tool/internal/state"
)

// SPAM-R2-A (docs/review-thunderbird-completeness-c5-adversarial-r2.md, the lead's call of
// 2026-11-09): an email about Thunderbird whose send failed is tried at most AlertSendTries (5)
// times per streak; after the 5th failure no more are tried, and the alert_send_failed WARN, in the
// lead's words, goes into the last-run record, where `status` and Status & health read it. The
// download email keeps its count in the streak record beside its sent flag; the shipped
// Thunderbird-not-synced email keeps it in its told-file. A failure never counts as sent.

// sendFailedWords is the alert_send_failed WARN's sentence (the lead's words).
const sendFailedWords = "MailArchive could not send its email about Thunderbird (5 tries). Check the email settings in MailArchive Desktop's Settings, then use Send test email."

// finishedRun writes a finished ok last-run record into out, as each scheduled run does before its
// alert step (which rewrites it, so a WARN must be added again by every run it still holds for).
func finishedRun(t *testing.T, out string) {
	t.Helper()
	now := time.Now().UTC()
	if err := state.WriteLastRun(out, state.LastRun{Status: state.RunOK, Started: now.Add(-time.Minute), Finished: now}); err != nil {
		t.Fatal(err)
	}
}

// sendFailedWarn is the last-run record's alert_send_failed WARNs at out.
func sendFailedWarn(t *testing.T, out string) []state.RunWarning {
	t.Helper()
	lr, _, err := state.ReadLastRun(out)
	if err != nil {
		t.Fatal(err)
	}
	var ws []state.RunWarning
	for _, w := range lr.Warnings {
		if w.Code == "alert_send_failed" {
			ws = append(ws, w)
		}
	}
	return ws
}

func failures(n int) []error {
	var e []error
	for range n {
		e = append(e, errors.New("the mail server did not answer"))
	}
	return e
}

// covers: MA-1291, MA-1184
// The Thunderbird download email: over 7 due runs whose sends all fail, 5 are tried; from the 5th
// failure on, each run's record holds one alert_send_failed WARN in the lead's words (none before),
// the log says no more are tried, and the email is never marked sent. The stopped email of the same
// streak has its own 5 tries. Positive twin first: a send that fails once, then goes, is marked sent
// with no WARN.
func TestTheDownloadEmailIsTriedAtMostFiveTimes(t *testing.T) {
	tw := newTBRig(t, alertsOnFailure, false)
	finishedRun(t, tw.out)
	tw.fail = failures(1)
	if tw.report(false, nil) || !tw.report(false, nil) {
		t.Fatal("positive twin: a failure then a send")
	}
	if still, _ := tw.flags(); !still || len(sendFailedWarn(t, tw.out)) != 0 {
		t.Fatalf("positive twin: flag %v, WARNs %+v", still, sendFailedWarn(t, tw.out))
	}

	g := newTBRig(t, alertsOnFailure, false)
	g.fail = failures(7)
	var buf bytes.Buffer
	for i := 1; i <= 7; i++ {
		finishedRun(t, g.out)
		if g.report(false, log.New(&buf, "", 0)) {
			t.Fatalf("run %d: a failed send reported sent", i)
		}
		ws := sendFailedWarn(t, g.out)
		if i < state.AlertSendTries && len(ws) != 0 {
			t.Errorf("run %d: the WARN before the 5th failure: %+v", i, ws)
		}
		if i >= state.AlertSendTries && (len(ws) != 1 || ws[0].Message != sendFailedWords) {
			t.Errorf("run %d: the WARN %+v, want one with %q", i, ws, sendFailedWords)
		}
	}
	if tried := 7 - len(g.fail); tried != state.AlertSendTries {
		t.Errorf("SPAM-R2-A: %d sends tried over 7 due runs, want %d", tried, state.AlertSendTries)
	}
	if still, _ := g.flags(); still {
		t.Error("failed sends marked the email sent")
	}
	if !strings.Contains(buf.String(), "no more are tried") {
		t.Errorf("the log: %q", buf.String())
	}
	stop := g.notice
	stop.Stopped = true
	g.fail = failures(1)
	finishedRun(t, g.out)
	ReportTBBehind(g.settings, nil, g.out, []state.TBNotice{stop}, false, nil)
	if len(g.fail) != 0 {
		t.Error("the stopped email was not tried: it has its own count")
	}
}

// covers: MA-1291, MA-1184
// The pass's TestR2Attack_CommittedButReplyBrokenResends, through the real SMTP sender and a loopback
// server that takes each message but breaks its reply to the end of the data ("25" and a drop): the
// client cannot know it was taken, so each counts as a failure. Over 7 due runs the message is
// handed over 5 times (the pass: once per due run, no cap), the flag stays unset, and the WARN is
// in the record. Positive twin first: a server that replies 250 takes it once, marked.
func TestABrokenReplyIsHandedOverAtMostFiveTimes(t *testing.T) {
	th, tp, tn := scriptedSMTPServer(t, "250 kept", true)
	twin := newTBRig(t, loopbackAlerts(th, tp), false)
	restore := SetSenderForTest(func(c alert.Config, pw string, ev alert.Event) error { return alert.Send(c, pw, ev) })
	defer restore()
	finishedRun(t, twin.out)
	twin.report(false, nil)
	twin.report(false, nil)
	if still, _ := twin.flags(); !still || atomic.LoadInt64(&tn.accepted) != 1 {
		t.Fatalf("positive twin: accepted %d, flag %v", atomic.LoadInt64(&tn.accepted), still)
	}

	host, port, n := scriptedSMTPServer(t, "25", false)
	g := newTBRig(t, loopbackAlerts(host, port), false) // installs the rig's fake sender: the real one goes after
	defer SetSenderForTest(func(c alert.Config, pw string, ev alert.Event) error { return alert.Send(c, pw, ev) })()
	for range 7 {
		finishedRun(t, g.out)
		g.report(false, nil)
	}
	still, _ := g.flags()
	if got := atomic.LoadInt64(&n.tried); got != state.AlertSendTries || still {
		t.Errorf("SPAM-R2-A: handed over in full %d times over 7 due runs (want %d), flag %v", got, state.AlertSendTries, still)
	}
	if ws := sendFailedWarn(t, g.out); len(ws) != 1 {
		t.Errorf("the WARN: %+v", ws)
	}
}

// covers: MA-1291, MA-910
// The shipped Thunderbird-not-synced email, the same cap: its count of failed sends is kept in its
// told-file, beside the streak it was told for, so 7 stale runs whose sends all fail try 5, the
// told-file never says it was told, and the WARN is in the record from the 5th failure on. A new
// streak (a new serial) has its own tries. Positive twin first: a failure then a send is told once,
// with no WARN.
func TestTheSyncEmailIsTriedAtMostFiveTimes(t *testing.T) {
	tw := newSyncRig(t, alertsOnFailure)
	tw.plant(staleFrom(state.SyncStreak{}, syncT0, "owner-active-skipped"))
	finishedRun(t, tw.out)
	tw.fail = failures(1)
	tw.tell(false)
	tw.tell(false)
	tw.tell(false)
	if tw.count() != 1 || len(sendFailedWarn(t, tw.out)) != 0 {
		t.Fatalf("positive twin: %d emails, WARNs %+v", tw.count(), sendFailedWarn(t, tw.out))
	}

	g := newSyncRig(t, alertsOnFailure)
	s := staleFrom(state.SyncStreak{}, syncT0, "owner-active-skipped").Named()
	g.plant(s)
	g.fail = failures(7)
	for i := 1; i <= 7; i++ {
		finishedRun(t, g.out)
		g.tell(false)
		if ws := sendFailedWarn(t, g.out); (i >= state.AlertSendTries) != (len(ws) == 1) || len(ws) == 1 && ws[0].Message != sendFailedWords {
			t.Errorf("run %d: the WARN %+v", i, ws)
		}
	}
	if tried := 7 - len(g.fail); tried != state.AlertSendTries || g.count() != 0 {
		t.Errorf("SPAM-R2-A (sync email): %d sends tried over 7 stale runs, %d sent", tried, g.count())
	}
	told, err := readSyncAlert(SyncAlertPath(g.settings, g.key))
	if err != nil || told.toldOf(s) || told.Tries != state.AlertSendTries {
		t.Errorf("the told-file: %+v (%v)", told, err)
	}
	// The next streak has its own tries: it goes.
	g.fail = nil
	g.plant(staleFrom(s.Synced(syncT0.Add(5*24*time.Hour)), syncT0.Add(6*24*time.Hour), "owner-active-skipped"))
	finishedRun(t, g.out)
	g.tell(false)
	if g.count() != 1 || len(sendFailedWarn(t, g.out)) != 0 {
		t.Errorf("a new streak: %d emails, WARNs %+v", g.count(), sendFailedWarn(t, g.out))
	}
}
