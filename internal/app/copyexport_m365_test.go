package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/graph"
	"mail-archive-tool/internal/graph/graphtest"
)

// bigBody streams n bytes of a repeating pattern, 64 KiB at a time.
func bigBody(n int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		chunk := bytes.Repeat([]byte("0123456789abcdef"), 4096)
		for left := n; left > 0; {
			c := chunk
			if len(c) > left {
				c = c[:left]
			}
			if _, err := w.Write(c); err != nil {
				return
			}
			left -= len(c)
		}
	}
}

// peakHeap runs fn and returns the peak LIVE heap it held, less the live heap in use
// before it started. It is a retention measure, not an allocation one: before every
// sample it forces a garbage collection and reads HeapAlloc, so only reachable memory
// is counted. Memory the run holds that scales with the work — an entries slice — is
// reachable, survives the collection, and is seen; the per-message buffers it allocates
// and drops are not. The peak of these collected samples therefore does not depend on
// when the collector happened to run relative to the sampler, so it is stable across
// machines — unlike a raw HeapInuse peak, which counts whatever uncollected garbage
// existed at the sampling instant and so read tens of MiB higher on one runner than
// another for the very same streaming run.
func peakHeap(fn func()) uint64 {
	// Keep a low GC target so the forced collections below are cheap and the run's own
	// churn stays bounded between samples.
	defer debug.SetGCPercent(debug.SetGCPercent(20))
	var peak atomic.Uint64
	sample := func() {
		runtime.GC()
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		if m.HeapAlloc > peak.Load() {
			peak.Store(m.HeapAlloc)
		}
	}
	runtime.GC()
	var base runtime.MemStats
	runtime.ReadMemStats(&base)
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		for {
			sample() // the live set at this instant; the plateau of a streaming run
			select {
			case <-stop:
				return
			case <-time.After(50 * time.Millisecond):
			}
		}
	}()
	fn()
	close(stop)
	<-done
	sample() // a final reading, in case the high-water mark was at the very end
	if p := peak.Load(); p > base.HeapAlloc {
		return p - base.HeapAlloc
	}
	return 0
}

// covers: MA-419, R29, R17, S52
// Microsoft 365 originals: each .eml equals the fake's $value bytes (a body with CRLF,
// LF and binary bytes is copied untouched), its manifest SHA-256 and size equal the
// file's and the run-record line carries the same hash and size, the in-run heap
// stays under a bound while a 50 MiB message streams (the original is streamed to
// disk, never buffered), and the first page's "honored" immutable-id flag is recorded
// from its refs, with an empty first page unable to carry it. Readable HTML is off in
// this slice: the on half is E5's.
func TestCopyExportM365OriginalsAreByteExact(t *testing.T) {
	f := newMeFake(t)
	mixed := []byte("From: a@example.com\r\nSubject: crlf\n\tlf only\r\n\r\nbody \x00\x01\xff\xfe line\r\nend")
	f.value("MIXED", func(w http.ResponseWriter, r *http.Request) { w.Write(mixed) })
	f.value("BIG", bigBody(50<<20))
	src := f.source(t)
	src.Overall = 30 * time.Second
	e := newCEEnv(t)
	e.sizeCap = 256 << 20
	e.overall = 30 * time.Second
	msgs := idMsgs("A1", "MIXED", "BIG")
	p := planFor(t, e, src, msgs)

	var res *CopyResult
	if peak := peakHeap(func() { res = runWith(t, e, p, src) }); peak > 24<<20 {
		t.Errorf("the heap grew by %d MiB while a 50 MiB message streamed: the original is being buffered", peak>>20)
	}
	if res.Counts.Exported != 3 || res.Outcome != CopyOutcomeComplete {
		t.Fatalf("%s, %+v", res.Label, res.Counts)
	}
	want := map[string][]byte{"A1": ceBody("A1"), "MIXED": mixed}
	files := map[int]ceFileRec{}
	if err := walkMessages(e.recordPath(res.RunID), func(pl *cePlanLine, m *ceMsgLine) error {
		if m == nil || len(m.Files) != 1 {
			t.Errorf("message %d has no single recorded file: %+v", pl.N, m)
			return nil
		}
		files[pl.N] = m.Files[0]
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for i, m := range msgs {
		rec := assure.Reached(t, files[i+1], "the recorded file")
		onDisk, err := os.ReadFile(filepath.Join(res.Root, filepath.FromSlash(rec.Path)))
		if err != nil {
			t.Fatal(err)
		}
		h := sha256.Sum256(onDisk)
		if rec.SHA256 != hex.EncodeToString(h[:]) || rec.Size != int64(len(onDisk)) || rec.Role != roleOriginal {
			t.Errorf("%s: the record says %s / %d, the file is %x / %d", m.SourceID, rec.SHA256, rec.Size, h, len(onDisk))
		}
		if b, ok := want[m.SourceID]; ok && !bytes.Equal(onDisk, b) {
			t.Errorf("%s: the .eml is not the served bytes: %q", m.SourceID, onDisk)
		}
		if m.SourceID == "BIG" && len(onDisk) != 50<<20 {
			t.Errorf("BIG is %d bytes, want 50 MiB", len(onDisk))
		}
	}
	man := manifestOf(t, res.Root)
	if man["counts"].(map[string]any)["exported"] != 3.0 {
		t.Errorf("manifest counts = %v", man["counts"])
	}
	// The manifest's own SHA-256 and size for each .eml equal the file's.
	ents := assure.Reached(t, man["messages"].([]any), "manifest messages")
	if len(ents) != len(msgs) {
		t.Fatalf("%d manifest entries, want %d", len(ents), len(msgs))
	}
	for i, raw := range ents {
		ent := raw.(map[string]any)
		files := ent["files"].([]any)
		if len(files) != 1 {
			t.Fatalf("manifest entry %d has %d files", i, len(files))
		}
		mf := files[0].(map[string]any)
		onDisk, err := os.ReadFile(filepath.Join(res.Root, filepath.FromSlash(mf["path"].(string))))
		if err != nil {
			t.Fatal(err)
		}
		h := sha256.Sum256(onDisk)
		if mf["sha256"] != hex.EncodeToString(h[:]) || mf["size"] != float64(len(onDisk)) || mf["role"] != roleOriginal {
			t.Errorf("%s: the manifest says %v / %v, the file is %x / %d", msgs[i].SourceID, mf["sha256"], mf["size"], h, len(onDisk))
		}
	}
}

// covers: MA-419, R29, R17, S52
// The first page's "honored" flag is recorded from its refs (PhysID == ID on a ref) for
// the manifest's immutable_ids: true only when a listed folder's first page said so
// and none contradicted it; a folder with an empty first page says nothing, and a first
// page that was not honored makes the flag false.
func TestCopyExportRecordsTheHonoredFlagFromTheFirstPage(t *testing.T) {
	path := []CopyFolderRef{{ID: "F1", Name: "Inbox"}}
	for _, tc := range []struct {
		name    string
		honored bool
		pages   []string
		want    bool
	}{
		{"honored, two pages", true, []string{"[" + msgJSON("A", 5) + "]", "[" + msgJSON("B", 6) + "]"}, true},
		{"not honored", false, []string{"[" + msgJSON("A", 5) + "]"}, false},
		{"an empty first page cannot carry it", true, []string{"[]"}, false},
	} {
		f := newMeFake(t)
		f.honored = tc.honored
		f.page("F1", tc.pages...)
		src := f.source(t)
		got, err := src.ListFolder(context.Background(), path)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if tc.want && len(got) == 0 {
			t.Fatalf("%s: nothing listed", tc.name)
		}
		if src.Describe().ImmutableIDs != tc.want {
			t.Errorf("%s: immutable_ids = %v, want %v", tc.name, src.Describe().ImmutableIDs, tc.want)
		}
		if tc.want {
			if got[0].SizeHint != 5 || got[0].Subject != "subj-A" || got[0].FromName != "A" {
				t.Errorf("%s: the listing lost a field: %+v", tc.name, got[0])
			}
		}
	}
}

// covers: MA-426, R29, R10, R1, S52
// The export's size cap is the shared one, graph.MIMEMaxBytes (256 MiB), not a figure of
// its own: with no cap set, MIMETo's limit is min(that cap, the free-space budget), and a
// message declared above it is listed as larger than this version of MailArchive exports
// (256 MiB), terminal.
func TestCopyExportTheSizeCapIsTheSharedOne(t *testing.T) {
	e, src := newCEEnv(t), newFakeSrc()
	e.freeDest = func(string) (uint64, error) { return 1 << 40, nil }
	if e.cap() != graph.MIMEMaxBytes {
		t.Fatalf("the export's cap is %d, the shared cap is %d", e.cap(), graph.MIMEMaxBytes)
	}
	var limits []int64
	src.fetchFn = func(ctx context.Context, m *CopyMessage, w io.Writer, max int64) (int64, error) {
		limits = append(limits, max)
		if m.SourceID == "M00002" {
			return 0, &graph.TooLargeError{MessageID: m.SourceID, Limit: max, Declared: graph.MIMEMaxBytes + 1}
		}
		n, err := w.Write(ceBody(m.SourceID))
		return int64(n), err
	}
	res := mustRun(t, e, cePlan(t, e, src, ceMsgs(3)), src)
	if res.Counts.Exported != 2 || res.Counts.Failed != 1 || res.Stop != nil {
		t.Fatalf("a message above the cap ended the run as %s (%+v) %+v", res.Label, res.Stop, res.Counts)
	}
	for _, l := range limits {
		if l != graph.MIMEMaxBytes {
			t.Errorf("MIMETo was given a limit of %d, want min(shared cap, budget) = %d", l, int64(graph.MIMEMaxBytes))
		}
	}
	if fail := failureOf(t, res.Root, "M00002"); fail == nil || fail["class"] != classTerminal ||
		!strings.Contains(fail["reason"].(string), "larger than this version of MailArchive exports (256 MiB)") {
		t.Errorf("M00002 is listed as %v", fail)
	}
}

// covers: MA-426, R29, R10, R1, S52
// Per-message isolation: a fake Inbox [A1, A2, POISON (500 ErrorMimeContentConversionFailed),
// GONE (404), A3, HUGE (above the scaled size cap)] exports A1, A2 and A3; POISON is
// listed as retryable with Graph's reason; GONE is listed as "no longer in the
// mailbox" and attempted once; HUGE is listed as larger than this version of
// MailArchive exports, terminal; the outcome is 3; "Export failed messages again" reads
// only POISON; and a tree standing in for the archive is byte-unchanged.
func TestCopyExportIsolatesAMessageThatFails(t *testing.T) {
	f := newMeFake(t)
	f.value("POISON", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		io.WriteString(w, `{"error":{"code":"ErrorMimeContentConversionFailed","message":"cannot convert"}}`)
	})
	f.value("GONE", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		io.WriteString(w, `{"error":{"code":"ErrorItemNotFound","message":"gone"}}`)
	})
	f.value("HUGE", graphtest.Sized(8192, true))
	archive := filepath.Join(t.TempDir(), "archive")
	os.MkdirAll(filepath.Join(archive, "store"), 0o755)
	os.WriteFile(filepath.Join(archive, "store", "page.html"), []byte("<html>"), 0o600)
	before := treeFiles(t, archive)

	e := newCEEnv(t)
	e.sizeCap = 4096
	src := f.source(t)
	ids := []string{"A1", "A2", "POISON", "GONE", "A3", "HUGE"}
	res := runWith(t, e, planFor(t, e, src, idMsgs(ids...)), src)
	if res.Outcome != CopyOutcomePartial || res.Counts.Exported != 3 || res.Counts.Failed != 3 {
		t.Fatalf("%s, %+v (outcome %d)", res.Label, res.Counts, res.Outcome)
	}
	for _, id := range []string{"A1", "A2", "A3"} {
		if f.hit(id) != 1 {
			t.Errorf("%s was requested %d times", id, f.hit(id))
		}
	}
	for _, c := range []struct{ id, class, reason string }{
		{"POISON", classRetryable, "ErrorMimeContentConversionFailed"},
		{"GONE", classTerminal, "no longer in the mailbox"},
		{"HUGE", classTerminal, "larger than this version of MailArchive exports"},
	} {
		fail := assure.Reached(t, failureOf(t, res.Root, c.id), c.id+"'s failure")
		if fail["class"] != c.class || !strings.Contains(fail["reason"].(string), c.reason) {
			t.Errorf("%s is listed as %v: %v, want %s: ...%s...", c.id, fail["class"], fail["reason"], c.class, c.reason)
		}
	}
	if f.hit("GONE") != 1 {
		t.Errorf("GONE was attempted %d times, once only (a 404 is never retried in-run)", f.hit("GONE"))
	}
	again, err := e.RunFailed(res.RunID)
	if err != nil || len(again) != 1 || again[0].SourceID != "POISON" {
		t.Errorf("\"Export failed messages again\" reads %+v (%v), want POISON only", again, err)
	}
	after := treeFiles(t, archive)
	for p, h := range before {
		if after[p] != h {
			t.Errorf("the archive stand-in changed: %s", p)
		}
	}
}
