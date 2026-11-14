package desktop

import (
	"html"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/gmailanswer"
	"mail-archive-tool/internal/state"
)

// settingsGmail is the Settings card's Gmail part of the page, "" when it has none.
func settingsGmail(page string) string {
	page = html.UnescapeString(page)
	at := strings.Index(page, `<div id="settingsGmail">`)
	if at < 0 {
		return ""
	}
	end := strings.Index(page[at:], `<div style="margin-top:14px"><button class="btn secondary" id="saveSettings">`)
	if end < 0 {
		end = len(page) - at
	}
	return page[at : at+end]
}

// covers: MA-1295, R45
// Settings keeps the Gmail question for the current archive, so a Desktop Not now can be undone
// (the B3 friction review's F1), and says when the archive is switched on and that every computer
// then needs 1.8 (F6). Positive twin first: while the archive's row asks, Settings does not ask a
// second time. After Not now the row asks no more and Settings shows the same question, with
// "You said Not now on this computer"; it still does after the next backup (which keeps the store
// as held, not waiting); Yes there names the held store, is kept on this computer, and the Not now
// goes. Once a backup has switched the store on, Settings says the setup line and the 1.8 line.
func TestSettingsKeepsTheGmailQuestionAndSaysWhenGmailIsOn(t *testing.T) {
	e := newDeleteEnv(t)
	out := e.plainArchive("Gmail")
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: out}); err != nil {
		t.Fatal(err)
	}
	gmailWaits(t, out, "gmail-store")
	page := e.page()
	if !strings.Contains(html.UnescapeString(page), state.GmailQuestion) || strings.Contains(settingsGmail(page), state.GmailQuestion) {
		t.Fatal("positive twin: the row asks and Settings asks again, or nobody asks")
	}

	if _, m := e.post("/api/gmail-answer", map[string]any{"path": out, "answer": "not-now"}); m["ok"] != true {
		t.Fatalf("Not now: %v", m)
	}
	set := settingsGmail(e.page())
	if !strings.Contains(set, state.GmailQuestion) || !strings.Contains(set, gmailNotNowKeptLine) || !strings.Contains(set, `data-gmail-answer="yes" data-path="`+out+`"`) {
		t.Fatalf("after Not now Settings does not keep the question: %q", set)
	}
	if r := cardRow(t, e, out); r.Gmail != "" {
		t.Fatal("after Not now the row still asks")
	}
	// The next backup keeps the store as held.
	now := time.Now().UTC()
	if err := state.WriteLastRun(out, state.LastRun{Status: state.RunOK, Started: now, Finished: now, GmailHeld: []string{"gmail-store"}}); err != nil {
		t.Fatal(err)
	}
	if set := settingsGmail(e.page()); !strings.Contains(set, state.GmailQuestion) {
		t.Fatalf("after the next backup Settings no longer asks: %q", set)
	}
	if _, m := e.post("/api/gmail-answer", map[string]any{"path": out, "answer": "yes"}); m["ok"] != true || m["note"] != gmailYesWaitsFor(out, true) {
		t.Fatalf("Yes from Settings: %v", m)
	}
	if a := gmailanswer.Read(out); a.NotNow || len(a.Yes) != 1 || a.Yes[0] != "gmail-store" {
		t.Fatalf("the yes from Settings is kept as %+v", a)
	}

	// Switched on by a backup: the setup line and the 1.8 line.
	if err := state.WriteLastRun(out, state.LastRun{Status: state.RunOK, Started: now, Finished: now, GmailOn: []string{"gmail-store"}}); err != nil {
		t.Fatal(err)
	}
	set = settingsGmail(e.page())
	if !strings.Contains(set, state.GmailSetupLine+" "+state.GmailNeedsLine) || strings.Contains(set, state.GmailQuestion) {
		t.Fatalf("a switched-on archive's Settings: %q", set)
	}
}
