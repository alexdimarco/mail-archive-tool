package desktop

import (
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/state"
)

// covers: MA-1216, R45, S82, S83
// The archive card is one of the four places that say a newer format's words (fold check 6's
// S7; docs/design-gmail-dedup.md §7.4): an archive at version 8 (a newer MailArchive part-way
// through tidying it) says the tidy-up words, one at version 9 "needs a newer MailArchive", in
// place of "Not reachable right now", and neither offers Switch. Positive twin first: a version-7
// archive, which this build reads, is a normal row. The card reads only; the manifests are
// unchanged.
func TestTheCardSaysTheTidyUpWordsForVersion8(t *testing.T) {
	write := func(v string) (string, []byte) {
		t.Helper()
		dir := t.TempDir()
		body := []byte(`{"version":` + v + `,"entries":{},"stores":{"path:/m":"a"}}`)
		if err := os.WriteFile(filepath.Join(dir, ".mailarchive-manifest.json"), body, 0o644); err != nil {
			t.Fatal(err)
		}
		return dir, body
	}
	v7, _ := write("7")
	v8, body8 := write("8")
	v9, _ := write("9")
	rows := Config{SettingsPath: filepath.Join(t.TempDir(), "s.json")}.archiveRows(
		archindex.Index{Entries: []archindex.Entry{{Path: v7}, {Path: v8}, {Path: v9}}})
	by := map[string]ArchiveRow{}
	for _, r := range rows {
		by[r.Path] = r
	}
	if r := by[v7]; !r.Available || r.Problem != "" || !r.OffersSwitch() {
		t.Fatalf("positive twin: a version-7 archive's row: %+v", r)
	}
	if r := by[v8]; r.Problem != state.TidyUpWords || r.OffersSwitch() {
		t.Fatalf("a version-8 archive's row says %q (switch offered %v), want the tidy-up words", r.Problem, r.OffersSwitch())
	}
	if r := by[v9]; r.Problem != state.NewerWords || r.OffersSwitch() {
		t.Fatalf("a version-9 archive's row says %q (switch offered %v), want the newer words", r.Problem, r.OffersSwitch())
	}
	if after, _ := os.ReadFile(filepath.Join(v8, ".mailarchive-manifest.json")); string(after) != string(body8) {
		t.Fatal("rendering the card changed a version-8 manifest")
	}
}
