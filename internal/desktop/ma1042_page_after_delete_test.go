package desktop

import (
	"html"
	"net/http"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/seal"
)

// covers: MA-1042, R36, S41
// The empty page of someone who has archives leads to them (the 1.5.3 re-walk's N12): the guide is
// drawn closed, so Your archives is not a screen and a half below the line that points to it, and
// the first-run words ("Open or make an archive below to begin", "New to MailArchive?") give way to
// "Choose an archive under Your archives below.". Positive twin first: with no archive at all the
// guide opens at Getting started and the first-run words stay.
func TestTheEmptyPageOfSomeoneWithArchivesLeadsToThem(t *testing.T) {
	const pointer = `id="helpPointer"`
	fresh := newDeleteEnv(t)
	fp := fresh.page()
	if h := readHelpCard(t, fp); !h.open["Getting started"] || !strings.Contains(fp, pointer) || !strings.Contains(fp, "Open or make an archive below to begin.") {
		t.Fatalf("the twin: Getting started open %v, pointer %v", h.open["Getting started"], strings.Contains(fp, pointer))
	}

	e := newDeleteEnv(t)
	e.plainArchive("One")
	e.plainArchive("Two")
	p := e.page()
	h := readHelpCard(t, p)
	if h.open["Getting started"] || strings.Contains(p, pointer) || strings.Contains(p, "Open or make an archive below to begin.") ||
		!strings.Contains(p, `<p class="lede">MailArchive keeps a copy of your mail on this computer. Choose an archive under Your archives below.</p>`) ||
		!strings.Contains(p, `id="noArchiveSwitch"`) {
		t.Fatalf("the empty page with archives: Getting started open %v, pointer %v\n%s", h.open["Getting started"], strings.Contains(p, pointer), overviewLede(t, p))
	}
}

// covers: MA-1042, R36, S41
// In Your archives a disabled Remove from list says why as text on its row, not only as a tooltip,
// which a touch screen cannot show (the 1.5.3 re-walk's N13, the operator's delegate's decision):
// "Remove from list is not offered for it now. WHY". Not on a row whose delete did not finish
// (its "Delete not finished" says it), nor on the lone "This archive" card (its note says it).
// Positive twin first: an archive that may be removed from the list says nothing of the kind.
func TestADisabledRemoveFromListSaysWhyOnItsRow(t *testing.T) {
	e := newDeleteEnv(t)
	cur := e.plainArchive("Current")
	other := e.plainArchive("Other")
	half := e.plainArchive("Half")
	stopAt2(t, e, half)
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: cur}); err != nil {
		t.Fatal(err)
	}
	page := e.page()
	if r := rowFor(t, page, other); strings.Contains(r, "data-unlist-why") {
		t.Fatalf("the twin says why: %s", r)
	}
	_, refused := e.post("/api/archive/unlist", map[string]any{"path": cur})
	why := replyErr(refused)
	if why == "" {
		t.Fatalf("the route no longer refuses the archive in use: %v", refused)
	}
	if r := rowFor(t, page, cur); !strings.Contains(r, `<span class="card-sub row-why" data-unlist-why>Remove from list is not offered for it now. `+why+`</span>`) {
		t.Errorf("the current archive's row does not say why: %s", r)
	}
	if r := rowFor(t, page, half); strings.Contains(r, "data-unlist-why") || !strings.Contains(r, "Delete not finished: click Delete again") {
		t.Errorf("a row whose delete did not finish says it twice: %s", r)
	}

	lone := newDeleteEnv(t)
	only := lone.plainArchive("Only")
	if err := SaveSettings(lone.cfg.SettingsPath, Settings{Out: only}); err != nil {
		t.Fatal(err)
	}
	lp := html.UnescapeString(lone.page())
	if strings.Contains(lp, "data-unlist-why") || !strings.Contains(lp, `id="thisArchiveUnlistWhy"`) {
		t.Errorf("the This archive card says why twice, or not at all")
	}
}

// covers: MA-1042, R36, S41
// The page script after a delete or Remove from list (the 1.5.3 re-walk's N12, N14, N20), run
// under Node: Remove from list's words after its reload are headed by the archive's folder, are not
// said a second time on the row, and give way to the next result, a removed row's reason going with it (unlistNote, 4 checks);
// dismissing the result of a delete of the page's own archive draws the page again from its top,
// and another archive's does not (dismissToTop); Save deletion record says what it asked of the
// browser (saveRecordLine). Each scenario checks its positive twin first.
func TestThePageAfterADeleteOrRemoveFromList(t *testing.T) {
	for _, s := range []struct {
		name   string
		checks int
	}{{"unlistNote", 4}, {"dismissToTop", 1}, {"saveRecordLine", 1}} {
		if passed := runPageScenario(t, s.name); len(passed) != s.checks {
			t.Errorf("the %s scenario passed %d of its %d checks: %v", s.name, len(passed), s.checks, passed)
		}
	}
}

// covers: MA-1042, R36
// A refusal because a delete did not finish, which only a tab drawn before that delete is offered,
// tells the tab to reload (the 1.5.3 re-walk's N15: it kept offering Switch and Pause, and only
// "This page is out of date" said to reload): Pause, Resume, a Switch and Remove from list end
// "Reload this page to see its row as it is now.". Positive twin first: another refusal — Remove
// from list of the archive in use — says nothing of reloading.
func TestADeleteNotFinishedRefusalSaysToReload(t *testing.T) {
	const reload = " Reload this page to see its row as it is now."
	e := newDeleteEnv(t)
	cur := e.plainArchive("Current")
	half := e.plainArchive("Half")
	stopAt2(t, e, half)
	parked := e.plainArchive("Parked")
	if err := seal.Seal(parked, "parked"); err != nil {
		t.Fatal(err)
	}
	stopAt2(t, e, parked)
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: cur}); err != nil {
		t.Fatal(err)
	}
	if _, m := e.post("/api/archive/unlist", map[string]any{"path": cur}); replyErr(m) == "" || strings.Contains(replyErr(m), "Reload") {
		t.Fatalf("the twin: %v", m)
	}
	for _, c := range []struct {
		route string
		body  map[string]any
		done  string
		same  func() bool
	}{
		{"/api/pause-archive", map[string]any{"path": half}, "it was not paused", func() bool { return !seal.IsSealed(half) }},
		{"/api/resume-archive", map[string]any{"path": parked}, "it was not resumed", func() bool { return seal.IsSealed(parked) }},
		{"/api/settings", map[string]any{"Out": half}, "it was not made the current archive", func() bool { return e.d.cfg.effectiveOut() == cur }},
		{"/api/archive/unlist", map[string]any{"path": half}, "it was not removed from the list", func() bool { return e.listed(half) }},
	} {
		_, m := e.post(c.route, c.body)
		msg := replyErr(m)
		assure.Refused(t, http.StatusOK, msg, assure.Code(http.StatusOK),
			assure.Names("Delete not finished: click Delete again", c.done, "nothing was changed."+reload),
			assure.NoSideEffect(func() bool { return m["ok"] == false && strings.HasSuffix(msg, reload) && c.same() }))
	}
}
