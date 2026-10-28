package desktop

// encryptionJS is the Encryption card's script (the P3-3 plan, 3c): the only place a
// password or recovery key is typed in MailArchive Desktop (§7.1). The secret goes in
// one guarded POST's body and the field is cleared after it; nothing it answers is
// put in the page but as text (X15). It is its own file so the inert-script scan
// covers it (MA-407).
const encryptionJS = `(function(){
const csrf=document.querySelector('meta[name=csrf]').content;
const $=id=>document.getElementById(id);
function steps(){const c=$('sec-encsteps');if(c)c.hidden=false;}
function say(t){const n=$('encNote');if(n){steps();n.hidden=false;n.textContent=t;}}
// busy shows that a pressed button is working (MA-872): the button is disabled and marked
// aria-busy, its label becomes the working label, and the card's note says what is happening.
// It returns done, which the reply calls, success or error: done() gives the button back;
// done(true) leaves it disabled, for a page about to reload.
function busy(b,label,text){
  if(text)say(text);
  if(!b)return()=>{};
  const was=b.textContent,had=document.activeElement===b;
  b.disabled=true;b.setAttribute('aria-busy','true');if(label)b.textContent=label;
  return keep=>{b.removeAttribute('aria-busy');if(label)b.textContent=was;b.disabled=!!keep;
    if(had&&!keep&&b.isConnected&&(!document.activeElement||document.activeElement===document.body))b.focus();};
}
async function send(path,body){
  const r=await fetch(path,{method:'POST',headers:{'X-CSRF-Token':csrf,'Content-Type':'application/json'},body:JSON.stringify(body||{})});
  return r.json();
}
const form=$('encUnlockForm');
if(form)form.addEventListener('submit',async ev=>{
  ev.preventDefault();
  const s=$('encSecret'),rem=$('encRemember'),btn=$('encUnlock');
  if(!s||!s.value){say('Type the password or recovery key first.');return;}
  const body={secret:s.value,remember:!!(rem&&rem.checked)};
  s.value='';
  const done=busy(btn,'Unlocking…','Unlocking… this takes a few seconds.');
  let d=null;
  try{
    d=await send('api/encryption/unlock',body);
    for(let i=0;i<2&&d&&!d.ok&&d.confirm;i++){
      if(!confirm(d.error)){d={ok:false,error:'Nothing was opened.'};break;}
      if(d.confirm==='cost')body.cost=d.cost;else if(d.confirm==='rerecord')body.reRecord=true;
      d=await send('api/encryption/unlock',body);
    }
  }catch(e){d={ok:false,error:'Could not reach MailArchive Desktop — reload the page.'};}
  body.secret='';
  done(!!(d&&d.ok));
  if(d&&d.ok){say('Unlocked.');setTimeout(()=>location.reload(),600);}
  else say((d&&d.error)||'Could not unlock.');
});
const ur=$('encUnlockRemembered');
if(ur)ur.addEventListener('click',async()=>{
  const done=busy(ur,'Unlocking…','Unlocking… this takes a few seconds.');
  let d=null;
  try{d=await send('api/encryption/unlock',{remembered:true});}
  catch(e){d={ok:false,error:'Could not reach MailArchive Desktop — reload the page.'};}
  done(!!(d&&d.ok));
  if(d&&d.ok){say('Unlocked.');setTimeout(()=>location.reload(),600);}
  else say((d&&d.error)||'Could not unlock.');
});
// Make an unencrypted copy: the plan first, confirmed once — twice for a folder that
// may leave this computer — then the copy runs as the Desktop's task (K5, X14). The
// button is busy while each request runs, and given back while the questions are asked.
const cy=$('encCopy');
if(cy)cy.addEventListener('click',async()=>{
  let d=null,done=busy(cy,'Choosing…','A folder picker has opened — it may be behind this window.');
  try{d=await send('api/encryption/copy/plan');}
  catch(e){done();say('Could not reach MailArchive Desktop — reload the page.');return;}
  done();
  if(!d||!d.ok){say((d&&d.error)||'Could not plan the copy.');return;}
  if(d.canceled){say('Nothing was copied.');return;}
  const plan=(d.lines||[]).join('\n');
  if(!confirm(plan+'\n\nMake this copy?')){say('Nothing was copied.');return;}
  let again=false;
  if(d.askTwice){
    again=confirm('The readable copy may leave this computer:\n'+(d.leaves||[]).join('\n')+'\n\nMake it there anyway?');
    if(!again){say('Nothing was copied.');return;}
  }
  done=busy(cy,'Starting…','Starting the copy…');
  try{
    const s=await send('api/encryption/copy/start',{token:d.token,confirm:true,confirmAgain:again});
    say(s.ok?'The copy is being made — it shows in Activity as it goes. Locking the archive does not stop it; Shut down does.':(s.error||'Could not start the copy.'));
  }catch(e){say('Could not reach MailArchive Desktop — reload the page.');}
  finally{done();}
});
const lk=$('encLock');
if(lk)lk.addEventListener('click',async()=>{
  const done=busy(lk,'Locking…','Locking the archive…');
  try{const d=await send('api/encryption/lock');if(d.ok&&d.note){done(true);say(d.note);setTimeout(()=>location.reload(),6000);return;}if(d.ok){done(true);location.reload();return;}done();say(d.error||'Could not lock.');}
  catch(e){done();say('Could not reach MailArchive Desktop — reload the page.');}
});
// A key change: sent once, then again with the confirmation the page asked for.
async function change(path,body){
  let d=await send(path,body);
  for(let i=0;i<2&&d&&!d.ok&&d.confirm;i++){
    if(!confirm(d.question||d.error))return{ok:false,error:'Nothing was changed.'};
    if(d.confirm==='rerecord')body.reRecord=true;else body.confirm=true;
    d=await send(path,body);
  }
  return d;
}
function lines(d,fallback){return d.ok?((d.lines||[fallback]).join('\n')):(d.error||'Could not do that.');}
const cp=$('encChangePwBtn');
if(cp)cp.addEventListener('click',async()=>{
  const a=$('encNewPw'),b=$('encNewPw2');
  const body={newPassword:a?a.value:'',again:b?b.value:''};
  const done=busy(cp,'Changing…','Changing the password… this takes a few seconds.');
  try{const d=await change('api/encryption/change-password',body);body.newPassword='';body.again='';
    if(a)a.value='';if(b)b.value='';say(lines(d,'The password was changed.'));}
  catch(e){say('Could not reach MailArchive Desktop — reload the page.');}
  finally{done();}
});
let newKey='',token='',restoring=false,creating=false;
function showKey(on){const k=$('encNewKey');if(k)k.textContent=on?newKey:'';}
function startKey(d){
  newKey=d.recoveryKey;token=d.token;showKey(true);
  const w=$('encNewKeyWarn');if(w)w.textContent=d.warning||'';
  const box=$('encNewKeyBox');if(box)box.hidden=false;steps();
}
// Make a new encrypted archive: the wizard's windows choose what and where, then the
// password is typed here and the recovery key shown and typed back before anything
// is written.
document.querySelectorAll('[data-create-encrypted]').forEach(b=>b.addEventListener('click',async()=>{
  const done=busy(b,'Waiting for the wizard…','Answer the questions in the windows that open on your computer…');
  let d;
  try{d=await send('api/encryption/create',{});}catch(e){d={ok:false,error:'Could not reach MailArchive Desktop — reload the page.'};}
  done();
  if(d.canceled){say('Nothing was made.');return;}
  if(!d.ok){say(d.error||'Could not make an encrypted archive.');return;}
  token=d.token;creating=true;
  const p=$('encCreatePath'),h=$('encCreateHint'),f=$('encCreateForm');
  if(p)p.textContent='The new encrypted archive goes in '+d.path+'. Choose its password.';
  if(h)h.textContent=d.hint||'';
  if(f)f.hidden=false;say('');
}));
const ckb=$('encCreateKeyBtn');
if(ckb)ckb.addEventListener('click',async()=>{
  const a=$('encCreatePw'),c=$('encCreatePw2'),r=$('encCreateRemember'),pc=$('encCreateProgram');
  const body={token:token,newPassword:a?a.value:'',again:c?c.value:'',remember:!!(r&&r.checked),programCopies:!pc||pc.checked};
  const done=busy(ckb,'Making the key…','Making the key… this takes a few seconds.');
  let d;
  try{d=await send('api/encryption/create/key',body);}catch(e){d={ok:false,error:'Could not reach MailArchive Desktop — reload the page.'};}
  body.newPassword='';body.again='';done();
  if(!d.ok){say(d.error||'Could not make the key.');return;}
  if(a)a.value='';if(c)c.value='';
  const f=$('encCreateForm');if(f)f.hidden=true;
  startKey(d);say('');
});
const nk=$('encNewKeyBtn');
if(nk)nk.addEventListener('click',async()=>{
  const done=busy(nk,'Making a key…','Making a new recovery key…');
  try{const d=await change('api/encryption/new-recovery-key',{});done();
    if(!d.ok){say(d.error||'Could not make a new recovery key.');return;}
    startKey(d);nk.hidden=true;say('');}
  catch(e){done();say('Could not reach MailArchive Desktop — reload the page.');}
});
// Put back the key file: from the recovery file and recovery key, or the remembered key.
for(const [id,from] of [['encRestoreFileBtn','file'],['encRestoreRememberedBtn','remembered']]){
  const b=$(id);
  if(b)b.addEventListener('click',async()=>{
    const k=$('encRestoreKey'),a=$('encRestorePw'),c=$('encRestorePw2');
    const body={from:from,recoveryKey:k?k.value:'',newPassword:a?a.value:'',again:c?c.value:''};
    const done=busy(b,'Putting it back…',from==='file'?'Choose the recovery file in the window that opens — it may be behind this window.':'Putting back the key file… this takes a few seconds.');
    let d;
    try{
      d=await send('api/encryption/restore',body);
      for(let i=0;i<2&&d&&!d.ok&&d.confirm;i++){
        if(!confirm(d.error)){d={ok:false,error:'Nothing was changed.'};break;}
        if(d.confirm==='cost')body.cost=d.cost;else body.reRecord=true;
        d=await send('api/encryption/restore',body);
      }
    }catch(e){d={ok:false,error:'Could not reach MailArchive Desktop — reload the page.'};}
    body.recoveryKey='';body.newPassword='';body.again='';
    done();
    if(d&&d.canceled){say('Nothing was changed.');return;}
    if(!d||!d.ok){say((d&&d.error)||'Could not put back the key file.');return;}
    if(k)k.value='';if(a)a.value='';if(c)c.value='';
    restoring=true;startKey(d);say('');const f=$('encRestoreForm');if(f)f.hidden=true;
  });
}
const sv=$('encSaveFileBtn');
if(sv)sv.addEventListener('click',async()=>{
  const done=busy(sv,'Saving…','Choose where to save the recovery file in the window that opens — it may be behind this window.');
  try{const d=await send('api/encryption/new-recovery-key/save-file',{token:token});
    if(d.canceled){say('The recovery file was not saved.');return;}say(d.ok?('Saved: '+d.path+'\n'+d.note):(d.error||'Could not save the recovery file.'));}
  catch(e){say('Could not reach MailArchive Desktop — reload the page.');}
  finally{done();}
});
const sd=$('encSavedBtn');
if(sd)sd.addEventListener('click',()=>{showKey(false);const t=$('encTypeBack');if(t)t.hidden=false;sd.hidden=true;});
const sa=$('encShowAgainBtn');
if(sa)sa.addEventListener('click',()=>showKey(true));
const uk=$('encUseKeyBtn');
if(uk)uk.addEventListener('click',async()=>{
  const t=$('encTypedKey');
  const done=busy(uk,'Checking…','Checking the recovery key you typed…');
  try{const d=await send('api/encryption/new-recovery-key/confirm',{token:token,typed:t?t.value:''});
    if(d.ok){if(t)t.value='';newKey='';token='';showKey(false);const box=$('encNewKeyBox');if(box)box.hidden=true;}
    say(lines(d,'The new recovery key is in effect.'));
    if(d.ok&&(restoring||creating))setTimeout(()=>location.reload(),8000);}
  catch(e){say('Could not reach MailArchive Desktop — reload the page.');}
  finally{done();}
});
const ck=$('encCancelKeyBtn');
if(ck)ck.addEventListener('click',async()=>{
  const done=busy(ck,'Cancelling…','Cancelling…');
  try{await send('api/encryption/new-recovery-key/cancel',{token:token});}catch(e){}
  done();
  newKey='';token='';showKey(false);const box=$('encNewKeyBox');if(box)box.hidden=true;if(nk)nk.hidden=false;
  const f=$('encRestoreForm');if(f)f.hidden=false;restoring=false;
  const cf=$('encCreateForm');if(cf)cf.hidden=true;creating=false;
  say('Nothing was changed.');
});
for(const [id,on] of [['encRememberKey',true],['encForgetKey',false]]){
  const b=$(id);
  if(b)b.addEventListener('click',async()=>{
    const done=on?busy(b,'Remembering…','Remembering the key on this computer…'):busy(b,'Forgetting…','Forgetting the key on this computer…');
    try{const d=await change('api/encryption/remember',{on:on});if(d.ok){done(true);location.reload();return;}done();say(d.error||'Could not do that.');}
    catch(e){done();say('Could not reach MailArchive Desktop — reload the page.');}
  });
}
// Keep copies of MailArchive in the archive: saved at once; the next backup acts on it.
const pcs=$('encProgramCopies');
if(pcs)pcs.addEventListener('change',async()=>{
  const on=pcs.checked;pcs.disabled=true;
  let d;
  try{d=await send('api/encryption/program-copies',{on:on});}catch(e){d={ok:false,error:'Could not reach MailArchive Desktop — reload the page.'};}
  pcs.disabled=false;
  if(!d.ok)pcs.checked=!on;
  say(lines(d,'Saved.'));
  const p=$('encProgram');if(d.ok&&p)p.textContent=(d.lines||[''])[0];
});
})();
`
