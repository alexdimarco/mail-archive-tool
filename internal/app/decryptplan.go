package app

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/instance"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/util"
)

// An unencrypted copy of an encrypted archive (phase 3 §10, P3-4) is planned without
// the key, and the plan is what the copy executes (X14): where the new folder goes,
// its exact name, how large it can be, the free space, and why the destination may
// leave this computer — each such reason asks twice (K5). Planning writes nothing.

// DecryptPlan is a planned unencrypted copy.
type DecryptPlan struct {
	Out    string // the encrypted archive, absolute
	Parent string // the folder the copy is made in
	Name   string // the new folder's name, fixed here: <archive>-unencrypted-<date>[-N]
	Dest   string // Parent/Name

	// NeedBytes is an upper bound on the copy's size: the sealed files it is made
	// from (the vault does not compress, so each is at least its plaintext) and a
	// reserve for the pages, report and README the copy writes besides.
	NeedBytes int64
	// MailBytes and ReserveBytes are NeedBytes' two parts, for a plan that shows them
	// apart (P3-8 friction: the estimate read as inflated): about the size of the mail
	// itself, and the room kept for the pages, report and README the copy writes.
	MailBytes, ReserveBytes int64
	FreeBytes               uint64 // the destination's free space; 0 with FreeKnown false when it cannot be read
	FreeKnown               bool

	// Leaves says why the copy may leave this computer — a synced folder, a folder
	// that may be synced, a removable or external drive, a network share. Each asks
	// twice (K5).
	Leaves   []string
	Warnings []string

	pointer container.Peeked // the record as it was planned: a backup since then makes the plan stale
	outside *CopyOutside
	parent  ceDirKey // the folder Parent named when it was planned
	parentK bool     // parent could be read
}

// AskTwice reports whether the copy must be confirmed twice before it starts (K5).
func (p *DecryptPlan) AskTwice() bool { return len(p.Leaves) > 0 }

// decryptReserve is the room kept beside the sealed files' size for what the copy
// writes that they do not hold: the folder pages, the report and the README.
func decryptReserve(sealed int64) int64 { return 32<<20 + sealed/50 }

// ErrDecryptStale refuses a copy whose plan no longer holds: a backup changed the
// archive, or the new folder's name was taken, since it was planned (X14).
var ErrDecryptStale = errors.New("the unencrypted copy's plan no longer holds")

// PlanDecryptCopy plans an unencrypted copy of the encrypted archive out into a new
// folder in parent (phase 3 §10). archives are the other archive folders this
// computer knows, settings the settings folders a front end keeps besides
// MailArchive's own; the copy is made inside none of them. It needs no key and
// writes nothing.
func (e *CopyExportEnv) PlanDecryptCopy(out, parent string, archives, settings []string, now time.Time) (*DecryptPlan, error) {
	abs, err := filepath.Abs(out)
	if err != nil {
		return nil, err
	}
	abs = filepath.Clean(abs)
	if !UnlockableAt(abs) {
		return nil, refuse("R-src", fmt.Sprintf("%s is not an encrypted archive MailArchive can open here, so no unencrypted copy of it can be made — `mailarchive status -out %q` says why.", abs, abs))
	}
	outside, err := e.CheckDecryptDestination(parent, abs, archives, settings)
	if err != nil {
		return nil, err
	}
	p := &DecryptPlan{Out: abs, Parent: parent, outside: outside}
	if p.Name, err = freeCopyName(parent, filepath.Base(abs)+"-unencrypted-"+now.Format("2006-01-02")); err != nil {
		return nil, err
	}
	p.Dest = filepath.Join(parent, p.Name)
	if p.pointer, err = container.PeekFile(abs); err != nil {
		return nil, fmt.Errorf("reading %s's record: %w", abs, err)
	}
	sealed := sealedSize(abs)
	p.MailBytes, p.ReserveBytes = sealed, decryptReserve(sealed)
	p.NeedBytes = p.MailBytes + p.ReserveBytes
	if free, ferr := e.freeOnDest(parent); ferr == nil {
		p.FreeBytes, p.FreeKnown = free, true
		if int64(free) < p.NeedBytes {
			p.Warnings = append(p.Warnings, fmt.Sprintf("%s may not have room: %s is free, and the copy takes about %s for the mail plus up to %s kept free for its pages, report and README", parent, humanBytes(int64(free)), humanBytes(p.MailBytes), humanBytes(p.ReserveBytes)))
		}
	}
	p.Leaves = e.copyLeaves(parent)
	p.parent, p.parentK = e.folderKey(parent)
	return p, nil
}

// folderKey is the identity of the folder dir names now, by path.
func (e *CopyExportEnv) folderKey(dir string) (ceDirKey, bool) {
	ids, err := e.destChain(dir)
	if err != nil || len(ids) == 0 {
		return ceDirKey{}, false
	}
	return dirKeyOf(ids[0]), true
}

// copyLeaves says why parent may leave this computer (K5): each reason asks twice. It
// judges the folder as spelled and where its links lead, so a link into a synced
// folder or onto a removable drive asks twice too.
func (e *CopyExportEnv) copyLeaves(parent string) []string {
	var why []string
	spellings := []string{parent}
	if rp := util.ResolveExisting(parent); rp != parent {
		spellings = append(spellings, rp)
	}
	svc, synced := "", false
	for _, s := range spellings {
		if svc, synced = util.UnderCloudSync(s); synced {
			break
		}
	}
	if synced {
		why = append(why, fmt.Sprintf("%s is synced to %s: the readable copy will be uploaded there", parent, svc))
	} else if mayICloud(spellings) {
		why = append(why, fmt.Sprintf("%s may be synced to iCloud (when iCloud keeps your Desktop and Documents folders): the readable copy may be uploaded there", parent))
	}
	if e.removable(parent) || (len(spellings) > 1 && e.removable(spellings[1])) {
		why = append(why, fmt.Sprintf("%s is on a removable or external drive: the readable copy leaves this computer with it", parent))
	}
	if e.viewFor(parent).Kind == viewShare {
		why = append(why, fmt.Sprintf("%s is on a network share: the readable copy is written to another computer", parent))
	}
	return why
}

// mayICloud says one of the spellings is in the Desktop or Documents folder, which
// macOS may keep in iCloud — by path, or by identity for a link or another spelling
// of the same folder (/System/Volumes/Data/Users/…).
func mayICloud(spellings []string) bool {
	home, err := os.UserHomeDir()
	if err != nil || runtime.GOOS != "darwin" {
		return false
	}
	for _, s := range spellings {
		if util.MaySync(s, home) {
			return true
		}
		for _, d := range []string{"Desktop", "Documents"} {
			if base := filepath.Join(home, d); dirExists(base) && util.Within(base, s) {
				return true
			}
		}
	}
	return false
}

func dirExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}

// CheckDecryptDestination checks parent as the folder an unencrypted copy of the
// encrypted archive out is made in: an absolute, clean path to an existing folder;
// not inside an encrypted archive; not inside a folder MailArchive keeps its
// settings, record, sign-in or working copies in (uninstall removes those, and they
// are not meant to hold mail); and outside out and every other archive, by path and
// by identity (CheckDestinationOther). It writes nothing; it returns what the copy
// checks again before it starts.
func (e *CopyExportEnv) CheckDecryptDestination(parent, out string, archives, settings []string) (*CopyOutside, error) {
	all, known, err := e.checkDecryptPaths(parent, out, archives, settings)
	if err != nil {
		return nil, err
	}
	outside, err := e.CheckDestinationOther(parent, all, out)
	if err != nil {
		var r *CopyRefusal
		if errors.As(err, &r) {
			return nil, copyWording(r, parent)
		}
		return nil, err
	}
	// The other archives this computer knows, each on its own, so a refusal names the
	// archive and its cause — never a share or FUSE mount the folder is not on (the PG
	// pass, FIX-02; R-f2 kept for them, CRIT-07).
	for _, k := range known {
		o, err := e.CheckDestinationOther(parent, []string{k}, out)
		if err != nil {
			var r *CopyRefusal
			if errors.As(err, &r) && r.ID == "R-f2" {
				return nil, refuse("R-f2", fmt.Sprintf("MailArchive couldn't confirm that %s is outside the archive %s, which this computer knows — that archive is on a network share or a FUSE mount, or too large or partly unreadable to check — so no unencrypted copy was made. Choose a folder on another drive; if that archive is no longer used, remove it from Your archives in MailArchive Desktop.", parent, k))
			}
			if errors.As(err, &r) {
				return nil, copyWording(r, parent)
			}
			return nil, err
		}
		outside.archives = append(outside.archives, o.archives...)
		outside.sets = append(outside.sets, o.sets...)
	}
	return outside, nil
}

// checkDecryptPaths is the copy's checks that walk no archive: the path, inside an
// encrypted archive, MailArchive's own folders, and inside an archive by path. It
// returns the archives to compare by identity: those named and those found above the
// folder, and apart the others this computer knows.
func (e *CopyExportEnv) checkDecryptPaths(parent, out string, archives, settings []string) (all, known []string, _ error) {
	if r := checkCopyParent(parent); r != nil {
		return nil, nil, r
	}
	if res, _ := encmark.At(parent); res.Encrypted || res.ByRecordErr {
		why := res.Why
		if res.ByRecordErr {
			why = "this computer's record of encrypted archives cannot be used, and it holds MailArchive's files"
		}
		where := fmt.Sprintf("is inside an encrypted archive (%s)", res.Folder)
		if res.Folder == parent || res.ByRecordErr {
			where = "may be an encrypted archive"
			if !res.ByRecordErr {
				where = "is an encrypted archive"
			}
		}
		return nil, nil, refuse("R-enc", fmt.Sprintf("The folder %s %s — %s. An unencrypted copy is never made inside one: choose a folder outside it.", parent, where, why))
	}
	rp := util.ResolveExisting(parent)
	for _, d := range append(mailarchiveOwnDirs(), settings...) {
		if strings.TrimSpace(d) == "" {
			continue
		}
		if util.Within(util.ResolveExisting(d), rp) {
			return nil, nil, refuse("R-set", fmt.Sprintf("The folder %s is inside %s, where MailArchive keeps its settings or working files — uninstalling MailArchive removes it. Choose a folder of your own for the unencrypted copy.", parent, d))
		}
	}
	all = append([]string{out}, archives...)
	all = append(all, ArchivesAbove(parent)...)
	for _, k := range knownArchives() {
		if !slices.Contains(all, k) {
			known = append(known, k)
		}
	}
	for _, a := range append(append([]string(nil), all...), known...) {
		if overlap, how := util.Overlap(parent, strings.TrimSpace(a)); overlap && how != "contains" && strings.TrimSpace(a) != "" {
			return nil, nil, copyRefusalF(a)
		}
	}
	return all, known, nil
}

// knownArchives are the archive folders this computer knows: the Desktop's list, its
// current archive and the wizard's last one. The copy is made outside each, by path
// and by identity, from either front end.
var knownArchives = func() []string {
	var out []string
	if idx, err := archindex.Read(); err == nil {
		for _, e := range idx.Entries {
			out = append(out, e.Path)
		}
	}
	if f, err := job.DesktopSettingsFile(); err == nil {
		out = append(out, job.ReadDesktopOut(f))
	}
	if f, err := job.LastArchiveFile(); err == nil {
		out = append(out, job.ReadLastArchive(f))
	}
	kept := out[:0]
	for _, a := range out {
		if a = strings.TrimSpace(a); filepath.IsAbs(a) {
			kept = append(kept, filepath.Clean(a))
		}
	}
	return kept
}

// mailarchiveOwnDirs are the folders MailArchive keeps its settings, this
// computer's record of encrypted archives, its instance files, its sign-in and its
// working copies in.
func mailarchiveOwnDirs() []string {
	var dirs []string
	for _, f := range []func() (string, error){job.Dir, instance.Dir, encrecord.Dir, graphconfig.DefaultConfigDir, job.ScratchDir} {
		if d, err := f(); err == nil {
			dirs = append(dirs, d)
		}
	}
	if d, err := job.ScratchDir(); err == nil {
		dirs = append(dirs, filepath.Dir(d)) // the cache folder the scratch folder is in
	}
	return dirs
}

// checkCopyParent is the copy's path check, worded for it and for either front end.
func checkCopyParent(parent string) *CopyRefusal {
	switch {
	case strings.TrimSpace(parent) == "":
		return refuse("R-e", "No folder is chosen for the unencrypted copy — name an existing folder; the copy is made in a new folder inside it.")
	case strings.ContainsRune(parent, 0) || !filepath.IsAbs(parent) || parent != filepath.Clean(parent):
		return refuse("R-e", fmt.Sprintf("The folder %s isn't a full path to an existing folder — MailArchive doesn't expand ~, %%NAME%% or $NAME. Name the full path, for example %s.", printable(parent), DestExampleFor(runtime.GOOS)))
	}
	fi, err := os.Stat(parent)
	switch {
	case err != nil:
		return refuse("R-e", fmt.Sprintf("The folder %s doesn't exist — the unencrypted copy is made in a new folder inside an existing one.", parent))
	case !fi.IsDir():
		return refuse("R-e", fmt.Sprintf("%s is a file, not a folder — the unencrypted copy is made in a new folder inside an existing one.", parent))
	}
	return nil
}

func copyRefusalF(archive string) *CopyRefusal {
	return refuse("R-f", fmt.Sprintf("That folder is inside an archive (%s) — an unencrypted copy made there would mix into it. Choose a folder outside %s.", archive, archive))
}

// copyWording rewords a refusal of the export's destination check for the copy.
func copyWording(r *CopyRefusal, parent string) *CopyRefusal {
	switch r.ID {
	case "R-f":
		return refuse("R-f", fmt.Sprintf("The folder %s is an archive folder under another name (a link, a share or a mount of it) — an unencrypted copy made there would mix into it. Choose a folder outside every archive.", parent))
	case "R-f2":
		return refuse("R-f2", fmt.Sprintf("MailArchive couldn't confirm that %s is outside the archive — it or the archive is on a network share or a FUSE mount, where one folder can show under two names, so nothing was written. Choose a folder on this computer's own drive.", parent))
	}
	return r
}

// freeCopyName is base, or base-2, base-3 … — the first name nothing in parent
// holds now.
func freeCopyName(parent, base string) (string, error) {
	for n := 1; n <= 99; n++ {
		name := base
		if n > 1 {
			name = fmt.Sprintf("%s-%d", base, n)
		}
		if _, err := os.Lstat(filepath.Join(parent, name)); errors.Is(err, fs.ErrNotExist) {
			return name, nil
		} else if err != nil {
			return "", fmt.Errorf("checking %s: %w", filepath.Join(parent, name), err)
		}
	}
	return "", refuse("R-name", fmt.Sprintf("%s already holds %s and 98 more like it — remove the copies you no longer need, or choose another folder.", parent, base))
}

// sealedSize is the size of the sealed files an unencrypted copy is made from: the
// units and the record's chunks. A link or an unreadable entry adds nothing.
func sealedSize(out string) int64 {
	var total int64
	for _, dir := range []string{export.UnitsDir, container.ChunksDir} {
		filepath.WalkDir(filepath.Join(out, dir), func(_ string, d fs.DirEntry, err error) error {
			if err != nil || !d.Type().IsRegular() {
				return nil
			}
			if fi, ierr := d.Info(); ierr == nil {
				total += fi.Size()
			}
			return nil
		})
	}
	return total
}

// Recheck is the copy's repetition of its plan's checks just before it starts
// (X14): the destination again, and the plan itself — the archive's record and the
// new folder's name as they were planned — else ErrDecryptStale.
func (p *DecryptPlan) Recheck(e *CopyExportEnv, archives, settings []string) error {
	// The path checks again; identity against the sets the plan walked, never a second
	// walk of every archive under the lock (the PG pass, FIX-02).
	if _, _, err := e.checkDecryptPaths(p.Parent, p.Out, archives, settings); err != nil {
		return err
	}
	if err := p.outside.Recheck(e, p.Parent); err != nil {
		var r *CopyRefusal
		if errors.As(err, &r) {
			return copyWording(r, p.Parent)
		}
		return err
	}
	if k, ok := e.folderKey(p.Parent); ok != p.parentK || k != p.parent {
		return fmt.Errorf("%w: %s is not the folder it was when the copy was planned — plan the copy again", ErrDecryptStale, p.Parent)
	}
	for _, why := range e.copyLeaves(p.Parent) {
		if !slices.Contains(p.Leaves, why) {
			return fmt.Errorf("%w: %s — that was not so when the copy was planned; plan the copy again", ErrDecryptStale, why)
		}
	}
	now, err := container.PeekFile(p.Out)
	if err != nil || now != p.pointer {
		return fmt.Errorf("%w: %s changed since the copy was planned (a backup ran) — plan the copy again", ErrDecryptStale, p.Out)
	}
	if _, err := os.Lstat(p.Dest); !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: %s now exists — plan the copy again", ErrDecryptStale, p.Dest)
	}
	return nil
}
