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
const manifestVersion = 6

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
	// BodyFallback (informational, additive) names why the page shows the
	// mail's HTML as escaped source instead of rendering it (export.Fallback*):
	// the content is all there, as text, and every inline image is in the zip.
	BodyFallback string `json:"body_fallback,omitempty"`
	Subject      string `json:"subject,omitempty"`
	Date         string `json:"date,omitempty"` // RFC 3339 UTC

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

	// PhysID (version 6) is the source's per-PHYSICAL-message discriminator — the
	// Graph immutable id — a capability-gated HINT for the pre-download skip and a
	// distinctness tie-breaker (a distinct message that reuses an already-archived
	// Message-ID has a different PhysID). Empty for local imports, for a message
	// whose provider withheld the immutable id, and for any record captured before
	// v6. It is NEVER part of the message identity or fingerprint, and the content
	// fingerprint remains the sole adopt-vs-split arbiter — PhysID only breaks a tie
	// and lets a same-envelope distinct reuse be told apart (design closure rev-6).
	PhysID string `json:"phys_id,omitempty"`

	// ContentHash (version 6) is the body-inclusive content digest
	// (model.Message.ContentDigest) recorded on a live-path record so the PhysID
	// closure can tell a distinct Message-ID reuse (different content) from a churn
	// (same content, reissued immutable id) WITHOUT the original .eml — closing #8 on
	// a default archive, not only a -raw one (design closure rev-6.1). Empty on a
	// pre-v6/floor record and on folder-scoped local records; the closure engages
	// only where it is present, so an archive with no ContentHash stays the shipped
	// Message-ID floor. It is NOT part of the identity, key, or Fingerprint.
	ContentHash string `json:"chash,omitempty"`

	// AltPhysIDs (version 6) are ADDITIONAL content-equal immutable ids for this
	// record beyond the primary PhysID — the ids of other simultaneously-live copies
	// of the same message (same Message-ID, identical content, distinct Graph ids
	// from a copy-to-folder) and any reissued id (a migration). The live path skips
	// a listing entry whose id is the primary OR any AltPhysIDs entry, so a copied
	// message is not re-downloaded every run (design closure rev-6.2). Like PhysID it
	// is NEVER part of the identity, key, or Fingerprint.
	AltPhysIDs []string `json:"alt_phys_ids,omitempty"`

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

	// Locator (additive; fault-isolation §4.2, FC6(3)) names where a partly-read
	// item sat in its store (pst-nid:<store-uid>/<nid>, maildir:<folder>/<unique>),
	// so a later healthy read of the same item — which may land on another key —
	// finds and supersedes it, and a recovery never matches across stores. It is
	// set ONLY on a record that carries an unreadable:* label, so a healthy
	// input's records are byte-identical to before (P14). Load drops it from any
	// other record, and a re-examine restores one an older binary's save lost.
	Locator string `json:"loc,omitempty"`
	// RecoveredBy (additive) is the key of the record that replaced this partly-read
	// one (§4.7.4): its unreadable:* labels became the info label "superseded" and
	// nothing was written or deleted. Load drops one that names no existing key.
	RecoveredBy string `json:"recovered_by,omitempty"`

	// ParseScheme (additive; design-mime-fidelity §4.10, MC12) is the parser scheme
	// the record was last written or re-examined under. 0 means a build from before
	// the scheme existed wrote it and none has re-read it since; an older binary's
	// save drops the field, so the next run by this build stamps it again in memory
	// (StampParseScheme) at no cost to the files. Nothing acts on it before the repair
	// slice: it is the migration's progress marker. The exporter stamps it on every
	// write and, in memory, on every re-read (MA-329's golden carries it since the
	// owner's decision of 2026-10-11).
	ParseScheme int `json:"parse_scheme,omitempty"`

	// ParserGen and RawDigest (additive; fault-isolation §4.2, FC25) stamp an unparsed
	// stub, the record of a message whose MIME parse gave up: ParserGen is the parser
	// generation that failed on it (unparsed is terminal FOR THAT GENERATION: same
	// bytes, same parser, same result, so a newer parser re-examines it once), and
	// RawDigest the sha-256 (hex) of its raw bytes, which a later healthy record of the
	// same bytes finds the stub by (it is superseded, metadata only). Only an unparsed
	// record carries them: a healthy input's records are byte-identical to before
	// (P14), Load drops them from any other, and an older binary's save drops them
	// from a stub, which the next run reads as generation 0 and re-examines once.
	ParserGen int    `json:"parser_gen,omitempty"`
	RawDigest string `json:"raw_sha256,omitempty"`

	// FidelityFallback (additive; design-mime-fidelity MC19) is the parse scheme under
	// which this record was written from v1's content because the fidelity parser
	// panicked or held less than v1 did. A later scheme may parse the message, so the
	// repair slice re-examines it; the count of such records is what status shows.
	FidelityFallback int `json:"fidelity_fallback,omitempty"`
}

// ParserGenCurrent is the parser generation this build writes on an unparsed stub. It
// is the parse scheme: a build that changes how a message is parsed bumps the scheme,
// and so re-examines every stub the older parser gave up on, once (FC25).
const ParserGenCurrent = ParseSchemeCurrent

// validDigest says s is a lower-case hex sha-256.
func validDigest(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// StaleUnparsed reports whether the record is an unparsed stub that an older parser
// generation than gen gave up on: a run re-examines it once (FC25). A stub no binary
// stamped (an older binary's save dropped the field) is generation 0.
func (r Record) StaleUnparsed(gen int) bool {
	return r.hasLabel(LabelUnparsed) && r.ParserGen < gen
}

// IsLegacyPlaceholder reports whether the record is an older build's collapsed
// "(unreadable message)" record, re-classed to the info label legacy-placeholder
// (FC27): it stands for no message in particular.
func (r Record) IsLegacyPlaceholder() bool { return r.hasLabel(LabelLegacyPlaceholder) }

// ParseSchemeCurrent is the parser scheme this build writes and stamps. Slice S1 of
// design-mime-fidelity bumps it to 1: it routes identity, stems and ContentHash but
// registers no fidelity note, so no record is yet "damaged" by it.
//
// Slice S2 introduces the first fidelity notes, under ParseSchemeFidelity, and should
// move this to 2. It does not: MA-329's golden (internal/app/testdata/p14-golden.json)
// holds parse_scheme 1 on its 46 records, and moving the stamp is a baseline change that
// is the owner's to accept. Until it moves, a record written under this build carries
// 1, and a re-read that shows a note newer than that leaves it unstamped (examinedBy), so
// the repair slice sees it as not yet examined.
const ParseSchemeCurrent = 1

// ParseSchemeFidelity is the parse scheme that introduced slice S2's fidelity notes
// (design-mime-fidelity §4.10): every note it registers carries it as Since.
const ParseSchemeFidelity = 2

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
	return len(r.Missing) > 0 || len(r.Terminal) > 0 || len(r.Unresolved) > 0 || r.BodyFallback != ""
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

	// CaptureGaps is the capture-gap ledger: the items and folders a source holds
	// that no record represents (gaps.go). INERT: never consulted by any skip,
	// fetch or export decision (P5); not part of Entries, byIdent or byPath, so its
	// keys can never collide with a record key. Additive and omitted when empty, so
	// the manifest version stays 6 (J2): an older binary ignores it and drops it on
	// its next save, which loses diagnostics only (§6).
	CaptureGaps map[string]CaptureGap `json:"capture_gaps,omitempty"`

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

	// byLocator is a lazily built reverse index (token\x00locator → key) over
	// exactly the records that carry an unreadable:* label and a Locator — a
	// handful, so a lookup per exported message costs nothing on a healthy input.
	byLocator map[string]string
	// byRawDigest is the same for unparsed stubs: token\x00raw digest → key, built
	// lazily over the records that carry both the label and a digest (FC25).
	byRawDigest map[string]string

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
	PhysID      string // the record's Graph immutable id (version 6); empty when withheld/local (see Record.PhysID)
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

// snapshotStoreID is migrateStoreID for a manifest read as a snapshot (LoadFrom): the id is the
// lexical form and the file system is never asked about it. A manifest handed over in another
// archive folder is data, and a store id in it is text its writer chose: resolved with
// filepath.EvalSymlinks it would make this machine Lstat that path (on Windows a UNC id opens an
// SMB connection and offers the owner's credentials) and follow a link out of the folder
// (E4-AO1). The snapshot's only uses of an id are its prefix and its ".pst"/".ost" suffix.
func snapshotStoreID(old string) string {
	if strings.HasPrefix(old, "path:") || strings.HasPrefix(old, "mailbox:") {
		return old
	}
	if strings.Contains(old, "@") && !filepath.IsAbs(old) {
		return MailboxSourceID(old)
	}
	p := filepath.Clean(old)
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		p = strings.ToLower(p)
	}
	return "path:" + p
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
	seg := tokenSegment(store)
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

// tokenSegment is the store token's first choice for a store's display name: its
// sanitized segment, which must also pass SafeToken, because the exporter refuses
// (run-fatal) a store whose token does not. util.SanitizeSegment keeps DEL
// (U+007F), which SafeToken refuses, and the name of a store comes from the data
// file itself, so a name with one DEL byte would otherwise end every run at that
// input (S2A-AO1). Such a name loses its control bytes and gets a hash of the
// original name, so it never merges with a plain name that spells the same.
func tokenSegment(store string) string {
	seg := util.SanitizeSegment(store)
	if SafeToken(seg) {
		return seg
	}
	stripped := util.SanitizeSegment(util.StripControl(store))
	if SafeToken(stripped) {
		return stripped + "~" + util.ShortHash(store)
	}
	return "store~" + util.ShortHash(store)
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
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return newManifest(path), nil
	}
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	return parseManifest(path, path, data)
}

// LoadFrom parses the bytes of the manifest called name, which the caller read itself
// (through an os.Root on the archive, under a size cap, so that a concurrent writer's
// rename cannot fail the read on Windows and a hostile folder cannot make it read
// without bound). The result is a snapshot: it is bound to no path, so Save refuses,
// and every migration Load makes stays in memory (the extraction workspace's second
// archive source writes nothing in the folder it reads, design §4.3).
func LoadFrom(name string, data []byte) (*Manifest, error) {
	return parseManifest("", name, data)
}

func newManifest(path string) *Manifest {
	return &Manifest{path: path, Version: manifestVersion, LoadedVersion: manifestVersion, Entries: map[string]Record{}, byIdent: map[string][]IdentRef{}}
}

// NewerFormatError is the refusal of a manifest a newer mailarchive wrote.
type NewerFormatError struct {
	Name       string // the manifest as the caller named it
	Version    int    // the format version it holds
	Understood int    // the version this build understands
}

func (e *NewerFormatError) Error() string {
	return fmt.Sprintf("manifest %s was written by a newer mailarchive (format version %d; this build understands %d): upgrade this copy of mailarchive, or point -out at an archive this version wrote", e.Name, e.Version, e.Understood)
}

// parseManifest is Load's work on the bytes data of the manifest called name, bound to path.
func parseManifest(path, name string, data []byte) (*Manifest, error) {
	m := newManifest(path)
	if len(data) == 0 {
		return m, nil
	}
	if err := json.Unmarshal(data, m); err != nil {
		return nil, fmt.Errorf("manifest %s is corrupt or truncated (%v): restore it from a backup, or delete it to re-export everything (the archived files are untouched; incremental runs will rewrite them in place)", name, err)
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
		return nil, &NewerFormatError{Name: name, Version: m.Version, Understood: manifestVersion}
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
			var id string
			if path == "" { // a snapshot (LoadFrom): nothing in the manifest is looked up on disk
				id = snapshotStoreID(old)
			} else {
				id = migrateStoreID(old)
			}
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
	// The ledger and the partly-read records' fields are untrusted on read, like
	// the last-run record (MA-157): the ledger is bounded, stripped and checked
	// against Stores; a Locator that no unreadable:* label owns and a RecoveredBy
	// that names no record are dropped (§4.2).
	m.sanitizeGaps(time.Now().UTC())
	for key, r := range m.Entries {
		changed := false
		if r.Locator != "" && !r.hasUnreadable() {
			r.Locator, changed = "", true
		}
		if r.RecoveredBy != "" {
			if _, ok := m.Entries[r.RecoveredBy]; !ok {
				r.RecoveredBy, changed = "", true
			}
		}
		// The parser stamp and the raw digest belong to an unparsed stub only (FC25):
		// elsewhere they are stray, and a digest that is no sha-256 is not one.
		if !r.hasLabel(LabelUnparsed) && (r.ParserGen != 0 || r.RawDigest != "") {
			r.ParserGen, r.RawDigest, changed = 0, "", true
		}
		if r.RawDigest != "" && !validDigest(r.RawDigest) {
			r.RawDigest, changed = "", true
		}
		if changed {
			m.Entries[key] = r
		}
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

// HasToken reports whether any record is filed under the store token: an archive
// that holds its tree on disk. The local run asks it when it probes whether the
// archive location is still there (FC12).
func (m *Manifest) HasToken(token string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	prefix := token + keySeparator
	for k := range m.Entries {
		if strings.HasPrefix(k, prefix) {
			return true
		}
	}
	return false
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
	m.identUpsertLocked(key, r.Fingerprint, r.PhysID)
	if m.byLocator != nil {
		if old, ok := m.Entries[key]; ok {
			m.locatorDropLocked(key, old)
		}
		m.locatorAddLocked(key, r)
	}
	if m.byRawDigest != nil {
		if old, ok := m.Entries[key]; ok {
			m.digestDropLocked(key, old)
		}
		m.digestAddLocked(key, r)
	}
	m.Entries[key] = r
}

// digestAddLocked indexes r when it is an unparsed stub with a raw digest. The caller
// holds m.mu and m.byRawDigest is built.
func (m *Manifest) digestAddLocked(key string, r Record) {
	if r.RawDigest != "" && r.hasLabel(LabelUnparsed) {
		m.byRawDigest[locatorIndexKey(key, r.RawDigest)] = key
	}
}

// digestDropLocked removes old's index entry when it points at key.
func (m *Manifest) digestDropLocked(key string, old Record) {
	if old.RawDigest == "" {
		return
	}
	dk := locatorIndexKey(key, old.RawDigest)
	if m.byRawDigest[dk] == key {
		delete(m.byRawDigest, dk)
	}
}

// buildDigestIndexLocked builds byRawDigest on first use. The caller holds m.mu.
func (m *Manifest) buildDigestIndexLocked() {
	if m.byRawDigest != nil {
		return
	}
	m.byRawDigest = map[string]string{}
	for k, r := range m.Entries {
		m.digestAddLocked(k, r)
	}
}

// HasUnparsedStubs reports whether the manifest holds any unparsed stub with a raw
// digest, so a caller does not hash the bytes of every healthy message of an archive
// that has none (the index is built once; this is a map length).
func (m *Manifest) HasUnparsedStubs() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.buildDigestIndexLocked()
	return len(m.byRawDigest) > 0
}

// SupersedeUnparsed settles an unparsed stub whose bytes a later capture parsed whole
// (§4.7.4(4), FC25): the same-token stub with this RawDigest has its unparsed label
// re-classed to the info label "superseded" and RecoveredBy set to newKey, in one
// record update. Like SupersedePartial it is METADATA ONLY: no file is written,
// rendered or deleted. It never touches the record at newKey itself, and reports
// whether a stub was superseded.
func (m *Manifest) SupersedeUnparsed(token, digest, newKey string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.buildDigestIndexLocked()
	key, ok := m.byRawDigest[token+keySeparator+digest]
	if !ok || key == newKey {
		return false
	}
	r, ok := m.Entries[key]
	if !ok || !r.hasLabel(LabelUnparsed) {
		return false
	}
	m.digestDropLocked(key, r)
	r.Missing = nil
	r.Terminal = []string{LabelSuperseded}
	r.RecoveredBy, r.ParserGen, r.RawDigest = newKey, 0, ""
	m.Entries[key] = r
	return true
}

// StampParserGen records that this run re-examined the unparsed stub at key under
// parser generation gen and it still could not be parsed: the generation is raised to
// gen (never lowered), so the stub is not re-examined again until the parser changes
// (FC25). It reports whether the record changed.
func (m *Manifest) StampParserGen(key string, gen int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.Entries[key]
	if !ok || !r.hasLabel(LabelUnparsed) || r.ParserGen >= gen {
		return false
	}
	r.ParserGen = gen
	m.Entries[key] = r
	return true
}

// locatorIndexKey is the byLocator key of a record under key: the store token
// (the key's first component) and the record's locator.
func locatorIndexKey(key, locator string) string {
	tok := key
	if i := strings.IndexByte(key, keySeparator[0]); i >= 0 {
		tok = key[:i]
	}
	return tok + keySeparator + locator
}

// locatorAddLocked indexes r when it is a partly-read record with a Locator. The
// caller holds m.mu and m.byLocator is built.
func (m *Manifest) locatorAddLocked(key string, r Record) {
	if r.Locator != "" && r.hasUnreadable() {
		m.byLocator[locatorIndexKey(key, r.Locator)] = key
	}
}

// locatorDropLocked removes old's index entry when it points at key. The caller
// holds m.mu and m.byLocator is built.
func (m *Manifest) locatorDropLocked(key string, old Record) {
	if old.Locator == "" {
		return
	}
	lk := locatorIndexKey(key, old.Locator)
	if m.byLocator[lk] == key {
		delete(m.byLocator, lk)
	}
}

// KeyForLocator returns the key of the same-token partly-read record (one that
// carries an unreadable:* label) whose Locator is locator, if any (§4.7.4). The
// reverse index is built on first use and covers exactly those records, so a
// lookup per exported message is a map hit. A locator never matches across
// tokens, and a PST locator carries its store's uid, so it never matches across
// two stores either (FC8).
func (m *Manifest) KeyForLocator(token, locator string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if locator == "" {
		return "", false
	}
	if m.byLocator == nil {
		m.byLocator = map[string]string{}
		for k, r := range m.Entries {
			m.locatorAddLocked(k, r)
		}
	}
	k, ok := m.byLocator[token+keySeparator+locator]
	return k, ok
}

// SupersedePartial settles a partly-read record whose item a later capture
// stored under another key (§4.7.4(3)): the same-token record with this Locator
// that carries an unreadable:* label has its gaps re-classed to the info
// label "superseded" and RecoveredBy set to newKey, in one record update. It is
// METADATA ONLY: no file is written, rendered or deleted, so nothing can mismatch
// Fixity and there is no crash window; the Locator is cleared because the record
// no longer carries an unreadable label. It reports whether a record was
// superseded (never the record at newKey itself — that one is healed in place).
func (m *Manifest) SupersedePartial(token, locator, newKey string) bool {
	key, ok := m.KeyForLocator(token, locator)
	if !ok || key == newKey {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.Entries[key]
	if !ok || !r.hasUnreadable() {
		return false
	}
	m.locatorDropLocked(key, r)
	// The record is settled as a whole: every gap it carried (the unreadable:*
	// labels and the "body" gap a body that could not be read leaves beside them)
	// is moot once the item sits whole under newKey, so its one label is
	// "superseded", of class info. The page stays; it is no longer "missing".
	r.Missing = nil
	r.Terminal = []string{LabelSuperseded}
	r.RecoveredBy, r.Locator = newKey, ""
	m.Entries[key] = r
	return true
}

// SetLocator restores the Locator an older binary's save dropped from a partly-
// read record (§4.2: any re-examine restores it from the capture); it is a no-op
// on a record that does not carry an unreadable:* label or already has one.
func (m *Manifest) SetLocator(key, locator string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.Entries[key]
	if !ok || locator == "" || r.Locator != "" || !r.hasUnreadable() {
		return false
	}
	r.Locator = locator
	m.Entries[key] = r
	if m.byLocator != nil {
		m.locatorAddLocked(key, r)
	}
	return true
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
// and unknown (legacy sentinels). A partly-read record (an unreadable:* label) is
// fillable, as it is in the record's Missing list; a Terminal label of class info
// (superseded) is nothing missing and counts as neither. The classes come from
// GapClass, the single owner of record gap labels (J5, FC26): MIME fidelity's and
// re-capture's labels join it as new cases, never as a second rewrite of this
// function.
func (m *Manifest) Counts() (fillable, terminal, unknown int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.Entries {
		switch {
		case r.Unknown():
			unknown++
		case r.Fillable():
			fillable++
		case r.terminalGap():
			terminal++
		}
	}
	return
}

// UnreadableCount returns how many records are partly read: they carry an
// unreadable:* label (§4.10). They are fillable records, but status reports them
// under their own reason with their own remedy, never as "missing content".
func (m *Manifest) UnreadableCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, r := range m.Entries {
		if r.hasUnreadable() {
			n++
		}
	}
	return n
}

// UnreadableOnlyCount returns how many records are fillable only because they
// are partly read: every fillable label they carry is an unreadable:* one. These
// are the records status leaves out of its incomplete_content count and the
// run summary out of "still missing content".
func (m *Manifest) UnreadableOnlyCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, r := range m.Entries {
		if !r.Fillable() || r.Unknown() {
			continue
		}
		only := true
		for _, l := range r.Missing {
			if !IsUnreadableLabel(l) {
				only = false
				break
			}
		}
		if only {
			n++
		}
	}
	return n
}

// UnparsedCount returns how many records are unparsed stubs (§4.1): their MIME
// parse panicked or fell back, and the bytes are kept as text.
func (m *Manifest) UnparsedCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, r := range m.Entries {
		if r.hasLabel(LabelUnparsed) {
			n++
		}
	}
	return n
}

// UnparsedTerminalCount returns how many records Counts() puts in its terminal
// tally only because they are unparsed stubs: not fillable, not legacy, and no
// other terminal label of class terminal. An unparsed message is kept as raw text,
// so status leaves these out of "source-empty (never fillable)" (friction F7).
func (m *Manifest) UnparsedTerminalCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, r := range m.Entries {
		if r.Unknown() || r.Fillable() || !r.hasLabel(LabelUnparsed) || !r.terminalGap() {
			continue
		}
		only := true
		for _, l := range r.Terminal {
			if l != LabelUnparsed && !IsParseGapLabel(l) && RecordGapClass(l, true) == ClassTerminal {
				only = false
				break
			}
		}
		if only {
			n++
		}
	}
	return n
}

// UndecodableCount returns how many records carry at least one parse gap (§4.9): a part
// this version could not decode, or one cut short, kept as received. It is informational
// like the terminal count: it never makes a run partial. Counts() leaves such a record
// out of terminal when every terminal label it has is a parse gap, so status can say
// "cannot decode" instead of "source-empty".
func (m *Manifest) UndecodableCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, r := range m.Entries {
		if r.hasParseGap() {
			n++
		}
	}
	return n
}

// FidelityFallbackCount returns how many records were written from v1's content because
// the fidelity parser failed or held less (MC19), from the additive fidelity_fallback
// field. status -json reports it as fidelity_fallback.
func (m *Manifest) FidelityFallbackCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, r := range m.Entries {
		if r.FidelityFallback > 0 {
			n++
		}
	}
	return n
}

// PartlyReadPaths returns the page paths (relative to the archive, control-
// stripped, sorted) of up to limit partly-read records, so status can name them
// (friction F7).
func (m *Manifest) PartlyReadPaths(limit int) []string {
	m.mu.Lock()
	var out []string
	for _, r := range m.Entries {
		if r.hasUnreadable() {
			out = append(out, util.StripControl(r.Path))
		}
	}
	m.mu.Unlock()
	sort.Strings(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// FallbackCounts returns, per reason, how many records show their page as
// source (RV-10), from the additive body_fallback field. Records without one
// (rendered as HTML, or written before the field) count nothing; nil when none.
func (m *Manifest) FallbackCounts() map[string]int {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out map[string]int
	for _, r := range m.Entries {
		if r.BodyFallback == "" {
			continue
		}
		if out == nil {
			out = map[string]int{}
		}
		out[r.BodyFallback]++
	}
	return out
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
	if m.byLocator != nil {
		if old, ok := m.Entries[key]; ok {
			m.locatorDropLocked(key, old)
		}
	}
	if m.byRawDigest != nil {
		if old, ok := m.Entries[key]; ok {
			m.digestDropLocked(key, old)
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

// StampParseScheme records, in memory, that this run re-read the record at key under
// parser scheme: it raises ParseScheme to scheme and never lowers it, and touches
// nothing else, so no file is written for it (MC12). It reports whether the record
// changed; the existing checkpoint saves it.
func (m *Manifest) StampParseScheme(key string, scheme int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.Entries[key]
	if !ok || r.ParseScheme >= scheme {
		return false
	}
	r.ParseScheme = scheme
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

// TouchSeen advances a record's LastSeen to seen WITHOUT changing its Folder or
// Present. The live path calls it on EVERY same-identity same-token sibling of an
// observed message: a single listing entry cannot say WHICH #fp-qualified sibling
// of a reused Message-ID it is, so all are stamped as observed this run, else an
// un-stamped but still-present sibling would be phantom-marked gone by SweepGone
// (EC5, R21). It never resurrects a record already gone (Present untouched), and
// leaves the folder to the primary record the observation actually matched.
func (m *Manifest) TouchSeen(key string, seen time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.Entries[key]
	if !ok {
		return false
	}
	r.LastSeen = seen
	m.Entries[key] = r
	return true
}

// SameStoreIdentityCount returns how many records of the given store token carry
// the identity. The exporter uses it to decide whether an adopt-never-split
// against a legacy-scheme record is UNAMBIGUOUS (exactly one record for the
// store+identity — the ordinary single-message migration, adopt) or must split
// instead (a distinct Message-ID reuse survived collapse, so adopting could
// overwrite the wrong physical message — R1; split to a #fp-qualified key, a
// bounded duplicate, never a drop).
func (m *Manifest) SameStoreIdentityCount(store, identity string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.byIdent == nil {
		m.buildIdentIndexLocked()
	}
	prefix := store + keySeparator
	n := 0
	for _, ref := range m.byIdent[identity] {
		if strings.HasPrefix(ref.Key, prefix) {
			n++
		}
	}
	return n
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
	return m.SweepGoneSparing(token, walked, thisRun, SweepSpare{})
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

// AddPhysID records id as one of the record's content-equal immutable ids and
// returns true when it changed the record (design closure rev-6.2). If the record
// has no primary PhysID yet, id becomes the primary (and is stamped into the
// identity index); otherwise, if id is not already known, it is appended to
// AltPhysIDs. So ONE mailbox-wide record can represent SEVERAL simultaneously-live
// copies of a message — same Message-ID, identical content, distinct Graph immutable
// ids (a copy filed into a second folder) — or a reissued id (a restore/migration),
// WITHOUT flipping a single slot (which made each copy re-download and the id flap
// every run). A no-op (empty id, unknown key, or id already known) returns false. It
// NEVER touches identity or fingerprint.
func (m *Manifest) AddPhysID(key, id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if id == "" {
		return false
	}
	r, ok := m.Entries[key]
	if !ok || recordHasPhysID(r, id) {
		return false
	}
	if r.PhysID == "" {
		r.PhysID = id
		m.Entries[key] = r
		if m.byIdent == nil {
			m.buildIdentIndexLocked()
		} else {
			bid := identityOf(key)
			for i := range m.byIdent[bid] {
				if m.byIdent[bid][i].Key == key {
					m.byIdent[bid][i].PhysID = id
					break
				}
			}
		}
		return true
	}
	r.AltPhysIDs = append(r.AltPhysIDs, id)
	m.Entries[key] = r
	return true
}

// HasPhysID reports whether the record at key carries id as one of its content-equal
// immutable ids (the primary PhysID or an AltPhysIDs entry).
func (m *Manifest) HasPhysID(key, id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.Entries[key]
	if !ok {
		return false
	}
	return recordHasPhysID(r, id)
}

// recordHasPhysID reports whether id is a non-empty immutable id of r (primary or
// alternate). The caller holds m.mu (or owns r).
func recordHasPhysID(r Record, id string) bool {
	if id == "" {
		return false
	}
	if r.PhysID == id {
		return true
	}
	for _, a := range r.AltPhysIDs {
		if a == id {
			return true
		}
	}
	return false
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
// sameContent (optional): reports whether two archived files (paths relative to
// -out) are byte-identical. The collapse merges same-fingerprint records only
// when it says yes — because the Fingerprint excludes bodies, two DISTINCT
// messages that reused one Message-ID with an identical envelope hash equal, and
// merging them would drop one from the manifest and index (R1). A nil callback
// (a caller with no file access, e.g. a unit test on synthetic fingerprints)
// merges by fingerprint alone.
func (m *Manifest) CollapseByIdentity(sameContent func(relA, relB string) bool, tokens ...string) ([]CollapseLoss, map[string]string) {
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
		case len(parts) >= 3 && isIdentity(parts[2]):
			// v3 folder-scoped key (token\x00folder\x00identity, optionally #fp at
			// parts[3]): the identity is at parts[2]. Tested FIRST — a #fp-qualified
			// LiveKey has a 16-hex fingerprint at parts[2] (never identity-shaped), so
			// this is unambiguous AND correctly migrates a v3 key even when its FOLDER
			// is literally named "mid:…"/"sha:…" (which would fool the parts[1] test).
			if tokenSet != nil && !tokenSet[parts[0]] {
				newEntries[k] = r // a mailbox not archived this run — leave as-is.
				continue
			}
			g := gid{token: parts[0], identity: parts[2]}
			groups[g] = append(groups[g], member{oldKey: k, rec: r})
		case len(parts) >= 2 && isIdentity(parts[1]):
			newEntries[k] = r // v4/v5 LiveKey or #fp-qualified LiveKey — already collapsed.
			continue
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
		var subs []*sub
		for _, mem := range members {
			fp := mem.rec.Fingerprint
			if fp == "" {
				// An empty fingerprint proves nothing — never merge it (R1).
				subs = append(subs, &sub{fp: "", members: []member{mem}})
				continue
			}
			// Join an existing same-fingerprint sub-group ONLY when the on-disk
			// files are byte-identical (a real move-duplicate). Two distinct
			// messages that reused one Message-ID with an identical envelope hash to
			// the same fingerprint (bodies are excluded); a content check keeps them
			// as separate siblings instead of dropping one (R1).
			placed := false
			for _, s := range subs {
				if s.fp != fp {
					continue
				}
				if sameContent == nil || sameContent(s.members[0].rec.Path, mem.rec.Path) {
					s.members = append(s.members, mem)
					placed = true
					break
				}
			}
			if !placed {
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
				nk = Qualify(base, s.fp)
				if _, taken := newEntries[nk]; s.fp == "" || taken {
					// Either an empty fingerprint (cannot #fp-split), or a same-
					// fingerprint COLLISION — two distinct-content messages that reused
					// one Message-ID with an identical envelope. Qualify by a
					// deterministic hash of the record's own key so BOTH survive under
					// unique keys (never drop one — R1).
					nk = Qualify(base, util.HashHex(s.firstKey, 8))
				}
			}
			// Record each loser's kept-on-disk file on the survivor (R13: the loser
			// copy is NOT deleted now), so a later redaction reaches every copy of
			// the message, not just the survivor's (rev-4 §6, #4).
			for _, l := range s.losers {
				s.rec.AlsoFiles = append(s.rec.AlsoFiles, l.rec.Path)
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
	m.byLocator = nil
	m.byRawDigest = nil
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
		m.byIdent[id] = append(m.byIdent[id], IdentRef{Key: k, Fingerprint: r.Fingerprint, PhysID: r.PhysID})
	}
}

// identUpsertLocked records (key, fp, physID) in the identity index, replacing any
// prior fingerprint AND PhysID for the same key. The caller holds m.mu.
func (m *Manifest) identUpsertLocked(key, fp, physID string) {
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
			lst[i].PhysID = physID
			return
		}
	}
	m.byIdent[id] = append(lst, IdentRef{Key: key, Fingerprint: fp, PhysID: physID})
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
// cannot corrupt an existing manifest. The rename waits out a moment of Windows
// file sharing (replaceFile) before it reports failure.
func (m *Manifest) Save() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.path == "" {
		return errors.New("this manifest is a read-only snapshot (LoadFrom) and has no file to save to")
	}

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
	if err := replaceFile(tmpName, m.path); err != nil {
		return fmt.Errorf("replace manifest: %w", err)
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync() // best effort: not every filesystem supports fsync on a directory
		d.Close()
	}
	return nil
}
