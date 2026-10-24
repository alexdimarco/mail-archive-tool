package wizard

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/presync"
)

// covers: MA-574
// A Yes to the wizard's keep-current sync question records job.ThunderbirdSync on
// the written job (presence = ON) with NO profile — derived and membership-
// checked at run time (§4.3/PC3); a No records no sync.
func TestJobForSetsThunderbirdSync(t *testing.T) {
	on := jobFor("s", wizardChoice{out: "/o", inputs: []string{"/in"}, tbSync: true})
	if on.ThunderbirdSync == nil {
		t.Error("tbSync=true must record job.ThunderbirdSync")
	} else if on.ThunderbirdSync.Profile != "" {
		t.Errorf("the wizard must record no profile (derive at run time), got %q", on.ThunderbirdSync.Profile)
	}
	off := jobFor("s", wizardChoice{out: "/o", inputs: []string{"/in"}})
	if off.ThunderbirdSync != nil {
		t.Errorf("tbSync=false must record no sync, got %+v", off.ThunderbirdSync)
	}
}

// covers: MA-574, MA-567
// The schedule-step sync question is built from the single source notice
// (presync.SyncNotice) so the GUI wording cannot drift from the §6 paragraph.
func TestPresyncQuestionDrawsFromNotice(t *testing.T) {
	q := presyncQuestionText()
	if !strings.Contains(q, presync.SyncNotice) {
		t.Error("the wizard sync question must contain presync.SyncNotice (MA-567 coherence)")
	}
}
