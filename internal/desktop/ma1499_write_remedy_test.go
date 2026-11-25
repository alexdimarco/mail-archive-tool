package desktop

import (
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/wizard"
)

// covers: MA-1499, S90
// Round B's leftover of the friction review's F2 (docs/design-per-mailbox-archives.md revision 3): when
// this computer did not let MailArchive make a folder in the set's parent, the set stops and the
// archive carries its remedy (wizard.Made.Remedy, the summary's F2 words); the Desktop's note then
// gives that remedy, not "choose the same folder", which would only refuse again. Positive twin first:
// an archive that did not finish for another reason keeps "To finish them, … choose the same folder".
func TestTheNoteForAParentItCouldNotWriteGivesItsRemedy(t *testing.T) {
	g := newSetRig(t)
	a := g.archive("ann@qz-a.example", true)
	_, r := g.add([]wizard.Made{
		setMade(a, "ann@qz-a.example", wizard.MadeFinished, "", g.parent),
		setMade("", "bob@qz-b.example", wizard.MadeFailed, "its folder could not be read", g.parent),
		setMade("", "cy@qz-c.example", wizard.MadeNotStarted, "", g.parent),
	}, nil)
	if !strings.Contains(r.Message, "To finish them, click Make a new archive… again, choose One archive for each mailbox and choose the same folder, "+g.parent+".") {
		t.Fatalf("positive twin: another failure keeps the same-folder words: %q", r.Message)
	}

	h := newSetRig(t)
	const remedy = "Allow MailArchive there (Windows Security, Controlled folder access), or run this again and choose another folder."
	refused := setMade("", "ann@qz-a.example", wizard.MadeFailed, "this computer did not let MailArchive make a folder in "+h.parent, h.parent)
	refused.Remedy = remedy
	_, r = h.add([]wizard.Made{refused, setMade("", "bob@qz-b.example", wizard.MadeNotStarted, "", h.parent)}, nil)
	if !strings.HasSuffix(r.Message, " "+remedy) || strings.Contains(r.Message, "choose the same folder") ||
		!strings.Contains(r.Message, "ann@qz-a.example did not finish: this computer did not let MailArchive make a folder in "+filepath.Clean(h.parent)+".") {
		t.Errorf("the note must give the remedy, not the same folder:\n%s", r.Message)
	}
	if r.OK || !r.Stopped || len(r.Paths) != 0 {
		t.Errorf("nothing was added, and the note stays on the page: %+v", r)
	}
}
