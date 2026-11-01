// Package archdelete is the read-only half of deleting an archive
// (docs/design-archive-delete.md, slice 1a): the plan of §3.1. Plan looks at a
// folder, refuses what §7 can decide from the folder and from facts its caller
// injects, and lists, by name, exactly the files Delete would remove (§4), the
// mail copies the archive did not record (§4.3), the operating-system litter it
// would clear (§4.4) and what it would keep. It removes nothing and writes
// nothing: the ordered steps of §5.3 are a later slice and act on a plan only
// after re-checking it under the archive lock.
//
// The folder is untrusted input. Its manifest is read through an os.Root with a
// cap sized for real archives (never archindex's 1 MiB, [C10]); a recorded Path
// counts only in the stem-only shape of §4.1 [C11]; and every element of a name
// Delete would remove or walk is checked with Lstat and, on Windows, its reparse
// tag (util.IsLinkLike), so a link, junction or mount point refuses the whole
// delete [C11, NEW-reparse-mechanism].
package archdelete

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"mail-archive-tool/internal/util"
)

// Kind is what a folder holds.
type Kind int

const (
	// Plain is an unencrypted archive: a manifest beside the mail files.
	Plain Kind = iota + 1
	// Encrypted is an encrypted archive: a vault and sealed data.
	Encrypted
)

func (k Kind) String() string {
	switch k {
	case Plain:
		return "plain"
	case Encrypted:
		return "encrypted"
	}
	return "unknown"
}

// ItemKind says why Delete removes a file (§4).
type ItemKind int

const (
	// Message is a recorded stem's .html, -attachments.zip or .eml (§4.1 Records).
	Message ItemKind = iota + 1
	// IndexPage is index.html / index-N.html in a folder a record passes through, or
	// the root index.html (§4.1 Index pages).
	IndexPage
	// Leftover is a crashed run's .mailarchive-*.tmp or .mailarchive-snapshot-* (§4.1).
	Leftover
	// Fixed is one of the fixed root names, a discovered schedule's log, or an
	// _outlook-pst .pst file (§4.1).
	Fixed
	// State is a .mailarchive-* state file or descriptor at the root (§4.1), or one of
	// an encrypted archive's reserved names (§4.2).
	State
	// Sealed is a file of an encrypted archive's validated layouts: units/, chunks/,
	// inventory/, program/ (§4.2).
	Sealed
	// Anchor is the manifest (plain) or a vault file (encrypted), removed last.
	Anchor
)

// Item is one file Delete removes, relative to the archive folder with forward
// slashes.
type Item struct {
	Rel  string
	Size int64
	Kind ItemKind
}

// Identity is a folder's file identity: its volume and its file id (on Unix the
// device and inode).
type Identity struct {
	Volume uint64
	FileID uint64
}

// String is the identity as the plan and the record word it, identically (§8.1).
func (id Identity) String() string {
	return fmt.Sprintf("volume %d, file id %d", id.Volume, id.FileID)
}

// FileFacts are the manifest's or the vault's size, modification time and SHA-256,
// as read at the plan (§3.1).
type FileFacts struct {
	Name    string
	Size    int64
	ModTime time.Time
	SHA256  string
}

// Captured are the values the deletion record keeps, read at the plan while the
// archive is still there (§3.1, §8.1 [R3:NEW-FINDING-record-capture-time]). A zero
// time or a false Known means "not known".
type Captured struct {
	// Known is false for an encrypted archive whose key was not open at the plan:
	// its message count, date range and first run are sealed (§8.1 [O2-05]).
	Known    bool
	Messages int
	// MailFrom and MailTo are the date range of the mail (the records' dates).
	MailFrom, MailTo time.Time
	// FirstRun is the first run header in .mailarchive-history.jsonl; zero when the
	// history is missing or unreadable.
	FirstRun time.Time
	// LastBackup is the run history's newest entry whose status is ok.
	LastBackup time.Time
	// KeyOpen says an encrypted archive's key was open here at the plan.
	KeyOpen bool
}

// RecordRef is this computer's record of an encrypted archive, found while the
// folder exists (§3.1 [NF-2]).
type RecordRef struct {
	Recorded bool
	ID       string
	Spelling string
}

// DeletePlan is what Plan found: the facts §3.1 records and the file list of §4.
// Delete acts only on what is named here.
type DeletePlan struct {
	// Folder is the archive folder, absolute and cleaned; Name its last element (what
	// the person types to confirm, §3.1).
	Folder string
	Name   string
	// Identity is the folder's identity at the plan; Delete's os.Root must be the
	// same folder [C15].
	Identity Identity
	Kind     Kind
	// Anchor is the manifest (plain) or the vault file (encrypted).
	Anchor FileFacts
	// Remove lists every file Delete removes, the anchor last (§5.3's order: a delete
	// stopped part way is still recognised as an archive).
	Remove []Item
	// Litter are operating-system files (util.IsLitter) in folders a removed name
	// passes through (§4.4); UnrecordedLitter those in folders only a ticked §4.3 file
	// passes through.
	Litter           []string
	UnrecordedLitter []string
	// Unrecorded are §4.3's mail copies in a store tree no record names: kept unless
	// the person ticks the box [O1].
	Unrecorded []Item
	// Kept are the files and folders Delete never touches ("kept: not the
	// archive's", §8 (c)): inside a store tree each file is named; outside the store
	// trees each top-level entry is named once, never walked.
	Kept []string
	// Folders are the folders Delete may remove once empty but for litter, deepest
	// first (§4 "Folders are removed with os.Remove, deepest first").
	Folders []string
	// Stores is the store token set of §4.1, in byte order: store N is Stores[N-1]
	// (§8.1 [R4:NF-R4-STORE-NUMBER-ORDER]).
	Stores []string
	// Schedules are the scheduler entries discovery found (§5.1).
	Schedules Discovery
	// Record is this computer's record of an encrypted archive.
	Record RecordRef
	// Captured are the record's values read at the plan.
	Captured Captured
	// Paused: the folder holds the pause seal; the plan says "This archive is
	// paused." [C13].
	Paused bool
	// Synced names the sync service the folder is under, or "" (§9).
	Synced string
	// Files and Bytes count Remove and Litter.
	Files int
	Bytes int64
}

// StoreNumber is token's store number (1-based) in the byte order of the store
// token set, or 0 when token is not a store token.
func (p *DeletePlan) StoreNumber(token string) int { return storeNumber(p.Stores, token) }

// Case names a refusal of §7.
type Case string

const (
	CaseUnreachable    Case = "unreachable"
	CaseNotArchive     Case = "not-an-archive"
	CaseProtected      Case = "protected"
	CaseLink           Case = "link"
	CaseMixed          Case = "mixed"
	CaseBusy           Case = "busy"
	CaseDescriptor     Case = "descriptor"
	CaseBackupFor      Case = "backupfor"
	CaseLaunchOut      Case = "launch-out"
	CaseCurrentArchive Case = "current-archive"
)

// Refusal is a §7 refusal: the cause, and a remedy that can be done from the row.
// Nothing was changed.
type Refusal struct {
	Case    Case
	Message string
}

func (r *Refusal) Error() string { return r.Message }

// ExitCode is the command line's exit code for a refusal (§3.3: 1 refused).
func (r *Refusal) ExitCode() int { return 1 }

// AsRefusal returns err as a *Refusal, or nil.
func AsRefusal(err error) *Refusal {
	var r *Refusal
	if errors.As(err, &r) {
		return r
	}
	return nil
}

func refuse(c Case, format string, args ...any) error {
	return &Refusal{Case: c, Message: fmt.Sprintf(format, args...)}
}

// The §7 wordings that are fixed text.
const (
	msgUnreachable = "Reconnect the drive or folder first: nothing can be checked."
	msgNotArchive  = "This folder no longer holds an archive. Use Remove from list."
	msgMixed       = "This folder holds files of more than one kind. Delete it by hand."
	msgBusy        = "A backup of this archive is running. Try again when it ends."
	msgLaunchOut   = "Restart MailArchive without -out, then delete."
)

// Settings are the Desktop settings a refusal reads, injected by the caller.
type Settings struct {
	// Current is the current archive (desktop-settings.json's Out).
	Current string
	// CaptureBackupFor is captureBackupFor()'s folder: the folder whose name the
	// current archive's Microsoft 365 capture is registered under ("" when unset; a
	// BackupFor that names a run-job counts as unset, desktop/schedule.go:85-91) [C4].
	CaptureBackupFor string
	// LaunchOut is the folder the Desktop was started with -out, or "" [C16].
	LaunchOut string
}

// Deps are the seams Plan reads through. A nil func is "nothing to report".
type Deps struct {
	// Discover lists the folder's scheduler entries (§5.1); FileDiscovery is the
	// part that only reads files. Nil finds none.
	Discover Discoverer
	// Busy reports a backup, Run now, verify, pre-sync session, Desktop capture or
	// workspace job running on the folder [C12]. Every real caller sets it.
	Busy func(folder string) bool
	// Record looks up this computer's record of an encrypted archive at folder
	// (RecordLookup is the real one). Nil records nothing.
	Record func(folder string) (RecordRef, error)
	// Sealed returns an encrypted archive's sealed values when its key is open here
	// (the Desktop's open archive); ok false when it is not.
	Sealed func(folder string) (c Captured, ok bool)
	// Settings are the Desktop's settings §7 and §5.1 read.
	Settings Settings
	// Protected are the folders §7's protected row names [C15].
	Protected Protected
	// GOOS selects the platform's name rules (case folding); "" is runtime.GOOS.
	GOOS string
}

func (d Deps) goos() string {
	if d.GOOS != "" {
		return d.GOOS
	}
	return runtime.GOOS
}

// Plan builds the plan of §3.1 for folder, or the §7 refusal that applies. It
// reads only: nothing in the folder or anywhere else is changed.
func Plan(folder string, deps Deps) (*DeletePlan, error) {
	abs, err := filepath.Abs(folder)
	if err != nil {
		abs = filepath.Clean(folder)
	}
	abs = filepath.Clean(abs)
	fi, err := os.Lstat(abs)
	if err != nil {
		return nil, refuse(CaseUnreachable, msgUnreachable)
	}
	if link, lerr := util.IsLinkLike(abs); lerr != nil {
		return nil, refuse(CaseUnreachable, msgUnreachable)
	} else if link {
		return nil, linkRefusal(abs)
	}
	if !fi.IsDir() {
		return nil, refuse(CaseNotArchive, msgNotArchive)
	}
	if err := checkProtected(abs, deps.Protected); err != nil {
		return nil, err
	}
	if deps.Settings.LaunchOut != "" && sameFolder(deps.Settings.LaunchOut, abs, deps.goos()) {
		return nil, refuse(CaseLaunchOut, msgLaunchOut)
	}
	if deps.Busy != nil && deps.Busy(abs) {
		return nil, refuse(CaseBusy, msgBusy)
	}
	root, err := os.OpenRoot(abs)
	if err != nil {
		return nil, refuse(CaseUnreachable, msgUnreachable)
	}
	defer root.Close()
	id, err := rootIdentity(root)
	if err != nil {
		return nil, refuse(CaseUnreachable, msgUnreachable)
	}
	p := &DeletePlan{Folder: abs, Name: filepath.Base(abs), Identity: id}
	if svc, ok := util.UnderCloudSync(abs); ok {
		p.Synced = svc
	}
	kind, err := classify(root, abs)
	if err != nil {
		return nil, err
	}
	p.Kind = kind
	if err := checkBackupFor(abs, deps); err != nil {
		return nil, err
	}
	if deps.Discover != nil {
		disc, derr := deps.Discover.Discover(abs)
		if derr != nil {
			return nil, derr
		}
		p.Schedules = disc
	}
	l := lister{root: root, folder: abs, goos: deps.goos(), plan: p}
	switch kind {
	case Plain:
		err = l.plain()
	case Encrypted:
		err = l.encrypted(deps)
	}
	if err != nil {
		return nil, err
	}
	l.finish()
	return p, nil
}

// checkBackupFor is §5.1 (a): the current archive's capture is registered under
// this folder's name, and this folder is not the current archive [C4].
func checkBackupFor(abs string, deps Deps) error {
	s := deps.Settings
	if s.CaptureBackupFor == "" || !sameFolder(s.CaptureBackupFor, abs, deps.goos()) {
		return nil
	}
	if s.Current != "" && sameFolder(s.Current, abs, deps.goos()) {
		return nil
	}
	return refuse(CaseBackupFor, "%s In Settings, remove the scheduled backup and set it up again (it is then registered under %s), then delete this one.", backupForReason(s.Current), currentName(s.Current))
}

func backupForReason(current string) string {
	return fmt.Sprintf("The scheduled backup of your current archive %s is still registered under this folder's name.", currentName(current))
}

func currentName(current string) string {
	if current == "" {
		return "(none chosen)"
	}
	return current
}

// UnlistRefusal is Remove from list's refusals that facts decide (§7's Remove
// column): the capture registered under this folder's name (§5.1 (b)), the archive
// in use while reachable and an archive (by §4's archive test), and a Desktop
// started with -out naming it. nil means Remove from list may go on.
func UnlistRefusal(folder string, deps Deps) error {
	abs, err := filepath.Abs(folder)
	if err != nil {
		abs = filepath.Clean(folder)
	}
	s := deps.Settings
	goos := deps.goos()
	if s.LaunchOut != "" && sameFolder(s.LaunchOut, abs, goos) {
		return refuse(CaseLaunchOut, "Restart MailArchive without -out, then remove it from the list.")
	}
	current := s.Current != "" && sameFolder(s.Current, abs, goos)
	if s.CaptureBackupFor != "" && sameFolder(s.CaptureBackupFor, abs, goos) && !current {
		return refuse(CaseBackupFor, "%s In Settings, remove the scheduled backup and set it up again (it is then registered under %s), then remove this one from the list.", backupForReason(s.Current), currentName(s.Current))
	}
	if current && IsArchive(abs) {
		return refuse(CaseCurrentArchive, "This is the archive in use. Switch to another archive, or add one (Add another archive), then remove this one from the list.")
	}
	return nil
}

// IsArchive is §4's archive test [NEW-isarchive-cap]: the folder holds a manifest
// §4.1's reader parses (no 1 MiB cap), or an encrypted archive's marker — never
// archindex.IsArchive or the row's Available flag.
func IsArchive(folder string) bool {
	root, err := os.OpenRoot(folder)
	if err != nil {
		return false
	}
	defer root.Close()
	k, err := classify(root, folder)
	return err == nil && k != 0
}

func linkRefusal(link string) error {
	return refuse(CaseLink, "This archive's folder is, or holds where its files are, a link: %s. Delete it by hand.", link)
}

// sameFolder is "the same folder" as §3.1 matches it: by file identity when both
// can be read, else by spelling (case folded where the file system ignores case).
func sameFolder(a, b, goos string) bool {
	if a == "" || b == "" {
		return false
	}
	fa, ea := os.Stat(a)
	fb, eb := os.Stat(b)
	if ea == nil && eb == nil {
		return os.SameFile(fa, fb)
	}
	ca, cb := cleanAbs(a), cleanAbs(b)
	if goos == "windows" || goos == "darwin" {
		return strings.EqualFold(ca, cb)
	}
	return ca == cb
}

func cleanAbs(p string) string {
	if a, err := filepath.Abs(p); err == nil {
		return filepath.Clean(a)
	}
	return filepath.Clean(p)
}
