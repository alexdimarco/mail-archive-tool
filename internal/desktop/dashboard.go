// Package desktop is the in-process dashboard-and-launcher behind the
// `mailarchive-desktop` command: a loopback-only local web control panel that
// drives the engine (capture, sign-in, reader, scheduling) by calling our
// internal packages directly — never by shelling out to a child binary
// (design-mailarchive-desktop). Slice 1 is the informational shell: status cards,
// the Microsoft 365 connection, and the Security & privacy panel.
package desktop

import (
	"encoding/json"
	"html/template"
	"net/http"
	"runtime"
	"strings"

	"mail-archive-tool/internal/graph"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/server"
)

// Config wires the dashboard to one archive and the Graph config/credentials.
type Config struct {
	Out        string
	ConfigPath string
	Store      graphconfig.SecretStore
	// TokenStore/TokenCachePath locate the device sign-in token (vault on Windows,
	// a file elsewhere) so the dashboard can show whether you are signed in and
	// clear it. Both may be zero in the informational shell.
	TokenStore     graph.TokenStore
	TokenCachePath string

	// SettingsPath is the dashboard preferences file (archive location, raw,
	// Deleted/Junk, schedule); default under the OS config dir.
	SettingsPath string

	// BaseURL/TokenURL/DeviceAuthURL are test overrides for the in-process capture
	// (empty = the Microsoft production endpoints).
	BaseURL, TokenURL, DeviceAuthURL string

	// ReaderURL is where the embedded archive reader is served; the Open-archive /
	// Go-back links point here. Empty in the informational shell.
	ReaderURL string
}

// tokenCfg locates the device sign-in token for a configured tenant/client: the
// Config overrides (used by tests), else the OS default — Credential Manager on
// Windows (vault), a 0600 file elsewhere — matching the CLI so both see one sign-in.
func (cfg Config) tokenCfg(c *graphconfig.Config) graph.Config {
	store, path := cfg.TokenStore, cfg.TokenCachePath
	if store == nil && path == "" {
		store = graphconfig.DefaultTokenStore(c.Tenant, c.ClientID)
		path, _ = graphconfig.DefaultTokenCachePath(c.Tenant, c.ClientID)
	}
	return graph.Config{Tenant: c.Tenant, ClientID: c.ClientID, TokenStore: store, TokenCachePath: path}
}

// signInState reports whether a saved sign-in exists (reads only the local token
// cache, no network) and the mailbox it is for.
func (cfg Config) signInState() (string, bool) {
	c := cfg.load()
	if c == nil || strings.TrimSpace(c.Tenant) == "" || strings.TrimSpace(c.ClientID) == "" {
		return "", false
	}
	return graph.SignedIn(cfg.tokenCfg(c))
}

// dashboard holds the per-process CSRF token and the live capture state.
type dashboard struct {
	cfg  Config
	csrf string
	cap  *captureState
}

// DashboardHandler serves the dashboard: GET "/" (Overview), the static script,
// and the loopback-only, CSRF/Origin/Host-guarded action endpoints.
func DashboardHandler(cfg Config) http.Handler {
	d := &dashboard{cfg: cfg, csrf: server.NewCSRFToken(), cap: &captureState{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(d.cfg.overview(d.csrf)))
	})
	mux.HandleFunc("/dashboard.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Write([]byte(dashboardJS))
	})
	mux.HandleFunc("/api/clear-signin", d.clearSignin)
	mux.HandleFunc("/api/capture", d.capture)
	mux.HandleFunc("/api/activity", d.activity)
	mux.HandleFunc("/api/settings", d.saveSettings)
	return dashboardHeaders(mux)
}

// saveSettings persists the dashboard preferences (loopback-only, CSRF-guarded).
func (d *dashboard) saveSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if !server.GuardLocalPOST(r, d.csrf) {
		http.Error(w, "refused: same-origin, tokened, local requests only", http.StatusForbidden)
		return
	}
	var in struct {
		Out                                  string
		KeepRaw, IncludeDeleted, IncludeJunk bool
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&in); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	s := d.cfg.settings()
	s.Out = strings.TrimSpace(in.Out)
	s.KeepRaw, s.IncludeDeleted, s.IncludeJunk = in.KeepRaw, in.IncludeDeleted, in.IncludeJunk
	if err := SaveSettings(d.cfg.SettingsPath, s); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// clearSignin removes the saved delegated sign-in (loopback-only, CSRF-guarded), so
// the next capture performs a fresh device sign-in.
func (d *dashboard) clearSignin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if !server.GuardLocalPOST(r, d.csrf) {
		http.Error(w, "refused: same-origin, tokened, local requests only", http.StatusForbidden)
		return
	}
	c := d.cfg.load()
	if c == nil {
		writeJSON(w, map[string]any{"ok": false, "error": "not configured"})
		return
	}
	if err := graph.ClearSignIn(d.cfg.tokenCfg(c)); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}

// dashCSP mirrors the setup CSP: same-origin script + fetch (later slices POST via
// fetch), inline styles, data: images; nothing remote, never framed.
const dashCSP = "default-src 'none'; script-src 'self'; connect-src 'self'; style-src 'unsafe-inline'; img-src data:; frame-ancestors 'none'; base-uri 'none'"

func dashboardHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", dashCSP)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

type card struct{ Label, Value, Class string } // Class: good | warn | ""

// cards computes the four Overview status cards from real state. The detailed
// GREEN/WARN/RED "Status & health" view (mirroring the `status` verb) arrives in a
// later slice; these four are the dashboard's operational snapshot.
func (cfg Config) cards() []card {
	out := []card{{"Archive engine", "Ready", "good"}}

	conn := card{"Microsoft 365", "Not configured", "warn"}
	if c := cfg.load(); c != nil && strings.TrimSpace(c.Tenant) != "" {
		if _, ok := cfg.signInState(); ok {
			conn = card{"Microsoft 365", "Signed in", "good"}
		} else {
			conn = card{"Microsoft 365", "Sign-in needed", "warn"}
		}
	}
	out = append(out, conn)

	out = append(out, card{"Capture", "Idle", "good"})

	sc := card{"Weekly backup", "Not installed", "warn"}
	if strings.TrimSpace(cfg.Out) != "" {
		switch schedule.Query(schedule.DefaultNameFor(cfg.Out)) {
		case schedule.Installed:
			sc = card{"Weekly backup", "Installed", "good"}
		case schedule.SchedulerUnavailable:
			sc = card{"Weekly backup", "Scheduler unavailable", "warn"}
		}
	}
	return append(out, sc)
}

func (cfg Config) load() *graphconfig.Config {
	if strings.TrimSpace(cfg.ConfigPath) == "" {
		return nil
	}
	c, err := graphconfig.Load(cfg.ConfigPath)
	if err != nil {
		return nil
	}
	return c
}

func authLabel(a string) string {
	if strings.EqualFold(a, "app") {
		return "app-only"
	}
	return "device"
}

// vaultPhrase is the OS-aware credential-storage wording (DC5): the client secret
// and sign-in live in Windows Credential Manager on Windows, a 0600 file elsewhere.
func vaultPhrase() string {
	if runtime.GOOS == "windows" {
		return "Windows Credential Manager"
	}
	return "a file only you can read"
}

func (cfg Config) overview(csrf string) string {
	c := cfg.load()
	upn, signedIn := cfg.signInState()
	s := cfg.settings()
	data := struct {
		CSRF           string
		Cards          []card
		Configured     bool
		SignedIn       bool
		UPN            string
		Tenant         string
		ClientID       string
		Auth           string
		Vault          string
		OutDir         string
		ReaderURL      string
		KeepRaw        bool
		IncludeDeleted bool
		IncludeJunk    bool
	}{CSRF: csrf, Cards: cfg.cards(), Vault: vaultPhrase(), OutDir: cfg.effectiveOut(), ReaderURL: cfg.ReaderURL, SignedIn: signedIn, UPN: upn,
		KeepRaw: s.KeepRaw, IncludeDeleted: s.IncludeDeleted, IncludeJunk: s.IncludeJunk}
	if c != nil {
		data.Configured = strings.TrimSpace(c.Tenant) != ""
		data.Tenant, data.ClientID, data.Auth = c.Tenant, c.ClientID, authLabel(c.Auth)
	}
	var b strings.Builder
	if err := overviewTmpl.Execute(&b, data); err != nil {
		return "<!doctype html><p>dashboard template error</p>"
	}
	return b.String()
}

// Loopback re-exports server.IsLoopback so main can gate the bind without importing
// the server package directly for one predicate.
func Loopback(addr string) bool { return server.IsLoopback(addr) }

var overviewTmpl = template.Must(template.New("overview").Parse(`<!DOCTYPE html>
<html lang="en"><head>
<meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="csrf" content="{{.CSRF}}">
<title>MailArchive Desktop</title>
<style>
:root{color-scheme:light dark;--bg:#f3f5f8;--panel:#fff;--text:#18212b;--muted:#667085;--line:#d9e0e8;--accent:#2563eb;--accent-soft:#eaf1ff;--good:#157347;--warn:#a15c00;--danger:#b42318;--sidebar:#0f172a;--sidebar-text:#e5e7eb;--sidebar-muted:#94a3b8}
@media(prefers-color-scheme:dark){:root{--bg:#0f1319;--panel:#171c24;--text:#eef2f6;--muted:#9aa4b2;--line:#2a3441;--accent:#8aa4ff;--accent-soft:#202943;--good:#62d59a;--warn:#f4c66b;--danger:#ff8a80;--sidebar:#090d13;--sidebar-text:#eef2f6;--sidebar-muted:#8e99a8}}
*{box-sizing:border-box}
body{margin:0;font:15px system-ui,-apple-system,Segoe UI,Roboto,sans-serif;background:var(--bg);color:var(--text)}
.app-shell{min-height:100vh;display:grid;grid-template-columns:246px minmax(0,1fr)}
.sidebar{background:var(--sidebar);color:var(--sidebar-text);padding:22px 16px;display:flex;flex-direction:column;gap:18px}
.brand{font-weight:800;font-size:1.05rem}.brand span{display:block;color:var(--sidebar-muted);font-size:.78rem;font-weight:500;margin-top:2px}
.nav-title{color:var(--sidebar-muted);font-size:.72rem;font-weight:800;letter-spacing:.08em;text-transform:uppercase;margin:10px 0 4px}
.nav a{display:block;color:var(--sidebar-text);text-decoration:none;padding:9px 11px;border-radius:9px;font-weight:650}
.nav a.active{background:rgba(255,255,255,.11)}.nav a:hover{background:rgba(255,255,255,.08)}
.sidebar-foot{margin-top:auto;color:var(--sidebar-muted);font-size:.78rem}
.content{padding:30px 34px 52px;min-width:0}.inner{max-width:1040px;margin:0 auto}
h1{font-size:1.8rem;letter-spacing:-.02em;margin:0 0 4px}.lede{color:var(--muted);margin:0 0 22px}
.status-grid{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:12px;margin-bottom:20px}
.status-card{background:var(--panel);border:1px solid var(--line);border-radius:12px;padding:15px}
.status-card .label{color:var(--muted);font-size:.8rem}.status-card .value{font-size:1.08rem;font-weight:800;margin-top:6px;overflow-wrap:anywhere}
.good{color:var(--good)}.warn{color:var(--warn)}
.card{background:var(--panel);border:1px solid var(--line);border-radius:14px;padding:19px;margin-bottom:16px}
.card h2{font-size:1.05rem;margin:0 0 12px}
.row{display:grid;grid-template-columns:180px minmax(0,1fr);gap:12px;padding:8px 0;border-bottom:1px solid var(--line)}
.row:last-child{border-bottom:0}.row .k{color:var(--muted);font-size:.85rem}.row .v{font-weight:650;overflow-wrap:anywhere}
.sec li{margin:8px 0;line-height:1.45}.sec b{color:var(--text)}
.mono{font-family:ui-monospace,SFMono-Regular,Consolas,monospace}
.btn-link{background:none;border:0;color:var(--accent);font:inherit;font-weight:700;cursor:pointer;padding:0;text-decoration:underline}
.note{margin-top:12px;padding:10px 12px;border-radius:9px;font-size:.9rem;background:var(--accent-soft);border:1px solid color-mix(in srgb,var(--accent) 30%,var(--line))}
.card-sub{color:var(--muted);font-size:.9rem;margin:-4px 0 14px}
.btn{display:inline-flex;align-items:center;justify-content:center;padding:9px 15px;border-radius:8px;border:0;background:var(--accent);color:#fff;font-weight:750;cursor:pointer}
.btn.secondary{background:var(--panel);color:var(--accent);border:1px solid var(--accent)}
.btn:disabled{opacity:.5;cursor:not-allowed}
label{display:block;font-weight:700;margin:14px 0 5px}
input{width:100%;padding:10px 11px;border:1px solid var(--line);border-radius:8px;background:var(--panel);color:var(--text)}
.checkline{display:flex;align-items:center;gap:8px;font-weight:600;margin-top:10px}.checkline input{width:auto}
.device-box{margin-top:14px;padding:14px;border:2px dashed var(--accent);border-radius:10px;background:var(--accent-soft)}
.device-code{font:800 1.7rem/1.2 ui-monospace,SFMono-Regular,Consolas,monospace;letter-spacing:.1em;margin-top:6px}
pre{white-space:pre-wrap;overflow-wrap:anywhere;background:var(--bg);border:1px solid var(--line);border-radius:10px;padding:12px;max-height:280px;overflow:auto;margin-top:14px;font-size:.84rem}
@media(max-width:900px){.app-shell{grid-template-columns:1fr}.status-grid{grid-template-columns:1fr 1fr}.sidebar-foot{display:none}}
</style></head>
<body>
<div class="app-shell">
  <aside class="sidebar">
    <div class="brand">MailArchive<span>Desktop</span></div>
    <nav class="nav">
      <div class="nav-title">Workspace</div>
      <a class="active" href="/">Overview</a>
      <a href="#" onclick="document.getElementById('archiveBtn').click();return false">Archive now</a>
      <a href="{{.ReaderURL}}" target="_blank" rel="noopener">Open archive</a>
      <a href="{{.ReaderURL}}goback" target="_blank" rel="noopener">Go back in time</a>
      <div class="nav-title">Diagnostics</div>
      <a href="/">Status &amp; health</a>
      <a href="/">Activity log</a>
      <div class="nav-title">Setup</div>
      <a href="/">Microsoft 365 connection</a>
      <a href="/">Weekly backup</a>
    </nav>
    <div class="sidebar-foot">Local control panel<br>127.0.0.1</div>
  </aside>
  <main class="content"><div class="inner">
    <h1>Overview</h1>
    <p class="lede">Your personal Microsoft 365 archive, running locally on this PC.</p>

    <div class="status-grid">
      {{range .Cards}}<div class="status-card"><div class="label">{{.Label}}</div><div class="value {{.Class}}">{{.Value}}</div></div>{{end}}
    </div>

    <div class="card">
      <h2>Microsoft 365 connection</h2>
      {{if .Configured}}
      <div class="row"><div class="k">Tenant</div><div class="v mono">{{.Tenant}}</div></div>
      <div class="row"><div class="k">Application (client) ID</div><div class="v mono">{{.ClientID}}</div></div>
      <div class="row"><div class="k">Sign-in mode</div><div class="v">{{.Auth}}</div></div>
      <div class="row"><div class="k">Sign-in</div><div class="v">{{if .SignedIn}}<span class="good">Signed in as {{.UPN}}</span> &nbsp; <button class="btn-link" id="clearSignin">Clear sign-in</button>{{else}}<span class="warn">Sign-in needed</span> — the first capture signs you in with a Microsoft device code.{{end}}</div></div>
      {{else}}
      <div class="row"><div class="k">Status</div><div class="v warn">Not configured — run <span class="mono">mailarchive setup</span> to add your tenant and application ID.</div></div>
      {{end}}
      {{if .OutDir}}<div class="row"><div class="k">Archive location</div><div class="v mono">{{.OutDir}}</div></div>{{end}}
      <div class="note" id="note" hidden></div>
    </div>

    <div class="card">
      <h2>Archive now</h2>
      <p class="card-sub">Fetch the latest mail into your archive. The first time, you sign in with a Microsoft device code shown here.</p>
      <button class="btn" id="archiveBtn">Archive now</button>
      {{if .ReaderURL}}<a class="btn secondary" href="{{.ReaderURL}}" target="_blank" rel="noopener">Open archive</a> <a class="btn secondary" href="{{.ReaderURL}}goback" target="_blank" rel="noopener">Go back in time</a>{{end}}
      <div class="device-box" id="device" hidden>
        <div>To sign in, open <a id="deviceUrl" target="_blank" rel="noopener">this Microsoft page</a> and enter the code:</div>
        <div class="device-code" id="deviceCode"></div>
      </div>
      <pre id="activity" hidden></pre>
      <div class="note" id="captureResult" hidden></div>
    </div>

    <div class="card">
      <h2>Settings</h2>
      <label for="outDir">Archive location</label>
      <input id="outDir" value="{{.OutDir}}" placeholder="e.g. C:\MailArchive, a mapped drive, or a UNC path" spellcheck="false">
      <label class="checkline"><input type="checkbox" id="keepRaw"{{if .KeepRaw}} checked{{end}}> Keep each message's original .eml too</label>
      <label class="checkline"><input type="checkbox" id="incDeleted"{{if .IncludeDeleted}} checked{{end}}> Include Deleted Items</label>
      <label class="checkline"><input type="checkbox" id="incJunk"{{if .IncludeJunk}} checked{{end}}> Include Junk Email</label>
      <div style="margin-top:14px"><button class="btn secondary" id="saveSettings">Save settings</button></div>
      <div class="note" id="settingsNote" hidden></div>
    </div>

    <div class="card sec">
      <h2>Security &amp; privacy</h2>
      <ul>
        <li><b>Runs entirely on this PC.</b> The control panel and your archive are local; this page is served only on 127.0.0.1 (this computer) and refuses any network address.</li>
        <li><b>Read-only to your mailbox.</b> The tool makes only Microsoft read requests (Mail.Read). It can never send, move, change, or delete anything in your mailbox.</li>
        <li><b>Your sign-in stays on this machine.</b> Your Microsoft sign-in (and any app secret) is stored in {{.Vault}} — never uploaded anywhere.</li>
        <li><b>No third-party servers.</b> Your mail travels only between Microsoft and this PC — nothing goes to us or any other third party.</li>
        <li><b>Your own mailbox only.</b> Signing in as yourself archives only your mailbox, with no tenant-wide permission.</li>
        <li><b>Safe to open offline.</b> Every saved message page runs no scripts and loads nothing from the network, so opening old mail can never execute anything.</li>
        <li><b>Reversible.</b> You can revoke access any time, and uninstalling keeps your archive — your saved mail is never deleted.</li>
      </ul>
    </div>
  </div></main>
</div>
<script src="/dashboard.js"></script>
</body></html>`))

// dashboardJS wires the action buttons (clear sign-in, settings, archive-now with
// device-code + activity polling). Served at /dashboard.js so it runs under
// script-src 'self'.
const dashboardJS = `const csrf=document.querySelector('meta[name=csrf]').content;
const $=id=>document.getElementById(id);
const post=path=>fetch(path,{method:'POST',headers:{'X-CSRF-Token':csrf}});

const clr=$('clearSignin');
if(clr)clr.addEventListener('click',async()=>{
  const n=$('note');
  try{const d=await(await post('/api/clear-signin')).json();
    if(n){n.hidden=false;n.textContent=d.ok?'Signed out. The next capture will sign in again.':(d.error||'Could not clear the sign-in.');}
    if(d.ok)setTimeout(()=>location.reload(),900);
  }catch(e){if(n){n.hidden=false;n.textContent='Error: '+e;}}
});

const save=$('saveSettings');
if(save)save.addEventListener('click',async()=>{
  const n=$('settingsNote');
  const body={out:$('outDir').value,keepRaw:$('keepRaw').checked,includeDeleted:$('incDeleted').checked,includeJunk:$('incJunk').checked};
  try{const d=await(await fetch('/api/settings',{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':csrf},body:JSON.stringify(body)})).json();
    if(n){n.hidden=false;n.textContent=d.ok?'Settings saved.':(d.error||'Could not save.');}
  }catch(e){if(n){n.hidden=false;n.textContent='Error: '+e;}}
});

let polling=false;
const btn=$('archiveBtn');
if(btn)btn.addEventListener('click',async()=>{
  btn.disabled=true;const r=$('captureResult');if(r)r.hidden=true;
  try{const d=await(await post('/api/capture')).json();
    if(!d.ok){btn.disabled=false;if(r){r.hidden=false;r.textContent=d.error||'Could not start.';}return;}
    start();
  }catch(e){btn.disabled=false;if(r){r.hidden=false;r.textContent='Error: '+e;}}
});
function start(){if(!polling){polling=true;poll();}}
async function poll(){
  try{const d=await(await fetch('/api/activity')).json();render(d);
    if(d.running){setTimeout(poll,1500);return;}
  }catch(e){}
  polling=false;if(btn)btn.disabled=false;
}
function render(d){
  const dev=$('device'),act=$('activity'),res=$('captureResult');
  if(d.device&&d.device.code){dev.hidden=false;$('deviceCode').textContent=d.device.code;$('deviceUrl').href=d.device.url;}
  else dev.hidden=true;
  if(d.log&&d.log.length){act.hidden=false;act.textContent=d.log.join('\n');act.scrollTop=act.scrollHeight;}
  if(!d.running&&res){res.hidden=false;res.textContent=d.error?('Capture failed: '+d.error):(d.result||'Done.');}
}
fetch('/api/activity').then(r=>r.json()).then(d=>{if(d.running)start();}).catch(()=>{});
`
