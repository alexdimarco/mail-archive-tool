package desktop

import (
	"testing"

	"mail-archive-tool/internal/archdelete"
)

// covers: MA-1039, R36, S41
// The delete panel of a plan that finishes a delete warns when a file of it is still held by
// another program (the 1.5.3 re-walk's N17): the plan's reply carries heldLine, and the panel
// shows it right after its finishing line. Positive twin first: none held, an empty heldLine.
func TestThePanelWarnsOfAFileStillHeld(t *testing.T) {
	p := &archdelete.DeletePlan{Folder: "/a/H", Name: "H", Kind: archdelete.Plain}
	if v, _ := planView(p)["heldLine"].(string); v != "" {
		t.Fatalf("the twin: %q", v)
	}
	p.Held = []string{"x.html"}
	if v, _ := planView(p)["heldLine"].(string); v != p.HeldLine() || v == "" {
		t.Fatalf("heldLine %q, want %q", v, p.HeldLine())
	}
	if passed := runPageScenario(t, "heldLine"); len(passed) != 1 {
		t.Errorf("the heldLine scenario passed %d of its 1 check: %v", len(passed), passed)
	}
}
