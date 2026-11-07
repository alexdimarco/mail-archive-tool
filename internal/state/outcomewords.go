package state

import (
	"strconv"
	"strings"
)

// The words a backup's outcome is told in (design-plain-failure-words; the lead's words,
// owner-delegated 2026-11-02: say what happened first, then name the remedy). One table serves
// every surface that tells how a backup ended — the run's own error text (which the last-run
// record, the run history, the alert email, BACKUP-NEEDS-ATTENTION.txt and the command line
// carry), Recent backups, the Scheduled backup card's note and `status` — so a remedy reads the
// same wherever it appears. attachments-report.tsv is named only as the details, never as the
// only explanation.

// ReportAddress is where a person reports a mistake in MailArchive (owner question Q2 of
// design-plain-failure-words; the lead's recommendation is built: the project's GitHub issues).
const ReportAddress = "https://github.com/alexdimarco/mail-archive-tool/issues"

// The remedies a partial run's kinds name, the same on every surface.
const (
	// MistakeRemedy is the remedy of a message a mistake in MailArchive kept out of the archive
	// (export-panic). It is also GapRemedy's for that reason, so the report's row, `status`'s gap
	// line and the run's own words say the same.
	MistakeRemedy = "your mail is fine (the original is untouched); please report it at " + ReportAddress
	// RetryRemedy is the remedy of what the mail source did not hand over this time (a fillable
	// gap that is not the archive's own fault).
	RetryRemedy = "each backup tries them again; if they keep failing, Status & health in MailArchive Desktop, or `mailarchive status`, says what to do"
	// SourceRemedy is the remedy of what the mail source cannot hand over as it stands (a
	// terminal gap), and of a gap whose kind this version does not know.
	SourceRemedy = "Status & health in MailArchive Desktop, or `mailarchive status`, says what to do for each"
	// PartlyReadRemedy is the remedy of a message that was only partly read.
	PartlyReadRemedy = "each backup tries them again"
)

// ArchiveWriteRemedy is the remedy of what could not be written to the archive: GapRemedy's
// own for export-failed, so the gap lines and the run's words agree.
func ArchiveWriteRemedy() string { return GapRemedy("export-failed", "", "") }

// PartialLead opens a partial run's words: what happened, before any count.
const PartialLead = "Not everything was saved."

// PartialDetails names the report as the details, never as the only explanation.
const PartialDetails = "Details: attachments-report.tsv in the archive folder."

// The kinds of what a partial run could not save, by the reason and class of each capture gap
// (design-plain-failure-words §2). Info gaps are not problems and have no kind.
const (
	KindMistake      = "mailarchive_mistake" // export-panic: a mistake in MailArchive
	KindArchiveWrite = "archive_write"       // export-failed, folder-unwritable: the archive could not be written
	KindRetried      = "retried"             // any other fillable gap: the source did not hand it over this time
	KindSource       = "source_unreadable"   // any other terminal gap: the source cannot hand it over as it stands
)

// PartialKindOf is the kind of a gap with this reason and class, "" for one that is not a
// problem (an info gap).
func PartialKindOf(reason, class string) string {
	switch {
	case class != ClassFillable && class != ClassTerminal:
		return ""
	case reason == "export-panic":
		return KindMistake
	case reason == "export-failed", reason == "folder-unwritable":
		return KindArchiveWrite
	case class == ClassFillable:
		return KindRetried
	}
	return KindSource
}

// PartialKind counts one kind's messages and folders.
type PartialKind struct {
	Messages int `json:"messages,omitempty"`
	Folders  int `json:"folders,omitempty"`
}

func (k PartialKind) empty() bool { return k.Messages == 0 && k.Folders == 0 }

// PartialCounts is what a partial run could not save, by kind: the run's last-run record and
// its history entry carry it as "partial" (additive; absent on a run that saved everything, on
// an older record and on an encrypted archive's), and `status -json` carries it in last_run.
type PartialCounts struct {
	Mistake      PartialKind `json:"mailarchive_mistake,omitzero"`
	ArchiveWrite PartialKind `json:"archive_write,omitzero"`
	Source       PartialKind `json:"source_unreadable,omitzero"`
	Retried      PartialKind `json:"retried,omitzero"`
	// Other is what a record from elsewhere counts that no kind above takes.
	Other PartialKind `json:"other,omitzero"`
	// PartlyRead is how many messages were only partly read.
	PartlyRead int `json:"partly_read,omitempty"`
}

// Add counts one gap this run observed under its kind; an info gap adds nothing.
func (p *PartialCounts) Add(g CaptureGap) {
	var k *PartialKind
	switch PartialKindOf(g.Reason, g.Class) {
	case KindMistake:
		k = &p.Mistake
	case KindArchiveWrite:
		k = &p.ArchiveWrite
	case KindRetried:
		k = &p.Retried
	case KindSource:
		k = &p.Source
	default:
		return
	}
	if g.Scope == GapScopeFolder {
		k.Folders++
	} else {
		k.Messages += g.Weight()
	}
}

// sane is p as a reader keeps it from a record anyone who can write the archive folder can
// write: nil when it is nil, counts nothing, or holds a negative count.
func (p *PartialCounts) sane() *PartialCounts {
	if p == nil || p.Empty() {
		return nil
	}
	for _, k := range []PartialKind{p.Mistake, p.ArchiveWrite, p.Source, p.Retried, p.Other} {
		if k.Messages < 0 || k.Folders < 0 {
			return nil
		}
	}
	if p.PartlyRead < 0 {
		return nil
	}
	return p
}

// Empty says nothing is counted.
func (p PartialCounts) Empty() bool {
	return p.Mistake.empty() && p.ArchiveWrite.empty() && p.Source.empty() && p.Retried.empty() && p.Other.empty() && p.PartlyRead == 0
}

// PartialSentences says, one sentence per kind and the most serious first, what a partial run
// could not save and what to do: a mistake in MailArchive, then the archive, then the source,
// then what is tried again, then what was only partly read.
func PartialSentences(p PartialCounts) []string {
	var s []string
	if !p.Mistake.empty() {
		s = append(s, (countWords(p.Mistake))+" could not be saved because of a mistake in MailArchive — "+MistakeRemedy+".")
	}
	if !p.ArchiveWrite.empty() {
		s = append(s, (countWords(p.ArchiveWrite))+" could not be written to the archive — "+ArchiveWriteRemedy()+".")
	}
	if !p.Source.empty() {
		s = append(s, (countWords(p.Source))+" cannot be read from your mail source as it stands (for example a damaged file) — "+SourceRemedy+".")
	}
	if !p.Retried.empty() {
		s = append(s, (countWords(p.Retried))+" could not be read from your mail this time — "+RetryRemedy+".")
	}
	if !p.Other.empty() {
		s = append(s, (countWords(p.Other))+" could not be saved — "+SourceRemedy+".")
	}
	if p.PartlyRead > 0 {
		s = append(s, (Count(p.PartlyRead, "message", "messages"))+" could only be partly read from your mail — "+PartlyReadRemedy+".")
	}
	return s
}

// PartialText is a partial run's words on one line: the lead, the sentences, and the details
// when the archive has a report (an encrypted archive has none).
func PartialText(p PartialCounts, withReport bool) string {
	parts := append([]string{PartialLead}, PartialSentences(p)...)
	if withReport {
		parts = append(parts, PartialDetails)
	}
	return strings.Join(parts, " ")
}

// NotDownloadedSentence says that n messages in the archive are not on this computer yet in
// full (fillable records: the mail app has not downloaded them), and that later backups add
// them: what Recent backups and Run now say about them.
func NotDownloadedSentence(n int) string {
	return (Count(n, "message", "messages")) + " " + pick(n, "is", "are") + " not on this computer in full yet: your mail app (such as Thunderbird) has not downloaded " + pick(n, "it", "them") +
		". Later backups add " + pick(n, "it", "them") + " once it has; opening " + pick(n, "it", "them") + " in your mail app makes it download " + pick(n, "it", "them") + " sooner."
}

// countWords is a kind's count in words: "4 messages", "1 folder", "2 messages and 1 folder".
func countWords(k PartialKind) string {
	var w []string
	if k.Messages > 0 {
		w = append(w, Count(k.Messages, "message", "messages"))
	}
	if k.Folders > 0 {
		w = append(w, Count(k.Folders, "folder", "folders"))
	}
	return strings.Join(w, " and ")
}

// Count is n with its noun, the digits grouped by commas: "1 message", "20,351 messages".
func Count(n int, one, many string) string {
	return thousands(n) + " " + pick(n, one, many)
}

func pick(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func thousands(n int) string {
	s := strconv.Itoa(n)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	if neg {
		s = "-" + s
	}
	return s
}
