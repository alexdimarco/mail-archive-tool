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
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"mail-archive-tool/internal/util"
)

// manifestVersion: 1 = paths only; 2 = completeness tracking (Missing/Terminal/
// Unresolved per record); 3 = store-scoped identity (the key carries the store
// token as its first component: token\x00folder\x00identity); 4 = the go-back
// timeline — a mailbox-wide live key (token\x00identity, no folder — LiveKey),
// per-record folder-over-time fields (Folder/FirstFolder/FirstSeen/LastSeen/
// Present), and version-gated migrations. Fields are additive. A version-1 file
// is migrated to the "unknown" sentinel on load. The v2→v3 re-scope is now
// VERSION-gated on the stored version integer (< 3), not on the NUL count, so a
// folder-less one-NUL live key (a legitimate v4 key) is never mistaken for a
// legacy v2 folder\x00identity key and re-scoped (GB-01/F1). Load fills the v4
// timeline defaults for a pre-v4 record; the fingerprint-safe v3→v4 collapse of
// move-duplicates is CollapseByIdentity, invoked by the live path (a one-shot
// local import keeps its folder-scoped keys and R3 — §3.6). Load refuses a
// stored version above manifestVersion.
const manifestVersion = 5

// Fingerprint scheme tags (version 5). A record's FpScheme says which algorithm
// produced its Fingerprint. fpSchemeLegacy (0, the zero value, so every pre-v5
// or empty-fp record is legacy on load) is NOT comparable to a current
// fingerprint: the go-back work changed the fingerprint's date term (from the
// MIME Date header to the source's delivered timestamp), so a v1–v4 fp differs
// byte-for-byte from one this build computes over the same message. The download
// paths (a fillable retry; a no-Message-ID re-observation) therefore ADOPT — and
// never #fp-split against — a legacy-scheme sibling, so a migrated record is
// recognised as the same message and never re-split into a duplicate (design
// rev-4 §3). fpSchemeCurrent marks a fingerprint this build wrote.
const (
	fpSchemeLegacy = 0
	// FpSchemeCurrent is the scheme this build stamps on a fingerprint it writes;
	// exported so the exporter (which computes the fingerprint) can tag a Record
	// and can ask whether a stored fingerprint is comparable (FingerprintComparable).
	FpSchemeCurrent = 1
)

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

	// FpScheme (version 5) tags which algorithm produced Fingerprint: 0
	// (fpSchemeLegacy, the zero value) = a pre-v5 or empty fp that is NOT
	// comparable to a current fingerprint; fpSchemeCurrent = one this build wrote.
	// The download #fp-split ADOPTS a legacy-scheme sibling rather than splitting
	// against it, so a migrated record is never re-duplicated (design rev-4 §3).
	FpScheme int `json:"fp_scheme,omitempty"`

	// AlsoFiles (version 5) lists extra on-disk paths (relative to -out) that
	// belong to this message but sit outside its canonical Path — the files of a
	// move-duplicate LOSER that CollapseByIdentity unified into this record (the
	// loser's copy stays on disk, R13). Redaction (reindex) deletes these
	// alongside Path so no copy of a redacted message is left served (rev-4 §6).
	AlsoFiles []string `json:"also_files,omitempty"`

	// Fixity (version 3) records the sha256 + byte length of each file the
	// exporter wrote for this record, so `verify` can detect bit-rot,
	// truncation or an accidental edit of an archived file. It is empty on
	// records written before fixity, or before `verify -record` baselined
	// them: such files are *unrecorded*, never *modified* (F3, FC11).
	Fixity *Fixity `json:"fixity,omitempty"`

	// Folder-over-time (version 4, the go-back timeline). Folder (above) is the
	// message's CURRENT — most-recently-observed — source folder; FirstFolder is
	// where it was first captured (the physical, static-page grouping the file
	// lives under, which a normal run never moves — R13). FirstSeen and LastSeen
	// bound its observed lifetime, and Present is false once a full mailbox walk
	// finds it gone (the file is KEPT — that is a timeline event, not a
	// redaction). A pre-v4 record gets these filled at load (Present=true,
	// FirstFolder=Folder, FirstSeen=LastSeen=ExportedAt — GB-05); the live path
	// maintains them thereafter through MergeFields and CollapseByIdentity. The
	// times serialize like ExportedAt (a zero time is rendered, not omitted);
	// FirstFolder and Present carry omitempty so a folder-scoped local record,
	// which uses neither, stays compact.
	FirstFolder string    `json:"first_folder,omitempty"`
	FirstSeen   time.Time `json:"first_seen,omitempty"`
	LastSeen    time.Time `json:"last_seen,omitempty"`
	Present     bool      `json:"present,omitempty"`
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

	// CollapsedTokens records, per store token, that the one-time v3→v5
	// identity-collapse (CollapseByIdentity) has run for that mailbox — set after
	// the first successful live run of the token whether or not it had anything to
	// collapse, so a fresh v5 archive is marked immediately and a still-v3 second
	// mailbox onboarded in a LATER run is collapsed then (per-token, never a
	// manifest-wide scalar — EC4). The live path skips the collapse scan for a
	// marked token.
	CollapsedTokens map[string]bool `json:"collapsed_tokens,omitempty"`

	// Migrated counts the records converted to the unknown sentinel by this
	// Load (a version-1 file). Zero for a version-2/3 file.
	Migrated int `json:"-"`

	// Rekeyed counts the records re-scoped to a store-qualified key by this
	// Load (v2 keys, holding one NUL). Zero when nothing was re-scoped.
	Rekeyed int `json:"-"`

	// StoresMigrated counts the store ids canonicalized (or dropped, or collapsed
	// onto another token) by this Load — a one-time upgrade of an archive keyed by
	// the raw input spelling, or a defensive drop of a tampered token. Zero when
	// the map was already canonical.
	StoresMigrated int `json:"-"`

	// LoadedVersion is the format version integer read from disk, BEFORE Load
	// upgrades Version to the current constant. The live path reads it to decide
	// whether the fingerprint-safe v3→v4 collapse (CollapseByIdentity) still owes
	// a run (LoadedVersion < 4); it is manifestVersion for a fresh/empty archive.
	LoadedVersion int `json:"-"`

	// Collapsed counts the manifest rows unified away by the last
	// CollapseByIdentity call (each a move-duplicate merged into its surviving
	// sibling). The live path logs "collapsed N move-duplicates by identity".
	Collapsed int `json:"-"`

	// byPath is a lazily built reverse index (export path → key) used to
	// detect a file-stem collision between two different keys (R4).
	byPath map[string]string

	// byIdent is the mailbox-wide identity index (message identity → every
	// manifest key carrying it, with that key's fingerprint), built at load and
	// maintained on Add/Delete. The live path looks a candidate's Message-ID up
	// here to find an already-archived sibling before download (R17) and to let a
	// move-collapsed record coexist with its #fp-qualified siblings (§3.1/§3.2).
	byIdent map[string][]IdentRef
}

// IdentRef pairs a manifest key with its record's content fingerprint, the value
// type of the mailbox-wide identity index (see KeysForIdentity).
type IdentRef struct {
	Key         string
	Fingerprint string
}

// CollapseLoss records one manifest row dropped by CollapseByIdentity: its own
// key and export path (the file is LEFT ON DISK — R13; `verify` reports it as
// unexpected until a reconcile sweeps it), the folder it occupied, its capture
// time, and the key of the surviving sibling that now represents the message.
// The caller records each as a history folder-assertion event (so no location is
// lost — §3.1) and prunes the loser's index row; no message is silently dropped.
type CollapseLoss struct {
	LoserKey    string
	LoserPath   string
	Folder      string
	ExportedAt  time.Time
	SurvivorKey string
}

// Key builds the manifest key for a message. The token scopes identity to the
// store (two mailboxes archived into one -out never collide, R6), the folder
// scopes it within the store (the same email filed in two folders is exported
// to both, R3), and a re-run still skips each (token, folder, message) triple
// it has already written.
func Key(token, folderPath, identity string) string {
	return token + keySeparator + folderPath + keySeparator + identity
}

// LiveKey builds the mailbox-wide physical key for a message captured from a
// live source (Graph; IMAP later): token\x00identity, with NO folder component.
// One message is stored once per mailbox (R3 reworded); its folder over time is
// carried in the record (Folder/FirstFolder) and the history log, not in the
// key, so a folder MOVE is a field/log update rather than a second archived copy
// (§3.1). Qualify appends a content fingerprint exactly as for a folder-scoped
// Key, filing a genuinely different message that reused a Message-ID under its
// own #fp key (R1). A live key holds ONE NUL; because v4 key-format
// discrimination is now version-gated (see Load), that single NUL is never
// mistaken for a legacy v2 folder\x00identity key and re-scoped. Folder-scoped
// Key is retained for one-shot local imports, which keep per-folder copies.
func LiveKey(token, identity string) string {
	return token + keySeparator + identity
}

// Qualify appends a content fingerprint to a manifest key with the NUL
// separator, filing a DIFFERENT message that reused an already-archived
// Message-ID under its own key. NUL cannot occur in any source-derived key
// component (Message-ID parsing yields only vchar/multibyte; the sha fallback is
// hex), so a qualified key can never coincide with any plaintext identity-derived
// key — a crafted Message-ID (even one literally containing "#"+fingerprint) can
// never pre-occupy the slot and make a distinct message look already-seen
// (AGG2-1/R1/R3). A qualified key holds three NUL separators, so MigrateKey —
// which re-scopes only one-NUL v2 keys — never disturbs it.
func Qualify(key, fp string) string {
	return key + keySeparator + fp
}

// SafeToken reports whether tok is a single, safe on-disk path segment: it names
// exactly one directory under the output root and can never traverse out of it.
// A token read back from a manifest is untrusted (any writer of the archive
// directory can tamper with the persisted map), so both Load and the exporter
// validate it before it becomes a directory name (INS2-1/R4).
func SafeToken(tok string) bool {
	if tok == "" || tok == "." || tok == ".." {
		return false
	}
	if strings.ContainsRune(tok, '/') || strings.ContainsRune(tok, filepath.Separator) {
		return false
	}
	if tok != filepath.Base(tok) {
		return false
	}
	for _, r := range tok {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// PathSourceID is the store id for a local file/directory source, canonicalized
// so different spellings of one physical source — relative vs absolute, a
// symlinked component, a trailing separator, or letter case on a case-insensitive
// volume — resolve to ONE store token instead of minting a duplicate tree
// (INT-CC-1/R2). The absolute path is symlink-resolved when it can be and cleaned
// otherwise, and case-folded on the case-insensitive platforms (Windows, macOS).
// The "path:" prefix keeps a filesystem id from ever colliding with a Graph
// mailbox id. Callers keep reading the source through the original path; only the
// token seed is canonicalized.
func PathSourceID(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		abs = p
	}
	if resolved, rerr := filepath.EvalSymlinks(abs); rerr == nil {
		abs = resolved
	} else {
		abs = filepath.Clean(abs)
	}
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		abs = strings.ToLower(abs)
	}
	return "path:" + abs
}

// MailboxSourceID is the store id for a Graph mailbox, lower-cased and trimmed
// because Microsoft treats the UPN case-insensitively — so a re-run whose
// -mailbox differs only in case still hits the same token and re-downloads
// nothing (INT-CC-1/R17).
func MailboxSourceID(addr string) string {
	return "mailbox:" + strings.ToLower(strings.TrimSpace(addr))
}

// migrateStoreID rewrites a legacy (unprefixed) store id to its canonical form.
// An unprefixed id that looks like an email address (contains '@' and is not an
// absolute path) is a Graph mailbox; anything else is a filesystem path. An id
// already carrying a "path:"/"mailbox:" prefix is returned unchanged, so the
// migration is idempotent.
func migrateStoreID(old string) string {
	if strings.HasPrefix(old, "path:") || strings.HasPrefix(old, "mailbox:") {
		return old
	}
	if strings.Contains(old, "@") && !filepath.IsAbs(old) {
		return MailboxSourceID(old)
	}
	return PathSourceID(old)
}

// preferPlainToken picks which store token to keep when two source spellings
// collapse to one id: the plain (hashless) segment if exactly one is plain, else
// the lexicographically smaller — a stable, order-independent choice. The stale
// duplicate tree the other token named on disk is left as-is (it cannot be
// silently merged); future runs write only to the kept token.
func preferPlainToken(a, b string) string {
	aPlain, bPlain := !strings.Contains(a, "~"), !strings.Contains(b, "~")
	switch {
	case aPlain && !bPlain:
		return a
	case bPlain && !aPlain:
		return b
	case a <= b:
		return a
	default:
		return b
	}
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
	if tok, ok := m.Stores[sourceID]; ok && SafeToken(tok) {
		return tok
	}
	// A missing id assigns a fresh token below; a stored token that is not a
	// single safe segment (a tampered manifest) is DISCARDED and re-derived here
	// rather than trusted, so a write can never be redirected outside the output
	// root (INS2-1/R4). Re-derivation keeps a corrupted archive usable while
	// guaranteeing containment.
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
	m := &Manifest{path: path, Version: manifestVersion, LoadedVersion: manifestVersion, Entries: map[string]Record{}, byIdent: map[string][]IdentRef{}}

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
	// Remember the on-disk version before any upgrade rewrites it: the live path
	// consults LoadedVersion to know whether the v3→v4 collapse still owes a run.
	m.LoadedVersion = m.Version
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
	// same mail archived from two stores stops colliding (R6). VERSION-gated on
	// the stored version integer (< 3), not on the NUL count: a v4 live key
	// (token\x00identity) legitimately holds ONE NUL, so a content-driven rule
	// would mistake it for a v2 folder\x00identity key and wrongly re-scope it
	// (GB-01/F1). Gating on the version means a v2 excursion by an old binary
	// (which regresses the file to version 2 and writes one-NUL keys) is still
	// repaired by the next load, while a v3/v4 key is left exactly as it is —
	// idempotent, never double-prefixed, and a folder-less live key is never
	// touched (FC1). Within a v2 file MigrateKey still discriminates per key by
	// content, so a mixed-key excursion re-scopes only its one-NUL rows.
	if m.LoadedVersion < 3 && len(m.Entries) > 0 {
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
			// A collision on the new key is an old-binary excursion healing. Keep
			// the record ALREADY at the v3 key — the store-qualified survivor an
			// upgraded binary wrote at the canonical path — and drop the legacy v2
			// entry, so verify keeps pointing at the canonical file instead of the
			// excursion's duplicate copy (Friction #5/R8). When the v3 key is free,
			// the re-scoped record takes it.
			if _, exists := m.Entries[rk.newKey]; !exists {
				m.Entries[rk.newKey] = rk.rec
			}
			m.Rekeyed++
		}
	}
	// v3→v4 timeline defaults: a pre-v4 record has none of the folder-over-time
	// fields, so seed them from what it already knows — Present=true (a walk has
	// not yet found it gone), FirstFolder=Folder (the folder it was captured in),
	// FirstSeen=LastSeen=ExportedAt (its capture time bounds a one-observation
	// lifetime) — GB-05. Version-gated on the stored version so a genuine v4
	// record's maintained fields (a Present=false gone message, a distinct
	// FirstFolder) are never stomped on reload. This is always safe: it only
	// fills empty fields and never merges records — the fingerprint-safe collapse
	// of move-duplicates is CollapseByIdentity, confined to the live path (§3.6).
	if m.LoadedVersion < 4 {
		for key, r := range m.Entries {
			if r.FirstFolder == "" {
				r.FirstFolder = r.Folder
			}
			if r.FirstSeen.IsZero() {
				r.FirstSeen = r.ExportedAt
			}
			if r.LastSeen.IsZero() {
				r.LastSeen = r.ExportedAt
			}
			r.Present = true
			m.Entries[key] = r
		}
	}
	// Canonicalize/validate the store id map (INT-CC-1/INS2-1). Existing archives
	// keyed each store by the raw -input spelling (or the raw mailbox), so a run
	// computing a canonical id would miss the entry and re-archive into a
	// duplicate tree; rewrite each id to its canonical form (path:/mailbox:) so a
	// post-upgrade run finds the existing token. Two spellings that resolve to one
	// id collapse onto the token that owns the plain segment, and a token that is
	// not a single safe path segment (tampered or corrupt) is dropped so Token
	// re-derives a fresh, in-root one.
	if len(m.Stores) > 0 {
		olds := make([]string, 0, len(m.Stores))
		for k := range m.Stores {
			olds = append(olds, k)
		}
		sort.Strings(olds) // deterministic collision resolution
		migrated := make(map[string]string, len(m.Stores))
		for _, old := range olds {
			tok := m.Stores[old]
			if !SafeToken(tok) {
				m.StoresMigrated++ // dropped; Token re-derives a fresh one on next use
				continue
			}
			id := migrateStoreID(old)
			changed := id != old
			if existing, ok := migrated[id]; ok {
				migrated[id] = preferPlainToken(existing, tok)
				changed = true
			} else {
				migrated[id] = tok
			}
			if changed {
				m.StoresMigrated++
			}
		}
		m.Stores = migrated
	}
	if m.Version < manifestVersion {
		m.Version = manifestVersion
	}
	m.buildIdentIndexLocked()
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
	m.identUpsertLocked(key, r.Fingerprint)
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
// "Fixity coverage: N of M records recorded" from these fields alone (FC12).
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
	m.identRemoveLocked(key)
	delete(m.Entries, key)
}

// Len returns the number of recorded entries.
func (m *Manifest) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.Entries)
}

// MergeFields applies a fresh observation to the record at key WITHOUT rebuilding
// it: it rewrites only the mutable timeline fields — Folder (the current source
// folder), LastSeen, Present — and leaves every capture-time field untouched
// (Path, Fixity, Fingerprint, ExportedAt, the completeness lists, and the
// set-once FirstFolder/FirstSeen). It is the no-download move merge (§3.2): a
// moved message keeps its first-captured file and its recorded fixity while its
// folder over time is tracked. The key and fingerprint are unchanged, so the
// identity index is unaffected. Returns false if key is absent.
func (m *Manifest) MergeFields(key, folder string, seen time.Time, present bool) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.Entries[key]
	if !ok {
		return false
	}
	r.Folder = folder
	r.LastSeen = seen
	r.Present = present
	m.Entries[key] = r
	return true
}

// SetPresent flips only a record's Present flag, leaving Folder/LastSeen and
// every other field untouched; it reports whether the key existed. It is the
// exact undo of a SweepGone flip: the live path calls SetPresent(key, true) when
// the paired {k,gone} history append fails, so the saved manifest (the trailing
// anchor, §3.5) never durably records a 'gone' the log does not have — the
// manifest must never lead the log (#13). The message keeps its prior
// Folder/LastSeen and is re-detected gone (and the event retried) next run.
func (m *Manifest) SetPresent(key string, present bool) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.Entries[key]
	if !ok {
		return false
	}
	r.Present = present
	m.Entries[key] = r
	return true
}

// WalkedFolders is a per-run observed-set: the folder paths a single mailbox
// walk actually visited to completion this run. It is the SCOPE of
// gone-detection (§3.4) — gone is computed only over records whose recorded
// folder is in this set, so a message in an excluded folder (never walked) or in
// a folder a partial run never reached retains its state and is never marked
// gone. One WalkedFolders is built per mailbox walk; Mark records a folder once
// its listing has been fully consumed.
type WalkedFolders struct {
	set map[string]bool
}

// NewWalkedFolders returns an empty per-run observed-set.
func NewWalkedFolders() *WalkedFolders {
	return &WalkedFolders{set: map[string]bool{}}
}

// Mark records that folder was fully walked this run.
func (w *WalkedFolders) Mark(folder string) {
	if w.set == nil {
		w.set = map[string]bool{}
	}
	w.set[folder] = true
}

// Has reports whether folder was walked this run.
func (w *WalkedFolders) Has(folder string) bool { return w != nil && w.set[folder] }

// Len is the number of distinct folders walked this run.
func (w *WalkedFolders) Len() int {
	if w == nil {
		return 0
	}
	return len(w.set)
}

// SweepGone performs gone-detection by full reconciliation (§3.4): after every
// non-excluded folder of the mailbox named by token has been fully walked in a
// run that held its lock, it marks gone every still-Present record of that token
// whose recorded folder was actually walked (walked.Has(Folder)) but which was
// NOT re-observed this run (LastSeen strictly before thisRun — every observation
// stamps LastSeen=thisRun, so a still-present message never qualifies). "Gone"
// means the message left the LIVE mailbox: Present is flipped to false IN PLACE,
// keeping the record's last known Folder and LastSeen so a past view still shows
// where and when it last lived, and the first-captured file is KEPT on disk (a
// timeline event, not a redaction — R13/T5). It returns the swept keys in sorted
// order so the caller appends one {k,gone} history event per key and the events
// are durable before the manifest records Present=false (crash order §3.5).
//
// The `walked` scope is the guard against a false positive: a message in an
// excluded or unwalked folder (walked.Has(Folder) is false) is never swept, and
// a record already gone (Present=false) or observed this run is left untouched.
// It is called ONCE per mailbox, at the end of the full walk — NEVER at a
// checkpoint, which fires mid-walk when a not-yet-walked folder's messages have
// not been re-observed; the caller also skips the sweep entirely when the walk
// did not complete (a folder error, a ctx cancel, or a lost lock), so a
// partial/aborted run marks nothing gone.
func (m *Manifest) SweepGone(token string, walked *WalkedFolders, thisRun time.Time) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	prefix := token + keySeparator
	var gone []string
	for key, r := range m.Entries {
		if !strings.HasPrefix(key, prefix) {
			continue // another mailbox's record — out of this sweep's scope
		}
		if !r.Present || !r.LastSeen.Before(thisRun) {
			continue // already gone, or observed this run
		}
		if !walked.Has(r.Folder) {
			continue // excluded/unwalked folder — retain its last state
		}
		gone = append(gone, key)
	}
	sort.Strings(gone)
	for _, key := range gone {
		r := m.Entries[key]
		r.Present = false // keep the last known Folder/LastSeen; the file stays (R13)
		m.Entries[key] = r
	}
	return gone
}

// KeysForIdentity returns every (key, fingerprint) sharing the message identity
// (Message-ID, or the content-hash identity when absent), so the live path can
// find an already-archived sibling before download (R17) and let a
// move-collapsed record coexist with its #fp-qualified siblings. The result is a
// fresh copy, safe to range while the manifest is separately mutated.
func (m *Manifest) KeysForIdentity(identity string) []IdentRef {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.byIdent == nil {
		m.buildIdentIndexLocked()
	}
	src := m.byIdent[identity]
	out := make([]IdentRef, len(src))
	copy(out, src)
	return out
}

// isIdentity reports whether a key component is a message identity — the value
// model.Message.Identity() returns ("mid:"+Message-ID or "sha:"+content hash).
// It lets CollapseByIdentity tell a LiveKey (identity at parts[1]) from a v3
// folder-scoped key (folder at parts[1], identity at parts[2]) by SHAPE, never by
// NUL/component count (EC4).
func isIdentity(s string) bool {
	return strings.HasPrefix(s, "mid:") || strings.HasPrefix(s, "sha:")
}

// OwesCollapse reports whether the one-time v3→v5 identity-collapse still owes a
// run for a store token (it has not been marked done). A marked token is skipped,
// so steady-state runs pay no collapse scan.
func (m *Manifest) OwesCollapse(token string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return !m.CollapsedTokens[token]
}

// MarkCollapsed records that the identity-collapse has run for the given tokens
// (set after a successful live run whether or not it collapsed anything).
func (m *Manifest) MarkCollapsed(tokens ...string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.CollapsedTokens == nil {
		m.CollapsedTokens = map[string]bool{}
	}
	for _, t := range tokens {
		m.CollapsedTokens[t] = true
	}
}

// CollapseByIdentity performs the one-time v3→v5 fingerprint-safe collapse for
// the given store token(s) (empty = all). It unifies v3 folder-scoped records
// that share a (token, identity) AND an equal non-empty fingerprint into one
// mailbox-wide LiveKey record (a move-duplicate → one copy, R3); distinct-
// fingerprint reuses stay #fp-qualified siblings (R1). It returns the losers (for
// the caller to prune) AND the survivor re-key map (old folder-scoped key → new
// LiveKey), so the caller re-keys the search index to match (EC5). Keys that are
// already LiveKey-shaped (identity at parts[1]) are kept verbatim.
func (m *Manifest) CollapseByIdentity(tokens ...string) ([]CollapseLoss, map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.Entries) == 0 {
		m.Collapsed = 0
		return nil, nil
	}
	// Scope to the mailbox token(s) archived this run (empty = all): a mailbox not
	// walked this run keeps its keys untouched (R3, EC4).
	var tokenSet map[string]bool
	if len(tokens) > 0 {
		tokenSet = make(map[string]bool, len(tokens))
		for _, t := range tokens {
			tokenSet[t] = true
		}
	}

	type member struct {
		oldKey string
		rec    Record
	}
	type gid struct{ token, identity string }

	groups := map[gid][]member{}
	newEntries := make(map[string]Record, len(m.Entries))
	for k, r := range m.Entries {
		// Discriminate the key SHAPE by which component is the message identity
		// (mid:/sha:), NEVER by NUL/component count — a #fp-qualified LiveKey
		// (token\x00identity\x00fp) has the same three components as a v3
		// folder-scoped key (token\x00folder\x00identity) and must NOT be collapsed
		// (EC4). A v3 folder-scoped key carries its identity at parts[2]; a LiveKey
		// family carries it at parts[1].
		parts := strings.Split(k, keySeparator)
		switch {
		case len(parts) >= 2 && isIdentity(parts[1]):
			newEntries[k] = r // v4/v5 LiveKey or #fp-qualified LiveKey — already collapsed.
			continue
		case len(parts) >= 3 && isIdentity(parts[2]):
			if tokenSet != nil && !tokenSet[parts[0]] {
				newEntries[k] = r // a mailbox not archived this run — leave as-is.
				continue
			}
			g := gid{token: parts[0], identity: parts[2]}
			groups[g] = append(groups[g], member{oldKey: k, rec: r})
		default:
			newEntries[k] = r // unrecognised shape — never guess; keep verbatim.
		}
	}

	// Deterministic group order so the collapse is reproducible.
	gids := make([]gid, 0, len(groups))
	for g := range groups {
		gids = append(gids, g)
	}
	sort.Slice(gids, func(i, j int) bool {
		if gids[i].token != gids[j].token {
			return gids[i].token < gids[j].token
		}
		return gids[i].identity < gids[j].identity
	})

	var losses []CollapseLoss
	remap := map[string]string{} // survivor old key → new LiveKey (EC5: re-key the index)
	for _, g := range gids {
		members := groups[g]

		// Partition into fingerprint sub-groups: equal NON-EMPTY fingerprints
		// share a sub-group (same message, e.g. moved between folders); each
		// empty-fingerprint member is its own sub-group (an absent fingerprint
		// cannot prove sameness — never merge it, R1).
		type sub struct {
			fp      string
			members []member
		}
		byFP := map[string]int{}
		var subs []*sub
		for _, mem := range members {
			fp := mem.rec.Fingerprint
			if fp == "" {
				subs = append(subs, &sub{fp: "", members: []member{mem}})
				continue
			}
			if idx, ok := byFP[fp]; ok {
				subs[idx].members = append(subs[idx].members, mem)
			} else {
				byFP[fp] = len(subs)
				subs = append(subs, &sub{fp: fp, members: []member{mem}})
			}
		}

		// One survivor per sub-group.
		type survivor struct {
			fp        string
			rec       Record
			firstKey  string
			firstSeen time.Time
			losers    []member
		}
		var survivors []survivor
		for _, sg := range subs {
			ms := sg.members
			sort.Slice(ms, func(i, j int) bool {
				ti, tj := ms[i].rec.ExportedAt, ms[j].rec.ExportedAt
				if !ti.Equal(tj) {
					return ti.Before(tj)
				}
				return ms[i].oldKey < ms[j].oldKey
			})
			earliest := ms[0]
			latest := ms[0]
			for _, x := range ms[1:] {
				if x.rec.ExportedAt.After(latest.rec.ExportedAt) {
					latest = x
				}
			}
			rec := earliest.rec // first file wins: keeps Path/Fixity/Fingerprint/completeness
			rec.FirstFolder = earliest.rec.Folder
			rec.FirstSeen = earliest.rec.ExportedAt
			rec.Folder = latest.rec.Folder
			rec.LastSeen = latest.rec.ExportedAt
			rec.Present = true
			survivors = append(survivors, survivor{
				fp: sg.fp, rec: rec, firstKey: earliest.oldKey,
				firstSeen: earliest.rec.ExportedAt, losers: ms[1:],
			})
		}

		// The earliest survivor takes the base live key; the rest are its
		// #fp-qualified siblings (distinct messages that reused the identity).
		sort.Slice(survivors, func(i, j int) bool {
			if !survivors[i].firstSeen.Equal(survivors[j].firstSeen) {
				return survivors[i].firstSeen.Before(survivors[j].firstSeen)
			}
			return survivors[i].firstKey < survivors[j].firstKey
		})
		base := LiveKey(g.token, g.identity)
		for i := range survivors {
			s := &survivors[i]
			nk := base
			if i > 0 {
				if s.fp != "" {
					nk = Qualify(base, s.fp)
				} else {
					// An empty-fingerprint non-first survivor cannot be #fp-split;
					// qualify by a deterministic token of its own key so it still
					// survives under a unique key (no drop). Pathological — a genuine
					// v3 archive fingerprints every record.
					nk = Qualify(base, util.HashHex(s.firstKey, 8))
				}
			}
			newEntries[nk] = s.rec
			if s.firstKey != nk {
				remap[s.firstKey] = nk // the survivor's index row must follow the re-key
			}
			for _, l := range s.losers {
				losses = append(losses, CollapseLoss{
					LoserKey: l.oldKey, LoserPath: l.rec.Path, Folder: l.rec.Folder,
					ExportedAt: l.rec.ExportedAt, SurvivorKey: nk,
				})
			}
		}
	}

	m.Entries = newEntries
	m.byPath = nil // export paths unchanged, but the reverse map is rebuilt lazily
	m.buildIdentIndexLocked()
	m.Collapsed = len(losses)
	return losses, remap
}

// identityOf returns the second NUL-delimited component of a manifest key: the
// message identity of a live-path key (token\x00identity[\x00fp]) — and the
// folder of a folder-scoped local key (token\x00folder\x00identity[\x00fp]),
// which is harmless noise in the mailbox-wide index because that index is
// consulted only on the live path. Empty for a key with no separator.
func identityOf(key string) string {
	i := strings.IndexByte(key, keySeparator[0])
	if i < 0 {
		return ""
	}
	rest := key[i+1:]
	if j := strings.IndexByte(rest, keySeparator[0]); j >= 0 {
		return rest[:j]
	}
	return rest
}

// buildIdentIndexLocked rebuilds the mailbox-wide identity index from Entries.
// The caller either holds m.mu or is Load (single-threaded construction).
func (m *Manifest) buildIdentIndexLocked() {
	m.byIdent = make(map[string][]IdentRef, len(m.Entries))
	for k, r := range m.Entries {
		id := identityOf(k)
		if id == "" {
			continue
		}
		m.byIdent[id] = append(m.byIdent[id], IdentRef{Key: k, Fingerprint: r.Fingerprint})
	}
}

// identUpsertLocked records (key, fp) in the identity index, replacing any prior
// fingerprint for the same key. The caller holds m.mu.
func (m *Manifest) identUpsertLocked(key, fp string) {
	if m.byIdent == nil {
		m.buildIdentIndexLocked()
	}
	id := identityOf(key)
	if id == "" {
		return
	}
	lst := m.byIdent[id]
	for i := range lst {
		if lst[i].Key == key {
			lst[i].Fingerprint = fp
			return
		}
	}
	m.byIdent[id] = append(lst, IdentRef{Key: key, Fingerprint: fp})
}

// identRemoveLocked drops key from the identity index. The caller holds m.mu.
func (m *Manifest) identRemoveLocked(key string) {
	if m.byIdent == nil {
		return
	}
	id := identityOf(key)
	if id == "" {
		return
	}
	lst := m.byIdent[id]
	for i := range lst {
		if lst[i].Key == key {
			m.byIdent[id] = append(lst[:i], lst[i+1:]...)
			if len(m.byIdent[id]) == 0 {
				delete(m.byIdent, id)
			}
			return
		}
	}
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
