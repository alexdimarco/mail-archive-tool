package server

// The go-back (point-in-time) view: `serve` renders, server-side, either the
// CURRENT mailbox (each message under its Record.Folder, departed messages
// hidden) or the mailbox AS OF a chosen date D (the history log folded to D).
// Both projections are intersected with the files actually on disk, so a
// message removed from the archive (files deleted, then `reindex`) appears at NO
// date (T5/R21). The static file:// pages are never touched — they stay grouped
// by first-captured folder (T3, R13); current-following and go-back are
// serve-only projections. The page carries NO script (fully server-rendered
// under a script-free CSP, R19); every mail-derived string it prints — a folder
// name or subject that a hostile mailbox can shape — reaches the page through
// html/template's contextual auto-escaping, so a folder named `<script>` or a
// control-laden subject is inert text, never markup. A ?at that does not parse
// as YYYY-MM-DD is rejected without reflecting the raw value; an event whose key
// is not a message present on disk surfaces nothing.

import (
	"html/template"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/state"
)

// gobackServer renders the go-back view over the archive at outDir, using the
// search index only to label a message by its subject.
type gobackServer struct {
	outDir string
	ix     *index.Index
}

// gobackPage is the rendered model. Every field is either a tool-controlled
// constant (Availability, the class names) or a value html/template escapes in
// its context (folder names, subjects); AtDate/date-track entries are normalized
// YYYY-MM-DD strings, never the raw ?at bytes.
type gobackPage struct {
	Availability string // "available" | "partial" | "unavailable"
	AvailNote    string
	Current      bool
	AtDate       string   // normalized YYYY-MM-DD when !Current
	BadDate      bool     // the ?at value did not parse — announced, never echoed
	DateTrack    []string // observed run dates, newest first
	Folders      []gobackFolder
	Total        int
	ManifestErr  bool // the manifest could not be read — the current set is unknown
}

type gobackFolder struct {
	Name     string
	Messages []gobackMsg
}

type gobackMsg struct {
	Label string
	URL   template.URL
}

// serveGoback handles GET /goback and GET /goback?at=YYYY-MM-DD.
func (g *gobackServer) serveGoback(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/goback" {
		http.NotFound(w, r)
		return
	}

	hpath := filepath.Join(g.outDir, state.HistoryName)
	stat, statErr := state.HistoryStatus(hpath)
	var page gobackPage
	switch {
	case !stat.Exists:
		page.Availability = "unavailable"
		page.AvailNote = "Go-back is unavailable: this archive has no history timeline (.mailarchive-history.jsonl). Showing the current mailbox only. A live capture (graph) writes the timeline; a one-shot local import has none."
	case statErr != nil || !stat.Clean():
		page.Availability = "partial"
		page.AvailNote = "Go-back is partial: the history timeline is damaged (a torn tail or an unreadable line). Past dates may be incomplete. The next scheduled run repairs a torn tail; `reindex` compacts the log."
	default:
		page.Availability = "available"
		page.AvailNote = "Go-back is available."
	}

	// Decide the view. An at request is honored only when the timeline is not
	// unavailable; a bad date falls back to the current view, announced (never
	// silently current-only, and never echoing the raw value — R19/§3.4).
	// Read the timeline ONCE per request; the fold (at-view) and the date track
	// both reuse this slice instead of re-reading the file (#12, adversarial
	// 2026-09-12). A read error yields no events — the availability banner set
	// above already tells the reader the timeline is partial or unavailable.
	events, _ := state.ReadHistory(hpath)

	atStr := r.URL.Query().Get("at")
	page.Current = true
	if atStr != "" {
		if d, ok := parseGobackDate(atStr); !ok {
			page.BadDate = true
		} else if page.Availability != "unavailable" {
			page.Current = false
			page.AtDate = d.Format("2006-01-02")
			page.buildAtView(g, events, endOfDay(d))
		}
	}

	subjects := g.subjectsByPath()
	if page.Current {
		page.buildCurrentView(g, subjects)
	} else {
		page.labelMessages(subjects)
	}

	// The date track is the observed run cadence, drawn from the already-read log
	// even when a bad date pushed us back to the current view.
	page.DateTrack = state.RunDates(events)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = gobackTmpl.Execute(w, page)
}

// buildCurrentView groups every present-now record under its current folder,
// intersected with the files on disk (T3/T5).
func (p *gobackPage) buildCurrentView(g *gobackServer, subjects map[string]string) {
	m, err := state.Load(filepath.Join(g.outDir, ".mailarchive-manifest.json"))
	if err != nil {
		p.ManifestErr = true
		return
	}
	byFolder := map[string][]gobackMsg{}
	for _, rec := range m.All() {
		if !presentNow(rec) {
			continue // a live record swept gone is hidden in the current view
		}
		if !onDisk(g.outDir, rec.Path) {
			continue // file removed from the archive — absent at every view (R21)
		}
		byFolder[folderLabel(rec.Folder)] = append(byFolder[folderLabel(rec.Folder)],
			gobackMsg{Label: label(subjects, rec.Path), URL: fileURL(rec.Path)})
	}
	p.Folders, p.Total = assembleFolders(byFolder)
}

// buildAtView folds the history to upTo and groups each present-at-D message
// under its THEN-folder, intersected with the manifest (a redacted key is gone)
// and the files on disk (T4/T5/R21). It records the raw path so labelMessages
// can attach subjects after the shared subject map is built.
func (p *gobackPage) buildAtView(g *gobackServer, events []state.HistoryEvent, upTo time.Time) {
	fold := state.FoldEvents(events, upTo)
	m, merr := state.Load(filepath.Join(g.outDir, ".mailarchive-manifest.json"))
	if merr != nil {
		p.ManifestErr = true
		return
	}
	byFolder := map[string][]gobackMsg{}
	for key, fs := range fold {
		if !fs.Present {
			continue // gone as of D (kept its last folder, but hidden here)
		}
		rec, ok := m.Get(key)
		if !ok {
			continue // redacted: the manifest row was pruned — no off-disk surfacing
		}
		if !onDisk(g.outDir, rec.Path) {
			continue // file removed from the archive — absent at every date (R21)
		}
		byFolder[folderLabel(fs.Folder)] = append(byFolder[folderLabel(fs.Folder)],
			gobackMsg{Label: rec.Path, URL: fileURL(rec.Path)}) // Label filled below
	}
	p.Folders, p.Total = assembleFolders(byFolder)
}

// labelMessages replaces the placeholder labels (the raw path) with subjects
// once the shared subject map is available, so buildAtView needs no index access.
func (p *gobackPage) labelMessages(subjects map[string]string) {
	for fi := range p.Folders {
		for mi := range p.Folders[fi].Messages {
			p.Folders[fi].Messages[mi].Label = label(subjects, p.Folders[fi].Messages[mi].Label)
		}
	}
}

// assembleFolders sorts folders by name and messages by label for a stable,
// legible page, and totals the messages shown.
func assembleFolders(byFolder map[string][]gobackMsg) ([]gobackFolder, int) {
	names := make([]string, 0, len(byFolder))
	for n := range byFolder {
		names = append(names, n)
	}
	sort.Strings(names)
	folders := make([]gobackFolder, 0, len(names))
	total := 0
	for _, n := range names {
		msgs := byFolder[n]
		sort.Slice(msgs, func(i, j int) bool { return msgs[i].Label < msgs[j].Label })
		folders = append(folders, gobackFolder{Name: n, Messages: msgs})
		total += len(msgs)
	}
	return folders, total
}

// subjectsByPath builds a path→subject map from the search index so the go-back
// listing shows a subject rather than a filename; a message with no indexed
// subject falls back to its file's base name. An index read error yields an
// empty map (labels fall back), never a failed page.
func (g *gobackServer) subjectsByPath() map[string]string {
	subjects := map[string]string{}
	if g.ix == nil {
		return subjects
	}
	_ = g.ix.EachDoc(func(row index.PageRow) error {
		subjects[row.Path] = row.Subject
		return nil
	})
	return subjects
}

// presentNow reports whether a record belongs in the CURRENT view. A live
// record swept gone (Present=false with a recorded LastSeen) has departed the
// mailbox and is hidden now (visible only at a past date); a folder-scoped local
// record (never part of a live walk, so LastSeen is zero) is always shown.
func presentNow(rec state.Record) bool {
	return rec.Present || rec.LastSeen.IsZero()
}

// onDisk reports whether a record's exported file exists as a regular file under
// the archive root — the intersection that makes redaction span every date
// (T5/R21).
func onDisk(outDir, rel string) bool {
	if rel == "" {
		return false
	}
	st, err := os.Stat(filepath.Join(outDir, filepath.FromSlash(rel)))
	return err == nil && !st.IsDir()
}

// label returns the message's indexed subject, or its file's base name when the
// subject is empty/unknown. html/template escapes the result at render.
func label(subjects map[string]string, relPath string) string {
	if s := strings.TrimSpace(subjects[relPath]); s != "" {
		return s
	}
	if b := path.Base(relPath); b != "." && b != "/" {
		return b
	}
	return "(message)"
}

// folderLabel maps an empty folder to a visible placeholder.
func folderLabel(f string) string {
	if strings.TrimSpace(f) == "" {
		return "(unfiled)"
	}
	return f
}

// fileURL builds the /files/ URL for a slash-relative archive path, percent-
// encoding each segment (the same shape app.js builds). The path is tool-made,
// so the value is safe as a template.URL.
func fileURL(rel string) template.URL {
	parts := strings.Split(rel, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return template.URL("/files/" + strings.Join(parts, "/"))
}

// parseGobackDate parses a ?at value STRICTLY as YYYY-MM-DD in UTC; anything
// else (a time component, an out-of-range field, a traversal attempt) is
// rejected, so a hostile ?at cannot escape the on-disk set or reach the page.
func parseGobackDate(s string) (time.Time, bool) {
	t, err := time.ParseInLocation("2006-01-02", s, time.UTC)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// endOfDay returns the last instant of d (UTC), so folding to a date includes
// every run that happened during that day.
func endOfDay(d time.Time) time.Time {
	return time.Date(d.Year(), d.Month(), d.Day(), 23, 59, 59, int(time.Second-time.Nanosecond), time.UTC)
}

// gobackTmpl renders the page. Contextual auto-escaping is the injection defense
// for every mail-derived value (folder names, subjects).
var gobackTmpl = template.Must(template.New("goback").Parse(`<!DOCTYPE html>
<html lang="en"><head>
<meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>Go back — Archive</title>
<style>
:root{color-scheme:light dark}
*{box-sizing:border-box}
body{margin:0;font-family:-apple-system,Segoe UI,Roboto,Helvetica,Arial,sans-serif;color:#1a1a1a;background:#fff}
@media (prefers-color-scheme:dark){body{color:#e6e6e6;background:#161616}a{color:#7cb0ff}.bar{background:#1e1e1e;border-color:#2c2c2c}.chip{background:#242424;border-color:#3a3a3a}.chip.sel{background:#33507a;border-color:#33507a}.folder{border-color:#2a2a2a}}
.bar{position:sticky;top:0;z-index:5;background:#f7f7f8;border-bottom:1px solid #ddd;padding:12px 16px}
.row{display:flex;gap:12px;align-items:baseline;max-width:1000px;margin:0 auto}
.row a{margin-left:auto;font-size:13px}
.main{max-width:1000px;margin:0 auto;padding:12px 16px 60px}
.avail{padding:8px 12px;border-radius:6px;font-size:13px;margin:8px 0}
.avail-available{background:#e7f6e7}.avail-partial{background:#fff3cd}.avail-unavailable{background:#f8d7da}
@media (prefers-color-scheme:dark){.avail-available{background:#1c331c}.avail-partial{background:#3a3210}.avail-unavailable{background:#3a1c1e}}
.note{background:#fff3cd;padding:8px 12px;border-radius:6px;font-size:13px;margin:8px 0}
@media (prefers-color-scheme:dark){.note{background:#3a3210}}
.track{display:flex;gap:6px;flex-wrap:wrap;align-items:center;margin:10px 0}
.track-label{font-size:12px;color:#888}
.chip{font-size:12px;padding:2px 9px;border:1px solid #ccc;border-radius:12px;background:#eee;text-decoration:none;color:inherit}
.chip.sel{background:#33507a;color:#fff;border-color:#33507a}
h1{font-size:16px;margin:16px 0 6px}
.folder{border:1px solid #eee;border-radius:6px;padding:8px 12px;margin:10px 0}
.folder h2{font-size:14px;margin:2px 0 6px}
.folder ul{margin:0;padding-left:20px}
.folder li{font-size:13px;margin:3px 0}
.empty{color:#888;padding:30px 0;text-align:center}
</style></head>
<body>
<header class="bar"><div class="row"><strong>Archive · point in time</strong><a href="/">Search →</a></div></header>
<main class="main">
<div class="avail avail-{{.Availability}}">{{.AvailNote}}</div>
{{if .BadDate}}<div class="note">That date could not be read — use the YYYY-MM-DD form, or pick one from the date track below.</div>{{end}}
{{if .ManifestErr}}<div class="note">The archive manifest could not be read, so the message set is unknown here. Run <code>mailarchive status</code> for the posture.</div>{{end}}
{{if .DateTrack}}<nav class="track" aria-label="observed run dates"><span class="track-label">Jump to:</span><a class="chip{{if .Current}} sel{{end}}" href="/goback">now</a>{{range .DateTrack}}<a class="chip{{if eq $.AtDate .}} sel{{end}}" href="/goback?at={{.}}">{{.}}</a>{{end}}</nav>{{end}}
<h1>{{if .Current}}The mailbox now{{else}}As of {{.AtDate}}{{end}} · {{.Total}} message{{if ne .Total 1}}s{{end}}</h1>
{{if not .Folders}}<div class="empty">No messages{{if not .Current}} were archived as of {{.AtDate}}{{end}}.</div>{{end}}
{{range .Folders}}<section class="folder"><h2>{{.Name}}</h2><ul>{{range .Messages}}<li><a href="{{.URL}}" target="_blank" rel="noopener">{{.Label}}</a></li>{{end}}</ul></section>
{{end}}
</main>
</body></html>`))
