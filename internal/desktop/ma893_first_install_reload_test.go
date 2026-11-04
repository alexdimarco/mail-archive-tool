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
	if !strings.Contains(save, "if(moved){") || !strings.Contains(save[strings.Index(save, "if(moved){"):], "reloadPage()") {
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
	if !strings.Contains(first, "reloadPage()") {
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
	reload := strings.Index(first, "if(kept){say(parts.join(' '));setTimeout(()=>reloadPage(),600);return true;}")
	if set < 0 || reload < 0 || set > reload {
		t.Fatalf("a first install's note is not kept before the reload:\n%s", first)
	}
	if strings.Count(first, "reloadPage()") != 1 {
		t.Errorf("a first install reloads somewhere the note is not kept:\n%s", first)
	}
	if !strings.Contains(first[reload:], "Reload the page to see Remove, Repair and Run now") || !strings.Contains(first[reload:], "schedState();") {
		t.Errorf("where the note can't be kept, the page does not keep it and say how to see the buttons:\n%s", first)
	}
	load := "try{const kept=sessionStorage.getItem(keptNoteKey);if(kept){sessionStorage.removeItem(keptNoteKey);const n=$('settingsNote');if(n){n.hidden=false;n.textContent=kept;}}}catch(e){}"
	if !strings.Contains(dashboardJS, load) {
		t.Error("the page does not show a kept note when it is drawn, or does not forget it")
	}
	// The two others are a deletion record's saved mark (MA-986), each guarded too.
	for _, guarded := range []string{"try{if(sessionStorage.getItem(delSavedKey(v.id))==='1')h.dataset.saved='1';}catch(e){}",
		"try{sessionStorage.setItem(delSavedKey(v.id),'1');}catch(e){}"} {
		if !strings.Contains(dashboardJS, guarded) {
			t.Errorf("the page script's deletion-record mark is not guarded: %q missing", guarded)
		}
	}
	// And Remove from list's words kept for its reload (MA-1020, amended 2026-11-02): its set, its
	// read and its removal, each inside a try, named here so each new call is checked as guarded.
	// Amended 2026-11-02 (MA-1042, the 1.5.3 re-walk's N12, N14): the note is kept with its folder,
	// and the page drawn after a Dismiss is asked to start at its top — three more calls, each named.
	for _, guarded := range []string{"try{sessionStorage.setItem(delUnlistedKey,JSON.stringify({path:b.dataset.path,said:said}));}catch(e){}",
		"try{const kept=sessionStorage.getItem(delUnlistedKey);\n  if(kept){sessionStorage.removeItem(delUnlistedKey);",
		"{try{sessionStorage.setItem(delTopKey,'1');}catch(e){}}",
		"try{if(sessionStorage.getItem(delTopKey)){sessionStorage.removeItem(delTopKey);"} {
		if !strings.Contains(dashboardJS, guarded) {
			t.Errorf("the page script's kept Remove from list note is not guarded: %q missing", guarded)
		}
	}
	if !strings.Contains(dashboardJS, "else{const top=$('heldDeletes');if(top)top.before(note);}}\n}catch(e){}") {
		t.Error("the kept Remove from list note's read is not closed by its catch")
	}
	// Amended 2026-11-03 (MA-1052, design-archive-delete §15.4's DM-17): Open an archive folder… keeps
	// the server's words for a folder it listed across the page drawn again, through the same kept
	// note (read above) — one more call, named here and guarded.
	if !strings.Contains(dashboardJS, "try{sessionStorage.setItem(keptNoteKey,d.note);}catch(e){}") {
		t.Error("the page script's kept Open an archive folder… note is not guarded")
	}
	// Amended 2026-11-03 (MA-1065, the 1.5.6 friction review's note B): the page drawn again after
	// Dismiss or Remove from list is asked to start at Your archives — three more calls (its set, its
	// read and its removal), each named here and guarded.
	for _, guarded := range []string{"function delBackToArchives(){try{sessionStorage.setItem(delArchivesKey,'1');}catch(e){}}",
		"try{if(sessionStorage.getItem(delArchivesKey)){sessionStorage.removeItem(delArchivesKey);"} {
		if !strings.Contains(dashboardJS, guarded) {
			t.Errorf("the page script's back-to-Your-archives mark is not guarded: %q missing", guarded)
		}
	}
	if !strings.Contains(dashboardJS, "if(window.addEventListener)window.addEventListener('load',()=>setTimeout(go,0));}}}catch(e){}") {
		t.Error("the back-to-Your-archives read is not closed by its catch")
	}
	// Amended 2026-11-03 (MA-1070): Open an archive folder… asks the page drawn next to bring an
	// encrypted archive's Unlock box in — three more calls (its set, its read and its removal), each
	// named here and guarded.
	for _, guarded := range []string{"try{sessionStorage.setItem(stepUnlockKey,'1');}catch(e){}",
		"try{if(sessionStorage.getItem(stepUnlockKey)){sessionStorage.removeItem(stepUnlockKey);"} {
		if !strings.Contains(dashboardJS, guarded) {
			t.Errorf("the page script's step-unlock mark is not guarded: %q missing", guarded)
		}
	}
	if !strings.Contains(dashboardJS, "if(window.addEventListener)window.addEventListener('load',()=>setTimeout(go,0));}}}catch(e){}\n\n// \"Use another browser") {
		t.Error("the step-unlock read is not closed by its catch")
	}
	if n := strings.Count(dashboardJS, "sessionStorage."); n != 18 {
		t.Errorf("the page script calls storage %d times, want the 18 guarded ones", n)
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
