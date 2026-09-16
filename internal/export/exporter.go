// Package export renders normalized messages to self-contained HTML files with
// per-email attachment archives, mirroring the source folder tree and tracking
// exported items for incremental runs.
package export

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/util"
)

// Mode selects how the manifest is consulted.
type Mode int

const (
	// Incremental skips any message already recorded in the manifest.
	Incremental Mode = iota
	// Full re-exports every message regardless of the manifest.
	Full
)

// Stats accumulates a per-run summary.
type Stats struct {
	Exported            int
	SkippedManifest     int
	SkippedDate         int
	Attachments         int // attachment files written into zips
	AttachmentsInline   int // inline images embedded into the HTML as data URIs
	AttachmentsEmpty    int // declared attachments that produced zero bytes (e.g. not downloaded)
	AttachmentErrors    int // attachments whose stream failed mid-read (torn fetch); reported, not archived
	UnresolvedInlineRef int // cid: references in the HTML with no matching image present
	NonHTMLBodies       int // messages exported from plain/RTF because no HTML body existed
	NoBody              int // messages exported with no body content at all
	RawWritten          int // original .eml files written (KeepRaw; 0 for sources with no original bytes, e.g. Outlook items)
	RawAvailable        int // exported messages that HAD original bytes available, whether or not KeepRaw wrote them; >0 without KeepRaw means the source is raw-capable but no .eml was preserved (extract will find nothing — PC15)

	// Incremental completeness (R1/R2): fillable gaps are re-examined.
	Retried         int // seen-but-fillable records re-examined this run
	Filled          int // retries that recovered content and were rewritten
	StillIncomplete int // retries that recovered nothing (nothing rewritten)
	Resolved        int // legacy "unknown" records resolved without recapture (complete-at-fetch sources)
}

// Issue is one verification finding: something referenced but not fully
// exported, so the archive can be audited for completeness.
type Issue struct {
	Folder  string
	Subject string
	RelPath string // exported HTML path relative to OutDir
	Date    time.Time
	Kind    string // "empty-attachment" | "attachment-error" | "unresolved-inline-image" | "id-reuse"
	Detail  string // attachment name or cid token
}

// Exporter writes messages to disk and updates the manifest.
type Exporter struct {
	OutDir   string
	Manifest *state.Manifest
	Mode     Mode
	Since    time.Time // zero means no date filter
	Log      *log.Logger

	// KeepRaw preserves each message's original RFC 822 bytes as <stem>.eml
	// beside the html, for sources that have them (mbox, maildir, Graph).
	KeepRaw bool

	// SourceComplete says the source delivers a message whole on every read
	// (Graph: one GET returns the full MIME), so a gap can never be filled by
	// re-reading: it is recorded as terminal, not fillable. On-demand sources
	// (PST/OST caches, Thunderbird, Evolution) leave it false.
	SourceComplete bool

	// DedupMailboxWide keys a message by its identity ALONE — mailbox-wide,
	// folder-independent (state.LiveKey) — instead of by (store, folder,
	// identity), so a message that lives in (or moves between) several folders is
	// stored once per mailbox and its folder over time is a record field, not a
	// second archived copy (R3 reworded, §3.1). It is set ONLY on the live/repeat
	// path (Graph; IMAP later); a one-shot local import leaves it false and keeps
	// the folder-scoped key and R3 (§3.6). Under option D (design rev-4) the stored
	// Fingerprint is the CONTENT fingerprint on both settings — the distinct-reuse
	// discriminator, compared only post-download — and the pre-download skip is by
	// Message-ID membership in the graph layer, so no envelope signature is stored
	// or compared (rev-2's retired mechanism). A no-Message-ID message dedups
	// mailbox-wide on its post-download content-hash identity.
	DedupMailboxWide bool

	// OnExported, if set, is called after a message is successfully written
	// (used to feed the search index). relPath is the HTML path relative to
	// OutDir (forward-slashed); key is the manifest key.
	OnExported func(store string, folderPath []string, m *model.Message, relPath, key string)

	// RunAt is the run's timestamp, stamped as LastSeen when a mailbox-wide
	// observation SKIPS the write (DedupMailboxWide) — a no-Message-ID message
	// reaches the exporter, not the pre-download fast-path, because its identity
	// is only known post-download, and every observation (a skip included) must
	// advance LastSeen so gone-detection never marks a still-present message gone
	// (§3.4/§3.6). It is the same value the live fast-path stamps, so with-mid and
	// no-mid observations in one run agree. Zero (a local import, or a test that
	// omits it) falls back to time.Now at skip time.
	RunAt time.Time

	// OnManifestSkip, if set, is called when a mailbox-wide incremental skip
	// (DedupMailboxWide) observes an already-archived, complete message the write
	// was skipped for — the exporter has already stamped the record's timeline
	// (LastSeen/Present/Folder via MergeFields); this callback lets the live path
	// record the side effects it alone can reach when the message MOVED: a history
	// folder-assertion and the body-free index folder update (§3.2). moved is true
	// only when the observed folder differs from the record's previous folder, so
	// an unchanged re-observation costs no history/index write. It is the no-mid
	// analogue of the pre-download fast-path's move recording.
	OnManifestSkip func(key string, folderPath []string, moved bool)

	Stats  Stats
	Issues []Issue // verification findings (attachments/inline images not fully exported)

	// reuseLogged dedupes the per-run #8-residual id-reuse note (NoteIDReuse) so an
	// identity with multiple distinct fingerprints is reported once per run, not
	// once per folder-observation.
	reuseLogged map[string]bool

	// churnLogged dedupes the per-run phys-churn anomaly (NotePhysChurn) so an
	// identity whose immutable id was reissued (a one-time restore/migration) is
	// reported once per run.
	churnLogged map[string]bool
}

// Export writes a single message. It returns true if the message was written
// (false when skipped by the manifest or the date filter, or when a retry found
// nothing new to write).
//
// Decision (R1/R2): a message already in the manifest is skipped unless its
// record is fillable — then it is re-examined regardless of the -since window,
// first by a cheap probe (no render, no temp files) and, only if some
// previously-missing item is now present, captured and committed again. An
// unseen message goes through the date filter, then capture and commit.
// The store argument is the store TOKEN (state.Token), not a raw display name:
// it names both the on-disk directory (OutDir/token/…) and the first component
// of the key, so two mailboxes archived into one -out never collide even when
// their display names match (F1). Callers compute it once per source.
func (e *Exporter) Export(store string, folderPath []string, m *model.Message) (bool, error) {
	date := m.Date()
	folderKey := strings.Join(folderPath, "/")
	// Option D (design rev-4): the stored fingerprint is the CONTENT fingerprint
	// on BOTH paths — the sole distinct-reuse discriminator, compared only
	// POST-download in the #fp-split below. The live path differs only in the KEY
	// (mailbox-wide, folder-independent), so one message is one record whatever
	// folder it moves through (R3). The pre-download skip is by Message-ID
	// membership in the graph layer, which stores and compares no envelope
	// signature — rev-2's retired mechanism, which re-downloaded inline-attachment
	// mail every run (candidate/stored disagreed) and could drop a distinct reuse
	// whose envelope matched (adversarial #7/#8).
	fp := m.Fingerprint()
	var key string
	if e.DedupMailboxWide {
		key = state.LiveKey(store, m.Identity())
	} else {
		key = state.Key(store, folderKey, m.Identity())
	}

	// A DIFFERENT message reusing an already-archived Message-ID is a distinct
	// message, not a duplicate: it lives under an envelope-qualified key. The
	// fingerprint covers only the stable envelope (never bodies or attachment
	// bytes), so a fill of an incomplete record keeps its fingerprint and a
	// mismatch always means a different message — whether the earlier record was
	// complete or not. The record's fingerprint anchors which message owns the
	// plain key, so names stay stable whatever order a later run walks them in
	// (R3/R1).
	// Adopt-never-split, LIVE PATH ONLY (design rev-4 §3). On the live/repeat path
	// (DedupMailboxWide) a legacy-scheme fingerprint (FpScheme != current: any
	// pre-v5 or empty fp) is NOT comparable to a current one — the go-back work
	// changed the fingerprint's date term (delivered timestamp vs the MIME Date
	// header) — so a mismatch against a legacy sibling is NO evidence of a distinct
	// message: adopt it (the seen+complete record is skipped below — one copy, no
	// duplicate), never #fp-split a migrated record into a duplicate (R3). On this
	// path a with-Message-ID record is anyway skipped by membership in the graph
	// layer before Export, so only a no-Message-ID/content-hash record reaches
	// here, where an equal identity means equal content — adopt is safe. A LOCAL
	// import (flag off) is NOT gated: its content fingerprint is computed the same
	// way in every version (Date from the MIME header), so it stays comparable and
	// a genuine distinct reuse still #fp-splits — both survive (R1, MA-86).
	// Adopt-never-split (live path) applies ONLY when this identity has a single
	// same-store record — the ordinary single-message migration, where adopting a
	// legacy-scheme record is unambiguous. With MORE THAN ONE record (a distinct
	// Message-ID reuse survived collapse), adopting could overwrite the WRONG
	// physical message (an R1/R13 drop) or, in full mode, duplicate one; so the
	// split is NOT suppressed there — a genuine fingerprint mismatch #fp-splits to
	// a qualified key (a bounded duplicate, never a drop). This also covers full
	// mode, which bypasses the graph membership skip and re-exports every message.
	identityMultiRecord := e.DedupMailboxWide && e.Manifest.SameStoreIdentityCount(store, m.Identity()) > 1
	isMid := strings.HasPrefix(m.Identity(), "mid:")
	prev, seen := e.Manifest.Get(key)

	// PhysID-aware placement (design closure rev-6.1 §4, content-hash arbiter), LIVE
	// PATH + Message-ID identity + a known immutable id. The immutable id is the
	// distinctness signal and the recorded body-inclusive CONTENT HASH is the
	// churn-vs-distinct arbiter (no .eml / -raw needed). placeByPhysID scans the
	// identity's SAME-TOKEN siblings and returns (key, churn, ok): adopt an id match
	// or a content match (churn/backfill), or split a genuinely distinct reuse under
	// a PhysID-hash key (HC1). ok is false — fall to the fingerprint floor unchanged
	// — whenever it cannot safely decide by content: no incoming content hash, or ANY
	// same-token sibling lacks a stored content hash (a pre-v6/floor record). So an
	// archive with no content hashes stays the shipped Message-ID floor: no
	// re-download storm, no unsafe split, no drop beyond the documented floor (HC8: a
	// no-Message-ID identity already folds the body into contentHash — left to the
	// floor).
	physResolved := false
	if e.DedupMailboxWide && isMid && m.PhysID != "" {
		if k, churn, ok := e.placeByPhysID(store, m, fp); ok {
			key = k
			if churn {
				e.NotePhysChurn(m.Identity())
			}
			prev, seen = e.Manifest.Get(key)
			physResolved = true
		}
	}

	for !physResolved && seen && prev.Fingerprint != "" && prev.Fingerprint != fp &&
		!(e.DedupMailboxWide && prev.FpScheme != state.FpSchemeCurrent && !identityMultiRecord) {
		// A DIFFERENT message reused this key's Message-ID: file it under a
		// fingerprint-qualified key. The separator is NUL (state.Qualify), never a
		// character legal in a Message-ID, so no crafted id — not even one that
		// literally contains "#"+fingerprint — can pre-occupy the qualified slot
		// and make this distinct message look already-seen. Should the qualified
		// slot somehow hold yet another message (a fingerprint collision), qualify
		// again rather than treat this one as seen: a mismatch must never become a
		// silent skip (AGG2-1/R1).
		key = state.Qualify(key, fp)
		prev, seen = e.Manifest.Get(key)
	}

	retry := false
	if e.Mode == Incremental && seen {
		if !prev.Fillable() {
			// A mailbox-wide observation of an already-archived, complete message
			// writes nothing, but this run still SAW the message: stamp its timeline
			// (LastSeen=thisRun, Present) and, when its observed folder differs,
			// record the move. A no-Message-ID message lands here — not the
			// pre-download fast-path — because its identity is a post-download content
			// hash, so without this stamp its LastSeen would never advance (a
			// still-present message would be wrongly marked gone once gone-detection
			// lands) and a move would never be followed (§3.4/§3.6). The move's
			// history/index side effects are the live path's (OnManifestSkip); the
			// field merge is here so the manifest is correct even without a callback.
			// A folder-scoped local import (flag off) keeps the plain skip and R3.
			if e.DedupMailboxWide {
				// Emit a folder-assertion on a MOVE (folder changed) OR on
				// present-again (the record was gone and is seen again): both are
				// one folder-assertion shape, and the fold reads an assertion after
				// a gone event as present-again (§3.3). This mirrors the graph
				// fast-path's `rec.Folder != folderKey || !rec.Present`; without the
				// `!prev.Present` twin a no-Message-ID message that went gone and
				// reappeared in the SAME folder got no event, so the fold reported
				// it gone forever after (#5, adversarial 2026-09-12).
				assert := prev.Folder != folderKey || !prev.Present
				seenAt := e.RunAt
				if seenAt.IsZero() {
					seenAt = time.Now().UTC()
				}
				e.Manifest.MergeFields(key, folderKey, seenAt, true)
				if e.OnManifestSkip != nil {
					e.OnManifestSkip(key, folderPath, assert)
				}
			}
			e.Stats.SkippedManifest++
			return false, nil
		}
		e.Stats.Retried++
		if !probeImproves(m, prev.Missing) {
			e.Stats.StillIncomplete++
			return false, nil
		}
		retry = true
	}
	if !retry && !e.Since.IsZero() && !date.IsZero() && date.Before(e.Since) {
		e.Stats.SkippedDate++
		return false, nil
	}

	// store is the sanitized, disambiguated token (state.Token). A last-line guard
	// before it becomes a directory segment: refuse a token that is not a single
	// safe path segment, so a tampered manifest can never redirect a write outside
	// the output root no matter what reached here (INS2-1/R4).
	if !state.SafeToken(store) {
		return false, fmt.Errorf("refusing to export: store token %q is not a single safe path segment; a write to %s could otherwise escape the archive (tampered manifest?)", store, e.OutDir)
	}
	// store is used verbatim as the directory segment — the same string that
	// scopes the key, so the on-disk tree and the manifest never disagree (F1).
	// The FILE is written under the message's first-captured folder, never the
	// currently-observed one: on the mailbox-wide path a re-export of an already-
	// archived message (a full run, or a move) keeps the physical file put (R13);
	// only the recorded current folder follows the move. writeFolderPath is the
	// current folder for a new record and the first-captured folder for a seen one.
	writeFolderPath := folderPath
	if e.DedupMailboxWide && seen && prev.FirstFolder != "" {
		writeFolderPath = strings.Split(prev.FirstFolder, "/")
	}
	dirParts := append([]string{e.OutDir, store}, writeFolderPath...)
	dir := filepath.Join(dirParts...)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, fmt.Errorf("create output dir %s: %w", dir, err)
	}

	base, htmlPath, relSlash := e.stemFor(dir, date, m.Subject, key)
	rawName := ""
	if e.KeepRaw && len(m.Raw) > 0 {
		rawName = base + ".eml"
	}
	ctx := RenderContext{
		RootRel:        strings.Repeat("../", 1+len(writeFolderPath)), // store dir + folders
		FolderIndexRel: "index.html",
		RawName:        rawName,
	}
	if hasArchivable(m.Attachments, map[int]bool{}) { // refined below once inline embedding is known
		ctx.ZipName = base + zipSuffix
	}
	rr, err := RenderWith(m, ctx)
	if err != nil {
		return false, fmt.Errorf("render %q: %w", m.Subject, err)
	}
	if ctx.ZipName != "" && !hasArchivable(m.Attachments, rr.Consumed) {
		// Every attachment was embedded inline: no zip will exist, so re-render
		// without the link (cheap: such messages are small).
		ctx.ZipName = ""
		if rr, err = RenderWith(m, ctx); err != nil {
			return false, fmt.Errorf("render %q: %w", m.Subject, err)
		}
	}
	htmlBytes, inlineConsumed := rr.HTML, rr.Consumed

	// Fixity of every file this capture writes, recorded from the exact bytes
	// as they are written so `verify` can later detect bit-rot or truncation
	// (F3). A re-capture builds a fresh Fixity, replacing any earlier one.
	var fx state.Fixity
	if rawName != "" {
		d, err := writeFileAtomicDigest(filepath.Join(dir, rawName), m.Raw)
		if err != nil {
			return false, fmt.Errorf("write %s: %w", rawName, err)
		}
		fx.EML = &d
		e.Stats.RawWritten++
	}

	e.Stats.AttachmentsInline += len(inlineConsumed)

	// Gaps: what this capture could not deliver. Fillable or terminal per the
	// source (SourceComplete); never silent (R1).
	var gaps []string
	if !hasBody(m) {
		gaps = append(gaps, state.MissingBody)
	}

	// Attachments first (zip, then html): a visible html always has its zip.
	zipPath := filepath.Join(dir, base+zipSuffix)
	if hasArchivable(m.Attachments, inlineConsumed) {
		zr, zerr := WriteZip(zipPath, m.Attachments, inlineConsumed)
		if zerr != nil {
			// A failed archive should not abort the whole export, but it is
			// never silent: every attachment of the message is then unarchived.
			e.Log.Printf("warning: attachments for %s: %v", htmlPath, zerr)
			for i := range m.Attachments {
				if !inlineConsumed[i] {
					zr.Failed = append(zr.Failed, attachmentLabel(m.Attachments[i], i))
				}
			}
		}
		e.Stats.Attachments += zr.Written
		fx.Zip = zr.Digest // nil unless a zip was actually committed
		for _, name := range zr.Empty {
			e.Stats.AttachmentsEmpty++
			e.addIssue(folderKey, m, relSlash, "empty-attachment", name)
			gaps = append(gaps, name)
		}
		for _, name := range zr.Failed {
			e.Stats.AttachmentErrors++
			e.addIssue(folderKey, m, relSlash, "attachment-error", name)
			gaps = append(gaps, name)
		}
	} else {
		os.Remove(zipPath) // a re-capture with nothing archivable must not keep a stale zip
	}

	hd, err := writeFileAtomicDigest(htmlPath, htmlBytes)
	if err != nil {
		return false, fmt.Errorf("write %s: %w", htmlPath, err)
	}
	fx.HTML = &hd

	// Inline images referenced by cid: that we could not embed (missing from the
	// message, e.g. dangling references in a reply/forward chain).
	unresolved := rr.Unresolved
	for _, cid := range unresolved {
		e.Stats.UnresolvedInlineRef++
		e.addIssue(folderKey, m, relSlash, "unresolved-inline-image", cid)
	}

	now := time.Now().UTC()
	rec := state.Record{Path: relSlash, Folder: folderKey, ExportedAt: now, Unresolved: unresolved}
	if e.SourceComplete {
		rec.Terminal = gaps
	} else {
		rec.Missing = gaps
	}
	if rec.HasIssues() {
		rec.Subject = m.Subject
		if !date.IsZero() {
			rec.Date = date.UTC().Format(time.RFC3339)
		}
	}
	rec.Fingerprint = fp
	rec.FpScheme = state.FpSchemeCurrent // this build's fingerprint scheme (rev-4 §3)
	rec.Fixity = &fx
	// Folder-over-time timeline (the go-back record fields) on the live path:
	// Present now, LastSeen now, and the set-once FirstFolder/FirstSeen preserved
	// across a re-export so a full re-run never loses when/where a message was
	// first captured (§3.1). A folder-scoped local record leaves them empty
	// (omitempty); Load fills its defaults.
	if e.DedupMailboxWide {
		rec.Present = true
		rec.LastSeen = now
		// PhysID (closure rev-6 §1/HC5): store this listing's immutable id. When the
		// listing withheld it (empty) but a prior record already knew it, carry that
		// baseline forward so a later distinct reuse still has a PhysID to be told
		// apart by. Inert here — the exporter/fast-path consult it in H3/H4.
		rec.PhysID = m.PhysID
		if rec.PhysID == "" && seen {
			rec.PhysID = prev.PhysID
		}
		// The body-inclusive content hash is the closure's churn-vs-distinct arbiter
		// (rev-6.1). Recorded on every live-path write so a later run can tell a
		// distinct reuse of this Message-ID from the same message re-observed, without
		// the original .eml. Reflects THIS capture's content (a fill updates it).
		rec.ContentHash = m.ContentDigest()
		if seen && !prev.FirstSeen.IsZero() {
			rec.FirstFolder = prev.FirstFolder
			rec.FirstSeen = prev.FirstSeen
		} else {
			rec.FirstFolder = folderKey
			rec.FirstSeen = now
		}
		// Carry the collapse-loser file list across a re-export, so a full run does
		// not strand loser copies outside redaction's reach (rev-4 §6, #4).
		if seen {
			rec.AlsoFiles = prev.AlsoFiles
		}
	}
	e.Manifest.Add(key, rec)
	if e.OnExported != nil {
		e.OnExported(store, folderPath, m, relSlash, key)
	}
	// Re-exported under a new name (subject or date changed, naming rule
	// changed): the previous html/zip are this message's own and must not
	// linger as duplicates (R6/R13). But NEVER delete the prior file when we are
	// adopting a LEGACY-scheme record on the live path: its identity is not
	// content-verified (the fingerprint scheme is incomparable), so the message we
	// just downloaded MIGHT be a distinct reuse of a gone original rather than the
	// same message — deleting the original's file would be irreversible data loss
	// (R13). Keeping it costs at most a harmless orphan (verify flags it); the
	// full resolution of the reuse-vs-same ambiguity is the deferred ImmutableId
	// closure. (Adversarial re-check #1, 2026-09-12.)
	adoptingLegacy := e.DedupMailboxWide && seen && prev.FpScheme != state.FpSchemeCurrent
	if seen && prev.Path != "" && prev.Path != relSlash && !adoptingLegacy {
		old := filepath.Join(e.OutDir, filepath.FromSlash(prev.Path))
		os.Remove(old)
		os.Remove(strings.TrimSuffix(old, ".html") + zipSuffix)
		os.Remove(strings.TrimSuffix(old, ".html") + ".eml")
	}

	switch {
	case !hasBody(m):
		e.Stats.NoBody++
	case strings.TrimSpace(m.HTMLBody) == "":
		e.Stats.NonHTMLBodies++
	}
	if retry {
		e.Stats.Filled++
	}
	// Whether or not KeepRaw wrote it, note that this message HAD original bytes:
	// a raw-capable source archived without -raw is the extract-foreclosure the
	// capture-time warning surfaces (PC15).
	if len(m.Raw) > 0 {
		e.Stats.RawAvailable++
	}
	e.Stats.Exported++
	return true, nil
}

// stemFor picks the file stem for key inside dir. The 8-hex digest of the key is
// unique in practice; when a DIFFERENT key already owns that stem (a 32-bit
// collision) the digest is lengthened, deterministically from this key alone,
// so nothing is ever overwritten and a re-run makes the same choice (R4).
func (e *Exporter) stemFor(dir string, date time.Time, subject, key string) (base, htmlPath, relSlash string) {
	// Path budget: Windows limits a full path to 260 characters and the
	// archive root is the operator's. Keep the archive-relative path under
	// relPathBudget by shrinking the subject slug (the only elastic part) —
	// the subject stays in the html header and the index, never lost.
	slug := slugMax
	for {
		rel := filepath.Join(strings.TrimPrefix(dir, e.OutDir), baseNameN(date, subjectSlug(subject, slug), key, 8)+".html")
		if len(rel) <= relPathBudget || slug == 0 {
			break
		}
		switch slug {
		case slugMax:
			slug = 24
		default:
			slug = 0
		}
	}
	for _, n := range []int{8, 12, 16, 24, 40} {
		base = baseNameN(date, subjectSlug(subject, slug), key, n)
		htmlPath = filepath.Join(dir, base+".html")
		rel, err := filepath.Rel(e.OutDir, htmlPath)
		if err != nil {
			rel = htmlPath
		}
		relSlash = filepath.ToSlash(rel)
		if owner, ok := e.Manifest.KeyForPath(relSlash); !ok || owner == key {
			return
		}
	}
	return
}

const (
	slugMax       = 60  // runes of subject slug in a file stem
	relPathBudget = 200 // archive-relative path length we try to stay under
)

// subjectSlug returns the subject slug bounded to n runes, or "" for n == 0.
func subjectSlug(subject string, n int) string {
	if n == 0 {
		return ""
	}
	return util.Slug(subject, n)
}

// hasBody reports whether any body source is present.
func hasBody(m *model.Message) bool {
	return strings.TrimSpace(m.HTMLBody) != "" || strings.TrimSpace(m.PlainBody) != "" || strings.TrimSpace(m.RTFBody) != ""
}

// probeImproves reports whether any item in oldMissing is now present in the
// source message — the subset rule (a retry is promoted when the old missing set
// is not contained in the new one) evaluated without rendering or writing
// anything. The unknown sentinel always improves: a legacy record is re-captured
// once so it can carry the truth.
func probeImproves(m *model.Message, oldMissing []string) bool {
	for _, item := range oldMissing {
		switch item {
		case state.UnknownSentinel:
			return true
		case state.MissingBody:
			if hasBody(m) {
				return true
			}
		default:
			for i := range m.Attachments {
				if attachmentLabel(m.Attachments[i], i) == item && attachmentBytes(m.Attachments[i]) > 0 {
					return true
				}
			}
		}
	}
	return false
}

// attachmentBytes counts an attachment's bytes by streaming it to nowhere.
func attachmentBytes(a model.Attachment) int64 {
	if a.WriteTo == nil {
		return 0
	}
	n, err := a.WriteTo(io.Discard)
	if err != nil {
		return 0
	}
	return n
}

// addIssue records a verification finding (capped so a pathological archive
// can't exhaust memory; the counters in Stats remain exact).
func (e *Exporter) addIssue(folder string, m *model.Message, relPath, kind, detail string) {
	const maxIssues = 10000
	if len(e.Issues) >= maxIssues {
		return
	}
	e.Issues = append(e.Issues, Issue{
		Folder:  folder,
		Subject: m.Subject,
		RelPath: relPath,
		Date:    m.Date(),
		Kind:    kind,
		Detail:  detail,
	})
}

// NoteIDReuse records the bounded, mandatory #8-residual log for the live path
// (design rev-4 §5, EC9): when a pre-download membership skip observes an identity
// that is ALREADY archived under MORE THAN ONE distinct fingerprint (a genuine
// Message-ID reuse — e.g. two distinct messages that shared an id in a migrated v3
// archive), it is a case where a further distinct reuse could be skipped without
// capture on the floor (the deferred Graph ImmutableId closure detects such
// reuses pre-download). It fires only on genuine ambiguity (distinct > 1) and is
// deduped per identity per run, so it never floods; a single distinct sibling (the
// ordinary case) logs nothing. It appends an "id-reuse" Issue and logs a note.
func (e *Exporter) NoteIDReuse(identity string, distinct int) {
	if distinct <= 1 {
		return
	}
	if e.reuseLogged == nil {
		e.reuseLogged = map[string]bool{}
	}
	if e.reuseLogged[identity] {
		return
	}
	e.reuseLogged[identity] = true
	e.Issues = append(e.Issues, Issue{Kind: "id-reuse", Detail: identity})
	if e.Log != nil {
		e.Log.Printf("note: %s is archived under %d distinct fingerprints (a reused Internet-Message-ID); a further distinct reuse of it can be skipped without capture on this run — the deferred Graph ImmutableId closure detects such reuses (docs/goback.md)", identity, distinct)
	}
}

// placeByPhysID chooses the manifest key for a live-path message that carries a
// Graph immutable id under a Message-ID identity (design closure rev-6.1 §4,
// content-hash arbiter). The caller guarantees a non-empty PhysID. It scans the
// identity's SAME-TOKEN siblings (never another store's — R6) and returns
// (key, churn, ok):
//  1. a sibling already stores THIS immutable id -> that sibling (the same physical
//     message re-observed); adopt. ok=true.
//  2. otherwise, if EVERY same-token sibling carries a stored content hash: a
//     sibling whose content hash EQUALS this message's is the same message with a
//     REISSUED id -> adopt it and BACKFILL its PhysID (churn=true so the caller logs
//     it — an empty-id sibling would be a plain backfill, but on this path all
//     siblings are v6 records so a differing id means a reissue); no content match ->
//     a genuinely DISTINCT reuse -> split under a key qualified by a HASH OF THE
//     IMMUTABLE ID, not the fingerprint (HC1: identical envelopes collide under an fp
//     key — an R1 drop). The slot-taken loop requalifies so a hash collision never
//     becomes a skip. ok=true.
//  3. if ANY same-token sibling LACKS a content hash (a pre-v6 / floor record, or the
//     incoming message has none) -> ok=FALSE: the closure cannot safely tell a
//     distinct reuse from the archived message without a comparable content hash, so
//     it defers to the shipped Message-ID floor (no unsafe split, no drop, no
//     re-download storm to backfill legacy records). Such an archive closes #8 only
//     for messages captured fresh under v6.
func (e *Exporter) placeByPhysID(store string, m *model.Message, fp string) (string, bool, bool) {
	ch := m.ContentDigest()
	if ch == "" {
		return "", false, false
	}
	base := state.LiveKey(store, m.Identity())
	tokenPrefix := store + "\x00"
	var sibs []state.IdentRef
	for _, s := range e.Manifest.KeysForIdentity(m.Identity()) {
		if strings.HasPrefix(s.Key, tokenPrefix) {
			sibs = append(sibs, s)
		}
	}
	// 1. exact immutable-id match: the same physical message.
	for _, s := range sibs {
		if s.PhysID == m.PhysID {
			return s.Key, false, true
		}
	}
	// Require every same-token sibling to be content-comparable; a pre-v6/floor
	// sibling (no content hash) means the identity is not v6-managed -> defer to the
	// floor (step 3 in the doc).
	recs := make([]state.Record, len(sibs))
	for i, s := range sibs {
		r, ok := e.Manifest.Get(s.Key)
		if !ok || r.ContentHash == "" {
			return "", false, false
		}
		recs[i] = r
	}
	// 2. a content match: the same message whose immutable id was reissued (a churn).
	for i, s := range sibs {
		if recs[i].ContentHash != ch {
			continue
		}
		e.Manifest.BackfillPhys(s.Key, m.PhysID)
		return s.Key, true, true
	}
	// 3. a genuinely distinct reuse: key by a hash of the immutable id.
	if _, taken := e.Manifest.Get(base); !taken {
		return base, false, true
	}
	key := state.Qualify(base, util.HashHex(m.PhysID, 8))
	for {
		r, taken := e.Manifest.Get(key)
		if !taken || r.PhysID == m.PhysID {
			return key, false, true
		}
		key = state.Qualify(key, util.HashHex(m.PhysID, 8))
	}
}

// NotePhysChurn records the phys-churn anomaly (design closure rev-6.1 §4/HC7): a
// message whose Graph immutable id was REISSUED between runs (a one-time restore or
// cross-tenant migration) is recognized by its byte-identical content, adopted in
// place, and its stored PhysID updated. It is not a distinct reuse and is never
// split; it is logged so an operator can see that an id namespace shifted. Deduped
// per identity per run.
func (e *Exporter) NotePhysChurn(identity string) {
	if e.churnLogged == nil {
		e.churnLogged = map[string]bool{}
	}
	if e.churnLogged[identity] {
		return
	}
	e.churnLogged[identity] = true
	e.Issues = append(e.Issues, Issue{Kind: "phys-churn", Detail: identity})
	if e.Log != nil {
		e.Log.Printf("note: %s kept its content but its immutable id was reissued (a restore or cross-tenant migration); adopted in place with the new id, not duplicated (docs/goback.md)", identity)
	}
}

// reInlineCID matches a cid: reference token in HTML.
var reInlineCID = regexp.MustCompile(`(?i)cid:([a-z0-9._%+@-]{1,120})`)

// unresolvedInlineRefs returns the distinct cid: tokens still present in the
// rendered HTML — i.e. inline images that could not be embedded because no
// matching part existed in the message.
func unresolvedInlineRefs(html []byte) []string {
	matches := reInlineCID.FindAllSubmatch(html, -1)
	if matches == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, m := range matches {
		cid := string(m[1])
		if !seen[cid] {
			seen[cid] = true
			out = append(out, cid)
		}
	}
	return out
}

// baseName builds the shared file stem for a message's HTML and zip.
func baseName(date time.Time, subject, key string) string {
	return baseNameN(date, util.Slug(subject, slugMax), key, 8)
}

// baseNameN is baseName with an n-hex digest and a pre-computed slug (which
// may be empty under the path budget; see stemFor).
func baseNameN(date time.Time, slug, key string, n int) string {
	ts := "0000-00-00_0000"
	if !date.IsZero() {
		ts = date.UTC().Format("2006-01-02_1504")
	}
	if slug == "" {
		return ts + "_" + util.HashHex(key, n)
	}
	return ts + "_" + slug + "_" + util.HashHex(key, n)
}

// hasArchivable reports whether any attachment would go into the zip (i.e. is
// not consumed inline).
func hasArchivable(atts []model.Attachment, inlineConsumed map[int]bool) bool {
	for i := range atts {
		if !inlineConsumed[i] {
			return true
		}
	}
	return false
}
