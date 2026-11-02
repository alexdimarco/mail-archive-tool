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
	line("Date", in.When.Format("2006-01-02 15:04:05 MST (-07:00)"))
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
	const sealed = "not known: encrypted, key not open"
	c := p.Captured
	if c.Known {
		line("Messages", fmt.Sprintf("%d%s", c.Messages, asRecorded))
		if c.MailFrom.IsZero() {
			line("Mail dates", "none recorded")
		} else {
			line("Mail dates", fmt.Sprintf("%s to %s%s", day(c.MailFrom), day(c.MailTo), asRecorded))
		}
		if c.FirstRun.IsZero() {
			line("First run", "not known")
		} else {
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
	if planned {
		line("Files to remove", fmt.Sprintf("%d (%s)", p.Files, size(p.Bytes)))
	} else {
		line("Files removed", fmt.Sprintf("%d (%s)", in.FilesRemoved, size(in.BytesRemoved)))
	}
	anchor := p.Anchor.SHA256
	if anchor == "" {
		anchor = "not known"
	}
	line(anchorKey(p), anchor)
	if planned {
		for _, n := range p.Schedules.Names() {
			line("Schedule to stop", n)
		}
		// §8.1 "those not removed: check it yourself": the names discovery could not
		// confirm, another computer's, and the macOS custom names (conformance gap 2).
		for _, n := range p.Schedules.notRemoved() {
			line("Schedule not removed (check it yourself)", n)
		}
	} else {
		for _, n := range in.SchedulesStopped {
			line("Schedule stopped", n)
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
		line("This computer's record and key", "not applicable")
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

func day(t time.Time) string   { return t.UTC().Format("2006-01-02") }
func stamp(t time.Time) string { return t.UTC().Format("2006-01-02 15:04 UTC") }

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

// redact names a file for the record [O2-01]: in a store, "store N/" (the store
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
			prefix := fmt.Sprintf("store %d/", n)
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
