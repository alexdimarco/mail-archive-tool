// Package archindex is the Desktop-maintained, folder-only index of the archives
// an install manages: known-archives.json in the per-user config dir. It records
// ONLY folder locations and provenance; every per-archive fact (kind, label,
// sealed state, schedule) is derived live from the archive itself, so nothing here
// can drift against the archive (operator C2 decision, 2026-10-20,
// docs/design-archive-manager.md rev 2 §4). "Which archive is current" lives in
// desktop-settings.json out, never here — the index carries no active field (the
// active-archive invariant).
//
// The index is a convenience cache, not a system of record: a lost index is
// rebuilt by import + the recovery scan (§5.1). It is a LEAF package — wizard,
// desktop and the cmd layer import it; it imports none of them.
package archindex

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/util"
)

// KindEncrypted is the kind of an encrypted archive: known without its key from
// its vault marker (C10); what it was made from is sealed.
const KindEncrypted = "encrypted"

const (
	// Name of the index file, beside last-archive.txt and the job files.
	Name = "known-archives.json"
	// Version of the index format; a file written by a newer build is refused
	// (the job-file / manifest discipline).
	Version = 1
	// MaxEntries bounds the index so a recovery scan that finds many archives
	// cannot produce a file the reader then refuses as oversize (SD-8). Sized for
	// the handful of archives a person keeps, with generous headroom.
	MaxEntries = 512
	// maxIndexBytes caps the index read, sized to MaxEntries — NOT the 64 KiB
	// job-file cap (SD-8).
	maxIndexBytes = 256 * 1024
	// The manifest read during recognition is bounded by recognise.go's manifestCap and
	// its walk's bounds (SD-2, amended 2026-11-02 by MA-1034: it was a 1 MiB read parsed
	// whole, which a real archive of about 2,200 messages outgrows).
	// manifestName marks a folder as a MailArchive archive.
	manifestName = ".mailarchive-manifest.json"
)

// Entry is one managed archive: a folder location plus provenance, nothing else.
type Entry struct {
	Path       string    `json:"path"`
	AddedAt    time.Time `json:"added_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
}

// Index is the folder-only list. No active / state / schedule / kind / label field
// lives here, by design (the active-archive invariant; MA-662).
type Index struct {
	Version int     `json:"version"`
	Entries []Entry `json:"entries"`
}

// Path is the index file in the per-user config dir (job.Dir()).
func Path() (string, error) {
	d, err := job.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, Name), nil
}

// Read loads the index from the config dir; an absent index is an empty one (the
// caller's cue to import + offer the recovery scan). See ReadFile.
func Read() (Index, error) {
	p, err := Path()
	if err != nil {
		return Index{Version: Version}, err
	}
	return ReadFile(p)
}

// ReadFile loads the index at path with the hardened ReadSecureFile discipline — a
// symlink, non-regular, group/other-readable or oversize file is refused (SD-4) —
// and refuses a file written by a newer build or one over the entry bound (SD-8).
// An absent file yields an empty index with no error.
func ReadFile(path string) (Index, error) {
	data, err := util.ReadSecureFile(path, "archive index file", maxIndexBytes)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Index{Version: Version}, nil
		}
		return Index{}, err
	}
	var idx Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return Index{}, fmt.Errorf("archive index file %s is unreadable (%v): remove it to rebuild the list", path, err)
	}
	if idx.Version > Version {
		return Index{}, fmt.Errorf("archive index file %s was written by a newer mailarchive (version %d, this build reads %d): upgrade, or remove it to rebuild the list", path, idx.Version, Version)
	}
	if len(idx.Entries) > MaxEntries {
		return Index{}, fmt.Errorf("archive index file %s lists %d archives, over the %d limit: remove it to rebuild the list", path, len(idx.Entries), MaxEntries)
	}
	return idx, nil
}

// Write persists the index to the config dir. See WriteFile.
func Write(idx Index) error {
	p, err := Path()
	if err != nil {
		return err
	}
	return WriteFile(p, idx)
}

// WriteFile persists idx to path atomically and owner-only, then fsyncs the parent
// directory so a lost update is not silent (SD-7). It refuses to write more than
// MaxEntries (SD-8).
func WriteFile(path string, idx Index) error {
	idx.Version = Version
	if len(idx.Entries) > MaxEntries {
		return fmt.Errorf("refusing to write archive index %s with %d archives, over the %d limit: remove some first", path, len(idx.Entries), MaxEntries)
	}
	if err := encmark.RefuseSettingsWrite(path, "the list of your archives"); err != nil {
		return err
	}
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	if err := util.WriteFileAtomic0600(path, data); err != nil {
		return err
	}
	fsyncDir(filepath.Dir(path))
	return nil
}

// fsyncDir flushes a directory entry so the atomic rename is durable across a
// crash (SD-7). Best-effort: some filesystems (and Windows) do not support a
// directory fsync, and a failure here does not undo the rename that already placed
// the file.
func fsyncDir(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	_ = d.Sync()
	_ = d.Close()
}

// IsArchive reports whether folder holds a readable MailArchive archive, read the
// SAFE way (see inspect). It doubles as the availability probe: an entry whose
// folder is unreachable or no longer an archive is shown greyed, never removed
// (SD-6, MA-657).
func IsArchive(folder string) bool {
	_, ok := inspect(folder)
	return ok
}

// Kind derives local / m365 / mixed / empty from the archive's manifest, or "" when
// the folder is not a readable archive. Derived live, never cached in the index.
func Kind(folder string) string {
	k, _ := inspect(folder)
	return k
}

// inspect reads folder's manifest the SAFE way and returns its derived kind. It
// opens the manifest INSIDE an os.Root on the folder (so a manifest symlink cannot
// escape it, and a pipe or a folder is refused: util.OpenRegularFileIn), and walks it
// as it streams in under recognise.go's bounds (SD-2; MA-1034, MA-1035): at most
// manifestCap bytes, only the stores (and a name or the version) kept, under bounds of
// their own, the records checked byte by byte and skipped, never held. Its
// store ids are read lexically (state.SnapshotStoreID, as state.LoadFrom reads them),
// never resolved on disk, so a planted id like a UNC path never opens an SMB
// connection or follows a link out of the folder (SD-1; never state.Load /
// archiveKind, which run filepath.EvalSymlinks on those untrusted ids). The verdict is
// kept for the folder while it and its manifest stay the same file, of the same size
// and modification time (recognised), so a page load does not read a large manifest
// again; reads of the same folder at once walk it once (walkAlone).
func inspect(folder string) (kind string, ok bool) {
	folder = strings.TrimSpace(folder)
	if folder == "" {
		return "", false
	}
	// An encrypted archive is an archive: listed, labelled, never greyed out as
	// broken because its manifest is not a plaintext one (C10, phase2a §5).
	if enc, _, _ := encmark.Here(folder); enc {
		return KindEncrypted, true
	}
	key := filepath.Clean(folder)
	// Reads of the same folder at once walk it once: a later one waits, holding no file open,
	// then takes the kept verdict (MA-1035).
	defer walkAlone(key)()
	root, err := os.OpenRoot(folder)
	if err != nil {
		return "", false
	}
	defer root.Close()
	f, err := util.OpenRegularFileIn(root, manifestName)
	if err != nil {
		return "", false
	}
	defer f.Close()
	man, err := f.Stat()
	if err != nil {
		return "", false
	}
	dir, err := statOpen(root)
	if err != nil {
		return "", false
	}
	if r, ok := remembered(key, dir, man); ok {
		return r.kind, r.ok
	}
	if man.Size() > manifestCap {
		remember(key, recognised{dir: dir, man: man})
		return "", false
	}
	kind, err = scanManifest(manifestReader(f))
	if err != nil && readError(err) {
		return "", false // a read that failed may pass next time: not kept
	}
	remember(key, recognised{dir: dir, man: man, kind: kind, ok: err == nil})
	return kind, err == nil
}

// manifestReader is the open manifest as the walk reads it: the file itself. A var only so a test
// can inject a read that fails part way, or hold a walk while another starts (fault injection).
var manifestReader = func(f *os.File) io.Reader { return f }

// statOpen is the root folder's own facts, read from an open handle so its identity holds on
// Windows too (os.SameFile there reads a file's id from its handle).
func statOpen(root *os.Root) (os.FileInfo, error) {
	d, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	defer d.Close()
	return d.Stat()
}

// Reconcile adds any pointer folder that is a real archive and not already listed,
// stamping it at now; it is MONOTONE — it only adds and refreshes LastSeenAt, never
// removes (import pre-populates but never prunes; SD-9, MA-653). It stops at
// MaxEntries (SD-8); the caller logs when the cap is hit. Pointers are the known
// locations the caller gathered (desktop-settings out, last-archive.txt,
// descriptor-named folders) — archindex reads none of those itself, staying a leaf.
func Reconcile(idx Index, pointers []string, now time.Time) Index {
	have := map[string]bool{}
	for i := range idx.Entries {
		have[clean(idx.Entries[i].Path)] = true
	}
	for _, p := range pointers {
		p = clean(p)
		if p == "" {
			continue
		}
		if have[p] {
			touch(&idx, p, now)
			continue
		}
		if len(idx.Entries) >= MaxEntries {
			break
		}
		if !IsArchive(p) {
			continue
		}
		idx.Entries = append(idx.Entries, Entry{Path: p, AddedAt: now, LastSeenAt: now})
		have[p] = true
	}
	return idx
}

// Forget removes exactly the entry for path (an explicit owner action; the index
// only shrinks this way, S1-C5, MA-663). Other entries are left untouched.
func Forget(idx Index, path string) Index {
	path = clean(path)
	kept := make([]Entry, 0, len(idx.Entries))
	for _, e := range idx.Entries {
		if clean(e.Path) != path {
			kept = append(kept, e)
		}
	}
	idx.Entries = kept
	return idx
}

// Available reports whether the entry's folder is reachable AND still an archive, by
// a real bounded read (not a bare Stat, which a dehydrated cloud placeholder passes
// while offline; SD-6). An unavailable entry is shown greyed, never removed.
func Available(e Entry) bool { return IsArchive(e.Path) }

func touch(idx *Index, cleanedPath string, now time.Time) {
	for i := range idx.Entries {
		if clean(idx.Entries[i].Path) == cleanedPath {
			idx.Entries[i].LastSeenAt = now
			return
		}
	}
}

func clean(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	return filepath.Clean(p)
}
