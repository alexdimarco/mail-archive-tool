// Package desktop is the in-process dashboard-and-launcher behind the
// `mailarchive-desktop` command: a loopback-only local web control panel that
// drives the engine (capture, sign-in, reader, scheduling) by calling our
// internal packages directly — never by shelling out to a child binary
// (design-mailarchive-desktop). Slice 1 is the informational shell: status cards,
// the Microsoft 365 connection, and the Security & privacy panel.
package desktop

import (
	"html/template"
	"net/http"
	"runtime"
	"strings"

	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/server"
)

// Config wires the dashboard to one archive and the Graph config. Store is the
// secret store (Credential Manager on Windows / a file elsewhere); it may be nil
// in the informational shell.
type Config struct {
	Out        string
	ConfigPath string
	Store      graphconfig.SecretStore
}

// DashboardHandler serves the dashboard. Slice 1: GET "/" (the Overview page) and
// the restyled static assets. State-changing actions arrive in later slices and
// will be loopback-only + CSRF/Origin/Host-guarded (the `setup` pattern).
func DashboardHandler(cfg Config) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(cfg.overview()))
	})
	return dashboardHeaders(mux)
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
		conn = card{"Microsoft 365", "Configured (" + authLabel(c.Auth) + ")", "good"}
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

func (cfg Config) overview() string {
	c := cfg.load()
	data := struct {
		Cards      []card
		Configured bool
		Tenant     string
		ClientID   string
		Auth       string
		Vault      string
		OutDir     string
	}{Cards: cfg.cards(), Vault: vaultPhrase(), OutDir: cfg.Out}
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
@media(max-width:900px){.app-shell{grid-template-columns:1fr}.status-grid{grid-template-columns:1fr 1fr}.sidebar-foot{display:none}}
</style></head>
<body>
<div class="app-shell">
  <aside class="sidebar">
    <div class="brand">MailArchive<span>Desktop</span></div>
    <nav class="nav">
      <div class="nav-title">Workspace</div>
      <a class="active" href="/">Overview</a>
      <a href="/">Archive now</a>
      <a href="/">Open archive</a>
      <a href="/">Go back in time</a>
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
      {{else}}
      <div class="row"><div class="k">Status</div><div class="v warn">Not configured — run <span class="mono">mailarchive setup</span> to add your tenant and application ID.</div></div>
      {{end}}
      {{if .OutDir}}<div class="row"><div class="k">Archive location</div><div class="v mono">{{.OutDir}}</div></div>{{end}}
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
</body></html>`))
