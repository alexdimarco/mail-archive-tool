// Package state persists which messages have already been exported so that
// incremental runs only write new items.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"mail-archive-tool/internal/util"
)

// manifestVersion: 1 = paths only; 2 = completeness tracking (Missing/Terminal/
// Unresolved per record); 3 = store-scoped identity (the key carries the store
// token as its first component: token\x00folder\x00identity). Fields are
// additive. A version-1 file is migrated to the "unknown" sentinel on load; the
// v2→v3 re-scope is content-driven (a key holding exactly one NUL is a v2 key)
// and never triggered by the version integer, so it self-heals after an
// old-binary excursion (see Load).
const manifestVersion = 3

// UnknownSentinel marks a record whose completeness predates tracking: it is
// fillable, so the next incremental run re-examines it once.
const UnknownSentinel = "unknown"

// MissingBody is the gap label for a message captured with no body at all.
const MissingBody = "body"

// keySeparator joins the folder path and message identity into a manifest key.
// The NUL byte cannot appear in either component, so it is an unambiguous
// delimiter. encoding/json escapes it within the on-disk key string.
const keySeparator = "\x00"

// Record is what we remember about a single exported message.
type Record struct {
	Path       string    `json:"path"`   // export path relative to the output root
	Folder     string    `json:"folder"` // human-readable source folder path
	ExportedAt time.Time `json:"exported_at"`

	// Completeness (version 2). Missing lists FILLABLE gaps — "body", an
	// attachment label, or UnknownSentinel — that an on-demand source may still
	// deliver, so incremental runs re-examine the record. Terminal lists the
	// same kinds of gap from a complete-at-fetch source (nothing will ever fill
	// them; recorded and reported, never retried). Unresolved lists cid: tokens
	// with no matching part (informational). Subject/Date are carried only when
	// one of the three is non-empty, so the manifest grows with issues, not with
	// messages.
	Missing    []string `json:"missing,omitempty"`
	Terminal   []string `json:"terminal,omitempty"`
	Unresolved []string `json:"unresolved,omitempty"`
	Subject    string   `json:"subject,omitempty"`
	Date       string   `json:"date,omitempty"` // RFC 3339 UTC

	// Fingerprint (16 hex) of the message content, so a different message
	// reusing this record's Message-ID is recognized (R3). Empty on legacy
	// records.
	Fingerprint string `json:"fp,omitempty"`

	// Fixity (version 3) records the sha256 + byte length of each file the
	// exporter wrote for this record, so `verify` can detect bit-rot,
	// truncation or an accidental edit of an archived file. It is empty on
	// records written before fixity, or before `verify -record` baselined
	// them: such files are *unrecorded*, never *modified* (F3, FC11).
	Fixity *Fixity `json:"fixity,omitempty"`
}

// FileDigest is the recorded fixity of one exported file: the sha256 (hex) of
// its bytes and its length as written. `verify` re-hashes the file, capped at
// Size+1 bytes, and compares (F3/F4).
type FileDigest struct {
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// Fixity holds the digests of the files an export wrote for a single record —
// the `.html` always, the `-attachments.zip` and the `.eml` when they exist.
// The three are siblings derived from the record's Path, never repeated as
// keys, so the per-record cost is ~100 bytes per file (FC11).
type Fixity struct {
	HTML *FileDigest `json:"html,omitempty"`
	Zip  *FileDigest `json:"zip,omitempty"`
	EML  *FileDigest `json:"eml,omitempty"`
}

// HasDigest reports whether the record carries at least one recorded file
// digest.
func (r Record) HasDigest() bool {
	return r.Fixity != nil && (r.Fixity.HTML != nil || r.Fixity.Zip != nil || r.Fixity.EML != nil)
}

// Fillable reports whether an incremental run should re-examine the record.
func (r Record) Fillable() bool { return len(r.Missing) > 0 }

// Complete reports whether nothing at all is missing (fillable or terminal).
func (r Record) Complete() bool { return len(r.Missing) == 0 && len(r.Terminal) == 0 }

// HasIssues reports whether the record has anything to show in the report.
func (r Record) HasIssues() bool {
	return len(r.Missing) > 0 || len(r.Terminal) > 0 || len(r.Unresolved) > 0
}

// Unknown reports whether the record carries the legacy sentinel.
func (r Record) Unknown() bool {
	return len(r.Missing) == 1 && r.Missing[0] == UnknownSentinel
}

// Manifest is the set of exported messages, keyed by Key(token, folder,
// identity).
type Manifest struct {
	path string

	mu      sync.Mutex
	Version int               `json:"version"`
	Entries map[string]Record `json:"entries"`

	// Stores maps a source id (the original input path, or the Graph mailbox
	// address) to the store token that names both its on-disk directory and the
	// first component of its keys. The token is sticky: once assigned it is
	// persisted here, so directory and key names are order-independent from run
	// to run (R2, F1). A pre-v3 manifest has no stores map; tokens are then
	// assigned first-come on the next run, reproducing the plain segment the
	// files already use.
	Stores map[string]string `json:"stores,omitempty"`

	// Migrated counts the records converted to the unknown sentinel by this
	// Load (a version-1 file). Zero for a version-2/3 file.
	Migrated int `json:"-"`

	// Rekeyed counts the records re-scoped to a store-qualified key by this
	// Load (v2 keys, holding one NUL). Zero when nothing was re-scoped.
	Rekeyed int `json:"-"`

	// byPath is a lazily built reverse index (export path → key) used to
	// detect a file-stem collision between two different keys (R4).
	byPath map[string]string
}

// Key builds the manifest key for a message. The token scopes identity to the
// store (two mailboxes archived into one -out never collide, R6), the folder
// scopes it within the store (the same email filed in two folders is exported
// to both, R3), and a re-run still skips each (token, folder, message) triple
// it has already written.
func Key(token, folderPath, identity string) string {
	return token + keySeparator + folderPath + keySeparator + identity
}

// Token returns the store token for sourceID, assigning one the first time the
// source is seen and persisting it so it never changes (F1/FC2). The token is
// util.SanitizeSegment(store) for the first source to claim that segment; a
// later, different source that resolves to the same segment gets
// segment~<8 hex of sourceID> (lengthening the hash on the improbable second
// collision), so distinct sources with the same display name — Outlook's
// default "Outlook Data File", Thunderbird's "Local Folders" under every
// profile — never merge into one tree. The same token names the on-disk
// directory and the key.
func (m *Manifest) Token(sourceID, store string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Stores == nil {
		m.Stores = map[string]string{}
	}
	if tok, ok := m.Stores[sourceID]; ok {
		return tok
	}
	seg := util.SanitizeSegment(store)
	if !m.segmentOwnedLocked(seg) {
		m.Stores[sourceID] = seg
		return seg
	}
	for n := 8; ; n += 8 {
		if n > 40 {
			n = 40
		}
		tok := seg + "~" + util.HashHex(sourceID, n)
		if !m.segmentOwnedLocked(tok) {
			m.Stores[sourceID] = tok
			return tok
		}
		if n == 40 {
			// The full digest already collides with a different source: fall
			// back to a stable, unique token so nothing is ever lost.
			tok = seg + "~" + util.HashHex(sourceID+"\x00"+seg, 40)
			m.Stores[sourceID] = tok
			return tok
		}
	}
}

// segmentOwnedLocked reports whether some source already holds token tok. The
// caller holds m.mu.
func (m *Manifest) segmentOwnedLocked(tok string) bool {
	for _, t := range m.Stores {
		if t == tok {
			return true
		}
	}
	return false
}

// MigrateKey re-scopes a legacy (v2) manifest/index key to a v3 store-qualified
// key, driven purely by the key's content, never by a version integer (FC1). A
// v2 key holds exactly one NUL (folder\x00identity); a v3 key holds two
// (token\x00folder\x00identity), so the NUL count is an exact discriminator and
// a v3 key is never re-scoped (never double-prefixed). The store token is the
// first path segment of the record's own export path — the directory the files
// already live under — so the re-scoped key matches the token a fresh run
// computes. It returns the new key and true when a re-scope applies. The index
// repair passes the same function over its rows (path is the row's stored
// path), so both are repaired by identical content rules.
func MigrateKey(key, path string) (string, bool) {
	if path == "" {
		return key, false
	}
	if strings.Count(key, keySeparator) != 1 {
		return key, false
	}
	seg := firstSegment(path)
	if seg == "" {
		return key, false
	}
	return seg + keySeparator + key, true
}

// firstSegment returns the first forward-slash-delimited segment of a
// forward-slashed relative path ("token/folder/stem.html" → "token").
func firstSegment(path string) string {
	path = strings.TrimPrefix(path, "/")
	if i := strings.IndexByte(path, '/'); i >= 0 {
		return path[:i]
	}
	return path
}

// Load reads the manifest at path. A missing file yields an empty manifest
// bound to that path (so a later Save creates it).
func Load(path string) (*Manifest, error) {
	m := &Manifest{path: path, Version: manifestVersion, Entries: map[string]Record{}}

	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	if len(data) == 0 {
		return m, nil
	}
	if err := json.Unmarshal(data, m); err != nil {
		return nil, fmt.Errorf("manifest %s is corrupt or truncated (%v): restore it from a backup, or delete it to re-export everything (the archived files are untouched; incremental runs will rewrite them in place)", path, err)
	}
	if m.Entries == nil {
		m.Entries = map[string]Record{}
	}
	// Refuse a manifest written by a newer mailarchive rather than silently
	// mangling a format we do not understand (mirrors the job-file guard). The
	// already-shipped v2 binary has no such guard, so this only protects v3 and
	// later from still-newer binaries; a shared/synced -out must be written only
	// by upgraded binaries (README rule). (FC1.)
	if m.Version > manifestVersion {
		return nil, fmt.Errorf("manifest %s was written by a newer mailarchive (format version %d; this build understands %d): upgrade this copy of mailarchive, or point -out at an archive this version wrote", path, m.Version, manifestVersion)
	}
	// A version-1 file — written before completeness tracking, or rewritten by
	// an older binary after a downgrade — cannot say which entries are
	// incomplete. Never assume "complete": mark every record with the unknown
	// sentinel so the next incremental run re-examines it once (R1). The gate is
	// the literal version 1, decoupled from manifestVersion, so bumping the
	// constant can never re-sentinel a complete v2 archive (FC3).
	if m.Version < 2 {
		for key, r := range m.Entries {
			if !r.HasIssues() {
				r.Missing = []string{UnknownSentinel}
				m.Entries[key] = r
				m.Migrated++
			}
		}
	}
	// v2→v3 re-scope: every key holding exactly one NUL is a v2 key and is
	// re-keyed by its record's own store (the first segment of its path), so the
	// same mail archived from two stores stops colliding (R6). Content-driven,
	// not version-gated: a v2 excursion by an old binary (which regresses the
	// file to version 2 and writes one-NUL keys) is repaired by the next v3
	// load, and a key already holding two NULs is left exactly as it is —
	// idempotent, never double-prefixed (FC1).
	if len(m.Entries) > 0 {
		type rekey struct {
			oldKey, newKey string
			rec            Record
		}
		var rekeys []rekey
		for key, r := range m.Entries {
			if nk, ok := MigrateKey(key, r.Path); ok {
				rekeys = append(rekeys, rekey{oldKey: key, newKey: nk, rec: r})
			}
		}
		for _, rk := range rekeys {
			delete(m.Entries, rk.oldKey)
			// A collision on the new key is an old-binary excursion healing:
			// the re-scoped (later) record replaces the survivor, so exactly one
			// v3 key remains per message (R8).
			m.Entries[rk.newKey] = rk.rec
			m.Rekeyed++
		}
	}
	if m.Version < manifestVersion {
		m.Version = manifestVersion
	}
	return m, nil
}

// Has reports whether key has already been exported.
func (m *Manifest) Has(key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.Entries[key]
	return ok
}

// Get returns the record for key.
func (m *Manifest) Get(key string) (Record, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.Entries[key]
	return r, ok
}

// Add records key as exported.
func (m *Manifest) Add(key string, r Record) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.byPath != nil {
		if old, ok := m.Entries[key]; ok && old.Path != r.Path {
			delete(m.byPath, old.Path)
		}
		if r.Path != "" {
			m.byPath[r.Path] = key
		}
	}
	m.Entries[key] = r
}

// KeyForPath returns the key whose record owns the export path, if any. The
// reverse index is built on first use (rare: only when a stem already exists
// on disk for a key that has no record at that path).
func (m *Manifest) KeyForPath(path string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.byPath == nil {
		m.byPath = make(map[string]string, len(m.Entries))
		for k, r := range m.Entries {
			if r.Path != "" {
				m.byPath[r.Path] = k
			}
		}
	}
	k, ok := m.byPath[path]
	return k, ok
}

// Resolve clears the unknown sentinel from key without touching anything else:
// used when a complete-at-fetch source revisits a legacy record (nothing
// fillable can exist, so the record is complete as captured).
func (m *Manifest) Resolve(key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.Entries[key]
	if !ok || !r.Unknown() {
		return false
	}
	r.Missing = nil
	if !r.HasIssues() {
		r.Subject, r.Date = "", ""
	}
	m.Entries[key] = r
	return true
}

// Issues returns every record with something to report, sorted by folder,
// then date, then path (a stable order for the regenerated report).
func (m *Manifest) Issues() []Record {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Record
	for _, r := range m.Entries {
		if r.HasIssues() {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Folder != out[j].Folder {
			return out[i].Folder < out[j].Folder
		}
		if out[i].Date != out[j].Date {
			return out[i].Date < out[j].Date
		}
		return out[i].Path < out[j].Path
	})
	return out
}

// Counts returns how many records are fillable (excluding sentinels), terminal,
// and unknown (legacy sentinels).
func (m *Manifest) Counts() (fillable, terminal, unknown int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.Entries {
		switch {
		case r.Unknown():
			unknown++
		case r.Fillable():
			fillable++
		case len(r.Terminal) > 0:
			terminal++
		}
	}
	return
}

// FixityCounts returns how many records carry at least one recorded file
// digest (withFixity) and the total number of records (total). `status` prints
// "Fixity: N of M records carry digests" from these fields alone (FC12).
func (m *Manifest) FixityCounts() (withFixity, total int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	total = len(m.Entries)
	for _, r := range m.Entries {
		if r.HasDigest() {
			withFixity++
		}
	}
	return
}

// All returns a snapshot copy of every key→record, safe to range over while the
// manifest is separately updated in the same goroutine (verify iterates the
// snapshot and writes baselined fixity back through Add).
func (m *Manifest) All() map[string]Record {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]Record, len(m.Entries))
	for k, r := range m.Entries {
		out[k] = r
	}
	return out
}

// Delete removes key from the manifest. Absent keys are a no-op. Used by the
// reindex self-repair to prune entries whose exported file no longer exists.
func (m *Manifest) Delete(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.byPath != nil {
		if old, ok := m.Entries[key]; ok {
			delete(m.byPath, old.Path)
		}
	}
	delete(m.Entries, key)
}

// Len returns the number of recorded entries.
func (m *Manifest) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.Entries)
}

// Save writes the manifest atomically (temp file + rename) so a crash mid-write
// cannot corrupt an existing manifest.
func (m *Manifest) Save() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Version = manifestVersion
	// Compact, not pretty-printed: the manifest is machine state (one record per
	// exported message), never hand-read, and indentation roughly doubles its
	// size on a large archive (nas-03). Load still reads older pretty files.
	data, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}

	dir := filepath.Dir(m.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create manifest dir: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".manifest-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp manifest: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp manifest: %w", err)
	}
	// Durability, not just namespace atomicity: the bytes must be on disk
	// before the rename makes them the manifest of record, and the directory
	// entry must be on disk before we report success (R5).
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync temp manifest: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp manifest: %w", err)
	}
	if err := os.Rename(tmpName, m.path); err != nil {
		return fmt.Errorf("replace manifest: %w", err)
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync() // best effort: not every filesystem supports fsync on a directory
		d.Close()
	}
	return nil
}
