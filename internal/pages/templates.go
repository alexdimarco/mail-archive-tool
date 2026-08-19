package pages

import (
	"html/template"
	"net/url"
	"strings"
)

var tmplFuncs = template.FuncMap{
	// urlpath escapes each path segment so filenames/folders with spaces or
	// special characters link correctly, without escaping the separators.
	"urlpath": func(p string) string {
		parts := strings.Split(p, "/")
		for i, seg := range parts {
			parts[i] = url.PathEscape(seg)
		}
		return strings.Join(parts, "/")
	},
}

const sharedCSS = `
:root{color-scheme:light dark}
body{font-family:-apple-system,Segoe UI,Roboto,Helvetica,Arial,sans-serif;margin:0;color:#1a1a1a;background:#fff}
@media (prefers-color-scheme:dark){body{color:#e6e6e6;background:#181818}a{color:#7cb0ff}
 .bar{background:#222;border-color:#333}tr:hover{background:#222}th{background:#202020}}
.bar{position:sticky;top:0;background:#f7f7f8;border-bottom:1px solid #ddd;padding:12px 20px}
h1{font-size:16px;margin:0 0 4px}
.muted{color:#888;font-size:13px}
.wrap{padding:12px 20px}
input[type=search]{width:100%;max-width:480px;padding:8px 10px;font-size:14px;box-sizing:border-box}
table{border-collapse:collapse;width:100%;font-size:14px}
th,td{text-align:left;padding:6px 10px;border-bottom:1px solid #eee;vertical-align:top}
@media (prefers-color-scheme:dark){th,td{border-color:#2a2a2a}}
th{position:sticky;top:0;background:#f0f0f1;cursor:pointer;user-select:none}
td.date{white-space:nowrap;color:#666;font-variant-numeric:tabular-nums}
td.from{white-space:nowrap;max-width:220px;overflow:hidden;text-overflow:ellipsis}
.note{margin:10px 0;padding:8px 12px;background:#fff6e0;border:1px solid #f0d78a;border-radius:4px;font-size:13px}
@media (prefers-color-scheme:dark){.note{background:#33280d;border-color:#5c4a1a}}
`

const folderTmpl = `<!DOCTYPE html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Folder}}</title><style>` + sharedCSS + `</style></head><body>
<div class="bar">
  <h1>{{.Folder}}</h1>
  <div class="muted">{{.Total}} message(s) · <a href="{{.RootRelPrefix}}index.html">↑ all folders</a></div>
  <div style="margin-top:8px"><input type="search" id="q" placeholder="Filter these messages… (type to narrow)"></div>
</div>
<div class="wrap">
{{if gt .Omitted 0}}<div class="note">Showing the newest {{len .Rows}} of {{.Total}} messages. {{.Omitted}} more are omitted from this page — use full-text search (<code>mailarchive serve</code>) to find them.</div>{{end}}
<table id="t"><thead><tr>
  <th data-i="0">Date</th><th data-i="1">From</th><th data-i="2">Subject</th><th data-i="3">📎</th>
</tr></thead><tbody>
{{range .Rows}}<tr>
  <td class="date">{{.DateStr}}</td>
  <td class="from">{{.From}}</td>
  <td><a href="{{urlpath .File}}">{{if .Subject}}{{.Subject}}{{else}}(no subject){{end}}</a></td>
  <td>{{if .HasAttach}}<a href="{{urlpath .ZipFile}}" title="attachments">📎</a>{{end}}</td>
</tr>{{end}}
</tbody></table>
</div>
<script>
const q=document.getElementById('q'),rows=[...document.querySelectorAll('#t tbody tr')];
q.addEventListener('input',()=>{const s=q.value.toLowerCase();for(const r of rows){r.style.display=r.textContent.toLowerCase().includes(s)?'':'none';}});
document.querySelectorAll('#t th').forEach(th=>th.addEventListener('click',()=>{
  const i=+th.dataset.i,tb=document.querySelector('#t tbody');
  const asc=th._asc=!th._asc;
  [...tb.rows].sort((a,b)=>{const x=a.cells[i].textContent,y=b.cells[i].textContent;return asc?x.localeCompare(y):y.localeCompare(x);}).forEach(r=>tb.appendChild(r));
}));
</script>
</body></html>`

const rootTmpl = `<!DOCTYPE html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>Email archive</title><style>` + sharedCSS + `
ul{list-style:none;padding:0;max-width:760px}
li{display:flex;justify-content:space-between;padding:6px 10px;border-bottom:1px solid #eee}
@media (prefers-color-scheme:dark){li{border-color:#2a2a2a}}
li .c{color:#888;font-variant-numeric:tabular-nums}
</style></head><body>
<div class="bar"><h1>Email archive</h1><div class="muted">Generated {{.Generated}}</div></div>
<div class="wrap">
<div class="note">
  <b>Full-text search:</b> run <code>mailarchive serve -out .</code> in this folder and open the printed
  <code>http://localhost:…</code> URL for ranked search across every message and attachment name.<br>
  Or from a terminal: <code>rg -i "your text" .</code> (ripgrep), or add this folder to Windows Search.
</div>
<h1 style="font-size:15px;margin:16px 0 6px">Folders</h1>
<ul>
{{range .Folders}}<li><a href="{{urlpath .Dir}}/index.html">{{.Dir}}</a><span class="c">{{.Total}}</span></li>{{end}}
</ul>
</div>
</body></html>`

var (
	folderTemplate = template.Must(template.New("folder").Funcs(tmplFuncs).Parse(folderTmpl))
	rootTemplate   = template.Must(template.New("root").Funcs(tmplFuncs).Parse(rootTmpl))
)
