package source

import (
	"bytes"
	"testing"

	"mail-archive-tool/internal/model"
)

// v1AttachmentBytes is the bytes of the attachment v1 read called name, and whether v1 has one.
func v1AttachmentBytes(t *testing.T, v1 *model.Message, name string) ([]byte, bool) {
	t.Helper()
	a, ok := attNamed(v1, name)
	if !ok {
		return nil, false
	}
	return bytesOf(t, a), true
}

// covers: MA-332, MA-347, MA-349, R2, R26
// MS2-AI3. Where the walker's content differs from v1's it says so in a parse note, because
// the note is what keeps a record v1 wrote from being stamped as examined on re-read (the
// guard "no note means v1's content", MA-332): text attachment bytes kept as sent where v1
// converted them (inline text/csv; a charset v1's table converts, such as ascii or one with
// a stray space), later parts the walker keeps where v1 stopped at an empty or failing
// nested multipart, and a Subject the walker decodes where v1 showed it as stored.
func TestEveryDifferenceFromV1IsNoted(t *testing.T) {
	useFidelity(t)

	t.Run("inline text/csv in a converting charset", func(t *testing.T) {
		data := hostile(`multipart/mixed; boundary="bd"`, "--bd\r\nContent-Type: text/plain\r\n\r\nhi\r\n"+
			"--bd\r\nContent-Type: text/csv; charset=iso-8859-1\r\nContent-Disposition: inline\r\n\r\nM\xfcller\r\n--bd--\r\n")
		v1, _ := replayV1(data)
		m := safeParseMessage(data)
		old, ok := v1AttachmentBytes(t, v1, "inline")
		a, ok2 := attNamed(m, "inline")
		if !ok || !ok2 || bytes.Equal(old, bytesOf(t, a)) {
			t.Fatalf("the premise: v1 %q, walker %v, want different bytes", old, attNames(m))
		}
		if !hasNote(m, noteTextAttachmentBytes) {
			t.Errorf("the walker kept other bytes than v1 and noted %v, want text-attachment-bytes", noteCodes(m))
		}
	})

	for _, label := range []string{"ascii", `"UTF-8 "`} {
		t.Run("attachment text/plain charset "+label, func(t *testing.T) {
			data := hostile(`multipart/mixed; boundary="bd"`, "--bd\r\nContent-Type: text/plain\r\n\r\nhi\r\n"+
				"--bd\r\nContent-Type: text/plain; charset="+label+"\r\nContent-Disposition: attachment; filename=\"c.txt\"\r\n\r\ncaf\xe9\r\n--bd--\r\n")
			v1, _ := replayV1(data)
			m := safeParseMessage(data)
			old, ok := v1AttachmentBytes(t, v1, "c.txt")
			a, ok2 := attNamed(m, "c.txt")
			if !ok || !ok2 || bytes.Equal(old, bytesOf(t, a)) {
				t.Fatalf("the premise: v1 %q, walker %v, want different bytes", old, attNames(m))
			}
			if !hasNote(m, noteTextAttachmentBytes) {
				t.Errorf("the walker kept other bytes than v1 and noted %v, want text-attachment-bytes", noteCodes(m))
			}
		})
	}

	t.Run("an empty nested multipart that stopped v1", func(t *testing.T) {
		pdf := "--bd\r\nContent-Type: application/pdf\r\nContent-Disposition: attachment; filename=\"contract.pdf\"\r\n\r\nthe contract\r\n--bd--\r\n"
		data := hostile(`multipart/mixed; boundary="bd"`, "--bd\r\nContent-Type: multipart/related; boundary=\"rel\"\r\n\r\n\r\n"+pdf)
		v1, _ := replayV1(data)
		m := safeParseMessage(data)
		if _, ok := attNamed(v1, "contract.pdf"); ok {
			t.Fatalf("the premise: v1 lost contract.pdf, but has %v", attNames(v1))
		}
		if _, ok := attNamed(m, "contract.pdf"); !ok {
			t.Fatalf("the walker lost contract.pdf too: %v", attNames(m))
		}
		if !hasNote(m, noteContinuedAfterError) {
			t.Errorf("the walker kept a part v1 lost and noted %v, want continued-after-error", noteCodes(m))
		}
	})

	t.Run("an empty nested multipart that is last stopped v1 at nothing", func(t *testing.T) {
		data := hostile(`multipart/mixed; boundary="bd"`, "--bd\r\nContent-Type: text/plain\r\n\r\nhi\r\n"+
			"--bd\r\nContent-Type: multipart/related; boundary=\"rel\"\r\n\r\n\r\n--bd--\r\n")
		if m := safeParseMessage(data); hasNote(m, noteContinuedAfterError) {
			t.Errorf("nothing followed the empty multipart, but the message is noted %v", noteCodes(m))
		}
	})

	t.Run("a Subject the walker decodes and v1 showed as stored", func(t *testing.T) {
		data := []byte("From: a@x.example\r\nTo: b@x.example\r\nSubject: =??B??=0\r\nDate: Mon, 02 Jan 2023 15:04:05 +0000\r\n\r\nbody\r\n")
		v1, _ := replayV1(data)
		m := safeParseMessage(data)
		if v1.Subject == m.Subject {
			t.Fatalf("the premise: v1 %q and the walker %q should differ", v1.Subject, m.Subject)
		}
		if len(m.ParseNotes) == 0 {
			t.Errorf("the Subject is %q where v1's is %q, with no note", m.Subject, v1.Subject)
		}
	})
}
