package app

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"mail-archive-tool/internal/util"
)

// Export activity (design §4.16): the history the page shows, built from the journal,
// the run records and the fingerprint ledger, and the reconciler that keeps the
// folder-dependent part of it fresh off the request path (XC4). Status comes only from
// the journal and the run record: the export directory is probed for presence, the
// marker and identity, never parsed for status, ids or hashes, and no fingerprint is
// computed from it for a run that was not sealed.

// ActCheckThis is "Check this export" (XC14), offered on any export whose folder is found.
const ActCheckThis = "check-this-export"

// CopySetPart is one part of a retry set as the page and the receipt name it.
type CopySetPart struct {
	Run         string
	RootName    string
	Part        int
	Fingerprint string // this part's own fingerprint, from the ledger
	Gone        bool   // marked gone: a recipient without it cannot check the set
}

// CopySetInfo is a retry set's fingerprint (XC15): SHA-256 over the SUMS files of the
// chain's sealed parts, in the order they sealed, never recomputed from disk.
type CopySetInfo struct {
	Fingerprint string
	Parts       []CopySetPart // in seal order
}

// CopyRunInfo is one card of Export activity.
type CopyRunInfo struct {
	Run          string
	Chain        string
	Started      time.Time
	SourceKind   string
	SourceLabel  string // for an archive-sourced run: which archive, and the stores of the selected folders (friction F9)
	Mailbox      string
	Dest         string // the export directory's recorded path
	RootName     string
	Planned      int
	Exported     int
	Failed       int
	NotAttempted int
	OriginalOnly int
	Status       CopyStatus // the sealed status; "" while unsealed
	Label        string
	Kind         string // the reconciler's observation, when there is one
	Running      bool
	Fingerprint  string // the sealed fingerprint; "" is "not recorded at export time"
	Actions      []string
	Stop         *CopyStop
	Set          *CopySetInfo // a retry set's fingerprint, for a run in a set of more than one part
	RetryOf      string
	Part         int
	Remaining    int // for "Export these N messages again" (a gone run)
	Receipt      string
	Next         string        // what to do next, in a sentence, for a card that is waiting on the owner (F13)
	SetNote      string        // for a part of a retry set: how much of the whole set is exported (F10)
	Failures     []CopyFailure // the first few messages that were not exported, from the run record (F7)
	FailuresMore int           // how many more there are
	AttachLine   string        // Extract attachments: what was made, and in which way, for a finished run that had it on (EW-E6 friction F3, F11)
	AttachWarn   string        // the messages exported without all of their attachments, and how to get them (EW-E6 friction F1)
}

// CopyFailure is one message that was not exported and why, as the card's "Which ones?" shows.
type CopyFailure struct {
	Subject string
	Folder  string
	Reason  string
}

// finishingTail ends the label of a run that was fully written and sealed but whose marker
// action is still owed: what to click, in the owner's words.
const finishingTail = " — click Check and finish to confirm it"

// copyFailuresShown is how many failed messages a card lists.
const copyFailuresShown = 20

// failuresOf lists the first messages of a run record that failed, with their reasons.
func failuresOf(path string) (list []CopyFailure, more int) {
	_ = walkMessages(path, func(p *cePlanLine, m *ceMsgLine) error {
		if m == nil || m.Status != statusFailed {
			return nil
		}
		if len(list) >= copyFailuresShown {
			more++
			return nil
		}
		list = append(list, CopyFailure{Subject: util.StripControl(p.Subject), Folder: p.FolderPath(), Reason: plainReason(util.StripControl(m.Reason))})
		return nil
	})
	return list, more
}

// plainReason words a failed message's recorded reason for the card.
func plainReason(r string) string {
	if strings.HasPrefix(r, "the export stopped while this message was being exported") {
		return "the export stopped while this message was downloading"
	}
	return r
}

var partSuffix = regexp.MustCompile(`-part(\d+)(?:-\d+)?$`)

// partOfRoot is the part number a root name carries ("<first root>-part3"), 1 for a first part.
func partOfRoot(name string) int {
	if m := partSuffix.FindStringSubmatch(name); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil {
			return n
		}
	}
	return 1
}

// journalRuns groups this host's journal lines by run, in the order each run first appears.
func journalRuns(lines []ceJournalLine) (order []string, by map[string][]ceJournalLine) {
	by = map[string][]ceJournalLine{}
	for _, l := range lines {
		if _, ok := by[l.Run]; !ok {
			order = append(order, l.Run)
		}
		by[l.Run] = append(by[l.Run], l)
	}
	return order, by
}

func lastEvent(lines []ceJournalLine, event string) *ceJournalLine {
	for i := len(lines) - 1; i >= 0; i-- {
		if lines[i].Event == event {
			return &lines[i]
		}
	}
	return nil
}

// stopFromEnd rebuilds the stop a journal `end` or `stopped` line carries ("R-g, destination").
func stopFromEnd(reason string) *CopyStop {
	rid, class, _ := strings.Cut(reason, ", ")
	if !strings.HasPrefix(rid, "R-") && rid != "internal" {
		return nil
	}
	return &CopyStop{RID: rid, Class: class}
}

// History is the Export activity page's runs, newest first: from this host's journal,
// each with its run record, the fingerprint ledger and, when the reconciler has one, its
// observation. live names the run the export slot is running now. A run whose folder
// has not been probed yet says so; nothing here waits on a probe (XC4).
func (e *CopyExportEnv) History(obs map[string]CopyObservation, live string) ([]CopyRunInfo, error) {
	lines, err := e.readJournal()
	if err != nil {
		return nil, stateErr(err)
	}
	ledger, _ := e.readLedger()
	order, by := journalRuns(lines)
	chainSets := map[string]*CopySetInfo{}
	var out []CopyRunInfo
	for i := len(order) - 1; i >= 0; i-- {
		run := order[i]
		info, ok := e.historyOne(run, by[run], ledger, obs, live)
		if !ok {
			continue
		}
		if info.Chain != "" {
			if _, seen := chainSets[info.Chain]; !seen {
				chainSets[info.Chain] = e.chainSet(info.Chain, ledger, lines)
			}
			if s := chainSets[info.Chain]; s != nil && len(s.Parts) > 1 {
				info.Set = s
			}
		}
		out = append(out, info)
	}
	// A retry set's offers and totals are those of the whole set (F9, F10); the receipt, which
	// carries the set's total, is made last.
	byRun := map[string]*CopyRunInfo{}
	for i := range out {
		byRun[out[i].Run] = &out[i]
	}
	for i := range out {
		info := &out[i]
		if info.Set != nil {
			planned := 0
			if first := firstPart(info.Set); first != "" {
				if f := byRun[first]; f != nil {
					planned = f.Planned
				}
			}
			e.applySet(info, planned)
		}
		if info.Fingerprint != "" {
			info.Receipt = receiptText(*info, info.Set)
		}
	}
	return out, nil
}

// firstPart is the run of a set's part 1.
func firstPart(set *CopySetInfo) string {
	first := set.Parts[0]
	for _, p := range set.Parts {
		if p.Part < first.Part {
			first = p
		}
	}
	return first.Run
}

// historyOne is one card. ok is false for a run the journal names with no start line.
func (e *CopyExportEnv) historyOne(run string, jl []ceJournalLine, ledger []ceLedgerLine, obs map[string]CopyObservation, live string) (CopyRunInfo, bool) {
	start := lastEvent(jl, "start")
	if start == nil {
		return CopyRunInfo{}, false
	}
	end := lastEvent(jl, "end")
	info := CopyRunInfo{Run: run, Started: start.At, SourceKind: util.StripControl(start.SourceKind), Mailbox: util.StripControl(start.Mailbox),
		Dest: util.StripControl(start.DestDir), RootName: util.StripControl(filepath.Base(start.DestDir)), Planned: start.Planned, Part: partOfRoot(filepath.Base(start.DestDir))}
	if l := lastEvent(jl, "located"); l != nil {
		info.Dest = util.StripControl(l.DestDir)
	}
	sum, serr := e.readSummaryCached(e.recordPath(run))
	hasRec := serr == nil
	if hasRec {
		h := sum.Header
		info.Chain, info.RetryOf, info.Started, info.Planned = h.Chain, h.RetryOf, h.Started, h.Planned
		info.RootName, info.Part = h.RootName, len(h.ChainParts)+1
		if h.Source.Kind == "archive" {
			info.SourceLabel = archiveCardLabel(h.Source, h.Folders)
		}
		if p := folderPath(sum); p != "" {
			info.Dest = util.StripControl(p) // where the folder is now: the located path, else the recorded root
		}
		if sum.Sealed != nil {
			c := sum.Sealed.Counts
			info.Status, info.Fingerprint = sum.Sealed.Status, sum.Sealed.Fingerprint
			if sum.Sealed.Stop != nil {
				st := *sum.Sealed.Stop // a copy: the summary may be the cached one
				info.Stop = &st
			}
			info.Exported, info.Failed, info.NotAttempted, info.OriginalOnly = c.Exported, c.Failed, c.NotAttempted, c.OriginalOnly
			if sum.Sealed.Stop != nil && sum.Sealed.Stop.InFlight != "" && info.Failed > 0 {
				// the message the stop cut off is listed as failed so a retry takes it; the card says
				// it was not tried again, instead of counting a sign-in or a disk as a bad message (F8)
				info.Failed--
				info.NotAttempted++
			}
		} else {
			info.Exported = sum.Statuses[statusExported] + sum.Statuses[statusWithoutOrig] + sum.Statuses[statusOriginalOnly]
			info.Failed = sum.Statuses[statusFailed]
			info.NotAttempted = max(0, info.Planned-info.Exported-info.Failed)
			if sum.Stopped != nil {
				info.Stop = &CopyStop{RID: sum.Stopped.RID, Class: sum.Stopped.Class}
			}
		}
	} else if end != nil {
		if IsRecordDamaged(serr) {
			info.Next = "MailArchive can't read the record it keeps of this export, so Finish, Export again and the other retry buttons are not possible. The folder itself is untouched: Open folder and Check this export still work."
		}
		info.Exported, info.Failed = end.Exported, end.Failed
		info.NotAttempted = max(0, end.Planned-end.Exported-end.Failed)
		info.Planned, info.Status, info.Fingerprint = end.Planned, CopyStatus(end.Status), end.Fingerprint
		info.Stop = stopFromEnd(end.Reason)
	} else if serr != nil && !errors.Is(serr, fs.ErrNotExist) {
		// a record of a newer format, or one that cannot be read: its own words
		info.Label = "this export's run record can't be read (" + serr.Error() + ")"
		if IsRecordDamaged(serr) {
			info.Label = "this export's record is damaged, so it can't be finished or retried from here"
			info.Next = "The folder itself is untouched: Open folder and Check this export still work."
			info.Actions = []string{ActOpenFolder, ActCheckThis}
		}
		return info, true
	} else {
		return CopyRunInfo{}, false // no record and no end: nothing to show
	}
	if info.Fingerprint == "" {
		for _, l := range ledger {
			if l.Run == run && l.Fingerprint != "" {
				info.Fingerprint = l.Fingerprint
			}
		}
	}
	if info.Stop != nil && info.Stop.RID != "" && (info.Stop.Message == "" || info.Status == CopyStatusFinishedAsPartial) {
		// An unsealed run keeps only the stop's id: its card still says why it stopped and what
		// to do, whatever the reconciler found about the folder since (F8). A run finished as
		// partial was finished from that record alone, so what its seal holds is the same short
		// reason, and its card says the same full words (the re-verification of F8).
		plan := &CopyPlan{Dest: info.Dest, Source: CopySourceInfo{Mailbox: info.Mailbox}}
		stop := *info.Stop
		stop.Message = refusalText(stop.RID, info.Exported, info.Planned, "", plan, e.StateDir)
		info.Stop = &stop
	}
	if hasRec && info.Failed > 0 {
		info.Failures, info.FailuresMore = e.failuresCached(run)
		info.FailuresMore = max(0, info.Failed-len(info.Failures))
	}
	if hasRec && sum.Sealed != nil && sum.Header.Options.Attachments {
		info.AttachLine = attachCardLine(sum.Header.Options.AttachmentsAsZip, sum.Attach)
		info.AttachWarn = attachCardWarn(sum.Attach.Again)
	}

	o, hasObs := obs[run]
	if hasObs {
		info.Kind = o.Kind
	}
	switch {
	case live == run:
		info.Running, info.Label = true, "Running"
	case hasRec && sum.Gone:
		info.Label, info.Kind = "This folder is gone", obsGone
		info.Actions = []string{ActExportThese}
		if rest, err := e.RunRemaining(run); err == nil {
			info.Remaining = len(rest)
		}
	case !hasRec && hasReconciled(jl, obsGone):
		info.Label, info.Kind = "This folder is gone", obsGone
	case hasObs && o.Kind != obsSettledRun && o.Kind != obsNoRecord && o.Label != "":
		info.Label, info.Actions = o.Label, e.cardActions(sum, o)
		info.Next = nextFor(o.Kind)
	case info.Status != "" && end != nil:
		info.Label = copyLabel(info.Status, CopyCounts{Selected: info.Planned, Exported: info.Exported, Failed: info.Failed, NotAttempted: info.NotAttempted,
			OriginalOnly: info.OriginalOnly}, info.Stop)
		info.Actions = e.endedActions(sum, info)
	case info.Status != "":
		info.Label = "Finished writing (" + copyLabel(info.Status, CopyCounts{Selected: info.Planned, Exported: info.Exported, Failed: info.Failed,
			NotAttempted: info.NotAttempted, OriginalOnly: info.OriginalOnly}, info.Stop) + ")" + finishingTail
		info.Actions = []string{ActCheckAndFinish, ActCheckThis}
		info.Next = nextFor(obsFinishing)
	default:
		info.Label = "interrupted — looking for its folder"
		if info.Stop != nil && info.Stop.RID != "" {
			info.Label = "Stopped: " + stopReason(info.Stop.RID) + " — looking for its folder"
		}
		info.Next = "MailArchive is looking for this export's folder; its buttons appear in a few seconds (up to 5 on a network drive)."
	}
	return info, true
}

// attachCardLine is the line of a finished export's card about Extract attachments: in which way the
// attachments were made, how many were, how many the reader found when some are not in a zip or a folder, and
// how many messages have a note about an attachment (EW-E6 friction F3, F11).
func attachCardLine(asZip bool, t ceAttachTotals) string {
	way, made := "loose files", ""
	if asZip {
		way = "one zip per message"
	}
	switch {
	case t.Found == 0:
		made = "none of these messages had an attachment"
	case asZip && t.Made == 0:
		made = fmt.Sprintf("none of the %s found are in a zip", commas(t.Found))
	case asZip && t.Made == t.Found:
		made = fmt.Sprintf("%s %s in %s %s", commas(t.Made), plural(t.Made, "attachment", "attachments"), commas(t.Zips), plural(t.Zips, "zip", "zips"))
	case asZip:
		made = fmt.Sprintf("%s of the %s found are in %s %s", commas(t.Made), commas(t.Found), commas(t.Zips), plural(t.Zips, "zip", "zips"))
	case t.Made == 0:
		made = fmt.Sprintf("none of the %s found were extracted", commas(t.Found))
	case t.Made == t.Found:
		made = fmt.Sprintf("%s %s from %s %s", commas(t.Made), plural(t.Made, "file", "files"), commas(t.Msgs), plural(t.Msgs, "message", "messages"))
	default:
		made = fmt.Sprintf("%s of the %s found were extracted, from %s %s", commas(t.Made), commas(t.Found), commas(t.Msgs), plural(t.Msgs, "message", "messages"))
	}
	line := "Attachments, " + way + ": " + made + "."
	if t.Noted > 0 {
		line += fmt.Sprintf(" %s %s a note about an attachment (README.txt lists them).", commas(t.Noted), plural(t.Noted, "message has", "messages have"))
	}
	return line
}

// attachCardWarn is the warning on the card of an export in which n messages were exported without
// all of their attachments (a stop, a full disk or a failed write cut them): the messages' originals are
// whole, and the way to the attachments is to select them again, because no button of the card takes a
// message that was exported (EW-E6 friction F1).
func attachCardWarn(n int) string {
	switch {
	case n <= 0:
		return ""
	case n == 1:
		return "1 message was exported without all of its attachments, because the export was stopped, the disk was full or a file could not be written. Its .eml is complete. To get its attachments, select that message again in a new export; 'Export the remaining messages' does not include it. README.txt names it, and its note in export-manifest.json says what is missing."
	}
	return fmt.Sprintf("%s messages were exported without all of their attachments, because the export was stopped, the disk was full or a file could not be written. Their .eml files are complete. To get their attachments, select those messages again in a new export; 'Export the remaining messages' does not include them. README.txt names the first %d, and each one's note in export-manifest.json says what is missing.", commas(n), attachReadmeList)
}

// archiveCardLabel names the archive an export activity card is of: "your archive, folder <name>" for the
// configured archive, "another archive folder <name>" for a folder read-only, and, in brackets, the stores of the
// folders that were selected (with the mailbox a store holds), so that two cards can be told apart (friction F9).
func archiveCardLabel(src CopySourceInfo, folders []CopyPlanFolder) string {
	label := "your archive, folder " + util.StripControl(filepath.Base(src.Archive))
	if src.Other {
		label = "another archive folder " + util.StripControl(filepath.Base(src.Archive))
	}
	mailbox := map[string]string{}
	for _, s := range src.Stores {
		mailbox[s.Store] = s.Mailbox
	}
	var stores []string
	seen := map[string]bool{}
	for _, f := range folders {
		tok, _, _ := strings.Cut(f.Folder, "/")
		if tok == "" || seen[tok] || tok == "Individually picked messages" {
			continue
		}
		seen[tok] = true
		name := util.StripControl(tok)
		if mb := mailbox[tok]; mb != "" {
			name += " — " + util.StripControl(mb)
		}
		stores = append(stores, name)
	}
	if len(stores) > 0 {
		label += " (" + strings.Join(stores, ", ") + ")"
	}
	return label
}

// nextFor is the sentence that tells the owner what to do with a card that is waiting on them
// (friction F13): finishing an interrupted export is the first of three clicks, and nothing
// else on the card says so.
func nextFor(kind string) string {
	switch kind {
	case obsInterrupt, obsEmpty, obsChanged:
		return "To continue: click Finish as partial (it keeps everything written so far and lists the rest), then click Export the remaining messages, then Start export."
	case obsFinishing:
		return "Everything was written. Click Check and finish to confirm it; nothing else is needed."
	}
	return ""
}

// hasReconciled reports whether a journal reconciled line of kind status exists among lines.
func hasReconciled(lines []ceJournalLine, status string) bool {
	for _, l := range lines {
		if l.Event == "reconciled" && l.Status == status {
			return true
		}
	}
	return false
}

// endedActions are the actions of a run that ended and whose marker action is done:
// the folder can be opened and checked, and the status says what can be exported again.
func (e *CopyExportEnv) endedActions(sum *ceRecSummary, info CopyRunInfo) []string {
	acts := []string{ActOpenFolder, ActCheckThis}
	if sum == nil { // the record is deleted: its chain is complete, nothing is left to export
		return acts
	}
	// "Export failed messages again" is offered while a failure a retry can change is left: a
	// message whose file is a link, damaged or removed fails for good, and a button whose click is
	// refused is no help (friction F3).
	switch info.Status {
	case CopyStatusPartial:
		if info.Failed > 0 && sum.RetryableFailed > 0 {
			acts = append(acts, ActExportFailed)
		}
	case CopyStatusCancelled:
		if info.Failed > 0 && sum.RetryableFailed > 0 {
			acts = append(acts, ActExportFailed)
		}
		acts = append(acts, ActExportRemaining)
	case CopyStatusFailed, CopyStatusFinishedAsPartial:
		acts = append(acts, ActExportRemaining)
	}
	return e.pruneRetryOffers(info.Run, &sum.Header, acts)
}

// cardActions are the actions a reconciler observation allows, with the folder actions
// (open, check) added where the folder is found and confirmed.
func (e *CopyExportEnv) cardActions(sum *ceRecSummary, o CopyObservation) []string {
	acts := append([]string(nil), o.Actions...)
	switch o.Kind {
	case obsRecovered, obsFinishing, obsChanged:
		if !hasAction(acts, ActCheckThis) {
			acts = append(acts, ActCheckThis)
		}
	}
	if o.Kind == obsRecovered && !hasAction(acts, ActOpenFolder) {
		acts = append([]string{ActOpenFolder}, acts...)
	}
	return acts
}

func hasAction(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// chainSet is the retry set's fingerprint as the chain's most recent ledger line that
// carries a set state saved it, with each part named from its record or its journal
// line. A part marked gone is flagged: its bytes stay in the set, and a recipient
// without it cannot check the set.
func (e *CopyExportEnv) chainSet(chain string, ledger []ceLedgerLine, journal []ceJournalLine) *CopySetInfo {
	var latest *ceLedgerLine
	for i := len(ledger) - 1; i >= 0; i-- {
		if ledger[i].Chain == chain && ledger[i].SetState != "" {
			latest = &ledger[i]
			break
		}
	}
	if latest == nil {
		return nil
	}
	own := map[string]string{}
	for _, l := range ledger {
		if l.Fingerprint != "" {
			own[l.Run] = l.Fingerprint
		}
	}
	set := &CopySetInfo{Fingerprint: latest.SetFingerprint}
	for _, run := range latest.Parts {
		p := CopySetPart{Run: run, Fingerprint: own[run], Part: 1}
		if sum, err := e.readSummaryCached(e.recordPath(run)); err == nil {
			p.RootName, p.Part, p.Gone = sum.Header.RootName, len(sum.Header.ChainParts)+1, sum.Gone
		} else {
			for _, l := range journal {
				if l.Run == run && l.Event == "start" {
					p.RootName = util.StripControl(filepath.Base(l.DestDir))
					p.Part = partOfRoot(p.RootName)
				}
			}
		}
		for _, l := range journal {
			if l.Run == run && l.Event == "reconciled" && l.Status == obsGone {
				p.Gone = true
			}
		}
		set.Parts = append(set.Parts, p)
	}
	return set
}

// Receipt is the text of "Save fingerprint receipt" (XC14): a small file the sender keeps
// outside the export, where it would prove nothing, with the fingerprint in groups of 8
// for reading aloud and, for a retry set, the set fingerprint with its part order.
func (e *CopyExportEnv) Receipt(run string) (string, error) {
	lines, err := e.readJournal()
	if err != nil {
		return "", stateErr(err)
	}
	ledger, _ := e.readLedger()
	order, by := journalRuns(lines)
	if !hasAction(order, run) {
		return "", fmt.Errorf("MailArchive has no record of that export")
	}
	info, ok := e.historyOne(run, by[run], ledger, nil, "")
	if !ok || info.Fingerprint == "" {
		return "", fmt.Errorf("the fingerprint was not recorded at export time, so there is no receipt to save")
	}
	if set := e.chainSet(info.Chain, ledger, lines); set != nil && len(set.Parts) > 1 {
		info.Set = set
		planned := 0
		if l := lastEvent(by[firstPart(set)], "start"); l != nil {
			planned = l.Planned
		}
		e.applySet(&info, planned)
	}
	return receiptText(info, info.Set), nil
}

// receiptText words the receipt of one card, with its retry set when it has one.
func receiptText(info CopyRunInfo, chain *CopySetInfo) string {
	var b strings.Builder
	fmt.Fprintf(&b, "MailArchive export fingerprint receipt\n")
	fmt.Fprintf(&b, "Keep this outside the export: it is the sender's anchor. Give the fingerprint to the recipient separately (email, phone).\n\n")
	fmt.Fprintf(&b, "Export folder: %s\n", info.RootName)
	fmt.Fprintf(&b, "Run: %s\n", info.Run)
	if info.Mailbox != "" {
		fmt.Fprintf(&b, "Mailbox: %s\n", info.Mailbox)
	}
	fmt.Fprintf(&b, "Started (UTC): %s\n", info.Started.UTC().Format("2006-01-02 15:04:05"))
	fmt.Fprintf(&b, "Counts: %d selected, %d exported, %d failed, %d not attempted\n", info.Planned, info.Exported, info.Failed, info.NotAttempted)
	fmt.Fprintf(&b, "Fingerprint (SHA-256 of SHA256SUMS.txt): %s\n", GroupFingerprint(info.Fingerprint))
	if chain != nil && len(chain.Parts) > 1 {
		if info.SetNote != "" {
			fmt.Fprintf(&b, "\nSet total: %s\n", info.SetNote)
		}
		fmt.Fprintf(&b, "\nThis export is one part of a set. Set fingerprint: %s\n", GroupFingerprint(chain.Fingerprint))
		fmt.Fprintf(&b, "It is the SHA-256 over the parts' SHA256SUMS.txt files, in the order the parts were finished:\n")
		for i, p := range chain.Parts {
			note := ""
			if p.Gone {
				note = " (marked gone: a recipient without this part cannot check the set)"
			}
			fmt.Fprintf(&b, "  %d. %s: %s%s\n", i+1, p.RootName, GroupFingerprint(p.Fingerprint), note)
		}
	}
	return b.String()
}

// GroupFingerprint is a fingerprint in groups of 8 hex digits, for reading aloud.
func GroupFingerprint(fp string) string {
	var parts []string
	for len(fp) > 8 {
		parts = append(parts, fp[:8])
		fp = fp[8:]
	}
	if fp != "" {
		parts = append(parts, fp)
	}
	return strings.Join(parts, " ")
}

// ---- the folder of a run, from its record or, once that is deleted, the journal ----

// ceFolderSpec is where a run's export folder is and what identifies it.
type ceFolderSpec struct {
	Run      string
	Path     string
	Identity ceIdentity
	Sum      *ceRecSummary // nil when the run's record is deleted
}

// folderSpec reads where run's folder was recorded and the identity recorded for it:
// from the run record, or, for a settled run whose record is deleted (§4.19: reconcile
// never probes such a run), from the journal's start line, whose run id is the nonce,
// and a later `located` line [RR-2b re-verification 2].
func (e *CopyExportEnv) folderSpec(run string) (*ceFolderSpec, error) {
	sum, err := readSummary(e.recordPath(run))
	if err == nil {
		return &ceFolderSpec{Run: run, Path: folderPath(sum), Identity: sum.Header.Identity, Sum: sum}, nil
	}
	if !errors.Is(err, fs.ErrNotExist) && !IsRecordDamaged(err) { // a damaged record is read as a deleted one: the journal knows the folder
		return nil, err
	}
	lines, _ := e.readJournal()
	var start, located *ceJournalLine
	for i := range lines {
		if lines[i].Run != run {
			continue
		}
		switch lines[i].Event {
		case "start":
			start = &lines[i]
		case "located":
			located = &lines[i]
		}
	}
	if start == nil || start.RootID == nil || start.DestDir == "" {
		return nil, fmt.Errorf("MailArchive has no record of where that export's folder is")
	}
	spec := &ceFolderSpec{Run: run, Path: start.DestDir, Identity: *start.RootID}
	if located != nil && located.DestDir != "" {
		spec.Path = located.DestDir
	}
	return spec, nil
}

// confirmedFolder probes spec's folder and applies §4.9's tuple: a folder that is not
// there, did not answer or cannot be confirmed is R-r2, a positive mismatch (another
// run id, a link, a non-directory, a replaced folder) is R-r.
func (e *CopyExportEnv) confirmedFolder(spec *ceFolderSpec) (ceFolderSeen, error) {
	seen, err, timedOut := probeBounded(spec.Path)
	switch {
	case !timedOut && (err == nil || errors.Is(err, fs.ErrNotExist)) && !seen.Exists:
		return seen, refusalR2Missing(spec.Path)
	case timedOut || (err != nil && !errors.Is(err, fs.ErrNotExist)) || !seen.Exists:
		return seen, refusalR2(spec.Path)
	}
	switch v, _ := judgeIdentity(spec.Run, spec.Identity, seen); v {
	case idMismatch:
		return seen, refusalR(spec.Path)
	case idUnconfirmed:
		return seen, refusalR2(spec.Path)
	}
	return seen, nil
}

// CopyOpenTarget is what "Open folder" reveals: the verified export root and the file
// inside it a reveal selects.
type CopyOpenTarget struct {
	Root   string
	Readme string
}

// OpenTarget is "Open folder" (XC33): it never takes a path from the request. Before the
// recorded root is opened it must be no symlink or reparse point, a directory, match the
// identity recorded at creation by §4.9's tuple, and hold the marker or the manifest. A
// symlink, a non-directory or a positive mismatch is R-r; an identity it could not
// confirm, or a folder with neither marker nor manifest, is R-r2, never R-r.
func (e *CopyExportEnv) OpenTarget(run string) (*CopyOpenTarget, error) {
	spec, err := e.folderSpec(run)
	if err != nil {
		return nil, err
	}
	seen, err := e.confirmedFolder(spec)
	if err != nil {
		return nil, err
	}
	if !seen.HasMarker {
		if fi, lerr := os.Lstat(filepath.Join(spec.Path, copyManifestName)); lerr != nil || !fi.Mode().IsRegular() {
			return nil, refusalR2(spec.Path)
		}
	}
	return &CopyOpenTarget{Root: spec.Path, Readme: filepath.Join(spec.Path, copyReadmeName)}, nil
}

// FinishRun is the activity page's "Finish as partial" for an unsealed run and "Check and
// finish" for a sealed one (XC2, XC5), both from the run record alone, in the folder
// §4.9's tuple confirms.
func (e *CopyExportEnv) FinishRun(run string) (*CopyResult, error) {
	sum, err := readSummary(e.recordPath(run))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, refuse("R-h", "This export's run record is missing, so MailArchive can't finish it — Check this export still works.")
		}
		return nil, err
	}
	if sum.Sealed != nil {
		return e.CheckAndFinish(run)
	}
	return e.FinishAsPartial(run)
}

// SetCaptureBusy tells the engine how to see a capture: while fn reports true a run backs
// off before each message, and that pause is its own, never counted toward the no-progress
// window (design §4.14). The desktop passes "a capture runs in this process, or the
// archive lock is held".
func (e *CopyExportEnv) SetCaptureBusy(fn func() bool) { e.captureBusy = fn }

// SetFreeBytes replaces how free space is read on the destination and on the state
// volume: a test seam for the free-space policy (XC11).
func (e *CopyExportEnv) SetFreeBytes(dest, state func(dir string) (uint64, error)) {
	e.freeDest, e.freeState = dest, state
}

// SetStepHook installs fn to be called after each named write of a run (the engine's own
// fault points, "W6:<n>" after message n among them): a test's seam. It runs in the run's
// goroutine, so it may block (a wedged destination) or panic (a bug in the run, which the
// DC3 recover turns into "Stopped: an internal error"). Set it before a run starts.
func (e *CopyExportEnv) SetStepHook(fn func(step string)) {
	if e.faults == nil {
		e.faults = &copyFaults{}
	}
	e.faults.hook = fn
}

// Ended reports whether the journal holds the run's `end`: no marker action is left.
func (e *CopyExportEnv) Ended(run string) bool { return e.journalHas(run, "end") }

// WedgeStop records that a run did not stop when it was cancelled, after its caller
// released the export slot and Abandoned the run (design §4.13, "a wedged run", XC10): a
// `stopped` line in the run record and a `stopped` journal event, never an `end`, so
// once the goroutine has returned, or after a restart, the run is reconciled like any
// unsealed run and Finish as partial is offered (RR2-C3).
func (e *CopyExportEnv) WedgeStop(run string) error {
	reason := "the destination is not responding"
	rec, err := e.openRecord(run)
	if err != nil {
		return err
	}
	defer rec.Close()
	if err := rec.append(ceStoppedLine{T: "stopped", RID: "R-g", Class: stopClassDestination, Reason: reason, At: e.nowTime()}); err != nil {
		return err
	}
	return e.journalAppend(ceJournalLine{Run: run, Event: "stopped", Reason: "R-g, " + stopClassDestination})
}

// ---- Check this export --------------------------------------------------------

// CopyCheck is what "Check this export" found (XC14): the recorded folder re-hashed
// against its SHA256SUMS.txt, the sums compared with the sealed fingerprint, and the
// files nothing lists.
type CopyCheck struct {
	OK       bool // GREEN: every listed file matches, the sums are the sealed ones, nothing unlisted
	Files    int  // listed files checked
	Changed  []string
	Missing  []string
	Unlisted []string
	// Fingerprint is the sealed one; FingerprintOK is false when SHA256SUMS.txt on disk
	// is not the one that was sealed. NoFingerprint is true for a run that was never
	// sealed: "fingerprint not recorded at export time", never computed from the disk.
	Fingerprint   string
	FingerprintOK bool
	NoFingerprint bool
	MarkerPresent bool
	// ReadableOnly is true when the RED is only readable copies (.html) that changed or are missing:
	// every original is as it was (slice E5), and the Remedy says so.
	ReadableOnly bool
	// DerivedOnly is true when the RED is only derived copies that changed or are missing: readable
	// copies and extracted attachments (slice E6), each a convenience made from the .eml beside it.
	// ReadableOnly implies it.
	DerivedOnly bool
	Problems    []string // the findings in words
	Remedy      string   // what to do about a RED, "" when GREEN
}

// copyCheckFileCap bounds the number of lines of SHA256SUMS.txt that Check reads.
const copyCheckSumsCap = 64 << 20

// CheckExport re-hashes every file listed in the recorded export folder's
// SHA256SUMS.txt, compares sha256(SUMS on disk) with the sealed fingerprint, and lists
// the files in the folder that nothing lists. It reads only, in the folder §4.9's tuple
// confirms (R-r or R-r2 otherwise), and takes the expected fingerprint from the run
// record's sealed line or the ledger, never from the folder.
func (e *CopyExportEnv) CheckExport(run string) (*CopyCheck, error) {
	spec, err := e.folderSpec(run)
	if err != nil {
		return nil, err
	}
	seen, err := e.confirmedFolder(spec)
	if err != nil {
		return nil, err
	}
	res := &CopyCheck{MarkerPresent: seen.HasMarker}
	if spec.Sum != nil && spec.Sum.Sealed != nil {
		res.Fingerprint = spec.Sum.Sealed.Fingerprint
	} else {
		ledger, _ := e.readLedger()
		for _, l := range ledger {
			if l.Run == run && l.Fingerprint != "" {
				res.Fingerprint = l.Fingerprint
			}
		}
	}
	res.NoFingerprint = res.Fingerprint == ""
	root, err := os.OpenRoot(spec.Path)
	if err != nil {
		return nil, refusalR2(spec.Path)
	}
	defer root.Close()
	// What is read is what this open reached, so it is judged on the handle, not only
	// on the path the probe saw a moment earlier (EW-AI1).
	switch v, _ := judgeIdentity(spec.Run, spec.Identity, seenFromRoot(root)); v {
	case idMismatch:
		return nil, refusalR(spec.Path)
	case idUnconfirmed:
		return nil, refusalR2(spec.Path)
	}

	problem := func(format string, a ...any) { res.Problems = append(res.Problems, fmt.Sprintf(format, a...)) }
	f, err := openRegularIn(root, copySumsName)
	if err != nil {
		if spec.Sum != nil && spec.Sum.Sealed == nil {
			problem("this export is unfinished: it has no %s yet, so there is nothing to check it against. Finish it first (Finish as partial, on its card)", copySumsName)
		} else {
			problem("%s is missing, so nothing in this folder can be checked", copySumsName)
		}
		res.Remedy = checkRemedy
		return res, nil
	}
	data, err := readLimited(f, copyCheckSumsCap)
	f.Close()
	if err != nil {
		problem("%s can't be read", copySumsName)
		return res, nil
	}
	if res.NoFingerprint {
		problem("fingerprint not recorded at export time: MailArchive cannot say whether %s is the one it wrote", copySumsName)
	} else if res.FingerprintOK = hexSHA256(data) == res.Fingerprint; !res.FingerprintOK {
		problem("%s is not the one that was sealed when the export finished: the files may have been re-hashed after the export", copySumsName)
	}
	listed := map[string]bool{}
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if line == "" {
			continue
		}
		hash, path, ok := strings.Cut(line, "  ")
		if !ok || len(hash) != 64 || path == "" {
			problem("a line of %s is malformed", copySumsName)
			continue
		}
		listed[path] = true
		res.Files++
		got, herr := hashRootFile(root, path)
		switch {
		case herr != nil:
			res.Missing = append(res.Missing, path)
		case got != hash:
			res.Changed = append(res.Changed, path)
		}
	}
	_ = fs.WalkDir(root.FS(), ".", func(p string, d fs.DirEntry, werr error) error {
		if werr != nil || d.IsDir() {
			return nil
		}
		if p == copySumsName || p == copyMarkerName || listed[p] {
			return nil
		}
		res.Unlisted = append(res.Unlisted, p)
		return nil
	})
	sort.Strings(res.Changed)
	sort.Strings(res.Missing)
	sort.Strings(res.Unlisted)
	// A readable copy is a convenience made from the .eml beside it: one that changed or is
	// missing is named as that, and says nothing of the original (slice E5, MA-513).
	// An extracted attachment is the same kind of file (slice E6, E6-AO4).
	readable, attachments := e.derivedPaths(run)
	nDerived, nAttach := 0, 0
	derived := func(p, what string) bool {
		switch {
		case readable[p]:
			problem("the readable copy %s "+what+". It is a convenience made from the .eml beside it, and says nothing about that .eml", p)
		case attachments[p]:
			problem("the extracted attachment %s "+what+". It is a convenience made from the .eml beside it, and says nothing about that .eml", p)
			nAttach++
		default:
			return false
		}
		nDerived++
		return true
	}
	for _, p := range res.Changed {
		if !derived(p, "has changed") {
			problem("%s has changed", p)
		}
	}
	for _, p := range res.Missing {
		if !derived(p, "is missing") {
			problem("%s is missing", p)
		}
	}
	for _, p := range res.Unlisted {
		problem("%s is in the folder but is not listed in %s: it is not part of the export", p, copySumsName)
	}
	res.OK = len(res.Problems) == 0 && res.FingerprintOK
	res.DerivedOnly = nDerived > 0 && len(res.Problems) == nDerived && res.FingerprintOK
	res.ReadableOnly = res.DerivedOnly && nAttach == 0
	switch {
	case res.ReadableOnly:
		res.Remedy = checkReadableRemedy
	case res.DerivedOnly:
		res.Remedy = checkDerivedRemedy
	case !res.OK:
		res.Remedy = checkRemedy
	}
	return res, nil
}

// derivedPaths are the readable copies and the extracted attachments the run record lists, by
// their path as the sums give it; nil when the record is gone, and the files are then reported like
// any other file.
func (e *CopyExportEnv) derivedPaths(run string) (readable, attachments map[string]bool) {
	_ = walkProgress(e.recordPath(run), func(m *ceMsgLine) error {
		for _, f := range m.Files {
			switch f.Role {
			case roleDerivedHTML:
				if readable == nil {
					readable = map[string]bool{}
				}
				readable[sumsPath(f.Path)] = true
			case roleDerivedAttach, roleDerivedAttachZip:
				if attachments == nil {
					attachments = map[string]bool{}
				}
				attachments[sumsPath(f.Path)] = true
			}
		}
		return nil
	})
	return readable, attachments
}

// checkReadableRemedy is what a RED check tells the owner when only readable copies are changed or
// missing: every original in the folder is as it was, and the copies are conveniences.
const checkReadableRemedy = "Every original in this folder is as it was: only the readable copies (.html) named above are changed or missing, and each is a convenience made from the .eml beside it. Open that .eml instead, put the copy back, or export the messages again from the Extraction workspace."

// checkDerivedRemedy is what a RED check tells the owner when only derived copies are changed or
// missing and at least one is an extracted attachment (slice E6): the originals are as they were.
const checkDerivedRemedy = "Every original in this folder is as it was: only the derived copies named above (readable pages and extracted attachments) are changed or missing, and each is a convenience made from the .eml beside it. Open that .eml instead, put the copy back, or export the messages again from the Extraction workspace."

// checkRemedy is what a RED check tells the owner to do (friction F12): the findings name the
// files, this says what follows from them.
const checkRemedy = "Do not hand this folder over as it is. Put back the files named above, or export the messages again from the Extraction workspace."

// readLimited reads r whole, refusing more than n bytes.
func readLimited(r io.Reader, n int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, n+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > n {
		return nil, errors.New("larger than the bound")
	}
	return b, nil
}

// hashReader is the SHA-256 of what r holds.
func hashReader(r io.Reader) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// hashRootFile is the SHA-256 of rel in root, read through the root.
func hashRootFile(root *os.Root, rel string) (string, error) {
	f, err := openRegularIn(root, rel)
	if err != nil {
		return "", err
	}
	defer f.Close()
	return hashReader(f)
}

// ---- retry --------------------------------------------------------------------

// R-l and the refusals a retry meets (design §7.6).
func refusalL(was, now string) *CopyRefusal {
	return refuse("R-l", fmt.Sprintf("This selection was made in %s's mailbox, but you're now signed in as %s. Load folders again to start a new selection.", was, now))
}

// RetryButton is the label of the card button a retry of kind which ("failed" or "rest") is
// started from.
func RetryButton(which string) string {
	if which == "failed" {
		return "Export failed messages again"
	}
	return "Export the remaining messages"
}

// RunMailbox is the mailbox a run exported, from its record, or its journal line once the
// record is deleted: "" when neither names one.
func (e *CopyExportEnv) RunMailbox(run string) string {
	if sum, err := readSummary(e.recordPath(run)); err == nil {
		return util.StripControl(sum.Header.Source.Mailbox)
	}
	lines, _ := e.readJournal()
	if l := lastEvent(func() []ceJournalLine {
		var own []ceJournalLine
		for _, l := range lines {
			if l.Run == run {
				own = append(own, l)
			}
		}
		return own
	}(), "start"); l != nil {
		return util.StripControl(l.Mailbox)
	}
	return ""
}

// RunSource is the source a run exported from, read from its record: its kind and, for an
// archive, its folder and whether it is another archive folder. ok is false when the record
// is gone, which a retry cannot do without (XC2).
func (e *CopyExportEnv) RunSource(run string) (CopySourceInfo, bool) {
	sum, err := readSummary(e.recordPath(run))
	if err != nil {
		return CopySourceInfo{}, false
	}
	return sum.Header.Source, true
}

// RetryInput builds the preview input of "Export failed messages again" (which
// "failed") or "Export the remaining messages" (which "rest") from run's RUN RECORD,
// never the destination's manifest (XC2, P4's second admission path): the ids, names and
// metadata come from the record, there is no seen-set and no per-id GET, and a retry sits
// outside the 10,000-pick cap and inside the 200,000 run cap. The signer must match
// (R-l). The new export goes to the next part of the set, named "<first root>-part<N>",
// whose manifest names retry_of and carries the whole chain (XC15).
func (e *CopyExportEnv) RetryInput(run, which, signer string) (*CopyPlanInput, error) {
	sum, err := readSummary(e.recordPath(run))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, refuse("R-h", "This export's run record is missing, so there is nothing to export again — Check this export still works.")
		}
		return nil, err
	}
	h := sum.Header
	if signer != "" && h.Source.Kind == "m365" && !strings.EqualFold(h.Source.Mailbox, signer) {
		return nil, refusalL(h.Source.Mailbox, signer)
	}
	var msgs []CopyMessage
	switch which {
	case "failed":
		msgs, err = e.RunFailed(run)
	case "rest":
		msgs, err = e.RunRemaining(run)
	default:
		return nil, fmt.Errorf("retry: unknown kind %q", which)
	}
	if err != nil {
		return nil, err
	}
	if len(msgs) == 0 {
		kind := h.Source.Kind
		if kind == "archive" && h.Source.Other {
			kind = kindArchiveOther
		}
		return nil, refuse("R-h", noRetryText(which, sum.Statuses[statusFailed], sum.RetryableFailed, kind))
	}
	first := h.RootName
	if len(h.ChainParts) > 0 {
		first = h.ChainParts[0].Root
	}
	first = partSuffix.ReplaceAllString(first, "")
	parts := append([]CopyChainRef(nil), h.ChainParts...)
	own := CopyChainRef{Part: len(h.ChainParts) + 1, RunID: run, Root: h.RootName}
	if sum.Sealed != nil {
		own.Fingerprint = sum.Sealed.Fingerprint
	}
	parts = append(parts, own)
	e.ledgerMu.Lock()
	n := len(e.chainRunsLocked(h.Chain))
	e.ledgerMu.Unlock()
	next := max(own.Part+1, n+1)
	chain := CopyChain{ID: h.Chain, RetryOf: run, Parts: parts, RootName: fmt.Sprintf("%s-part%d", first, next)}
	return &CopyPlanInput{Source: h.Source, Folders: foldersOf(msgs), Messages: msgs, Options: h.Options, Dest: h.Dest, Chain: chain}, nil
}

// kindArchiveOther is the kind noRetryText is given for another archive folder (read-only, someone else's).
const kindArchiveOther = "archive-other"

// noRetryText is R-h for a retry with nothing to retry (friction F3): what happened, why, and what to
// do. failed is how many messages of the run failed. A message that failed for a reason a retry
// cannot change (a link, a damaged or removed file, a message over the size limit) is never offered
// again, and the owner is told where the reasons are and what can still be done.
func noRetryText(which string, failed, retryable int, kind string) string {
	switch {
	case failed == 0 && which == "failed":
		return "There are no messages left to export from this export — no message failed in it, so there is nothing to export again."
	case failed == 0:
		return "There are no messages left to export from this export — every message in it was exported."
	case retryable > 0: // they could be tried again, and another part of the set already did
		return "There are no messages left to export from this export — the messages that failed were exported by another part of its set, so there is nothing left to do (the set's total is on this export's card)."
	}
	n := "1 message that failed"
	if failed != 1 {
		n = commas(failed) + " messages that failed"
	}
	fix := "If you can fix that, choose the folder again in the Extraction workspace and export it as a new export."
	switch kind {
	case "archive":
		fix = "Fix that first (restore a file that is missing or damaged from a backup of the archive, or copy an archive that holds links into a plain folder), then choose the folder again in the Extraction workspace and export it as a new export."
	case kindArchiveOther: // read-only, and its reader has no backup of it: the owner of the copy is the one to ask
		fix = "Fix that first (ask whoever gave you the archive for a complete copy of a file that is missing or damaged, or copy an archive that holds links into a plain folder), then choose the folder again in the Extraction workspace and export it as a new export."
	}
	return fmt.Sprintf("The %s in this export can't be tried again — each failed for a reason that trying again won't change, and the reason is listed under 'Which ones?' on this export's card. %s", n, fix)
}

// foldersOf summarizes msgs by folder path, in the order first met: the retry's
// selection section.
func foldersOf(msgs []CopyMessage) []CopyPlanFolder {
	var out []CopyPlanFolder
	at := map[string]int{}
	for i := range msgs {
		p := msgs[i].FolderPath()
		if k, ok := at[p]; ok {
			out[k].Messages++
			continue
		}
		at[p] = len(out)
		out = append(out, CopyPlanFolder{Folder: p, Messages: 1})
	}
	return out
}

// ---- the reconciler (XC4) ------------------------------------------------------

// CopyReconciler reconciles each unresolved run once per launch, in the background and
// downgrade-only, and caches what it found for the history page. Progress never waits
// on it: the cache is read under a mutex that is not held during a probe, and every
// probe is bounded.
type CopyReconciler struct {
	env *CopyExportEnv

	mu   sync.Mutex
	obs  map[string]CopyObservation
	done chan struct{}
}

// NewReconciler is a reconciler over e.
func (e *CopyExportEnv) NewReconciler() *CopyReconciler {
	return &CopyReconciler{env: e, obs: map[string]CopyObservation{}, done: make(chan struct{})}
}

// copyProbeWorkers is how many folders are probed at once at launch.
const copyProbeWorkers = 4

// Launch is the launch sweep and the once-per-launch reconcile (design §4.16): the sweep
// of the run records in the state directory runs now, before the export slot opens (it
// reads only the state directory); then each run that has a record and no terminal
// outcome, with no journal `end`, is probed in the background. The returned error is the
// sweep's.
func (r *CopyReconciler) Launch() error {
	e := r.env
	_, serr := e.SweepRunRecords()
	lines, _ := e.readJournal()
	order, by := journalRuns(lines)
	var todo []string
	for _, run := range order {
		if lastEvent(by[run], "start") != nil && lastEvent(by[run], "end") == nil {
			if _, err := os.Stat(e.recordPath(run)); err == nil {
				todo = append(todo, run)
			}
		}
	}
	go func() {
		defer close(r.done)
		work := make(chan string)
		var wg sync.WaitGroup
		for i := 0; i < copyProbeWorkers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for run := range work {
					r.reconcile(run)
				}
			}()
		}
		for _, run := range todo {
			work <- run
		}
		close(work)
		wg.Wait()
	}()
	return serr
}

// Wait blocks until the launch's background pass has finished (tests).
func (r *CopyReconciler) Wait() { <-r.done }

func (r *CopyReconciler) reconcile(run string) CopyObservation {
	obs, err := r.env.ReconcileRun(run)
	if err != nil {
		obs.Run, obs.Kind, obs.Label = run, "error", "couldn't reconcile this export: "+err.Error()
	}
	r.mu.Lock()
	r.obs[run] = obs
	r.mu.Unlock()
	return obs
}

// Record caches an outcome a user action reached (This folder is gone, say), so the
// history shows it without waiting for a probe.
func (r *CopyReconciler) Record(obs CopyObservation) {
	r.mu.Lock()
	r.obs[obs.Run] = obs
	r.mu.Unlock()
}

// LookAgain probes run's folder now (the card's "Look again", or a user action that
// changed something), bounded like every probe.
func (r *CopyReconciler) LookAgain(run string) CopyObservation { return r.reconcile(run) }

// Observations is a copy of the cached observations.
func (r *CopyReconciler) Observations() map[string]CopyObservation {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]CopyObservation, len(r.obs))
	for k, v := range r.obs {
		out[k] = v
	}
	return out
}

// Forget drops a cached observation, so a run whose outcome a user action changed shows
// what its record and the journal now say.
func (r *CopyReconciler) Forget(run string) {
	r.mu.Lock()
	delete(r.obs, run)
	r.mu.Unlock()
}
