package server

// pageHTML is the single-page search + reader UI (inline CSS; its script is
// served separately at /app.js so the page runs under script-src 'self'). It
// talks to /api/search, /api/facets, and /files/.
const pageHTML = `<!DOCTYPE html>
<html lang="en"><head>
<meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>Archive Search</title>
<style>
/* Shared MailArchive design tokens (light + dark), aligned with the desktop
   dashboard look: one accent, theme-aware surfaces, system font. */
:root{color-scheme:light dark;--bg:#f3f5f8;--panel:#fff;--text:#18212b;--muted:#667085;--line:#d9e0e8;--accent:#2563eb;--accent-soft:#eaf1ff;--good:#157347;--warn:#a15c00;--danger:#b42318}
@media (prefers-color-scheme:dark){:root{--bg:#0f1319;--panel:#171c24;--text:#eef2f6;--muted:#9aa4b2;--line:#2a3441;--accent:#8aa4ff;--accent-soft:#202943;--good:#62d59a;--warn:#f4c66b;--danger:#ff8a80}}
*{box-sizing:border-box}
body{margin:0;font:15px system-ui,-apple-system,Segoe UI,Roboto,sans-serif;color:var(--text);background:var(--bg)}
a{color:var(--accent)}
button,input,select{font:inherit}
.bar{position:sticky;top:0;z-index:5;background:var(--panel);border-bottom:1px solid var(--line);padding:14px 16px}
.bar::before{content:"";position:absolute;left:0;right:0;top:0;height:3px;background:var(--accent)}
.row{display:flex;gap:9px;flex-wrap:wrap;align-items:center;max-width:1040px;margin:0 auto}
#q{flex:1;min-width:240px;font-size:15px}
input,select{padding:9px 11px;font-size:13px;border:1px solid var(--line);border-radius:8px;background:var(--panel);color:var(--text)}
.filters{max-width:1040px;margin:10px auto 0;display:flex;gap:10px;flex-wrap:wrap;align-items:center;font-size:13px;color:var(--muted)}
.main{max-width:1040px;margin:0 auto;padding:12px 16px 60px}
.meta{color:var(--muted);font-size:13px;margin:12px 2px}
.hit{background:var(--panel);border:1px solid var(--line);border-radius:12px;padding:13px 15px;margin:10px 0}
.hit:hover{background:var(--accent-soft);border-color:color-mix(in srgb,var(--accent) 30%,var(--line))}
.hit .subj{font-weight:700;font-size:15px}
.hit .subj a{text-decoration:none}
.hit .line{color:var(--muted);font-size:13px;margin-top:3px;display:flex;gap:10px;flex-wrap:wrap}
.hit .snip{margin-top:7px;font-size:13px;color:var(--text);opacity:.85;line-height:1.45}
mark{background:var(--accent-soft);color:inherit;padding:0 2px;border-radius:3px}
.pill{background:var(--accent-soft);color:var(--muted);border-radius:999px;padding:2px 9px;font-size:12px}
a.pill{text-decoration:none;color:var(--accent)}
.pager{display:flex;gap:10px;justify-content:center;margin:20px 0}
button{display:inline-flex;align-items:center;justify-content:center;padding:9px 15px;font-weight:650;cursor:pointer;border:1px solid var(--line);border-radius:8px;background:var(--panel);color:var(--text)}
button:hover:not(:disabled){border-color:var(--accent);color:var(--accent)}
button:disabled{opacity:.45;cursor:default}
.empty{color:var(--muted);text-align:center;padding:40px}
</style></head>
<body>
<div class="bar">
  <div class="row">
    <input id="q" type="search" placeholder="Search subject, body, people, attachments…  (try  from:bob after:2025-01 invoice)" autofocus>
    <select id="sort"><option value="relevance">Relevance</option><option value="date">Newest</option></select>
    <a href="/goback" style="font-size:13px;white-space:nowrap">Go back in time →</a>
  </div>
  <div class="filters">
    <label>Folder <select id="folder"><option value="">all</option></select></label>
    <label>Year <select id="year"><option value="">any</option></select></label>
    <label><input type="checkbox" id="attach"> has attachment</label>
    <span id="count" style="margin-left:auto"></span>
  </div>
</div>
<div class="main">
  <div class="meta" id="meta"></div>
  <div id="results"></div>
  <div class="pager"><button id="prev">‹ Prev</button><button id="next">Next ›</button></div>
</div>
<script src="/app.js"></script>
</body></html>`

// appJS is the UI script, served at /app.js.
const appJS = `const $=s=>document.querySelector(s);
let offset=0,limit=50,lastTotal=0;
const state=()=>({q:$('#q').value,sort:$('#sort').value,folder:$('#folder').value,year:$('#year').value,attach:$('#attach').checked?'1':'',limit,offset});

function fileURL(p){return '/files/'+p.split('/').map(encodeURIComponent).join('/');}
function zipURL(p){return fileURL(p.replace(/\.html$/,'-attachments.zip'));}
function fmtDate(s){if(!s)return '';const d=new Date(s);return isNaN(d)?'':d.toISOString().slice(0,10);}
function esc(s){return (s||'').replace(/[&<>]/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;'}[c]));}

async function search(){
  const p=new URLSearchParams(state());
  const r=await fetch('/api/search?'+p);const d=await r.json();
  lastTotal=d.total;render(d);
}
function render(d){
  const box=$('#results');box.innerHTML='';
  $('#meta').textContent=d.total+' result'+(d.total===1?'':'s')+(d.total>limit?'  ·  '+(offset+1)+'–'+Math.min(offset+limit,d.total):'');
  if(!d.results||!d.results.length){box.innerHTML='<div class="empty">No matches.</div>';}
  for(const m of (d.results||[])){
    const el=document.createElement('div');el.className='hit';
    // The snippet is HTML-escaped server-side; only the index's own <mark> tags are live (R19).
    el.innerHTML=
      '<div class="subj"><a href="'+fileURL(m.path)+'" target="_blank" rel="noopener">'+(esc(m.subject)||'(no subject)')+'</a></div>'+
      '<div class="line"><span>'+(esc(m.senderName)||esc(m.senderEmail)||'')+'</span>'+
        '<span>'+fmtDate(m.date)+'</span>'+
        '<span class="pill">'+esc(m.folder)+'</span>'+
        (m.hasAttach?'<a class="pill" href="'+zipURL(m.path)+'">📎 zip</a>':'')+'</div>'+
      (m.snippet?'<div class="snip">'+m.snippet+'</div>':'');
    box.appendChild(el);
  }
  $('#prev').disabled=offset<=0;
  $('#next').disabled=offset+limit>=d.total;
}
async function facets(){
  const d=await(await fetch('/api/facets')).json();
  $('#count').textContent=(d.total||0).toLocaleString()+' messages indexed';
  for(const f of (d.folders||[])){const o=document.createElement('option');o.value=f.folder;o.textContent=f.folder+' ('+f.count+')';$('#folder').appendChild(o);}
  for(const y of (d.years||[])){const o=document.createElement('option');o.value=y;o.textContent=y;$('#year').appendChild(o);}
}
let t;const go=()=>{offset=0;search();};
$('#q').addEventListener('input',()=>{clearTimeout(t);t=setTimeout(go,180);});
for(const id of ['#sort','#folder','#year','#attach'])$(id).addEventListener('change',go);
$('#prev').onclick=()=>{if(offset>0){offset-=limit;search();window.scrollTo(0,0);}};
$('#next').onclick=()=>{if(offset+limit<lastTotal){offset+=limit;search();window.scrollTo(0,0);}};
facets();search();
`
