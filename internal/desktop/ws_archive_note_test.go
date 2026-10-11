package desktop

import (
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/state"
)

// covers: MA-435, MA-531, R20, R29, S52
// The note for rows the loaded tree does not know names causes and remedies that are true (E4-AC5, re-verified): it said
// a backup or capture ran since and that Load folders again includes the messages, which is untrue for rows
// whose records the manifest no longer lists (a record removed from the archive, the index not yet rebuilt):
// loading again never includes them, and the note came back every time. When the manifest is as it was loaded the
// note says it does not list them, and names both causes with the remedy of each: a capture still running (the
// index is written ahead of the manifest; Load folders again when it has finished) and a message removed from the
// archive (it stays in the index until it is brought in step: for the configured archive, Rebuild search index; for
// another archive folder, which that does not act on, nothing is advised). When the manifest did change since it was
// loaded, the earlier wording stands (the positive twin, TestArchiveListSaysWhenTheArchiveChangedSinceItWasLoaded).
func TestArchiveListNoteForRowsTheManifestDoesNotListIsTrue(t *testing.T) {
	for _, c := range []struct {
		name    string
		kind    string
		rebuild bool
	}{{"the configured archive", "archive", true}, {"another archive folder", "archive-other", false}} {
		t.Run(c.name, func(t *testing.T) {
			e := newWsEnv(t)
			dir := e.out
			if c.kind == "archive-other" {
				dir = filepath.Join(e.dir, "inherited")
			}
			f := buildArchiveAt(t, dir)
			key := f.key("hello")
			f.edit(func(m *state.Manifest) { m.Delete(key) }) // the record is gone from the manifest, the index still lists it
			arch := archSource(c.kind, dir)
			e.archFolders(arch) // loaded after the removal: the archive does not change from here
			_, out := e.archRows(arch, "alice")
			n, _ := out["listNote"].(string)
			if !strings.Contains(n, "not shown") || !strings.Contains(n, "manifest, as you loaded it, does not list them") || !strings.Contains(n, "removed from the archive") {
				t.Errorf("the note for a row the manifest does not list: %q, want it said the manifest does not list it, and that a removed message stays in the index", n)
			}
			if strings.Contains(n, "backup or capture ran") || strings.Contains(n, "changed since") {
				t.Errorf("the note for a row the manifest does not list: %q, want no claim that the archive changed (it did not)", n)
			}
			if got := strings.Contains(n, "Rebuild search index"); got != c.rebuild {
				t.Errorf("the note %q names Rebuild search index: %v, want %v", n, got, c.rebuild)
			}
		})
	}
}
