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
	"runtime"
	"sort"
	"strings"
	"time"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/util"
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
	// Home is the folder a Gmail email lives in when this row lists it in another folder it is
	// in (docs/design-gmail-dedup.md §8.4); File and ZipFile then lead there. "" for a row in its
	// own folder.
	Home string
	name string // the file's own name: rows of a folder are listed by it, newest first
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

	// A Gmail email is listed on the page of every folder it is in, linking to its files in its
	// home folder (§8.4 and its GD-21 fold).
	members, err := memberRows(ix)
	if err != nil {
		return 0, err
	}

	flush := func() error {
		if curDir == "" {
			return nil
		}
		if more := members[curDir]; len(more) > 0 {
			curRows = append(curRows, more...)
			total += len(more)
			sort.SliceStable(curRows, func(i, j int) bool { return curRows[i].name > curRows[j].name })
			delete(members, curDir)
		}
		if err := buildFolderPages(curDir, curRows, total, now, emit); err != nil {
			return err
		}
		folders = append(folders, folderInfo{Dir: curDir, Total: total, Shown: len(curRows)})
		curRows = nil
		total = 0
		return nil
	}

	err = ix.EachDoc(func(d index.PageRow) error {
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
				name:      path.Base(d.Path),
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
	// A folder whose every email lives elsewhere (Sent Mail, when its bodies are kept in All
	// Mail) still gets its page.
	rest := make([]string, 0, len(members))
	for dir := range members {
		rest = append(rest, dir)
	}
	sort.Strings(rest)
	for _, dir := range rest {
		curDir = dir
		if err := flush(); err != nil {
			return 0, err
		}
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

// memberRows is every Gmail member row of the index, by the folder page that lists it
// ("store/folder"). The GD-21 limits (docs/design-gmail-dedup.md §8.4 fold): a row's link is built
// only from the email's own recorded path, and only when that path is inside the same store's
// folder as the page; the page's folder must be a clean relative folder of that store, or the row
// is left out (a page is never written outside the store's folder, R4). The link is relative, so
// it works from file:// and in the encrypted reader alike, with no script.
func memberRows(ix *index.Index) (map[string][]row, error) {
	out := map[string][]row{}
	err := ix.EachMember(func(m index.MemberRow) error {
		if !exporterFolder(m.Store) || strings.Contains(m.Store, "/") || !exporterFolder(m.Folder) {
			return nil
		}
		segs := strings.Split(m.Path, "/")
		if len(segs) < 2 || segs[0] != m.Store || !cleanSegments(m.Path) || !strings.HasSuffix(m.Path, ".html") {
			return nil
		}
		dir := m.Store + "/" + m.Folder
		home := strings.Join(segs[1:len(segs)-1], "/")
		if home == m.Folder {
			return nil // its own folder lists it already
		}
		// Gmail's own name, "All Mail", never "[Gmail]/All Mail" (the B3 friction review's F11).
		shown := state.ShownFolder(home)
		if shown == "" {
			shown = "the account's top folder"
		}
		up := strings.Repeat("../", strings.Count(m.Folder, "/")+1)
		rel := up + strings.Join(segs[1:], "/")
		out[dir] = append(out[dir], row{
			File:      rel,
			ZipFile:   strings.TrimSuffix(rel, ".html") + "-attachments.zip",
			Subject:   m.Subject,
			From:      formatFrom(m.SenderName, m.SenderEmail),
			Date:      m.Date,
			HasAttach: m.HasAttach,
			Home:      shown,
			name:      segs[len(segs)-1],
		})
		return nil
	})
	return out, err
}

// cleanSegments reports whether p is a relative slash path whose every segment is a plain name:
// not empty, not "." or "..", with no backslash or control character, and on Windows no colon
// (a drive or a stream name there, never a folder).
func cleanSegments(p string) bool {
	if p == "" {
		return false
	}
	for _, s := range strings.Split(p, "/") {
		if s == "" || s == "." || s == ".." || strings.Contains(s, `\`) || (runtime.GOOS == "windows" && strings.Contains(s, ":")) {
			return false
		}
		for _, r := range s {
			if r < 0x20 || r == 0x7f {
				return false
			}
		}
	}
	return true
}

// exporterFolder reports whether p, a store or a folder a page is written for, is clean
// (cleanSegments) and each of its segments is what the exporter's name rule (util.SanitizeSegment,
// which the local walk applies to every folder) writes: so no name Windows spells another way
// ("INBOX." is INBOX's own folder there, ".. " is "..") becomes a page's folder (the B3 adversarial
// pass's A2).
func exporterFolder(p string) bool {
	if !cleanSegments(p) {
		return false
	}
	for _, s := range strings.Split(p, "/") {
		if util.SanitizeSegment(s) != s {
			return false
		}
	}
	return true
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
