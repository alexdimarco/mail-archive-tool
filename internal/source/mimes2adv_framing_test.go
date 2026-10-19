package source

import (
	"bytes"
	"strings"
	"testing"

	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/state"
)

// covers: MA-349, MA-333, R1, R26
// MS2-AC4. A multipart framing error does not drop what follows: the bytes from the delimiter
// the reader failed on to the end are kept, still encoded, as remaining-parts.undecoded with a
// corrupt-part gap, as the depth and part limits keep theirs (design 4.3 step 3: keep what
// was read, and the bytes are there). The sender, not a fault, decided which attachments
// would be dropped by one long delimiter line.
func TestAFramingErrorKeepsTheBytesAfterIt(t *testing.T) {
	useFidelity(t)
	pdf := randomBytes(600)
	attach := "Content-Type: application/pdf\r\nContent-Disposition: attachment; filename=\"secret.pdf\"\r\nContent-Transfer-Encoding: base64\r\n\r\n" + base64Lines(pdf)
	body := cutPrefix + "--bd" + strings.Repeat(" ", 5000) + "\r\n" + attach + "--bd--\r\n"

	var m *model.Message
	within(t, 30*1e9, func() { m = safeParseMessage(hostile(`multipart/mixed; boundary="bd"`, body)) })
	requireNoStub(t, "a padded delimiter", m)
	if !strings.Contains(m.PlainBody, "hello") {
		t.Errorf("the part before the error was lost: %q", m.PlainBody)
	}
	a, ok := attNamed(m, "remaining-parts.undecoded")
	if !ok || a.GapKind != state.GapKindCorruptPart || !bytes.Contains(bytesOf(t, a), []byte(`filename="secret.pdf"`)) {
		t.Fatalf("attachments %v, want remaining-parts.undecoded (corrupt-part) holding the unread part", attNames(m))
	}
	if !bytes.HasPrefix(bytesOf(t, a), []byte("--bd")) {
		t.Errorf("the blob does not start at the delimiter that failed: %.60q", bytesOf(t, a))
	}
	healthy(t)
}
