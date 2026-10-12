package desktop

// The workspace's scripts. Each is one static file under script-src 'self', with no inline
// handler. They build the DOM with createElement and textContent only: no innerHTML,
// outerHTML, insertAdjacentHTML, document.write, eval or Function (X15, MA-407), so a
// subject or a folder name that holds markup is shown as the text it is.

// commonJS is the little every script shares: the guarded POST and its error text.
const commonJS = `const csrf=document.querySelector('meta[name=csrf]').content;
const epoch=(document.querySelector('meta[name=epoch]')||{}).content||'';
const $=id=>document.getElementById(id);
const RESTART='MailArchive Desktop restarted — reload the page; your selection can\'t be reused.';
function api(path,body){
  return fetch(path,{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':csrf,'X-Epoch':epoch},body:JSON.stringify(body||{})})
    .then(r=>r.json().then(d=>{d.status=r.status;return d;}).catch(()=>({ok:false,id:'R-y',error:RESTART,status:r.status})))
    .catch(e=>({ok:false,error:'MailArchive Desktop could not be reached — is it still running?'}));
}
function el(tag,text,cls){const e=document.createElement(tag);if(text!==undefined&&text!==null)e.textContent=text;if(cls)e.className=cls;return e;}
function clear(e){while(e.firstChild)e.removeChild(e.firstChild);}
function show(id,text){const e=$(id);if(!e)return;e.hidden=!text;e.textContent=text||'';}
function fmt(n){return Number(n).toLocaleString('en-US');}
`

// scoped puts a whole script, its shared helpers included, inside one function. Classic
// scripts on one page share a single global lexical scope, so two that each declared
// `const csrf` at the top level (the Overview's dashboard.js and signin.js; the workspace's
// signin.js and workspace.js) made the second one a SyntaxError that never ran (F27).
func scoped(body string) string { return "(function(){\n" + body + "\n})();\n" }

// signinJS drives the sign-in box on the Overview's Microsoft 365 card, on the workspace page
// and on Export activity: it starts the sign-in, shows the code the guarded status POST
// returns, and asks the weekly-backup question when the account changes (XC7, XC8).
var signinJS = scoped(commonJS + `(function(){
const btn=$('signInBtn');if(!btn)return;
let timer=null;
// started: this page started the sign-in or saw it running. Only then does a 'done' reload the
// page; a page that merely loaded never reloads for a sign-in that finished earlier (F28).
let started=false;
function render(d){
  if(d.state==='running'||d.state==='confirm')started=true;
  const box=$('signInBox');box.hidden=false;
  const b=$('signInBanner');if(b&&d.account)b.textContent='MailArchive Desktop is running as '+d.account+' on this computer.';
  const n=$('signInNotice');if(n&&d.notice)n.textContent=d.notice;
  const dev=$('signInDevice');
  if(d.state==='running'&&d.device){dev.hidden=false;$('signInCode').textContent=d.device.code;
    const u=$('signInUrl');if(/^https:\/\//.test(d.device.url))u.href=d.device.url;}
  else dev.hidden=true;
  const c=$('signInConfirm');
  if(d.state==='confirm'&&d.confirm){c.hidden=false;$('signInConfirmText').textContent=d.confirm.text;}else c.hidden=true;
  if(d.state==='done'&&started){stop();btn.disabled=false;
    show('banner',d.signedOut?'You chose Sign out: the new sign-in was not saved.':'Signed in as '+d.upn+'.');
    setTimeout(()=>location.reload(),900);}
  if(d.state==='failed'&&started){stop();btn.disabled=false;show('banner',d.error||'The sign-in did not finish.');}
}
function poll(answer,initial){
  api('api/m365/signin-status',answer?{answer:answer}:(initial?{initial:true}:{})).then(d=>{
    if(!d.ok){stop();btn.disabled=false;show('banner',d.error||RESTART);return;}
    render(d);
    // a page reloaded while a sign-in is running picks the polling back up
    if(initial&&started&&!timer&&(d.state==='running'||d.state==='confirm')){btn.disabled=true;timer=setInterval(()=>poll(),1500);}
  });
}
function stop(){if(timer){clearInterval(timer);timer=null;}}
btn.addEventListener('click',()=>{
  btn.disabled=true;show('banner','');
  api('api/m365/signin',{}).then(d=>{
    if(!d.ok){btn.disabled=false;show('banner',d.error||'Could not start the sign-in.');return;}
    started=true;poll();if(!timer)timer=setInterval(()=>poll(),1500);
  });
});
const k=$('signInKeep'),o=$('signInOut');
if(k)k.addEventListener('click',()=>poll('keep'));
if(o)o.addEventListener('click',()=>poll('signout'));
poll(undefined,true);
})();
`)

// workspaceJS is the Extraction workspace page: the source, the folder tree and its
// selection, the message list, the destination, the options and the preview.
var workspaceJS = scoped(commonJS + `(function(){
const st={upn:'',src:'m365',archivePath:'',archAvail:{},m365Ready:false,folders:[],byId:{},kids:{},open:{},entries:[],picks:{},current:'',rows:[],more:false,cursor:'',total:0,previewId:'',countTimer:null,running:false,counts:0};
function errText(d){return d.error||'Something went wrong.';}
function showErr(d){
  show('banner',errText(d));
  // after these the page's folders are out of date: Load folders is the way on (R-l2 for an archive that moved)
  if(d&&(d.id==='R-j2'||d.id==='R-y'||d.id==='R-l'||d.id==='R-l2'||d.id==='R-b'||d.id==='R-c'||d.id==='R-o'||d.id==='R-o2'||d.id==='R-o3'||d.id==='R-o4'||d.id==='R-p'))$('loadFolders').disabled=false;
  // the refusals that say "sign in" put the Sign in button in front of the owner (F2)
  if(d&&st.src==='m365'&&(d.id==='R-b'||d.id==='R-c'||d.id==='R-l'))$('signInArea').hidden=false;
}

// ---- the source: Microsoft 365, the configured archive, or another archive folder (read-only) ----
// What a request says its source is: the archive path is the folder the selection was made from, as
// "Load folders" reported it, so the server can tell a selection made in an archive that has since moved.
function srcReq(loading){
  if(st.src==='m365')return {kind:'m365',mailbox:st.upn};
  if(st.src==='archive-other')return {kind:'archive-other',archive:loading?$('otherPath').value:st.archivePath};
  return {kind:'archive',archive:loading?'':st.archivePath};
}
const UNIT={m365:'items Microsoft 365 reports',archive:'records the archive lists','archive-other':'records the archive lists'};
function panelText(extra,arch){
  if(!arch&&st.src==='m365')return 'Always exports the original EML and a manifest with hashes. Each run creates a separate export directory.';
  return 'Exports the original EML where your archive kept one'+(extra||'')+'; otherwise the archived page and attachments. Always writes a manifest with hashes. Each run creates a separate export directory.';
}
function renderSource(){
  const k=st.src,a=st.archAvail||{};
  $('m365Part').hidden=k!=='m365';$('archivePart').hidden=k==='m365';$('otherPart').hidden=k!=='archive-other';
  if(k==='archive')show('archLine',a.state==='ready'?('MailArchive archive — '+a.path):(a.note||'There is no archive yet.'));
  if(k==='archive-other')show('archLine',st.otherHolds||'Another archive folder — read-only');
  $('loadFolders').disabled=k==='m365'?!st.m365Ready:(k==='archive'?(a.state!=='ready'&&a.state!=='gone'):false);
  $('panelNote').textContent=panelText('');
}
function resetPick(){
  st.entries=[];st.picks={};st.current='';st.rows=[];st.open={};st.folders=[];st.byId={};st.kids={};st.previewId='';st.archivePath='';st.otherHolds='';
  $('pickCard').hidden=true;$('destCard').hidden=true;$('footerSel').hidden=true;$('previewCard').hidden=true;
  show('selNote','');show('listNote','');clear($('tree'));clear($('msgs'));
}
$('srcKind').addEventListener('change',()=>{
  st.src=$('srcKind').value;resetPick();show('banner','');renderSource();
});
$('otherPath').addEventListener('input',()=>{if(st.archivePath){resetPick();}$('otherNote').textContent='';});
// Browse chooses the other archive folder, and checking it names the mailboxes it holds, or says why it is not one
$('browseOther').addEventListener('click',()=>{
  $('otherNote').textContent='A folder picker has opened — it may be behind this window.';
  api('api/workspace/browse',{title:'Choose the archive\'s top folder'}).then(d=>{
    $('otherNote').textContent='';
    if(d.path){$('otherPath').value=d.path;checkOther();}
    else if(d.unavailable||d.disabled||d.busy)show('banner',errText(d));
  });
});
function checkOther(){
  const p=$('otherPath').value;if(!p)return;
  api('api/workspace/source',{otherArchive:p}).then(d=>{
    if(!d.ok){showErr(d);return;}
    const a=d.archive||{};
    st.otherHolds='Another archive folder — '+a.path+(a.stores&&a.stores.length?' (holds: '+a.stores.map(s=>s.label).join(', ')+')':'')+', read-only';
    show('archLine',st.otherHolds);show('banner',a.note||'');
  });
}
$('otherPath').addEventListener('change',checkOther);

// ---- source ------------------------------------------------------------
function loadSource(){
  api('api/workspace/source',{}).then(d=>{
    if(!d.ok){showErr(d);return;}
    st.running=!!d.running;
    const m=d.m365||{};
    st.archAvail=d.archive||{};
    if(m.state==='ready'){st.upn=m.upn;st.m365Ready=true;show('mbox','Mailbox: '+m.upn+' (your own mailbox)');}
    else if(m.state==='needs-sign-in'){show('mbox',m.note||'Microsoft 365 refused your saved sign-in. Click Sign in to sign in again.');$('signInArea').hidden=false;}
    else if(m.state==='not-signed-in'){show('mbox','Not signed in to Microsoft 365');$('signInArea').hidden=false;}
    else{show('mbox','Microsoft 365 isn\'t set up yet — its settings are saved by MailArchive\'s command-line tool (a separate download): run mailarchive setup there, then come back. To read an archive you were given, or your own, choose it in the Source list above: no Microsoft 365 is needed for that.');
      if(st.src==='m365'){const k=(st.archAvail&&st.archAvail.state==='ready')?'archive':'archive-other';$('srcKind').value=k;st.src=k;}}
    renderSource();
    const s=d.settings||{};
    if(s.dest&&!$('destPath').value)$('destPath').value=s.dest;
    $('optPreserve').checked=s.preserve!==false;
    $('optHTML').checked=s.html!==false;
    $('optAttach').checked=s.attachments===true;
    $(s.attachmentsAs==='loose'?'optAttachLoose':'optAttachZip').checked=true;
    syncAttachMode();
    if(st.running)$('browseNote').textContent='Browse is off while an export runs — type the folder\'s path instead.';
    const h=location.hash||'';
    if(h.indexOf('#preview=')===0){resumePreview(h.substring(9));}
  });
}
$('loadFolders').addEventListener('click',()=>{
  $('loadFolders').disabled=true;show('banner','');
  api('api/workspace/folders',{source:srcReq(true)}).then(d=>{
    $('loadFolders').disabled=false;
    if(!d.ok){showErr(d);return;}
    if(st.src==='m365')st.upn=d.upn;
    else{
      st.archivePath=d.archive;
      const holds=(d.stores||[]).map(s=>s.label).join(', ');
      show('archLine',(st.src==='archive'?'MailArchive archive — ':'Another archive folder — ')+d.archive+(holds?' (holds: '+holds+')':'')+(st.src==='archive-other'?', read-only':''));
    }
    d.folders.forEach(f=>{if(f.label)f.name=f.label;}); // an archive's store is shown with the mailbox it holds
    st.folders=d.folders;st.byId={};st.kids={};
    d.folders.forEach(f=>{st.byId[f.id]=f;const p=f.parent||'';(st.kids[p]=st.kids[p]||[]).push(f.id);});
    st.entries=[];st.picks={};st.current='';st.rows=[];st.open={};
    $('pickCard').hidden=false;$('destCard').hidden=false;$('footerSel').hidden=false;
    renderTree();renderRows();updateCount();
    if(d.note)show('banner',d.note);
  });
});

// ---- the tree ------------------------------------------------------------
function covering(id){
  for(const e of st.entries){
    if(e.id===id)return e;
    if(e.subtree&&isUnder(id,e.id))return e;
  }
  return null;
}
function isUnder(id,anc){
  let cur=st.byId[id];let n=0;
  while(cur&&cur.parent&&n++<400){if(cur.parent===anc)return true;cur=st.byId[cur.parent];}
  return false;
}
// The tree and the list are redrawn after every tick, open or close. A redraw keeps what is
// open (st.open) and puts the keyboard focus back on the same control (data-key), so a
// keyboard user is not thrown to the top of the page, and a mouse user's open folders stay open.
function keepFocus(root,draw){
  const a=document.activeElement;
  const key=(a&&root.contains(a)&&a.dataset)?a.dataset.key:'';
  draw();
  if(!key)return;
  for(const n of root.querySelectorAll('[data-key]')){if(n.dataset.key===key){n.focus();break;}}
}
function renderTree(){keepFocus($('tree'),drawTree);}
function drawTree(){
  const root=$('tree');clear(root);
  const ul=el('ul');
  (st.kids['']||[]).forEach(id=>ul.appendChild(nodeEl(id)));
  root.appendChild(ul);
}
function nodeEl(id){
  const f=st.byId[id];const li=el('li');
  const row=el('div',null,'node');
  const kids=st.kids[id]||[];
  const arrow=el('button',kids.length?(st.open[id]?'▾':'▸'):'','arrow');arrow.type='button';arrow.dataset.key='a:'+id;
  if(kids.length){
    arrow.setAttribute('aria-expanded',st.open[id]?'true':'false');
    arrow.setAttribute('aria-label',(st.open[id]?'Hide':'Show')+' the subfolders of '+f.name);
  }else{arrow.disabled=true;arrow.tabIndex=-1;arrow.setAttribute('aria-hidden','true');}
  const cb=el('input');cb.type='checkbox';cb.dataset.key='c:'+id;
  const cov=covering(id);
  cb.checked=!!cov;
  if(cov&&cov.id!==id)cb.disabled=true;
  cb.setAttribute('aria-label','Select '+f.name+' ('+fmt(f.count)+' messages)'+((cov&&cov.id!==id)?', included with its parent folder':''));
  cb.addEventListener('change',()=>toggleFolder(id,cb.checked));
  const name=el('button',f.name,'name'+(st.current===id?' current':''));name.type='button';name.dataset.key='n:'+id;
  name.setAttribute('aria-label','Open '+f.name+' to see its messages');
  if(st.current===id)name.setAttribute('aria-current','true');
  name.addEventListener('click',()=>openFolder(id));
  const cnt=el('span','('+fmt(f.count)+')','cnt');
  row.appendChild(arrow);row.appendChild(cb);row.appendChild(name);row.appendChild(cnt);
  if(f.wellKnown){row.appendChild(el('span',f.wellKnown==='deleted'?'Deleted Items':'Junk Email','tag'));}
  li.appendChild(row);
  if(kids.length){
    const sub=el('ul');sub.hidden=!st.open[id];
    kids.forEach(k=>sub.appendChild(nodeEl(k)));
    li.appendChild(sub);
    arrow.addEventListener('click',()=>{st.open[id]=!st.open[id];renderTree();});
  }
  return li;
}
function toggleFolder(id,on){
  if(on){if(!covering(id))st.entries.push({id:id,subtree:$('inclSub').checked,exclude:[]});}
  else{st.entries=st.entries.filter(e=>e.id!==id);}
  renderTree();renderRows();updateCount();
}
// The messages a folder holds, with its subfolders' when they are included: the number the note
// under Selected says (it used to say the folder's own count beside "and its subfolders").
function subtreeCount(id){
  let n=(st.byId[id]||{}).count||0;
  (st.kids[id]||[]).forEach(k=>{n+=subtreeCount(k);});
  return n;
}
function selectionNote(id){
  const f=st.byId[id];const withSub=$('inclSub').checked&&(st.kids[id]||[]).length>0;
  return 'Selected '+fmt(withSub?subtreeCount(id):f.count)+' messages from '+f.name+(withSub?' and its subfolders.':'.');
}
$('selectFolder').addEventListener('click',()=>{
  if(!st.current)return;
  if(!covering(st.current))st.entries.push({id:st.current,subtree:$('inclSub').checked,exclude:[]});
  show('selNote',selectionNote(st.current));
  renderTree();renderRows();updateCount();
});
// Include subfolders applies to every folder that is selected, now and later, so the box means
// what it says: unticking it takes the subfolders out of the folders already ticked.
$('inclSub').addEventListener('change',()=>{
  const on=$('inclSub').checked;
  st.entries.forEach(e=>{e.subtree=on;});
  if(st.current&&covering(st.current))show('selNote',selectionNote(st.current));
  renderTree();renderRows();updateCount();
});

// ---- the message list ----------------------------------------------------------
function openFolder(id){
  st.current=id;st.rows=[];st.cursor='';st.more=false;
  $('selectFolder').disabled=false;
  renderTree();loadRows(false);
}
function loadRows(more){
  const body={folder:st.current,source:srcReq()};
  if(more){body.cursor=st.cursor;if(st.rows.length)body.after=st.rows[st.rows.length-1].h;}
  api('api/workspace/messages',body).then(d=>{
    if(!d.ok){
      showErr(d);
      if(d.reload&&more){st.rows=[];loadRows(false);}
      return;
    }
    st.rows=st.rows.concat(d.rows);st.more=d.more;st.cursor=d.cursor||'';st.total=d.count;
    // an archive's list may be unavailable (no index, a newer one, a rebuild running) or incomplete: the
    // note says so, and whole-folder selection from the manifest still works
    show('listNote',d.listUnavailable||d.listNote||'');
    renderRows();
  });
}
$('loadMore').addEventListener('click',()=>loadRows(true));
function rowChecked(h){
  const cov=covering(st.current);
  if(cov)return cov.exclude.indexOf(h)<0;
  return !!st.picks[h];
}
function setRow(h,on){
  const cov=covering(st.current);
  if(cov){const i=cov.exclude.indexOf(h);if(on&&i>=0)cov.exclude.splice(i,1);if(!on&&i<0)cov.exclude.push(h);}
  else{if(on)st.picks[h]=true;else delete st.picks[h];}
}
function renderRows(){keepFocus($('msgs'),drawRows);}
function drawRows(){
  const box=$('msgs');clear(box);
  const q=$('filter').value.toLowerCase();
  let shown=0;
  st.rows.forEach(r=>{
    if(q&&(r.subject+' '+r.from+' '+r.fromName).toLowerCase().indexOf(q)<0)return;
    shown++;
    const row=el('div',null,'msg');
    const cb=el('input');cb.type='checkbox';cb.checked=rowChecked(r.h);cb.dataset.key='m:'+r.h;
    cb.setAttribute('aria-label','Select the message '+(r.subject||'(no subject)')+(r.fromName?' from '+r.fromName:''));
    cb.addEventListener('change',()=>{setRow(r.h,cb.checked);updateCount();});
    const mid=el('div');
    mid.appendChild(el('div',r.subject||'(no subject)','subj'));
    mid.appendChild(el('div',(r.fromName?r.fromName+' ':'')+(r.from?'<'+r.from+'>':''),'sub'));
    row.appendChild(cb);row.appendChild(mid);
    row.appendChild(el('div',r.received?new Date(r.received).toLocaleString():'','when'));
    box.appendChild(row);
  });
  $('loadMore').hidden=!st.more;
  $('shown').textContent=st.current?('Showing '+fmt(st.rows.length)+' of '+fmt(st.total)):'';
  $('selAll').checked=false;
}
$('filter').addEventListener('input',renderRows);
$('selAll').addEventListener('change',()=>{
  const on=$('selAll').checked;
  st.rows.forEach(r=>setRow(r.h,on));
  renderRows();$('selAll').checked=on;updateCount();
});

// ---- the selection and its count -----------------------------------------------
// the way attachments are saved is chosen only while Extract attachments is on
function syncAttachMode(){const on=$('optAttach').checked;$('optAttachZip').disabled=!on;$('optAttachLoose').disabled=!on;}
$('optAttach').addEventListener('change',syncAttachMode);
function selection(){
  const messages=Object.keys(st.picks).map(h=>({h:h}));
  return {source:srcReq(),folders:st.entries.map(e=>({id:e.id,subtree:e.subtree,exclude:e.exclude})),messages:messages,
    dest:$('destPath').value,options:{preserve:$('optPreserve').checked,html:$('optHTML').checked,attachments:$('optAttach').checked,attachmentsAs:$('optAttachLoose').checked?'loose':'zip'}};
}
function updateCount(){
  clearTimeout(st.countTimer);
  const empty=!st.entries.length&&!Object.keys(st.picks).length;
  $('previewBtn').disabled=empty;
  if(empty){$('selCount').textContent='0 messages selected';return;}
  st.countTimer=setTimeout(()=>{
    api('api/workspace/count',selection()).then(d=>{
      if(!d.ok){showErr(d);return;}
      // One export holds up to 200,000 messages: say so now, not after Preview has read them all.
      // One folder alone cannot be split across exports in this version: "select fewer folders" is no remedy (F15).
      const oneFolder=st.entries.length===1&&!Object.keys(st.picks).length;
      const over=oneFolder?' — more than the 200,000 one export can hold, and one folder can\'t be split across exports yet: pick up to 10,000 of its messages by hand, or export its subfolders one at a time'
        :' — more than the 200,000 one export can hold: select fewer folders or messages';
      $('selCount').textContent=fmt(d.count)+' messages selected ('+UNIT[st.src]+')'+(d.count>200000?over:'');
    });
  },300);
}
$('clearSel').addEventListener('click',()=>{
  if(!st.entries.length&&!Object.keys(st.picks).length)return;
  if(!confirm('Clear the whole selection?'))return;
  st.entries=[];st.picks={};show('selNote','');renderTree();renderRows();updateCount();
});

// ---- destination -------------------------------------------------------------------
$('browse').addEventListener('click',()=>{
  $('browseNote').textContent='A folder picker has opened — it may be behind this window.';
  api('api/workspace/browse',{title:'Choose the folder to export into'}).then(d=>{
    $('browseNote').textContent='';
    if(d.path)$('destPath').value=d.path;
    else if(d.unavailable||d.disabled||d.busy)show('banner',errText(d));
  });
});
$('mkdir').addEventListener('click',()=>{
  api('api/workspace/mkdir',{parent:$('destPath').value,name:$('newName').value}).then(d=>{
    if(!d.ok){showErr(d);return;}
    $('destPath').value=d.path;$('newName').value='';show('banner','');
  });
});

// ---- preview and start --------------------------------------------------------------
$('previewBtn').addEventListener('click',()=>{
  show('banner','');
  api('api/workspace/preview',selection()).then(d=>{
    if(!d.ok){showErr(d);return;}
    resumePreview(d.previewId);
  });
});
function resumePreview(id){
  st.previewId=id;$('previewCard').hidden=false;
  const body=$('previewBody');clear(body);body.appendChild(el('p','Reading your folders…'));
  const tick=()=>api('api/workspace/preview-status',{previewId:id}).then(d=>{
    if(d.retryOf)retryDestination(d.retryOf,d.retryWhich,'');
    if(!d.ok&&d.state!=='failed'){showErr(d);$('previewCard').hidden=true;return;}
    if(d.state==='running'){clear(body);body.appendChild(el('p',d.progress||'Reading your folders…'));setTimeout(tick,1000);return;}
    if(d.state==='failed'){showErr(d);$('previewCard').hidden=true;return;}
    renderPreview(d.result);
  });
  tick();
}
// A retry (Export the remaining messages, Export failed messages again) is previewed here too.
// It needs no folders, but it needs somewhere to go: the destination controls are shown, so a
// full or missing drive (R-g2, R-e) can be answered with another folder (F5).
function retryDestination(run,which,dest){
  st.retry={run:run,which:which||'rest'};
  $('destCard').hidden=false;$('retryDest').hidden=false;
  if(dest&&!$('destPath').dataset.touched)$('destPath').value=dest;
}
$('destPath').addEventListener('input',()=>{$('destPath').dataset.touched='1';});
$('rePreviewBtn').addEventListener('click',()=>{
  if(!st.retry)return;
  show('banner','');
  api('api/exports/retry',{runId:st.retry.run,which:st.retry.which,dest:$('destPath').value}).then(d=>{
    if(!d.ok){showErr(d);return;}
    resumePreview(d.previewId);
  });
});
function line(k,v){const r=el('div',null,'row');r.appendChild(el('div',k,'k'));r.appendChild(el('div',v,'v'));return r;}
function renderPreview(p){
  if(p.retryOf)retryDestination(p.retryOf,p.retryWhich,p.dest);
  const body=$('previewBody');clear(body);
  const arch=!!p.archive;
  let count=fmt(p.count)+' messages';
  if(p.reported!==p.count)count+=' ('+fmt(p.reported)+(arch?' listed in the archive':' reported by Microsoft 365')+')';
  // the archive's panel note says how many of the selected messages have an original .eml (ruling Q8)
  if(arch)$('panelNote').textContent=panelText(' ('+fmt(p.archive.withOriginal)+' of '+fmt(p.count)+' selected)',true);
  body.appendChild(line('Messages',count));
  body.appendChild(line('Size','about '+p.estimate+(p.unknownSize?' ('+fmt(p.unknownSize)+' of unknown size)':'')));
  if(p.html)body.appendChild(line('Readable HTML',arch?('a page (.html) beside each of the '+fmt(p.archive.withOriginal)+' original messages; the size above includes them'):'a page (.html) beside each original; the size above includes them'));
  if(p.attachments)body.appendChild(line('Extract attachments',p.attachmentsZip?(arch?'each message\'s attachments in one zip (.attachments.zip) beside its original, from the original where the archive kept one; a message the archive kept no original of has the archive\'s own attachments zip, copied as it is; the size above includes them':'each message\'s attachments in one zip (.attachments.zip) beside its original; the size above includes them'):(arch?'each attachment as a file in a folder beside its message, from the original where the archive kept one and from its attachments zip where it did not (up to 1,000 files and 4 GB a message); the size above includes them':'each attachment as a file in a folder beside its message; the size above includes them')));
  body.appendChild(line('Into',p.exportDir));
  body.appendChild(line('Free space',p.free+' free; about '+p.reserveText+' is kept back to finish'));
  body.appendChild(line('Who can read it',p.posture.line));
  (p.folders||[]).forEach(f=>body.appendChild(line(f.folder,fmt(f.found)+' found, '+fmt(f.reported)+(arch?' listed':' reported')+(f.excluded?', '+fmt(f.excluded)+' left out':''))));
  if(p.longestPath)body.appendChild(line('Longest path',p.longestPath+' characters'+(p.over259?' ('+fmt(p.over259)+' over 259)':'')));
  (p.warnings||[]).forEach(w=>{body.appendChild(el('div','Warning: '+w,'note warnbox'));});
  (p.info||[]).forEach(w=>{body.appendChild(el('div',w,'note'));});
  if((p.samplePaths||[]).length){const ul=el('ul',null,'plain');p.samplePaths.forEach(s=>ul.appendChild(el('li',s,'mono')));body.appendChild(ul);}
}
$('startBtn').addEventListener('click',()=>{
  $('startBtn').disabled=true;
  api('api/exports/start',{previewId:st.previewId}).then(d=>{
    $('startBtn').disabled=false;
    if(!d.ok){showErr(d);$('previewCard').hidden=true;return;}
    st.leaving=true; // the export started: leaving this page is not losing a selection
    location.href='exports';
  });
});
$('backBtn').addEventListener('click',()=>{$('previewCard').hidden=true;st.previewId='';});
window.addEventListener('beforeunload',e=>{
  if(st.leaving)return;
  if(st.entries.length||Object.keys(st.picks).length){e.preventDefault();e.returnValue='';}
});
loadSource();
})();
`)

// exportsJS is the Export activity page: the running export with Cancel, and the history
// with each card's actions.
var exportsJS = scoped(commonJS + `(function(){
let busy=false;
const LABELS={
  'open-folder':'Open folder','check-this-export':'Check this export','export-failed-again':'Export failed messages again',
  'export-remaining':'Export the remaining messages','export-these-again':'Export these messages again','finish-as-partial':'Finish as partial',
  'check-and-finish':'Check and finish','look-again':'Look again','locate':'Locate…','this-folder-is-gone':'This folder is gone'};
function note(text){show('banner',text);}

// ---- the running export -----------------------------------------------------------
let runId='';
function renderRun(p){
  const card=$('runningCard');
  if(!p.running){card.hidden=true;runId='';}
  else{
    card.hidden=false;runId=p.runId;
    const pct=p.selected?Math.round(100*p.done/p.selected):0;
    $('runBar').style.width=pct+'%';
    const wrap=$('runBarWrap');wrap.setAttribute('aria-valuenow',String(pct));wrap.setAttribute('aria-valuetext',fmt(p.done)+' of '+fmt(p.selected)+' done');
    $('runLabel').textContent=fmt(p.done)+' of '+fmt(p.selected)+' done, '+fmt(p.failed)+' failed';
    const eta=p.etaSeconds>0?' · about '+Math.ceil(p.etaSeconds/60)+' min left':'';
    $('runStats').textContent=p.bytesText+' written · '+(p.folder||'')+' · '+fmt(p.rate)+' B/s'+eta;
    $('runLog').textContent=(p.log||[]).join('\n');
    const w=$('runWaiting');w.hidden=!p.waiting;w.textContent=p.waiting||'';
  }
  const last=$('lastCard');
  if(!p.running&&p.last){last.hidden=false;const b=$('lastBody');clear(b);
    b.appendChild(el('div',p.last.label,'v'));
    if(p.last.stop)b.appendChild(el('div',p.last.stop,'note warnbox'));
  }else last.hidden=true;
}
$('cancelBtn').addEventListener('click',()=>{
  if(!runId)return;
  api('api/exports/cancel',{runId:runId}).then(d=>{if(!d.ok)note(d.error);});
});
// A tab whose MailArchive Desktop has gone says so (the answer has no HTTP status when the
// request itself failed), instead of keeping a Running card frozen; it clears when the app answers.
let down=false;
function pollRun(){api('api/exports/progress',{}).then(d=>{
  if(d.ok){if(down){down=false;note('');}renderRun(d);}
  else if(d.status===undefined){down=true;note(d.error);}
});}

// ---- the history ----------------------------------------------------------------------
function cardEl(c){
  const box=el('div',null,'run-card');
  box.appendChild(el('h3',c.label));
  const when=c.started?new Date(c.started).toLocaleString():'';
  box.appendChild(el('div',when+' · '+(c.sourceLabel||c.mailbox||c.source)+' · '+c.rootName,'card-sub'));
  if(c.running)box.appendChild(el('div','Writing now: see Running export above.'));
  else box.appendChild(el('div',fmt(c.exported)+' exported, '+fmt(c.failed)+' failed, '+fmt(c.notAttempted)+' not attempted of '+fmt(c.planned)));
  if(c.attachLine)box.appendChild(el('div',c.attachLine,'card-sub'));
  if(c.attachWarn)box.appendChild(el('div',c.attachWarn,'note warnbox'));
  if(c.setNote)box.appendChild(el('div',c.setNote,'card-sub'));
  if(c.stop)box.appendChild(el('div',c.stop,'note warnbox'));
  if(c.next)box.appendChild(el('div',c.next,'note'));
  if((c.failures||[]).length){
    const d=el('details');d.appendChild(el('summary','Which ones? ('+fmt(c.failed)+' not exported)'));
    const ul=el('ul',null,'plain');
    c.failures.forEach(f=>ul.appendChild(el('li',f.subject+' — '+f.folder+' — '+f.reason)));
    if(c.failuresMore)ul.appendChild(el('li','and '+fmt(c.failuresMore)+' more, listed in the export\'s export-manifest.json'));
    d.appendChild(ul);box.appendChild(d);
  }
  if(c.fingerprint){
    const f=el('div');f.appendChild(el('span','Fingerprint '));f.appendChild(el('span',c.fingerprintGrouped,'fp'));
    const cp=el('button','Copy','btn small secondary');cp.type='button';
    cp.addEventListener('click',()=>{if(navigator.clipboard)navigator.clipboard.writeText(c.fingerprint);cp.textContent='Copied';});
    f.appendChild(document.createTextNode(' '));f.appendChild(cp);
    if(c.receipt){const sv=el('button','Save fingerprint receipt','btn small secondary');sv.type='button';
      sv.addEventListener('click',()=>saveReceipt(c));f.appendChild(document.createTextNode(' '));f.appendChild(sv);}
    box.appendChild(f);
  }else if(c.fingerprintNote)box.appendChild(el('div',c.fingerprintNote,'card-sub'));
  if(c.set){
    const s=el('div');s.appendChild(el('span','Set fingerprint '));s.appendChild(el('span',c.set.grouped,'fp'));
    box.appendChild(s);
    box.appendChild(el('div','In the order the parts were finished; a recipient hashes the parts\' SHA256SUMS.txt files in that order.','card-sub'));
    c.set.parts.forEach((p,i)=>box.appendChild(el('div',(i+1)+'. '+p.rootName+': '+p.grouped+(p.gone?' (marked gone: a recipient without this part cannot check the set)':''),'card-sub')));
  }
  const acts=el('div',null,'actions');
  (c.actions||[]).forEach(a=>{
    let label=LABELS[a]||a;
    if(a==='export-these-again'&&c.remaining)label='Export these '+fmt(c.remaining)+' messages again';
    const b=el('button',label,'btn small'+(a==='open-folder'||a==='check-this-export'?' secondary':''));b.type='button';
    b.dataset.run=c.run;b.dataset.act=a;
    b.addEventListener('click',()=>act(a,c,box));acts.appendChild(b);
  });
  box.appendChild(acts);
  const res=el('div');res.className='cardres';res.setAttribute('aria-live','polite');
  if(c.result&&c.result.error)res.appendChild(el('div',c.result.error,'note warnbox'));
  box.appendChild(res);
  return box;
}
function saveReceipt(c){
  const blob=new Blob([c.receipt],{type:'text/plain'});
  const a=document.createElement('a');a.href=URL.createObjectURL(blob);a.download='mailarchive-fingerprint-'+c.run.substring(0,8)+'.txt';
  document.body.appendChild(a);a.click();document.body.removeChild(a);
}
function resultOf(box){return box.querySelector('.cardres');}
function act(a,c,box){
  const out=resultOf(box);clear(out);note('');
  // A refusal is kept by the server on this run's card (it comes back with the next history
  // answer, which rebuilds every card), and is shown at once here and in the page's alert.
  const fail=d=>{const t=d.error||'That did not work.';out.appendChild(el('div',t,'note warnbox'));note(t);};
  const done=d=>{if(!d.ok)fail(d);loadHistory(true);};
  switch(a){
  case 'open-folder':api('api/exports/open-folder',{runId:c.run}).then(d=>{if(!d.ok){fail(d);loadHistory(true);}});break;
  case 'check-this-export':api('api/exports/check',{runId:c.run}).then(d=>{
      if(!d.ok){fail(d);loadHistory(true);return;}
      out.appendChild(el('div',(d.green?'GREEN':'RED')+': '+fmt(d.files)+' files checked','v '+(d.green?'good':'bad')));
      (d.problems||[]).forEach(p=>out.appendChild(el('div',p,'note warnbox')));
      if(d.remedy)out.appendChild(el('div',d.remedy,'note warnbox'));
    });break;
  case 'export-failed-again':api('api/exports/retry',{runId:c.run,which:'failed'}).then(d=>{if(!d.ok){done(d);return;}location.href='workspace#preview='+d.previewId;});break;
  case 'export-remaining':case 'export-these-again':api('api/exports/retry',{runId:c.run,which:'rest'}).then(d=>{if(!d.ok){done(d);return;}location.href='workspace#preview='+d.previewId;});break;
  case 'finish-as-partial':if(confirm('Finish this export as a partial one? Its README, manifest and checksums will list only what was written.'))api('api/exports/finish',{runId:c.run}).then(done);break;
  case 'check-and-finish':api('api/exports/finish',{runId:c.run}).then(done);break;
  case 'look-again':api('api/exports/locate',{runId:c.run,action:'again'}).then(done);break;
  case 'locate':api('api/workspace/browse',{title:'Choose the export folder'}).then(d=>{
      if(!d.path){if(!d.canceled)fail(d);return;}
      api('api/exports/locate',{runId:c.run,action:'locate',path:d.path}).then(done);});break;
  case 'this-folder-is-gone':if(confirm('Tell MailArchive this folder is gone? You can then export its messages again.'))api('api/exports/locate',{runId:c.run,action:'gone'}).then(done);break;
  }
}
// The Sign in card: shown when a retry was refused for the sign-in, or the saved sign-in was
// refused, so "Sign in again" in those messages names a button that is on this page.
function renderSignIn(s){
  const c=$('signInCard');if(!c)return;
  const on=!!(s&&s.needed);
  if(on)$('signInWhy').textContent=(s.why||'')+(s.as?' Sign in as '+s.as+'.':'');
  c.hidden=!on;
}
// The history is redrawn from the server's answer. A timed refresh never redraws under the
// card the keyboard is in; a redraw after a click (force) puts the focus back on the button
// that was pressed (the same run, the same action), so a keyboard user keeps their place.
function loadHistory(force){
  const h=$('history');
  if(busy||(!force&&h.contains(document.activeElement)))return;
  busy=true;
  api('api/exports/history',{}).then(d=>{
    busy=false;
    if(!d.ok){note(d.error);return;}
    renderSignIn(d.signIn);
    const a=document.activeElement;
    const keep=(a&&h.contains(a)&&a.dataset)?{run:a.dataset.run,act:a.dataset.act}:null;
    clear(h);
    (d.runs||[]).forEach(c=>h.appendChild(cardEl(c)));
    $('historyEmpty').hidden=(d.runs||[]).length>0;
    if(keep&&keep.act){for(const b of h.querySelectorAll('button[data-act]')){if(b.dataset.run===keep.run&&b.dataset.act===keep.act){b.focus();break;}}}
  });
}
pollRun();loadHistory();
setInterval(pollRun,1500);
setInterval(()=>{if(!document.querySelector('.cardres .v'))loadHistory();},5000);
})();
`)
