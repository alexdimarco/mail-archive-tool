package desktop

import (
	"regexp"
	"strings"
	"testing"

	"mail-archive-tool/internal/archdelete"
)

// covers: MA-1477, MA-1476, R36, S41
// The delete panel's words and what it offers (the 1.7.5 friction review's F7, F8 and F13; the
// adversarial pass's OUT-03), under Node against the page-script harness: the checking line says the
// name can be typed meanwhile; the armed line says the second check and how to stop; a plan that
// needs a decision says what (the server's closed list), and one whose only miss is the typed name
// says that; an encrypted archive's panel offers no Delete when the check finishes and says why; a
// check that loses its connection says so in plain words; and once a delete is sent the page no
// longer holds its token, so leaving the page cannot discard it, while a refusal before it began
// gives the token back. Positive twins first in each scenario.
func TestThePanelSaysWhatItFoundAndOffersOnlyWhatCanApply(t *testing.T) {
	for scenario, want := range map[string]int{"foundWords": 4, "encryptedNoArm": 3, "lostCheck": 2, "sentHoldsNoToken": 3} {
		t.Run(scenario, func(t *testing.T) {
			if passed := runPageScenario(t, scenario); len(passed) != want {
				t.Errorf("%s passed %d of its %d checks: %v", scenario, len(passed), want, passed)
			}
		})
	}
}

// covers: MA-1476, R36
// What needs a decision is said from one closed list (F7): planView sends "decide" beside armable,
// from decideReasons, which armable is made from, so the two cannot disagree. Positive twin first: a
// plain plan sends no "decide" and is armable; an encrypted archive's row is marked
// data-delete-encrypted as the page is drawn (F8), a plain one is not.
func TestTheDecideReasonsNameWhatNeedsADecision(t *testing.T) {
	base := armableBase()
	if _, ok := planView(base)["decide"]; ok || !armable(base) {
		t.Fatal("positive twin: a plain plan sends no decide and is armable")
	}
	for want, set := range map[string]func(p *archdelete.DeletePlan){
		"mail copies this archive did not record": armableKeys["unrecorded"].set,
		"a synced folder":                         armableKeys["synced"].set,
		"scheduled backups to check yourself":     armableKeys["checkYourself"].set,
		"files another program has open":          armableKeys["heldLine"].set,
		"an earlier delete to finish":             armableKeys["finishesLine"].set,
		"an encrypted archive's key":              armableKeys["keyLine"].set,
	} {
		p := armableBase()
		set(p)
		got, _ := planView(p)["decide"].([]string)
		if len(got) != 1 || got[0] != want || armable(p) {
			t.Errorf("decide for %q: %v (armable %v)", want, got, armable(p))
		}
	}
	e := newDeleteEnv(t)
	e.list(backedUpEncrypted(t))
	e.plainArchive("Plainrow")
	page := e.page()
	if n := strings.Count(page, "data-delete-encrypted"); n != 1 {
		t.Errorf("the encrypted row's Delete… is marked data-delete-encrypted, the plain one not: %d marks", n)
	}
}

// covers: MA-1476, MA-1477, R36
// The adversarial pass's OUT-04 (refuted, a structural guard kept): the server checks a plan's
// armable and row only for a request that says armed, so every delete the page sends by itself must
// say armed:true and name the panel's own row. The script holds exactly two posts to
// api/archive/delete: the armed one (armed:true, row:row), sent only by delSend(true) from the plan's
// reply (delFill), and Delete permanently's (the box's answer, no armed), sent only by its click.
// Positive twin first: both posts are found.
func TestEveryAutomaticDeletePostIsArmedWithItsRow(t *testing.T) {
	posts := regexp.MustCompile(`delPost\('api/archive/delete',\{[^}]*\}\)`).FindAllString(dashboardJS, -1)
	if len(posts) == 0 {
		t.Fatal("positive twin: the script posts the delete")
	}
	armed, manual := 0, 0
	for _, p := range posts {
		switch {
		case strings.Contains(p, "armed:true") && strings.Contains(p, "row:row"):
			armed++
		case strings.Contains(p, "unrecorded:!!(box&&box.checked)") && !strings.Contains(p, "armed"):
			manual++
		default:
			t.Errorf("a delete post that is neither the armed one nor Delete permanently's: %s", p)
		}
	}
	if armed != 1 || manual != 1 || len(posts) != 2 {
		t.Errorf("want one armed post and one Delete permanently post, got %d and %d of %d", armed, manual, len(posts))
	}
	if n := strings.Count(dashboardJS, "'api/archive/delete'"); n != 2 {
		t.Errorf("the delete route is named %d times; any other send must be armed with its row", n)
	}
	if strings.Count(dashboardJS, "delSend(false)") != 1 || !strings.Contains(dashboardJS, "go.addEventListener('click',()=>delSend(false))") {
		t.Error("Delete permanently's post is sent only by its click")
	}
	if strings.Count(dashboardJS, "delSend(true)") != 1 || !strings.Contains(jsFuncs(dashboardJS)["delPanel"], "p.armable===true&&delNameMatches(input.value,p.name)){delSend(true)") {
		t.Error("the armed post is sent only from the plan's reply, when armable and the name matches")
	}
	if !strings.Contains(dashboardJS, "fromArm?delPost('api/archive/delete',{token:p.token,typed:input.value,unrecorded:false,drawnFor:drawnFor,armed:true,row:row})") {
		t.Error("delSend(true) sends the armed post")
	}
}
