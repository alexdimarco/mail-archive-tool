package source

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/state"
)

// poisoned makes the frozen v1 parse panic on a message that carries the word POISON,
// the seam the stub is exercised through (no real input is known that panics it).
func poisoned(t *testing.T) {
	t.Helper()
	restore := InjectParsePanics(func(data []byte) bool { return bytes.Contains(data, []byte("POISON")) })
	t.Cleanup(restore)
}

// covers: MA-318, R1, R3, R7, R10, S46
// A MIME-panicking message keeps its bytes (design-fault-isolation §4.5, F33, FC25): it
// is the "(unreadable message)" stub with Raw, the unparsed label, its transport headers
// and the START of its bytes as bounded, control-free text ending in the fixed note; its
// identity is STILL today's constant stub (ruling J8: no fold, MA-330's golden holds),
// whichever raw source it came from. The twin first: a healthy message beside it is
// untouched, and a message that merely carries the stub's subject is not mistaken for it.
func TestAPanickingMessageIsARawPreservingStub(t *testing.T) {
	poisoned(t)
	big := "From: a@example.com\r\nSubject: POISON big\r\nMessage-ID: <big@x>\r\n\r\n" + strings.Repeat("x", 200<<10) + "TAIL-BEYOND-THE-BOUND\x00\x1b[31m\r\n"
	healthy := localMail("healthy")
	lookalike := "From: a@example.com\r\nSubject: (unreadable message)\r\nMessage-ID: <look@x>\r\n\r\nthe real body\r\n"

	if m := ParseRFC822([]byte(healthy)); m.Subject != "healthy" || len(m.ReadGaps) != 0 {
		t.Fatalf("twin: a healthy message = %+v; want it parsed as ever", m)
	}
	if m := ParseRFC822([]byte(lookalike)); len(m.ReadGaps) != 0 || m.PlainBody == "" || m.InternetMessageID != "look@x" {
		t.Errorf("a message with the stub's subject = %+v; want it parsed, not taken for the stub", m)
	}

	m := ParseRFC822([]byte(big))
	if m.Subject != stubSubject || len(m.ReadGaps) != 1 || m.ReadGaps[0] != state.LabelUnparsed {
		t.Fatalf("stub = %+v; want the (unreadable message) stub labelled unparsed", m)
	}
	if !bytes.Equal(m.Raw, []byte(big)) {
		t.Error("the stub does not keep the message's bytes: -raw would write nothing")
	}
	if !strings.Contains(m.TransportHeaders, "Message-ID: <big@x>") {
		t.Errorf("transport headers %q; want the message's own", m.TransportHeaders)
	}
	if !strings.HasPrefix(m.PlainBody, "From: a@example.com\nSubject: POISON big\n") || !strings.HasSuffix(m.PlainBody, stubNote) {
		t.Errorf("the stub's text starts %q and ends with the note: %v", m.PlainBody[:60], strings.HasSuffix(m.PlainBody, stubNote))
	}
	if len(m.PlainBody) > stubTextLimit+len(stubNote) {
		t.Errorf("the stub's text is %d bytes; want at most the first 64 KiB and the note", len(m.PlainBody))
	}
	if strings.Contains(m.PlainBody, "TAIL-BEYOND-THE-BOUND") || strings.ContainsAny(m.PlainBody, "\x00\x1b\r") {
		t.Error("the stub's text holds bytes past the bound, or a control character")
	}

	// Identity: the constant stub, whatever the bytes (J8).
	other := ParseRFC822([]byte("Subject: POISON other\r\n\r\ndifferent bytes\r\n"))
	if m.Identity() != model.LegacyStubIdentity() || other.Identity() != model.LegacyStubIdentity() {
		t.Errorf("stub identities %q and %q; want today's constant %q for both (the fold is ESC-1's)", m.Identity(), other.Identity(), model.LegacyStubIdentity())
	}
	if m.ReadError == "" {
		t.Error("the stub gives no reason for its notice")
	}
}

// covers: MA-318, R1, R10, S46
// stubText cuts on a rune boundary: invalid UTF-8 (a cut in the middle of a rune, or
// bytes that never were text) becomes a replacement character, never invalid text in a
// page or an index.
func TestStubTextIsValidUTF8AtTheCut(t *testing.T) {
	data := []byte(strings.Repeat("a", stubTextLimit-1) + "é" + "tail")
	text := stubText(data)
	if !strings.HasSuffix(text, stubNote) {
		t.Fatal("no note")
	}
	if body := strings.TrimSuffix(text, stubNote); !strings.HasSuffix(body, "�") {
		t.Errorf("the cut rune was kept as %q; want one replacement character", body[len(body)-4:])
	}
	if t2 := stubText([]byte("ok \xff\xfe bytes")); !strings.Contains(t2, "ok �") {
		t.Errorf("invalid bytes read %q; want replacement characters", t2)
	}
}

// covers: MA-318, R1, R10, S46
// The raw stub through every raw reader: an mbox, a maildir and an Evolution store each
// deliver a MIME-panicking message as the stub with its bytes and label, and go on.
func TestEveryRawReaderDeliversTheRawStub(t *testing.T) {
	poisoned(t)
	poison := "From: a@example.com\r\nSubject: POISON\r\nMessage-ID: <p@x>\r\n\r\npoison body\r\n"
	dir := t.TempDir()
	mbox := filepath.Join(dir, "mbox")
	writeLocal(t, filepath.Join(mbox, "Inbox"), "From a@example.com Mon Mar  3 09:00:00 2025\n"+strings.ReplaceAll(poison, "\r\n", "\n")+"\nFrom a@example.com Mon Mar  3 09:00:00 2025\n"+strings.ReplaceAll(localMail("ok"), "\r\n", "\n")+"\n")
	maildir := filepath.Join(dir, "maildir")
	writeLocal(t, filepath.Join(maildir, "Inbox", "cur", "p:2,S"), poison)
	writeLocal(t, filepath.Join(maildir, "Inbox", "cur", "ok:2,S"), localMail("ok"))
	evo := filepath.Join(dir, "evo")
	writeLocal(t, filepath.Join(evo, "..maildir++"), "")
	writeLocal(t, filepath.Join(evo, "cur", "p"), poison)
	writeLocal(t, filepath.Join(evo, "cur", "ok"), localMail("ok"))
	for name, path := range map[string]string{"mbox": mbox, "maildir": maildir, "evolution": evo} {
		got := walkLocal(t, path, OpenOptions{})
		var stubs, healthy int
		for _, m := range got.msgs {
			switch {
			case m.Subject == stubSubject && len(m.Raw) > 0 && len(m.ReadGaps) == 1:
				stubs++
				if !strings.Contains(m.PlainBody, "poison body") {
					t.Errorf("%s: the stub's text %q does not show the message", name, m.PlainBody)
				}
			case m.Subject == "ok":
				healthy++
			}
		}
		if stubs != 1 || healthy != 1 || len(got.faults) != 0 {
			t.Errorf("%s: %d stubs, %d healthy messages, faults %+v; want 1, 1, none: a panicking message costs only itself", name, stubs, healthy, got.faults)
		}
	}
}
