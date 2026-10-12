package state

import (
	"strings"

	"mail-archive-tool/internal/util"
)

// Record gap labels. A record's Missing and Terminal lists name what one capture
// could not deliver: "body", an attachment's label, the legacy "unknown"
// sentinel, and the labels below that a reader sets through
// model.Message.ReadGaps (fault-isolation §4.1). GapClass is the SINGLE OWNER of
// the special labels' meaning (owner ruling J5): the exporter, the status
// counts, the report and probeImproves (internal/export) all ask it, and MIME
// fidelity's and re-capture's labels register here as new cases, never as a
// second rewrite.
const (
	// LabelUnreadableBody is a PST item whose RTF/HTML/plain body read panicked.
	LabelUnreadableBody = "unreadable:body"
	// LabelUnreadableAttachments is a PST item whose WHOLE attachment table failed
	// (an error or a panic). Per-part gaps are g3's Attachment.GapKind.
	LabelUnreadableAttachments = "unreadable:attachments"
	// LabelUnparsed is a raw-bytes message whose MIME parse panicked, or the
	// fallback "(unparseable message)" stub: terminal for this parser generation.
	LabelUnparsed = "unparsed"
	// LabelSuperseded is what a partly-read record's unreadable:* labels become
	// when a later capture of the same item landed under another key: metadata
	// only, nothing missing any more (FC6(4)).
	LabelSuperseded = "superseded"
	// LabelLegacyPlaceholder is what the record of an old build's collapsed
	// "(unreadable message)" stub becomes when a run first sees it (FC27): every
	// local stub of that build shared one identity, so the record stands for no
	// message in particular and nothing it could hold is still missing. Info.
	LabelLegacyPlaceholder = "legacy-placeholder"
)

// Parse-gap kinds (design-mime-fidelity §4.9, P10): what the fidelity parser could not
// decode of a message, kept as received where the bytes exist. A record carries one as a
// label made by ParseGapLabel in its Missing or Terminal list, so an older binary keeps,
// counts and reports it as an ordinary attachment gap. They register here, the single
// owner of record gap labels (rulings J5), as informational: they never make a run partial.
const (
	// GapKindUndecodedPart is a part whose transfer encoding this version does not
	// decode (x-uuencode and the like). Re-reading the same bytes gives the same answer,
	// so it is terminal on every source.
	GapKindUndecodedPart = "undecoded-part"
	// GapKindStructureLimit is the rest of a message kept unread because it is nested
	// or divided past a bound (P13). Terminal on every source.
	GapKindStructureLimit = "structure-limit"
	// GapKindFidelityFallback is a message kept as the earlier parser read it because the
	// fidelity parser failed or held less (MC4, MC19). Terminal on every source.
	GapKindFidelityFallback = "fidelity-fallback"
	// GapKindIdentityStub is a message the earlier parser could not read at all, filed
	// under that parser's placeholder identity (MC6). Terminal on every source.
	GapKindIdentityStub = "identity-stub"
	// GapKindCorruptPart is a part whose data is cut short or damaged, kept as far as it
	// decoded. A truncation in an on-demand cache can be cured by a complete copy, so it
	// is fillable there and terminal on a source that delivers a message whole (MC20).
	GapKindCorruptPart = "corrupt-part"
	// GapKindUnsplitMultipart is a multipart whose parts could not be separated, shown as
	// one body of text. Fillable on an on-demand source, terminal on Graph (MC20).
	GapKindUnsplitMultipart = "unsplit-multipart"
)

// maxParseGapDetail bounds the detail of a parse-gap label, in runes.
const maxParseGapDetail = 300

// parseGapSep delimits a parse-gap label: "\x00kind\x00detail". A name a build writes is
// control-stripped, so no other label begins with it.
const parseGapSep = "\x00"

// ParseGapLabel is the record label of a parse gap: its kind and a detail that is
// control-stripped and at most 300 runes.
func ParseGapLabel(kind, detail string) string {
	detail = util.StripControl(detail)
	if r := []rune(detail); len(r) > maxParseGapDetail {
		detail = string(r[:maxParseGapDetail])
	}
	return parseGapSep + kind + parseGapSep + detail
}

// ParseGapParts splits a parse-gap label into its kind and detail; ok is false for any
// other label.
func ParseGapParts(label string) (kind, detail string, ok bool) {
	rest, found := strings.CutPrefix(label, parseGapSep)
	if !found {
		return "", "", false
	}
	kind, detail, found = strings.Cut(rest, parseGapSep)
	if !found || kind == "" {
		return "", "", false
	}
	return kind, detail, true
}

// IsParseGapLabel reports whether label is a parse-gap label.
func IsParseGapLabel(label string) bool {
	_, _, ok := ParseGapParts(label)
	return ok
}

// parseGapFillable says a kind can be cured by a later, complete copy of the message.
func parseGapFillable(kind string) bool {
	return kind == GapKindCorruptPart || kind == GapKindUnsplitMultipart
}

// unreadablePrefix starts every label of a partly-read PST record.
const unreadablePrefix = "unreadable:"

// IsUnreadableLabel reports whether label marks a partly-read record.
func IsUnreadableLabel(label string) bool { return strings.HasPrefix(label, unreadablePrefix) }

// GapClass is the class of a special record gap label: unreadable:* is fillable
// (a repair in place, or a later read, may deliver it), unparsed is terminal
// (same bytes, same parser, same result), superseded and legacy-placeholder are
// info (nothing is missing). It returns "" for
// an ordinary content label ("body", an attachment's label, the legacy
// "unknown" sentinel): the list the label sits in (Missing: fillable, Terminal:
// terminal) decides its class — see RecordGapClass.
func GapClass(label string) string {
	switch {
	case IsUnreadableLabel(label):
		return ClassFillable
	case label == LabelUnparsed:
		return ClassTerminal
	case label == LabelSuperseded, label == LabelLegacyPlaceholder:
		return ClassInfo
	}
	if kind, _, ok := ParseGapParts(label); ok && !parseGapFillable(kind) {
		// A parse gap the same bytes will always give again. The fillable kinds follow the
		// source like any other gap (the list they sit in decides).
		return ClassTerminal
	}
	return ""
}

// RecordGapClass is the class of one entry of a record's Missing (terminalList
// false) or Terminal (true) list.
func RecordGapClass(label string, terminalList bool) string {
	if c := GapClass(label); c != "" {
		return c
	}
	if terminalList {
		return ClassTerminal
	}
	return ClassFillable
}

// hasUnreadable reports whether the record carries an unreadable:* label.
func (r Record) hasUnreadable() bool {
	for _, l := range r.Missing {
		if IsUnreadableLabel(l) {
			return true
		}
	}
	for _, l := range r.Terminal {
		if IsUnreadableLabel(l) {
			return true
		}
	}
	return false
}

// hasLabel reports whether label is in the record's Missing or Terminal list.
func (r Record) hasLabel(label string) bool {
	for _, l := range r.Missing {
		if l == label {
			return true
		}
	}
	for _, l := range r.Terminal {
		if l == label {
			return true
		}
	}
	return false
}

// terminalGap reports whether some Terminal label has the class terminal (a
// superseded label is info: nothing is missing). A parse gap is not one: the part is
// kept as received and counted on its own (UndecodableCount), so it is not "source-empty".
func (r Record) terminalGap() bool {
	for _, l := range r.Terminal {
		if !IsParseGapLabel(l) && RecordGapClass(l, true) == ClassTerminal {
			return true
		}
	}
	return false
}

// hasParseGap reports whether the record carries a parse-gap label in either list.
func (r Record) hasParseGap() bool {
	for _, l := range r.Missing {
		if IsParseGapLabel(l) {
			return true
		}
	}
	for _, l := range r.Terminal {
		if IsParseGapLabel(l) {
			return true
		}
	}
	return false
}

// HasLabels reports whether the record already carries every one of labels, in
// either list.
func (r Record) HasLabels(labels []string) bool {
	for _, l := range labels {
		if !r.hasLabel(l) {
			return false
		}
	}
	return true
}

// Unreadable reports whether the record is partly read: it carries an
// unreadable:* label.
func (r Record) Unreadable() bool { return r.hasUnreadable() }
