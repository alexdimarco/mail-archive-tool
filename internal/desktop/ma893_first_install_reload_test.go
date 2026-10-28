package desktop

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/schedule"
)

// firstInstallBranch is the page script's doSave branch that posts a changed schedule.
func firstInstallBranch(t *testing.T) string {
	t.Helper()
	i, j := strings.Index(dashboardJS, "async function doSave("), strings.Index(dashboardJS, "async function schedPost(")
	if i < 0 || j < i {
		t.Fatal("the page script has no doSave before schedPost")
	}
	save := dashboardJS[i:j]
	if !strings.Contains(save, "if(moved){") || !strings.Contains(save[strings.Index(save, "if(moved){"):], "location.reload()") {
		t.Fatal("the positive twin: a location save no longer reloads")
	}
	k := strings.Index(save, "if(schedChanged()){")
	if k < 0 {
		t.Fatal("doSave no longer posts a changed schedule")
	}
	return save[k:]
}

// covers: MA-893, R37
// Structural (the page script): after Settings sets up a schedule where none was installed, the
// page is drawn again, so the Scheduled backup card shows its Remove, Repair and Run now in place
// of "No scheduled backup is set up" (the part A pass); an edit of an installed schedule keeps
// updating the state line in place. Positive twin first: a location save reloads, as before.
func TestAFirstInstallFromSettingsRedrawsTheCard(t *testing.T) {
	branch := firstInstallBranch(t)
	w := strings.Index(branch, "const wasNone=")
	p := strings.Index(branch, "await schedPost()")
	r := strings.Index(branch, "if(r.ok&&wasNone){")
	if w < 0 || p < 0 || r < 0 || !(w < p && p < r) {
		t.Fatalf("a first install does not note that nothing was installed before posting, and act on it after:\n%s", branch)
	}
	first := branch[r:strings.Index(branch, "if(r.ok){")]
	if !strings.Contains(first, "location.reload()") {
		t.Errorf("a first install does not draw the page again:\n%s", first)
	}
	if !strings.Contains(branch, "schedState();") {
		t.Error("an edit of an installed schedule no longer updates the state line")
	}
}

// covers: MA-893, R37
// A first install's note stays in sight (the part A re-verification, 2026-10-28): the reply's note
// (Hourly's cost of one email an hour, a scheduled verify's skips, a recreated record) is carried
// across the reload that draws the card again — kept for this tab, read back into the Settings note
// when the page is drawn, then forgotten — and the reload waits for it to be kept; where the browser
// keeps nothing, the page is not drawn again on a timer, the note stays, and it says how to see the
// card's buttons. Every storage call is guarded, so a browser that refuses storage still draws the
// page. Positive twin first: a first install with no note reloads, as before.
func TestAFirstInstallKeepsItsNoteAcrossTheRedraw(t *testing.T) {
	branch := firstInstallBranch(t)
	r := strings.Index(branch, "if(r.ok&&wasNone){")
	first := branch[r:strings.Index(branch, "if(r.ok){")]
	if !strings.Contains(first, "let kept=!r.note;") {
		t.Fatalf("the positive twin: a first install with no note no longer reloads at once:\n%s", first)
	}
	set := strings.Index(first, "try{sessionStorage.setItem(keptNoteKey,parts.join(' '));kept=true;}catch(e){}")
	reload := strings.Index(first, "if(kept){say(parts.join(' '));setTimeout(()=>location.reload(),600);return true;}")
	if set < 0 || reload < 0 || set > reload {
		t.Fatalf("a first install's note is not kept before the reload:\n%s", first)
	}
	if strings.Count(first, "location.reload()") != 1 {
		t.Errorf("a first install reloads somewhere the note is not kept:\n%s", first)
	}
	if !strings.Contains(first[reload:], "Reload the page to see Remove, Repair and Run now") || !strings.Contains(first[reload:], "schedState();") {
		t.Errorf("where the note can't be kept, the page does not keep it and say how to see the buttons:\n%s", first)
	}
	load := "try{const kept=sessionStorage.getItem(keptNoteKey);if(kept){sessionStorage.removeItem(keptNoteKey);const n=$('settingsNote');if(n){n.hidden=false;n.textContent=kept;}}}catch(e){}"
	if !strings.Contains(dashboardJS, load) {
		t.Error("the page does not show a kept note when it is drawn, or does not forget it")
	}
	if n := strings.Count(dashboardJS, "sessionStorage."); n != 3 {
		t.Errorf("the page script calls storage %d times, want the 3 guarded ones", n)
	}
}

// covers: MA-893, MA-876, R37
// "Was installed" is its own page fact (the part A re-verification): an installed capture whose
// recorded cadence can't be read draws no installed interval (MA-876) but data-was-installed="1",
// and the script reads either, so choosing Daily there is an edit ("Backup schedule updated."),
// never a first install. Positive twin first: with nothing installed the fact is empty.
func TestAnEditOfAnUnreadableCadenceIsNotAFirstInstall(t *testing.T) {
	g := captureRig(t)
	if s := cardOf(g.page(), "sec-settings"); !strings.Contains(s, `data-was-installed=""`) {
		t.Fatalf("the positive twin: nothing installed, yet the page says something was:\n%s", s)
	}
	writeCaptureDescriptor(t, g.out, schedule.Weekly, "05:00")
	g.fake.tasks[g.out] = true
	if s := cardOf(g.page(), "sec-settings"); !strings.Contains(s, `data-installed-interval="weekly"`) || !strings.Contains(s, `data-was-installed="1"`) {
		t.Fatalf("an installed capture does not draw that it was installed:\n%s", s)
	}
	if err := schedule.RemoveDescriptor(g.out); err != nil {
		t.Fatal(err)
	}
	away := g.out + "-moved-away"
	if err := SaveSettings(g.settings, Settings{Out: g.out, BackupFor: away}); err != nil {
		t.Fatal(err)
	}
	g.fake.tasks = map[string]bool{away: true}
	s := cardOf(g.page(), "sec-settings")
	if !strings.Contains(s, `<option value="" selected>As installed (can`) || !strings.Contains(s, `data-installed-interval=""`) || !strings.Contains(s, `data-was-installed="1"`) {
		t.Fatalf("an installed capture whose cadence can't be read does not draw that it was installed:\n%s", s)
	}
	if !strings.Contains(firstInstallBranch(t), "const wasNone=!(schedFields&&(schedFields.dataset.installedInterval||schedFields.dataset.wasInstalled==='1'));") {
		t.Error("the script's first-install test does not read the page's was-installed fact")
	}
}
