package desktop

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// covers: MA-1017, R36, S41
// After a delete of the page's own archive that got past step 4, while its result is open, the
// page's controls for that archive do nothing (the 1.5.3 friction review's H4): the page script
// says "This archive was deleted: click Dismiss on its result to refresh this page." at the top
// and at the top of each of its cards, greys them and makes them inert, clears the switch note
// about it and takes the sidebar's reader and Encryption links away. Your archives (with the
// result), This archive, Add another archive, the guide, Security & privacy and Uninstall are not
// the archive's and stay. Positive twin first (in the scenario): a delete of another archive
// changes none of it.
func TestTheDeletedArchivesPageControlsDoNothingUntilDismissed(t *testing.T) {
	if passed := runPageScenario(t, "deletedCurrent"); len(passed) != 7 {
		t.Errorf("the deletedCurrent scenario passed %d of its 7 checks: %v", len(passed), passed)
	}
	// The cards the script greys are the ones the page draws, and none of those that stay.
	m := regexp.MustCompile(`const delArchiveCards=\[([^\]]*)\]`).FindStringSubmatch(dashboardJS)
	if m == nil {
		t.Fatal("the page script names no cards to grey")
	}
	src, err := os.ReadFile("dashboard.go")
	if err != nil {
		t.Fatal(err)
	}
	ids := regexp.MustCompile(`'([^']+)'`).FindAllStringSubmatch(m[1], -1)
	if len(ids) < 10 {
		t.Fatalf("the page script greys only %d cards: %s", len(ids), m[1])
	}
	for _, id := range ids {
		if !strings.Contains(string(src), `class="card" id="`+id[1]+`"`) {
			t.Errorf("the page script greys %s, a card the Overview does not draw", id[1])
		}
	}
	for _, keep := range []string{"sec-archives", "sec-this-archive", "sec-add-archive", "sec-uninstall", "sec-help"} {
		if strings.Contains(m[1], "'"+keep+"'") {
			t.Errorf("the page script greys %s, which is not the deleted archive's", keep)
		}
	}
}

// covers: MA-1017, R36, S41
// With no archive chosen but archives listed — after a delete of the archive in use, once its
// result is dismissed — the "No archive chosen yet" card says to switch to one of them, not that
// MailArchive Desktop has no archive (the 1.5.3 friction review's H4). Positive twin first: with
// no archive listed it still says MailArchive Desktop has no archive yet.
func TestTheEmptyPageSaysToSwitchWhenArchivesAreListed(t *testing.T) {
	const none = "MailArchive Desktop has no archive yet."
	const sw = `<span id="noArchiveSwitch">No archive is chosen yet. <b>Your archives</b>, below, lists the archives MailArchive knows on this computer: to use one, click <b>Switch to this archive</b> on its row.`
	empty := newDeleteEnv(t)
	if p := empty.page(); !strings.Contains(p, none) || strings.Contains(p, "noArchiveSwitch") {
		t.Fatal("the positive twin: with no archive listed the page does not say it has none")
	}

	e := newDeleteEnv(t)
	cur := e.plainArchive("Current")
	e.plainArchive("Other")
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: cur}); err != nil {
		t.Fatal(err)
	}
	if r := e.planAndDelete(cur); r["status"] != "complete" {
		t.Fatalf("the delete of the current archive: %v", r)
	}
	if s := LoadSettings(e.cfg.SettingsPath); s.Out != "" {
		t.Fatalf("the delete left %q the archive in use", s.Out)
	}
	p := e.page()
	if !strings.Contains(p, sw) || strings.Contains(p, none) {
		t.Errorf("with archives listed and none chosen, the page does not say to switch to one")
	}
	if !strings.Contains(p, `id="sec-archives"`) || !strings.Contains(p, "data-switch-archive") {
		t.Error("the Switch to this archive the page points at is not drawn")
	}
}
