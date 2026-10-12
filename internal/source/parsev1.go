package source

// FROZEN — identity inputs. Never edit except under §5's drift policy (MC28);
// MA-330/MA-331 pin it (sha256).
//
// The four functions below are a verbatim copy of parseMessage, fallbackParse,
// addressList and bytesWriterTo, renamed with a V1 suffix. The copy is taken at the
// release tag v0.7.1 (commit 066f86dbae867f00f5d0340a2ce6a83cbed2e088, "packaging:
// v0.7.1 release notes"): the code that wrote users' archives, not the working tree.
// `git diff v0.7.1 -- internal/source/mbox.go` was empty when it was taken, so
// mbox.go's own functions at the base of the slice (4148af1) were the tag's.
//
// What they call is frozen with them. go-message v0.16.0's message, mail, textproto
// and charset packages, the stdlib net/mail, mime, mime/quotedprintable and
// encoding/base64 (go1.26.9), and go-pst v6.0.2's charset label table are vendored
// under internal/source/parsev1/, so a dependency update or a toolchain move cannot
// change what these functions compute. The helpers they call for message STATE
// (importanceFromHeaders, sensitivityFromHeader, mozillaReadFlag, mozillaCategories)
// feed no identity input and stay shared with mbox.go.
//
// The panic branch is not frozen: replayV1 and stubIdentityTerms live in their own
// files, so fault-isolation's stub change never edits this one (MC7, ruling J5).

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	_ "mail-archive-tool/internal/source/parsev1/_dep/charset" // the v1 label table: go-message's, with go-pst's names
	"mail-archive-tool/internal/source/parsev1/_dep/mail"
	netmail "mail-archive-tool/internal/source/parsev1/_dep/netmail"

	"mail-archive-tool/internal/model"
)

// parseMessageV1 converts one raw RFC 5322 message into a model.Message, falling
// back to a header-only parse if full MIME parsing fails (nothing is dropped).
func parseMessageV1(data []byte) *model.Message {
	mr, err := mail.CreateReader(bytes.NewReader(data))
	if err != nil {
		return fallbackParseV1(data)
	}
	h := mr.Header

	msg := &model.Message{Raw: data}
	msg.Subject, _ = h.Subject()
	if id, err := h.MessageID(); err == nil {
		msg.InternetMessageID = id
	}
	if d, err := h.Date(); err == nil && !d.IsZero() {
		msg.Received = d     // keep the original offset: it is part of the record
		msg.IdentityDate = d // the immutable MIME Date term for identity/fingerprint
	}
	if from, err := h.AddressList("From"); err == nil && len(from) > 0 {
		msg.SenderName = from[0].Name
		msg.SenderEmail = from[0].Address
	}
	msg.To = addressListV1(&h, "To")
	msg.Cc = addressListV1(&h, "Cc")
	msg.Bcc = addressListV1(&h, "Bcc")
	msg.ReplyTo = addressListV1(&h, "Reply-To")
	msg.InReplyTo = strings.Trim(h.Get("In-Reply-To"), "<> ")
	msg.References = strings.TrimSpace(h.Get("References"))

	// Message state from headers. Read from X-Mozilla-Status wins when present
	// (Thunderbird mbox); otherwise Unread is left for the maildir "S" flag or
	// stays false for a plain mbox with no read-state signal.
	msg.Importance = importanceFromHeaders(h.Get("Importance"), h.Get("X-Priority"))
	msg.Sensitivity = sensitivityFromHeader(h.Get("Sensitivity"))
	if unread, ok := mozillaReadFlag(h.Get("X-Mozilla-Status")); ok {
		msg.Unread = unread
	}
	// Categories come ONLY from Thunderbird's local tagging (X-Mozilla-Keys),
	// never the sender-settable RFC Keywords header (QC6).
	msg.Categories = mozillaCategories(h.Get("X-Mozilla-Keys"))

	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		body, _ := io.ReadAll(p.Body)
		switch ph := p.Header.(type) {
		case *mail.InlineHeader:
			ct, _, _ := ph.ContentType()
			switch {
			case strings.HasPrefix(ct, "text/html"):
				if msg.HTMLBody == "" {
					msg.HTMLBody = string(body)
				}
			case strings.HasPrefix(ct, "text/plain"):
				if msg.PlainBody == "" {
					msg.PlainBody = string(body)
				}
			default:
				// Inline non-text (e.g. an embedded image referenced by cid).
				cid := strings.Trim(ph.Get("Content-Id"), "<>")
				name := cid
				if name == "" {
					name = "inline"
				}
				msg.Attachments = append(msg.Attachments, model.Attachment{
					Filename: name, MimeType: ct, ContentID: cid, WriteTo: bytesWriterToV1(body),
				})
			}
		case *mail.AttachmentHeader:
			filename, _ := ph.Filename()
			ct, _, _ := ph.ContentType()
			msg.Attachments = append(msg.Attachments, model.Attachment{
				Filename:  filename,
				MimeType:  ct,
				ContentID: strings.Trim(ph.Get("Content-Id"), "<>"),
				WriteTo:   bytesWriterToV1(body),
			})
		}
	}
	return msg
}

// fallbackParseV1 handles messages that are not full MIME (rare): keep the headers
// and the raw body as plain text so the message is still exported and indexed.
func fallbackParseV1(data []byte) *model.Message {
	m, err := netmail.ReadMessage(bytes.NewReader(data))
	if err != nil {
		return &model.Message{Subject: "(unparseable message)", PlainBody: string(data), Raw: data}
	}
	body, _ := io.ReadAll(m.Body)
	msg := &model.Message{
		Subject:           m.Header.Get("Subject"),
		InternetMessageID: strings.Trim(m.Header.Get("Message-Id"), "<>"),
		To:                m.Header.Get("To"),
		Cc:                m.Header.Get("Cc"),
		Bcc:               m.Header.Get("Bcc"),
		ReplyTo:           m.Header.Get("Reply-To"),
		InReplyTo:         strings.Trim(m.Header.Get("In-Reply-To"), "<> "),
		References:        strings.TrimSpace(m.Header.Get("References")),
		PlainBody:         string(body),
		Raw:               data,
	}
	if addrs, err := m.Header.AddressList("From"); err == nil && len(addrs) > 0 {
		msg.SenderName = addrs[0].Name
		msg.SenderEmail = addrs[0].Address
	}
	if d, err := m.Header.Date(); err == nil {
		msg.Received = d
		msg.IdentityDate = d // the immutable MIME Date term for identity/fingerprint
	}
	msg.Importance = importanceFromHeaders(m.Header.Get("Importance"), m.Header.Get("X-Priority"))
	msg.Sensitivity = sensitivityFromHeader(m.Header.Get("Sensitivity"))
	if unread, ok := mozillaReadFlag(m.Header.Get("X-Mozilla-Status")); ok {
		msg.Unread = unread
	}
	msg.Categories = mozillaCategories(m.Header.Get("X-Mozilla-Keys"))
	return msg
}

func addressListV1(h *mail.Header, key string) string {
	addrs, err := h.AddressList(key)
	if err != nil || len(addrs) == 0 {
		return strings.TrimSpace(h.Get(key))
	}
	var parts []string
	for _, a := range addrs {
		if a.Name != "" {
			parts = append(parts, fmt.Sprintf("%s <%s>", a.Name, a.Address))
		} else {
			parts = append(parts, a.Address)
		}
	}
	return strings.Join(parts, ", ")
}

func bytesWriterToV1(data []byte) func(io.Writer) (int64, error) {
	return func(w io.Writer) (int64, error) {
		n, err := w.Write(data)
		return int64(n), err
	}
}
