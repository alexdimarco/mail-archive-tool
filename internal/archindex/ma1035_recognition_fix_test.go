package archindex

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/state"
)

// hostileArchive makes dir an archive whose manifest the real state package writes, with one
// record whose subject is subject (an exporter records the subject of a message with issues, such
// as a dangling cid: image): json.Marshal writes each '<', '&', control character and invalid
// UTF-8 byte of it as six bytes. It returns the manifest's size.
func hostileArchive(t *testing.T, dir, subject string) int64 {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	m, err := state.Load(filepath.Join(dir, manifestName))
	if err != nil {
		t.Fatal(err)
	}
	tok := m.Token(localSource.id, localSource.name)
	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	m.Add(tok+"\x00<hostile@x>", state.Record{
		Path: tok + "/Inbox/2024-01-01_0000_hostile_00000001.html", Folder: "Inbox", ExportedAt: at,
		Missing: []string{"cid:logo"}, Subject: subject, Fingerprint: "0123456789abcdef", FpScheme: state.FpSchemeCurrent,
	})
	m.Add(tok+"\x00<ok@x>", state.Record{
		Path: tok + "/Inbox/2024-01-01_0000_ordinary_00000002.html", Folder: "Inbox", ExportedAt: at,
		Fingerprint: "fedcba9876543210", FpScheme: state.FpSchemeCurrent,
	})
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(dir, manifestName))
	if err != nil {
		t.Fatal(err)
	}
	return fi.Size()
}

// covers: MA-1035, MA-1034, MA-660
// One message can make a record's value far over 1 MiB as the manifest stores it: a subject of
// 180,000 '<' (about 176 KiB, under the 1 MiB header cap) is written as about 1.03 MiB, since
// json.Marshal writes each '<', '&', control character or invalid UTF-8 byte as six bytes. Such an
// archive is an archive, of the kind a whole LoadFrom gives (the MA-1034 adversarial pass, finding
// 1: the walk held each value whole under a 1 MiB bound, so one hostile email made the archive
// "not an archive" while `status` found nothing wrong). The records' values are skipped without
// being held, so a value of 8 MiB, a top-level value over 1 MiB and a run of white space over 2 MiB
// are read as LoadFrom reads them too. Positive twin first: the archive with an ordinary subject.
func TestARecordValueOverOneMiBIsRecognised(t *testing.T) {
	plain := filepath.Join(t.TempDir(), "Plain")
	hostileArchive(t, plain, "an ordinary subject")
	if !IsArchive(plain) || Kind(plain) != "local" {
		t.Fatalf("the positive twin: %v %q", IsArchive(plain), Kind(plain))
	}

	for name, subject := range map[string]string{
		"180,000 '<'":                 strings.Repeat("<", 180000),
		"180,000 '&'":                 strings.Repeat("&", 180000),
		"180,000 control characters":  strings.Repeat("\x01", 180000),
		"180,000 invalid UTF-8 bytes": strings.Repeat("\xff", 180000),
		"8 MiB of '<'":                strings.Repeat("<", 1400000),
	} {
		dir := filepath.Join(t.TempDir(), "Hostile")
		if size := hostileArchive(t, dir, subject); size <= 1<<20 {
			t.Fatalf("%s: the fixture: the manifest must hold a value over 1 MiB, got %d bytes", name, size)
		}
		want, ok := loadFromKind(t, dir)
		if !ok || want != "local" {
			t.Fatalf("%s: the oracle: LoadFrom reads %q (%v)", name, want, ok)
		}
		if !IsArchive(dir) {
			t.Errorf("%s: an archive whose record holds a value over 1 MiB must be recognised", name)
			continue
		}
		if k := assure.Reached(t, Kind(dir), name+": the kind"); k != want {
			t.Errorf("%s: kind %q, want %q", name, k, want)
		}
		if !Available(Entry{Path: dir}) {
			t.Errorf("%s: the archive must read as available", name)
		}
	}

	// Values the walk skips at the top level, and white space, as LoadFrom reads them.
	for name, body := range map[string]string{
		"a top-level value over 1 MiB":    `{"version":6,"subject":"` + strings.Repeat("x", 1<<20+1) + `","stores":{"path:/m":"a"}}`,
		"a run of white space over 2 MiB": `{"version":6,` + strings.Repeat(" ", 2<<20) + `"stores":{"path:/m":"a"}}`,
	} {
		dir := filepath.Join(t.TempDir(), "Skipped")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, manifestName), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		want, ok := loadFromKind(t, dir)
		got, gotOK := inspect(dir)
		if !ok || got != want || gotOK != ok {
			t.Errorf("%s: recognition reads (%q, %v); LoadFrom reads (%q, %v)", name, got, gotOK, want, ok)
		}
	}
}

// covers: MA-1035, MA-1034
// A value the walk skips is not held: walking a manifest whose one record holds a 32 MiB string
// keeps the heap far under its size. Positive twin first: the stream is recognised.
func TestASkippedValueIsNotHeld(t *testing.T) {
	const n = 32 << 20
	head := `{"version":6,"stores":{"path:/m":"a"},"entries":{"k":{"subject":"`
	tail := `"}}}`
	var base runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&base)
	body := &repeatStream{b: '<', n: n}
	kind, err := scanManifest(io.MultiReader(strings.NewReader(head), body, strings.NewReader(tail)))
	if err != nil || kind != "local" {
		t.Fatalf("the positive twin: the stream reads (%q, %v)", kind, err)
	}
	if grew := int64(body.peak) - int64(base.HeapAlloc); grew > 8<<20 {
		t.Fatalf("walking a %d-byte value grew the heap by %d bytes: a skipped value must not be held", n, grew)
	}
}

// repeatStream is n copies of b, made as they are read; it notes the heap's peak every MiB.
type repeatStream struct {
	b       byte
	n, read int
	peak    uint64
}

func (r *repeatStream) Read(p []byte) (int, error) {
	if r.read >= r.n {
		return 0, io.EOF
	}
	if len(p) > r.n-r.read {
		p = p[:r.n-r.read]
	}
	for i := range p {
		p[i] = r.b
	}
	before := r.read
	r.read += len(p)
	if r.read>>20 != before>>20 {
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		if ms.HeapAlloc > r.peak {
			r.peak = ms.HeapAlloc
		}
	}
	return len(p), nil
}

// covers: MA-1035, MA-1034
// The read's cap holds on the stream itself, not only on the file's size before the read (the
// MA-1034 adversarial pass, finding 3): a manifest stream longer than manifestCap (standing in at
// 4 KiB) is refused as too large, whether the bytes over the cap are records or white space after
// the object. Positive twin first: the same stream under a cap over its length is recognised.
func TestTheReadCapHoldsOnTheStream(t *testing.T) {
	stream := func() io.Reader { return &manifestStream{records: 40} }
	n, err := io.Copy(io.Discard, stream())
	if err != nil || n <= 4<<10 {
		t.Fatalf("the stream: %d bytes, %v", n, err)
	}
	old := manifestCap
	defer func() { manifestCap = old }()
	manifestCap = n
	if kind, err := scanManifest(stream()); err != nil || kind != "m365" {
		t.Fatalf("the positive twin: a stream of exactly the cap reads (%q, %v)", kind, err)
	}
	manifestCap = 4 << 10
	// The bytes over the cap are never read: a stream of 4,000 records (over 1 MiB), or a small
	// object followed by 1 MiB of white space, is read no further than the cap and one byte.
	long := &countingReader{r: &manifestStream{records: 4000}}
	if _, err := scanManifest(long); !errors.Is(err, errManifestTooLarge) {
		t.Errorf("a stream of records over the cap: %v, want %v", err, errManifestTooLarge)
	}
	if long.n > manifestCap+1 {
		t.Errorf("a stream of records over the cap was read to %d bytes, past the cap (%d)", long.n, manifestCap)
	}
	small := `{"version":6,"stores":{"path:/m":"a"}}`
	spaced := &countingReader{r: io.MultiReader(strings.NewReader(small), strings.NewReader(strings.Repeat("\n", 1<<20)))}
	if _, err := scanManifest(spaced); !errors.Is(err, errManifestTooLarge) {
		t.Errorf("a stream of white space over the cap after the object: %v, want %v", err, errManifestTooLarge)
	}
	if spaced.n > manifestCap+1 {
		t.Errorf("a stream of white space over the cap was read to %d bytes, past the cap (%d)", spaced.n, manifestCap)
	}
}

// countingReader counts the bytes read from r.
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	k, err := c.r.Read(p)
	c.n += int64(k)
	return k, err
}

// failingReader gives the first n bytes of r, then fails as a read of a held or damaged file fails.
type failingReader struct {
	r io.Reader
	n int
}

func (f *failingReader) Read(p []byte) (int, error) {
	if f.n <= 0 {
		return 0, errors.New("the process cannot access the file because another process has locked a portion of the file")
	}
	if len(p) > f.n {
		p = p[:f.n]
	}
	k, err := f.r.Read(p)
	f.n -= k
	return k, err
}

// covers: MA-1035, MA-1034
// A read that fails part way (a sharing violation, an I/O error) is not kept as the folder's
// verdict: the folder is "not an archive" for that read, and read again next time (the MA-1034
// adversarial pass, finding 4). Fault injection through manifestReader. Positive twin first: the
// archive read whole is recognised, and a second read with nothing changed is the kept verdict.
func TestAFailedReadIsNotKept(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Held")
	realArchive(t, dir, 200, localSource)
	old := manifestReader
	defer func() { manifestReader = old }()
	var reads atomic.Int32
	failing := true
	manifestReader = func(f *os.File) io.Reader {
		reads.Add(1)
		if failing {
			return &failingReader{r: f, n: 4096}
		}
		return f
	}

	twin := filepath.Join(t.TempDir(), "Twin")
	realArchive(t, twin, 200, localSource)
	failing = false
	if !IsArchive(twin) || !IsArchive(twin) || reads.Load() != 1 {
		t.Fatalf("the positive twin: recognised and kept, read %d times (want 1)", reads.Load())
	}

	failing = true
	if IsArchive(dir) {
		t.Fatal("a read that fails part way must not recognise the archive")
	}
	failing = false
	if !IsArchive(dir) {
		t.Fatal("a read that failed must not be kept: the next read must recognise the archive")
	}
	if got := reads.Load(); got != 3 {
		t.Fatalf("the manifest was read %d times, want 3 (the twin once, the failed read, the read again)", got)
	}
}

// covers: MA-1035, MA-1034
// The stores kept to tell the kind are bounded by what they cost in memory, not by their text
// alone (the MA-1034 adversarial pass, finding 6): each store counts its text and a fixed cost, so
// 70,000 tiny stores (under 1 MiB of text, several MiB of map: the pass measured about 130 bytes of
// heap a store) are refused, while an archive of
// 1,000 sources, ids of real length, is recognised. Positive twin first.
func TestTheStoresBoundCountsEachStore(t *testing.T) {
	write := func(name string, n int, id func(int) string, tok string) string {
		t.Helper()
		dir := filepath.Join(t.TempDir(), name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		var b strings.Builder
		b.WriteString(`{"version":6,"stores":{`)
		for i := 0; i < n; i++ {
			if i > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, "%q:%s", id(i), tok)
		}
		b.WriteString(`}}`)
		if err := os.WriteFile(filepath.Join(dir, manifestName), []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	many := write("Sources", 1000, func(i int) string {
		return fmt.Sprintf("path:/Users/someone/Library/Thunderbird/Profiles/abcd1234.default-release/Mail/Local Folders/Folder %04d", i)
	}, `"Folder-0000"`)
	if want, ok := loadFromKind(t, many); !ok || want != "local" || !IsArchive(many) || Kind(many) != "local" {
		t.Fatalf("the positive twin: 1,000 sources read %v %q (LoadFrom %q %v)", IsArchive(many), Kind(many), want, ok)
	}

	tiny := write("Tiny", 70000, func(i int) string { return fmt.Sprintf("%x", i) }, `null`)
	data, err := os.ReadFile(filepath.Join(tiny, manifestName))
	if err != nil || len(data) >= 1<<20 {
		t.Fatalf("the fixture: the stores' text must be under 1 MiB, got %d bytes (%v)", len(data), err)
	}
	if IsArchive(tiny) || Kind(tiny) != "" {
		t.Error("70,000 tiny stores must be refused by the stores' bound")
	}
}

// covers: MA-1035, MA-1034
// Two first reads of the same folder at once walk its manifest once: the second waits for the
// first and takes its verdict (the MA-1034 adversarial pass, finding 7: two page loads at once
// each walked a large manifest whole). Positive twin first: a read alone walks it once.
func TestConcurrentFirstReadsWalkOnce(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Once")
	realArchive(t, dir, 200, localSource)
	old := manifestReader
	defer func() { manifestReader = old }()
	var reads atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	manifestReader = func(f *os.File) io.Reader {
		if reads.Add(1) == 1 {
			close(started)
			<-release
		}
		return f
	}

	twin := filepath.Join(t.TempDir(), "Alone")
	realArchive(t, twin, 200, localSource)
	go func() { <-started; close(release) }()
	if !IsArchive(twin) || reads.Load() != 1 {
		t.Fatalf("the positive twin: a read alone: read %d times", reads.Load())
	}

	reads.Store(0)
	started, release = make(chan struct{}), make(chan struct{})
	var wg sync.WaitGroup
	got := make([]bool, 2)
	wg.Add(1)
	go func() { defer wg.Done(); got[0] = IsArchive(dir) }()
	<-started
	wg.Add(1)
	go func() { defer wg.Done(); got[1] = IsArchive(dir) }()
	time.Sleep(300 * time.Millisecond) // the second read arrives while the first walks
	close(release)
	wg.Wait()
	if !got[0] || !got[1] {
		t.Fatalf("both reads must recognise the archive: %v", got)
	}
	if n := reads.Load(); n != 1 {
		t.Fatalf("two first reads at once walked the manifest %d times, want 1", n)
	}
}
