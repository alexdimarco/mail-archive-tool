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
	"strings"

	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/graph"
	"mail-archive-tool/internal/graphconfig"
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
}

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

// litter are the names operating systems drop into folders, per OS (§4.5); every
// case ignores them. Matched case-insensitively.
var litter = map[string]bool{
	".ds_store": true, ".spotlight-v100": true, ".fseventsd": true, ".trashes": true, ".temporaryitems": true,
	"thumbs.db": true, "desktop.ini": true, "$recycle.bin": true, "system volume information": true,
	"lost+found": true,
}

// isLitter reports an operating system's own file, AppleDouble "._*" files included.
func isLitter(name string) bool {
	return litter[strings.ToLower(name)] || strings.HasPrefix(name, "._")
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
			res.Encrypted, res.Folder, res.Why = true, path, rerr.Error()
		}
		if res.Encrypted {
			return res, nil
		}
		return res, merr
	}
	if m, ok := rec.Lookup(path); ok {
		c := classify(m)
		res := Result{Encrypted: true, Folder: m.Folder, Recorded: true, ID: m.ID, Case: c, Why: Wording(c, m.Folder)}
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

// classify decides a recorded folder's case, in the order of §4.5.
func classify(m encrecord.Match) Case {
	if link, _ := util.IsLinkLike(m.Folder); link {
		return Link
	}
	if m.Path.Unproven {
		return Unproven
	}
	entries, err := os.ReadDir(m.Folder)
	if errors.Is(err, os.ErrNotExist) {
		return Gone
	}
	data := false
	for _, e := range entries {
		n := e.Name()
		switch {
		case isLitter(n):
		case isReserved(n):
			data = data || sealedData[n]
		default:
			return Other
		}
	}
	claims := vault.Claims(m.Folder)
	for _, id := range claims.IDs {
		if id.String() != m.ID {
			return Foreign
		}
	}
	if claims.Parses {
		return None
	}
	if data || claims.Present {
		return KeysMissing
	}
	return Gone
}

// forgetOffer is how a refusal points at forgetting (P3-1a ships the verb; until
// then it says MailArchive cannot write here).
const forgetOffer = "MailArchive cannot write here"

// Wording is a case's refusal for folder (§4.5).
func Wording(c Case, folder string) string {
	switch c {
	case Link:
		return fmt.Sprintf("%s was an encrypted archive this computer made, and is now a link to somewhere else — it may have been tampered with; %s", folder, forgetOffer)
	case Unproven:
		return fmt.Sprintf("this computer once recorded an encrypted archive at %s, and its record was repaired; open it with its password to confirm", folder)
	case Other:
		return fmt.Sprintf("%s was an encrypted archive this computer made, and now holds other files — it may have been tampered with, or another drive may now use this path; restore the whole archive folder from a backup copy", folder)
	case Foreign:
		return fmt.Sprintf("%s holds files naming a different encrypted archive than the one this computer recorded there — it may have been tampered with; %s", folder, forgetOffer)
	case KeysMissing:
		return fmt.Sprintf("%s holds an encrypted archive this computer made, but its key files are missing or damaged — restore the whole archive folder from a backup copy", folder)
	case Gone:
		return fmt.Sprintf("%s used to hold an encrypted archive this computer made; %s", folder, forgetOffer)
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
// or by this computer's record of exactly out — and why.
func Here(out string) (bool, string, error) {
	enc, why, merr := vault.EncryptedMarker(out)
	if enc {
		return true, why, nil
	}
	rec, rerr := load()
	if rerr != nil {
		if holdsArchiveFiles(out) {
			return true, rerr.Error(), nil
		}
		return false, "", merr
	}
	if m, ok := rec.Lookup(out); ok && encrecord.Spelling(m.Folder) == encrecord.Spelling(out) {
		c := classify(m)
		if c == None {
			return true, "it is an encrypted archive this computer made", nil
		}
		return true, Wording(c, m.Folder), nil
	}
	return false, "", merr
}
