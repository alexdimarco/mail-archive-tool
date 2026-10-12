package source

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"math/rand"
	"runtime"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/state"
)

// peakHeapOf runs f and returns the highest in-use heap it reached above what was live
// before it, sampled while it ran, with the collector kept tight so garbage does not
// pass for retained memory.
func peakHeapOf(f func()) uint64 {
	old := debug.SetGCPercent(20)
	defer debug.SetGCPercent(old)
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
			case <-time.After(time.Millisecond):
			}
		}
	}()
	f()
	close(stop)
	<-done
	if p := peak.Load(); p > base.HeapInuse {
		return p - base.HeapInuse
	}
	return 0
}

// nestedMixed is a message of levels nested multipart/mixed containers whose innermost
// holds one base64 attachment big.bin of leaf (the walker's depth bound is 32).
func nestedMixed(levels int, leaf []byte) []byte {
	enc := base64.StdEncoding.EncodeToString(leaf)
	var wrapped strings.Builder
	for i := 0; i < len(enc); i += 76 {
		end := min(i+76, len(enc))
		wrapped.WriteString(enc[i:end])
		wrapped.WriteString("\r\n")
	}
	body := fmt.Sprintf("--b%d\r\nContent-Type: application/octet-stream\r\nContent-Transfer-Encoding: base64\r\n"+
		"Content-Disposition: attachment; filename=\"big.bin\"\r\n\r\n%s--b%d--\r\n", levels, wrapped.String(), levels)
	for i := levels - 1; i >= 1; i-- {
		body = fmt.Sprintf("--b%d\r\nContent-Type: multipart/mixed; boundary=\"b%d\"\r\n\r\n%s\r\n--b%d--\r\n", i, i+1, body, i)
	}
	return hostile(`multipart/mixed; boundary="b1"`, body)
}

// covers: MA-349, R10
// MS2-AO2. Nested multiparts do not keep one copy of the message per level: a part is read
// as a window onto its parent's bytes, so the peak heap of the walker on a message nested
// 31 deep is a small multiple of the message, as on a flat one (the measured baseline of
// MA-349's build was about 7.7 times a 16 MiB message for the whole route; the walker
// alone had a copy per level, 45 times).
func TestNestedMultipartsDoNotCopyTheMessageAtEveryLevel(t *testing.T) {
	useFidelity(t)
	leaf := make([]byte, 3<<20)
	rand.New(rand.NewSource(1)).Read(leaf)

	flat := nestedMixed(1, leaf)
	nested := nestedMixed(31, leaf)
	var m *model.Message
	flatPeak := peakHeapOf(func() { m = parseFidelity(flat) })
	if a, ok := attNamed(m, "big.bin"); !ok || !bytes.Equal(bytesOf(t, a), leaf) {
		t.Fatalf("flat: attachments %v, want big.bin holding the leaf", attNames(m))
	}
	nestedPeak := peakHeapOf(func() { m = parseFidelity(nested) })
	if a, ok := attNamed(m, "big.bin"); !ok || !bytes.Equal(bytesOf(t, a), leaf) || len(m.ParseGaps) != 0 {
		t.Fatalf("nested 31 deep: attachments %v gaps %v, want big.bin holding the leaf and no gap", attNames(m), m.ParseGaps)
	}
	t.Logf("message %d KiB: flat peak %d KiB (x%.1f), nested 31 deep peak %d KiB (x%.1f)",
		len(nested)>>10, flatPeak>>10, float64(flatPeak)/float64(len(flat)), nestedPeak>>10, float64(nestedPeak)/float64(len(nested)))
	if limit := 8 * uint64(len(nested)); nestedPeak > limit {
		t.Errorf("nested 31 deep: peak heap %d KiB is %.1f times the %d KiB message; want at most 8 times (a flat message peaks at %.1f)",
			nestedPeak>>10, float64(nestedPeak)/float64(len(nested)), len(nested)>>10, float64(flatPeak)/float64(len(flat)))
	}
}

// covers: MA-349, R10
// MS2-AO3. The 1 MiB cap on a part's header (MC22) is enforced before the header is read:
// a part whose header is millions of tiny fields is kept unread as one structure-limit
// blob, its siblings are read, and the walker's peak heap stays a small multiple of the
// message (reading the header first cost about 120 times the message). The frozen v1
// replay has the same amplification and is not the walker's to change (ESC-MS2-1).
func TestAPartHeaderOverTheCapIsNotReadFirst(t *testing.T) {
	useFidelity(t)
	padded := "--bd\r\n" + strings.Repeat("X:\r\n", 1<<20) + "Content-Type: text/plain\r\n\r\nthe padded part\r\n"
	text := "--bd\r\nContent-Type: text/plain\r\n\r\nfirst part\r\n"
	sibling := "--bd\r\nContent-Type: application/pdf\r\nContent-Disposition: attachment; filename=\"sibling.pdf\"\r\n\r\nthe sibling\r\n--bd--\r\n"
	data := hostile(`multipart/mixed; boundary="bd"`, text+padded+sibling)

	var m *model.Message
	peak := peakHeapOf(func() { m = parseFidelity(data) })
	a, ok := attNamed(m, "part-2.undecoded")
	if !ok || a.GapKind != state.GapKindStructureLimit || !bytes.Contains(bytesOf(t, a), []byte("the padded part")) {
		t.Errorf("attachments %v, want part-2.undecoded (structure-limit) holding the part", attNames(m))
	}
	if s, ok := attNamed(m, "sibling.pdf"); !ok || string(bytesOf(t, s)) != "the sibling" {
		t.Errorf("an oversized part header cost its sibling: attachments %v", attNames(m))
	}
	if !strings.Contains(m.PlainBody, "first part") {
		t.Errorf("the part before the oversized one was lost: %q", m.PlainBody)
	}
	t.Logf("message %d KiB, walker peak %d KiB (x%.1f)", len(data)>>10, peak>>10, float64(peak)/float64(len(data)))
	if limit := 2 * uint64(len(data)); peak > limit {
		t.Errorf("peak heap %d KiB is %.1f times the %d KiB message; want at most 2 times", peak>>10, float64(peak)/float64(len(data)), len(data)>>10)
	}

	// The same message with LF line ends (the reader switches to LF at the first delimiter):
	// the scan finds the delimiters as the reader does.
	lf := []byte(strings.ReplaceAll(string(data), "\r\n", "\n"))
	got := parseFidelity(lf)
	if a, ok := attNamed(got, "part-2.undecoded"); !ok || !bytes.Contains(bytesOf(t, a), []byte("the padded part")) {
		t.Errorf("LF: attachments %v, want part-2.undecoded holding the padded part", attNames(got))
	}
	if s, ok := attNamed(got, "sibling.pdf"); !ok || string(bytesOf(t, s)) != "the sibling" {
		t.Errorf("LF: the sibling of the oversized part: attachments %v", attNames(got))
	}
	// A first delimiter ending in a bare LF makes the reader take only LF line ends as
	// delimiters, so it sees no second part: the padded header is part of the first part's body,
	// is never parsed as a header, and the scan agrees (no part is wrongly cut out).
	mixed := bytes.Replace(data, []byte("--bd\r\nContent-Type: text/plain\r\n\r\nfirst part"), []byte("--bd\nContent-Type: text/plain\n\nfirst part"), 1)
	if mp := peakHeapOf(func() { got = parseFidelity(mixed) }); mp > 8*uint64(len(mixed)) {
		t.Errorf("a mixed-ending message peaks at %d KiB, %.1f times its size", mp>>10, float64(mp)/float64(len(mixed)))
	}
	if _, ok := attNamed(got, "part-2.undecoded"); ok {
		t.Errorf("the scan cut a part the reader does not see: attachments %v", attNames(got))
	}

	// The twin: a header just under the cap is read, as ever.
	under := "--bd\r\nX-Pad: " + strings.Repeat("a", 1<<19) + "\r\nContent-Type: text/plain\r\n\r\nthe padded part\r\n--bd--\r\n"
	if got := parseFidelity(hostile(`multipart/mixed; boundary="bd"`, under)); !strings.Contains(got.PlainBody, "the padded part") || len(got.Attachments) != 0 {
		t.Errorf("a header under the cap: body %q attachments %v", got.PlainBody, attNames(got))
	}
}
