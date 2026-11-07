package desktop

// restoreTBJS is the page's half of Restore to Thunderbird (docs/design-restore-thunderbird.md
// §7.3, §6.2, §7.4; slice R7; MA-1131), on the four api/restore-tb routes and the job slot's
// api/activity and api/job/cancel. It is the last part of dashboard.js, so it uses the page's
// own busy, showStep, csrf and the delete script's delEl, delButton and delSaveFile. Every step
// is brought into view with showStep, the keyboard on its first control and its status line
// saying what is needed (MA-1070's helper). Every word from the server — profile names, folder
// names, the plan's lines, the result — reaches the page through textContent only (X15), never a
// browser confirm() or prompt(). The tick and the second confirmation are only how the person
// says them: the server checks both (tbengine's Confirmed). Nothing here uses the browser's storage.
const restoreTBJS = `
// ---- Restore to Thunderbird (design-restore-thunderbird §7.3, slice R7).
const rtbBtn=$('rtbBtn');
// The list the server sent (its ID) and the position chosen in it: never a path (RT-15).
let rtbList='',rtbPos=-1,rtbToken='',rtbFollowing=false,rtbRecord=null,rtbChoice=false,rtbFirstProfile=null;
// rtbStatus says words on a step's own status line without moving the keyboard or the page
// (a step that changes while the person is elsewhere on the page; friction P2, P3).
function rtbStatus(id,t){const s=$(id),st=s&&s.querySelector('[data-step-status]');if(st){st.hidden=!t;st.textContent=t||'';}}
// rtbHere: the keyboard is in the card, or nowhere in particular, so a step may take it.
function rtbHere(){const a=document.activeElement;return !a||a===document.body||!!(a.closest&&a.closest('#sec-restore-tb'));}
// The R7 friction review, settled 2026-11-07 (the lead's words, owner-delegated).
const rtbNotAnswering='MailArchive Desktop is not answering — the restore may have stopped. Restart MailArchive Desktop to see what happened.';
const rtbChangeProfile='Choose the Thunderbird profile to restore into, then click Next.';
// rtbGroup groups a count's digits ("15,000"), as the plan and the progress do (F13).
function rtbGroup(n){return String(n).replace(/\B(?=(\d{3})+(?!\d))/g,',');}
function rtbPost(path,body){return fetch(path,{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':csrf},body:JSON.stringify(body||{})}).then(r=>r.json());}
function rtbSay(t){const n=$('rtbNote');if(n){n.hidden=!t;n.textContent=t||'';}}
const rtbSteps=['rtbProfileStep','rtbWhatStep','rtbPlanStep','rtbRunStep','rtbResultStep'];
// rtbOnly shows the panel with only the steps named (none: the panel closes).
function rtbOnly(ids){rtbSteps.forEach(id=>{const s=$(id);if(s)s.hidden=ids.indexOf(id)<0;});const p=$('rtbPanel');if(p)p.hidden=ids.length===0;}
function rtbClose(said){rtbOnly([]);rtbToken='';rtbSay(said);if(rtbBtn){rtbBtn.disabled=false;rtbBtn.focus();}}
const rtbUnreachable='Could not reach MailArchive Desktop — reload the page.';
if(rtbBtn)rtbBtn.addEventListener('click',async()=>{
  rtbSay('');
  const done=busy(rtbBtn,'Looking for Thunderbird…',$('rtbNote'),'Looking for Thunderbird profiles on this computer…');
  let d=null;
  try{d=await rtbPost('api/restore-tb/profiles');}catch(e){d={ok:false,error:rtbUnreachable};}
  if(!d.ok){done();rtbSay(d.error||'Could not look for Thunderbird profiles.');return;}
  done(true);rtbSay('');
  rtbList=d.listID;rtbProfiles(d);
});
// Step 1 (§3.1): skipped when there is one profile; otherwise each listed, the pre-selected one
// ticked, one that cannot be used shown with why and not offered.
function rtbProfiles(d){
  const ps=d.profiles||[],usable=ps.filter(p=>p.usable);
  rtbPos=d.default;rtbChoice=false;
  if(ps.length===1&&usable.length===1){rtbPos=usable[0].position;rtbWhat();return;}
  rtbChoice=true;
  const box=$('rtbProfiles');box.textContent='';
  let first=null;
  ps.forEach(p=>{
    const row=delEl('div'),lab=delEl('label'),r=document.createElement('input');
    r.type='radio';r.name='rtbProfile';r.value=String(p.position);r.id='rtbProfile'+p.position;r.setAttribute('data-rtb-profile','');
    if(!p.usable)r.disabled=true;
    if(p.position===d.default){r.checked=true;first=r;}
    if(!first&&p.usable)first=r;
    r.addEventListener('change',()=>{rtbPos=p.position;});
    lab.append(r,' '+p.name+' — '+p.dir+(p.opensByDefault?' (Thunderbird opens it by default)':'')+(p.backedUp?' (this archive backs it up)':''));
    row.append(lab);
    if(!p.usable&&p.unusable)row.append(delEl('p',p.unusable,'card-sub'));
    box.append(row);
  });
  rtbFirstProfile=first;
  const nx=delButton('Next','btn');nx.id='rtbNext';
  nx.addEventListener('click',()=>{if(rtbPos<0){showStep($('rtbProfileStep'),first,'Choose a profile first, then click Next.');return;}rtbWhat();});
  const cn=delButton('Cancel','btn secondary');cn.addEventListener('click',()=>rtbClose('Nothing was restored.'));
  const bar=delEl('div');bar.append(nx,' ',cn);box.append(bar);
  rtbOnly(['rtbProfileStep']);
  if(!usable.length){showStep($('rtbProfileStep'),cn,'None of the Thunderbird profiles on this computer can be restored into; each says why. Click Cancel.');nx.disabled=true;return;}
  showStep($('rtbProfileStep'),first,'This computer has more than one Thunderbird profile. Choose the one to restore into, then click Next.');
}
// Step 2 (§6.5): the whole archive, or the folders ticked in the tree, loaded when asked for.
function rtbWhat(){
  const t=$('rtbTree');if(t){t.hidden=!($('rtbChoose')&&$('rtbChoose').checked);}
  const bk=$('rtbBack');if(bk)bk.hidden=!rtbChoice;
  rtbOnly(['rtbWhatStep']);
  showStep($('rtbWhatStep'),$('rtbWhole'),'Next, choose what to restore: the whole archive, or the folders you tick. Then click Check: nothing is written until you click Restore.');
}
// Change profile (friction P1): back from what to restore to the profile list, the profile
// chosen still ticked and the keyboard on it.
const rtbBack=$('rtbBack');
if(rtbBack)rtbBack.addEventListener('click',()=>{
  rtbToken='';rtbOnly(['rtbProfileStep']);
  const on=rtbPos>=0?document.querySelector('[data-rtb-profile][value="'+rtbPos+'"]'):null;
  showStep($('rtbProfileStep'),on||rtbFirstProfile,rtbChangeProfile);
});
const rtbChooseR=$('rtbChoose'),rtbWholeR=$('rtbWhole');
if(rtbWholeR)rtbWholeR.addEventListener('change',()=>{const t=$('rtbTree');if(t)t.hidden=true;});
if(rtbChooseR)rtbChooseR.addEventListener('change',async()=>{
  const t=$('rtbTree');if(!t)return;t.hidden=false;
  const want=rtbList+':'+rtbPos;if(t.dataset.loaded===want)return;
  t.textContent='Loading the archive\'s folders…';
  let d=null;
  try{d=await rtbPost('api/restore-tb/folders',{listID:rtbList,position:rtbPos});}catch(e){d={ok:false,error:rtbUnreachable};}
  t.textContent='';
  if(!d.ok){showStep($('rtbWhatStep'),rtbWholeR,d.error||'Could not load the folders.');return;}
  t.dataset.loaded=want;
  const depth={};
  (d.folders||[]).forEach(f=>{
    depth[f.id]=f.parent&&depth[f.parent]!==undefined?depth[f.parent]+1:0;
    const row=delEl('div'),lab=delEl('label'),c=document.createElement('input');
    row.style.paddingLeft=(depth[f.id]*1.4)+'em';
    c.type='checkbox';c.value=f.id;c.setAttribute('data-rtb-folder','');if(f.parent)c.setAttribute('data-rtb-parent',f.parent);
    c.addEventListener('change',rtbTreeSync);
    lab.append(c,' '+f.name+' ('+rtbGroup(f.total)+')');row.append(lab);t.append(row);
  });
  showStep($('rtbWhatStep'),t.querySelector('[data-rtb-folder]')||rtbWholeR,'Tick the folders to restore; a folder brings its subfolders. The number beside each is how many messages in it and its subfolders can go back. Then click Check.');
});
// A ticked folder brings its subfolders (friction P6): they are shown ticked and cannot be
// unticked on their own; unticking the folder gives them back. The tree comes parents first.
function rtbTreeSync(){
  const on={};
  document.querySelectorAll('[data-rtb-folder]').forEach(c=>{
    const p=c.getAttribute('data-rtb-parent'),under=!!(p&&on[p]);
    if(under){c.checked=true;c.disabled=true;}else{if(c.disabled)c.checked=false;c.disabled=false;}
    on[c.value]=under||c.checked;
  });
}
const rtbCheck=$('rtbCheck');
if(rtbCheck)rtbCheck.addEventListener('click',async()=>{
  const whole=!(rtbChooseR&&rtbChooseR.checked),folders=[];
  document.querySelectorAll('[data-rtb-folder]').forEach(c=>{if(c.checked&&!c.disabled)folders.push(c.value);});
  const done=busy(rtbCheck,'Checking…',null,'Checking what the restore would do; nothing is written…');
  let d=null;
  try{d=await rtbPost('api/restore-tb/plan',{listID:rtbList,position:rtbPos,whole:whole,folders:whole?[]:folders});}catch(e){d={ok:false,error:rtbUnreachable};}
  done();
  // A refusal is said in place (§7.3 step 3), the keyboard on Check, which the words name.
  if(!d.ok){showStep($('rtbWhatStep'),rtbCheck,d.error||'Could not check the restore.');return;}
  rtbPlan(d);
});
// Step 3 (§6.4): the plan, every line in the engine's order (W-RECAPTURE first when it applies);
// for an encrypted archive the tick, and the second question only when the plan asks twice.
function rtbPlan(d){
  rtbToken=d.token;
  const ul=$('rtbPlan');ul.textContent='';
  (d.lines||[]).forEach(l=>{const li=delEl('li',l.words);li.setAttribute('data-code',l.code);ul.append(li);});
  const tb=$('rtbTickBox'),tk=$('rtbTick'),ab=$('rtbAgainBox'),ag=$('rtbAgain'),aw=$('rtbAgainWhy');
  if(tk)tk.checked=false;if(ag)ag.checked=false;
  if(tb)tb.hidden=!d.encrypted;
  if(ab)ab.hidden=!d.askTwice;
  if(aw)aw.textContent=d.againLine||'';
  rtbOnly(['rtbPlanStep']);
  if(d.encrypted)showStep($('rtbPlanStep'),tk,'This is what the restore will do; nothing is written yet. The restored mail will not be encrypted: tick the box below if you understand'+(d.askTwice?', and the second one,':',')+' then click Restore.');
  else showStep($('rtbPlanStep'),$('rtbGo'),'This is what the restore will do; nothing is written yet. Click Restore to go ahead, or Cancel.');
}
const rtbGo=$('rtbGo');
if(rtbGo)rtbGo.addEventListener('click',async()=>{
  const tk=$('rtbTick'),ag=$('rtbAgain');
  const body={token:rtbToken,notEncryptedOK:!!(tk&&tk.checked),confirmAgain:!!(ag&&ag.checked)};
  const done=busy(rtbGo,'Starting…',null,'Starting the restore…');
  let d=null;
  try{d=await rtbPost('api/restore-tb/start',body);}catch(e){d={ok:false,error:rtbUnreachable};}
  done();
  if(!d.ok){
    // The plan is spent: back to Check. A missing tick or second yes keeps it: said at that box.
    if(d.planUsed){rtbToken='';rtbOnly(['rtbWhatStep']);showStep($('rtbWhatStep'),rtbCheck,d.error||'Click Check again.');return;}
    showStep($('rtbPlanStep'),d.need==='tick'?tk:(d.need==='again'?ag:rtbGo),d.error||'Could not start the restore.');
    return;
  }
  rtbToken='';rtbRun(d.job,d.wait);
});
// Step 4: the restore runs as the Desktop's task. W-WAIT is said for the whole run; the progress
// line follows the task (counts only for an encrypted archive: the server never sends a name);
// Cancel the restore stops it, and its one cleanup removes what it wrote.
function rtbRun(job,wait){
  rtbOnly(['rtbRunStep']);
  const w=$('rtbWait');if(w)w.textContent=wait||'';
  const p=$('rtbProgress');if(p)p.textContent='Starting…';
  const st=$('rtbStop');if(st){st.hidden=false;st.disabled=false;}
  if(rtbBtn)rtbBtn.disabled=true;
  showStep($('rtbRunStep'),st,rtbRunWords);
  rtbFollow(job);
}
const rtbRunWords='The restore is running. Leave Thunderbird closed until it finishes. Cancel the restore stops it, and nothing is added to Thunderbird.';
// Friction P2 (settled 2026-11-07): when MailArchive Desktop stops answering (killed, crashed,
// the computer asleep), three missed asks in a row say so in place of a frozen progress line;
// the page keeps asking, and says the progress again if it answers.
function rtbFollow(job){
  if(rtbFollowing)return;rtbFollowing=true;
  let missed=0;
  const ask=()=>fetch('api/activity').then(r=>r.json()).then(d=>{
    missed=0;
    if(String(d.job||'')!==String(job)||!d.restore){rtbFollowing=false;rtbEnded({words:'The restore has ended, but another task ran after it, so how it ended is not shown here.'});return;}
    const r=d.restore,p=$('rtbProgress'),w=$('rtbWait');
    if(r.running){if(w&&r.wait)w.textContent=r.wait;if(p)p.textContent=r.checking?(r.checkingLine||r.line):(r.line||'Starting…');rtbStatus('rtbRunStep',rtbRunWords);setTimeout(ask,1000);return;}
    rtbFollowing=false;rtbEnded(r.result||{words:r.error||'The restore ended.'});
  }).catch(()=>{
    // The status line says it once; the progress line keeps what was last seen, marked as such
    // (the Edge re-walk, 2026-11-07: the same words twice read as a stutter).
    if(++missed>=3){const p=$('rtbProgress');if(p&&p.textContent&&p.textContent.indexOf('Last seen: ')!==0)p.textContent='Last seen: '+p.textContent;rtbStatus('rtbRunStep',rtbNotAnswering);}
    setTimeout(ask,3000);
  });
  ask();
}
const rtbStop=$('rtbStop');
if(rtbStop)rtbStop.addEventListener('click',async()=>{
  const done=busy(rtbStop,'Cancelling…',null,'Asking the restore to stop; it removes what it wrote…');
  try{const d=await rtbPost('api/job/cancel');if(!d.ok){const p=$('rtbProgress');if(p)p.textContent=d.error||'Could not cancel the restore.';}}catch(e){}
  done(true);
});
// Step 5 (§7.4): the result, in the engine's words, with Save this record — saved by the browser
// where the person chooses; for an encrypted archive it says beside the button that the record
// names the restored folders (§6.2 note).
function rtbEnded(v){
  rtbOnly(['rtbResultStep']);
  const res=$('rtbResult');if(res)res.textContent=v.words||'';
  const sv=$('rtbSave'),note=$('rtbRecordNote');
  rtbRecord=v.record?v:null;
  if(sv)sv.hidden=!v.record;
  if(note){note.hidden=!(v.record&&v.recordNote);note.textContent=v.recordNote||'';}
  if(rtbBtn)rtbBtn.disabled=false;
  const lead=v.done?'The restore has finished.':(v.stopped?'The restore was stopped.':'The restore did not finish.');
  const said=lead+' What happened is said below.'+(v.record?' Save this record keeps a copy as a text file.':'');
  // Friction P3: a restore that ends while the keyboard is elsewhere on the page (Confirm shut
  // down, another card) says its result in place and leaves the keyboard where it is.
  if(rtbHere())showStep($('rtbResultStep'),v.record?sv:$('rtbDone'),said);
  else rtbStatus('rtbResultStep',said);
}
const rtbSave=$('rtbSave');
if(rtbSave)rtbSave.addEventListener('click',()=>{if(rtbRecord)delSaveFile(rtbRecord.recordName,rtbRecord.record);});
for(const [id,said] of [['rtbClose','Nothing was restored.'],['rtbPlanCancel','Nothing was restored.'],['rtbDone','']]){
  const b=$(id);if(b)b.addEventListener('click',()=>rtbClose(said));
}
// A page drawn while a restore runs follows it again in its panel.
if(rtbBtn)fetch('api/activity').then(r=>r.json()).then(d=>{if(d&&d.restore&&d.restore.running)rtbRun(d.job,d.restore.wait);}).catch(()=>{});
`
