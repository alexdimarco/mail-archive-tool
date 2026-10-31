package desktop

import (
	"os"
	"path/filepath"
	"strings"

	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/thunderbird"
)

// What the archive was made from, read from the manifest's store ids ("path:..." is a local mail
// file or folder, "mailbox:..." a Microsoft 365 mailbox). The desktop reads, searches, checks and
// exports any of them; only the Microsoft 365 ones can be topped up from here (MA-546, docs/trying-the-extraction-workspace.md).
const (
	kindNone  = ""      // no archive location, or no manifest to read
	kindEmpty = "empty" // an archive with no source recorded yet
	kindM365  = "m365"
	kindLocal = "local"
	kindMixed = "mixed"
	// kindEncrypted is an encrypted archive: what it was made from is sealed.
	kindEncrypted = "encrypted"
)

// archiveKind says what the archive at out was made from. A manifest that cannot be read is
// kindNone: nothing is hidden or disabled on a guess.
func archiveKind(out string) string {
	if strings.TrimSpace(out) == "" {
		return kindNone
	}
	// An encrypted archive's sources are sealed: it is labelled, and the features a
	// source kind enables (pre-sync, local schedule rebuild) stay off (phase2a §5).
	if enc, _, _ := encmark.Here(out); enc {
		return kindEncrypted
	}
	mpath := filepath.Join(out, manifestName)
	if _, err := os.Stat(mpath); err != nil {
		return kindNone
	}
	m, err := state.Load(mpath)
	if err != nil {
		return kindNone
	}
	var local, cloud bool
	for id := range m.Stores {
		if strings.HasPrefix(id, "mailbox:") {
			cloud = true
		} else {
			local = true
		}
	}
	switch {
	case local && cloud:
		return kindMixed
	case local:
		return kindLocal
	case cloud:
		return kindM365
	}
	return kindEmpty
}

const manifestName = ".mailarchive-manifest.json"

// kindLine is the plain-words line that says what the archive is.
func kindLine(kind string) string {
	switch kind {
	case kindLocal:
		return "This archive was made from local mail files (for example Thunderbird), not from Microsoft 365."
	case kindM365:
		return "This archive was made from Microsoft 365."
	case kindMixed:
		return "This archive holds both Microsoft 365 mail and mail from local files (for example Thunderbird)."
	case kindEmpty:
		return "This archive has no mail in it yet."
	case kindEncrypted:
		return "This archive is encrypted: its messages, its search index and what it was made from are sealed, and open only with its password or recovery key." // MA-731
	}
	return ""
}

// localOnly is whether Archive now and the weekly backup cannot apply: the archive has only local
// mail and Microsoft 365 was never set up here. A Microsoft 365, mixed or empty archive, or one
// with a Microsoft 365 setup, keeps them (the owner may be about to add a mailbox).
//
// An encrypted archive's sources are sealed, so its kind is read from what this computer
// holds: a backup job set up here for it reads local mail files only (job.Job has no
// Microsoft 365 source), so such an archive is local whatever else is set up; with no job,
// it is local when Microsoft 365 was never set up here, as a plaintext one is (P3-8, B25).
func (cfg Config) localOnly() bool {
	out := cfg.effectiveOut()
	switch archiveKind(out) {
	case kindLocal:
	case kindEncrypted:
		if encryptedRunJob(out) {
			return true
		}
	default:
		return false
	}
	c := cfg.load()
	return c == nil || strings.TrimSpace(c.Tenant) == ""
}

// encryptedRunJob reports that a backup job was set up on this computer for the encrypted
// archive at out — what Run now repeats (the P3-3 plan, 3d) — read from owner-side state
// only, never from the archive.
func encryptedRunJob(out string) bool {
	return schedule.BuildKindEvidence(out).Has(schedule.KindRunJob)
}

// encBackupHow is how new mail is added to the encrypted archive at out, in plain words:
// with Run now when a backup job was set up here for it, otherwise at a terminal with the
// exact command — an archive made at the command line is backed up there (P3-8, B26).
// What it was made from is sealed inside it, so both forms are named: from mail files,
// and from Microsoft 365 — a local-mail command alone cannot add a Microsoft 365
// archive's new mail (the P3-8 pass, DESK-2).
func encBackupHow(goos, out string, runNow bool) string {
	if runNow {
		return "To add new mail, unlock it on its Encryption card, then click Run now on the Scheduled backup card."
	}
	mail := shellJoin(AddMailArgv("<the mail folder or file it was made from>", out), goos)
	m365 := shellJoin(AddGraphArgv(out), goos)
	return "It was made at the command line, so it is backed up there, the same way it was made. To add new mail, type this in a terminal — if it was made from mail files: " + mail +
		"; if it was made from Microsoft 365: " + m365 + " followed by the same sign-in options it was made with (-tenant, -client-id and the rest). Either asks for the archive's password, then adds only what is new."
}

// archiveNowOffLine is the line shown in place of the Archive now button, "" when the button
// applies: a local-only archive's steps, or for any encrypted archive why Archive now cannot
// fill it from here and how it is backed up instead — Archive now never captures into an
// encrypted archive (app.RefuseKeyless), whatever it was made from (P3-8, B25).
func (cfg Config) archiveNowOffLine() string {
	out := cfg.effectiveOut()
	if archiveKind(out) == kindEncrypted {
		return "Archive now is off for this archive: it fetches mail from Microsoft 365 into an archive that is not encrypted, and this one is encrypted. " +
			encBackupHow(cfg.goos(), out, encryptedRunJob(out))
	}
	if cfg.localOnly() {
		return archiveNowOff(cfg.goos(), out)
	}
	return ""
}

// newMailLineFor is the Status & health line that says where new mail comes from, for a
// local-only archive; an encrypted one is told how it is backed up (P3-8, B25).
func (cfg Config) newMailLineFor(out string) string {
	if archiveKind(out) == kindEncrypted {
		return "New mail: MailArchive Desktop does not fetch Microsoft 365 mail into an encrypted archive. " + encBackupHow(cfg.goos(), out, encryptedRunJob(out))
	}
	return newMailLine(cfg.goos(), out)
}

// AddMailArgv is the command line that adds new mail from a Thunderbird (or any mbox / maildir)
// folder to the archive at out: an incremental run, which is the default mode, writes only what is
// not archived yet. The dashboard shows exactly this, and a test parses it with the command's own
// flag parser, so the text cannot drift from the flags.
func AddMailArgv(folder, out string) []string {
	return []string{"mailarchive", "-input", folder, "-out", out}
}

// AddGraphArgv is the start of the command line that adds new Microsoft 365 mail to the
// archive at out; the sign-in options it was made with follow it.
func AddGraphArgv(out string) []string {
	return []string{"mailarchive", "graph", "-out", out}
}

// AddMailAutoArgv is the same with every Thunderbird (and Evolution, and on Windows Outlook) store
// the tool finds on this computer.
func AddMailAutoArgv(out string) []string {
	return []string{"mailarchive", "-auto", "-out", out}
}

// thunderbirdFolderExample is where a Thunderbird local folders directory is, per OS (the same
// profile roots -auto looks in).
func thunderbirdFolderExample(goos string) string {
	switch goos {
	case "windows":
		return `%APPDATA%\Thunderbird\Profiles\<profile>\Mail\Local Folders`
	case "darwin":
		return "~/Library/Thunderbird/Profiles/<profile>/Mail/Local Folders"
	}
	return "~/.thunderbird/<profile>/Mail/Local Folders"
}

// shellJoin renders argv for a terminal of goos, quoting only what needs it.
func shellJoin(argv []string, goos string) string {
	parts := make([]string, len(argv))
	for i, a := range argv {
		parts[i] = shellQuote(a, goos)
	}
	return strings.Join(parts, " ")
}

func shellQuote(s, goos string) string {
	if s != "" && !strings.ContainsAny(s, " \t\"'$`\\&|;<>()*?[]{}!#") {
		return s
	}
	if goos == "windows" {
		return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// addMailHelp is the owner-facing steps for adding new mail to the local-origin archive at out
// (what, why, what to do; docs/ux-contract.md X-series). The GUI and the command line both read
// the same archive folder and add only what is new.
func addMailHelp(goos, out string) string {
	folder := thunderbirdFolderExample(goos)
	cmd := shellJoin(AddMailArgv("<your Thunderbird mail folder>", out), goos) // the folder is the owner's to fill in
	auto := shellJoin(AddMailAutoArgv(out), goos)
	return "To add new mail to it, use the MailArchive wizard (mailarchive wizard) or the mailarchive command with this same archive folder:\n" +
		"  - the MailArchive wizard (mailarchive wizard): open it, choose \"Thunderbird / mbox mail folder\" (or \"Auto-detect my mailboxes\"), pick your Thunderbird mail folder (for example " + folder + "), then at \"Select the output folder\" pick this archive folder: " + out + ". Choose Incremental when it asks for the mode: only new messages are added.\n" +
		"  - mailarchive (a terminal): " + cmd + "\n" +
		"    or, to take every Thunderbird store it finds: " + auto + "\n" +
		"    (an incremental run is the default; a message already archived is not copied again)."
}

// archiveNowOffLead is the Archive now card's first line on a local-origin archive (MA-941):
// what Archive now is for, in one sentence. The card then says where to click instead.
const archiveNowOffLead = "Archive now is off for this archive. It is for Microsoft 365 archives, and this one was made from mail files on this computer."

// archiveNowOff is a direct Archive now request's refusal on a local-origin archive: the
// card's first line, where to click instead, and the hand steps (MA-546, MA-941).
func archiveNowOff(goos, out string) string {
	return archiveNowOffLead + " To back it up now, click Run now on the Scheduled backup card; if it has no scheduled backup yet, choose How often in Settings and click Save settings first, and Run now then appears there. You can also add new mail by hand. " + addMailHelp(goos, out)
}

// What the Archive now card of a local-origin archive points at instead (MA-941): the
// way this page offers to back it up now.
const (
	nowWayRunNow = "runnow" // Run now is on the Scheduled backup card
	nowWayCreate = "create" // no schedule yet: How often in Settings, then Save settings
	nowWayExport = "export" // a command-line schedule: the Scheduled backup card says why and how
	nowWayHand   = "hand"   // no way on this page: the wizard or a terminal
)

// newMailLine is the line of the Status & health view that says where new mail comes from.
func newMailLine(goos, out string) string {
	return "New mail: this archive was made from local mail files, so it does not update itself from Microsoft 365. " + addMailHelp(goos, out)
}

// OfferedArchive is the archive the GUI used last (the file its wizard writes), when it is a folder
// that exists and holds an archive: what the dashboard offers when no archive location is set.
// Read-only; nil-safe on every error. Config.LastArchive carries it; the binary supplies it.
func OfferedArchive() string {
	p, err := job.LastArchiveFile()
	if err != nil {
		return ""
	}
	return validArchiveFolder(job.ReadLastArchive(p))
}

// validArchiveFolder is dir when it is an existing folder that holds a MailArchive manifest, else "".
func validArchiveFolder(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return ""
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return ""
	}
	if fi, err := os.Stat(filepath.Join(dir, manifestName)); err != nil || fi.IsDir() {
		return ""
	}
	return dir
}

// offer is the archive to offer on the empty state, "" when an archive is already set or the GUI
// remembered none.
func (cfg Config) offer() string {
	if cfg.effectiveOut() != "" || cfg.LastArchive == nil {
		return ""
	}
	return validArchiveFolder(cfg.LastArchive())
}

// localArchiveThunderbirdSourced reports whether the archive at out was made from a
// Thunderbird store — read from the MANIFEST's local store ids ("path:<dir>"), NOT
// from LastRun.Job (which records no -input for an -auto run, so keying on it would
// hide the toggle for the common auto-discovered case — design TP-D correction 4). A
// store path under a Thunderbird profile (thunderbird.FindProfileDir ascends to a
// prefs.js) means the pre-sync toggle is worth offering for this archive. A manifest
// that cannot be read, or a store whose profile is not present on this machine, is
// simply not Thunderbird-sourced here (the toggle stays hidden — safe default).
func localArchiveThunderbirdSourced(out string) bool {
	if strings.TrimSpace(out) == "" {
		return false
	}
	m, err := state.Load(filepath.Join(out, manifestName))
	if err != nil {
		return false
	}
	for id := range m.Stores {
		if !strings.HasPrefix(id, "path:") {
			continue // a Microsoft 365 "mailbox:" id, never Thunderbird
		}
		p := strings.TrimPrefix(id, "path:")
		if isRemotePath(p) {
			continue // a network or non-local path in the record is never looked at (C-17, MA-869)
		}
		if _, ok := findProfileDir(p); ok {
			return true
		}
	}
	return false
}

// findProfileDir is thunderbird.FindProfileDir, which looks at the source's folders; a seam, so a
// test can see which paths are looked at.
var findProfileDir = thunderbird.FindProfileDir
