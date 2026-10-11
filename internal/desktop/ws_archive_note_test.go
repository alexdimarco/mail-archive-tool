package desktop

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/state"
)

// dyingContext is a context that is cancelled after its Err has been asked n times: a cancel that comes in
// the middle of a read, where a context cancelled before it starts shows only the first look.
type dyingContext struct {
	asked atomic.Int64
	n     int64
}

func (c *dyingContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (c *dyingContext) Done() <-chan struct{}       { return nil }
func (c *dyingContext) Value(any) any               { return nil }
func (c *dyingContext) Err() error {
	if c.asked.Add(1) > c.n {
		return context.Canceled
	}
	return nil
}

// covers: MA-531, MA-435, MA-512, R29, S52
// The words for a search.db that cannot be used name the right archive (E4-AI8, re-verified): the
// corrupt-index variant was held by no test, so the line for another archive folder (list and preview) could
// say "Rebuild search index fixes the list", an action on the configured archive, and nothing failed. A
// search.db of another archive folder that is not a database says it can't be used (list) or read (preview) and
// sends the owner nowhere; the configured archive with the same file is told to rebuild it (the positive twin).
func TestArchiveCorruptIndexWordsNameTheRightArchive(t *testing.T) {
	e := newWsEnv(t)
	f := buildArchiveAt(t, e.out)
	other := filepath.Join(e.dir, "inherited")
	g := buildArchiveAt(t, other)
	garbage := []byte(strings.Repeat("this is not a database ", 400))
	for _, p := range []string{f.path("search.db"), g.path("search.db")} {
		for _, side := range []string{"-wal", "-shm"} {
			os.Remove(p + side)
		}
		if err := os.WriteFile(p, garbage, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []struct {
		name    string
		src     map[string]any
		rebuild bool
	}{{"the configured archive", archSource("archive", e.out), true}, {"another archive folder", archSource("archive-other", other), false}} {
		e.archFolders(c.src)
		_, out := e.archRows(c.src, "alice")
		list, _ := out["listUnavailable"].(string)
		if !strings.Contains(list, "search index can't be used") || strings.Contains(list, "Rebuild") != c.rebuild {
			t.Errorf("%s, the list of an index that is not a database: %q, want it said it can't be used, and Rebuild named: %v", c.name, list, c.rebuild)
		}
		_, res := e.preview(e.archSel(c.src, entry("alice", false)))
		warn := strings.Join(toStrings(res["warnings"]), " | ")
		if !strings.Contains(warn, "search index can't be read") || strings.Contains(warn, "Rebuild") != c.rebuild {
			t.Errorf("%s, the preview with an index that is not a database: %q, want it said it can't be read, and Rebuild named: %v", c.name, warn, c.rebuild)
		}
	}
}

// covers: MA-435, MA-531, R29, S52
// The preview's read of the index stops at the next row when the preview is cancelled in the middle of it
// (E4-AC7, re-verified): the test of a context cancelled before the read began stays green when the check
// in the row loop is removed, because the entry and the final look catch it, and it is that check that stops
// a read of a million rows. This context is cancelled after its second look: the entry look and the first
// row pass, and the second row's look stops the read, with the rows read until then and no more.
func TestArchiveMetaStopsInTheMiddleOfTheReadWhenThePreviewIsCancelled(t *testing.T) {
	e := newWsEnv(t)
	buildArchiveAt(t, e.out)
	a, err := app.OpenArchive(e.out, app.ArchiveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	var ids []string
	for _, n := range a.Tree() {
		ids = append(ids, n.ID)
	}
	meta, _, err := e.d.archiveMeta(&dyingContext{n: 1 << 30}, a, ids) // the positive twin: nobody cancels
	if err != nil || len(meta) != 6 {
		t.Fatalf("the positive twin: %d rows, %v", len(meta), err)
	}
	c := &dyingContext{n: 2}
	meta, _, err = e.d.archiveMeta(c, a, ids)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("a preview cancelled in the middle of the read of the index: %v, want context.Canceled", err)
	}
	if len(meta) != 0 {
		t.Errorf("the cancelled read returned %d rows", len(meta))
	}
	if got := c.asked.Load(); got != 4 { // the entry, the first row, the second row (where it stopped), and the look that names the cause
		t.Errorf("the read looked at the context %d times, want 4: it stops at the row after the cancel, not at the end", got)
	}
}

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
