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

// peakHeap samples the heap while fn runs and returns its highest in-use size less
// what was in use before.
func peakHeap(fn func()) uint64 {
	// A low GC target keeps the sampled heap close to what the run holds live, not what
	// it has made garbage since the last collection.
	defer debug.SetGCPercent(debug.SetGCPercent(20))
	runtime.GC()
	var base runtime.MemStats
	runtime.ReadMemStats(&base)
	var peak atomic.Uint64
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		var m runtime.MemStats
		for {
			runtime.ReadMemStats(&m)
			if m.HeapInuse > peak.Load() {
				peak.Store(m.HeapInuse)
			}
			select {
			case <-stop:
				return
			case <-time.After(5 * time.Millisecond):
			}
		}
	}()
	fn()
	close(stop)
	<-done
	if p := peak.Load(); p > base.HeapInuse {
		return p - base.HeapInuse
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
	if man := manifestOf(t, res.Root); man["counts"].(map[string]any)["exported"] != 3.0 {
		t.Errorf("manifest counts = %v", man["counts"])
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
