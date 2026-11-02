package desktop

import (
	"html"
	"os"
	"strings"
	"testing"
)

// covers: MA-1016, R36, S41
// Remove from list is drawn disabled, the refusal's own words as its title, on a row for which
// the route would refuse it (the 1.5.3 friction review's D3): the archive in use while it is
// reachable and still an archive, in Your archives and on the lone "This archive" card, whose
// note then says why in words a person can read without pointing at the button. The route
// still refuses it in the same words. Positive twins first: another archive's Remove from list
// is enabled with its plain title, and a current archive whose folder is gone (which the route
// lets go, §5.2) is offered.
func TestRemoveFromListIsDisabledWhereItWouldBeRefused(t *testing.T) {
	e := newDeleteEnv(t)
	cur := e.plainArchive("Current")
	other := e.plainArchive("Other")
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: cur}); err != nil {
		t.Fatal(err)
	}
	page := e.page()
	unl := rowButton(rowFor(t, page, other), "data-unlist-archive")
	if unl == "" || strings.Contains(unl, "disabled") || !strings.Contains(unl, `title="Take this archive off this list.`) {
		t.Fatalf("the positive twin: another archive's Remove from list is not enabled with its plain title: %s", unl)
	}

	_, refused := e.post("/api/archive/unlist", map[string]any{"path": cur})
	why := replyErr(refused)
	if !strings.Contains(why, "This is the archive in use") {
		t.Fatalf("the route no longer refuses the archive in use: %v", refused)
	}
	unl = rowButton(rowFor(t, page, cur), "data-unlist-archive")
	if !strings.Contains(unl, " disabled") || !strings.Contains(unl, `title="`+why+`"`) {
		t.Errorf("the current archive's Remove from list is not disabled with the route's words as its title (%q): %s", why, unl)
	}

	// The lone current archive: the "This archive" card's button, and its note.
	lone := newDeleteEnv(t)
	only := lone.plainArchive("Only")
	if err := SaveSettings(lone.cfg.SettingsPath, Settings{Out: only}); err != nil {
		t.Fatal(err)
	}
	lp := lone.page()
	if !strings.Contains(lp, `id="sec-this-archive"`) {
		t.Fatal("a lone current archive has no This archive card")
	}
	unl = rowButton(rowFor(t, lp, only), "data-unlist-archive")
	if !strings.Contains(unl, " disabled") || !strings.Contains(unl, `title="`+why+`"`) {
		t.Errorf("the lone archive's Remove from list is not disabled with the route's words as its title: %s", unl)
	}
	note := html.UnescapeString(lp)
	if !strings.Contains(note, `<span id="thisArchiveUnlistWhy"><b>Remove from list</b> is not offered for it now. `+why+`</span>`) ||
		strings.Contains(note, "<b>Remove from list</b> takes it off MailArchive's list") {
		t.Errorf("the This archive card does not say why Remove from list is not offered")
	}

	// The current archive's folder gone: the route lets it go, so it is offered.
	if err := os.RemoveAll(only); err != nil {
		t.Fatal(err)
	}
	lp = lone.page()
	unl = rowButton(rowFor(t, lp, only), "data-unlist-archive")
	if unl == "" || strings.Contains(unl, "disabled") || strings.Contains(lp, `id="thisArchiveUnlistWhy"`) {
		t.Errorf("a current archive whose folder is gone is not offered Remove from list: %s", unl)
	}
}
