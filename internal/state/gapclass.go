package state

import "strings"

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
)

// unreadablePrefix starts every label of a partly-read PST record.
const unreadablePrefix = "unreadable:"

// IsUnreadableLabel reports whether label marks a partly-read record.
func IsUnreadableLabel(label string) bool { return strings.HasPrefix(label, unreadablePrefix) }

// GapClass is the class of a special record gap label: unreadable:* is fillable
// (a repair in place, or a later read, may deliver it), unparsed is terminal
// (same bytes, same parser, same result), superseded is info. It returns "" for
// an ordinary content label ("body", an attachment's label, the legacy
// "unknown" sentinel): the list the label sits in (Missing: fillable, Terminal:
// terminal) decides its class — see RecordGapClass.
func GapClass(label string) string {
	switch {
	case IsUnreadableLabel(label):
		return ClassFillable
	case label == LabelUnparsed:
		return ClassTerminal
	case label == LabelSuperseded:
		return ClassInfo
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
// superseded label is info: nothing is missing).
func (r Record) terminalGap() bool {
	for _, l := range r.Terminal {
		if RecordGapClass(l, true) == ClassTerminal {
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
