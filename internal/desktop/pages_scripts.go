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
function render(d){
  const box=$('signInBox');box.hidden=false;
  const b=$('signInBanner');if(b&&d.account)b.textContent='MailArchive Desktop is running as '+d.account+' on this computer.';
  const n=$('signInNotice');if(n&&d.notice)n.textContent=d.notice;
  const dev=$('signInDevice');
  if(d.state==='running'&&d.device){dev.hidden=false;$('signInCode').textContent=d.device.code;
    const u=$('signInUrl');if(/^https:\/\//.test(d.device.url))u.href=d.device.url;}
  else dev.hidden=true;
  const c=$('signInConfirm');
  if(d.state==='confirm'&&d.confirm){c.hidden=false;$('signInConfirmText').textContent=d.confirm.text;}else c.hidden=true;
  if(d.state==='done'){stop();btn.disabled=false;
    show('banner',d.signedOut?'You chose Sign out: the new sign-in was not saved.':'Signed in as '+d.upn+'.');
    setTimeout(()=>location.reload(),900);}
  if(d.state==='failed'){stop();btn.disabled=false;show('banner',d.error||'The sign-in did not finish.');}
}
function poll(answer){
  api('api/m365/signin-status',answer?{answer:answer}:{}).then(d=>{
    if(!d.ok){stop();btn.disabled=false;show('banner',d.error||RESTART);return;}
    render(d);
  });
}
function stop(){if(timer){clearInterval(timer);timer=null;}}
btn.addEventListener('click',()=>{
  btn.disabled=true;show('banner','');
  api('api/m365/signin',{}).then(d=>{
    if(!d.ok){btn.disabled=false;show('banner',d.error||'Could not start the sign-in.');return;}
    poll();timer=setInterval(()=>poll(),1500);
  });
});
const k=$('signInKeep'),o=$('signInOut');
if(k)k.addEventListener('click',()=>poll('keep'));
if(o)o.addEventListener('click',()=>poll('signout'));
poll();
})();
`)

// workspaceJS is the Extraction workspace page: the source, the folder tree and its
// selection, the message list, the destination, the options and the preview.
var workspaceJS = scoped(commonJS + `(function(){
const st={upn:'',folders:[],byId:{},kids:{},entries:[],picks:{},current:'',rows:[],more:false,cursor:'',total:0,previewId:'',countTimer:null,running:false,counts:0};
function errText(d){return d.error||'Something went wrong.';}
function showErr(d){
  show('banner',errText(d));
  if(d&&(d.id==='R-j2'||d.id==='R-y'||d.id==='R-l'||d.id==='R-b'))$('loadFolders').disabled=false;
}

// ---- source ------------------------------------------------------------
function loadSource(){
  api('api/workspace/source',{}).then(d=>{
    if(!d.ok){showErr(d);return;}
    st.running=!!d.running;
    const m=d.m365||{};
    if(m.state==='ready'){st.upn=m.upn;show('mbox','Mailbox: '+m.upn+' (your own mailbox)');$('loadFolders').disabled=false;}
    else if(m.state==='not-signed-in'){show('mbox','Not signed in to Microsoft 365');$('signInArea').hidden=false;}
    else{show('mbox','Microsoft 365 isn\'t set up yet — run mailarchive setup, then come back.');}
    const s=d.settings||{};
    if(s.dest&&!$('destPath').value)$('destPath').value=s.dest;
    $('optPreserve').checked=s.preserve!==false;
    if(st.running)$('browseNote').textContent='Browse is off while an export runs — type the folder\'s path instead.';
    const h=location.hash||'';
    if(h.indexOf('#preview=')===0){resumePreview(h.substring(9));}
  });
}
$('loadFolders').addEventListener('click',()=>{
  $('loadFolders').disabled=true;show('banner','');
  api('api/workspace/folders',{}).then(d=>{
    $('loadFolders').disabled=false;
    if(!d.ok){showErr(d);return;}
    st.upn=d.upn;st.folders=d.folders;st.byId={};st.kids={};
    d.folders.forEach(f=>{st.byId[f.id]=f;const p=f.parent||'';(st.kids[p]=st.kids[p]||[]).push(f.id);});
    st.entries=[];st.picks={};st.current='';st.rows=[];
    $('pickCard').hidden=false;$('destCard').hidden=false;$('footerSel').hidden=false;
    renderTree();renderRows();updateCount();
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
function renderTree(){
  const root=$('tree');clear(root);
  const ul=el('ul');
  (st.kids['']||[]).forEach(id=>ul.appendChild(nodeEl(id)));
  root.appendChild(ul);
}
function nodeEl(id){
  const f=st.byId[id];const li=el('li');
  const row=el('div',null,'node');
  const kids=st.kids[id]||[];
  const arrow=el('span',kids.length?'▸':'','arrow');
  const cb=el('input');cb.type='checkbox';
  const cov=covering(id);
  cb.checked=!!cov;
  if(cov&&cov.id!==id)cb.disabled=true;
  cb.addEventListener('change',()=>toggleFolder(id,cb.checked));
  const name=el('span',f.name,'name'+(st.current===id?' current':''));
  name.addEventListener('click',()=>openFolder(id));
  const cnt=el('span','('+fmt(f.count)+')','cnt');
  row.appendChild(arrow);row.appendChild(cb);row.appendChild(name);row.appendChild(cnt);
  if(f.wellKnown){row.appendChild(el('span',f.wellKnown==='deleted'?'Deleted Items':'Junk Email','tag'));}
  li.appendChild(row);
  if(kids.length){
    const sub=el('ul');sub.hidden=true;
    kids.forEach(k=>sub.appendChild(nodeEl(k)));
    li.appendChild(sub);
    arrow.addEventListener('click',()=>{sub.hidden=!sub.hidden;arrow.textContent=sub.hidden?'▸':'▾';});
  }
  return li;
}
function toggleFolder(id,on){
  if(on){if(!covering(id))st.entries.push({id:id,subtree:$('inclSub').checked,exclude:[]});}
  else{st.entries=st.entries.filter(e=>e.id!==id);}
  renderTree();renderRows();updateCount();
}
$('selectFolder').addEventListener('click',()=>{
  if(!st.current)return;
  if(!covering(st.current))st.entries.push({id:st.current,subtree:$('inclSub').checked,exclude:[]});
  const f=st.byId[st.current];
  show('selNote','Selected '+fmt(f.count)+' messages from '+f.name+(($('inclSub').checked&&(st.kids[st.current]||[]).length)?' (and its subfolders).':'.'));
  renderTree();renderRows();updateCount();
});

// ---- the message list ----------------------------------------------------------
function openFolder(id){
  st.current=id;st.rows=[];st.cursor='';st.more=false;
  $('selectFolder').disabled=false;
  renderTree();loadRows(false);
}
function loadRows(more){
  const body={folder:st.current};
  if(more){body.cursor=st.cursor;if(st.rows.length)body.after=st.rows[st.rows.length-1].h;}
  api('api/workspace/messages',body).then(d=>{
    if(!d.ok){
      showErr(d);
      if(d.reload&&more){st.rows=[];loadRows(false);}
      return;
    }
    st.rows=st.rows.concat(d.rows);st.more=d.more;st.cursor=d.cursor||'';st.total=d.count;
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
function renderRows(){
  const box=$('msgs');clear(box);
  const q=$('filter').value.toLowerCase();
  let shown=0;
  st.rows.forEach(r=>{
    if(q&&(r.subject+' '+r.from+' '+r.fromName).toLowerCase().indexOf(q)<0)return;
    shown++;
    const row=el('div',null,'msg');
    const cb=el('input');cb.type='checkbox';cb.checked=rowChecked(r.h);
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
function selection(){
  const messages=Object.keys(st.picks).map(h=>({h:h}));
  return {source:{kind:'m365',mailbox:st.upn},folders:st.entries.map(e=>({id:e.id,subtree:e.subtree,exclude:e.exclude})),messages:messages,
    dest:$('destPath').value,options:{preserve:$('optPreserve').checked}};
}
function updateCount(){
  clearTimeout(st.countTimer);
  const empty=!st.entries.length&&!Object.keys(st.picks).length;
  $('previewBtn').disabled=empty;
  if(empty){$('selCount').textContent='0 messages selected';return;}
  st.countTimer=setTimeout(()=>{
    api('api/workspace/count',selection()).then(d=>{
      if(!d.ok){showErr(d);return;}
      $('selCount').textContent=fmt(d.count)+' messages selected (items Microsoft 365 reports)';
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
    if(!d.ok&&d.state!=='failed'){showErr(d);$('previewCard').hidden=true;return;}
    if(d.state==='running'){clear(body);body.appendChild(el('p',d.progress||'Reading your folders…'));setTimeout(tick,1000);return;}
    if(d.state==='failed'){showErr(d);$('previewCard').hidden=true;return;}
    renderPreview(d.result);
  });
  tick();
}
function line(k,v){const r=el('div',null,'row');r.appendChild(el('div',k,'k'));r.appendChild(el('div',v,'v'));return r;}
function renderPreview(p){
  const body=$('previewBody');clear(body);
  let count=fmt(p.count)+' messages';
  if(p.reported!==p.count)count+=' ('+fmt(p.reported)+' reported by Microsoft 365)';
  body.appendChild(line('Messages',count));
  body.appendChild(line('Size','about '+p.estimate+(p.unknownSize?' ('+fmt(p.unknownSize)+' of unknown size)':'')));
  body.appendChild(line('Into',p.exportDir));
  body.appendChild(line('Free space',p.free+' free; about '+p.reserveText+' is kept back to finish'));
  body.appendChild(line('Who can read it',p.posture.line));
  (p.folders||[]).forEach(f=>body.appendChild(line(f.folder,fmt(f.found)+' found, '+fmt(f.reported)+' reported'+(f.excluded?', '+fmt(f.excluded)+' left out':''))));
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
    location.href='exports';
  });
});
$('backBtn').addEventListener('click',()=>{$('previewCard').hidden=true;st.previewId='';});
window.addEventListener('beforeunload',e=>{
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
    $('runLabel').textContent=fmt(p.done)+' of '+fmt(p.selected)+' done, '+fmt(p.failed)+' failed';
    const eta=p.etaSeconds>0?' · about '+Math.ceil(p.etaSeconds/60)+' min left':'';
    $('runStats').textContent=p.bytesText+' written · '+(p.folder||'')+' · '+fmt(p.rate)+' B/s'+eta;
    $('runLog').textContent=(p.log||[]).join('\n');
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
function pollRun(){api('api/exports/progress',{}).then(d=>{if(d.ok)renderRun(d);});}

// ---- the history ----------------------------------------------------------------------
function cardEl(c){
  const box=el('div',null,'run-card');
  box.appendChild(el('h3',c.label));
  const when=c.started?new Date(c.started).toLocaleString():'';
  box.appendChild(el('div',when+' · '+(c.mailbox||c.source)+' · '+c.rootName,'card-sub'));
  box.appendChild(el('div',fmt(c.exported)+' exported, '+fmt(c.failed)+' failed, '+fmt(c.notAttempted)+' not attempted of '+fmt(c.planned)));
  if(c.stop)box.appendChild(el('div',c.stop,'note warnbox'));
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
    b.addEventListener('click',()=>act(a,c,box));acts.appendChild(b);
  });
  box.appendChild(acts);
  const res=el('div');res.className='cardres';
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
  const done=d=>{if(!d.ok)fail(d);loadHistory();};
  switch(a){
  case 'open-folder':api('api/exports/open-folder',{runId:c.run}).then(d=>{if(!d.ok){fail(d);loadHistory();}});break;
  case 'check-this-export':api('api/exports/check',{runId:c.run}).then(d=>{
      if(!d.ok){fail(d);loadHistory();return;}
      out.appendChild(el('div',(d.green?'GREEN':'RED')+': '+fmt(d.files)+' files checked','v '+(d.green?'good':'bad')));
      (d.problems||[]).forEach(p=>out.appendChild(el('div',p,'note warnbox')));
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
function loadHistory(){
  if(busy)return;busy=true;
  api('api/exports/history',{}).then(d=>{
    busy=false;
    if(!d.ok){note(d.error);return;}
    const h=$('history');clear(h);
    (d.runs||[]).forEach(c=>h.appendChild(cardEl(c)));
    $('historyEmpty').hidden=(d.runs||[]).length>0;
  });
}
pollRun();loadHistory();
setInterval(pollRun,1500);
setInterval(()=>{if(!document.querySelector('.cardres .v'))loadHistory();},5000);
})();
`)
