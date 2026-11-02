package desktop

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/seal"
)

// covers: MA-1017, R36, S41
// With no archive chosen, the empty page points at Switch to this archive only when a row of Your
// archives offers it (the 1.5.3 friction fixes' adversarial pass, L1): when the only listed
// archive's delete did not finish, or it is paused, no row offers Switch, and the page says none
// can be switched to now and that each row says why, never "click Switch to this archive on its
// row". Positive twin first: with an archive that can be switched to, the page points at its
// Switch, which is drawn.
func TestTheEmptyPagePointsAtSwitchOnlyWhenARowOffersIt(t *testing.T) {
	const sw = `<span id="noArchiveSwitch">`
	const none = `<span id="noArchiveNoSwitch">No archive is chosen yet. <b>Your archives</b>, below, lists the archives MailArchive knows on this computer, but none can be switched to now: each row says why.`
	ok := newDeleteEnv(t)
	ok.plainArchive("Usable")
	if p := ok.page(); !strings.Contains(p, sw) || !strings.Contains(p, "data-switch-archive") || strings.Contains(p, "noArchiveNoSwitch") {
		t.Fatal("the positive twin: with an archive that can be switched to, the page does not point at its Switch")
	}

	halfway := newDeleteEnv(t)
	stopAt2(t, halfway, halfway.plainArchive("Halfway"))
	paused := newDeleteEnv(t)
	if err := seal.Seal(paused.plainArchive("Paused"), "paused for the test"); err != nil {
		t.Fatal(err)
	}
	for name, e := range map[string]*delEnv{"a delete not finished": halfway, "paused": paused} {
		p := e.page()
		if strings.Contains(p, "data-switch-archive") {
			t.Fatalf("%s: the row offers Switch", name)
		}
		if strings.Contains(p, sw) || !strings.Contains(p, none) {
			t.Errorf("%s: the empty page points at a Switch to this archive no row offers", name)
		}
	}
}
