package desktop

// dashboardJS is the Overview's script, served at /dashboard.js: the action buttons
// (dashboardBaseJS, dashboard.go) and, after them, Delete and Remove from list (archiveDeleteJS).
// One script, so the page's own busy, noteBeside and csrf serve both, and every page test that
// reads dashboardJS reads the delete's buttons too.
const dashboardJS = dashboardBaseJS + archiveDeleteJS

// archiveDeleteJS is the page's half of Delete and Remove from list (docs/design-archive-delete.md
// §3.1, §3.2, §8, §8.1, §10; slice 3b, MA-982..MA-986), on slice 3a's four routes. Every panel,
// question and result is the page's own, inside the row it is about (beside the row, MA-955),
// built with createElement and textContent only (X15): never a browser confirm() or prompt()
// (C19). Words are §3.1's, §3.2's and §8's where they give them; the rest are the builder's.
// Which rows a delete's reply is about, and what it means for this page's own archive, the
// server says (rows, page: MA-986); the page compares no paths itself.
const archiveDeleteJS = `
// ---- Delete an archive and Remove from list (design-archive-delete §3, §8, §8.1, §10).
const delUnfinishedText='Delete not finished: click Delete again';
// What takes Archive now's and Run now's place on a page whose archive a delete reached (the
// delete reply's page, MA-986).
const delPageText={unfinished:delUnfinishedText,deleted:'This archive was deleted: click Dismiss on its result to refresh this page.',partial:'This archive was not fully deleted: click Dismiss on its result to refresh this page.'};
// The cards that show or change the page's own archive (MA-1017): after a delete of it got past
// step 4 (it is no longer the archive in use) they are greyed and do nothing (inert) until its
// result is dismissed and the page is drawn again. Your archives, This archive, Add another
// archive, the guide, Security & privacy and Uninstall are not the archive's and stay.
const delArchiveCards=['sec-m365','sec-export','sec-archive','sec-settings','sec-encsteps','sec-encryption','sec-backup','sec-alerts','sec-history','sec-health','sec-maintenance'];
// delWait: a reload the page started was held; delInFlight: deletes this page has asked for whose
// reply has not come; delSeq: the last Delete… or Remove from list clicked (an earlier one's
// slower reply opens nothing, MA-986).
var delWait=false,delInFlight=0,delSeq=0,delBoxSeq=0;
// reloadPage is every reload the page starts itself (§3.1 After a Delete, MA-984): while a
// delete's result is held on the page, or a delete asked for here has not answered (its reply
// would be lost, MA-986), it does not reload and says the page refreshes when the result is
// dismissed; the page reloads once the last result is dismissed (delDismiss).
function reloadPage(){
  if(delInFlight>0||document.querySelector('[data-held-id]')){delWait=true;document.querySelectorAll('[data-held-wait]').forEach(w=>{w.hidden=false;});return;}
  location.reload();
}
function delPost(path,body){return fetch(path,{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':csrf},body:JSON.stringify(body)}).then(r=>r.json());}
function delEl(tag,text,cls){const e=document.createElement(tag);if(text!==undefined&&text!==null)e.textContent=text;if(cls)e.className=cls;return e;}
function delButton(text,cls){const b=delEl('button',text,cls);b.type='button';return b;}
// One panel at a time: a delete's panel or Remove from list's question closes any other one — but
// never the panel of a delete still waiting for its reply (data-delete-waiting), whose "Keep this
// page open" note and result belong to that delete (del-s3b leftover 1).
function delClose(){document.querySelectorAll('.delete-panel,.unlist-ask').forEach(x=>{if(!x.hasAttribute('data-delete-waiting'))x.remove();});}
// The server's match (archdelete.NameMatches): not empty, both sides NFC, no case folding, no trimming.
function delNameMatches(typed,name){return typed!==''&&typed.normalize('NFC')===name.normalize('NFC');}
// A file saved through the browser, as the fingerprint receipt is: a string as UTF-8, bytes as they are.
function delSaveFile(name,data){
  const blob=new Blob([data],{type:'text/plain;charset=utf-8'});
  const a=document.createElement('a');a.href=URL.createObjectURL(blob);a.download=name;
  document.body.appendChild(a);a.click();document.body.removeChild(a);
  setTimeout(()=>URL.revokeObjectURL(a.href),60000);
}
// The row of Your archives (or This archive) the server named, by its own spelling.
function delRowOf(path){let r=null;document.querySelectorAll('.archive-row').forEach(li=>{if(li.dataset.path===path)r=li;});return r;}
// The row is gone from the list: its own buttons and its state go at once, the rest stays to read.
function delRowGone(li,text){
  Array.from(li.children).forEach(x=>{if(x.tagName==='BUTTON'||x.classList.contains('row-state'))x.remove();});
  li.append(delEl('span',text,'archive-tag'));
  li.classList.add('unavailable');
}
// A delete not finished (§5.4): the row's Switch, Pause and Resume give way to its words; Delete…
// and Forget stay.
function delMarkUnfinished(li){
  const st=li.querySelector('.row-state');
  if(st){while(st.firstChild)st.removeChild(st.firstChild);const t=document.createElement('span');t.className='archive-tag';t.textContent=delUnfinishedText;st.append(t);}
  li.setAttribute('data-delete-unfinished','');
}
// The archive this page was drawn for was deleted, or its delete did not finish (kind: the
// reply's page): Archive now and Run now, and their sidebar links, are not offered for it. Once
// it is no longer the archive in use (deleted, or not fully deleted past step 4), the rest of
// its page goes too (delPageGone).
function delPageArchive(kind){
  const text=delPageText[kind]||delUnfinishedText;
  const gone=kind==='deleted'||kind==='partial';
  ['archiveBtn','runBackupBtn'].forEach(id=>{const x=$(id);if(x&&!x.hidden){x.hidden=true;if(!gone){const t=document.createElement('span');t.className='archive-tag';t.textContent=text;x.after(t);}}});
  ['navArchive','navRunNow'].forEach(id=>{const x=$(id);if(x)x.hidden=true;});
  if(gone)delPageGone(text);
}
// The page's own archive is gone (MA-1017; the 1.5.3 friction review's H4): a line at the top
// says so; its cards (Status & health, Settings, the Encryption card's Unlocked and Lock, the
// Scheduled backup card…) are greyed and inert, each saying the same at its top; the sidebar's
// reader and Encryption links go; and the switch notes about it, no longer true, are cleared.
function delPageGone(text){
  if(!$('archiveGone')){const b=delEl('div',text,'note warnbox');b.id='archiveGone';b.setAttribute('role','status');const hd=$('heldDeletes');if(hd)hd.before(b);}
  const grey=c=>{if(c.hasAttribute('inert'))return;c.setAttribute('inert','');c.setAttribute('aria-disabled','true');c.classList.add('archive-gone');};
  delArchiveCards.forEach(id=>{const c=$(id);if(!c)return;
    if(!c.querySelector('.gone-tag')){const t=delEl('p',text,'archive-tag gone-tag');const h=c.querySelector('h2');if(h)h.after(t);else c.prepend(t);}
    grey(c);});
  document.querySelectorAll('.status-grid').forEach(grey);
  document.querySelectorAll('.nav a[target]').forEach(a=>{a.hidden=true;});
  const ne=$('navEncryption');if(ne)ne.hidden=true;
  ['switchLocks','switchLocksSettings'].forEach(id=>{const x=$(id);if(x)x.remove();});
}
// A record saved is remembered for this tab (a reload is no dismissal), so Dismiss asks only
// about a record not saved (§8.1).
function delSavedKey(id){return 'mailarchive-deletion-record-saved-'+id;}
// The held result of path whose record was not saved, or null.
function delUnsavedFor(path){let r=null;document.querySelectorAll('[data-held-id]').forEach(h=>{if(h.dataset.row===path&&h.dataset.saved!=='1')r=h;});return r;}
// Dismiss: the result is no longer held; once none is, the page is drawn again — when the
// person dismissed it (reload), or when a reload the page started was held meanwhile (delWait).
// A result already dismissed elsewhere (another tab: notHeld) goes too. It answers false when the
// result stays, 'reload' when the page is being drawn again, true otherwise.
async function delDismiss(h,reload){
  const msg=h.querySelector('[data-held-msg]');
  try{const d=await delPost('api/archive/delete/dismiss',{id:h.dataset.heldId});
    if(!d.ok&&!d.notHeld){if(msg){msg.hidden=false;msg.textContent=d.error||'Could not close this result.';}return false;}
    h.remove();
    if((reload||delWait)&&delInFlight===0&&!document.querySelector('[data-held-id]')){location.reload();return 'reload';}
    return true;
  }catch(e){if(msg){msg.hidden=false;msg.textContent='Error: '+e;}return false;}
}
// "Close without saving the deletion record?" asked in the result itself (§3.1, §8.1); then, when
// given, what was asked for goes on (not when the page is being drawn again).
function delAsk(h,then){
  const ask=h.querySelector('.held-ask'),yes=h.querySelector('[data-held-close]'),no=h.querySelector('[data-held-keep]');
  ask.hidden=false;yes.focus();
  yes.onclick=async()=>{const done=busy(yes,'Closing…',null,'Closing this result…');const r=await delDismiss(h,!then);if(!r){done();return;}if(then&&r!=='reload')then();};
  no.onclick=()=>{ask.hidden=true;const s=h.querySelector('[data-held-save]');if(s)s.focus();};
}
// A delete's result (§3.1 After a Delete, §8): its lines, Save deletion record (the record's exact
// bytes, any number of times) and Dismiss; where it sits follows "Where the result sits": beside
// its row while the row stays, else at the top of Your archives (or of the Overview). Drawing it
// changes no row: at a page load the server drew every row as it is now (a row of the same
// spelling may be a new archive, del-s3b-A1-04); a delete's reply changes them (delAfterDelete).
function delShowHeld(v){
  const h=document.createElement('div');h.className='note held-result';h.setAttribute('role','status');
  h.dataset.heldId=v.id;h.dataset.row=v.row;
  try{if(sessionStorage.getItem(delSavedKey(v.id))==='1')h.dataset.saved='1';}catch(e){}
  const head=delEl('p');head.append(delEl('b','Delete of '+v.folder));h.append(head);
  (v.lines||[]).forEach(t=>h.append(delEl('p',t)));
  const wait=delEl('p','This page refreshes when you dismiss this result.','card-sub');wait.setAttribute('data-held-wait','');wait.hidden=!delWait;h.append(wait);
  const acts=delEl('div',null,'del-actions');
  const sv=delButton('Save deletion record','btn');sv.setAttribute('data-held-save','');
  sv.addEventListener('click',()=>{delSaveFile(v.recordName,Uint8Array.from(atob(v.recordBase64),c=>c.charCodeAt(0)));h.dataset.saved='1';try{sessionStorage.setItem(delSavedKey(v.id),'1');}catch(e){}});
  const dis=delButton('Dismiss','btn secondary');
  dis.addEventListener('click',async()=>{
    if(h.dataset.saved!=='1'){delAsk(h,null);return;}
    const done=busy(dis,'Closing…',null,'Closing this result…');
    if(!await delDismiss(h,true))done();
  });
  acts.append(sv,dis);h.append(acts);
  const ask=delEl('div',null,'held-ask');ask.hidden=true;
  ask.append(delEl('p','Close without saving the deletion record?'));
  const yes=delButton('Close without saving','btn secondary');yes.setAttribute('data-held-close','');
  const no=delButton('Keep it','btn');no.setAttribute('data-held-keep','');
  const aa=delEl('div',null,'del-actions');aa.append(yes,no);ask.append(aa);h.append(ask);
  const msg=delEl('p',null,'warn');msg.hidden=true;msg.setAttribute('data-held-msg','');h.append(msg);
  const li=v.rowRemoved?null:delRowOf(v.row);
  if(li)li.append(h);
  else{const card=$('sec-archives'),top=$('heldDeletes');
    if(card){const h2=card.querySelector('h2');if(h2)h2.after(h);else card.prepend(h);}
    else if(top){top.hidden=false;top.append(h);}}
  return h;
}
// What a delete's reply changes on this page at once (§3.1 "Where the result sits"): every row
// the server says is this folder (d.rows: each spelling of it, as step 9 removes them and as
// deletions.log marks them) loses its buttons when step 9 removed it, or shows "Delete not
// finished" in place of Switch, Pause and Resume; and when the server says it was this page's own
// archive (d.page), Archive now and Run now give way — not for a delete refused after its start,
// which removed nothing (del-s3b-A1-02, A2-02).
function delAfterDelete(d){
  (d.rows||[]).forEach(p=>{const li=delRowOf(p);if(!li)return;
    if(d.rowRemoved)delRowGone(li,'Removed from Your archives.');
    else if(d.unfinished)delMarkUnfinished(li);});
  if(d.page)delPageArchive(d.page);
}
// The plan, beside the row (§3.1): what Delete removes and what it does not reach, Save this plan,
// then the folder's name typed before Delete permanently can be pressed.
function delPanel(p){
  const w=document.createElement('div');w.className='note delete-panel';w.setAttribute('role','group');w.setAttribute('aria-label','Delete this archive');
  const line=(text,cls)=>{const x=delEl('p',text,cls);w.append(x);return x;};
  const head=delEl('p');head.append(delEl('b','Delete this archive permanently'));w.append(head);
  line(p.threeLine,'card-sub');
  line('Folder: '+p.folder,'mono');
  line('Delete removes '+p.files+(p.files===1?' file':' files')+' ('+p.size+').');
  if(p.rerun)line('This finishes a delete that did not finish.');
  if(p.stores.length)line('Stores: '+p.stores.map(s=>'store '+s.number+' '+s.name).join(', '));
  if(p.schedules.length)line('Scheduled backups it stops: '+p.schedules.join(', '));
  if(p.checkYourself.length)line('Not removed, check it yourself: '+p.checkYourself.join(', '),'warn');
  p.notes.forEach(t=>line(t,'warn'));
  if(p.pausedLine)line(p.pausedLine);
  if(p.syncedLine)line(p.syncedLine,'warn');
  // The kept files by name (B6): the plan sends the first ten.
  if(p.kept){const kn=p.keptNames||[];line("Kept (not the archive's): "+(kn.length?kn.join(', ')+(p.kept>kn.length?' and '+(p.kept-kn.length)+' more':''):p.kept+(p.kept===1?' file':' files')));}
  let box=null;
  if(p.unrecorded){
    line('Mail copies this archive did not record: '+p.unrecorded);
    const ul=delEl('ul');p.unrecordedNames.forEach(x=>ul.append(delEl('li',x,'mono')));
    if(p.unrecorded>p.unrecordedNames.length)ul.append(delEl('li','and '+(p.unrecorded-p.unrecordedNames.length)+' more'));
    w.append(ul);
    const lab=delEl('label',null,'checkline');box=document.createElement('input');box.type='checkbox';
    lab.append(box,document.createTextNode(p.unrecordedBox));w.append(lab);
  }
  if(p.keyLine)line(p.keyLine);
  line(p.doesNotReach);
  const sa=delEl('div',null,'del-actions');
  const sv=delButton('Save this plan','btn secondary');sv.title='Save this plan as a text file, for example to have it approved first. Nothing is deleted.';
  // Said on screen once saved (B13): the browser may save it without asking.
  const saved=delEl('span',null,'card-sub');saved.setAttribute('role','status');saved.hidden=true;
  sv.addEventListener('click',()=>{delSaveFile(p.planName,p.planText);saved.hidden=false;saved.textContent='Plan saved as '+p.planName+', in your browser\'s downloads. Nothing was deleted.';});
  sa.append(sv,saved);w.append(sa);
  line(p.logLine,'card-sub');
  // A delete waiting for its reply keeps its panel while another opens, so each box has an id of
  // its own and its label names it.
  const boxId='delTyped'+(++delBoxSeq);
  // The folder's name never breaks across lines (B14: it broke at a hyphen at phone width).
  const lab=delEl('label');const at=p.typeLine.indexOf(p.name);
  if(at<0)lab.textContent=p.typeLine;
  else lab.append(document.createTextNode(p.typeLine.slice(0,at)),delEl('span',p.name,'mono del-name'),document.createTextNode(p.typeLine.slice(at+p.name.length)));
  const input=document.createElement('input');input.type='text';input.id=boxId;input.autocomplete='off';input.spellcheck=false;lab.htmlFor=boxId;
  w.append(lab,input);
  const hint=delEl('span',"doesn't match yet",'name-hint');hint.setAttribute('aria-live','polite');
  const go=delButton('Delete permanently','btn danger-fill');go.disabled=true;
  const cancel=delButton('Cancel','btn secondary');
  const ga=delEl('div',null,'del-actions');ga.append(go,cancel,hint);w.append(ga);
  const msg=delEl('div',null,'note warnbox');msg.hidden=true;msg.setAttribute('role','alert');w.append(msg);
  const match=()=>{const ok=delNameMatches(input.value,p.name);go.disabled=!ok;hint.hidden=ok;};
  input.addEventListener('input',match);match();
  cancel.addEventListener('click',()=>{w.remove();const n=$('settingsNote');if(n){n.hidden=false;n.textContent='Nothing was deleted.';}});
  go.addEventListener('click',async()=>{
    const done=busy(go,'Deleting…',msg,'Deleting this archive. Keep this page open until its result shows here…');
    delInFlight++;w.setAttribute('data-delete-waiting','');
    try{const d=await delPost('api/archive/delete',{token:p.token,typed:input.value,unrecorded:!!(box&&box.checked),drawnFor:drawnFor});
      // Its own panel goes, never another row's opened meanwhile (del-s3b leftover 1).
      // The result is brought into view (B14): on a narrow layout it sat at the top of the card, out of sight.
      if(d.id){done(true);w.remove();const hv=delShowHeld(d);delAfterDelete(d);if(hv.scrollIntoView)hv.scrollIntoView({block:'nearest'});return;}
      // A refusal that spent the plan (planUsed): Delete permanently could only fail now, so it
      // stays disabled and the reply says to click Delete… again (del-s3b conformance gap 3).
      done(!!d.planUsed);if(!d.planUsed)match();msg.hidden=false;msg.textContent=d.error||'Could not delete this archive.';
    }catch(e){done();match();msg.hidden=false;msg.textContent='Error: '+e;}
    finally{delInFlight--;w.removeAttribute('data-delete-waiting');if(delWait&&delInFlight===0&&!document.querySelector('[data-held-id]'))reloadPage();}
  });
  return w;
}
// Delete… (§3.1 step 1): every other panel closed, the plan route, then the panel inside the row
// — only for the last Delete… or Remove from list clicked, so no other panel opens meanwhile
// (MA-986).
async function delPlan(b,li){
  const my=++delSeq;
  delClose();
  noteBeside(li);
  const n=$('settingsNote'),done=busy(b,'Checking…',n,'Working out what Delete would remove. Nothing is deleted yet…');
  try{const p=await delPost('api/archive/delete/plan',{path:b.dataset.path});
    done();
    if(my!==delSeq)return;
    if(!p.ok){if(n){n.hidden=false;n.textContent=p.error||'Could not plan the delete.';}return;}
    if(n)n.hidden=true;
    li.append(delPanel(p));
    const t=li.querySelector('.delete-panel input');if(t)t.focus();
  }catch(e){done();if(my===delSeq&&n){n.hidden=false;n.textContent='Error: '+e;}}
}
document.querySelectorAll('[data-delete-archive]').forEach(b=>b.addEventListener('click',()=>{
  const li=b.closest('li');
  delSeq++; // an earlier Delete… or Remove from list still asking opens nothing now
  noteBeside(li); // said beside the row (MA-955)
  const h=delUnsavedFor(b.dataset.path);if(h){delAsk(h,()=>delPlan(b,li));return;}
  delPlan(b,li);
}));
// Remove from list (§3.2): the route's own question in the row, Yes or No; Yes stops only the
// schedules the question named.
function delUnlistAsk(li,b,q){
  const w=document.createElement('div');w.className='note unlist-ask';w.setAttribute('role','group');
  const t=document.createElement('p');t.textContent=q.question;w.append(t);
  const yes=delButton('Yes','btn'),no=delButton('No','btn secondary');
  const acts=delEl('div',null,'del-actions');acts.append(yes,no);w.append(acts);
  no.addEventListener('click',()=>{w.remove();const n=$('settingsNote');if(n){n.hidden=false;n.textContent='Nothing was changed.';}});
  yes.addEventListener('click',async()=>{
    const n=$('settingsNote'),done=busy(yes,'Removing…',n,'Removing this archive from the list…');
    try{const d=await delPost('api/archive/unlist',{path:b.dataset.path,confirm:true,named:q.named});
      if(d.ok){done(true);w.remove();const said=(d.lines||[]).join(' ')||'Removed from Your archives. The folder and its files are kept.';if(n){n.hidden=false;n.textContent=said;}
        // Said again at the top of the list once the page is drawn again, until the next reload (B14:
        // it showed for 1.5 seconds, then the row was simply gone).
        try{sessionStorage.setItem(delUnlistedKey,said);}catch(e){}
        delRowGone(li,'Removed from Your archives.');setTimeout(()=>reloadPage(),1500);return;}
      done();if(n){n.hidden=false;n.textContent=d.error||'Could not remove it from the list.';}
    }catch(e){done();if(n){n.hidden=false;n.textContent='Error: '+e;}}
  });
  return w;
}
document.querySelectorAll('[data-unlist-archive]').forEach(b=>b.addEventListener('click',async()=>{
  const li=b.closest('li');
  const my=++delSeq;
  noteBeside(li); // said beside the row (MA-955)
  delClose();
  const n=$('settingsNote'),done=busy(b,'Checking…',n,'Reading what Remove from list would change. Nothing is changed yet…');
  try{const q=await delPost('api/archive/unlist',{path:b.dataset.path});
    done();
    if(my!==delSeq)return;
    if(!q.ok){if(n){n.hidden=false;n.textContent=q.error||'Could not remove it from the list.';}return;}
    if(n)n.hidden=true;
    li.append(delUnlistAsk(li,b,q));
  }catch(e){done();if(my===delSeq&&n){n.hidden=false;n.textContent='Error: '+e;}}
}));
// What Remove from list said, shown once at the top of Your archives (or This archive) after the
// reload it started (B14, MA-1020); a page that cannot keep it says nothing more.
const delUnlistedKey='mailarchive-unlisted-note';
try{const said=sessionStorage.getItem(delUnlistedKey);
  if(said){sessionStorage.removeItem(delUnlistedKey);
    const card=$('sec-archives')||$('sec-this-archive');
    const note=delEl('div',said,'note');note.setAttribute('role','status');note.setAttribute('data-unlisted-note','');
    if(card){const h2=card.querySelector('h2');if(h2)h2.after(note);else card.prepend(note);}
    else{const top=$('heldDeletes');if(top)top.before(note);}}
}catch(e){}
// Every result the server holds is drawn when the page loads (a reload is no dismissal).
const hd=$('heldDeletes');
if(hd){try{JSON.parse(hd.dataset.heldDeletes||'[]').forEach(v=>delShowHeld(v));}catch(e){}}
// A delete under way when the page was drawn (a reload, another tab): its row says its result
// shows on this page when the delete ends, so the page asks until no delete is under way, then is
// drawn again, the result with it (MA-986).
function delWatch(){
  fetch('api/activity').then(r=>r.json()).then(d=>{if(d.deleting){setTimeout(delWatch,2000);return;}reloadPage();}).catch(()=>setTimeout(delWatch,5000));
}
if(document.querySelector('[data-delete-running]'))setTimeout(delWatch,2000);
`
