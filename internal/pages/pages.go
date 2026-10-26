// Package pages generates static, browsable HTML index pages for the export: a
// sortable/filterable index.html in every folder plus a root index.html. They
// need no server — just open them — and complement the full-text search UI.
package pages

import (
	"bytes"
	"fmt"
	"html/template"
	"log"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/index"
)

// pageSize is how many messages one folder page lists; a bigger folder gets
// index.html, index-2.html, … linked to each other, so every message is
// reachable by browsing alone (never truncated). A var so tests can shrink it.
var pageSize = 5000

type row struct {
	File      string
	ZipFile   string
	Subject   string
	From      string
	Date      time.Time
	HasAttach bool
}

type folderInfo struct {
	Dir   string // forward-slashed dir relative to the output root
	Total int
	Shown int
}

// GapBanner is the line the root page shows when the archive may not hold
// everything its source held (fault-isolation §4.10, FC23(7)): fixed text, never
// the text of a gap (a gap's subject and detail are untrusted), and no script.
// An inheritor with no tool reads it and opens the report.
const GapBanner = "Some items from the original mail source may not be in this archive — see attachments-report.tsv"

// GapBannerEncrypted is GapBanner for an encrypted archive, which holds no report
// file: `mailarchive status` lists the items (U16).
const GapBannerEncrypted = "Some items from the original mail source may not be in this archive — run mailarchive status on this archive's folder for the list"

// gapStampAttr is the attribute the root page carries its gap count in, so a
// later run can tell whether the page it wrote says what the manifest now says.
const gapStampAttr = `data-gaps="`

// StampedGaps returns the gap count the root index.html was last generated with,
// and whether the page exists. A page written before the stamp existed reads as
// 0, so an upgrade rewrites nothing by itself.
func StampedGaps(outDir string) (int, bool) {
	f, err := os.Open(filepath.Join(outDir, "index.html"))
	if err != nil {
		return 0, false
	}
	defer f.Close()
	head := make([]byte, 16<<10)
	n, _ := f.Read(head)
	s := string(head[:n])
	i := strings.Index(s, gapStampAttr)
	if i < 0 {
		return 0, true
	}
	rest := s[i+len(gapStampAttr):]
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		return 0, true
	}
	count := 0
	if _, err := fmt.Sscanf(rest[:end], "%d", &count); err != nil {
		return 0, true
	}
	return count, true
}

// Generate writes the folder and root index pages from the search index. gaps is
// the manifest-derived count of what the source held that this archive may not
// (ledger gaps, partly-read and unparsed records; app.gapBannerCount): when
// it is not zero the root page shows GapBanner, and the count is stamped in the
// page so a later run knows when to regenerate it (FC24). EVERY caller passes it
// — a run, a Graph run, reindex and rebuild — so none can drop the banner.
func Generate(outDir string, ix *index.Index, logger *log.Logger, gaps int) error {
	n, err := build(ix, gaps, false, func(rel string, page []byte) error {
		full := filepath.Join(outDir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		return export.WriteFileAtomic(full, page)
	})
	if err != nil {
		return err
	}
	if logger != nil {
		logger.Printf("Wrote %d folder index pages + root index.html", n)
	}
	return nil
}

// Render is Generate in memory, for an encrypted archive's reader
// (design-encrypted-archives-phase3 §7.2): every folder page and the root page,
// keyed by their archive-relative path ("index.html", "Inbox/index-2.html"), worded
// for an encrypted archive — no README, no report file, no plaintext on disk.
func Render(ix *index.Index, gaps int) (map[string][]byte, error) {
	out := map[string][]byte{}
	_, err := build(ix, gaps, true, func(rel string, page []byte) error {
		out[rel] = page
		return nil
	})
	return out, err
}

// build renders every folder page and the root page from the search index, handing
// each to emit by its archive-relative path, and returns how many folders it found.
func build(ix *index.Index, gaps int, encrypted bool, emit func(rel string, page []byte) error) (int, error) {
	var (
		folders []folderInfo
		curDir  string
		curRows []row
		total   int
	)
	now := time.Now().UTC().Format("2006-01-02 15:04 MST")

	flush := func() error {
		if curDir == "" {
			return nil
		}
		if err := buildFolderPages(curDir, curRows, total, now, emit); err != nil {
			return err
		}
		folders = append(folders, folderInfo{Dir: curDir, Total: total, Shown: len(curRows)})
		curRows = nil
		total = 0
		return nil
	}

	err := ix.EachDoc(func(d index.PageRow) error {
		dir := path.Dir(d.Path)
		if dir != curDir {
			if err := flush(); err != nil {
				return err
			}
			curDir = dir
		}
		total++
		{
			curRows = append(curRows, row{
				File:      path.Base(d.Path),
				ZipFile:   strings.TrimSuffix(path.Base(d.Path), ".html") + "-attachments.zip",
				Subject:   d.Subject,
				From:      formatFrom(d.SenderName, d.SenderEmail),
				Date:      d.Date,
				HasAttach: d.HasAttach,
			})
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	if err := flush(); err != nil {
		return 0, err
	}

	sort.Slice(folders, func(i, j int) bool { return folders[i].Dir < folders[j].Dir })
	if gaps < 0 {
		gaps = 0
	}
	page, err := render(rootTemplate, rootPageData{Folders: folders, Generated: now, Gaps: gaps, Encrypted: encrypted})
	if err != nil {
		return 0, err
	}
	if err := emit("index.html", page); err != nil {
		return 0, err
	}
	return len(folders), nil
}

func formatFrom(name, email string) string {
	name = strings.TrimSpace(name)
	email = strings.TrimSpace(email)
	switch {
	case name != "":
		return name
	case email != "":
		return email
	default:
		return "(unknown)"
	}
}

// pageName is the file name of the n-th (1-based) page of a folder.
func pageName(n int) string {
	if n <= 1 {
		return "index.html"
	}
	return fmt.Sprintf("index-%d.html", n)
}

func buildFolderPages(dir string, rows []row, total int, generated string, emit func(rel string, page []byte) error) error {
	pages := (len(rows) + pageSize - 1) / pageSize
	if pages == 0 {
		pages = 1
	}
	for p := 1; p <= pages; p++ {
		lo := (p - 1) * pageSize
		hi := lo + pageSize
		if hi > len(rows) {
			hi = len(rows)
		}
		data := folderPageData{
			Folder:    dir,
			Rows:      rows[lo:hi],
			Total:     total,
			Page:      p,
			Pages:     pages,
			Depth:     strings.Count(dir, "/") + 1, // path back to root
			Generated: generated,
		}
		if p > 1 {
			data.PrevHref = pageName(p - 1)
		}
		if p < pages {
			data.NextHref = pageName(p + 1)
		}
		page, err := render(folderTemplate, data)
		if err != nil {
			return err
		}
		if err := emit(path.Join(dir, pageName(p)), page); err != nil {
			return err
		}
	}
	return nil
}

// render executes a page template into bytes; Generate writes them atomically
// (temp + rename), so a crash never leaves a half-written index page.
func render(tmpl *template.Template, data any) ([]byte, error) {
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type folderPageData struct {
	Folder    string
	Rows      []row
	Total     int
	Page      int
	Pages     int
	PrevHref  string
	NextHref  string
	Depth     int
	Generated string
}

type rootPageData struct {
	Folders   []folderInfo
	Generated string
	Gaps      int
	// Encrypted: the page is an encrypted archive's, rendered by its reader — it
	// names no README or report file, which such an archive does not hold (U16).
	Encrypted bool
}

// RootRelPrefix returns "../" repeated depth times, to link from a folder page
// back toward the output root.
func (d folderPageData) RootRelPrefix() string {
	return strings.Repeat("../", d.Depth)
}

// pagerItem is one slot in the compact numbered pager: a page link, the current
// page (no link), or an ellipsis standing in for a run of omitted pages.
type pagerItem struct {
	Num      int    // page number (0 for an ellipsis)
	Href     string // relative link ("" for the current page and for ellipses)
	Current  bool
	Ellipsis bool
}

// pagerWindow is how many neighbouring pages flank the current one on each side.
const pagerWindow = 2

// Pager returns the compact numbered pager for this page: first, last, the
// current page and a ±pagerWindow band of neighbours, with an ellipsis wherever
// a run of page numbers is skipped (e.g. page 6 of 12 -> 1 … 4 5 6 7 8 … 12).
// It returns nil when there are three pages or fewer — the newer/older links
// already reach every page — so the template shows the numbers only for bigger
// folders. Every link is a plain relative filename, so it works from file://
// with no script. Order is newest-first: page 1 is the newest.
func (d folderPageData) Pager() []pagerItem {
	if d.Pages <= 3 {
		return nil
	}
	show := map[int]bool{1: true, d.Pages: true}
	for n := d.Page - pagerWindow; n <= d.Page+pagerWindow; n++ {
		if n >= 1 && n <= d.Pages {
			show[n] = true
		}
	}
	var items []pagerItem
	prev := 0
	for n := 1; n <= d.Pages; n++ {
		if !show[n] {
			continue
		}
		if prev != 0 && n != prev+1 {
			items = append(items, pagerItem{Ellipsis: true})
		}
		it := pagerItem{Num: n}
		if n == d.Page {
			it.Current = true
		} else {
			it.Href = pageName(n)
		}
		items = append(items, it)
		prev = n
	}
	return items
}

func (r row) DateStr() string {
	if r.Date.IsZero() {
		return ""
	}
	return r.Date.Format("2006-01-02 15:04")
}
