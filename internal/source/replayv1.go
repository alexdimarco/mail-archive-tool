package source

import (
	"crypto/sha256"

	"mail-archive-tool/internal/model"
)

// v1Parse is the frozen v1 parser replayV1 runs. It is a variable only so a test can
// make it panic: no input is known that does, so the stub's identity (MA-330) is
// exercised through this seam.
var v1Parse = parseMessageV1

// replayV1 parses data with the frozen v1 functions (parsev1.go), under their own
// recover. It is what the v1 parser, the code that wrote users' archives, makes of the
// message: its view is the message's identity (design-mime-fidelity P1) and the
// baseline a repair measures against. panicked says v1 panicked, in which case the
// message is nil and the identity comes from stubIdentityTerms. The panic branch is
// here, not in the frozen file, so fault-isolation's change of the stub never edits it.
func replayV1(data []byte) (m *model.Message, panicked bool) {
	defer func() {
		if recover() != nil {
			m, panicked = nil, true
		}
	}()
	return v1Parse(data), false
}

// ParseRFC822V1 is what a build from before the identity view returned from
// ParseRFC822: the frozen v1 replay's message with the transport headers on it (the
// constant stub when v1 panics), and no identity terms. A run "by v1 semantics" is an
// exporter fed these (MA-347); the production routing never calls it, and it does not
// move when the content parser does.
func ParseRFC822V1(data []byte) *model.Message {
	m, panicked := replayV1(data)
	if panicked || m == nil {
		return unreadableStub()
	}
	m.TransportHeaders = headerBlock(data)
	return m
}

// identityTermsOf copies the identity inputs out of a v1 message, which is how
// they are frozen: the terms hold values, not the message's slices or readers.
func identityTermsOf(m *model.Message) model.IdentityTerms {
	t := model.IdentityTerms{
		InternetMessageID: m.InternetMessageID, Subject: m.Subject, SenderEmail: m.SenderEmail,
		To: m.To, Cc: m.Cc, Bcc: m.Bcc, IdentityDate: m.IdentityDate,
		HTMLBody: m.HTMLBody, PlainBody: m.PlainBody, RTFBody: m.RTFBody,
	}
	if len(m.Attachments) > 0 {
		t.Attachments = make([]model.IdentityAttachment, len(m.Attachments))
		for i, a := range m.Attachments {
			t.Attachments[i] = model.IdentityAttachment{Filename: a.Filename, Size: a.Size}
		}
	}
	return t
}

// v1InventoryOf is each v1 attachment's index, label, size and SHA-256: what a
// repair compares an archived zip entry with (V1C, design-mime-fidelity §4.10). The
// replay's attachments are in memory already, so this reads no input again.
func v1InventoryOf(m *model.Message) []model.V1Item {
	if len(m.Attachments) == 0 {
		return nil
	}
	items := make([]model.V1Item, len(m.Attachments))
	for i, a := range m.Attachments {
		items[i] = model.V1Item{Index: i, Label: a.Label(i)}
		if a.WriteTo == nil {
			continue
		}
		h := sha256.New()
		n, err := a.WriteTo(h)
		if err != nil {
			continue // v1 holds each part's bytes in memory, so this cannot happen; no bytes, no match
		}
		items[i].Size = n
		copy(items[i].SHA256[:], h.Sum(nil))
	}
	return items
}
