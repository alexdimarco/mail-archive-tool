package archindex

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/state"
)

// source is one store of a real archive built for these tests: its id and its display name.
type source struct{ id, name string }

var (
	m365Source  = source{state.MailboxSourceID("user@contoso.com"), "user@contoso.com"}
	localSource = source{"path:/mail/Inbox.mbox", "Inbox"}
)

// realArchive makes dir an archive whose manifest the real state package writes: n records per
// source, each as a real export records a message. It returns the manifest's size.
func realArchive(t *testing.T, dir string, n int, sources ...source) int64 {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	m, err := state.Load(filepath.Join(dir, manifestName))
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2024, 3, 1, 9, 0, 0, 0, time.UTC)
	for _, s := range sources {
		tok := m.Token(s.id, s.name)
		for i := 0; i < n; i++ {
			m.Add(fmt.Sprintf("%s\x00<message-%08d@mail.example.com>", tok, i), state.Record{
				Path:        fmt.Sprintf("%s/Inbox/2024-03-01_0900_a-subject-slug-for-a-real-message_%08x.html", tok, i),
				Folder:      "Inbox",
				ExportedAt:  at,
				Fingerprint: fmt.Sprintf("%016x", i),
				FpScheme:    state.FpSchemeCurrent,
			})
		}
	}
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(dir, manifestName))
	if err != nil {
		t.Fatal(err)
	}
	return fi.Size()
}

// loadFromKind is the kind a whole parse of folder's manifest gives (state.LoadFrom, the reader
// recognition used before), with ok false when LoadFrom refuses it: the oracle the streaming walk
// must agree with.
func loadFromKind(t *testing.T, folder string) (string, bool) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(folder, manifestName))
	if err != nil {
		return "", false
	}
	m, err := state.LoadFrom(manifestName, data)
	if err != nil {
		return "", false
	}
	var local, cloud bool
	for id := range m.Stores {
		if strings.HasPrefix(id, "mailbox:") {
			cloud = true
		} else {
			local = true
		}
	}
	switch {
	case local && cloud:
		return "mixed", true
	case local:
		return "local", true
	case cloud:
		return "m365", true
	}
	return "empty", true
}

// covers: MA-1034, MA-660
// A real archive of thousands of messages, whose manifest the real state package wrote over
// 1 MiB, is an archive (the 1.5.3 re-walk's N1: from v1.1.0 recognition read at most 1 MiB, so
// such an archive was "not an archive" — greyed in Your archives, refused by Open an archive
// folder… and add-archive). IsArchive, Kind, Available and Reconcile see it, with the kind a whole
// LoadFrom of the same manifest gives. Positive twin first: a small archive of the same sources.
func TestARealManifestOverOneMiBIsRecognised(t *testing.T) {
	cases := []struct {
		name    string
		sources []source
		kind    string
	}{
		{"m365", []source{m365Source}, "m365"},
		{"local", []source{localSource}, "local"},
		{"mixed", []source{m365Source, localSource}, "mixed"},
	}
	for _, c := range cases {
		small := filepath.Join(t.TempDir(), "Small")
		realArchive(t, small, 3, c.sources...)
		if !IsArchive(small) || Kind(small) != c.kind {
			t.Fatalf("%s: the positive twin: a small archive reads %v %q", c.name, IsArchive(small), Kind(small))
		}

		big := filepath.Join(t.TempDir(), "Large "+c.name)
		if size := realArchive(t, big, 6000, c.sources...); size <= 1<<20 {
			t.Fatalf("%s: the fixture must be over 1 MiB, got %d bytes", c.name, size)
		}
		want, ok := loadFromKind(t, big)
		if !ok || want != c.kind {
			t.Fatalf("%s: the oracle: LoadFrom reads %q (%v)", c.name, want, ok)
		}
		if !IsArchive(big) {
			t.Fatalf("%s: a real archive whose manifest is over 1 MiB must be recognised", c.name)
		}
		if k := assure.Reached(t, Kind(big), "the large archive's kind"); k != c.kind {
			t.Fatalf("%s: kind %q, want %q", c.name, k, c.kind)
		}
		if !Available(Entry{Path: big}) {
			t.Fatalf("%s: a large archive must read as available, not greyed", c.name)
		}
		if idx := Reconcile(Index{}, []string{big}, t0); len(idx.Entries) != 1 || !has(idx, big) {
			t.Fatalf("%s: a large archive must be imported: %v", c.name, paths(idx))
		}
	}
}

// covers: MA-1034, MA-660
// The streaming walk gives the verdict and the kind a whole LoadFrom of the same bytes gives, for
// the shapes a manifest can take: legacy unprefixed ids (a mailbox or a path), a token that is
// not a safe segment (dropped, so not a store), null stores, a repeated "stores" (added to, as
// json.Unmarshal fills a map), names in other letter case, a zero-byte manifest, null, a version
// that is null or older, a newer version (refused), and records the walk skips.
func TestRecognitionAgreesWithAWholeParse(t *testing.T) {
	const bs = `\` // a backslash, so a JSON escape is spelt out in these manifests
	manifests := map[string]string{
		"legacy mailbox id":    `{"version":2,"stores":{"user@contoso.com":"a"}}`,
		"legacy path id":       `{"version":3,"stores":{"/mail/a.mbox":"a"}}`,
		"unsafe token":         `{"version":6,"stores":{"mailbox:x@y":"../a","path:/m":"b"}}`,
		"only unsafe tokens":   `{"version":6,"stores":{"mailbox:x@y":"a/b"}}`,
		"null token":           `{"version":6,"stores":{"mailbox:x@y":null}}`,
		"null stores":          `{"version":6,"stores":null}`,
		"repeated stores":      `{"version":6,"stores":{"mailbox:x@y":"a"},"stores":{"path:/m":"b"}}`,
		"stores reset":         `{"version":6,"stores":{"mailbox:x@y":"a"},"stores":null,"stores":{"path:/m":"b"}}`,
		"repeated id":          `{"version":6,"stores":{"path:/m":"b","path:/m":"../b"}}`,
		"folded names":         `{"Version":6,"STORES":{"mailbox:x@y":"a"},"Entries":{}}`,
		"zero bytes":           ``,
		"null":                 `null`,
		"null version":         `{"version":null,"stores":{"path:/m":"b"}}`,
		"no version":           `{"stores":{"path:/m":"b"}}`,
		"newer":                `{"version":7,"stores":{"path:/m":"b"}}`,
		"newer then older":     `{"version":7,"version":6,"stores":{"path:/m":"b"}}`,
		"records and extras":   `{"version":6,"entries":{"a\u0000b":{"path":"a/x.html","missing":["body"],"sealed":null},"c":null},"capture_gaps":{},"collapsed_tokens":{"a":true},"stores":{"path:/m":"a"}}`,
		"white space around":   " \n\t{\"version\":6,\"stores\":{\"path:/m\":\"a\"}}\n ",
		"version not integer":  `{"version":"6","stores":{"path:/m":"a"}}`,
		"stores an array":      `{"version":6,"stores":["path:/m"]}`,
		"store token a number": `{"version":6,"stores":{"path:/m":1}}`,
		"entries an array":     `{"version":6,"entries":[]}`,
		"a record a string":    `{"version":6,"entries":{"k":"v"}}`,
		"an array":             `[]`,
		"a string":             `"manifest"`,
		"trailing value":       `{"version":6}{}`,
		"trailing text":        `{"version":6} x`,
		"white space only":     "   ",
		"truncated":            `{"version":6,"stores":{"path:/m":"a"}`,
		// Added 2026-11-02 (MA-1035): the walk checks a skipped value byte by byte, so its
		// separators, escapes and numbers are read as LoadFrom reads them.
		"comma before }":             `{"version":6,"stores":{"path:/m":"a"},}`,
		"comma before } in a record": `{"version":6,"stores":{"path:/m":"a"},"entries":{"k":{"a":1,}}}`,
		"comma before } in entries":  `{"version":6,"stores":{"path:/m":"a"},"entries":{"k":{},}}`,
		"comma before ]":             `{"version":6,"stores":{"path:/m":"a"},"x":[1,]}`,
		"two commas":                 `{"version":6,,"stores":{"path:/m":"a"}}`,
		"no comma":                   `{"version":6 "stores":{"path:/m":"a"}}`,
		"no colon":                   `{"version":6,"x" 1,"stores":{"path:/m":"a"}}`,
		"a name not a string":        `{"version":6,x:1,"stores":{"path:/m":"a"}}`,
		"numbers":                    `{"version":6,"x":[0,-0,1.5,-2e10,3E+2,4e-1],"stores":{"path:/m":"a"}}`,
		"a leading zero":             `{"version":6,"x":01,"stores":{"path:/m":"a"}}`,
		"a bare point":               `{"version":6,"x":1.,"stores":{"path:/m":"a"}}`,
		"an exponent with no digit":  `{"version":6,"x":1e,"stores":{"path:/m":"a"}}`,
		"escapes":                    `{"version":6,"x":"\"\\\/\b\f\n\r\t` + bs + `u00e9","stores":{"path:/m":"a"}}`,
		"a bad escape":               `{"version":6,"x":"\x","stores":{"path:/m":"a"}}`,
		"a short \\u":                `{"version":6,"x":"\u00g9","stores":{"path:/m":"a"}}`,
		"a control character":        "{\"version\":6,\"x\":\"a\tb\",\"stores\":{\"path:/m\":\"a\"}}",
		"invalid UTF-8":              "{\"version\":6,\"x\":\"\xff\xfe\",\"stores\":{\"path:/m\":\"a\"}}",
		"literals":                   `{"version":6,"x":[true,false,null],"stores":{"path:/m":"a"}}`,
		"a bad literal":              `{"version":6,"x":nul,"stores":{"path:/m":"a"}}`,
		"a long name":                `{"version":6,"` + strings.Repeat("n", 1000) + `":1,"stores":{"path:/m":"a"}}`,
		"an escaped name":            `{"` + bs + `u0076ersion":7,"stores":{"path:/m":"a"}}`,
		"a folded name":              `{"version":6,"ſtores":{"mailbox:x@y":"a"}}`,
	}
	for name, body := range manifests {
		dir := filepath.Join(t.TempDir(), "A")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, manifestName), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		wantKind, wantOK := loadFromKind(t, dir)
		gotKind, gotOK := inspect(dir)
		if gotOK != wantOK || gotKind != wantKind {
			t.Errorf("%s: recognition reads (%q, %v); a whole LoadFrom reads (%q, %v)", name, gotKind, gotOK, wantKind, wantOK)
		}
	}
}

// covers: MA-1034, MA-660
// A hostile manifest is still refused, and none is read without bound: one over the read's cap
// (manifestCap stands in at 64 KiB for 1 GiB), one that nests deeper than any manifest, one that
// stops part way, a version over its bound, a skipped value over 1 MiB that does not parse, stores
// over 1 MiB, a newer format, a manifest that is a folder, and one that is a link out of the
// folder to a real manifest. Positive twin first: the same real manifest in the folder is
// recognised. (Amended 2026-11-02, MA-1035: one value or one run of white space over 1 MiB that
// the walk skips is now read as LoadFrom reads it.)
func TestAHostileManifestIsStillRefused(t *testing.T) {
	real := filepath.Join(t.TempDir(), "Real")
	realArchive(t, real, 3000, m365Source)
	if !IsArchive(real) || Kind(real) != "m365" {
		t.Fatalf("the positive twin: %v %q", IsArchive(real), Kind(real))
	}
	realBytes, err := os.ReadFile(filepath.Join(real, manifestName))
	if err != nil {
		t.Fatal(err)
	}
	refused := func(what string, body []byte) {
		t.Helper()
		dir := filepath.Join(t.TempDir(), "Hostile")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, manifestName), body, 0o644); err != nil {
			t.Fatal(err)
		}
		if IsArchive(dir) || Kind(dir) != "" || Available(Entry{Path: dir}) {
			t.Errorf("%s must be refused", what)
		}
	}

	// Over the read's cap: a real manifest is refused once manifestCap is under its size, and
	// recognised again under a cap over it.
	old := manifestCap
	manifestCap = 64 << 10
	refused("a manifest over the read's cap", realBytes)
	refused("a manifest one byte over the read's cap", append([]byte(`{"version":6,"stores":{"path:/m":"a"}}`), bytes.Repeat([]byte("\n"), int(manifestCap))...))
	manifestCap = old

	refused("a manifest nested deeper than any manifest", []byte(`{"version":6,"x":`+strings.Repeat("[", 200)+strings.Repeat("]", 200)+`}`))
	refused("a record nested deeper than any manifest", []byte(`{"version":6,"entries":{"k":{"a":`+strings.Repeat("[", 200)+strings.Repeat("]", 200)+`}}}`))
	refused("a manifest that stops part way", realBytes[:len(realBytes)/2])
	refused("a malformed manifest", []byte(`{"version":6,"stores":{"path:/m":"a",}}`))
	// Amended 2026-11-02 (MA-1035, the MA-1034 adversarial pass, finding 1): one value over 1 MiB
	// that the walk skips, and a run of white space over 1 MiB, are no longer refused: the walk
	// holds neither, and one email's subject is written over 1 MiB in a real manifest. They are
	// read as LoadFrom reads them in TestARecordValueOverOneMiBIsRecognised. What the walk does
	// hold is still bounded, and what it skips is still checked byte by byte: a version over its
	// bound, and a skipped value over 1 MiB that does not parse (a control character, a bad
	// escape, a string never closed), are refused.
	refused("a version over its bound", []byte(`{"version":6`+strings.Repeat("0", 300)+`,"stores":{"path:/m":"a"}}`))
	refused("a value over 1 MiB holding a control character", []byte(`{"version":6,"subject":"`+strings.Repeat("x", 1<<20+1)+"\x01"+`","stores":{"path:/m":"a"}}`))
	refused("a record's value over 1 MiB with a bad escape", []byte(`{"version":6,"stores":{"path:/m":"a"},"entries":{"k":{"subject":"`+strings.Repeat("x", 1<<20+1)+`\q"}}}`))
	refused("a record's value over 1 MiB never closed", []byte(`{"version":6,"stores":{"path:/m":"a"},"entries":{"k":{"subject":"`+strings.Repeat("x", 1<<20+1)))
	refused("a store over 1 MiB", []byte(`{"version":6,"stores":{"path:/m":"`+strings.Repeat("x", 1<<20+1)+`"}}`))
	var many bytes.Buffer
	many.WriteString(`{"version":6,"stores":{`)
	for i, kept := 0, 0; kept <= 1<<20; i++ {
		id, tok := fmt.Sprintf("path:/mail/a-store-of-a-hostile-folder-%08d.mbox", i), fmt.Sprintf("t%08d", i)
		fmt.Fprintf(&many, `%q:%q,`, id, tok)
		kept += len(id) + len(tok)
	}
	many.WriteString(`"path:/m":"a"}}`)
	refused("stores over 1 MiB", many.Bytes())
	if !bytes.Contains(realBytes, []byte(`"version":6`)) {
		t.Fatal("the fixture: the real manifest names its version as \"version\":6")
	}
	refused("a newer format", bytes.Replace(realBytes, []byte(`"version":6`), []byte(`"version":7`), 1))

	// A manifest that is a folder.
	folderDir := filepath.Join(t.TempDir(), "Folder")
	if err := os.MkdirAll(filepath.Join(folderDir, manifestName), 0o755); err != nil {
		t.Fatal(err)
	}
	if IsArchive(folderDir) {
		t.Error("a manifest that is a folder must be refused")
	}
	// A manifest that is a link out of the folder, to a real large manifest.
	if runtime.GOOS != "windows" {
		linkDir := filepath.Join(t.TempDir(), "Link")
		if err := os.MkdirAll(linkDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(real, manifestName), filepath.Join(linkDir, manifestName)); err != nil {
			t.Fatalf("symlink: %v", err)
		}
		if IsArchive(linkDir) {
			t.Error("a manifest that is a link out of the folder must not be followed (SD-1)")
		}
	}
}

// covers: MA-1034
// The verdict is kept while the folder and its manifest stay the same file, of the same size and
// modification time, and read again when the manifest changes: a manifest changed in place to a
// newer format is refused once its modification time moves, and a manifest replaced by another
// file is read again even when its size and time are the same. Positive twin first: an unchanged
// manifest keeps its verdict (the kept verdict is seen: a change that leaves size, time and file
// as they were is not read).
func TestTheKeptVerdictIsReadAgainAfterTheManifestChanges(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Kept")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	man := filepath.Join(dir, manifestName)
	v6 := []byte(`{"version":6,"stores":{"path:/m":"a"}}`)
	v9 := []byte(`{"version":9,"stores":{"path:/m":"a"}}`)
	if err := os.WriteFile(man, v6, 0o644); err != nil {
		t.Fatal(err)
	}
	when := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	if err := os.Chtimes(man, when, when); err != nil {
		t.Fatal(err)
	}
	if !IsArchive(dir) || Kind(dir) != "local" {
		t.Fatalf("the positive twin: %v %q", IsArchive(dir), Kind(dir))
	}

	// Changed in place, its size and time put back: the kept verdict stands (it is not read).
	f, err := os.OpenFile(man, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt(v9, 0); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err := os.Chtimes(man, when, when); err != nil {
		t.Fatal(err)
	}
	if !IsArchive(dir) {
		t.Fatal("an unchanged manifest (same file, size and time) must keep its verdict")
	}
	// Its time moves: read again, and the newer format is refused.
	later := when.Add(time.Second)
	if err := os.Chtimes(man, later, later); err != nil {
		t.Fatal(err)
	}
	if IsArchive(dir) {
		t.Fatal("a manifest whose modification time changed must be read again")
	}

	// Replaced by another file of the same size and time: read again.
	tmp := filepath.Join(dir, "next.json")
	if err := os.WriteFile(tmp, v6, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(tmp, later, later); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, man); err != nil {
		t.Fatal(err)
	}
	if !IsArchive(dir) {
		t.Fatal("a manifest replaced by another file must be read again")
	}
	// Its size changes: read again.
	if err := os.WriteFile(man, append(v9, ' '), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(man, later, later); err != nil {
		t.Fatal(err)
	}
	if IsArchive(dir) {
		t.Fatal("a manifest whose size changed must be read again")
	}
}

// covers: MA-1034
// Recognition does not hold the manifest in memory: walking a 48 MiB manifest as it streams in
// keeps the heap far under its size. Positive twin first: the stream is a manifest the walk
// recognises.
func TestRecognitionDoesNotHoldTheManifest(t *testing.T) {
	const records = 120000 // about 48 MiB of records
	gen := func() io.Reader { return &manifestStream{records: records} }
	n, err := io.Copy(io.Discard, gen())
	if err != nil || n < 40<<20 {
		t.Fatalf("the stream: %d bytes, %v", n, err)
	}
	var base runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&base)
	s := &manifestStream{records: records, sample: true}
	kind, err := scanManifest(s)
	if err != nil || kind != "m365" {
		t.Fatalf("the positive twin: the stream reads (%q, %v)", kind, err)
	}
	if grew := int64(s.peak) - int64(base.HeapAlloc); grew > 24<<20 {
		t.Fatalf("walking a %d-byte manifest grew the heap by %d bytes: it must not hold the manifest", n, grew)
	}
}

// manifestStream is a manifest of records made as it is read, so the test holds none of it; with
// sample set it notes the heap's peak every MiB read.
type manifestStream struct {
	records, i int
	buf        bytes.Buffer
	head, done bool
	sample     bool
	read       int
	peak       uint64
}

func (m *manifestStream) Read(p []byte) (int, error) {
	for m.buf.Len() < len(p) && !m.done {
		switch {
		case !m.head:
			m.buf.WriteString(`{"version":6,"stores":{"mailbox:user@contoso.com":"user@contoso.com"},"entries":{`)
			m.head = true
		case m.i < m.records:
			if m.i > 0 {
				m.buf.WriteByte(',')
			}
			rec, _ := json.Marshal(map[string]any{"path": fmt.Sprintf("user@contoso.com/Inbox/2024-03-01_0900_a-subject-slug-for-a-real-message_%08x.html", m.i),
				"folder": "Inbox", "exported_at": "2024-03-01T09:00:00Z", "subject": "A subject long enough to be like a real one", "fp": "0123456789abcdef", "fp_scheme": 1,
				"first_folder": "Inbox", "first_seen": "2024-03-01T09:00:00Z", "last_seen": "2024-03-01T09:00:00Z", "present": true})
			fmt.Fprintf(&m.buf, `"user@contoso.com\u0000<message-%08d@mail.example.com>":%s`, m.i, rec)
			m.i++
		default:
			m.buf.WriteString(`}}`)
			m.done = true
		}
	}
	n, err := m.buf.Read(p)
	if err == io.EOF && !m.done {
		err = nil
	}
	if m.sample {
		before := m.read
		m.read += n
		if m.read>>20 != before>>20 {
			var ms runtime.MemStats
			runtime.ReadMemStats(&ms)
			if ms.HeapAlloc > m.peak {
				m.peak = ms.HeapAlloc
			}
		}
	}
	return n, err
}
