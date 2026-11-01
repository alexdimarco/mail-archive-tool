package desktop

// dashboardJS is the Overview's script, served at /dashboard.js: the action buttons
// (dashboardBaseJS, dashboard.go) and, after them, Delete and Remove from list (archiveDeleteJS).
// One script, so the page's own busy, noteBeside and csrf serve both, and every page test that
// reads dashboardJS reads the delete's buttons too.
const dashboardJS = dashboardBaseJS + archiveDeleteJS

// archiveDeleteJS is the page's half of Delete and Remove from list (docs/design-archive-delete.md
// §3.1, §3.2, §8, §8.1, §10; slice 3b, MA-982..MA-985), on slice 3a's four routes. Every panel,
// question and result is the page's own, inside the row it is about (beside the row, MA-955),
// built with createElement and textContent only (X15): never a browser confirm() or prompt()
// (C19). Words are §3.1's, §3.2's and §8's where they give them; the rest are the builder's.
const archiveDeleteJS = `
// ---- Delete an archive and Remove from list (design-archive-delete §3, §8, §8.1, §10).
const delUnfinishedText='Delete not finished: click Delete again';
var delWait=false;
// reloadPage is every reload the page starts itself (§3.1 After a Delete, MA-984): while a
// delete's result is held on the page it does not reload and says the page refreshes when the
// result is dismissed; the page reloads once the last result is dismissed (delDismiss).
function reloadPage(){
  if(document.querySelector('[data-held-id]')){delWait=true;document.querySelectorAll('[data-held-wait]').forEach(w=>{w.hidden=false;});return;}
  location.reload();
}
function delPost(path,body){return fetch(path,{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':csrf},body:JSON.stringify(body)}).then(r=>r.json());}
function delEl(tag,text,cls){const e=document.createElement(tag);if(text!==undefined&&text!==null)e.textContent=text;if(cls)e.className=cls;return e;}
function delButton(text,cls){const b=delEl('button',text,cls);b.type='button';return b;}
// One panel at a time: a delete's panel or Remove from list's question closes any other one.
function delClose(){document.querySelectorAll('.delete-panel,.unlist-ask').forEach(x=>x.remove());}
// The server's match (archdelete.NameMatches): not empty, both sides NFC, no case folding, no trimming.
function delNameMatches(typed,name){return typed!==''&&typed.normalize('NFC')===name.normalize('NFC');}
// A file saved through the browser, as the fingerprint receipt is: a string as UTF-8, bytes as they are.
function delSaveFile(name,data){
  const blob=new Blob([data],{type:'text/plain;charset=utf-8'});
  const a=document.createElement('a');a.href=URL.createObjectURL(blob);a.download=name;
  document.body.appendChild(a);a.click();document.body.removeChild(a);
  setTimeout(()=>URL.revokeObjectURL(a.href),60000);
}
// The row of Your archives for path, by its own spelling.
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
// The archive this page was drawn for was deleted, or its delete did not finish: Archive now and
// Run now, and their sidebar links, are not offered for it.
function delPageArchive(unfinished){
  const text=unfinished?delUnfinishedText:'This archive was deleted: dismiss its result to see the page as it is now.';
  ['archiveBtn','runBackupBtn'].forEach(id=>{const x=$(id);if(x&&!x.hidden){x.hidden=true;const t=document.createElement('span');t.className='archive-tag';t.textContent=text;x.after(t);}});
  ['navArchive','navRunNow'].forEach(id=>{const x=$(id);if(x)x.hidden=true;});
}
// The held result of path whose record was not saved on this page, or null.
function delUnsavedFor(path){let r=null;document.querySelectorAll('[data-held-id]').forEach(h=>{if(h.dataset.row===path&&h.dataset.saved!=='1')r=h;});return r;}
// Dismiss: the result is no longer held; once none is, the page is drawn again (reload: false
// when a new Delete… goes on from here).
async function delDismiss(h,reload){
  const msg=h.querySelector('[data-held-msg]');
  try{const d=await delPost('api/archive/delete/dismiss',{id:h.dataset.heldId});
    if(!d.ok){if(msg){msg.hidden=false;msg.textContent=d.error||'Could not close this result.';}return false;}
    h.remove();
    if(reload&&!document.querySelector('[data-held-id]'))location.reload();
    return true;
  }catch(e){if(msg){msg.hidden=false;msg.textContent='Error: '+e;}return false;}
}
// "Close without saving the deletion record?" asked in the result itself (§3.1, §8.1); then, when
// given, what was asked for goes on.
function delAsk(h,then){
  const ask=h.querySelector('.held-ask'),yes=h.querySelector('[data-held-close]'),no=h.querySelector('[data-held-keep]');
  ask.hidden=false;yes.focus();
  yes.onclick=async()=>{const done=busy(yes,'Closing…',null,'Closing this result…');if(!await delDismiss(h,!then)){done();return;}if(then)then();};
  no.onclick=()=>{ask.hidden=true;const s=h.querySelector('[data-held-save]');if(s)s.focus();};
}
// A delete's result (§3.1 After a Delete, §8): its lines, Save deletion record (the record's exact
// bytes, any number of times) and Dismiss; where it sits follows "Where the result sits".
function delShowHeld(v){
  const h=document.createElement('div');h.className='note held-result';h.setAttribute('role','status');
  h.dataset.heldId=v.id;h.dataset.row=v.row;
  const head=delEl('p');head.append(delEl('b','Delete of '+v.folder));h.append(head);
  (v.lines||[]).forEach(t=>h.append(delEl('p',t)));
  const wait=delEl('p','This page refreshes when you dismiss this result.','card-sub');wait.setAttribute('data-held-wait','');wait.hidden=!delWait;h.append(wait);
  const acts=delEl('div',null,'del-actions');
  const sv=delButton('Save deletion record','btn');sv.setAttribute('data-held-save','');
  sv.addEventListener('click',()=>{delSaveFile(v.recordName,Uint8Array.from(atob(v.recordBase64),c=>c.charCodeAt(0)));h.dataset.saved='1';});
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
  const li=delRowOf(v.row);
  if(v.rowRemoved&&li)delRowGone(li,'Removed from Your archives.');
  if(v.rowRemoved||!li){const card=$('sec-archives'),top=$('heldDeletes');
    if(card){const h2=card.querySelector('h2');if(h2)h2.after(h);else card.prepend(h);}
    else if(top){top.hidden=false;top.append(h);}}
  else li.append(h);
  if(v.unfinished&&li&&!v.rowRemoved)delMarkUnfinished(li);
  if(v.row===drawnFor||v.folder===drawnFor)delPageArchive(v.unfinished);
}
// The plan, beside the row (§3.1): what Delete removes and what it does not reach, Save this plan,
// then the folder's name typed before Delete permanently can be pressed.
function delPanel(p){
  const w=document.createElement('div');w.className='note delete-panel';w.setAttribute('role','group');w.setAttribute('aria-label','Delete this archive');
  const line=(text,cls)=>{const x=delEl('p',text,cls);w.append(x);return x;};
  const head=delEl('p');head.append(delEl('b','Delete this archive permanently'));w.append(head);
  line(p.threeLine,'card-sub');
  line('Folder: '+p.folder,'mono');
  line('Delete removes '+p.files+' files ('+p.size+').');
  if(p.rerun)line('This finishes a delete that did not finish.');
  if(p.stores.length)line('Stores: '+p.stores.map(s=>'store '+s.number+' '+s.name).join(', '));
  if(p.schedules.length)line('Scheduled backups it stops: '+p.schedules.join(', '));
  if(p.checkYourself.length)line('Not removed, check it yourself: '+p.checkYourself.join(', '),'warn');
  p.notes.forEach(t=>line(t,'warn'));
  if(p.pausedLine)line(p.pausedLine);
  if(p.syncedLine)line(p.syncedLine,'warn');
  if(p.kept)line("Kept: not the archive's: "+p.kept);
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
  sv.addEventListener('click',()=>{delSaveFile(p.planName,p.planText);});
  sa.append(sv);w.append(sa);
  line(p.logLine,'card-sub');
  const lab=delEl('label',p.typeLine);const input=document.createElement('input');input.type='text';input.id='delTyped';input.autocomplete='off';input.spellcheck=false;lab.htmlFor='delTyped';
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
    try{const d=await delPost('api/archive/delete',{token:p.token,typed:input.value,unrecorded:!!(box&&box.checked)});
      if(d.id){done(true);delClose();delShowHeld(d);return;}
      done();match();msg.hidden=false;msg.textContent=d.error||'Could not delete this archive.';
    }catch(e){done();match();msg.hidden=false;msg.textContent='Error: '+e;}
  });
  return w;
}
// Delete… (§3.1 step 1): the plan route, then the panel inside the row.
async function delPlan(b,li){
  delClose();
  noteBeside(li);
  const n=$('settingsNote'),done=busy(b,'Checking…',n,'Working out what Delete would remove. Nothing is deleted yet…');
  try{const p=await delPost('api/archive/delete/plan',{path:b.dataset.path});
    done();
    if(!p.ok){if(n){n.hidden=false;n.textContent=p.error||'Could not plan the delete.';}return;}
    if(n)n.hidden=true;
    li.append(delPanel(p));
    const t=li.querySelector('.delete-panel input');if(t)t.focus();
  }catch(e){done();if(n){n.hidden=false;n.textContent='Error: '+e;}}
}
document.querySelectorAll('[data-delete-archive]').forEach(b=>b.addEventListener('click',()=>{
  const li=b.closest('li');
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
      if(d.ok){done(true);w.remove();if(n){n.hidden=false;n.textContent=(d.lines||[]).join(' ')||'Removed from Your archives. The folder and its files are kept.';}
        delRowGone(li,'Removed from Your archives.');setTimeout(()=>reloadPage(),1500);return;}
      done();if(n){n.hidden=false;n.textContent=d.error||'Could not remove it from the list.';}
    }catch(e){done();if(n){n.hidden=false;n.textContent='Error: '+e;}}
  });
  return w;
}
document.querySelectorAll('[data-unlist-archive]').forEach(b=>b.addEventListener('click',async()=>{
  const li=b.closest('li');
  noteBeside(li); // said beside the row (MA-955)
  delClose();
  const n=$('settingsNote'),done=busy(b,'Checking…',n,'Reading what Remove from list would change. Nothing is changed yet…');
  try{const q=await delPost('api/archive/unlist',{path:b.dataset.path});
    done();
    if(!q.ok){if(n){n.hidden=false;n.textContent=q.error||'Could not remove it from the list.';}return;}
    if(n)n.hidden=true;
    li.append(delUnlistAsk(li,b,q));
  }catch(e){done();if(n){n.hidden=false;n.textContent='Error: '+e;}}
}));
// Every result the server holds is drawn when the page loads (a reload is no dismissal).
const hd=$('heldDeletes');
if(hd){try{JSON.parse(hd.dataset.heldDeletes||'[]').forEach(v=>delShowHeld(v));}catch(e){}}
`
