package desktop

import (
	"strings"
	"testing"
)

// covers: MA-893, R37
// Structural (the page script): after Settings sets up a schedule where none was installed, the
// page is drawn again, so the Scheduled backup card shows its Remove, Repair and Run now in place
// of "No scheduled backup is set up" (the part A pass); an edit of an installed schedule keeps
// updating the state line in place. Positive twin first: a location save reloads, as before.
func TestAFirstInstallFromSettingsRedrawsTheCard(t *testing.T) {
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
	branch := save[k:]
	w := strings.Index(branch, "const wasNone=!(schedFields&&schedFields.dataset.installedInterval);")
	p := strings.Index(branch, "await schedPost()")
	r := strings.Index(branch, "if(r.ok&&wasNone){")
	if w < 0 || p < 0 || r < 0 || !(w < p && p < r) {
		t.Fatalf("a first install does not note that nothing was installed before posting, and act on it after:\n%s", branch)
	}
	if end := strings.Index(branch[r:], "}"); end < 0 || !strings.Contains(branch[r:r+end], "location.reload()") {
		t.Errorf("a first install does not draw the page again:\n%s", branch[r:])
	}
	if !strings.Contains(branch, "schedState();") {
		t.Error("an edit of an installed schedule no longer updates the state line")
	}
}
