// Package export renders normalized messages to self-contained HTML files with
// per-email attachment archives, mirroring the source folder tree and tracking
// exported items for incremental runs.
package export

import (
	"fmt"
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
	UnresolvedInlineRef int // cid: references in the HTML with no matching image present
	NonHTMLBodies       int // messages exported from plain/RTF because no HTML body existed
	NoBody              int // messages exported with no body content at all
}

// Issue is one verification finding: something referenced but not fully
// exported, so the archive can be audited for completeness.
type Issue struct {
	Folder  string
	Subject string
	RelPath string // exported HTML path relative to OutDir
	Date    time.Time
	Kind    string // "empty-attachment" | "unresolved-inline-image"
	Detail  string // attachment name or cid token
}

// Exporter writes messages to disk and updates the manifest.
type Exporter struct {
	OutDir   string
	Manifest *state.Manifest
	Mode     Mode
	Since    time.Time // zero means no date filter
	Log      *log.Logger

	// OnExported, if set, is called after a message is successfully written
	// (used to feed the search index). relPath is the HTML path relative to
	// OutDir (forward-slashed); key is the manifest key.
	OnExported func(store string, folderPath []string, m *model.Message, relPath, key string)

	Stats  Stats
	Issues []Issue // verification findings (attachments/inline images not fully exported)
}

// Export writes a single message. It returns true if the message was written
// (false when skipped by the date filter or the manifest).
func (e *Exporter) Export(store string, folderPath []string, m *model.Message) (bool, error) {
	date := m.Date()
	if !e.Since.IsZero() && !date.IsZero() && date.Before(e.Since) {
		e.Stats.SkippedDate++
		return false, nil
	}

	folderKey := strings.Join(folderPath, "/")
	key := state.Key(folderKey, m.Identity())
	if e.Mode == Incremental && e.Manifest.Has(key) {
		e.Stats.SkippedManifest++
		return false, nil
	}

	dirParts := append([]string{e.OutDir, util.SanitizeSegment(store)}, folderPath...)
	dir := filepath.Join(dirParts...)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, fmt.Errorf("create output dir %s: %w", dir, err)
	}

	base := baseName(date, m.Subject, key)

	htmlBytes, inlineConsumed, err := Render(m)
	if err != nil {
		return false, fmt.Errorf("render %q: %w", m.Subject, err)
	}
	htmlPath := filepath.Join(dir, base+".html")
	if err := os.WriteFile(htmlPath, htmlBytes, 0o644); err != nil {
		return false, fmt.Errorf("write %s: %w", htmlPath, err)
	}

	rel, err := filepath.Rel(e.OutDir, htmlPath)
	if err != nil {
		rel = htmlPath
	}
	relSlash := filepath.ToSlash(rel)

	e.Stats.AttachmentsInline += len(inlineConsumed)

	if hasArchivable(m.Attachments, inlineConsumed) {
		zipPath := filepath.Join(dir, base+"-attachments.zip")
		n, empty, err := WriteZip(zipPath, m.Attachments, inlineConsumed)
		if err != nil {
			// A failed archive should not abort the whole export.
			e.Log.Printf("warning: attachments for %s: %v", htmlPath, err)
		}
		e.Stats.Attachments += n
		for _, name := range empty {
			e.Stats.AttachmentsEmpty++
			e.addIssue(folderKey, m, relSlash, "empty-attachment", name)
		}
	}

	// Inline images referenced by cid: that we could not embed (missing from the
	// message, e.g. dangling references in a reply/forward chain).
	for _, cid := range unresolvedInlineRefs(htmlBytes) {
		e.Stats.UnresolvedInlineRef++
		e.addIssue(folderKey, m, relSlash, "unresolved-inline-image", cid)
	}
	e.Manifest.Add(key, state.Record{
		Path:       relSlash,
		Folder:     folderKey,
		ExportedAt: time.Now().UTC(),
	})
	if e.OnExported != nil {
		e.OnExported(store, folderPath, m, relSlash, key)
	}

	switch {
	case strings.TrimSpace(m.HTMLBody) == "" && strings.TrimSpace(m.PlainBody) == "" && strings.TrimSpace(m.RTFBody) == "":
		e.Stats.NoBody++
	case strings.TrimSpace(m.HTMLBody) == "":
		e.Stats.NonHTMLBodies++
	}
	e.Stats.Exported++
	return true, nil
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
	ts := "0000-00-00_0000"
	if !date.IsZero() {
		ts = date.UTC().Format("2006-01-02_1504")
	}
	return ts + "_" + util.Slug(subject, 60) + "_" + util.ShortHash(key)
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
