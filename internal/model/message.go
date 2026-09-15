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
	Subject     string
	SenderName  string
	SenderEmail string
	To          string
	Cc          string
	Bcc         string    // sent mail carries it; empty on received mail
	ReplyTo     string    // Reply-To header, when present
	InReplyTo   string    // In-Reply-To message id (threading)
	References  string    // References header (threading)
	Sent        time.Time // client submit time; zero if unknown. Zone = the original offset when known.
	Received    time.Time // message delivery time; zero if unknown. Zone = the original offset when known.

	// IdentityDate is the IMMUTABLE date term folded into the dedup identity and
	// fingerprint: the message's own MIME Date header, set once at parse and NEVER
	// overridden by a source's delivery-time metadata. Received, by contrast, is
	// overwritten on the Graph path with the mailbox receivedDateTime (for display
	// and file naming), which differs from the sender's Date header — so folding
	// Received would make a no-Message-ID message's sha: identity (and its
	// fingerprint) DRIFT across a pre-go-back → v5 upgrade, duplicating it and
	// phantom-marking the original gone. Folding IdentityDate keeps the identity
	// version-stable. Zero for a source that sets no MIME Date (PST/Outlook items,
	// a hand-built test message); identityTime() then falls back to Date(), so
	// those paths are unchanged.
	IdentityDate      time.Time
	InternetMessageID string

	// Raw holds the original RFC 822 bytes when the source has them (mbox,
	// maildir, Graph); nil for a PST/OST item. The exporter preserves it as
	// <stem>.eml when asked (KeepRaw); it is never required for rendering.
	Raw []byte

	// TransportHeaders is the message's internet header block as stored by the
	// source: the decoded PidTagTransportMessageHeaders (0x007D) for PST/OST,
	// or the header section of Raw for mbox/maildir/Graph. It is sender-
	// influenced text (Received / Authentication-Results / List-Id can be
	// forged), shown as-is and never trusted: it is excluded from the
	// fingerprint (a resend that only rewrites headers is still one message),
	// not indexed, and never used to synthesize an .eml. Often empty for PST
	// items that never crossed the internet.
	TransportHeaders string

	// Body sources, in precedence order. The exporter picks the richest one
	// that is present (HTML > plain > decoded RTF).
	HTMLBody  string
	PlainBody string
	RTFBody   string

	// Importance, Sensitivity and Unread are the message's mutable STATE at
	// capture time (the source's read/flag/priority markers), shown in the
	// page's "Status" row when set. Importance is "low"/"high" (empty = the
	// ordinary "normal"); Sensitivity is "personal"/"private"/"confidential"
	// (empty = normal/none); Unread is the read flag. They are a snapshot, NOT
	// part of the message's identity: they are deliberately excluded from
	// contentHash and Fingerprint — a message later marked read, or whose
	// importance changes, is still the same message (R2/R3) — and are not
	// indexed.
	Importance  string
	Sensitivity string
	Unread      bool

	// Categories are the message's classification tags at capture — a records
	// manager's own labels: the PST named "Keywords" property, Thunderbird's
	// X-Mozilla-Keys header, or Graph's categories array (empty when none).
	// Like the state fields above they are a mutable capture-time snapshot,
	// shown on the page in their OWN "Categories" row (never the Status line),
	// but NOT part of the message's identity: they are deliberately NOT
	// referenced by contentHash or Fingerprint — a re-classified message is
	// still the same message (R2/R3) — and are not indexed this increment.
	Categories []string

	// PhysID is the source's per-PHYSICAL-message discriminator (the Graph
	// immutable id), set by the source at capture — empty when the source has none
	// (local imports) or the provider withheld it. Like the state/category fields
	// above it is a capture-time value, NOT part of the message's identity: it is
	// deliberately EXCLUDED from Identity(), Fingerprint() and contentHash() (a
	// reused-id distinct message must still be told apart by content, not by a
	// transport id). The live path stores it as Record.PhysID and uses it as a
	// pre-download skip hint and a distinctness tie-breaker (closure rev-6).
	PhysID string

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

// identityTime is the version-stable date folded into the dedup identity and
// fingerprint: the immutable MIME Date (IdentityDate) when known, else Date().
// It must never reflect a source's mutable delivery time (the Graph
// receivedDateTime override), or a no-Message-ID identity would drift on upgrade.
func (m *Message) identityTime() time.Time {
	if !m.IdentityDate.IsZero() {
		return m.IdentityDate
	}
	return m.Date()
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
	fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s\x00%d\x00", m.Subject, m.SenderEmail, m.To, m.Cc, m.identityTime().UnixNano())
	for i, a := range m.Attachments {
		fmt.Fprintf(h, "%d:%s\x00", i, a.Filename)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// contentHash digests the fields that make a message itself. The body is part
// of it: without it, two distinct messages that share subject/sender/
// recipient/second/attachment-count (two drafts, generated mail with no
// Message-ID) would hash equal and the second would be dropped as a duplicate —
// silent data loss (R1/R3). Importance, Sensitivity and Unread are deliberately
// NOT part of this hash (nor of Fingerprint): they are mutable state, and
// hashing them would make a re-read that only saw a changed read/priority flag
// look like a different message.
func (m *Message) contentHash() string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%s\x00%s\x00%d\x00%d\x00",
		m.Subject, m.SenderEmail, m.To, m.identityTime().UnixNano(), len(m.Attachments))
	h.Write([]byte(m.HTMLBody))
	h.Write([]byte{0})
	h.Write([]byte(m.PlainBody))
	h.Write([]byte{0})
	h.Write([]byte(m.RTFBody))
	return hex.EncodeToString(h.Sum(nil))
}
