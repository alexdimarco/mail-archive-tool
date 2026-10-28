package desktop

import (
	"encoding/json"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// removePost posts the card's Remove as the page script sends it, naming the folder the page was
// drawn for.
func (g *a3Rig) removePost(drawnFor string) string {
	g.t.Helper()
	b, _ := json.Marshal(map[string]any{"drawnFor": drawnFor})
	return postJSON(g.h, "/api/schedule-remove", g.csrf, string(b)).Body.String()
}

// covers: MA-892, MA-860, R37
// The card's Remove honours the folder its page was drawn for (C-12; the part A pass): a tab left
// open from another archive is refused with "This page is out of date; reload it." before
// anything is removed — the scheduler entry, the job file, the archive's record and the settings
// file unchanged. Positive twin first: a Remove drawn for this archive removes its entry and job
// file; a post naming no folder (an older page) still removes.
func TestAStaleTabsRemoveChangesNothing(t *testing.T) {
	isolateHome(t)
	g := newA3Rig(t, tbProfileArchive(t), "darwin")
	create := func() {
		if body := g.post(`{"interval":"weekly","time":"03:00"}`); !strings.Contains(body, `"ok":true`) {
			t.Fatalf("create: %s", body)
		}
		g.recordLast()
	}
	create()
	if body := g.removePost(g.out); !strings.Contains(body, `"ok":true`) || g.fake.tasks[g.out] || fileBytes(g.jobPath) != nil {
		t.Fatalf("the positive twin, Remove drawn for this archive: %s (tasks %v)", body, g.fake.tasks)
	}

	create()
	u := g.snapshot()
	other := t.TempDir()
	body := g.removePost(other)
	assure.Refused(t, refusedCode(body), body,
		assure.Names(staleRefusal),
		assure.NoSideEffect(func() bool { return u.holds() && g.fake.tasks[g.out] && fileBytes(g.jobPath) != nil }))

	if body := postJSON(g.h, "/api/schedule-remove", g.csrf, `{}`).Body.String(); !strings.Contains(body, `"ok":true`) || g.fake.tasks[g.out] {
		t.Fatalf("a post naming no folder: %s", body)
	}
}
