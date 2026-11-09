package wizard

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/backupalert"
	"mail-archive-tool/internal/presync"
	"mail-archive-tool/internal/presyncjob"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/tbcomplete"
)

// tbStoreFixture is a real Thunderbird profile under dir with one IMAP account on host whose INBOX
// lists listed messages and holds have of them on disk, its folder cache written as Thunderbird
// writes it (keys at the store's path as given). It returns the store folder.
func tbStoreFixture(t *testing.T, dir, host string, listed, have int) string {
	t.Helper()
	p := filepath.Join(dir, "qzprofile.default-release")
	store := filepath.Join(p, "ImapMail", host)
	write := func(path, body string) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(p, "prefs.js"), "// Mozilla User Preferences\n\n"+
		`user_pref("mail.account.account1.server", "server1");`+"\n"+
		`user_pref("mail.server.server1.directory-rel", "[ProfD]ImapMail/`+host+`");`+"\n"+
		`user_pref("mail.server.server1.hostname", "`+host+`");`+"\n"+
		`user_pref("mail.server.server1.type", "imap");`+"\n"+
		`user_pref("mail.server.server1.name", "qzsam@`+host+`");`+"\n"+
		`user_pref("mail.accountmanager.accounts", "account1");`+"\n")
	write(filepath.Join(p, "compatibility.ini"), "[Compatibility]\nLastVersion=153.6.0_20251212000000/20251212000000\n")
	var mbox strings.Builder
	for i := 1; i <= have; i++ {
		fmt.Fprintf(&mbox, "From - Mon Mar  3 09:00:00 2025\nFrom: Alice <alice@example.com>\nTo: Bob <bob@example.com>\nSubject: message %d\nDate: Mon, 03 Mar 2025 09:00:00 +0000\nMessage-ID: <qz%d@c5.example>\nX-Mozilla-Status: 0001\nX-Mozilla-Status2: 00000000\nContent-Type: text/plain\n\nbody %d\n\n", i, i, i)
	}
	write(filepath.Join(store, "INBOX"), mbox.String())
	write(filepath.Join(store, "INBOX.msf"), "")
	checked := time.Now().Add(-time.Hour).Unix()
	cache := map[string]any{
		store: map[string]any{"flags": 0x0c, "totalMsgs": -1, "totalUnreadMsgs": -1, "pendingUnreadMsgs": 0, "pendingMsgs": 0, "expungedBytes": 0, "folderSize": -1},
		filepath.Join(store, "INBOX.msf"): map[string]any{
			"flags": tbcomplete.FlagInbox | tbcomplete.FlagImapBox | tbcomplete.FlagOffline, "totalMsgs": listed, "totalUnreadMsgs": 0,
			"pendingUnreadMsgs": 0, "pendingMsgs": 0, "expungedBytes": 0, "folderSize": listed * 1000, "boxFlags": 0, "hierDelim": 47,
			"onlineName": "INBOX", "aclFlags": 0, "serverTotal": listed, "serverUnseen": 0, "serverRecent": 0, "nextUID": listed + 1,
			"lastSyncTimeInSec": checked,
		},
	}
	data, err := json.Marshal(cache)
	if err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(p, tbcomplete.CacheFile), string(data))
	return store
}

// tbBackdate moves the archive's one streak 4 days back: its first and latest behind run, and its
// latest growth when grew is false (X then as now, so this run finds no growth); when grew is true
// X then was 1, so this run's X is growth.
func (g *localRunRig) tbBackdate(grew bool) {
	g.t.Helper()
	s, err := state.ReadTBStreak(g.out)
	if err != nil || len(s.Accounts) != 1 {
		g.t.Fatalf("the streak record: %v %+v", err, s)
	}
	back := time.Now().UTC().Add(-4 * 24 * time.Hour)
	a := &s.Accounts[0]
	a.FirstBehind, a.LastBehind, a.LastGrew, a.SeenAt, s.LastRunAt = back, back, back, back, back
	if grew {
		a.FirstX, a.GrewX = 1, 1
	}
	if err := state.WriteTBStreak(g.out, s); err != nil {
		g.t.Fatal(err)
	}
}

func (g *localRunRig) tbSent() int { return strings.Count(g.results(), backupalert.ResultTBBehind) }

// covers: MA-1184, MA-1188
// The Thunderbird download emails through the real scheduled entry point (RunHeadless) and a real
// Thunderbird profile with an account still downloading (3 of 40 messages): the first behind run
// sends nothing; 3 days into the streak, with X growing, one still-downloading email, whose words
// name the account by its folder and never by the name Thunderbird shows for it; the next run
// sends nothing; once X has not grown for 3 days, one stopped email; the run after sends nothing.
// A failed delivery is tried at the next run. A run that sends the recovery email defers it to
// the next run (one email per run). With alerts off nothing is sent. Positive twin first: the
// still-downloading email.
func TestTheScheduledRunSendsTheDownloadEmailsOncePerStreak(t *testing.T) {
	g := newLocalRunRig(t)
	store := tbStoreFixture(t, g.dir, "imap.qzc5.example", 40, 3)
	j := g.jobAt(filepath.Join(g.dir, "tb.json"), store)
	if RunHeadless(j) != 0 || len(g.sent) != 0 {
		t.Fatalf("the first behind run sent %q", g.results())
	}
	g.tbBackdate(true)
	if RunHeadless(j) != 0 || g.tbSent() != 1 || len(g.sent) != 1 {
		t.Fatalf("positive twin: 4 days into the streak sent %q, want one download email", g.results())
	}
	ev := g.sent[0]
	if ev.TB == nil || ev.TB.Stopped || len(ev.TB.Lines) != 1 || ev.Archive != g.out {
		t.Fatalf("the still-downloading email: %+v", ev)
	}
	line := ev.TB.Lines[0]
	if !strings.Contains(line, "the Thunderbird account kept in the folder imap.qzc5.example for 4 days") || !strings.Contains(line, "about 3 of 40 messages") ||
		strings.Contains(line, "qzsam") || strings.Contains(line, "Inbox") || strings.Contains(ev.TB.Remedy, "qzsam") {
		t.Errorf("the still-downloading words: %q / %q", line, ev.TB.Remedy)
	}
	if RunHeadless(j) != 0 || g.tbSent() != 1 {
		t.Fatalf("the next run sent again: %q", g.results())
	}
	g.tbBackdate(false)
	if RunHeadless(j) != 0 || g.tbSent() != 2 || !g.sent[1].TB.Stopped {
		t.Fatalf("3 days without growth sent %q, want the stopped email", g.results())
	}
	if !strings.Contains(g.sent[1].TB.Lines[0], "has not downloaded any more of the Thunderbird account kept in the folder imap.qzc5.example since") {
		t.Errorf("the stopped words: %q", g.sent[1].TB.Lines[0])
	}
	if RunHeadless(j) != 0 || g.tbSent() != 2 {
		t.Fatalf("the run after the stopped email sent again: %q", g.results())
	}

	// A failed delivery is tried at the next run.
	f := newLocalRunRig(t)
	fj := f.jobAt(filepath.Join(f.dir, "tb.json"), tbStoreFixture(t, f.dir, "imap.qzc5f.example", 40, 3))
	RunHeadless(fj)
	f.tbBackdate(true)
	f.sendErrs = []error{errors.New("the mail server did not answer")}
	if RunHeadless(fj) != 0 || len(f.sent) != 0 {
		t.Fatalf("a failed delivery recorded %q", f.results())
	}
	if RunHeadless(fj) != 0 || f.tbSent() != 1 {
		t.Fatalf("the next run did not try again: %q", f.results())
	}

	// The run that sends the recovery email defers it to the next run.
	r := newLocalRunRig(t)
	rs := tbStoreFixture(t, r.dir, "imap.qzc5r.example", 40, 3)
	rj := r.jobAt(filepath.Join(r.dir, "tb.json"), rs)
	RunHeadless(rj)
	r.tbBackdate(true)
	if RunHeadless(r.jobAt(filepath.Join(r.dir, "fail.json"), filepath.Join(r.dir, "gone-Inbox"))) != 1 || r.results() != backupalert.ResultFailed {
		t.Fatalf("the failed run sent %q", r.results())
	}
	if RunHeadless(rj) != 0 || r.results() != backupalert.ResultFailed+","+backupalert.ResultRecovered {
		t.Fatalf("the recovery run sent %q, want the download email deferred", r.results())
	}
	if RunHeadless(rj) != 0 || r.results() != backupalert.ResultFailed+","+backupalert.ResultRecovered+","+backupalert.ResultTBBehind {
		t.Fatalf("the run after the recovery sent %q", r.results())
	}

	// The run that sends the Thunderbird-not-synced email defers it too (one email per run).
	fake := &outcomeSession{}
	fake.set(presync.OutcomeOwnerActiveSkipped)
	restore := presyncjob.OverrideSessionForTest(fake.run, t.TempDir())
	defer restore()
	rt := presync.OverrideTablesForTest(nil, map[string]bool{runtime.GOOS: true})
	defer rt()
	y := newLocalRunRig(t)
	yj := y.syncJobAt(filepath.Join(y.dir, "tb.json"), tbStoreFixture(t, y.dir, "imap.qzc5y.example", 40, 3))
	RunHeadless(yj)
	y.tbBackdate(true)
	old := time.Now().Add(-4 * 24 * time.Hour)
	if err := state.WriteSyncStreak(y.out, state.SyncStreak{}.Miss("owner-active-skipped", old).Miss("owner-active-skipped", old.Add(time.Hour))); err != nil {
		t.Fatal(err)
	}
	if RunHeadless(yj) != 0 || y.results() != backupalert.ResultSyncStale {
		t.Fatalf("the sync email's run sent %q, want the download email deferred", y.results())
	}
	if RunHeadless(yj) != 0 || y.results() != backupalert.ResultSyncStale+","+backupalert.ResultTBBehind {
		t.Fatalf("the run after the sync email sent %q", y.results())
	}

	// Alerts off: nothing.
	o := newLocalRunRig(t)
	if err := os.WriteFile(o.settings, []byte(`{"alertEnabled":false}`), 0o600); err != nil {
		t.Fatal(err)
	}
	oj := o.jobAt(filepath.Join(o.dir, "tb.json"), tbStoreFixture(t, o.dir, "imap.qzc5o.example", 40, 3))
	RunHeadless(oj)
	o.tbBackdate(true)
	if RunHeadless(oj) != 0 || len(o.sent) != 0 {
		t.Fatalf("alerts off sent %q", o.results())
	}
}
