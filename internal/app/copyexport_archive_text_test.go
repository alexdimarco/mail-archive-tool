package app

import (
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"

	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/state"
)

// covers: MA-435, MA-531, R20, R29, S52
// The text of a manifest record is bounded where the plan takes it (E4-AO6, second half, found by the
// fresh-eyes check of the fixes): the list cuts the cells of search.db, but a record with no index row
// takes its subject from the manifest itself, and its gap labels and inline-image ids are the manifest's
// too. One record with a 16 MiB subject (a manifest is only bounded in bytes, R-o4) made a plan that held
// the subject whole, a reserve of twelve bytes for each, and a preview that allocated several times its
// size. The plan now takes at most index.MaxListSubjectBytes of a subject and 512 bytes of each label
// (cut at a character), and the reserve and the allocation follow. The positive twin: a record with a
// subject and a gap label inside the bounds is resolved as it was written.
func TestArchiveManifestRecordTextIsBoundedWhereThePlanTakesIt(t *testing.T) {
	a := buildCEArchive(t)
	huge := "x" + strings.Repeat("é", 8<<20) // 16 MiB of two-byte characters, every bound falling inside one
	a.setRec("hello", func(r *state.Record) {
		r.Subject = huge
		r.Missing = []string{huge}
		r.Unresolved = []string{huge}
	})
	a.setRec("second", func(r *state.Record) {
		r.Subject = "a subject inside the bound"
		r.Missing = []string{"report.pdf"}
	})
	s := a.open()
	e := newCEEnv(t)
	keys := []string{a.key("hello"), a.key("second")}

	var m0, m1 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m0)
	p, res := a.planWith(e, s, keys, nil) // no index row: the manifest's own subject
	runtime.ReadMemStats(&m1)
	if len(res.Messages) != 2 {
		t.Fatalf("resolved %d messages, want 2", len(res.Messages))
	}
	by := map[string]CopyMessage{}
	for _, m := range res.Messages {
		by[m.SourceID] = m
	}
	hello, second := by[keys[0]], by[keys[1]]
	if len(hello.Subject) > index.MaxListSubjectBytes || !utf8.ValidString(hello.Subject) || !strings.HasPrefix(hello.Subject, "xéé") {
		t.Errorf("a manifest subject of 16 MiB: %d bytes in the plan (valid UTF-8 %v), want a cut of at most %d bytes at a character", len(hello.Subject), utf8.ValidString(hello.Subject), index.MaxListSubjectBytes)
	}
	for _, n := range hello.Notes {
		if len(n) > 4096 {
			t.Errorf("a gap note of %d bytes in the plan: a label of the manifest is cut, as the subject is", len(n))
		}
	}
	if len(hello.Notes) != 2 {
		t.Errorf("the notes of the hostile record: %d, want its label and its inline image", len(hello.Notes))
	}
	if p.Reserve > 8<<20 {
		t.Errorf("the plan reserves %d bytes for two messages: the reserve is twelve bytes for each byte of text it holds", p.Reserve)
	}
	if got := m1.TotalAlloc - m0.TotalAlloc; got > 12<<20 {
		t.Errorf("resolving and planning one record with 16 MiB of text allocated %d MiB, want a few", got>>20)
	}
	// the positive twin
	if second.Subject != "a subject inside the bound" || len(second.Notes) != 1 || !strings.Contains(second.Notes[0], "report.pdf") {
		t.Errorf("a record inside the bounds: subject %q, notes %q", second.Subject, second.Notes)
	}
}
