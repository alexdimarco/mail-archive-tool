package export

import "testing"

// covers: MA-228, R1, S39
// The bounded #8-residual id-reuse note (design rev-4 §5, EC9) fires ONLY on
// genuine ambiguity — an identity already archived under MORE THAN ONE distinct
// fingerprint — and is deduped per identity per run, so it flags a real reuse
// (where a further distinct reuse could be skipped without capture on the floor)
// without flooding: an ordinary single-sibling skip logs nothing, and the same
// ambiguous identity seen in several folders logs once.
func TestNoteIDReuseFiresOnceOnAmbiguity(t *testing.T) {
	e := &Exporter{}

	e.NoteIDReuse("mid:<x@x>", 1) // the ordinary case — a single sibling — is silent
	if n := countKind(e, "id-reuse"); n != 0 {
		t.Errorf("a single-sibling identity logged %d id-reuse Issue(s), want 0", n)
	}

	e.NoteIDReuse("mid:<x@x>", 2) // ambiguous → logged
	e.NoteIDReuse("mid:<x@x>", 3) // same identity, another folder → deduped
	e.NoteIDReuse("mid:<y@y>", 2) // a different ambiguous identity → its own note
	if n := countKind(e, "id-reuse"); n != 2 {
		t.Errorf("id-reuse Issues = %d, want 2 (one per ambiguous identity, deduped per run)", n)
	}
}

func countKind(e *Exporter, kind string) int {
	n := 0
	for _, is := range e.Issues {
		if is.Kind == kind {
			n++
		}
	}
	return n
}
