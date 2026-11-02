package desktop

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/archindex"
)

// covers: MA-980, MA-1035, R36, S41
// A row the list's read cannot recognise, but whose manifest the delete's reader reads, is shown
// greyed and still offers Delete, its plan made (§10 [NEW-isarchive-cap], conformance gap 5): the
// rule TestAnArchiveWithAManifestOverOneMiBIsStillOfferedDelete proved until MA-1034 made a large
// manifest recognised (the MA-1034 adversarial pass, finding 2: no test then held a greyed row's
// Delete). The stand-in is a real manifest with one value nested deeper than the list's walk
// allows (64), which state.LoadFrom, the delete's reader, reads. Positive twin first: the same
// archive with its usual manifest is an available row offering Delete.
func TestARowTheListCannotRecogniseIsGreyedAndStillOfferedDelete(t *testing.T) {
	e := newDeleteEnv(t)
	dir := e.plainArchive("Deep")
	rowSays(t, e, dir, true, "")
	if row := rowFor(t, e.page(), dir); strings.Contains(row, "archive-row unavailable") {
		t.Fatalf("the positive twin is greyed: %s", row)
	}

	man := filepath.Join(dir, manifestName)
	data, err := os.ReadFile(man)
	if err != nil {
		t.Fatal(err)
	}
	i := bytes.IndexByte(data, '{')
	if i < 0 {
		t.Fatalf("the fixture: no object in %s", man)
	}
	deep := `"nested":` + strings.Repeat("[", 70) + strings.Repeat("]", 70) + `,`
	nested := append(append(append([]byte(nil), data[:i+1]...), deep...), data[i+1:]...)
	if err := os.WriteFile(man, nested, 0o600); err != nil {
		t.Fatal(err)
	}
	if archindex.IsArchive(dir) {
		t.Fatal("the stand-in: the list's walk must not recognise a value nested 70 deep")
	}

	row := rowFor(t, e.page(), dir)
	if !strings.Contains(row, "archive-row unavailable") {
		t.Fatalf("the stand-in: the row must be greyed: %s", row)
	}
	if !strings.Contains(row, `data-delete-ok="true"`) {
		t.Errorf("a greyed row whose manifest the delete reads does not offer Delete: %s", row)
	}
	if p := e.plan(dir); p["ok"] != true {
		t.Errorf("its plan: %v", p)
	}
}
