package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/schedule"
)

// archiveNowCard is the Archive now card's own part of the page, from its heading to the
// next card.
func archiveNowCard(t *testing.T, page string) string {
	t.Helper()
	i := strings.Index(page, `<div class="card" id="sec-archive">`)
	if i < 0 {
		t.Fatal("the page has no Archive now card")
	}
	j := strings.Index(page[i:], `<div class="card" id="sec-settings">`)
	if j < 0 {
		t.Fatal("the Archive now card has no end")
	}
	return page[i : i+j]
}

// beforeDetails is the card's text a person sees without opening anything: up to the
// "Other ways to add new mail" disclosure.
func beforeDetails(card string) string {
	if k := strings.Index(card, "<details"); k >= 0 {
		return card[:k]
	}
	return card
}

// covers: MA-941, MA-546
// The owner's Windows lab of 2026-10-31: on a local Thunderbird archive the Archive now
// card was a long wizard and command-line recipe that never named Run now ("not sure
// what you want me to click"). Now the card says in one line that Archive now is for
// Microsoft 365 archives, then where to click: Run now on the Scheduled backup card (a
// link to it) when Run now is there; with no schedule yet, How often in Settings and
// Save settings first (a link to Settings), after which Run now appears; with a
// schedule set up at the command line, the Scheduled backup card (which says why Run
// now is not offered, MA-942); otherwise, the ways below. The wizard and terminal steps
// (MA-546's exact argv) stay on the card under "Other ways to add new mail", open only
// when no Desktop way exists. The sidebar gains a "Run now" link to the Scheduled backup
// card wherever Run now is on the page (MA-941, item 2). Positive twin first: a
// Microsoft 365 archive keeps its Archive now button and gets no pointer or link.
func TestArchiveNowCardPointsAtRunNow(t *testing.T) {
	isolateHome(t)

	// The positive twin: a Microsoft 365 archive keeps Archive now, with no pointer.
	m365 := emptyArchive(t)
	addStore(t, m365, "mailbox:a@contoso.org")
	mp := newA3Rig(t, m365, "darwin").page()
	if !strings.Contains(mp, `id="archiveBtn"`) || strings.Contains(mp, `id="archiveNowWay"`) || strings.Contains(mp, `id="navRunNow"`) || !strings.Contains(mp, `id="navArchive"`) {
		t.Fatal("the positive twin: a Microsoft 365 archive lost Archive now, or got the local pointer or the Run now link")
	}

	out := tbProfileArchive(t)
	g := newA3Rig(t, out, "darwin")
	hand := addMailHelp("darwin", out)

	// No schedule yet: How often in Settings and Save settings first.
	card := archiveNowCard(t, g.page())
	seen := beforeDetails(card)
	for _, want := range []string{
		"Archive now is off for this archive. It is for Microsoft 365 archives, and this one was made from mail files on this computer.",
		`To back it up now, first give it a scheduled backup: in <a href="#sec-settings">Settings</a>, choose <b>How often</b> and click <b>Save settings</b>. <b>Run now</b> then appears on the Scheduled backup card.`,
	} {
		if !strings.Contains(seen, want) {
			t.Errorf("with no schedule, the card does not say %q:\n%s", want, seen)
		}
	}
	if strings.Contains(seen, "mailarchive ") || strings.Contains(seen, "Thunderbird / mbox mail folder") {
		t.Errorf("the card still leads with the wizard and terminal recipe:\n%s", seen)
	}
	if !strings.Contains(card, `<details class="steps" id="archiveNowHand"><summary>Other ways to add new mail: the wizard or a terminal</summary>`) || !shows(card, hand) {
		t.Errorf("the wizard and terminal steps are no longer on the card (MA-546):\n%s", card)
	}
	if strings.Contains(card, `id="archiveNowHand" open`) {
		t.Error("the hand steps are opened while a Desktop way exists")
	}
	if p := g.page(); strings.Contains(p, `id="navRunNow"`) || !strings.Contains(p, `<a href="#sec-archive" id="navArchive">Archive now</a>`) {
		t.Error("the sidebar links to Run now while no Run now is on the page, or no longer offers Archive now")
	}

	// A schedule made in Settings: Run now on the Scheduled backup card, and the sidebar link.
	if body := g.post(`{"interval":"daily","time":"04:00"}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("create: %s", body)
	}
	g.recordLast()
	page := g.page()
	card = archiveNowCard(t, page)
	if want := `To back it up now, click <b>Run now</b> on the <a href="#sec-backup">Scheduled backup</a> card.`; !strings.Contains(beforeDetails(card), want) {
		t.Errorf("with Run now on the page, the card does not say %q:\n%s", want, card)
	}
	if !strings.Contains(page, `id="runBackupBtn"`) || !strings.Contains(page, `<a href="#sec-backup" id="navRunNow">Run now</a>`) {
		t.Error("Run now, or the sidebar's link to it, is missing")
	}
	// The friction review's O1 (applied by the lead, 2026-10-31): with Run now shown, a local
	// archive's sidebar no longer offers Archive now, which is off for it; its card stays.
	if strings.Contains(page, `id="navArchive"`) || !strings.Contains(page, `<div class="card" id="sec-archive">`) {
		t.Error("with Run now shown, the sidebar still offers Archive now, or the Archive now card is gone")
	}

	// A schedule set up at the command line: the card says where to read why.
	os.Remove(g.jobPath)
	exp := schedule.Spec{Name: schedule.DefaultNameFor(out), Interval: schedule.Weekly, At: "03:00", Exe: "/opt/mailarchive",
		Args: []string{"-input", filepath.Join(out, "x"), "-out", out, "-mode", "incremental"}, Out: out}
	if err := schedule.WriteDescriptor(out, schedule.DescribeSpec(exp)); err != nil {
		t.Fatal(err)
	}
	page = g.page()
	if want := `Its scheduled backup was set up at the command line, so Run now isn't offered for it: the <a href="#sec-backup">Scheduled backup</a> card says how to get Run now.`; !strings.Contains(beforeDetails(archiveNowCard(t, page)), want) {
		t.Errorf("with a command-line schedule, the card does not say %q", want)
	}
	if strings.Contains(page, `id="navRunNow"`) {
		t.Error("the sidebar links to Run now for a command-line schedule, which shows none")
	}

	// No Desktop way (Linux has no local scheduling here): the hand steps, opened.
	lin := newA3Rig(t, tbProfileArchive(t), "linux")
	card = archiveNowCard(t, lin.page())
	if !strings.Contains(beforeDetails(card), "To add new mail now, use one of the ways below.") || !strings.Contains(card, `id="archiveNowHand" open`) {
		t.Errorf("with no Desktop way, the card does not open the hand steps:\n%s", card)
	}
}
