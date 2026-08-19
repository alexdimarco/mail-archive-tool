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
	Sent              time.Time // client submit time; zero if unknown
	Received          time.Time // message delivery time; zero if unknown
	InternetMessageID string

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
	// The body is part of the identity: without it, two distinct messages that
	// share subject/sender/recipient/second/attachment-count (e.g. two drafts,
	// or generated mail with no Message-ID) would hash equal and the second
	// would be dropped as a duplicate — silent data loss (R1/R3).
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%s\x00%s\x00%d\x00%d\x00",
		m.Subject, m.SenderEmail, m.To, m.Date().UnixNano(), len(m.Attachments))
	h.Write([]byte(m.HTMLBody))
	h.Write([]byte{0})
	h.Write([]byte(m.PlainBody))
	h.Write([]byte{0})
	h.Write([]byte(m.RTFBody))
	return "sha:" + hex.EncodeToString(h.Sum(nil))
}
