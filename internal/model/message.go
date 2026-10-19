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

	"mail-archive-tool/internal/util"
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

	// Embedded is a PST attached item (method 5) opened as a message, which
	// WriteZip renders as an entry of its own (design-mime-fidelity §4.8). Nothing
	// sets it before slice S6.
	Embedded *Message

	// GapKind names a parse gap for this part (design-mime-fidelity §4.9) and
	// GapDetail says what is not as the sender sent it. With GapKind set, a nil
	// WriteTo means nothing was archived and a non-nil one yields the bytes as
	// received. Nothing sets them before slice S2.
	GapKind, GapDetail string

	// ZipName is the final zip entry name, computed once before rendering so the
	// page, the placeholders and the gaps block name what the zip holds
	// (design-mime-fidelity §4.5, MC33). Empty until slice S3 computes it.
	ZipName string
}

// Label is the attachment's human-readable name in reports and gap lists: its
// file name, else cid:<Content-ID>, else attachment-<index>. It is also what a
// record's Missing list and the v1 inventory call the attachment.
//
// A name comes from the sender, so the label is control-stripped where it is made: a record
// label that began with a NUL would pass for a parse-gap label (state.ParseGapLabel), and a
// tab or line break would split a report row (MS2-AO4).
func (a Attachment) Label(index int) string {
	if name := util.StripControl(a.Filename); name != "" {
		return name
	}
	if cid := util.StripControl(a.ContentID); cid != "" {
		return "cid:" + cid
	}
	return fmt.Sprintf("attachment-%d", index)
}

// IdentityAttachment is one attachment as the identity view holds it: the name
// and size Fingerprint and ContentDigest read.
type IdentityAttachment struct {
	Filename string
	Size     int64
}

// IdentityTerms are the v1 parser's values of every input to Identity(),
// Fingerprint(), ContentDigest() and contentHash(). A raw-MIME message carries
// them whatever parser produced its displayed content, so no parser change moves
// an archived message's key, fingerprint or content hash (design-mime-fidelity
// P1, R27). The attachments are in v1 order.
type IdentityTerms struct {
	InternetMessageID, Subject, SenderEmail, To, Cc, Bcc string
	IdentityDate                                         time.Time
	HTMLBody, PlainBody, RTFBody                         string
	Attachments                                          []IdentityAttachment
	// NoIdentityDate says the identity has no date, so identityTime does not fall back
	// to the message's own Date(). The stub identity of a message the v1 parse panicked
	// on is dateless, and the message shown under it (the walker's) carries display dates.
	NoIdentityDate bool
}

// ParseNote is one fidelity event a parse recorded: a way the message differs
// from what the v1 parser produced (design-mime-fidelity §4.10). Since is the
// parse scheme that introduced Code. Nothing records one before slice S2.
type ParseNote struct {
	Code   string
	Since  int
	Detail string
}

// ParseGap is a message-level parse gap (design-mime-fidelity §4.9): a part or the
// whole message could not be decoded as the sender sent it, and Detail says what is not,
// in words a person reads on the page and in the report. Kind is one of state's
// GapKind* constants; a gap of one attachment sits on the attachment (GapKind) instead.
type ParseGap struct {
	Kind, Detail string
}

// V1Item is one attachment of the v1 replay: what a repair's precondition
// compares an archived zip entry with (design-mime-fidelity §4.10 V1C).
type V1Item struct {
	Index  int
	Label  string
	Size   int64
	SHA256 [32]byte
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

	// Locator names where a LOCAL reader found the message in its store, stable for
	// the life of that item: pst-nid:<store-uid>/<nid> for a PST item,
	// a maildir file's is ReadLocator, below; Graph sets none (fault-isolation §4.2). It is the key a recovery or a healing matches on (a healthy read of an
	// item that was read only in part finds the partial record by it) and the
	// locator of its ledger gap. Like PhysID it is a capture-time value that is
	// NEVER part of Identity(), Fingerprint() or contentHash(): no identity anywhere
	// folds a locator (FC7), so a healthy input's keys are exactly what they were
	// (P14).
	Locator string

	// ReadLocator is where a LOCAL raw-bytes reader (maildir) found the message in its
	// store: maildir:<folder>/<unique>, the locator a file-unreadable or
	// vanished-during-read gap of that file is recorded at, so the message's capture
	// closes that gap as recovered (§4.3 step 2). It is NOT Locator: the export step's
	// own faults for such a message keep their ident: locator (the post-parse rule of
	// §4.6), and the record never carries it. Like Locator it is a capture-time value
	// that is never part of Identity(), Fingerprint() or contentHash().
	ReadLocator string

	// ReadGaps are the labels of what the reader could not read of an item it
	// could partly read (state.LabelUnreadableBody, state.LabelUnreadableAttachments,
	// state.LabelUnparsed): the exporter records each in the record's gap lists, the
	// KEEP guard never lets such a capture replace an archived record, and the page
	// carries a notice (§4.7). It is empty for every healthy message and for an item
	// with nothing readable, which is a ledger gap and has no Message at all (FC7).
	ReadGaps []string

	// ReadError is the bounded detail of why the item was only partly read, shown in
	// the record notice. Untrusted: the exporter strips and bounds it.
	ReadError string

	// IdentityTerms are the v1 parser's values of every identity input. Non-nil, they
	// are what Identity(), Fingerprint(), ContentDigest() and the file stem read;
	// nil, the content fields above are the identity inputs, as for a PST item or a
	// hand-built message (idv). The raw-MIME parse always sets it.
	IdentityTerms *IdentityTerms

	// ParseNotes are the fidelity events of the parse that produced the content
	// fields (design-mime-fidelity §4.10). Empty while the content is v1's.
	ParseNotes []ParseNote

	// ParseGaps are the message-level parse gaps of the parse that produced the content
	// fields: an unsplit multipart, a structure limit, a fallback to v1's content, a
	// stub identity. A gap of one attachment is that attachment's GapKind. It is not
	// ReadGaps: a parse gap is deterministic and is no degraded read, so the KEEP guard
	// never holds a capture back for one (design-mime-fidelity §4.9, rulings J5).
	ParseGaps []ParseGap

	// V1Inventory is each attachment the v1 replay produced: its index, label, size
	// and SHA-256, the baseline a repair's "never to less" check compares an
	// archived zip with (V1C). Set by the raw-MIME parse; nil for a PST item.
	V1Inventory []V1Item

	// FellBack says this parse used v1's content because the content parser
	// panicked (design-mime-fidelity P13).
	FellBack bool

	Attachments []Attachment
}

// idv is the identity view: the one place the identity inputs are read from. It is
// IdentityTerms when set and the content fields otherwise, so a message nothing
// has set terms on behaves exactly as it always did.
func (m *Message) idv() IdentityTerms {
	if m.IdentityTerms != nil {
		return *m.IdentityTerms
	}
	t := IdentityTerms{
		InternetMessageID: m.InternetMessageID, Subject: m.Subject, SenderEmail: m.SenderEmail,
		To: m.To, Cc: m.Cc, Bcc: m.Bcc, IdentityDate: m.IdentityDate,
		HTMLBody: m.HTMLBody, PlainBody: m.PlainBody, RTFBody: m.RTFBody,
	}
	if len(m.Attachments) > 0 {
		t.Attachments = make([]IdentityAttachment, len(m.Attachments))
		for i, a := range m.Attachments {
			t.Attachments[i] = IdentityAttachment{Filename: a.Filename, Size: a.Size}
		}
	}
	return t
}

// IdentitySubject is the subject as the identity view holds it. A file stem is
// built from it, never from the displayed Subject, so a parser that decodes the
// subject differently cannot rename an archived message's files
// (design-mime-fidelity MC8).
func (m *Message) IdentitySubject() string { return m.idv().Subject }

// SetInternetMessageID sets a Message-ID a source learned out of band (the Graph
// listing's, for a message whose MIME could not be parsed). It feeds the identity
// as well as the content, which the identity view would otherwise shadow.
func (m *Message) SetInternetMessageID(id string) {
	m.InternetMessageID = id
	if m.IdentityTerms != nil {
		m.IdentityTerms.InternetMessageID = id
	}
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
// t is the identity view's terms: its IdentityDate is read, and Date() is still the
// message's own, so a Received override feeds a Date-less identity as it always did.
func (m *Message) identityTime(t IdentityTerms) time.Time {
	if !t.IdentityDate.IsZero() {
		return t.IdentityDate
	}
	if t.NoIdentityDate {
		return time.Time{}
	}
	return m.Date()
}

// Identity returns a stable key used to deduplicate the message across
// incremental runs. It prefers the RFC 5322 Message-ID header; when that is
// absent (drafts, calendar-adjacent items, some generated mail) it falls back
// to a content hash so re-runs still recognize the same item.
func (m *Message) Identity() string {
	if id := strings.TrimSpace(m.idv().InternetMessageID); id != "" {
		return "mid:" + id
	}
	return "sha:" + m.contentHash()
}

// LegacyStubIdentity is the identity an older build gave EVERY local item it could
// not read: its stub was `&Message{Subject: "(unreadable message)"}`, an empty
// message whose identity is the content hash of its empty fields, the same for
// every stub, which is why they collapsed into one record per folder (F33). A run
// re-classes those records (state.Manifest.ReclassLegacyPlaceholders, FC27). The
// PST reader no longer mints a stub (FC7); the mbox and maildir readers still do
// until FI-S2b, and a run re-classes their record again on its next pass.
func LegacyStubIdentity() string {
	return (&Message{Subject: "(unreadable message)"}).Identity()
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
	t := m.idv()
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s\x00%d\x00", t.Subject, t.SenderEmail, t.To, t.Cc, m.identityTime(t).UnixNano())
	for i, a := range t.Attachments {
		fmt.Fprintf(h, "%d:%s\x00", i, a.Filename)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// FingerprintNoAttachments is Fingerprint() as it would be computed over zero
// attachments: exactly what a record's fingerprint is when the capture lost its
// whole attachment table (unreadable:attachments set no attachments). A later
// healthy read of the same item compares THIS with that record's fingerprint, so
// it lands on the partly-read record's key instead of minting a distinct
// "Message-ID reuse" (§4.7.4(1), FC6).
func (m *Message) FingerprintNoAttachments() string {
	bare := *m
	bare.Attachments = nil
	if m.IdentityTerms != nil {
		// The terms hold their own attachments, which Fingerprint reads instead.
		terms := *m.IdentityTerms
		terms.Attachments = nil
		bare.IdentityTerms = &terms
	}
	return bare.Fingerprint()
}

// ContentDigest is the live-path PhysID closure's churn-vs-distinct arbiter (design
// closure rev-6.2): the hash the exporter stores as Record.ContentHash and compares
// to decide whether a Message-ID reuse whose immutable id it has not seen is the
// SAME message (a reissued/copied id → adopt) or a genuinely DISTINCT one (→ split).
//
// It MUST be at least as discriminating as Fingerprint() — otherwise the closure,
// which lets a ContentDigest match OVERRIDE the fingerprint #fp-split, could adopt
// (and thus drop) two messages that Fingerprint would keep apart. So it folds every
// field Fingerprint trusts — Subject, SenderEmail, To, Cc, the immutable date, and
// each attachment's name AND size — PLUS Bcc and all body variants (the content
// Fingerprint deliberately omits). A distinct reuse differing only in an attachment,
// Cc/Bcc, or body therefore gets a DIFFERENT digest and is split, not dropped
// (adversarial re-check 2026-09-16). It excludes transport headers, importance/
// categories and the immutable id itself, so a header rewrite or re-classification
// does not make one message look like another. It is NOT contentHash() and is NOT
// part of Identity()/Fingerprint() — changing it never shifts a message's key.
func (m *Message) ContentDigest() string {
	t := m.idv()
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s\x00%s\x00%d\x00",
		t.Subject, t.SenderEmail, t.To, t.Cc, t.Bcc, m.identityTime(t).UnixNano())
	for i, a := range t.Attachments {
		fmt.Fprintf(h, "%d:%s:%d\x00", i, a.Filename, a.Size)
	}
	h.Write([]byte(t.HTMLBody))
	h.Write([]byte{0})
	h.Write([]byte(t.PlainBody))
	h.Write([]byte{0})
	h.Write([]byte(t.RTFBody))
	return hex.EncodeToString(h.Sum(nil))
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
	t := m.idv()
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%s\x00%s\x00%d\x00%d\x00",
		t.Subject, t.SenderEmail, t.To, m.identityTime(t).UnixNano(), len(t.Attachments))
	h.Write([]byte(t.HTMLBody))
	h.Write([]byte{0})
	h.Write([]byte(t.PlainBody))
	h.Write([]byte{0})
	h.Write([]byte(t.RTFBody))
	return hex.EncodeToString(h.Sum(nil))
}
