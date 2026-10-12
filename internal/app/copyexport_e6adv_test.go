package app

import (
	"encoding/binary"
	"fmt"
	"runtime"
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

// e6advDirectoryOnly is a zip that is only a central directory of n entries with empty names (46
// bytes each) and its end record: 23 MB for 500,000 entries, which Go's zip reader turns into
// about 100 MB of heap before the extraction's own cut at 1,000 entries is reached. The record's
// count is the 16 bits Go compares, so the reader takes the directory as well formed.
func e6advDirectoryOnly(n int) []byte {
	b := make([]byte, 0, 46*n+22)
	for i := 0; i < n; i++ {
		rec := make([]byte, 46)
		binary.LittleEndian.PutUint32(rec, 0x02014b50)
		b = append(b, rec...)
	}
	end := make([]byte, 22)
	binary.LittleEndian.PutUint32(end, 0x06054b50)
	binary.LittleEndian.PutUint16(end[10:], uint16(n))
	binary.LittleEndian.PutUint32(end[12:], uint32(46*n))
	return append(b, end...)
}

// covers: MA-424, R1
// E6-AO7 (E6-AI5, E6-AC6): the zip's list of entries is bounded before it is read whole. Go's reader
// allocates one record for every entry of the central directory (about 200 bytes each) before the
// extraction can cut the list at 1,000, and a zip's size bound (256 MiB) is not an entry bound, so a
// zip of 500,000 empty entries cost about 100 MB of heap and one of 256 MiB over a gigabyte, from a
// message the archive writer could never make (it keeps at most 10,000 parts). The directory is
// read through a budget (4 MiB, 10,000 entries with long names and room to spare), and a zip over it is not
// opened: one note says so, nothing is extracted, the message stays exported. A zip of 1,002
// entries (TestArchiveZipEntriesAreExtractedWithinBounds) still has its first 1,000 extracted.
func TestAZipsListOfEntriesIsBoundedBeforeItIsRead(t *testing.T) {
	data := e6advDirectoryOnly(500000)
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	zr, _, err := openZip(data)
	runtime.ReadMemStats(&after)
	if err == nil || zr != nil {
		t.Errorf("a zip of 500,000 entries was opened (%v)", err)
	}
	if grown := after.TotalAlloc - before.TotalAlloc; grown > 32<<20 {
		t.Errorf("opening it allocated %d MiB: the whole directory was read before it was refused", grown>>20)
	}

	// the positive twin: what the archive writer can make, 10,000 entries with names of 255 bytes, opens
	var legit []e6ZipEntry
	for i := 0; i < 10000; i++ {
		legit = append(legit, e6ZipEntry{name: strings.Repeat("n", 245) + fmt.Sprintf("%05d", i), store: true})
	}
	if zr, _, err := openZip(e6Zip(t, legit...)); err != nil || len(zr.File) != 10000 {
		t.Errorf("a zip of 10,000 entries with long names was refused: %v", err)
	}

	res, ent, _ := e6WithZip(t, buildE6Archive(t), newCEEnv(t), data)
	if res.Counts.Exported != 1 || res.Status != CopyStatusComplete {
		t.Fatalf("the run: %s, %+v", res.Label, res.Counts)
	}
	if got := e6Extracted(t, res.Root, ent); len(got) != 0 {
		t.Errorf("extracted %v", e6Sizes(got))
	}
	if !e6advNote(ent, "could not be opened", "list of entries is larger than this version reads", "the zip beside it") {
		t.Errorf("no note says the list of entries is too large: %v", ent["notes"])
	}
}

// covers: MA-424, R4, R1
// E6-AO2 (E6-AI2, E6-AC4): the 200:1 bound is judged on the compressed bytes the entry really has,
// not on the compressed size its header claims, which is the sender's to write. A deflate stream of
// 20 MiB of zeros (about 20 KB, 1000:1) whose header claims a compressed size that makes it look
// 200:1 or less, or one so large that 200 times it wraps a uint64, is not extracted and not written,
// and is a note that names a zip bomb; the entry beside it is extracted. The code judged the claim,
// extracted all 20 MiB and said nothing.
func TestAZipBombIsJudgedOnItsRealPackedSizeNotItsHeaders(t *testing.T) {
	bomb := make([]byte, 20<<20)
	for name, claim := range map[string]uint64{"a claim of 110,000 bytes": 110000, "a claim that wraps 200 times": 461168601842843648} {
		t.Run(name, func(t *testing.T) {
			res, ent, written := e6advZipRun(t, newCEEnv(t),
				e6ZipEntry{name: "liar.bin", data: bomb, raw: true, declared: uint64(len(bomb)), packed: claim},
				e6ZipEntry{name: "ok.txt", data: []byte("fine")})
			got := e6Extracted(t, res.Root, ent)
			if _, ok := got["liar.bin"]; ok || string(got["ok.txt"]) != "fine" || len(got) != 1 {
				t.Errorf("extracted (name: bytes) %v", e6Sizes(got))
			}
			if !e6advNote(ent, `"liar.bin"`, "zip bomb") {
				t.Errorf("no note says liar.bin is taken for a zip bomb: %v", ent["notes"])
			}
			if written > 1<<10+4 {
				t.Errorf("%d bytes were written for an entry that expands 1000:1: it was expanded to disk", written)
			}
		})
	}
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
