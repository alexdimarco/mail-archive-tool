package source

import (
	"bytes"
	"testing"
)

// covers: MA-332, MA-349, R1, R3, R26
// MS2-AC7. The walker never shows less than v1 in a header field either: a Message-ID with
// 8-bit bytes in it, which v1 read (it took the message through its header fallback) and the
// strict reader rejects, is kept on the page and in the index, and the check that the walker
// holds v1 sees header fields as it sees attachments and bodies. The identity is v1's, as
// ever.
func TestTheWalkerKeepsAHeaderFieldV1Read(t *testing.T) {
	useFidelity(t)
	data := bytes.Replace(corpus(t, "charset-roman8.eml"), []byte("corpus"), []byte("co\xff\xferpus"), 1)
	v1, _ := replayV1(data)
	if v1 == nil || v1.InternetMessageID == "" {
		t.Fatalf("the premise: v1 reads the Message-ID, got %+v", v1)
	}
	m := safeParseMessage(data)
	if m.InternetMessageID != v1.InternetMessageID {
		t.Errorf("the Message-ID is %q, v1's is %q", m.InternetMessageID, v1.InternetMessageID)
	}
	if m.Identity() != v1.Identity() || m.Fingerprint() != v1.Fingerprint() {
		t.Errorf("the identity moved: %s, v1's %s", m.Identity(), v1.Identity())
	}

	// A field the walker reads as well as v1 is the walker's: an ordinary message is unchanged.
	plain := corpus(t, "ctl-plain-ascii.eml")
	if got, want := safeParseMessage(plain).InternetMessageID, corpusV1Parse(plain).InternetMessageID; got != want || got == "" {
		t.Errorf("an ordinary Message-ID is %q, want %q", got, want)
	}
}
