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
  if(btn)btn.disabled=true;
  say('Unlocking… this takes a few seconds.');
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
  if(btn)btn.disabled=false;
  if(d&&d.ok){say('Unlocked.');setTimeout(()=>location.reload(),600);}
  else say((d&&d.error)||'Could not unlock.');
});
const lk=$('encLock');
if(lk)lk.addEventListener('click',async()=>{
  try{const d=await send('api/encryption/lock');if(d.ok){location.reload();return;}say(d.error||'Could not lock.');}
  catch(e){say('Could not reach MailArchive Desktop — reload the page.');}
});
// A key change: sent once, then again with the confirmation the page asked for.
async function change(path,body){
  let d=await send(path,body);
  for(let i=0;i<2&&d&&!d.ok&&d.confirm;i++){
    if(!confirm(d.error))return{ok:false,error:'Nothing was changed.'};
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
  try{const d=await change('api/encryption/change-password',body);body.newPassword='';body.again='';
    if(a)a.value='';if(b)b.value='';say(lines(d,'The password was changed.'));}
  catch(e){say('Could not reach MailArchive Desktop — reload the page.');}
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
  b.disabled=true;say('Answer the questions in the windows that open on your computer…');
  let d;
  try{d=await send('api/encryption/create',{});}catch(e){d={ok:false,error:'Could not reach MailArchive Desktop — reload the page.'};}
  b.disabled=false;
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
  const a=$('encCreatePw'),c=$('encCreatePw2'),r=$('encCreateRemember');
  const body={token:token,newPassword:a?a.value:'',again:c?c.value:'',remember:!!(r&&r.checked)};
  ckb.disabled=true;say('Making the key… this takes a few seconds.');
  let d;
  try{d=await send('api/encryption/create/key',body);}catch(e){d={ok:false,error:'Could not reach MailArchive Desktop — reload the page.'};}
  body.newPassword='';body.again='';ckb.disabled=false;
  if(!d.ok){say(d.error||'Could not make the key.');return;}
  if(a)a.value='';if(c)c.value='';
  const f=$('encCreateForm');if(f)f.hidden=true;
  startKey(d);say('');
});
const nk=$('encNewKeyBtn');
if(nk)nk.addEventListener('click',async()=>{
  try{const d=await change('api/encryption/new-recovery-key',{});
    if(!d.ok){say(d.error||'Could not make a new recovery key.');return;}
    startKey(d);nk.hidden=true;}
  catch(e){say('Could not reach MailArchive Desktop — reload the page.');}
});
// Put back the key file: from the recovery file and recovery key, or the remembered key.
for(const [id,from] of [['encRestoreFileBtn','file'],['encRestoreRememberedBtn','remembered']]){
  const b=$(id);
  if(b)b.addEventListener('click',async()=>{
    const k=$('encRestoreKey'),a=$('encRestorePw'),c=$('encRestorePw2');
    const body={from:from,recoveryKey:k?k.value:'',newPassword:a?a.value:'',again:c?c.value:''};
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
    if(d&&d.canceled)return;
    if(!d||!d.ok){say((d&&d.error)||'Could not put back the key file.');return;}
    if(k)k.value='';if(a)a.value='';if(c)c.value='';
    restoring=true;startKey(d);const f=$('encRestoreForm');if(f)f.hidden=true;
  });
}
const sv=$('encSaveFileBtn');
if(sv)sv.addEventListener('click',async()=>{
  try{const d=await send('api/encryption/new-recovery-key/save-file',{token:token});
    if(d.canceled)return;say(d.ok?('Saved: '+d.path+'\n'+d.note):(d.error||'Could not save the recovery file.'));}
  catch(e){say('Could not reach MailArchive Desktop — reload the page.');}
});
const sd=$('encSavedBtn');
if(sd)sd.addEventListener('click',()=>{showKey(false);const t=$('encTypeBack');if(t)t.hidden=false;sd.hidden=true;});
const sa=$('encShowAgainBtn');
if(sa)sa.addEventListener('click',()=>showKey(true));
const uk=$('encUseKeyBtn');
if(uk)uk.addEventListener('click',async()=>{
  const t=$('encTypedKey');
  try{const d=await send('api/encryption/new-recovery-key/confirm',{token:token,typed:t?t.value:''});
    if(d.ok){if(t)t.value='';newKey='';token='';showKey(false);const box=$('encNewKeyBox');if(box)box.hidden=true;}
    say(lines(d,'The new recovery key is in effect.'));
    if(d.ok&&(restoring||creating))setTimeout(()=>location.reload(),8000);}
  catch(e){say('Could not reach MailArchive Desktop — reload the page.');}
});
const ck=$('encCancelKeyBtn');
if(ck)ck.addEventListener('click',async()=>{
  try{await send('api/encryption/new-recovery-key/cancel',{token:token});}catch(e){}
  newKey='';token='';showKey(false);const box=$('encNewKeyBox');if(box)box.hidden=true;if(nk)nk.hidden=false;
  const f=$('encRestoreForm');if(f)f.hidden=false;restoring=false;
  const cf=$('encCreateForm');if(cf)cf.hidden=true;creating=false;
  say('Nothing was changed.');
});
for(const [id,on] of [['encRememberKey',true],['encForgetKey',false]]){
  const b=$(id);
  if(b)b.addEventListener('click',async()=>{
    try{const d=await change('api/encryption/remember',{on:on});if(d.ok){location.reload();return;}say(d.error||'Could not do that.');}
    catch(e){say('Could not reach MailArchive Desktop — reload the page.');}
  });
}
})();
`
