package server

// pageHTML is the single-page search + reader UI. It is self-contained (inline
// CSS/JS) and talks to /api/search, /api/facets, and /files/.
const pageHTML = `<!DOCTYPE html>
<html lang="en"><head>
<meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>Archive Search</title>
<style>
:root{color-scheme:light dark}
*{box-sizing:border-box}
body{margin:0;font-family:-apple-system,Segoe UI,Roboto,Helvetica,Arial,sans-serif;color:#1a1a1a;background:#fff}
@media (prefers-color-scheme:dark){body{color:#e6e6e6;background:#161616}a{color:#7cb0ff}
 .bar,.filters{background:#1e1e1e;border-color:#2c2c2c}.hit{border-color:#2a2a2a}.hit:hover{background:#1f1f1f}
 input,select{background:#222;color:#e6e6e6;border-color:#3a3a3a}mark{background:#5a4a00;color:#ffe9a8}}
.bar{position:sticky;top:0;z-index:5;background:#f7f7f8;border-bottom:1px solid #ddd;padding:12px 16px}
.row{display:flex;gap:8px;flex-wrap:wrap;align-items:center;max-width:1000px;margin:0 auto}
#q{flex:1;min-width:240px;padding:9px 11px;font-size:15px}
input,select{padding:7px 9px;font-size:13px;border:1px solid #ccc;border-radius:4px}
.filters{max-width:1000px;margin:8px auto 0;display:flex;gap:8px;flex-wrap:wrap;align-items:center;font-size:13px;color:#666}
.main{max-width:1000px;margin:0 auto;padding:8px 16px 60px}
.meta{color:#888;font-size:13px;margin:10px 2px}
.hit{border:1px solid #eee;border-radius:6px;padding:10px 12px;margin:8px 0}
.hit:hover{background:#fafafa}
.hit .subj{font-weight:600;font-size:15px}
.hit .subj a{text-decoration:none}
.hit .line{color:#666;font-size:13px;margin-top:2px;display:flex;gap:10px;flex-wrap:wrap}
.hit .snip{margin-top:6px;font-size:13px;color:#444;line-height:1.4}
@media (prefers-color-scheme:dark){.hit .snip{color:#bbb}}
mark{background:#fff2a8;padding:0 1px;border-radius:2px}
.pill{background:#eee;border-radius:10px;padding:1px 8px;font-size:12px}
@media (prefers-color-scheme:dark){.pill{background:#2a2a2a}}
.pager{display:flex;gap:10px;justify-content:center;margin:18px 0}
button{padding:7px 14px;font-size:14px;cursor:pointer;border:1px solid #ccc;border-radius:4px;background:#fff}
@media (prefers-color-scheme:dark){button{background:#222;color:#e6e6e6;border-color:#3a3a3a}}
button:disabled{opacity:.4;cursor:default}
.empty{color:#888;text-align:center;padding:40px}
</style></head>
<body>
<div class="bar">
  <div class="row">
    <input id="q" type="search" placeholder="Search subject, body, people, attachments…  (try  from:bob after:2025-01 invoice)" autofocus>
    <select id="sort"><option value="relevance">Relevance</option><option value="date">Newest</option></select>
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
<script>
const $=s=>document.querySelector(s);
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
    // snippet from server may include <mark>; it is trusted text from our own index.
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
</script>
</body></html>`
