package source

import (
	"encoding/binary"
	"os"
	"testing"
	"unicode/utf16"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/model"
)

// utf16le encodes s as UTF-16LE bytes (the on-disk form of a PT_MV_UNICODE
// value), for building test blobs.
func utf16le(s string) []byte {
	enc := utf16.Encode([]rune(s))
	b := make([]byte, len(enc)*2)
	for i, u := range enc {
		binary.LittleEndian.PutUint16(b[i*2:], u)
	}
	return b
}

// mvBlob builds a well-formed PT_MV_UNICODE blob (count, offset table, values)
// carrying the given values.
func mvBlob(values ...string) []byte {
	headerEnd := 4 + 4*len(values)
	offsets := make([]uint32, len(values))
	var data []byte
	pos := headerEnd
	for i, v := range values {
		offsets[i] = uint32(pos)
		b := utf16le(v)
		data = append(data, b...)
		pos += len(b)
	}
	return mvBlobRaw(uint32(len(values)), offsets, data)
}

// mvBlobRaw builds a blob with an ARBITRARY declared count and offset table
// (which need not match the data) — for hostile/truncated cases.
func mvBlobRaw(count uint32, offsets []uint32, data []byte) []byte {
	out := make([]byte, 4+4*len(offsets))
	binary.LittleEndian.PutUint32(out[0:], count)
	for i, o := range offsets {
		binary.LittleEndian.PutUint32(out[4+i*4:], o)
	}
	return append(out, data...)
}

// covers: MA-190, R10, R1, S36
// parseMVUnicode reads a well-formed multi-valued unicode blob into its values,
// and — because the blob is untrusted — bounds every quantity BEFORE it
// allocates or indexes (QC2): a hostile blob whose declared count is 0xFFFFFFFF
// with out-of-range offsets, or one truncated mid-table, returns only the values
// it can safely reach, with no over-allocation (OOM) and no panic. The parse
// stops at the first offset that is out of [headerEnd, len] or moves backwards.
func TestParseMVUnicode(t *testing.T) {
	// A well-formed two-value blob returns both values in order.
	got := assure.Reached(t, parseMVUnicode(mvBlob("Alpha", "Beta")), "parsed two-value blob")
	if len(got) != 2 || got[0] != "Alpha" || got[1] != "Beta" {
		t.Fatalf("two-value blob = %v, want [Alpha Beta]", got)
	}

	// A hostile blob: a 0xFFFFFFFF declared count with wild offsets. The count
	// clamps to (len-4)/4 before anything is sized, and the first out-of-range
	// offset stops the parse — a bounded, empty, panic-free result.
	hostile := make([]byte, 20)
	binary.LittleEndian.PutUint32(hostile[0:], 0xFFFFFFFF) // count
	binary.LittleEndian.PutUint32(hostile[4:], 0xFFFFFFFF) // offset far past the end
	binary.LittleEndian.PutUint32(hostile[8:], 0x10000000)
	binary.LittleEndian.PutUint32(hostile[12:], 4)       // below headerEnd (backwards)
	binary.LittleEndian.PutUint32(hostile[16:], 1000000) // past the end
	if h := parseMVUnicode(hostile); len(h) != 0 {
		t.Errorf("hostile blob yielded %v, want no values (all offsets out of range)", h)
	}

	// A truncated table: a valid first value, then an offset past the end. The
	// parse keeps the in-range value and stops — it never reads past len(blob).
	partial := mvBlobRaw(2, []uint32{12, 100}, utf16le("Hi"))
	if p := parseMVUnicode(partial); len(p) != 1 || p[0] != "Hi" {
		t.Errorf("partial/truncated blob = %v, want only the in-range [Hi]", p)
	}

	// A huge declared count on a long blob clamps to the absolute ceiling before
	// allocating, so the result is bounded no matter what the header claims.
	long := make([]byte, 4+4*10000)
	binary.LittleEndian.PutUint32(long[0:], 0xFFFFFFFF)
	if n := len(parseMVUnicode(long)); n > 4096 {
		t.Errorf("huge-count blob yielded %d values, want it clamped to the ceiling", n)
	}

	// Too short to hold even the count: nothing, no panic.
	if s := parseMVUnicode([]byte{1, 2}); s != nil {
		t.Errorf("sub-header blob = %v, want nil", s)
	}
}

// covers: MA-190, R10, S36
// The PST categories path is graceful in absence: the support.pst fixture
// defines no categorized items, so every message it yields carries no
// Categories and the walk completes without error (the localized readCategories
// recover never turns a normal message into a stub). This is the U-tested
// graceful-absence half of QC8; real categorized-PST end-to-end is lab-pending
// (MA-194).
func TestPSTCategoriesAbsentOnFixture(t *testing.T) {
	if _, err := os.Stat(fixture); err != nil {
		t.Skipf("fixture missing: %v", err)
	}
	r, err := Open(fixture)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer r.Close()

	var total int
	err = r.Walk(func(_ []string, m *model.Message) error {
		total++
		if len(m.Categories) != 0 {
			t.Errorf("fixture message %q unexpectedly carried categories %v", m.Subject, m.Categories)
		}
		if m.Subject == "(unreadable message)" {
			t.Errorf("a message became an unreadable stub — the categories read must not fail the message")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if total == 0 {
		t.Fatal("fixture yielded no messages")
	}
}

// covers: MA-191, R1, S36
// Thunderbird's local tags reach Categories from the X-Mozilla-Keys header ONLY
// (QC6): the keys are whitespace-separated (and trailing-space padded),
// control-stripped, de-duplicated and order-stable. The sender-settable RFC
// Keywords header is NOT a category source, so a sender cannot inject
// "categories".
func TestMozillaKeysCategories(t *testing.T) {
	// Duplicate ("work" twice), trailing padding, and a control character in a
	// key: the result is deduped, order-stable and control-free.
	raw := "From: alice@example.com\r\nSubject: Tagged\r\nMessage-ID: <k@x>\r\n" +
		"X-Mozilla-Keys: work personal wo\x07rk   \r\n\r\nbody\r\n"
	m := assure.Reached(t, ParseRFC822([]byte(raw)), "parsed tagged message")
	got := assure.Reached(t, m.Categories, "captured categories")
	// "work", then "personal", then "work" (control-stripped) is a duplicate.
	if len(got) != 2 || got[0] != "work" || got[1] != "personal" {
		t.Fatalf("categories = %v, want [work personal] (deduped, order-stable, control-stripped)", got)
	}

	// The RFC Keywords header is NOT read: a sender-supplied Keywords line yields
	// no categories (QC6).
	sender := "From: mallory@example.com\r\nSubject: Forged tags\r\nMessage-ID: <k2@x>\r\n" +
		"Keywords: Confidential, Legal-Hold\r\n\r\nbody\r\n"
	sm := assure.Reached(t, ParseRFC822([]byte(sender)), "parsed keywords message")
	if len(sm.Categories) != 0 {
		t.Errorf("RFC Keywords header populated categories %v — a sender must not inject categories (QC6)", sm.Categories)
	}
}
