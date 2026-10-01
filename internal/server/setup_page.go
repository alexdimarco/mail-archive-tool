package server

import (
	"html/template"
	"strings"

	"mail-archive-tool/internal/graphconfig"
)

// setupPage renders the wizard, prefilled from any existing config. It reports
// whether a secret is already configured but NEVER emits the secret itself (W-C3).
func setupPage(cfgPath string, store graphconfig.SecretStore, csrf string) string {
	data := struct {
		CSRF             string
		Tenant, ClientID string
		Auth             string
		SecretConfigured bool
	}{CSRF: csrf, Auth: "device"}

	if cfg, err := graphconfig.Load(cfgPath); err == nil {
		data.Tenant, data.ClientID = cfg.Tenant, cfg.ClientID
		if cfg.Auth != "" {
			data.Auth = cfg.Auth
		}
		if cfg.Auth == "app" && cfg.Tenant != "" && cfg.ClientID != "" {
			if _, gerr := store.Get(graphconfig.AccountKey(cfg.Tenant, cfg.ClientID)); gerr == nil {
				data.SecretConfigured = true
			}
		}
	}

	var b strings.Builder
	if err := setupTmpl.Execute(&b, data); err != nil {
		return "<!doctype html><p>setup template error</p>"
	}
	return b.String()
}

var setupTmpl = template.Must(template.New("setup").Parse(`<!DOCTYPE html>
<html lang="en"><head>
<meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="csrf" content="{{.CSRF}}">
<title>MailArchive — Microsoft 365 setup</title>
<style>
:root{color-scheme:light dark;--bg:#f3f5f8;--panel:#fff;--text:#18212b;--muted:#667085;--line:#d9e0e8;--accent:#2563eb;--accent-soft:#eaf1ff;--good:#157347;--warn:#a15c00;--danger:#b42318}
@media (prefers-color-scheme:dark){:root{--bg:#0f1319;--panel:#171c24;--text:#eef2f6;--muted:#9aa4b2;--line:#2a3441;--accent:#8aa4ff;--accent-soft:#202943;--good:#62d59a;--warn:#f4c66b;--danger:#ff8a80}}
*{box-sizing:border-box}
body{margin:0;font:15px system-ui,-apple-system,Segoe UI,Roboto,sans-serif;color:var(--text);background:var(--bg)}
.wrap{max-width:680px;margin:0 auto;padding:34px 18px 60px}
h1{font-size:1.7rem;letter-spacing:-.02em;margin:0 0 4px}
.lede{color:var(--muted);margin:0 0 22px}
.card{background:var(--panel);border:1px solid var(--line);border-radius:14px;padding:22px;position:relative}
.card::before{content:"";position:absolute;left:0;right:0;top:0;height:3px;border-radius:14px 14px 0 0;background:var(--accent)}
label{display:block;font-weight:700;margin:16px 0 5px}
input{width:100%;padding:10px 11px;border:1px solid var(--line);border-radius:8px;background:var(--panel);color:var(--text)}
.help{color:var(--muted);font-size:.85rem;margin:5px 0 0}
.help b{color:var(--text)}
.modes{display:flex;gap:10px;margin:6px 0 0}
.mode{flex:1;border:1px solid var(--line);border-radius:10px;padding:11px 13px;cursor:pointer}
.mode.sel{border-color:var(--accent);background:var(--accent-soft)}
.mode strong{display:block}.mode span{color:var(--muted);font-size:.84rem}
.mode input{width:auto;margin-right:7px}
.btn{display:inline-flex;align-items:center;justify-content:center;padding:10px 16px;border-radius:8px;border:0;background:var(--accent);color:#fff;font-weight:750;cursor:pointer;margin-top:20px}
.btn:disabled{opacity:.5;cursor:not-allowed}
.note{border-radius:10px;padding:11px 13px;margin-top:16px;font-size:.9rem;display:none}
.note.ok{display:block;background:color-mix(in srgb,var(--good) 14%,var(--panel));border:1px solid color-mix(in srgb,var(--good) 35%,var(--line))}
.note.err{display:block;background:color-mix(in srgb,var(--danger) 14%,var(--panel));border:1px solid color-mix(in srgb,var(--danger) 35%,var(--line))}
.configured{color:var(--good);font-weight:700;font-size:.85rem;margin-top:5px}
.hidden{display:none}
</style></head>
<body>
<div class="wrap">
  <h1>Microsoft 365 setup</h1>
  <p class="lede">Enter the values from your Entra app registration. The archive then captures with no secret on the command line.</p>
  <div class="card">
    <label for="tenant">Directory (tenant) ID</label>
    <input id="tenant" value="{{.Tenant}}" placeholder="contoso.onmicrosoft.com or a GUID" autocomplete="off" spellcheck="false">
    <p class="help">App registration → <b>Overview</b> → Directory (tenant) ID.</p>

    <label for="clientId">Application (client) ID</label>
    <input id="clientId" value="{{.ClientID}}" placeholder="GUID" autocomplete="off" spellcheck="false">
    <p class="help">App registration → <b>Overview</b> → <b>Application (client) ID</b>.
       <b>Not an Object ID</b> — ignore both the app registration's Object ID and the
       enterprise application's Object ID; they sit next to it and are the wrong value.</p>

    <label>Authentication</label>
    <div class="modes">
      <label class="mode" id="mode-device"><input type="radio" name="auth" value="device"{{if ne .Auth "app"}} checked{{end}}><strong>Device (per-user)</strong><span>Sign in as yourself. No secret. Your own mailbox.</span></label>
      <label class="mode" id="mode-app"><input type="radio" name="auth" value="app"{{if eq .Auth "app"}} checked{{end}}><strong>App-only (admin)</strong><span>Client secret. Admin-consented mailboxes.</span></label>
    </div>

    <div id="secretRow"{{if ne .Auth "app"}} class="hidden"{{end}}>
      <label for="secret">Client secret</label>
      <input id="secret" type="password" placeholder="{{if .SecretConfigured}}•••••• (stored — leave blank to keep){{else}}paste the secret value{{end}}" autocomplete="off">
      {{if .SecretConfigured}}<div class="configured">✓ A secret is already stored in the credential manager.</div>{{end}}
      <p class="help">Stored in the OS credential manager (Windows) or a file readable only by you — never on the command line.</p>
    </div>

    <button class="btn" id="save">Save configuration</button>
    <div class="note" id="note"></div>
  </div>
</div>
<script src="/setup.js"></script>
</body></html>`))

// setupJS drives the mode toggle and POSTs the config via fetch with the CSRF
// token. Served at /setup.js so it runs under script-src 'self'.
const setupJS = `const $=s=>document.querySelector(s);
const csrf=document.querySelector('meta[name=csrf]').content;
function sel(){
  const app=document.querySelector('input[name=auth]:checked').value==='app';
  $('#secretRow').classList.toggle('hidden',!app);
  $('#mode-app').classList.toggle('sel',app);
  $('#mode-device').classList.toggle('sel',!app);
}
for(const r of document.querySelectorAll('input[name=auth]'))r.addEventListener('change',sel);
sel();
$('#save').addEventListener('click',async()=>{
  const note=$('#note');note.className='note';
  const body={tenant:$('#tenant').value,clientId:$('#clientId').value,
    auth:document.querySelector('input[name=auth]:checked').value,secret:$('#secret')?$('#secret').value:''};
  $('#save').disabled=true;
  try{
    const r=await fetch('/api/setup',{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':csrf},body:JSON.stringify(body)});
    const d=await r.json();
    if(d.ok){note.className='note ok';note.textContent='Saved. '+(d.auth==='device'?'Run a capture to sign in (device code).':(d.configured?'Secret stored.':'No secret stored.'));
      if($('#secret'))$('#secret').value='';}
    else{note.className='note err';note.textContent=d.error||'Save failed.';}
  }catch(e){note.className='note err';note.textContent='Save failed: '+e;}
  $('#save').disabled=false;
});
`
