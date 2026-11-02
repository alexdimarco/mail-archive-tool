package archdelete

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/util"
	"mail-archive-tool/internal/vault"
)

// The record's Status values (§8.1).
const (
	StatusPlanned           = "planned, nothing deleted"
	StatusComplete          = "complete"
	StatusPartial           = "not fully deleted"
	StatusRefusedAfterStart = "refused after start"
)

// DoesNotReach is §2's fixed "What Delete does not reach" line [C14].
const DoesNotReach = "a sync service's online recycle bin (OneDrive keeps deleted files about 30 days on a personal account, up to 93 on a work account) and version history; Windows File History and Previous Versions; macOS Time Machine; copies, exports or decrypted copies made elsewhere; sync tools other than those detected"

// DoesNotReachOn is DoesNotReach with only goos's own items (the 1.5.3 friction review's B4,
// MA-1019): Windows File History and Previous Versions on Windows, macOS Time Machine on a Mac,
// neither on Linux. The Desktop and the terminal show it; the saved plan and the deletion record
// keep the whole line, as they may be read on another computer.
func DoesNotReachOn(goos string) string {
	var out []string
	for _, it := range strings.Split(DoesNotReach, "; ") {
		if (strings.HasPrefix(it, "Windows ") && goos != "windows") || (strings.HasPrefix(it, "macOS ") && goos != "darwin") {
			continue
		}
		out = append(out, it)
	}
	return strings.Join(out, "; ")
}

// DoesNotReachLineOn is the "What Delete does not reach" line as shown on goos, with its final
// period (B4).
func DoesNotReachLineOn(goos string) string {
	return "What Delete does not reach: " + DoesNotReachOn(goos) + "."
}

// LocalLine is a result line as shown on goos: the result's "What Delete does not reach" line,
// which names every platform's items, becomes DoesNotReachLineOn; any other line is l.
func LocalLine(l, goos string) string {
	if l == "What Delete does not reach: "+DoesNotReach {
		return DoesNotReachLineOn(goos)
	}
	return l
}

// recordListMax is how many names of one kind the record lists.
const recordListMax = 10

// RecordInput is what the saved plan or the deletion record says (§8.1). For a
// plan (Status StatusPlanned) only Plan and the header fields are read; a deletion
// record adds what the steps did.
type RecordInput struct {
	Status   string
	When     time.Time
	Computer string
	Account  string
	Version  string
	Plan     *DeletePlan

	// What the delete did (not read for a plan).
	FilesRemoved        int
	BytesRemoved        int64
	SchedulesStopped    []string
	SchedulesNotRemoved []string
	// SchedulesAbsent are the entries the plan listed that step 2 found gone; RunNowRemoved the
	// Run now backups whose job files step 3 removed (B1, B15).
	SchedulesAbsent []string
	RunNowRemoved   []string
	// KeyForgotten is "forgotten", "kept: …" or "" (not applicable).
	KeyForgotten string
	// UnrecordedTicked is §4.3's box; for a plan, false.
	UnrecordedTicked bool
	// NotRemoved are names the archive recorded or owns that could not be removed
	// (§8 (a)); KeptFiles those kept as not the archive's (§8 (c)); LitterRemoved
	// counts §8 (b).
	NotRemoved    []string
	KeptFiles     []string
	LitterRemoved int
}

// RecordText builds the saved plan or the deletion record once (§8.1): UTF-8, one
// "Key: value" per line, Status first and only once, each value with its line
// breaks (CR, LF, CRLF) first replaced by a space and then through
// util.StripControl [R5:NF-R4-STRIPCONTROL-PROVEFAIL], a key with several values
// repeated on one line per value, and the last line "Record-SHA256: <hex>", the
// SHA-256 of every byte before it [O2-03]. A file is named only by its store's
// number and the date-and-hash parts of its stem: no store token (which can be a
// mailbox address), no folder name below a store and no subject slug is written
// [O2-01].
func RecordText(in RecordInput) []byte {
	var b strings.Builder
	line := func(key, value string) {
		b.WriteString(key)
		b.WriteString(": ")
		b.WriteString(cleanValue(value))
		b.WriteString("\n")
	}
	p := in.Plan
	planned := in.Status == StatusPlanned
	line("Status", in.Status)
	if p.Finishes != nil {
		// The delete this one finishes, its first run named (D1).
		line("Finishes", p.finishesValue())
	}
	// One form for every moment (B9): this computer's time with its offset, said once.
	line("Date", in.When.Local().Format("2006-01-02 15:04:05 -07:00"))
	line("Computer", in.Computer)
	line("Account", in.Account)
	line("MailArchive version", in.Version)
	line("Folder", p.Folder)
	line("Folder identity", p.Identity.String())
	line("Kind", p.Kind.String())
	if p.Kind == Encrypted {
		// §8.1 "the archive id for an encrypted one", recorded here or not
		// (conformance gap 3): this computer's record's ID, else the archive's own.
		switch {
		case p.Record.Recorded:
			line("Archive id", p.Record.ID)
		case p.ArchiveID != "":
			line("Archive id", p.ArchiveID)
		default:
			line("Archive id", "not known")
		}
	}
	const asRecorded = " (as recorded in the archive)"
	sealed := "not known: encrypted, key not open"
	if p.Kind != Encrypted || p.Rerun != RerunNone {
		// A folder an earlier run emptied or removed: its values are gone, not sealed — never
		// "encrypted" for a plain archive (the 1.5.3 re-walk's N5).
		sealed = "not known: the archive's files were already removed"
	}
	c := p.Captured
	if c.Known {
		line("Messages", fmt.Sprintf("%d%s", c.Messages, asRecorded))
		if c.MailFrom.IsZero() {
			line("Mail dates", "none recorded")
		} else {
			line("Mail dates", fmt.Sprintf("%s to %s%s", day(c.MailFrom), day(c.MailTo), asRecorded))
		}
		switch {
		case c.FirstRun.IsZero() && !c.OldestRun.IsZero():
			// Said why, and what the archive does record (B9).
			line("First run", "not known: the archive's history of its runs begins "+stamp(c.OldestRun)+asRecorded)
		case c.FirstRun.IsZero():
			line("First run", "not known: the archive records no history of its runs")
		default:
			line("First run", stamp(c.FirstRun)+asRecorded)
		}
	} else {
		line("Messages", sealed)
		line("Mail dates", sealed)
		line("First run", sealed)
	}
	if c.LastBackup.IsZero() {
		line("Last backup", "not known")
	} else {
		line("Last backup", stamp(c.LastBackup)+asRecorded)
	}
	if p.Finishes != nil {
		line("Files removed earlier", p.earlierValue())
	}
	if planned {
		line("Files to remove", fmt.Sprintf("%d (%s)", p.Files, size(p.Bytes)))
	} else {
		line("Files removed", fmt.Sprintf("%d (%s)", in.FilesRemoved, size(in.BytesRemoved)))
		if f := p.Finishes; f != nil && f.RemovedKnown {
			line("Files removed in all", fmt.Sprintf("%d (%s)", f.Removed+in.FilesRemoved, size(f.RemovedBytes+in.BytesRemoved)))
		}
	}
	anchor := p.Anchor.SHA256
	if anchor == "" {
		anchor = "not known"
	}
	line(anchorKey(p), anchor)
	// The schedules (B1, B15): the plan lists only what step 2 would stop (the scheduler asked),
	// each by its name and when it runs, or "none"; the record repeats those lines with what
	// happened — stopped, or already absent — and the Run now backup removed with its job file.
	// An encrypted archive, which cannot have a schedule, says nothing of none.
	if planned {
		listed := p.Schedules.Listed()
		for _, e := range listed {
			line("Schedule to stop", e.recordValue())
		}
		if len(listed) == 0 && p.Kind != Encrypted {
			line("Schedule to stop", "none")
		}
		for _, e := range p.Schedules.RunNowBackups() {
			line("Run now backup to remove", e.recordValue())
		}
		// §8.1 "those not removed: check it yourself": the names discovery could not
		// confirm, another computer's, and the macOS custom names (conformance gap 2).
		for _, n := range p.Schedules.notRemoved() {
			line("Schedule not removed (check it yourself)", n)
		}
	} else {
		for _, n := range in.SchedulesStopped {
			line("Schedule stopped", p.Schedules.recordValue(n))
		}
		for _, n := range in.SchedulesAbsent {
			line("Schedule already absent", p.Schedules.recordValue(n))
		}
		if len(in.SchedulesStopped)+len(in.SchedulesAbsent) == 0 && p.Kind != Encrypted {
			line("Schedule stopped", "none")
		}
		// Each the plan listed that step 2 neither stopped nor found absent — a delete that stopped
		// at step 2 — is said, so the record answers every line of its plan (the 1.5.3 friction
		// fixes' adversarial pass, finding 5).
		done := map[string]bool{}
		for _, n := range append(append([]string(nil), in.SchedulesStopped...), in.SchedulesAbsent...) {
			done[n] = true
		}
		for _, e := range p.Schedules.Listed() {
			if !done[e.Name] {
				line("Schedule not stopped", e.recordValue())
			}
		}
		for _, n := range in.RunNowRemoved {
			line("Run now backup removed", p.Schedules.recordValue(n))
		}
		for _, n := range in.SchedulesNotRemoved {
			line("Schedule not removed (check it yourself)", n)
		}
	}
	switch {
	case p.Kind != Encrypted || !p.Record.Recorded:
		line("This computer's record and key", "not applicable")
	case planned:
		line("This computer's record and key", plannedKeyValue(p))
	case in.KeyForgotten == "":
		// The delete did not reach step 8: the record is still there (B3, not "not applicable").
		line("This computer's record and key", "kept: the delete stopped before it reached them")
	default:
		line("This computer's record and key", in.KeyForgotten)
	}
	choice := "no"
	if in.UnrecordedTicked {
		choice = "yes"
	}
	line("Mail copies the archive did not record", fmt.Sprintf("%d; delete them too: %s", len(p.Unrecorded), choice))
	for i, it := range p.Unrecorded {
		if i == recordListMax {
			break
		}
		line("Mail copy not recorded", redact(it.Rel, p.Stores))
	}
	if !planned {
		line("Left (could not be removed)", fmt.Sprint(len(in.NotRemoved)))
		listRedacted(line, "Left", in.NotRemoved, p.Stores)
		line("Kept (not the archive's)", fmt.Sprint(len(in.KeptFiles)))
		listRedacted(line, "Kept", in.KeptFiles, p.Stores)
		line("Litter removed", fmt.Sprint(in.LitterRemoved))
	} else {
		line("Kept (not the archive's)", fmt.Sprint(len(p.Kept)))
		listRedacted(line, "Kept", p.Kept, p.Stores)
	}
	line("What Delete does not reach", DoesNotReach)
	sum := sha256.Sum256([]byte(b.String()))
	b.WriteString("Record-SHA256: " + hex.EncodeToString(sum[:]) + "\n")
	return []byte(b.String())
}

// plannedKeyValue is the saved plan's "This computer's record and key" for a recorded encrypted
// archive: what step 8 will do, as KeyLine says it on the panel, picked from the keyring's plan at
// the plan (the delete residuals' security pass, key-use finding 3), in the words step 8's own
// record uses afterwards. Builder's words, for the operator.
func plannedKeyValue(p *DeletePlan) string {
	k := p.KeyAtPlan
	switch {
	case p.KeyForgottenBefore:
		return "forgotten by the delete this one finishes"
	case k.Known && k.Last && k.NoStore:
		return "to be kept: this computer's secret store could not be opened"
	case k.Known && k.Last && !k.Remembered:
		return "record to be forgotten (no key is remembered)" // B3
	case k.Known && k.Last:
		return "to be forgotten"
	case k.Known:
		return "this copy to be forgotten; the key is kept for " + plural(len(k.Others), "other recorded copy", "other recorded copies")
	}
	return "not known: to be forgotten only if this is the last copy this computer recorded"
}

func listRedacted(line func(k, v string), key string, rels []string, stores []string) {
	for i, r := range rels {
		if i == recordListMax {
			break
		}
		line(key, redact(r, stores))
	}
}

// anchorKey words the digest line identically in the saved plan and the record
// [R3:O2-08], naming the file the digest is of: an encrypted archive with no vault
// file has another file as its anchor (del-s1a-A2-07).
func anchorKey(p *DeletePlan) string {
	if p.Kind != Encrypted {
		return "Manifest SHA-256"
	}
	switch p.Anchor.Name {
	case "", vault.FileName, vault.BackupName:
		return "Vault file SHA-256"
	case container.PointerName:
		return "Manifest pointer SHA-256"
	case container.MirrorName:
		return "Manifest pointer copy SHA-256"
	case container.EnvelopeName:
		return "Manifest envelope SHA-256"
	}
	return "Inventory SHA-256"
}

// cleanValue is §8.1's rule for every value: each line break (CR, LF, CRLF)
// replaced by a space, then util.StripControl.
func cleanValue(v string) string {
	v = strings.ReplaceAll(v, "\r\n", " ")
	v = strings.NewReplacer("\r", " ", "\n", " ").Replace(v)
	return util.StripControl(v)
}

// localStamp is a moment in this computer's time with its offset, the one form the plan's lines
// use ("2026-11-02 01:08 -05:00").
func localStamp(t time.Time) string { return t.Local().Format("2006-01-02 15:04 -07:00") }

func day(t time.Time) string { return t.UTC().Format("2006-01-02") }

// stamp is a recorded moment in the record's one form (B9): localStamp's.
func stamp(t time.Time) string { return localStamp(t) }

func size(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d bytes", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// redact names a file for the record [O2-01]: in a store, "mail folder N/" (the store
// token's number in byte order), "…/" for any folders below it, and the
// date-and-hash parts of the stem; outside the stores, only a name MailArchive
// itself gives (a fixed root name, a sealed file's hex name), else "…".
func redact(rel string, stores []string) string {
	parts := strings.Split(rel, "/")
	base := parts[len(parts)-1]
	if len(parts) >= 2 {
		n := storeNumber(stores, parts[0])
		for i := 0; n == 0 && i < len(stores); i++ {
			if strings.EqualFold(stores[i], parts[0]) {
				n = i + 1 // the same store spelled in another case (Windows, macOS)
			}
		}
		if n > 0 {
			// "mail folder N", as the terminal's plan numbers it (B12; the 1.5.3 re-walk's N16:
			// "store" was the record's jargon alone).
			prefix := fmt.Sprintf("mail folder %d/", n)
			if len(parts) > 2 {
				prefix += "…/"
			}
			return prefix + redactBase(base)
		}
		if layoutDirs[parts[0]] {
			return parts[0] + "/…/" + redactBase(base)
		}
		return "…/" + redactBase(base)
	}
	if knownRootName(base) {
		return base
	}
	return redactBase(base)
}

// redactBase keeps of a file name only what MailArchive put in it.
func redactBase(name string) string {
	switch {
	case unitRe.MatchString(name) || invShardRe.MatchString(name) || indexPageRe.MatchString(name):
		return name
	case isLeftover(name):
		if strings.HasPrefix(name, ".mailarchive-snapshot-") {
			return ".mailarchive-snapshot-…"
		}
		return ".mailarchive-….tmp"
	}
	for _, suffix := range []string{".html", "-attachments.zip", ".eml"} {
		if len(name) > len(suffix) && strings.EqualFold(name[len(name)-len(suffix):], suffix) {
			stem := name[:len(name)-len(suffix)]
			if m := stemShapeRe.FindStringSubmatch(stem); m != nil {
				if stem == m[1]+"_"+m[2] {
					return m[1] + "_" + m[2] + suffix
				}
				return m[1] + "_…_" + m[2] + suffix
			}
			return "…" + suffix
		}
	}
	return "…"
}

// layoutDirs are the folders MailArchive names itself, outside the stores.
var layoutDirs = map[string]bool{outlookPSTDir: true, "units": true, "chunks": true, "inventory": true, "program": true}

// knownRootName is a root name MailArchive itself writes.
func knownRootName(name string) bool {
	if name == vault.FileName || name == vault.BackupName || name == manifestName || name == lockName || encRootFiles[name] {
		return true
	}
	for _, n := range plainFixedNames {
		if n == name {
			return true
		}
	}
	return false
}
