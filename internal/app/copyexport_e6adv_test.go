package app

import (
	"strings"
	"testing"
)

// The tombstones of the EW-E6 adversarial pass (docs/review-extraction-workspace-e6-adversarial.md):
// each is the regression test of one confirmed finding, written red against the code at db48838
// and green with its fix. Each cites the catalog row that states the guarantee it keeps.

// e6advZipRun exports the archive's zip-only record with the entries as its attachments zip, under
// the bounds in e, and returns the result, the manifest entry and what the run wrote into
// attachments directories (expanded or not, kept or not).
func e6advZipRun(t *testing.T, e *CopyExportEnv, entries ...e6ZipEntry) (res *CopyResult, ent map[string]any, written int64) {
	t.Helper()
	a := buildE6Archive(t)
	e.fsWrap = func(f ceFS) ceFS { return &e6Counting{ceFS: f, n: &written} }
	res, ent, _ = e6WithZip(t, a, e, e6Zip(t, entries...))
	if res.Counts.Exported != 1 || res.Status != CopyStatusComplete {
		t.Fatalf("the run: %s, %+v", res.Label, res.Counts)
	}
	return res, ent, written
}

// e6advNote reports whether an entry carries an attachment note holding every one of the phrases.
func e6advNote(ent map[string]any, phrases ...string) bool {
	for _, n := range ent["notes"].([]any) {
		s := n.(string)
		ok := strings.HasPrefix(s, attachNotePrefix)
		for _, p := range phrases {
			ok = ok && strings.Contains(s, p)
		}
		if ok {
			return true
		}
	}
	return false
}

// covers: MA-424, R1
// E6-AO1 (E6-AC5): an entry that streams its whole size and then fails (a damaged checksum) is
// charged against the message's total like one that succeeds, so the per-message bound limits what
// is written, not only what is kept. Eight entries of 512 KiB that all fail, under a 1.5 MiB total:
// no more than 1.5 MiB reaches the destination (three entries) and the rest are notes that the
// total would be passed, where the code charged only the entries that succeeded and wrote all 4 MiB.
func TestFailedZipEntriesCountTowardThePerMessageTotal(t *testing.T) {
	e := newCEEnv(t)
	e.zipLimit = attachBounds{totalBytes: 3 << 19}
	var entries []e6ZipEntry
	for i := 0; i < 8; i++ {
		entries = append(entries, e6ZipEntry{name: "bad" + string(rune('a'+i)) + ".bin", data: append([]byte{byte(i + 1)}, e5Image(512<<10-1)...), raw: true, declared: 512 << 10, crc: 12345})
	}
	_, ent, written := e6advZipRun(t, e, entries...)
	if written > 3<<19 {
		t.Errorf("%d bytes were written for entries that all fail, against a total of %d: a failed entry is not charged", written, 3<<19)
	}
	if !e6advNote(ent, "extracting it would take", "past 1536 KiB") {
		t.Errorf("no note says the total would be passed: %v", ent["notes"])
	}
}
