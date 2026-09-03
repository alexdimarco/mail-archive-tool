package export

import (
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/state"
)

// covers: MA-89, R6, R4
// The subject slug is the only elastic part of an exported path: under a deep
// folder tree it shrinks (to 24 runes, then to nothing) so the archive-relative
// path stays within the budget, while a normal path keeps the full slug.
func TestPathBudgetShrinksSlug(t *testing.T) {
	out := t.TempDir()
	manifest := mustManifest(t)
	e := incExporter(out, manifest)
	subject := strings.Repeat("Quarterly-Report ", 8) // ≥ 60 runes of slug

	deep := []string{strings.Repeat("a", 100), strings.Repeat("b", 100), strings.Repeat("c", 60)}
	m := &model.Message{Subject: subject, Received: testDate, InternetMessageID: "<deep@x>", PlainBody: "x"}
	if _, err := e.Export("store", deep, m); err != nil {
		t.Fatal(err)
	}
	rec, _ := manifest.Get(state.Key("store", strings.Join(deep, "/"), m.Identity()))
	stem := filepath.Base(rec.Path)
	if strings.Contains(stem, "Quarterly") {
		t.Errorf("deep path kept the full slug: %s", stem)
	}
	if len(stem) > len("2026-06-01_1200_")+8+len(".html") {
		t.Errorf("deep path stem not reduced to date+hash: %s", stem)
	}

	shallow := &model.Message{Subject: subject, Received: testDate, InternetMessageID: "<shallow@x>", PlainBody: "x"}
	if _, err := e.Export("store", []string{"Inbox"}, shallow); err != nil {
		t.Fatal(err)
	}
	rec2, _ := manifest.Get(state.Key("store", "Inbox", shallow.Identity()))
	if !strings.Contains(rec2.Path, "Quarterly-Report") {
		t.Errorf("shallow path lost its slug: %s", rec2.Path)
	}
}
