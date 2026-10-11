package desktop

import (
	"html/template"
	"net/http"
	"strings"
)

// The workspace's two page shells and their scripts (extraction design §4.2): the pages
// carry no mailbox data, every number and name arrives through the guarded POST endpoints
// and reaches the DOM only as text (textContent; P17, X15). Every URL on them is relative,
// so they work under LC-C2's per-launch /s/<P>/ prefix (XC23): "workspace", "exports",
// "api/…", "./#sec-m365".

// pageCSS is the dashboard's own look (the Overview's stylesheet, copied: the two must
// stay alike) plus what the workspace's tree, list and cards add.
const pageCSS = `:root{color-scheme:light dark;--bg:#f3f5f8;--panel:#fff;--text:#18212b;--muted:#667085;--line:#d9e0e8;--accent:#2563eb;--accent-soft:#eaf1ff;--good:#157347;--warn:#a15c00;--danger:#b42318;--sidebar:#0f172a;--sidebar-text:#e5e7eb;--sidebar-muted:#94a3b8}
@media(prefers-color-scheme:dark){:root{--bg:#0f1319;--panel:#171c24;--text:#eef2f6;--muted:#9aa4b2;--line:#2a3441;--accent:#8aa4ff;--accent-soft:#202943;--good:#62d59a;--warn:#f4c66b;--danger:#ff8a80;--sidebar:#090d13;--sidebar-text:#eef2f6;--sidebar-muted:#8e99a8}}
*{box-sizing:border-box}
body{margin:0;font:15px system-ui,-apple-system,Segoe UI,Roboto,sans-serif;background:var(--bg);color:var(--text)}
.app-shell{min-height:100vh;display:grid;grid-template-columns:246px minmax(0,1fr)}
.sidebar{background:var(--sidebar);color:var(--sidebar-text);padding:22px 16px;display:flex;flex-direction:column;gap:18px}
.brand{font-weight:800;font-size:1.05rem}.brand span{display:block;color:var(--sidebar-muted);font-size:.78rem;font-weight:500;margin-top:2px}
.nav-title{color:var(--sidebar-muted);font-size:.72rem;font-weight:800;letter-spacing:.08em;text-transform:uppercase;margin:10px 0 4px}
.nav a{display:block;color:var(--sidebar-text);text-decoration:none;padding:9px 11px;border-radius:9px;font-weight:650}
.nav a.active{background:rgba(255,255,255,.11)}.nav a:hover{background:rgba(255,255,255,.08)}
.nav .nav-disabled{display:block;color:var(--sidebar-muted);padding:9px 11px;font-weight:650;cursor:not-allowed}
.sidebar-foot{margin-top:auto;color:var(--sidebar-muted);font-size:.78rem}
.whoami{display:flex;flex-wrap:wrap;gap:4px 18px;color:var(--muted);font-size:.85rem;margin:0 0 14px}
.warnbox{border-color:var(--warn);color:var(--text)}
.content{padding:30px 34px 52px;min-width:0}.inner{max-width:1040px;margin:0 auto}
h1{font-size:1.8rem;letter-spacing:-.02em;margin:0 0 4px}.lede{color:var(--muted);margin:0 0 22px}
.card{background:var(--panel);border:1px solid var(--line);border-radius:14px;padding:19px;margin-bottom:16px}
.card h2{font-size:1.05rem;margin:0 0 12px}.card[id],.anchor{scroll-margin-top:16px}
.row{display:grid;grid-template-columns:180px minmax(0,1fr);gap:12px;padding:8px 0;border-bottom:1px solid var(--line)}
.row:last-child{border-bottom:0}.row .k{color:var(--muted);font-size:.85rem}.row .v{font-weight:650;overflow-wrap:anywhere}
.mono{font-family:ui-monospace,SFMono-Regular,Consolas,monospace}
.btn-link{background:none;border:0;color:var(--accent);font:inherit;font-weight:700;cursor:pointer;padding:0;text-decoration:underline}
.note{margin-top:12px;padding:10px 12px;border-radius:9px;font-size:.9rem;background:var(--accent-soft);border:1px solid color-mix(in srgb,var(--accent) 30%,var(--line))}
.card-sub{color:var(--muted);font-size:.9rem;margin:-4px 0 14px}
.btn{display:inline-flex;align-items:center;justify-content:center;padding:9px 15px;border-radius:8px;border:0;background:var(--accent);color:#fff;font-weight:750;cursor:pointer;text-decoration:none}
.btn.secondary{background:var(--panel);color:var(--accent);border:1px solid var(--accent)}
.btn.small{padding:5px 10px;font-size:.85rem}
.btn:disabled{opacity:.5;cursor:not-allowed}
label{display:block;font-weight:700;margin:14px 0 5px}
input[type=text],input:not([type]){width:100%;padding:10px 11px;border:1px solid var(--line);border-radius:8px;background:var(--panel);color:var(--text)}
.checkline{display:flex;align-items:center;gap:8px;font-weight:600;margin-top:10px}.checkline input{width:auto}
.device-box{margin-top:14px;padding:14px;border:2px dashed var(--accent);border-radius:10px;background:var(--accent-soft)}
.device-code{font:800 1.7rem/1.2 ui-monospace,SFMono-Regular,Consolas,monospace;letter-spacing:.1em;margin-top:6px}
pre{white-space:pre-wrap;overflow-wrap:anywhere;background:var(--bg);border:1px solid var(--line);border-radius:10px;padding:12px;max-height:280px;overflow:auto;margin-top:14px;font-size:.84rem}
.good{color:var(--good)}.warn{color:var(--warn)}.bad{color:var(--danger)}
.step{display:inline-block;width:22px;height:22px;line-height:22px;text-align:center;border-radius:50%;background:var(--accent);color:#fff;font-size:.8rem;margin-right:6px}
.split{display:grid;grid-template-columns:minmax(0,1fr) minmax(0,1.4fr);gap:14px}
.tree{max-height:420px;overflow:auto;border:1px solid var(--line);border-radius:10px;padding:8px}
.tree ul{list-style:none;margin:0;padding-left:18px}.tree>ul{padding-left:0}
.node{display:flex;align-items:center;gap:6px;padding:3px 0}.node .name{cursor:pointer;overflow-wrap:anywhere}.node .name.current{font-weight:800;color:var(--accent)}
.node .cnt{color:var(--muted);font-size:.85rem}.node .tag{font-size:.72rem;color:var(--warn);border:1px solid var(--warn);border-radius:6px;padding:0 5px}
.arrow{width:22px;text-align:center;cursor:pointer;color:var(--muted);user-select:none}
.node button.name,.node button.arrow{background:none;border:0;font:inherit;color:inherit;padding:0 2px;text-align:left}
.node button.name.current{color:var(--accent)}.node button.arrow:disabled{cursor:default}
.node button:focus-visible,.msg input:focus-visible,.run-card button:focus-visible{outline:2px solid var(--accent);outline-offset:2px}
.msgs{max-height:420px;overflow:auto;border:1px solid var(--line);border-radius:10px}
.msg{display:grid;grid-template-columns:24px minmax(0,1fr) 120px;gap:8px;padding:7px 10px;border-bottom:1px solid var(--line);align-items:start}
.msg .subj{font-weight:650;overflow-wrap:anywhere}.msg .sub{color:var(--muted);font-size:.85rem;overflow-wrap:anywhere}.msg .when{color:var(--muted);font-size:.85rem}
.listbar{display:flex;gap:10px;align-items:center;flex-wrap:wrap;margin:8px 0}
.footer-sel{position:sticky;bottom:0;background:var(--panel);border:1px solid var(--line);border-radius:12px;padding:12px 16px;display:flex;gap:12px;align-items:center;flex-wrap:wrap;margin-top:12px}
.run-card{border:1px solid var(--line);border-radius:12px;padding:14px;margin:10px 0}
.run-card h3{margin:0 0 6px;font-size:1rem}.fp{font:700 .95rem ui-monospace,SFMono-Regular,Consolas,monospace;overflow-wrap:anywhere}
.actions{display:flex;gap:8px;flex-wrap:wrap;margin-top:10px}
.bar{height:10px;border-radius:6px;background:var(--line);overflow:hidden;margin:8px 0}.bar>div{height:100%;background:var(--accent)}
ul.plain{margin:6px 0;padding-left:20px}
@media(max-width:900px){.app-shell{grid-template-columns:1fr}.split{grid-template-columns:1fr}.sidebar-foot{display:none}}`

// pageData is what a workspace page is built from.
type pageData struct {
	CSRF      string
	Epoch     string
	Account   string
	ReaderURL string
	Active    string // "workspace" or "exports"
}

var pageFuncs = template.FuncMap{}

// pageTmpl holds both pages, the shared sidebar and the sign-in box.
var pageTmpl = template.Must(template.New("pages").Funcs(pageFuncs).Parse(`
{{define "head"}}<!DOCTYPE html>
<html lang="en"><head>
<meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="csrf" content="{{.CSRF}}"><meta name="epoch" content="{{.Epoch}}">
<title>{{.Title}}</title>
<style>` + "{{.CSS}}" + `</style></head>{{end}}

{{define "sidebar"}}<aside class="sidebar">
    <div class="brand">MailArchive<span>Desktop</span></div>
    <nav class="nav">
      <div class="nav-title">Workspace</div>
      <a href="./">Overview</a>
      <a href="./#sec-archive">Archive now</a>
      {{if .ReaderURL}}<a href="{{.ReaderURL}}" target="_blank" rel="noopener">Open archive</a>
      <a href="{{.ReaderURL}}goback" target="_blank" rel="noopener">Go back in time</a>{{else}}<span class="nav-disabled" title="The archive reader is not running">Open archive</span>
      <span class="nav-disabled" title="The archive reader is not running">Go back in time</span>{{end}}
      <div class="nav-title">Export</div>
      <a href="workspace"{{if eq .Active "workspace"}} class="active"{{end}}>Extraction workspace</a>
      <a href="./#sec-m365">Microsoft 365 connection</a>
      <a href="exports"{{if eq .Active "exports"}} class="active"{{end}}>Export activity</a>
      <div class="nav-title">Diagnostics</div>
      <a href="./#sec-health">Status &amp; health</a>
      <a href="./#sec-activity">Activity log</a>
      <div class="nav-title">Setup</div>
      <a href="./#sec-backup">Weekly backup</a>
    </nav>
    <div class="sidebar-foot">Local control panel<br>127.0.0.1</div>
  </aside>{{end}}

{{define "signinbox"}}<div class="device-box" id="signInBox" hidden>
        <div id="signInBanner" class="whoami"></div>
        <div id="signInDevice" hidden>To sign in, open <a id="signInUrl" target="_blank" rel="noopener">this Microsoft page</a> and enter the code:
          <div class="device-code" id="signInCode"></div></div>
        <div id="signInConfirm" hidden><div id="signInConfirmText"></div>
          <div class="actions"><button class="btn small" id="signInKeep" type="button">Keep</button> <button class="btn small secondary" id="signInOut" type="button">Sign out</button></div></div>
        <div class="card-sub" style="margin:10px 0 0" id="signInNotice"></div>
      </div>{{end}}

{{define "workspace"}}{{template "head" .}}
<body>
<div class="app-shell">
  {{template "sidebar" .}}
  <main class="content"><div class="inner">
    <div class="whoami" id="whoami" role="status"><span>MailArchive Desktop is running as <b>{{.Account}}</b> on this computer.</span></div>
    <h1>Extraction workspace</h1>
    <p class="lede">Copy selected folders and messages from your Microsoft 365 mailbox to a folder, with a manifest the recipient can check.</p>
    <div class="note warnbox" id="banner" role="alert" hidden></div>

    <div class="card" id="sourceCard">
      <h2><span class="step">1</span>Mailbox source</h2>
      <div class="row"><div class="k">Source</div><div class="v">Microsoft 365 — your mailbox</div></div>
      <div class="row"><div class="k">Mailbox</div><div class="v" id="mbox" aria-live="polite">checking…</div></div>
      <p class="card-sub">Shared mailboxes aren't available in this version. MailArchive signs in with read access to your own mailbox only.</p>
      <div id="signInArea" hidden><button class="btn" id="signInBtn" type="button">Sign in</button>
      {{template "signinbox" .}}</div>
      <button class="btn" id="loadFolders" type="button" disabled>Load folders</button>
      <div class="note" id="selNote" role="status" aria-live="polite" hidden></div>
    </div>

    <div class="card" id="pickCard" hidden>
      <div class="split">
        <div><div class="listbar"><label class="checkline" style="margin:0"><input type="checkbox" id="inclSub" checked> Include subfolders</label></div>
          <div class="tree" id="tree" role="group" aria-label="Mailbox folders"></div></div>
        <div><div class="listbar"><button class="btn small secondary" id="selectFolder" type="button" disabled>Select current folder</button>
          <input type="text" id="filter" aria-label="Filter the loaded messages by subject or sender" placeholder="Filter loaded messages by subject / sender" style="max-width:300px"></div>
          <div class="listbar"><label class="checkline" style="margin:0"><input type="checkbox" id="selAll"> Select all loaded</label><span id="shown" class="card-sub" style="margin:0" aria-live="polite"></span></div>
          <div class="msgs" id="msgs" role="group" aria-label="Messages in the open folder"></div>
          <div class="listbar"><button class="btn small secondary" id="loadMore" type="button" hidden>Load more messages</button></div></div>
      </div>
    </div>

    <div class="card" id="destCard" hidden>
      <h2><span class="step">2</span>Export destination</h2>
      <label for="destPath">Destination folder</label>
      <input type="text" id="destPath" spellcheck="false" placeholder="An existing folder, e.g. C:\Users\you\Downloads">
      <div class="listbar"><button class="btn small secondary" id="browse" type="button">Browse…</button><span id="browseNote" class="card-sub" style="margin:0"></span></div>
      <label for="newName">New subfolder name</label>
      <div class="listbar"><input type="text" id="newName" spellcheck="false" style="max-width:320px"><button class="btn small secondary" id="mkdir" type="button">Create folder</button></div>
      <label class="checkline"><input type="checkbox" id="optPreserve" checked> Preserve folder structure</label>
      <p class="card-sub" id="panelNote">Always exports the original EML and a manifest with hashes. Each run creates a separate export directory.</p>
      <div id="retryDest" hidden><p class="card-sub" id="retryDestNote">This is the rest of an earlier export. To send it somewhere else (a full or missing drive, say), choose the folder above, then preview it again.</p>
      <button class="btn small" id="rePreviewBtn" type="button">Preview again for this destination</button></div>
    </div>

    <div class="footer-sel" id="footerSel" hidden>
      <span id="selCount" role="status" aria-live="polite">0 messages selected</span>
      <button class="btn secondary small" id="clearSel" type="button">Clear selection</button>
      <button class="btn" id="previewBtn" type="button" disabled>Preview copy export</button>
    </div>

    <div class="card" id="previewCard" hidden>
      <h2>Preview</h2>
      <div id="previewBody" aria-live="polite"></div>
      <div class="actions"><button class="btn" id="startBtn" type="button">Start export</button> <button class="btn secondary" id="backBtn" type="button">Back</button></div>
    </div>
  </div></main>
</div>
<script src="signin.js"></script>
<script src="workspace.js"></script>
</body></html>{{end}}

{{define "exports"}}{{template "head" .}}
<body>
<div class="app-shell">
  {{template "sidebar" .}}
  <main class="content"><div class="inner">
    <div class="whoami" id="whoami" role="status"><span>MailArchive Desktop is running as <b>{{.Account}}</b> on this computer.</span></div>
    <h1>Export activity</h1>
    <p class="lede">The export that is running, and the exports you have made.</p>
    <div class="note warnbox" id="banner" role="alert" hidden></div>
    <div class="card" id="signInCard" hidden>
      <h2>Sign in to Microsoft 365</h2>
      <p id="signInWhy"></p>
      <button class="btn" id="signInBtn" type="button">Sign in</button>
      {{template "signinbox" .}}
    </div>
    <div class="card" id="runningCard" hidden>
      <h2>Running export</h2>
      <div id="runLabel" class="row"></div>
      <div class="bar" id="runBarWrap" role="progressbar" aria-label="Export progress" aria-valuemin="0" aria-valuemax="100" aria-valuenow="0"><div id="runBar" style="width:0%"></div></div>
      <div id="runStats"></div>
      <div class="actions"><button class="btn secondary small" id="cancelBtn" type="button">Cancel</button></div>
      <pre id="runLog"></pre>
    </div>
    <div class="card" id="lastCard" hidden><h2>Finished</h2><div id="lastBody" aria-live="polite"></div></div>
    <div class="card" id="historyCard">
      <h2>History</h2>
      <div id="history" role="group" aria-label="Exports you have made"></div>
      <p class="card-sub" id="historyEmpty" hidden>No exports yet. Start one in the <a href="workspace">Extraction workspace</a>.</p>
    </div>
  </div></main>
</div>
<script src="signin.js"></script>
<script src="exports.js"></script>
</body></html>{{end}}
`))

// pageHeader is the head's own data.
type pageHead struct {
	pageData
	Title string
	CSS   template.CSS
}

func (d *dashboard) renderPage(w http.ResponseWriter, name, title string) {
	data := pageHead{pageData: pageData{CSRF: d.csrf, Epoch: d.boot, Account: d.cfg.account(), ReaderURL: d.cfg.ReaderURL, Active: name}, Title: title, CSS: template.CSS(pageCSS)}
	var b strings.Builder
	if err := pageTmpl.ExecuteTemplate(&b, name, data); err != nil {
		http.Error(w, "page template error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write([]byte(b.String()))
}

func (d *dashboard) workspacePage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/workspace" {
		http.NotFound(w, r)
		return
	}
	d.renderPage(w, "workspace", "Extraction workspace")
}

func (d *dashboard) exportsPage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/exports" {
		http.NotFound(w, r)
		return
	}
	d.renderPage(w, "exports", "Export activity")
}

func serveJS(src string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Write([]byte(src))
	}
}
