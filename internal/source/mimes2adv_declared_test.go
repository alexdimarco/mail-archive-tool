package source

import (
	"testing"
)

// covers: MA-337, MA-349, R1, R26
// MS2-AO5. A damaged text attachment in a legacy charset does not send the whole message
// back to v1's reading. v1 converted the bytes that decoded (to UTF-8, by the declared
// charset); the walker keeps them as received under .partial, and the "fidelity contains
// v1" check converts them the way v1 did to find them, as it does for an intact one, so
// the later attachments the walker read are not discarded with a "nothing is lost" gap.
func TestADamagedLegacyTextAttachmentDoesNotForceTheFallback(t *testing.T) {
	useFidelity(t)
	damaged := "--bd\r\nContent-Type: text/plain; charset=iso-8859-1\r\nContent-Disposition: attachment; filename=\"notes.txt\"\r\n" +
		"Content-Transfer-Encoding: base64\r\n\r\nY2Fm6SBhdSBsYWl0@@\r\n"
	second := "--bd\r\nContent-Type: text/plain; charset=\"unknown-8bit\"\r\nContent-Disposition: attachment; filename=\"second.txt\"\r\n\r\nsecond\r\n"
	pdf := "--bd\r\nContent-Type: application/pdf\r\nContent-Disposition: attachment; filename=\"important.pdf\"\r\n\r\nthe pdf\r\n--bd--\r\n"
	body := "--bd\r\nContent-Type: text/plain\r\n\r\nthe body\r\n"

	m := safeParseMessage(hostile(`multipart/mixed; boundary="bd"`, body+damaged+second+pdf))
	requireNoStub(t, "a damaged legacy text attachment", m)
	if m.FellBack {
		t.Fatalf("the message fell back to v1's reading: attachments %v, gaps %v", attNames(m), m.ParseGaps)
	}
	if a, ok := attNamed(m, "notes.txt.partial"); !ok || string(bytesOf(t, a)) != "caf\xe9 au lait" {
		t.Errorf("attachments %v, want notes.txt.partial holding the bytes that decoded, as received", attNames(m))
	}
	for _, name := range []string{"second.txt", "important.pdf"} {
		if _, ok := attNamed(m, name); !ok {
			t.Errorf("attachments %v, want %s (a part the walker read and v1 lost)", attNames(m), name)
		}
	}

	// The twin: the same attachment intact is kept as it was, and the message does not fall back.
	intact := "--bd\r\nContent-Type: text/plain; charset=iso-8859-1\r\nContent-Disposition: attachment; filename=\"notes.txt\"\r\n" +
		"Content-Transfer-Encoding: base64\r\n\r\nY2Fm6SBhdSBsYWl0\r\n"
	m = safeParseMessage(hostile(`multipart/mixed; boundary="bd"`, body+intact+second+pdf))
	if m.FellBack || len(m.Attachments) != 3 {
		t.Errorf("the intact twin: attachments %v, fell back %t", attNames(m), m.FellBack)
	}
}
