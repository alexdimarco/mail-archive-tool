// Package encmark is the one answer to "is this folder an encrypted archive?" that
// every MailArchive writer asks before it writes (design-encrypted-archives-phase3
// §4.5, A15, A34). It answers from the folder's own markers (vault.EncryptedMarker,
// for the folder and every folder above it) OR from this computer's record of the
// encrypted archives it made (internal/encrecord). A folder this computer recorded
// stays encrypted whatever it now holds: an archive stripped of its key files and
// sealed data, and replanted with plaintext, is refused, not written into.
//
// For a recorded folder it also says which of six cases the folder is in, each
// with its own wording, decided in this order: L (the folder is now a link), 0 (an
// unproven path rebuilt by repair), 2 (files that are neither an encrypted
// archive's own nor operating-system litter), 1 (key files naming another archive),
// 3 (key files missing or damaged beside sealed data), 4 (nothing but reserved names
// and litter, or the folder gone).
package encmark

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/graph"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/instance"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/presync"
	"mail-archive-tool/internal/util"
	"mail-archive-tool/internal/vault"
)

// Case is how a recorded folder stands (§4.5).
type Case int

const (
	// None: not recorded, or recorded and holding its own archive.
	None Case = iota
	// Link: the recorded folder is now a link or junction.
	Link
	// Unproven: the path was rebuilt by repair and is not yet proven.
	Unproven
	// Other: it holds files that are neither reserved names nor litter.
	Other
	// Foreign: its key files name another archive.
	Foreign
	// KeysMissing: its key files are missing or damaged beside sealed data.
	KeysMissing
	// Gone: nothing but reserved names and litter, or the folder is gone.
	Gone
)

// Result is the answer for a folder.
type Result struct {
	// Encrypted: no keyless writer may write here.
	Encrypted bool
	// Folder is the folder judged: the path itself or a folder above it.
	Folder string
	// Why says why, in words for a refusal.
	Why string
	// Recorded: this computer's record names Folder; ID is the archive's id.
	Recorded bool
	ID       string
	Case     Case
	// RecordErr is set when this computer's record could not be used.
	RecordErr error
	// ByRecordErr: Encrypted only because the record cannot be used and the folder
	// holds MailArchive files — a refusal, not a sign the folder is encrypted (the
	// P3-0 adversarial pass, CON-01). A writer refuses with ErrRecordUnusable; the
	// keyless check never runs on such a folder; labels do not change.
	ByRecordErr bool
}

// ErrRecordUnusable refuses a write into a folder holding MailArchive files while
// this computer's record of encrypted archives cannot be used: the folder may be a
// recorded archive the record can no longer show (§4.3).
var ErrRecordUnusable = errors.New("this computer's record of encrypted archives cannot be used")

// reserved are the names an encrypted archive itself writes at its root (the
// tree grammar, MA-743; a test pins the two together), plus the conversion marker
// and the program copies (phase3 §11, §13).
var reserved = map[string]bool{
	".mailarchive-vault.json": true, ".mailarchive-vault.bak.json": true,
	".mailarchive-manifest.json": true, ".mailarchive-manifest.ptr": true, ".mailarchive-manifest.ptr.bak": true,
	".mailarchive-inventory": true, "inventory": true, "units": true, "chunks": true,
	".mailarchive.lock": true, ".mailarchive-lastrun.json": true, ".mailarchive-runhistory.json": true,
	".mailarchive-sourcestreak.json": true, ".mailarchive-lastverify.json": true, ".mailarchive-paused.json": true,
	"BACKUP-NEEDS-ATTENTION.txt": true, "ARCHIVE-INTEGRITY-ATTENTION.txt": true, "README.txt": true,
	".mailarchive-convert.json": true, "program": true,
}

// sealedData are the reserved names that hold the archive's sealed content: one
// of them beside missing key files is case 3, not case 4.
var sealedData = map[string]bool{
	".mailarchive-manifest.ptr": true, ".mailarchive-manifest.ptr.bak": true, ".mailarchive-inventory": true,
	"inventory": true, "units": true, "chunks": true,
}

var tempRe = regexp.MustCompile(`^\.mailarchive-[a-z-]*[0-9]+\.tmp$`)

// litter are the names operating systems drop into folders and drive roots, per OS
// (§4.5, extended by the P3-0 adversarial pass, CRIT-04); every case ignores them.
// Matched case-insensitively.
var litter = map[string]bool{
	// macOS
	".ds_store": true, ".spotlight-v100": true, ".fseventsd": true, ".trashes": true, ".temporaryitems": true,
	".documentrevisions-v100": true, ".volumeicon.icns": true, ".apdisk": true, ".localized": true, "icon\r": true,
	".com.apple.timemachine.donotpresent": true,
	// Windows
	"thumbs.db": true, "desktop.ini": true, "$recycle.bin": true, "system volume information": true,
	// Linux
	"lost+found": true,
}

// litterPrefixes are litter names that carry a suffix: AppleDouble "._*", the Linux
// desktop trash ".Trash-<uid>", macOS installer sandboxes; and Windows chkdsk's
// "found.NNN".
var (
	litterPrefixes = []string{"._", ".trash-", ".pkinstallsandboxmanager"}
	foundRe        = regexp.MustCompile(`^found\.[0-9]{3}$`)
)

// IsLitter reports an operating system's own file: a folder holding only these is
// empty to a person.
func IsLitter(name string) bool { return isLitter(name) }

// isLitter reports an operating system's own file.
func isLitter(name string) bool {
	low := strings.ToLower(name)
	if litter[low] || foundRe.MatchString(low) {
		return true
	}
	for _, p := range litterPrefixes {
		if strings.HasPrefix(low, p) {
			return true
		}
	}
	return false
}

// isReserved reports a name an encrypted archive itself writes at its root.
func isReserved(name string) bool { return reserved[name] || tempRe.MatchString(name) }

// load is encrecord.Load, a var so a test can make the record unusable.
var load = encrecord.Load

// At answers for path: the folder a writer would write into.
func At(path string) (Result, error) {
	dir, why, merr := vault.EncryptedAtOrAbove(path)
	rec, rerr := load()
	if rerr != nil {
		res := Result{Encrypted: dir != "", Folder: dir, Why: why, RecordErr: rerr}
		// While the record cannot be used, a folder holding MailArchive's own files
		// is refused: it may be a recorded archive the record no longer shows (§4.3).
		if !res.Encrypted && holdsArchiveFiles(path) {
			res.Encrypted, res.Folder, res.Why, res.ByRecordErr = true, path, rerr.Error(), true
		}
		if res.Encrypted {
			return res, nil
		}
		return res, merr
	}
	if m, ok := rec.Lookup(path); ok {
		c := classify(m)
		res := Result{Encrypted: true, Folder: m.Folder, Recorded: true, ID: m.ID, Case: c, Why: caseWording(c, m)}
		if c == None {
			res.Why = why
			if res.Why == "" {
				res.Why = "it is an encrypted archive this computer made"
			}
		}
		return res, nil
	}
	if dir != "" {
		return Result{Encrypted: true, Folder: dir, Why: why}, nil
	}
	return Result{}, merr
}

// holdsArchiveFiles reports whether the folder path holds a name only MailArchive
// writes (an archive's manifest, record files, lock, run records, temps).
func holdsArchiveFiles(path string) bool {
	entries, _ := os.ReadDir(path)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".mailarchive") {
			return true
		}
	}
	return false
}

// judged is the folder a match is judged at: the folder as reached when it was
// reached by its recorded spelling, otherwise the recorded spelling itself.
func judged(m encrecord.Match) string {
	if m.BySpelling {
		return m.Folder
	}
	return m.Path.Spelling
}

// caseWording is a recorded folder's refusal in its case's words. An unproven
// path whose key files are gone cannot be proven by opening it, so it is told how
// to forget it instead (the P3-1a pass, CRIT-02).
func caseWording(c Case, m encrecord.Match) string {
	if c == Unproven && !vault.Claims(judged(m)).Present {
		return fmt.Sprintf("this computer once recorded an encrypted archive at %s, and its record was repaired, but the archive's key files are not there; %s", m.Folder, forgetOffer("you removed it on purpose", m.Folder))
	}
	return Wording(c, m.Folder)
}

// classify decides a recorded folder's case, in the order of §4.5. The folder
// judged is the recorded one: the folder as reached when it was reached by its
// recorded spelling, otherwise the recorded spelling itself — so an archive
// reached through the link it was created with is judged where it is, not as "now
// a link" (the P3-0 adversarial pass, FID-01).
func classify(m encrecord.Match) Case {
	target := judged(m)
	// The folder, or one above it, is now a link: it was recorded by its resolved
	// location, so a recorded spelling resolving elsewhere had a link put on its way
	// (OUT-05).
	if link, _ := util.IsLinkLike(target); link || encrecord.ResolvesElsewhere(m.Path.Spelling) {
		return Link
	}
	if m.Path.Unproven {
		return Unproven
	}
	entries, err := os.ReadDir(target)
	if errors.Is(err, os.ErrNotExist) {
		return Gone
	}
	data := false
	for _, e := range entries {
		n := e.Name()
		switch {
		case isLitter(n):
		case n == ".mailarchive-manifest.json" && vault.ManifestIsPlaintext(filepath.Join(target, n)):
			// A plaintext MailArchive manifest is another archive's files under a
			// reserved name (OUT-06).
			return Other
		case isReserved(n):
			data = data || sealedData[n]
		default:
			return Other
		}
	}
	claims := vault.Claims(target)
	for _, id := range claims.IDs {
		if id.String() != m.ID {
			return Foreign
		}
	}
	// Key files of this archive from a newer MailArchive are newer, never damaged:
	// a keyed open asks for the upgrade (A7; FID-02).
	if claims.Parses || claims.Newer {
		return None
	}
	if data || claims.Present {
		return KeysMissing
	}
	return Gone
}

// forgetOffer is how a refusal points at forgetting the folder (§4.6): the verb
// P3-1a ships, which asks its own confirmations.
func forgetOffer(why, folder string) string {
	return fmt.Sprintf("if %s, run `mailarchive forget-archive -out %q`", why, folder)
}

// Wording is a case's refusal for folder (§4.5).
func Wording(c Case, folder string) string {
	switch c {
	case Link:
		return fmt.Sprintf("%s was an encrypted archive this computer made, and is now a link to somewhere else — it may have been tampered with; %s", folder, forgetOffer("you made this change on purpose", folder))
	case Unproven:
		return fmt.Sprintf("this computer once recorded an encrypted archive at %s, and its record was repaired; open it with its password to confirm", folder)
	case Other:
		return fmt.Sprintf("%s was an encrypted archive this computer made, and now holds other files — it may have been tampered with, or another drive may now use this path; restore the whole archive folder from a backup copy, or, %s", folder, forgetOffer("you replaced it on purpose", folder))
	case Foreign:
		return fmt.Sprintf("%s holds files naming a different encrypted archive than the one this computer recorded there — it may have been tampered with; %s", folder, forgetOffer("you put that archive there on purpose", folder))
	case KeysMissing:
		return fmt.Sprintf("%s holds an encrypted archive this computer made, but its key files are missing or damaged — restore the whole archive folder from a backup copy, or put the key files back with its recovery file and recovery key: `mailarchive restore-key-file -out %q -from FILE` (or `-remembered`, when this computer remembers its key)", folder, folder)
	case Gone:
		return fmt.Sprintf("%s used to hold an encrypted archive this computer made; %s", folder, forgetOffer("you removed it on purpose", folder))
	}
	return ""
}

// ErrSettingsInEncrypted refuses a settings, credential or record write whose file
// would sit inside an encrypted archive: it would hold plaintext — source paths,
// mailbox addresses, secrets — inside the archive (P2a-C14, extended in phase3 §6 to
// every settings write, plaintext users' included).
var ErrSettingsInEncrypted = errors.New("MailArchive's settings cannot be kept inside an encrypted archive")

// RefuseSettingsWrite refuses writing the settings file path (kind names it, such
// as "Desktop settings") when its folder is, or is inside, an encrypted archive.
func RefuseSettingsWrite(path, kind string) error {
	if path == "" {
		return nil
	}
	dir, why, _ := AtOrAbove(filepath.Dir(path))
	if dir != "" {
		return fmt.Errorf("%w: %s (%s) would be inside the encrypted archive %s (%s) — keep MailArchive's settings folder outside every archive",
			ErrSettingsInEncrypted, kind, path, dir, why)
	}
	return nil
}

// init installs the settings guard in graphconfig and graph, which cannot import
// this package (the vault's tests import graphconfig, which imports graph).
func init() {
	graphconfig.RefuseSettingsWrite = RefuseSettingsWrite
	graph.RefuseSettingsWrite = RefuseSettingsWrite
	job.RefuseSettingsWrite = RefuseSettingsWrite
	instance.RefuseSettingsWrite = RefuseSettingsWrite
	presync.RefuseSettingsWrite = RefuseSettingsWrite
	encrecord.RefuseSettingsWrite = RefuseSettingsWrite
}

// AtOrAbove is At in the shape of vault.EncryptedAtOrAbove — the folder judged
// encrypted ("" when none) and why — for callers that only refuse.
func AtOrAbove(path string) (dir, why string, err error) {
	r, err := At(path)
	if r.Encrypted {
		return r.Folder, r.Why, nil
	}
	return "", "", err
}

// Here answers for the folder out itself only, in the shape of
// vault.EncryptedMarker: whether out is an encrypted archive — by its own markers
// or by this computer's record of exactly out — and why. It is a LABEL (status,
// Desktop, the archive list): while the record cannot be used it answers from the
// markers alone, so a plaintext archive is never called encrypted for it (the
// P3-0 adversarial pass, CON-01, REG-02). What must be minimised asks MustMinimise.
func Here(out string) (bool, string, error) {
	enc, why, merr := vault.EncryptedMarker(out)
	if enc {
		return true, why, nil
	}
	rec, rerr := load()
	if rerr != nil {
		return false, "", merr
	}
	if m, ok := rec.Lookup(out); ok && encrecord.Spelling(m.Folder) == encrecord.Spelling(out) {
		c := classify(m)
		if c == None {
			return true, "it is an encrypted archive this computer made", nil
		}
		return true, caseWording(c, m), nil
	}
	return false, "", merr
}

// LostHere reports whether out's key files claim an archive a keyed open found
// missing from this computer's record while its key is remembered here (a lost
// record, §4.3): status warns until a backup or a remember records it again.
func LostHere(out string) bool { return len(LostIDs(out)) > 0 }

// LostIDs are the ids LostHere warns of at out: those its key files claim that the
// lost list holds and the record does not, so acknowledging the warning there clears
// only that folder's ids (§4.3; the P3-3 plan, 3b).
func LostIDs(out string) []string {
	lost, err := encrecord.Lost()
	if err != nil || len(lost) == 0 {
		return nil
	}
	rec, rerr := load()
	var ids []string
	for _, id := range vault.Claims(out).IDs {
		// A stale mark for an id the record now lists is no warning (CON-06).
		if rerr == nil && rec.Archives[id.String()] != nil {
			continue
		}
		if slices.Contains(lost, id.String()) {
			ids = append(ids, id.String())
		}
	}
	return ids
}

// NotSetUp says what is unfinished about an encrypted archive at out that has a key
// file and no backup yet (§5.1 step 7; the P3-1a pass, CRIT-05), or "" when it has
// a backup or is no such archive: a folder holding only a copy of the key file of
// an archive this computer recorded elsewhere, or an archive created but never
// backed up.
func NotSetUp(out string) string {
	c := vault.Claims(out)
	if !c.Parses {
		return ""
	}
	for _, name := range []string{".mailarchive-manifest.ptr", ".mailarchive-manifest.ptr.bak"} {
		if _, err := os.Lstat(filepath.Join(out, name)); err == nil {
			return ""
		}
	}
	if rec, err := load(); err == nil {
		here, ok := rec.Lookup(out)
		for _, id := range c.IDs {
			e := rec.Archives[id.String()]
			if e == nil || len(e.Paths) == 0 {
				continue
			}
			if !ok || here.ID != id.String() {
				return "this folder holds only a copy of the archive's key file; the archive itself is in a folder this computer recorded elsewhere"
			}
		}
	}
	return "this encrypted archive was created but has no backup yet — finish setting up: run a backup into it"
}

// MustMinimise reports whether a record written beside out must take an encrypted
// archive's minimised shape: out is an encrypted archive (Here), or whether it is
// cannot be told — its markers cannot be read, or this computer's record cannot be
// used while out holds MailArchive files. It fails closed, unlike Here: a refused
// run's record then never carries source paths or error text into a folder that
// may be an encrypted archive (FC-4; the P3-0 adversarial pass, CRIT-03).
func MustMinimise(out string) bool {
	if enc, _, err := Here(out); enc || err != nil {
		return true
	}
	if _, rerr := load(); rerr != nil && holdsArchiveFiles(out) {
		return true
	}
	return false
}
