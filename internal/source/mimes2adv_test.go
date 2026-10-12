package source

import (
	"strings"
	"testing"
	"time"
)

// Tombstones of the MF-S2 adversarial pass (docs/review-mime-fidelity-s2-adversarial.md):
// hostile MIME against the fidelity walker that every capture path now runs.

// covers: MA-349, R10
// MS2-AO1. A Content-Transfer-Encoding value made of comments costs time linear in its
// length: the header is capped at 1 MiB per part, and a cap that still allowed minutes of
// work would bound nothing. The comments are stripped in one pass, with the same result as
// before (the positive twin below).
func TestAManyCommentTransferEncodingIsLinear(t *testing.T) {
	useFidelity(t)

	for _, c := range []struct{ in, norm string }{
		{"base64", "base64"},
		{"BASE64 (a comment)", "base64"},
		{"(c)7-bit", "7bit"},
		{`"8-bit"`, "8bit"},
		{"quoted-printable (encoded)", "quoted-printable"},
		{"base64 (unclosed", "base64"},
		{"x (a(b)c)", "x c)"}, // a comment ends at the first ")": comments do not nest
		{"7bit x" + strings.Repeat("()", 3), "7bit x"},
	} {
		if got, _ := normalizeCTE(c.in); got != c.norm {
			t.Errorf("normalizeCTE(%q) = %q, want %q", c.in, got, c.norm)
		}
	}

	hostileCTE := "7bit x" + strings.Repeat("()", 200_000) // 400 KB, under the 1 MiB header cap
	start := time.Now()
	norm, changed := normalizeCTE(hostileCTE)
	took := time.Since(start)
	if norm != "7bit x" || !changed {
		t.Errorf("normalizeCTE of a comment flood = %.20q, %t; want \"7bit x\", true", norm, changed)
	}
	if took > time.Second {
		t.Errorf("normalizeCTE of 200,000 comments took %v; want linear time (the comment-by-comment rebuild took seconds)", took)
	}

	// Through the route every capture path takes: one part, then ten of them in a message.
	part := "--bd\r\nContent-Type: text/plain\r\nContent-Transfer-Encoding: " + hostileCTE + "\r\n\r\nhi\r\n"
	start = time.Now()
	m := safeParseMessage(hostile(`multipart/mixed; boundary="bd"`, strings.Repeat(part, 10)+"--bd--\r\n"))
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("a message of ten such parts took %v to parse; want well under a second", took)
	}
	requireNoStub(t, "a comment flood", m)
}
