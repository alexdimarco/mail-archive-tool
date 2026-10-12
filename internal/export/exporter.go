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
	HTMLFallback        int // HTML bodies shown as escaped source, for any of the four Fallback* reasons (too deep / unparseable / unsafe after re-parse / too large)
	NonHTMLBodies       int // messages exported from plain/RTF because no HTML body existed
	NoBody              int // messages exported with no body content at all
	RawWritten          int // original .eml files written (KeepRaw; 0 for sources with no original bytes, e.g. Outlook items)
	RawAvailable        int // exported messages that HAD original bytes available, whether or not KeepRaw wrote them; >0 without KeepRaw means the source is raw-capable but no .eml was preserved (extract will find nothing — PC15)

	// Incremental completeness (R1/R2): fillable gaps are re-examined.
	Retried         int // seen-but-fillable records re-examined this run
	Filled          int // retries that recovered content and were rewritten
	StillIncomplete int // retries that recovered nothing (nothing rewritten)
	Resolved        int // legacy "unknown" records resolved without recapture (complete-at-fetch sources)

	// Fault isolation (§4.2). Unreadable counts partly-read records — one that
	// carries an unreadable:* label — written or re-examined this run: it is what
	// makes a run partial besides the ledger's gaps. Unparsed counts unparsed stubs
	// written or seen. Kept counts the degraded captures the KEEP guard held back
	// from an archived record (P15).
	Unreadable int
	Unparsed   int
	Kept       int
	// ExportPanics counts the messages whose export panicked (the run records each as
	// an export-panic gap, never only in the log, F33); ExportFaults the messages
	// and folders the archive could not be written for (export-failed and
	// folder-unwritable gaps).
	ExportPanics int
	ExportFaults int
}

// Issue is one verification finding: something referenced but not fully
// exported, so the archive can be audited for completeness.
type Issue struct {
	Folder  string
	Subject string
	RelPath string // exported HTML path relative to OutDir
	Date    time.Time
	Kind    string // "empty-attachment" | "attachment-error" | "unresolved-inline-image" | "html-shown-as-source" | "id-reuse"
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

	// BeforeWrite, if set, is called with the path of each file the exporter is
	// about to write (the page and the preserved original); a non-nil error is that
	// write's failure. It is nil in production — the seam a test injects an errno
	// through (a full disk, a read-only volume) without a real one (MA-320).
	BeforeWrite func(path string) error

	Stats  Stats
	Issues []Issue // verification findings (attachments/inline images not fully exported)

	// reuseLogged dedupes the per-run #8-residual id-reuse note (NoteIDReuse) so an
	// identity with multiple distinct fingerprints is reported once per run, not
	// once per folder-observation.
	reuseLogged map[string]bool

	// gapRunAt is the stamp ledger gaps the exporter records carry when the caller
	// set no RunAt (a test, or a run that keeps no timeline): fixed on first use, so
	// every observation of one Exporter belongs to one run.
	gapRunAt time.Time
}

// runStamp is the run's timestamp for a ledger observation: RunAt when the
// caller set it, else one stamp for the Exporter's life.
func (e *Exporter) runStamp() time.Time {
	if !e.RunAt.IsZero() {
		return e.RunAt
	}
	if e.gapRunAt.IsZero() {
		e.gapRunAt = time.Now().UTC()
	}
	return e.gapRunAt
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

	// KEEP guard (P15, FC5): a capture that carries a read-gap label never replaces
	// an archived record that does not already carry it. It runs on every source and
	// in both modes, ahead of key placement and of any write, so a degraded read can
	// neither overwrite a complete page and zip nor mint a #fp-split sibling.
	if len(m.ReadGaps) > 0 && e.keepDegraded(store, folderKey, folderPath, m, key) {
		return false, nil
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
		if k, ok := e.placeByPhysID(store, m, fp); ok {
			key = k
			prev, seen = e.Manifest.Get(key)
			physResolved = true
		}
	}

	// A capture that carries a read gap is never #fp-split (§4.7.2): its envelope is
	// what the reader could read of an item that already has a record here, so a
	// differing fingerprint is no evidence of a distinct message. And a partly-read
	// record's fingerprint was computed over zero attachments, so a healthy read of
	// the same item is compared in that form too (§4.7.4(1)).
	for !physResolved && len(m.ReadGaps) == 0 && seen && prev.Fingerprint != "" && !sameFingerprint(prev, m, fp) &&
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
				e.observeArchived(key, prev, folderKey, folderPath)
			}
			// This run re-read the record, so it is examined under this build's parser
			// scheme: stamped in memory, no file written (MC12), unless the re-read
			// carries a note the record's scheme predates (examinedBy).
			if examinedBy(m, prev) {
				e.Manifest.StampParseScheme(key, state.ParseSchemeCurrent)
			}
			// A healthy read of an item that an earlier run only partly read, which
			// already has its own record here: the partly-read record is settled.
			e.supersedePartial(store, key, m)
			e.countReadGaps(m)
			e.Stats.SkippedManifest++
			return false, nil
		}
		e.Stats.Retried++
		if !probeImproves(m, prev.Missing) {
			e.Stats.StillIncomplete++
			if examinedBy(m, prev) { // re-read, as above (MC12)
				e.Manifest.StampParseScheme(key, state.ParseSchemeCurrent)
			}
			// Still partly read: it counts as a partly-read record seen this run, and
			// a Locator an older binary's save dropped is restored from the capture
			// (§4.2).
			e.countReadGaps(m)
			e.Manifest.SetLocator(key, m.Locator)
			return false, nil
		}
		retry = true
	}
	if !retry && !e.Since.IsZero() && !date.IsZero() && date.Before(e.Since) {
		// A message older than the window that the archive already holds (a Full run
		// downloads it; an Incremental one never reaches here with a record) is still
		// seen this run: it is stamped, and a move is followed, before the date skips
		// it. Left unstamped, a sweep of the walked folder would mark it gone while it
		// sits in the mailbox (S3-AI4).
		if e.DedupMailboxWide && seen {
			e.observeArchived(key, prev, folderKey, folderPath)
		}
		e.Stats.SkippedDate++
		return false, nil
	}

	// store is the sanitized, disambiguated token (state.Token). A last-line guard
	// before it becomes a directory segment: refuse a token that is not a single
	// safe path segment, so a tampered manifest can never redirect a write outside
	// the output root no matter what reached here (INS2-1/R4).
	if !state.SafeToken(store) {
		return false, &unsafeTokenError{fmt.Sprintf("refusing to export: store token %q is not a single safe path segment; a write to %s could otherwise escape the archive (tampered manifest?)", store, e.OutDir)}
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
		return false, &FolderError{Dir: dir, Err: err}
	}

	// The stem is built from the identity view's subject, never the displayed one: a
	// parser that decodes the subject differently cannot rename the files of a message
	// already archived, and an older binary computes the same stem (MC8).
	base, htmlPath, relSlash := e.stemFor(dir, date, m.IdentitySubject(), key)
	rawName, writeRaw := "", e.KeepRaw && len(m.Raw) > 0
	if writeRaw {
		rawName = base + ".eml"
	}
	// A re-capture that preserves no original of its own keeps the one the record has:
	// the page still links it and its fixity is carried on (MC8).
	var carriedEML *state.FileDigest
	if !writeRaw && seen {
		var carried bool
		if carriedEML, carried = carriedOriginal(dir, base, relSlash, prev); carried {
			rawName = base + ".eml"
		}
	}
	ctx := RenderContext{
		RootRel:        strings.Repeat("../", 1+len(writeFolderPath)), // store dir + folders
		FolderIndexRel: "index.html",
		RawName:        rawName,
		Store:          store,
		Folder:         folderKey,
	}
	// The zip's name can be offered before inline embedding is known: the page
	// links it only beside the attachments it lists — those NOT embedded
	// inline (renderHeader) — which is exactly when the zip below is written
	// (hasArchivable over rr.Consumed). A message whose every attachment is
	// inline shows no link, so the message is rendered ONCE (RV-8: it used to
	// be rendered a second time only to drop a link that was never shown, and
	// each render runs the sanitizer's parses).
	if hasArchivable(m.Attachments, map[int]bool{}) {
		ctx.ZipName = base + zipSuffix
	}
	rr, err := renderMessage(m, ctx)
	if err != nil {
		return false, &ItemError{msg: fmt.Sprintf("render %q", m.Subject), Err: err}
	}
	htmlBytes, inlineConsumed := rr.HTML, rr.Consumed

	// Fixity of every file this capture writes, recorded from the exact bytes
	// as they are written so `verify` can later detect bit-rot or truncation
	// (F3). A re-capture builds a fresh Fixity, replacing any earlier one.
	var fx state.Fixity
	if writeRaw {
		d, err := e.writeDigest(filepath.Join(dir, rawName), m.Raw)
		if err != nil {
			return false, &ItemError{Path: filepath.Join(dir, rawName), msg: "write " + rawName, Err: err}
		}
		fx.EML = &d
		e.Stats.RawWritten++
	} else if carriedEML != nil {
		fx.EML = carriedEML
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
			if parseGapNamed(m, name) {
				continue // its parse gap says it already: an empty .partial is a part cut short
			}
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

	hd, err := e.writeDigest(htmlPath, htmlBytes)
	if err != nil {
		return false, &ItemError{Path: htmlPath, msg: "write " + htmlPath, Err: err}
	}
	fx.HTML = &hd

	// Inline images referenced by cid: that we could not embed (missing from the
	// message, e.g. dangling references in a reply/forward chain).
	unresolved := rr.Unresolved
	for _, cid := range unresolved {
		e.Stats.UnresolvedInlineRef++
		e.addIssue(folderKey, m, relSlash, "unresolved-inline-image", cid)
	}

	if rr.BodyFallback != "" {
		e.Stats.HTMLFallback++
		e.addIssue(folderKey, m, relSlash, "html-shown-as-source", rr.BodyFallback)
	}

	now := time.Now().UTC()
	rec := state.Record{Path: relSlash, Folder: folderKey, ExportedAt: now, Unresolved: unresolved, BodyFallback: rr.BodyFallback}
	if e.SourceComplete {
		rec.Terminal = gaps
	} else {
		rec.Missing = gaps
	}
	// What the reader could not read of this item (§4.7.3): each label goes in the
	// list its class says (state.GapClass is the single owner). A label this build
	// does not know is filed by what the source can deliver, like any other gap.
	for _, l := range m.ReadGaps {
		e.fileGap(&rec, l)
	}
	// What the parser could not decode of it (design-mime-fidelity §4.9): a part kept as
	// received, one cut short, a multipart that could not be split. The same rule files
	// them: the deterministic kinds are terminal on every source, the two a complete copy
	// can cure follow SourceComplete (MC20). At most 32 labels and one summary.
	for _, l := range parseGapLabels(m) {
		e.fileGap(&rec, l)
	}
	if hasParseGapKind(m, state.GapKindFidelityFallback) {
		rec.FidelityFallback = state.ParseSchemeCurrent // a later scheme parses it again
	}
	// Only a partly-read record carries its Locator (FC6(3)): no other record's
	// bytes change, so a healthy input is what it was (P14).
	if rec.Unreadable() {
		rec.Locator = m.Locator
	}
	if rec.HasIssues() {
		rec.Subject = m.Subject
		if !date.IsZero() {
			rec.Date = date.UTC().Format(time.RFC3339)
		}
	}
	rec.Fingerprint = fp
	rec.FpScheme = state.FpSchemeCurrent // this build's fingerprint scheme (rev-4 §3)
	rec.ParseScheme = state.ParseSchemeCurrent
	// The body-inclusive content digest of the identity view (the v1 parse), on every
	// write, local records included: a later repair proves the archived message is the
	// one in the source by it (MC5). It is no identity input, so no key moves.
	rec.ContentHash = m.ContentDigest()
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
		// The body-inclusive content hash (set above for every write) is the closure's
		// churn-vs-distinct arbiter (rev-6.1): it lets a later run tell a distinct
		// reuse of this Message-ID from the same message re-observed, without the
		// original .eml. It reflects THIS capture's content (a fill updates it).
		if seen {
			// Carry the content-equal immutable-id SET forward on an adopt (rev-6.2):
			// a full-mode re-export or a fill REBUILDS the record, and without this the
			// fresh primary would overwrite AltPhysIDs, so the next incremental run
			// would re-download every OTHER copy of a copied message. Union the new
			// primary (already rec.PhysID) with the prior primary and its alternates,
			// dropping the primary itself and duplicates, order-stable.
			have := map[string]bool{rec.PhysID: true}
			for _, id := range append([]string{prev.PhysID}, prev.AltPhysIDs...) {
				if id == "" || have[id] {
					continue
				}
				have[id] = true
				rec.AltPhysIDs = append(rec.AltPhysIDs, id)
			}
		}
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
	e.countReadGaps(m)
	e.supersedePartial(store, key, m)
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
		if writeRaw {
			// The original goes only when this run wrote a new one beside the new
			// page: otherwise it is the record's only copy of the message (R20, MC8).
			os.Remove(strings.TrimSuffix(old, ".html") + ".eml")
		}
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

// examinedBy reports whether a re-read of prev's record may stamp it with the current
// parse scheme: no note this parse recorded was introduced after the scheme the record
// holds. The stamp is the repair's progress marker (MC12): a record whose re-read still
// shows a note newer than its scheme has been looked at but not repaired, and stamping it
// would make the repair's rule (a note with Since above the record's scheme) skip it for
// good. Nothing records a note before slice S2, so until then every re-read is stamped;
// the gate keeps a slice that bumps the scheme ahead of the repair slice (S7) from
// marking records it never repaired (MS1-AI1).
//
// It reads m.ParseNotes only, which is the whole predicate at scheme 1. The repair's rule
// (design, "Predicate") is wider: notes from renderNotes(m) (cid-text-ref, introduced by
// S4), prev.ParseScheme in RevokedSchemes, and 0 < prev.FidelityFallback < current. The
// slice that adds each of those inputs (S4 for renderNotes, S7 for the other two) must extend this gate in the same
// change, or its records are stamped as examined without having been (MS1-AI1 re-check).
func examinedBy(m *model.Message, prev state.Record) bool {
	for _, n := range m.ParseNotes {
		if n.Since > prev.ParseScheme {
			return false
		}
	}
	return true
}

// carriedOriginal says whether the preserved original (.eml) a seen record holds is on
// disk at the stem a re-capture writes to, and returns the fixity to carry on with it
// (nil for a record from before fixity). It carries only an original at the SAME stem:
// when the stem changed (a naming rule, the path budget, a moved message date) the old
// .eml stays where it is — the re-capture removes it only when it writes a new one —
// and the page no longer links it; moving it with its record is the commit of
// re-capture integrity (ruling J6), not this write.
func carriedOriginal(dir, base, relSlash string, prev state.Record) (*state.FileDigest, bool) {
	if prev.Path == "" || prev.Path != relSlash {
		return nil, false
	}
	if fi, err := os.Lstat(filepath.Join(dir, base+".eml")); err != nil || !fi.Mode().IsRegular() {
		return nil, false
	}
	if prev.Fixity != nil && prev.Fixity.EML != nil {
		d := *prev.Fixity.EML
		return &d, true
	}
	return nil, true
}

// writeDigest is writeFileAtomicDigest behind the BeforeWrite seam.
func (e *Exporter) writeDigest(dst string, data []byte) (state.FileDigest, error) {
	if e.BeforeWrite != nil {
		if err := e.BeforeWrite(dst); err != nil {
			return state.FileDigest{}, err
		}
	}
	return writeFileAtomicDigest(dst, data)
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

// sameFingerprint reports whether the capture is the item prev records. Besides
// the plain fingerprint, a partly-read record whose whole attachment table
// failed has a fingerprint computed over ZERO attachments, so a healthy read of
// the same item is compared in that form too: it must land on the partly-read
// record's key, not split from it as a "Message-ID reuse" (§4.7.4(1)).
func sameFingerprint(prev state.Record, m *model.Message, fp string) bool {
	if prev.Fingerprint == fp {
		return true
	}
	return prev.HasLabels([]string{state.LabelUnreadableAttachments}) && prev.Fingerprint == m.FingerprintNoAttachments()
}

// fileGap files one gap label in the list its class says: fillable in Missing, the rest
// in Terminal. state.GapClass is the single owner of the special labels' class; a label
// with none (an ordinary content label, a parse gap a later copy can cure) is filed by
// what the source can deliver.
func (e *Exporter) fileGap(rec *state.Record, label string) {
	class := state.GapClass(label)
	if class == "" && e.SourceComplete {
		class = state.ClassTerminal
	}
	if class == state.ClassFillable || class == "" {
		rec.Missing = appendUnique(rec.Missing, label)
	} else {
		rec.Terminal = appendUnique(rec.Terminal, label)
	}
}

// parseGapLabels are the record labels of m's parse gaps: its message-level gaps, then
// each attachment's, in order. A record holds at most state.MaxParseGapLabels of them and
// one summary label for the rest (MC21), so a hostile message cannot grow the manifest;
// the parts themselves are all archived.
func parseGapLabels(m *model.Message) []string {
	var all []string
	for _, g := range m.ParseGaps {
		all = append(all, state.ParseGapLabel(g.Kind, g.Detail))
	}
	for i, a := range m.Attachments {
		if a.GapKind != "" {
			all = append(all, state.ParseGapLabel(a.GapKind, attachmentLabel(a, i)+" — "+a.GapDetail))
		}
	}
	if len(all) <= state.MaxParseGapLabels {
		return all
	}
	more := len(all) - state.MaxParseGapLabels
	return append(all[:state.MaxParseGapLabels:state.MaxParseGapLabels],
		state.ParseGapLabel(state.GapKindStructureLimit, fmt.Sprintf("and %d more undecodable parts", more)))
}

// parseGapNamed reports whether an attachment called name carries a parse gap.
func parseGapNamed(m *model.Message, name string) bool {
	for i, a := range m.Attachments {
		if a.GapKind != "" && attachmentLabel(a, i) == name {
			return true
		}
	}
	return false
}

// hasParseGapKind reports whether the capture carries a message-level parse gap of kind.
func hasParseGapKind(m *model.Message, kind string) bool {
	for _, g := range m.ParseGaps {
		if g.Kind == kind {
			return true
		}
	}
	return false
}

// hasReadGap reports whether the capture carries the read-gap label.
func hasReadGap(m *model.Message, label string) bool {
	for _, l := range m.ReadGaps {
		if l == label {
			return true
		}
	}
	return false
}

// appendUnique appends s unless the list already holds it.
func appendUnique(list []string, s string) []string {
	for _, x := range list {
		if x == s {
			return list
		}
	}
	return append(list, s)
}

// countReadGaps counts the capture as a partly-read record and/or an unparsed
// stub seen this run (Stats.Unreadable makes the run partial; Unparsed does not).
func (e *Exporter) countReadGaps(m *model.Message) {
	unreadable, unparsed := false, false
	for _, l := range m.ReadGaps {
		unreadable = unreadable || state.IsUnreadableLabel(l)
		unparsed = unparsed || l == state.LabelUnparsed
	}
	if unreadable {
		e.Stats.Unreadable++
	}
	if unparsed {
		e.Stats.Unparsed++
	}
}

// observeArchived is a mailbox-wide observation of an already-archived message that
// writes nothing: the record's timeline follows it (LastSeen = this run, Present)
// and, on a MOVE (folder changed) or a present-again (the record was gone and is
// seen again), the live path's callback records the folder-assertion. Both are one
// folder-assertion shape, and the fold reads an assertion after a gone event as
// present-again (§3.3); without the !prev.Present twin a no-Message-ID message that
// went gone and reappeared in the SAME folder got no event, so the fold reported it
// gone forever after (#5, adversarial 2026-09-12). prev is the record's state before
// this observation.
func (e *Exporter) observeArchived(key string, prev state.Record, folderKey string, folderPath []string) {
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

// supersedePartial settles, after a healthy capture of an item (no read gap)
// that carries a Locator, a same-token partly-read record of the same item that
// sits under ANOTHER key: its unreadable:* labels become the info label
// "superseded" and RecoveredBy names the new key (§4.7.4(3)). Metadata only — no
// file is written, rendered or deleted, so no fixity can mismatch and no crash
// window exists.
func (e *Exporter) supersedePartial(store, key string, m *model.Message) {
	if len(m.ReadGaps) == 0 && m.Locator != "" {
		e.Manifest.SupersedePartial(store, m.Locator, key)
	}
}

// keepDegraded is the KEEP guard (P15, FC5). A capture that carries read-gap
// labels is never written over an existing same-token record of the same
// identity (on Graph, the same immutable id first) that does not already carry
// those labels: nothing is written, renamed or removed, the record is stamped
// seen (so the walk's sweep never calls it gone), the capture counts as Kept and
// an info "refresh-failed" gap names the label. It reports whether it held the
// capture back. A record that already carries the labels is the item's own
// partly-read record: that capture goes on to heal or re-examine it.
func (e *Exporter) keepDegraded(store, folderKey string, folderPath []string, m *model.Message, baseKey string) bool {
	prevKey, prev, ok := e.archivedRecordFor(store, m, baseKey)
	if !ok || prev.HasLabels(m.ReadGaps) {
		return false
	}
	seenAt := e.runStamp()
	if e.DedupMailboxWide {
		// The same stamp the incremental skip makes: the record's timeline follows the
		// observation, and a move or a present-again is recorded by the live path.
		exact := m.PhysID != "" && e.Manifest.HasPhysID(prevKey, m.PhysID)
		assert := prev.Folder != folderKey || !prev.Present
		e.Manifest.MergeFields(prevKey, folderKey, seenAt, true)
		if !exact {
			// A capture that is not matched to one immutable id cannot say WHICH
			// #fp-qualified sibling it observed: stamp them all (R21).
			prefix := store + "\x00"
			for _, sib := range e.Manifest.KeysForIdentity(m.Identity()) {
				if sib.Key != prevKey && strings.HasPrefix(sib.Key, prefix) {
					e.Manifest.TouchSeen(sib.Key, seenAt)
				}
			}
		}
		if e.OnManifestSkip != nil {
			e.OnManifestSkip(prevKey, folderPath, assert)
		}
	} else {
		e.Manifest.TouchSeen(prevKey, seenAt)
	}
	e.Stats.Kept++
	e.observeKept(store, folderKey, m)
	return true
}

// archivedRecordFor finds the record a degraded capture would have replaced: on
// the live path the same-token record that stores the capture's immutable id,
// else the record at the key the capture would have been filed under.
func (e *Exporter) archivedRecordFor(store string, m *model.Message, baseKey string) (string, state.Record, bool) {
	if e.DedupMailboxWide && m.PhysID != "" {
		prefix := store + "\x00"
		for _, sib := range e.Manifest.KeysForIdentity(m.Identity()) {
			if strings.HasPrefix(sib.Key, prefix) && e.Manifest.HasPhysID(sib.Key, m.PhysID) {
				if r, ok := e.Manifest.Get(sib.Key); ok {
					return sib.Key, r, true
				}
			}
		}
	}
	r, ok := e.Manifest.Get(baseKey)
	return baseKey, r, ok
}

// observeKept records the info gap a held-back capture leaves: reason
// refresh-failed, locator graph:<immutable id> on the live path (else
// ident:<identity>), naming the labels the capture carried. Info does not make a
// run partial; it says that an item's newest read was worse than its archived
// copy.
func (e *Exporter) observeKept(store, folderKey string, m *model.Message) {
	loc := "ident:" + m.Identity()
	if e.DedupMailboxWide && m.PhysID != "" {
		loc = state.BoundLocator("graph:" + m.PhysID) // the key the walk builds for the item (S3-AO9)
	}
	detail := "a newer read of this item was only partly readable (" + strings.Join(m.ReadGaps, ", ") + "); the archived copy was kept unchanged"
	if m.ReadError != "" {
		detail += ": " + m.ReadError
	}
	g := state.CaptureGap{
		Token: store, Folder: folderKey, Scope: state.GapScopeItem, Locator: loc,
		Reason: "refresh-failed", Class: state.ClassInfo, Subject: m.Subject, Detail: detail,
	}
	if d := m.Date(); !d.IsZero() {
		g.Received = d.UTC().Format(time.RFC3339)
	}
	e.Manifest.ObserveGap(g, e.runStamp())
}

// probeImproves reports whether any item in oldMissing is now present in the
// source message — the subset rule (a retry is promoted when the old missing set
// is not contained in the new one) evaluated without rendering or writing
// anything. The unknown sentinel always improves: a legacy record is re-captured
// once so it can carry the truth.
func probeImproves(m *model.Message, oldMissing []string) bool {
	var parseGaps []string // this read's parse-gap labels, made when an old one asks
	computed := false
	for _, item := range oldMissing {
		// A fillable parse gap (a part cut short, a multipart that could not be split)
		// improves when a re-read no longer yields it: the complete message has arrived.
		if state.IsParseGapLabel(item) {
			if !computed {
				parseGaps, computed = parseGapLabels(m), true
			}
			found := false
			for _, l := range parseGaps {
				found = found || l == item
			}
			if !found {
				return true
			}
			continue
		}
		// A read-gap label improves when the new read no longer carries it
		// (§4.7.4(2)); state.GapClass owns which labels those are. For
		// unreadable:attachments that also covers a real, empty attachment set.
		if state.IsUnreadableLabel(item) || item == state.LabelUnparsed {
			if !hasReadGap(m, item) {
				return true
			}
			continue
		}
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
func (e *Exporter) placeByPhysID(store string, m *model.Message, fp string) (string, bool) {
	ch := m.ContentDigest()
	if ch == "" {
		return "", false
	}
	base := state.LiveKey(store, m.Identity())
	tokenPrefix := store + "\x00"
	var keys []string
	var recs []state.Record
	for _, s := range e.Manifest.KeysForIdentity(m.Identity()) {
		if !strings.HasPrefix(s.Key, tokenPrefix) {
			continue
		}
		r, ok := e.Manifest.Get(s.Key)
		if !ok {
			continue
		}
		keys = append(keys, s.Key)
		recs = append(recs, r)
	}
	// 1. exact immutable-id match (primary OR an already-known alternate): the same
	//    physical message re-observed — adopt/skip.
	for i := range keys {
		if e.Manifest.HasPhysID(keys[i], m.PhysID) {
			return keys[i], true
		}
	}
	// Require every same-token sibling to be content-comparable; a pre-v6/floor
	// sibling (no content hash) means the identity is not v6-managed -> defer to the
	// floor (§4 step 3). This also keeps the closure off for a pre-v6 archive.
	for i := range recs {
		if recs[i].ContentHash == "" {
			return "", false
		}
	}
	// 2. a content match: the SAME message under a new immutable id — a copy filed
	//    into another folder, or a reissued id (migration). ADD the id to that
	//    record's content-equal set (never flip a single slot, never re-download it
	//    next run) and adopt. No split, no anomaly.
	for i := range keys {
		if recs[i].ContentHash != ch {
			continue
		}
		e.Manifest.AddPhysID(keys[i], m.PhysID)
		return keys[i], true
	}
	// 3. a genuinely DISTINCT reuse (no id match, content differs from every
	//    sibling): key by a hash of the immutable id (§3, HC1). Requalify on a taken
	//    slot so a hash collision never becomes a silent skip.
	if _, taken := e.Manifest.Get(base); !taken {
		return base, true
	}
	key := state.Qualify(base, util.HashHex(m.PhysID, 8))
	for {
		_, taken := e.Manifest.Get(key)
		if !taken || e.Manifest.HasPhysID(key, m.PhysID) {
			return key, true
		}
		key = state.Qualify(key, util.HashHex(m.PhysID, 8))
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

// StemName is a message's file stem by the archive's own convention (baseNameN),
// exported so the copy export names its files the same way and an inheritor sees
// one scheme (extraction design §4.9, XC36). slug is the subject's util.Slug,
// already bounded, or "" under the path budget.
func StemName(date time.Time, slug, key string, n int) string { return baseNameN(date, slug, key, n) }

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

// renderMessage renders one message's page. It is a variable only so a test
// can count the renders (the RV-8 cost guard: one per message); production
// always uses RenderWith.
var renderMessage = RenderWith
