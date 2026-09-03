// Package model defines the normalized mail types used across the exporter.
//
// The reader (internal/source) translates go-pst types into these structs so
// the export layer has no compile-time dependency on the PST library.
package model

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"time"
)

// Attachment is a normalized message attachment. WriteTo streams the raw
// content lazily from the underlying store, so large attachments are not held
// in memory until they are actually written.
type Attachment struct {
	Filename  string // best-available file name (long name > short name > generated)
	MimeType  string // PidTagAttachMimeTag, may be empty
	ContentID string // PidTagAttachContentId, used to resolve inline cid: images
	Size      int64  // reported size in bytes, 0 if unknown

	// WriteTo streams the attachment bytes to w. It may be called at most once
	// per intended output; callers that need the bytes twice should buffer.
	WriteTo func(w io.Writer) (int64, error)
}

// Message is a normalized mail item.
type Message struct {
	Subject           string
	SenderName        string
	SenderEmail       string
	To                string
	Cc                string
	Bcc               string    // sent mail carries it; empty on received mail
	ReplyTo           string    // Reply-To header, when present
	InReplyTo         string    // In-Reply-To message id (threading)
	References        string    // References header (threading)
	Sent              time.Time // client submit time; zero if unknown. Zone = the original offset when known.
	Received          time.Time // message delivery time; zero if unknown. Zone = the original offset when known.
	InternetMessageID string

	// Raw holds the original RFC 822 bytes when the source has them (mbox,
	// maildir, Graph); nil for a PST/OST item. The exporter preserves it as
	// <stem>.eml when asked (KeepRaw); it is never required for rendering.
	Raw []byte

	// Body sources, in precedence order. The exporter picks the richest one
	// that is present (HTML > plain > decoded RTF).
	HTMLBody  string
	PlainBody string
	RTFBody   string

	Attachments []Attachment
}

// Date returns the most meaningful timestamp for the message: delivery time if
// known, otherwise submit time. A zero time means the date is unknown.
func (m *Message) Date() time.Time {
	if !m.Received.IsZero() {
		return m.Received
	}
	return m.Sent
}

// Identity returns a stable key used to deduplicate the message across
// incremental runs. It prefers the RFC 5322 Message-ID header; when that is
// absent (drafts, calendar-adjacent items, some generated mail) it falls back
// to a content hash so re-runs still recognize the same item.
func (m *Message) Identity() string {
	if id := strings.TrimSpace(m.InternetMessageID); id != "" {
		return "mid:" + id
	}
	return "sha:" + m.contentHash()
}

// Fingerprint is a short (16 hex) digest of the message's STABLE envelope —
// subject, sender, recipients, date, attachment names — recorded on every
// manifest entry so that a DIFFERENT message reusing an already-archived
// Message-ID in the same folder is recognized and kept, not dropped (R3/R1).
// Bodies and attachment bytes are deliberately excluded: they change when an
// on-demand source finally delivers them (a fill), and a fill must not look
// like a different message. Two messages that reuse one Message-ID with an
// identical envelope and differ only in body are treated as one (a resend).
func (m *Message) Fingerprint() string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s\x00%d\x00", m.Subject, m.SenderEmail, m.To, m.Cc, m.Date().UnixNano())
	for i, a := range m.Attachments {
		fmt.Fprintf(h, "%d:%s\x00", i, a.Filename)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// contentHash digests the fields that make a message itself. The body is part
// of it: without it, two distinct messages that share subject/sender/
// recipient/second/attachment-count (two drafts, generated mail with no
// Message-ID) would hash equal and the second would be dropped as a duplicate —
// silent data loss (R1/R3).
func (m *Message) contentHash() string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%s\x00%s\x00%d\x00%d\x00",
		m.Subject, m.SenderEmail, m.To, m.Date().UnixNano(), len(m.Attachments))
	h.Write([]byte(m.HTMLBody))
	h.Write([]byte{0})
	h.Write([]byte(m.PlainBody))
	h.Write([]byte{0})
	h.Write([]byte(m.RTFBody))
	return hex.EncodeToString(h.Sum(nil))
}
