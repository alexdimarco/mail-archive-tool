package source

import (
	"math/rand"
	"strings"
	"testing"
	"unicode/utf8"

	"mail-archive-tool/internal/model"
)

// covers: MA-340, R7, R8, R26, R3
// C1-8, the bodies half: an unlabelled body, a us-ascii-labelled one and a utf-8-labelled
// one that hold Latin-1 bytes become valid UTF-8 ("Grüße aus Köln", no U+FFFD), noted
// legacy-8bit, and the message's identity is the frozen replay's. The page and search
// half of the row is TestLegacy8BitBodiesReachThePageAndSearch (the real pipeline); the
// headers half is TestLegacy8BitHeadersBecomeValidUTF8 (MF-S3).
func TestLegacy8BitTextBecomesValidUTF8(t *testing.T) {
	useFidelity(t)
	for _, file := range []string{"c18-latin1-body-unlabelled.eml", "c18-latin1-body-usascii.eml", "c18-latin1-body-utf8-label.eml"} {
		data := corpus(t, file)
		m := safeParseMessage(data)
		v1 := corpusV1Parse(data)
		if got := strings.TrimSpace(m.PlainBody); got != "Grüße aus Köln" {
			t.Errorf("%s: body %q, want %q", file, m.PlainBody, "Grüße aus Köln")
		}
		if !utf8.ValidString(m.PlainBody) || strings.ContainsRune(m.PlainBody, utf8.RuneError) {
			t.Errorf("%s: the body is not clean UTF-8: %q", file, m.PlainBody)
		}
		if !hasNote(m, noteLegacy8bit) {
			t.Errorf("%s: notes %v lack legacy-8bit", file, noteCodes(m))
		}
		if m.Identity() != v1.Identity() || m.Fingerprint() != v1.Fingerprint() || m.ContentDigest() != v1.ContentDigest() {
			t.Errorf("%s: the identity moved: %s %s %s, v1's %s %s %s", file, m.Identity(), m.Fingerprint(), m.ContentDigest(), v1.Identity(), v1.Fingerprint(), v1.ContentDigest())
		}
		// The positive twin: v1's body was not valid UTF-8, which the page showed as U+FFFD.
		if utf8.ValidString(v1.PlainBody) {
			t.Errorf("%s: v1's body is valid UTF-8: the test no longer shows the defect", file)
		}
	}
}

// covers: MA-340, R7, R26
// decodeText's rules (design §4.4): a label the shared table knows is decoded with it,
// exactly as v1 converted it; no label, utf-8 and us-ascii are valid UTF-8 as they are,
// else Windows-1252; any other label gives the same, with a charset-fallback note naming
// it (utf-7 says it is shown as stored). It never fails and registers nothing.
func TestDecodeTextRules(t *testing.T) {
	for _, c := range []struct {
		name, label string
		in          string
		want        string
		note        string
	}{
		{"no label, valid UTF-8", "", "Grüße", "Grüße", ""},
		{"no label, Latin-1", "", "Gr\xfc\xdfe", "Grüße", noteLegacy8bit},
		{"a quoted utf-8", `"UTF-8"`, "caf\xe9", "café", noteLegacy8bit},
		{"us-ascii with a comment", "us-ascii (Plain text)", "caf\xe9", "café", noteLegacy8bit},
		{"a known label, Latin-2", "ISO-8859-2", "\xb1", "ą", ""},
		{"a known label, Latin-1", "iso-8859-1", "Gr\xfc\xdfe", "Grüße", ""},
		{"a known label, shift_jis", "shift_jis", "\x93\xfa\x96\x7b", "日本", ""},
		{"an unknown label, UTF-8 bytes", "x-unknown", "Grüße", "Grüße", noteCharsetFallback},
		{"an unknown label, Latin-1 bytes", "unknown-8bit", "Gr\xfc\xdfe", "Grüße", noteCharsetFallback},
		{"utf-7 is shown as stored", "utf-7", "Hi +AOk-", "Hi +AOk-", noteCharsetFallback},
		{"empty input", "unknown-8bit", "", "", noteCharsetFallback},
	} {
		got, notes := decodeText(c.label, []byte(c.in))
		if got != c.want || !utf8.ValidString(got) {
			t.Errorf("%s: decodeText(%q, %q) = %q, want %q", c.name, c.label, c.in, got, c.want)
		}
		switch {
		case c.note == "" && len(notes) != 0:
			t.Errorf("%s: notes %v, want none", c.name, notes)
		case c.note != "" && (len(notes) != 1 || notes[0].Code != c.note || notes[0].Since != 2):
			t.Errorf("%s: notes %v, want the one %s (since scheme 2)", c.name, notes, c.note)
		}
	}
	if _, notes := decodeText("utf-7", []byte("x")); len(notes) != 1 || !strings.Contains(notes[0].Detail, "shown as stored") {
		t.Errorf("utf-7 note %v does not say the text is shown as stored", notes)
	}
}

// covers: MA-340, R7, R8, R26, R3
// Fuzz-seeded property: every body field of safeParseMessage(any bytes) is valid UTF-8,
// including when the content parser panics and when its result fails the "fidelity
// contains v1" check, so the fallback path shows text as clean as the walker's (MC19).
// The seeds are the corpus; the mutations come from a fixed seed, so a failure repeats.
// The header fields are not part of it: their rules are slice S3's (MA-340, headers).
func TestEveryBodyOfAnyMessageIsValidUTF8(t *testing.T) {
	useFidelity(t)
	rng := rand.New(rand.NewSource(20261011))
	mutate := func(b []byte) []byte {
		out := append([]byte(nil), b...)
		for i := 0; i < 1+rng.Intn(4) && len(out) > 0; i++ {
			switch rng.Intn(4) {
			case 0:
				out[rng.Intn(len(out))] = byte(0x80 + rng.Intn(0x80))
			case 1:
				out = out[:rng.Intn(len(out))]
			case 2:
				j := rng.Intn(len(out))
				out = append(out[:j], append([]byte{0xe9, 0xff, 0xc3}, out[j:]...)...)
			default:
				j := rng.Intn(len(out))
				out[j] ^= 1 << uint(rng.Intn(8))
			}
		}
		return out
	}
	check := func(label string, data []byte) {
		t.Helper()
		m := safeParseMessage(data)
		if m == nil {
			t.Fatalf("%s: no message", label)
		}
		for name, f := range map[string]string{"HTMLBody": m.HTMLBody, "PlainBody": m.PlainBody, "RTFBody": m.RTFBody} {
			if !utf8.ValidString(f) {
				t.Fatalf("%s: %s is not valid UTF-8: %q", label, name, f)
			}
		}
	}

	panics := func([]byte) *model.Message { panic("injected panic in the fidelity parser") }
	regresses := func(data []byte) *model.Message {
		m := parseMessage(data)
		m.HTMLBody, m.PlainBody, m.Attachments = "", "", nil // the fidelity capture holds less than v1
		return m
	}
	modes := []struct {
		name  string
		parse func([]byte) *model.Message
	}{{"the walker", nil}, {"a panicking walker", panics}, {"a walker that holds less", regresses}}

	fellBack := 0
	old := parseContent
	defer func() { parseContent = old }() // a failed check ends the test here, with the parser restored
	for _, mode := range modes {
		parseContent = mode.parse
		for _, it := range loadMimeCorpus(t) {
			check(mode.name+" "+it.name, it.data)
			if mode.parse != nil && safeParseMessage(it.data).FellBack {
				fellBack++
			}
			for k := 0; k < 12; k++ {
				check(mode.name+" a mutation of "+it.name, mutate(it.data))
			}
		}
	}
	// The positive twin: the injected faults really took the fallback path.
	if fellBack < 100 {
		t.Errorf("only %d corpus messages fell back under the injected faults: the property does not cover the fallback path", fellBack)
	}
}
