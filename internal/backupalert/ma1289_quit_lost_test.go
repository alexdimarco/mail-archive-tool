package backupalert

import (
	"bufio"
	"bytes"
	"fmt"
	"log"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"mail-archive-tool/internal/alert"
	"mail-archive-tool/internal/state"
)

// SPAM-01 (docs/review-thunderbird-completeness-c5-adversarial.md, the lead's call of 2026-11-09):
// an email the mail server accepted counts as sent, even when the goodbye after it fails, so a
// once-per-streak email is never sent again at every backup. The shared sender is fixed, so the
// already-shipped Thunderbird-not-synced email is fixed with it.

// smtpCounts is what a scripted loopback server saw: the messages it was handed (each DATA it read
// to the end) and the ones it accepted.
type smtpCounts struct{ tried, accepted int64 }

// scriptedSMTPServer is a loopback SMTP server on 127.0.0.1 (no mail leaves the computer) that
// answers the end of DATA with afterData (a 250 counts as accepted) and then, on QUIT, says
// goodbye (221) or drops the connection with no reply.
func scriptedSMTPServer(t *testing.T, afterData string, goodbye bool) (host string, port int, n *smtpCounts) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	n = &smtpCounts{}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(10 * time.Second))
				r, w := bufio.NewReader(c), bufio.NewWriter(c)
				say := func(s string) { w.WriteString(s + "\r\n"); w.Flush() }
				say("220 loopback ESMTP")
				for {
					line, err := r.ReadString('\n')
					if err != nil {
						return
					}
					switch cmd := strings.ToUpper(strings.TrimSpace(line)); {
					case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
						say("250 loopback")
					case cmd == "DATA":
						say("354 go ahead")
						for {
							l, err := r.ReadString('\n')
							if err != nil {
								return
							}
							if l == ".\r\n" {
								break
							}
						}
						atomic.AddInt64(&n.tried, 1)
						if strings.HasPrefix(afterData, "250") {
							atomic.AddInt64(&n.accepted, 1)
						}
						say(afterData)
					case cmd == "QUIT":
						if goodbye {
							say("221 bye")
						}
						return // with no goodbye: drop without 221
					default:
						say("250 ok")
					}
				}
			}(c)
		}
	}()
	a := ln.Addr().(*net.TCPAddr)
	return a.IP.String(), a.Port, n
}

// quitDropSMTP is the C5 pass's server: it ACCEPTS every message (250 after DATA) and then drops
// the connection on QUIT without a reply. It counts the messages it accepted.
func quitDropSMTP(t *testing.T) (host string, port int, accepted *int64) {
	host, port, n := scriptedSMTPServer(t, "250 accepted for delivery", false)
	return host, port, &n.accepted
}

func loopbackAlerts(host string, port int) string {
	return fmt.Sprintf(`{"alertEnabled":true,"alertOnFailure":true,"alertProvider":"smtp","alertHost":%q,"alertPort":%d,"alertSecurity":%q,"alertFrom":"b@example.org","alertTo":"t@example.org"}`, host, port, alert.SecurityNone)
}

// covers: MA-1289, MA-1184
// SPAM-01, the Thunderbird download email: the server accepted the message, then the goodbye's reply
// was lost. One email over 5 due runs, the streak's flag set after the first, and the log says the
// server took it. Positive twin first: a server that accepts and says goodbye, one email, flagged.
func TestAttackC5_AcceptedThenQuitLostResendsEveryRun(t *testing.T) {
	th, tp, tn := scriptedSMTPServer(t, "250 kept", true)
	twin := newTBRig(t, loopbackAlerts(th, tp), false)
	restore := SetSenderForTest(func(c alert.Config, pw string, ev alert.Event) error { return alert.Send(c, pw, ev) })
	if !twin.report(false, nil) {
		t.Fatal("positive twin: not sent")
	}
	// The same notice handed again (a second run's fold racing the mark) finds it marked: nothing.
	if twin.report(false, nil) {
		t.Error("a notice the record marks sent was sent again")
	}
	restore()
	if still, _ := twin.flags(); !still || atomic.LoadInt64(&tn.accepted) != 1 {
		t.Fatalf("positive twin: accepted %d, flag %v", atomic.LoadInt64(&tn.accepted), still)
	}

	host, port, accepted := quitDropSMTP(t)
	g := newTBRig(t, loopbackAlerts(host, port), false)
	restore = SetSenderForTest(func(c alert.Config, pw string, ev alert.Event) error { return alert.Send(c, pw, ev) })
	defer restore()
	var buf bytes.Buffer
	for i := 0; i < 5; i++ {
		g.report(false, log.New(&buf, "", 0))
	}
	still, _ := g.flags()
	got := atomic.LoadInt64(accepted)
	t.Logf("accepted by the server: %d; still_downloading_sent=%v", got, still)
	if got != 1 || !still {
		t.Errorf("SPAM-01: %d identical still-downloading emails accepted over 5 due runs; flag set=%v", got, still)
	}
	if !strings.Contains(buf.String(), "the mail server accepted the email") || !strings.Contains(buf.String(), "still downloading email was sent to") {
		t.Errorf("the log: %q", buf.String())
	}
}

// covers: MA-1289, MA-910
// SPAM-01, the already-shipped Thunderbird-not-synced email, through the same shared sender: the
// server accepted it and the goodbye was lost: one email over 5 stale runs, told. Positive twin
// first: with the goodbye, one email, told.
func TestTheSyncEmailAcceptedThenQuitLostGoesOnce(t *testing.T) {
	th, tp, tn := scriptedSMTPServer(t, "250 kept", true)
	twin := newSyncRig(t, loopbackAlerts(th, tp))
	twin.plant(staleFrom(state.SyncStreak{}, syncT0, "owner-active-skipped"))
	restore := SetSenderForTest(func(c alert.Config, pw string, ev alert.Event) error { return alert.Send(c, pw, ev) })
	twin.tell(false)
	twin.tell(false)
	restore()
	if got := atomic.LoadInt64(&tn.accepted); got != 1 {
		t.Fatalf("positive twin: accepted %d", got)
	}

	host, port, accepted := quitDropSMTP(t)
	g := newSyncRig(t, loopbackAlerts(host, port))
	g.plant(staleFrom(state.SyncStreak{}, syncT0, "owner-active-skipped"))
	restore = SetSenderForTest(func(c alert.Config, pw string, ev alert.Event) error { return alert.Send(c, pw, ev) })
	defer restore()
	for i := 0; i < 5; i++ {
		g.tell(false)
	}
	if got := atomic.LoadInt64(accepted); got != 1 {
		t.Errorf("SPAM-01 (sync email): %d identical emails accepted over 5 stale runs", got)
	}
	if told, err := readSyncAlert(SyncAlertPath(g.settings, g.key)); err != nil || told.ToldFor != 1 {
		t.Errorf("the told-file: %+v (%v)", told, err)
	}
}

// covers: MA-1289
// Every alert email goes through the one sender: the backup-result email and the dashboard's test
// email count a message the server accepted as sent too, the goodbye lost or not; a refused one is
// still a failure. Positive twin first: a refused message is "could not be sent" and the test
// email reports the error.
func TestEveryAlertEmailCountsAnAcceptedMessage(t *testing.T) {
	onSuccess := func(cfg string) string {
		return strings.Replace(cfg, `"alertOnFailure":true`, `"alertOnFailure":true,"alertOnSuccess":true`, 1)
	}
	rh, rp, _ := scriptedSMTPServer(t, "554 refused", true)
	refused := newStreakRig(t, onSuccess(loopbackAlerts(rh, rp)))
	restore := SetSenderForTest(func(c alert.Config, pw string, ev alert.Event) error { return alert.Send(c, pw, ev) })
	defer restore()
	var buf bytes.Buffer
	FireForRun(refused.settings, nil, "/archive", ResultOK, nil, 1, log.New(&buf, "", 0))
	if !strings.Contains(buf.String(), "could not be sent") {
		t.Fatalf("positive twin: a refused result email's log %q", buf.String())
	}
	if err := SendTest(refused.settings, nil, "/archive", nil); err == nil {
		t.Fatal("positive twin: a refused test email reported no error")
	}

	host, port, accepted := quitDropSMTP(t)
	g := newStreakRig(t, onSuccess(loopbackAlerts(host, port)))
	restore2 := SetSenderForTest(func(c alert.Config, pw string, ev alert.Event) error { return alert.Send(c, pw, ev) })
	defer restore2()
	buf.Reset()
	FireForRun(g.settings, nil, "/archive", ResultOK, nil, 1, log.New(&buf, "", 0))
	if !strings.Contains(buf.String(), "backup alert email sent") || strings.Contains(buf.String(), "could not be sent") {
		t.Errorf("the result email the server accepted: log %q", buf.String())
	}
	if err := SendTest(g.settings, nil, "/archive", nil); err != nil {
		t.Errorf("the test email the server accepted reported %v", err)
	}
	if got := atomic.LoadInt64(accepted); got != 2 {
		t.Errorf("the server accepted %d emails, want 2", got)
	}
}

// covers: MA-1289
// A message the server refused is still not sent: nothing is marked, the log says it could not be
// sent, and the next due run tries again (a failure before acceptance stays a failure). Positive
// twin first: a refused message, then the same server accepting, sends once and marks.
func TestARefusedEmailIsStillTriedAgain(t *testing.T) {
	host, port, n := scriptedSMTPServer(t, "554 refused", true)
	g := newTBRig(t, loopbackAlerts(host, port), false)
	restore := SetSenderForTest(func(c alert.Config, pw string, ev alert.Event) error { return alert.Send(c, pw, ev) })
	defer restore()
	var buf bytes.Buffer
	if g.report(false, log.New(&buf, "", 0)) || g.report(false, nil) {
		t.Fatal("a refused message was reported sent")
	}
	if still, _ := g.flags(); still || atomic.LoadInt64(&n.tried) != 2 || atomic.LoadInt64(&n.accepted) != 0 {
		t.Errorf("a refused message: tried %d, flag %v", atomic.LoadInt64(&n.tried), still)
	}
	if !strings.Contains(buf.String(), "could not be sent") || strings.Contains(buf.String(), "accepted") {
		t.Errorf("the log: %q", buf.String())
	}
}
