package app

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/state"
)

// covers: MA-418, MA-435, R4, R6, R29
// The archive source's sibling folders (MA-418's E4 half): two folders of one store whose names differ
// only in case, `Work` and `work`, are two nodes of the tree, and the export gives them two distinct
// directories (the second takes a "~" and a hash), deterministic across two runs, so neither
// overwrites the other on a case-insensitive disk.
func TestArchiveSourceSiblingFoldersKeepApart(t *testing.T) {
	a := buildCEArchive(t)
	a.setRec("hello", func(r *state.Record) { r.Folder = "Work" })
	a.setRec("second", func(r *state.Record) { r.Folder = "work" })
	s := a.open()
	names := map[string]int{}
	for _, n := range s.Tree() {
		names[n.ID] = n.Count
	}
	if names["alice/Work"] != 1 || names["alice/work"] != 1 {
		t.Fatalf("the tree: %v, want Work and work as two folders", names)
	}
	dirs := func() []string {
		e := newCEEnv(t)
		p := a.plan(e, s, []string{a.key("hello"), a.key("second")})
		res := a.run(e, p, s)
		if res.Counts.Exported != 2 {
			t.Fatalf("the run: %+v", res.Counts)
		}
		var out []string
		for _, f := range manifestOf(t, res.Root)["messages"].([]any) {
			for _, fl := range f.(map[string]any)["files"].([]any) {
				out = append(out, fl.(map[string]any)["path"].(string))
			}
		}
		return out
	}
	first, second := dirs(), dirs()
	if len(first) != 2 {
		t.Fatalf("files: %v", first)
	}
	dirOf := func(p string) string { return p[:strings.LastIndex(p, "/")] }
	// the month directory holds the files; the folder directory is its parent
	parent := func(p string) string { d := dirOf(p); return d[:strings.LastIndex(d, "/")] }
	if strings.EqualFold(parent(first[0]), parent(first[1])) {
		t.Errorf("Work and work share a directory when case is folded: %v", first)
	}
	for i := range first {
		if dirOf(first[i]) != dirOf(second[i]) {
			t.Errorf("the names are not deterministic across two runs: %v then %v", first, second)
		}
	}
}
