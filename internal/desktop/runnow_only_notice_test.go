package desktop

import (
	"html"
	"strings"
	"testing"

	"mail-archive-tool/internal/presync"
)

// The Settings card's sync notice for a backup run only on Run now (the Windows texts pass,
// design-presync-attended §14 gate 3, 2026-10-31, finding 3): the scheduled backup's notice
// (presync.SyncNoticeFor) talks about asking Thunderbird to quit and about a scheduled
// backup's 2-minute idle rule, neither of which a backup run only on Run now meets, so
// "Only when I click Run now" shows a Run now notice like the encrypted card's.

// runNowOnlyNoticeMac and runNowOnlyNoticeWin are that notice, pinned whole. Windows' says
// a Primary Password window takes the keyboard each time the sync starts Thunderbird
// (finding 5, lab L-A10 pp1).
const (
	runNowOnlyNoticeMac = "Before each Run now, MailArchive starts Thunderbird out of sight and waits for its mail to arrive (up to 20 minutes, often about 13), so the archive includes mail that had only reached the server, then leaves it open for you to quit from the Dock. Because it drives your real Thunderbird, starting it does what opening Thunderbird always does: it checks all your accounts, sends anything waiting in your Outbox, runs your message filters, and may show notifications. A Thunderbird you already have open is left alone — never started or closed. If you use more than one Thunderbird profile, only the one holding this mailbox is synced."
	runNowOnlyNoticeWin = "Before each Run now, MailArchive starts Thunderbird minimized and waits for its mail to arrive (up to 20 minutes, often about 13), so the archive includes mail that had only reached the server, then leaves it open for you to quit from its icon on the taskbar, or among the small icons by the clock. Because it drives your real Thunderbird, starting it does what opening Thunderbird always does: it checks all your accounts, sends anything waiting in your Outbox, runs your message filters, and may show notifications. A Thunderbird you already have open is left alone — never started or closed. If Thunderbird has a Primary Password, its password window appears and takes the keyboard each time the sync starts Thunderbird. If you use more than one Thunderbird profile, only the one holding this mailbox is synced."
)

// covers: MA-949, X16
// With How often "Only when I click Run now", the Settings card shows the Run now notice in
// place of the scheduled backup's, and on a Mac hides the Automation note, which is about a
// scheduled backup closing Thunderbird. Both notices are drawn, each marked, so the page can
// switch them as How often changes before a Save; CI has no JavaScript engine, so the switch
// in settingsState is checked as written. Positive twin first: Daily shows the scheduled
// backup's notice and the Mac note, and hides the Run now one. On Windows the Run now notice
// carries the password sentence, and where Run now does not start Thunderbird it says so
// instead of promising a sync.
func TestRunNowOnlyShowsARunNowSyncNotice(t *testing.T) {
	isolateHome(t)
	const (
		schedTag  = `<p class="note" id="schedPresyncNote" data-sched-notice>`
		schedOff  = `<p class="note" id="schedPresyncNote" data-sched-notice hidden>`
		manualTag = `<p class="note" id="schedPresyncManualNote" data-manual-notice>`
		manualOff = `<p class="note" id="schedPresyncManualNote" data-manual-notice hidden>`
		macTag    = `<p class="note" id="schedMacNote" data-sched-notice>`
		macOff    = `<p class="note" id="schedMacNote" data-sched-notice hidden>`
	)
	esc := html.EscapeString

	g := newA3Rig(t, tbProfileArchive(t), "darwin")
	if r := g.manualReply(`{"interval":"daily","time":"04:00","presync":true}`); r["ok"] != true {
		t.Fatalf("daily: %v", r)
	}
	g.recordLast()
	card := cardOf(g.page(), "sec-settings")
	if !strings.Contains(card, schedTag+esc(presync.SyncNoticeFor("darwin"))+"</p>") || !strings.Contains(card, manualOff+esc(runNowOnlyNoticeMac)+"</p>") ||
		!strings.Contains(card, macTag+esc(presync.MacAutomationNote)+"</p>") {
		t.Fatalf("positive twin: Daily does not show the scheduled backup's notice and the Mac note with the Run now notice hidden:\n%s", card)
	}

	if r := g.manualReply(`{"interval":"manual","time":"04:00","presync":true}`); r["ok"] != true {
		t.Fatalf("Only when I click Run now: %v", r)
	}
	card = cardOf(g.page(), "sec-settings")
	if !strings.Contains(card, manualTag+esc(runNowOnlyNoticeMac)+"</p>") || !strings.Contains(card, schedOff) || !strings.Contains(card, macOff) {
		t.Errorf("a Mac, Only when I click Run now: the Run now notice is not the one shown:\n%s", card)
	}

	w := newA3Rig(t, tbProfileArchive(t), "windows")
	if r := w.manualReply(`{"interval":"manual","time":"04:00","presync":true}`); r["ok"] != true {
		t.Fatalf("Windows, Only when I click Run now: %v", r)
	}
	card = cardOf(w.page(), "sec-settings")
	if !strings.Contains(card, manualTag+esc(runNowOnlyNoticeWin)+"</p>") || !strings.Contains(card, schedOff) || strings.Contains(card, `id="schedMacNote"`) {
		t.Errorf("Windows, Only when I click Run now: the Run now notice is not the one shown:\n%s", card)
	}
	if got := manualSyncNotice("windows"); got != runNowOnlyNoticeWin {
		t.Errorf("Windows' Run now notice:\n  %q\nwant\n  %q", got, runNowOnlyNoticeWin)
	}
	if got := manualSyncNotice("darwin"); got != runNowOnlyNoticeMac {
		t.Errorf("a Mac's Run now notice:\n  %q\nwant\n  %q", got, runNowOnlyNoticeMac)
	}
	for _, n := range []string{runNowOnlyNoticeMac, runNowOnlyNoticeWin} {
		for _, gone := range []string{"cheduled", "quit Thunderbird", "asks Thunderbird to quit", "on-exit", "2 minutes"} {
			if strings.Contains(n, gone) {
				t.Errorf("the Run now notice says %q: %q", gone, n)
			}
		}
	}

	r := presync.OverrideTablesForTest(nil, map[string]bool{"windows": false})
	off := manualSyncNotice("windows")
	r()
	if off != encSyncStartOffLine {
		t.Errorf("Windows with its start entry off: the Run now notice %q, want %q", off, encSyncStartOffLine)
	}

	// The switch as How often changes before a Save (structural).
	if !strings.Contains(dashboardJS, "const mn=document.querySelector('[data-manual-notice]');if(mn){mn.hidden=!manual;document.querySelectorAll('[data-sched-notice]').forEach(e=>{e.hidden=manual;});}") {
		t.Error("settingsState does not switch the notices with How often")
	}
}
