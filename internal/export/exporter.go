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
	Kind    string // "empty-attachment" | "attachment-error" | "unresolved-inline-image"
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

	// OnExported, if set, is called after a message is successfully written
	// (used to feed the search index). relPath is the HTML path relative to
	// OutDir (forward-slashed); key is the manifest key.
	OnExported func(store string, folderPath []string, m *model.Message, relPath, key string)

	Stats  Stats
	Issues []Issue // verification findings (attachments/inline images not fully exported)
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
func (e *Exporter) Export(store string, folderPath []string, m *model.Message) (bool, error) {
	date := m.Date()
	folderKey := strings.Join(folderPath, "/")
	key := state.Key(folderKey, m.Identity())
	fp := m.Fingerprint()

	// A DIFFERENT message reusing an already-archived Message-ID in this folder
	// is a distinct message, not a duplicate: it lives under a content-qualified
	// key. "Different" means the COMPLETE record's fingerprint disagrees — a
	// fillable record is expected to change as its content arrives, so there a
	// mismatch is the fill, not a collision. The record's fingerprint anchors
	// which message owns the plain key, so names stay stable whatever order a
	// later run walks them in (R3/R1).
	prev, seen := e.Manifest.Get(key)
	if seen && prev.Complete() && prev.Fingerprint != "" && prev.Fingerprint != fp {
		key += "#" + fp
		prev, seen = e.Manifest.Get(key)
	}

	retry := false
	if e.Mode == Incremental && seen {
		if !prev.Fillable() {
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

	dirParts := append([]string{e.OutDir, util.SanitizeSegment(store)}, folderPath...)
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
		RootRel:        strings.Repeat("../", 1+len(folderPath)), // store dir + folders
		FolderIndexRel: "index.html",
		RawName:        rawName,
	}
	if hasArchivable(m.Attachments, map[int]bool{}) { // refined below once inline embedding is known
		ctx.ZipName = base + zipSuffix
	}
	htmlBytes, inlineConsumed, err := RenderWith(m, ctx)
	if err != nil {
		return false, fmt.Errorf("render %q: %w", m.Subject, err)
	}
	if ctx.ZipName != "" && !hasArchivable(m.Attachments, inlineConsumed) {
		// Every attachment was embedded inline: no zip will exist, so re-render
		// without the link (cheap: such messages are small).
		ctx.ZipName = ""
		if htmlBytes, inlineConsumed, err = RenderWith(m, ctx); err != nil {
			return false, fmt.Errorf("render %q: %w", m.Subject, err)
		}
	}
	if rawName != "" {
		if err := writeFileAtomic(filepath.Join(dir, rawName), m.Raw); err != nil {
			return false, fmt.Errorf("write %s: %w", rawName, err)
		}
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

	if err := writeFileAtomic(htmlPath, htmlBytes); err != nil {
		return false, fmt.Errorf("write %s: %w", htmlPath, err)
	}

	// Inline images referenced by cid: that we could not embed (missing from the
	// message, e.g. dangling references in a reply/forward chain).
	unresolved := unresolvedInlineRefs(htmlBytes)
	for _, cid := range unresolved {
		e.Stats.UnresolvedInlineRef++
		e.addIssue(folderKey, m, relSlash, "unresolved-inline-image", cid)
	}

	rec := state.Record{Path: relSlash, Folder: folderKey, ExportedAt: time.Now().UTC(), Unresolved: unresolved}
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
	e.Manifest.Add(key, rec)
	if e.OnExported != nil {
		e.OnExported(store, folderPath, m, relSlash, key)
	}
	// Re-exported under a new name (subject or date changed, naming rule
	// changed): the previous html/zip are this message's own and must not
	// linger as duplicates (R6/R13).
	if seen && prev.Path != "" && prev.Path != relSlash {
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
