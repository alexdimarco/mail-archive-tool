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

// Generate writes the folder and root index pages from the search index.
func Generate(outDir string, ix *index.Index, logger *log.Logger) error {
	var (
		folders []folderInfo
		curDir  string
		curRows []row
		total   int
	)

	flush := func() error {
		if curDir == "" {
			return nil
		}
		if err := writeFolderPage(outDir, curDir, curRows, total); err != nil {
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
		return err
	}
	if err := flush(); err != nil {
		return err
	}

	sort.Slice(folders, func(i, j int) bool { return folders[i].Dir < folders[j].Dir })
	if err := writeRootPage(outDir, folders); err != nil {
		return err
	}
	if logger != nil {
		logger.Printf("Wrote %d folder index pages + root index.html", len(folders))
	}
	return nil
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

func writeFolderPage(outDir, dir string, rows []row, total int) error {
	full := filepath.Join(outDir, filepath.FromSlash(dir))
	if err := os.MkdirAll(full, 0o755); err != nil {
		return err
	}
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
			Generated: time.Now().UTC().Format("2006-01-02 15:04 MST"),
		}
		if p > 1 {
			data.PrevHref = pageName(p - 1)
		}
		if p < pages {
			data.NextHref = pageName(p + 1)
		}
		if err := renderFile(filepath.Join(full, pageName(p)), folderTemplate, data); err != nil {
			return err
		}
	}
	return nil
}

func writeRootPage(outDir string, folders []folderInfo) error {
	return renderFile(filepath.Join(outDir, "index.html"), rootTemplate, rootPageData{
		Folders:   folders,
		Generated: time.Now().UTC().Format("2006-01-02 15:04 MST"),
	})
}

// renderFile writes a page atomically (temp + rename), so a crash never leaves
// a half-written index page.
func renderFile(path string, tmpl *template.Template, data any) error {
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return err
	}
	return export.WriteFileAtomic(path, buf.Bytes())
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
}

// RootRelPrefix returns "../" repeated depth times, to link from a folder page
// back toward the output root.
func (d folderPageData) RootRelPrefix() string {
	return strings.Repeat("../", d.Depth)
}

func (r row) DateStr() string {
	if r.Date.IsZero() {
		return ""
	}
	return r.Date.Format("2006-01-02 15:04")
}
