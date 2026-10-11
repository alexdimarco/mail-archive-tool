package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// covers: MA-531, MA-512, R29, S52
// E4-AO5 (also E4-AC2 and the manifest half of E4-AI3), an encoder. R-o4 is a byte cap (1 GiB, RR2-C11,
// ruled), and the memory a manifest costs is not bounded by its bytes: a manifest of bare records
// ("s\0N":{}) holds about 650 bytes of heap per record, 31 to 34 times the file (a realistic manifest holds
// 1.6 times), so a dense manifest well under the cap, handed over as Another archive folder, can hold tens of
// GiB, once for the snapshot and again for the copy Changed reads during a run, and Go's out-of-memory is
// fatal. MA-531 says "no unbounded allocation". The remedy is a limit that is not the byte count: a record
// count (or a cost per record) with its own R-o4 wording, a lower byte cap, or both. The number and the
// wording are the owner's (the 1 GiB is a ruling), so the fix is not made here; this is the body it must
// satisfy: another folder's snapshot holds no more than 8 times the manifest's bytes, or is refused as R-o4.
func TestAnotherArchiveFolderManifestMemoryIsBoundedBySomethingBesidesItsBytes(t *testing.T) {
	t.Skip("OWNER DECISION PENDING (E4-AO5, RR2-C11 / MA-531 'no unbounded allocation'): the 1 GiB R-o4 cap bounds bytes, not memory (a manifest of bare records costs 31 times its size); a record-count or per-record limit, or a lower cap, is a ruling's number and an R-o4 wording")
	dir := tmpDir(t)
	var b strings.Builder
	b.WriteString(`{"version":4,"entries":{`)
	for i := 0; i < 400000; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `"s\u0000\u0000%d":{}`, i)
	}
	b.WriteString(`},"stores":{}}`)
	if err := os.WriteFile(filepath.Join(dir, archiveManifestName), []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	size := int64(b.Len())
	var m0, m1 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m0)
	s, err := OpenArchive(dir, ArchiveOptions{Other: true})
	if err != nil {
		var ref *CopyRefusal
		if errors.As(err, &ref) && ref.ID == "R-o4" {
			return // refused as over the limit: bounded
		}
		t.Fatal(err)
	}
	defer s.Close()
	runtime.GC()
	runtime.ReadMemStats(&m1)
	runtime.KeepAlive(s)
	if held := int64(m1.HeapAlloc) - int64(m0.HeapAlloc); held > 8*size {
		t.Errorf("another archive folder's snapshot of a %d MiB manifest holds %d MiB of heap (%dx its bytes): the cap bounds bytes, not memory", size>>20, held>>20, held/size)
	}
}
