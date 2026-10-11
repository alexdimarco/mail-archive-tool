// Package desktop is the in-process dashboard-and-launcher behind the
// `mailarchive-desktop` command: a loopback-only local web control panel that
// drives the engine (capture, sign-in, reader, scheduling) by calling our
// internal packages directly — never by shelling out to a child binary
// (design-mailarchive-desktop). Slice 1 is the informational shell: status cards,
// the Microsoft 365 connection, and the Security & privacy panel.
package desktop

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/graph"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/instance"
	"mail-archive-tool/internal/localweb"
	"mail-archive-tool/internal/lockfile"
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

	// Exe is the absolute path to this binary, recorded in the scheduled task so a
	// weekly backup runs the right program (default os.Executable()); set by tests.
	Exe string

	// Account names the OS account this process runs as: the banner, and the
	// device-sign-in box ("this sign-in will be saved for <account>"), so a
	// dashboard of another account that was opened by mistake is recognisable
	// (design-local-confidentiality P15, §8.4 login-CSRF). Default: the current user.
	Account string
	// Sessions reports how many browser links were redeemed this run and when the
	// last was; the header shows it at rest, kept apart from the capture log's
	// bounded ring [CC24]. Nil shows none opened.
	Sessions func() (int, time.Time)
	// Former is what this start found at MailArchive's former address 127.0.0.1:8097
	// (instance.FormerProgram or instance.FormerPreUpgrade): the header says so, so
	// a typed address or a bookmark is never trusted silently [CC22, CC23].
	Former string
	// NewLink makes a fresh one-time link to the dashboard, for "Use another
	// browser…"; nil when this process cannot (the endpoint then says so).
	NewLink func() string
	// GOOS overrides runtime.GOOS in the OS-aware copy (tests).
	GOOS string

	// Lifecycle is the process's own life (extraction design §4.3, XC6): its context is
	// what the workspace's Graph session and every preview and run are built on, and its
	// Drain stops and finalizes the running export. Nil: the background context, no drain.
	Lifecycle *Lifecycle
	// StateDir is the per-user, local, non-roaming state directory the run records, the
	// journal and the fingerprint ledger live in (design §4.16, §4.19); see StateDir().
	// Empty: the workspace can preview but cannot start an export.
	StateDir string
	// Host names this computer in the journal and the run records. Default os.Hostname().
	Host string
	// Version is the tool's version, written into each export's manifest.
	Version string
	// WorkspaceOff is the deployment switch (ruling Q6, MA-526): an administrator turns the
	// Extraction workspace off, so its pages and endpoints are never registered (404) and the
	// sidebar's Export group is hidden. Exports already on disk are unaffected.
	WorkspaceOff bool
	// BackupInstalled reports whether the weekly backup is installed (tests); nil asks the
	// OS scheduler about the configured archive.
	BackupInstalled func() bool
	// RemoveBackup removes the weekly backup (tests); nil asks the OS scheduler, as the
	// Weekly backup card's Remove does. Settings' "Save and remove the weekly backup" calls it.
	RemoveBackup func() error
	// BackupInstalledFor reports whether a weekly backup task named for the archive location out is
	// installed, and RemoveBackupFor removes it (tests, the OS scheduler's stand-in); nil asks the OS
	// scheduler. The task is named for the location it was installed for (schedule.DefaultNameFor),
	// which Settings can change afterwards (E4-AI6). BackupInstalled and RemoveBackup, when set, answer
	// for the configured archive alone and are used before these.
	BackupInstalledFor func(out string) bool
	RemoveBackupFor    func(out string) error
	// InstallBackup installs the weekly backup task (tests, the OS scheduler's stand-in); nil hands the
	// Spec to the OS scheduler (schedule.Install).
	InstallBackup func(spec schedule.Spec) error

	// Test overrides of the Graph client's bounds (zero = its defaults, fixed by design).
	RequestTimeout    time.Duration
	MIMETimeout       time.Duration
	MIMEIdleTimeout   time.Duration
	MIMEHeaderTimeout time.Duration
}

func (cfg Config) goos() string {
	if cfg.GOOS != "" {
		return cfg.GOOS
	}
	return runtime.GOOS
}

// account is the OS account the page names.
func (cfg Config) account() string {
	if cfg.Account != "" {
		return cfg.Account
	}
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return "this account"
}

// sessionsLine is the header's count of browser sessions opened this run, read
// from the session itself, never from the capture log's ring [CC24].
func (cfg Config) sessionsLine() string {
	n, last := 0, time.Time{}
	if cfg.Sessions != nil {
		n, last = cfg.Sessions()
	}
	if n == 0 {
		return "browser sessions opened this run: 0"
	}
	return fmt.Sprintf("browser sessions opened this run: %d (last at %s)", n, last.Local().Format("15:04"))
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

// dashboard holds the per-process CSRF token and the live capture state, and the
// extraction workspace's: its session, its export slot and its sign-in slot.
type dashboard struct {
	cfg  Config
	csrf string
	cap  *captureState

	boot  string // this process's epoch: a page that carries another one was made before a restart (R-y)
	life  *Lifecycle
	ws    *workspace
	ex    *exportsState
	si    *signInSlot
	flow  deviceFlow
	lapse lapse

	planMax int // a test lowers the 200,000-message limit of one export (R-i)

	// listGate keeps the workspace's list requests out of Rebuild's rename window: a list
	// holds it shared while search.db is open, the rebuild job takes it exclusively for an
	// instant to wait for them and then sets rebuilding, which makes every later list answer
	// "the index is being rebuilt" until the job ends (XC20).
	listGate   sync.RWMutex
	rebuilding atomic.Bool
	// rebuild is the Rebuild job's work; a test replaces it to stand inside the rebuild (nil: app.Rebuild).
	rebuild func(out string, l *log.Logger) (app.RebuildReport, error)

	// seams a test replaces
	now    func() time.Time
	pick   func(ctx context.Context, title string) (string, error)
	reveal func(name string, args []string) error
}

// newDashboard builds the dashboard's state. With a StateDir it also runs the launch
// sweep of the run records, then reconciles each unresolved run once in the background
// (design §4.16, XC4), before any export can start.
func newDashboard(cfg Config) *dashboard {
	d := &dashboard{cfg: cfg, csrf: server.NewCSRFToken(), cap: &captureState{}, boot: randomID(8), life: cfg.Lifecycle,
		si: &signInSlot{}, now: time.Now, pick: pickFolder, reveal: defaultReveal}
	d.ws = newWorkspace(d)
	d.ex = newExportsState(d)
	d.ex.env.SetCaptureBusy(d.captureRunning)
	if cfg.StateDir != "" && !cfg.WorkspaceOff {
		_ = d.ex.rec.Launch()
	}
	d.life.OnDrain(d.drain)
	return d
}

// captureRunning is whether a capture (or a verify or rebuild) runs in this process, or the
// archive lock is held by another run: the copy export backs off meanwhile, uncounted, until
// the throttle fix lands (design §4.14, XC30). It writes nothing: lockfile.Held probes by
// taking the lock for an instant, and Acquire retries briefly so a capture is never refused
// by that probe (EW-AC9).
func (d *dashboard) captureRunning() bool {
	d.cap.mu.Lock()
	running := d.cap.running
	d.cap.mu.Unlock()
	if running {
		return true
	}
	if out := d.cfg.effectiveOut(); out != "" {
		return lockfile.Held(filepath.Join(out, lockfile.Name))
	}
	return false
}

// DashboardHandler serves the dashboard: GET "/" (Overview), the static script,
// and the loopback-only, CSRF/Origin/Host-guarded action endpoints. Its routes are
// registered on a localweb.Mux so the binary's guard and route probe name them
// (design-local-confidentiality CC15).
func DashboardHandler(cfg Config) http.Handler { return newDashboard(cfg).router() }

// router registers the dashboard's routes: the Overview's, and, unless the deployment
// switch is off, the Extraction workspace's pages and endpoints.
func (d *dashboard) router() localweb.Router {
	mux := localweb.NewMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(d.cfg.overviewFor(d.csrf, d.boot)))
	})
	mux.HandleFunc("/dashboard.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Write([]byte(dashboardJS))
	})
	mux.HandleFunc("/api/clear-signin", d.clearSignin)
	mux.HandleFunc("/api/capture", d.capture)
	mux.HandleFunc("/api/activity", d.activity)
	mux.HandleFunc("/api/settings", d.saveSettings)
	mux.HandleFunc("/api/schedule-install", d.scheduleInstall)
	mux.HandleFunc("/api/schedule-remove", d.scheduleRemove)
	mux.HandleFunc("/api/schedule-state", d.scheduleState)
	mux.HandleFunc("/api/verify", d.verifyAction)
	mux.HandleFunc("/api/rebuild", d.rebuildAction)
	mux.HandleFunc("/api/health", d.healthAction)
	mux.HandleFunc("/api/browser-link", d.browserLink)
	if !d.cfg.WorkspaceOff {
		d.registerWorkspace(mux)
	}
	return localweb.Routed(dashboardHeaders(mux), mux)
}

// registerWorkspace registers the Extraction workspace's two page shells, its scripts and
// its nineteen POST endpoints on the dashboard's route registry (XC23): every one is seen
// by the guard and the route probe, and every URL they use is relative.
func (d *dashboard) registerWorkspace(mux *localweb.Mux) {
	mux.HandleFunc("/workspace", d.workspacePage)
	mux.HandleFunc("/exports", d.exportsPage)
	mux.HandleFunc("/workspace.js", serveJS(workspaceJS))
	mux.HandleFunc("/exports.js", serveJS(exportsJS))
	mux.HandleFunc("/signin.js", serveJS(signinJS))
	for _, r := range []struct {
		path string
		h    func(http.ResponseWriter, *http.Request)
	}{
		{"/api/m365/signin", d.signInAction},
		{"/api/m365/signin-status", d.signInStatusAction},
		{"/api/workspace/source", d.sourceAction},
		{"/api/workspace/folders", d.foldersAction},
		{"/api/workspace/messages", d.messagesAction},
		{"/api/workspace/count", d.countAction},
		{"/api/workspace/browse", d.browseAction},
		{"/api/workspace/mkdir", d.mkdirAction},
		{"/api/workspace/preview", d.previewAction},
		{"/api/workspace/preview-status", d.previewStatusAction},
		{"/api/exports/start", d.startAction},
		{"/api/exports/cancel", d.cancelAction},
		{"/api/exports/progress", d.progressAction},
		{"/api/exports/history", d.historyAction},
		{"/api/exports/open-folder", d.openFolderAction},
		{"/api/exports/retry", d.retryAction},
		{"/api/exports/finish", d.finishAction},
		{"/api/exports/locate", d.locateAction},
		{"/api/exports/check", d.checkAction},
	} {
		mux.HandleFunc(r.path, d.api(r.h))
	}
}

// browserLink mints a fresh one-time link for a second browser (a guarded POST:
// inside the session, with the CSRF token, from a loopback Origin and Host). The
// link is the way into a browser the shortcut does not open; it works once and
// lives ten minutes (design §4.C3).
func (d *dashboard) browserLink(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if !server.GuardLocalPOST(r, d.csrf) {
		http.Error(w, "refused: same-origin, tokened, local requests only", http.StatusForbidden)
		return
	}
	if d.cfg.NewLink == nil {
		writeJSON(w, map[string]any{"ok": false, "error": "This MailArchive cannot make another link: close it and open it from its shortcut."})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "link": d.cfg.NewLink()})
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
		// Backup answers the question a save that retargets the weekly backup asks first
		// (XC22): "keep" or "remove". Empty is the first ask.
		Backup string
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&in); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	out := strings.TrimSpace(in.Out)
	// The archive is copied, synced and browsed; MailArchive's own settings and
	// sign-in must not travel with it (design-local-confidentiality P10).
	if out != "" {
		if err := d.cfg.settingsOverlap(out); err != nil {
			writeJSONStatus(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
			return
		}
	}
	// A weekly backup reads the archive location at run time (MA-279), so a save that changes it
	// would silently make the next scheduled capture write the signed-in mailbox into that folder:
	// the harmful save is an informed one, at the moment it happens (XC22, MA-523). Nothing is
	// written, and nothing removed, until the user has answered.
	// The location after this save: an empty field means the launch location, so the question is about
	// that, and a save that leaves the location as it is asks nothing.
	after := out
	if after == "" {
		after = strings.TrimSpace(d.cfg.Out)
	}
	backupFor := "" // the location the weekly task is named for, when it is not the archive location
	removedBackup := false
	if !samePath(after, d.cfg.effectiveOut()) && d.backupInstalled() {
		switch in.Backup {
		case "keep":
			// the task keeps its name, and archives into the new location: the guard, the card and
			// Remove look for it under the name it has (E4-AI6)
			if p, _ := d.cfg.backupTask(); p != "" && !samePath(p, after) {
				backupFor = p
			}
		case "remove":
			if err := d.removeBackup(); err != nil {
				writeJSON(w, map[string]any{"ok": false, "error": "The weekly backup could not be removed (the scheduler said: " + err.Error() + "), so nothing was saved and your settings are as they were. Remove it yourself with Remove on the Weekly backup card, or choose Save and keep the backup."})
				return
			}
			removedBackup = true
		default:
			who, _ := d.signedInNow()
			signedIn := who != ""
			ask := fmt.Sprintf("Your weekly backup will now archive %s into %s.", who, after)
			switch {
			case !signedIn && after == "":
				ask = "You cleared the Archive location, so your weekly backup (it archives the Microsoft 365 account it uses; nobody is signed in right now) would have no folder to archive into."
			case !signedIn:
				ask = fmt.Sprintf("Your weekly backup will now archive the Microsoft 365 account it uses (nobody is signed in right now) into %s.", after)
			case after == "":
				ask = fmt.Sprintf("You cleared the Archive location, so your weekly backup (it archives %s) would have no folder to archive into.", who)
			}
			if out == "" && after != "" {
				ask = strings.TrimSuffix(ask, ".") + " (the Archive location is empty, so it is the folder MailArchive was started with)."
			}
			ask += " If you only want to read an archive someone gave you, choose Cancel: Extraction workspace → Source → Another archive folder reads it without changing any setting or your weekly backup."
			writeJSON(w, map[string]any{"ok": false, "confirm": true, "upn": who, "folder": after, "error": ask,
				"choices": []string{"keep", "remove", "cancel"}})
			return
		}
	} else if p, _ := d.cfg.backupTask(); p != "" && !samePath(p, after) {
		backupFor = p // the location is not changing, and the task is still named for another one
	}
	// Reloaded under the settings lock: only the fields this save owns change, so a
	// racing export start's own fields are never written back stale (XC34).
	if err := UpdateSettings(d.cfg.SettingsPath, "settings", func(s *Settings) {
		s.Out, s.KeepRaw, s.IncludeDeleted, s.IncludeJunk = out, in.KeepRaw, in.IncludeDeleted, in.IncludeJunk
		s.BackupFor = backupFor
	}); err != nil {
		msg := err.Error()
		if removedBackup { // "Save and remove" removes first: say so, or Cancel is what the owner thinks happened
			msg = "The weekly backup was removed, but the settings were not saved, so the archive location is as it was: " + msg + " Click Save settings again when the file can be read."
		}
		writeJSON(w, map[string]any{"ok": false, "error": msg})
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
	d.signInChanged("") // the workspace's session, previews and a running export go with it (XC7)
	writeJSON(w, map[string]any{"ok": true})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}

// writeJSONStatus is writeJSON with a status other than 200; the headers are
// set before the status is written, or they would not be sent.
func writeJSONStatus(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
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
	if eo := cfg.effectiveOut(); eo != "" {
		_, st := cfg.backupTask()
		switch st {
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

func orElse(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
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

func (cfg Config) overview(csrf string) string { return cfg.overviewFor(csrf, "") }

// overviewFor is the Overview page for a process whose epoch is epoch (the workspace's
// scripts send it back, so a page made before a restart is told so, R-y).
func (cfg Config) overviewFor(csrf, epoch string) string {
	c := cfg.load()
	upn, signedIn := cfg.signInState()
	s := cfg.settings()
	data := struct {
		Export         bool // the Extraction workspace is on (not turned off by the deployment switch, MA-526)
		Epoch          string
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
		Interval       string
		WeeklyTime     string
		Account        string
		SessionsLine   string
		FormerNotice   string
		ResidualOS     string // "macOS" or "Linux": where another account can read a just-opened link's argv
	}{Export: !cfg.WorkspaceOff, Epoch: epoch, CSRF: csrf, Cards: cfg.cards(), Vault: vaultPhrase(), OutDir: cfg.effectiveOut(), ReaderURL: cfg.ReaderURL, SignedIn: signedIn, UPN: upn,
		KeepRaw: s.KeepRaw, IncludeDeleted: s.IncludeDeleted, IncludeJunk: s.IncludeJunk,
		Interval: orElse(s.Interval, "weekly"), WeeklyTime: orElse(s.WeeklyTime, "03:00"),
		Account: cfg.account(), SessionsLine: cfg.sessionsLine(), FormerNotice: instance.NoticeFor(cfg.Former), ResidualOS: residualOS(cfg.goos())}
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

// residualOS names the systems where another account can read the command line
// of a program a browser was just opened with (P7, §8.4): Linux through
// /proc/*/cmdline and macOS through the setuid ps. Windows has neither.
func residualOS(goos string) string {
	switch goos {
	case "darwin":
		return "macOS"
	case "linux":
		return "Linux"
	}
	return ""
}

// Loopback re-exports server.IsLoopback so main can gate the bind without importing
// the server package directly for one predicate.
func Loopback(addr string) bool { return server.IsLoopback(addr) }

var overviewTmpl = template.Must(template.New("overview").Parse(`<!DOCTYPE html>
<html lang="en"><head>
<meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="csrf" content="{{.CSRF}}">{{if .Export}}<meta name="epoch" content="{{.Epoch}}">{{end}}
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
.nav .nav-disabled{display:block;color:var(--sidebar-muted);padding:9px 11px;font-weight:650;cursor:not-allowed}
.sidebar-foot{margin-top:auto;color:var(--sidebar-muted);font-size:.78rem}
.whoami{display:flex;flex-wrap:wrap;gap:4px 18px;color:var(--muted);font-size:.85rem;margin:0 0 14px}
.warnbox{border-color:var(--warn);color:var(--text)}
.content{padding:30px 34px 52px;min-width:0}.inner{max-width:1040px;margin:0 auto}
h1{font-size:1.8rem;letter-spacing:-.02em;margin:0 0 4px}.lede{color:var(--muted);margin:0 0 22px}
.status-grid{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:12px;margin-bottom:20px}
.status-card{background:var(--panel);border:1px solid var(--line);border-radius:12px;padding:15px}
.status-card .label{color:var(--muted);font-size:.8rem}.status-card .value{font-size:1.08rem;font-weight:800;margin-top:6px;overflow-wrap:anywhere}
.good{color:var(--good)}.warn{color:var(--warn)}
.card{background:var(--panel);border:1px solid var(--line);border-radius:14px;padding:19px;margin-bottom:16px}
.card h2{font-size:1.05rem;margin:0 0 12px}.card[id],.anchor{scroll-margin-top:16px}
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
.row2{display:grid;grid-template-columns:1fr 1fr;gap:12px}
select{width:100%;padding:10px 11px;border:1px solid var(--line);border-radius:8px;background:var(--panel);color:var(--text)}
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
      <a class="active" href="./">Overview</a>
      <a href="#sec-archive" id="navArchive">Archive now</a>
      {{if .ReaderURL}}<a href="{{.ReaderURL}}" target="_blank" rel="noopener">Open archive</a>
      <a href="{{.ReaderURL}}goback" target="_blank" rel="noopener">Go back in time</a>{{else}}<span class="nav-disabled" title="The archive reader is not running">Open archive</span>
      <span class="nav-disabled" title="The archive reader is not running">Go back in time</span>{{end}}
      {{if .Export}}<div class="nav-title">Export</div>
      <a href="#sec-export">Extraction workspace</a>
      <a href="#sec-export-activity">Export activity</a>{{end}}
      <div class="nav-title">Diagnostics</div>
      <a href="#sec-health">Status &amp; health</a>
      <a href="#sec-activity">Activity log</a>
      <div class="nav-title">Setup</div>
      <a href="#sec-m365">Microsoft 365 connection</a>
      <a href="#sec-backup">Weekly backup</a>
    </nav>
    <div class="sidebar-foot">Local control panel<br>127.0.0.1</div>
  </aside>
  <main class="content"><div class="inner">
    <div class="whoami" id="whoami" role="status">
      <span>MailArchive Desktop is running as <b>{{.Account}}</b> on this computer.</span>
      <span id="sessionsLine">{{.SessionsLine}}</span>
    </div>
    {{if .FormerNotice}}<div class="note warnbox" id="formerNotice" role="alert">{{.FormerNotice}}</div>{{end}}
    <h1>Overview</h1>
    <p class="lede">Your personal Microsoft 365 archive, running locally on this PC.</p>

    <div class="status-grid">
      {{range .Cards}}<div class="status-card"><div class="label">{{.Label}}</div><div class="value {{.Class}}">{{.Value}}</div></div>{{end}}
    </div>

    <div class="card" id="sec-m365">
      <h2>Microsoft 365 connection</h2>
      {{if .Configured}}
      <div class="row"><div class="k">Tenant</div><div class="v mono">{{.Tenant}}</div></div>
      <div class="row"><div class="k">Application (client) ID</div><div class="v mono">{{.ClientID}}</div></div>
      <div class="row"><div class="k">Sign-in mode</div><div class="v">{{.Auth}}</div></div>
      <div class="row"><div class="k">Sign-in</div><div class="v">{{if .SignedIn}}<span class="good">Signed in as {{.UPN}}</span> &nbsp; <button class="btn-link" id="clearSignin">Clear sign-in</button>{{else}}<span class="warn">Sign-in needed</span> — the first capture signs you in with a Microsoft device code.{{if .Export}} <button class="btn-link" id="signInBtn" type="button">Sign in</button>{{end}}{{end}}</div></div>
      <div class="row"><div class="k">Scope</div><div class="v">Microsoft 365: your own mailbox only{{if .UPN}} ({{.UPN}}){{end}}, read-only (Mail.Read). Shared mailboxes and other people's live mailboxes aren't available in this version. They would need a different Microsoft permission (Mail.Read.Shared) that this app does not request.</div></div>
      {{if and .Export (not .SignedIn)}}<div class="device-box" id="signInBox" hidden>
        <div id="signInBanner" class="whoami"></div>
        <div id="signInDevice" hidden>To sign in, open <a id="signInUrl" target="_blank" rel="noopener">this Microsoft page</a> and enter the code:
          <div class="device-code" id="signInCode"></div></div>
        <div id="signInConfirm" hidden><div id="signInConfirmText"></div>
          <div style="margin-top:8px"><button class="btn" id="signInKeep" type="button">Keep</button> <button class="btn secondary" id="signInOut" type="button">Sign out</button></div></div>
        <div class="card-sub" style="margin:10px 0 0" id="signInNotice"></div>
      </div><div class="note" id="banner" hidden></div>{{end}}
      {{else}}
      <div class="row"><div class="k">Status</div><div class="v warn">Not configured — run <span class="mono">mailarchive setup</span> to add your tenant and application ID.</div></div>
      {{end}}
      {{if .OutDir}}<div class="row"><div class="k">Archive location</div><div class="v mono">{{.OutDir}}</div></div>{{end}}
      <div class="note" id="note" hidden></div>
    </div>

    {{if .Export}}<div class="card" id="sec-export">
      <h2>Export</h2>
      <p class="card-sub">Copy selected folders and messages from your Microsoft 365 mailbox to a folder, with a manifest and checksums the recipient can check. Each export is a new folder; nothing is ever changed in your mailbox or your archive.</p>
      <a class="btn" href="workspace">Open the Extraction workspace</a>
      <a class="btn secondary" href="exports" id="sec-export-activity">Export activity</a>
    </div>{{end}}

    <div class="card" id="sec-archive">
      <h2>Archive now</h2>
      <p class="card-sub">Fetch the latest mail into your archive. The first time, you sign in with a Microsoft device code shown here.</p>
      <button class="btn" id="archiveBtn">Archive now</button>
      {{if .ReaderURL}}<a class="btn secondary" href="{{.ReaderURL}}" target="_blank" rel="noopener">Open archive</a> <a class="btn secondary" href="{{.ReaderURL}}goback" target="_blank" rel="noopener">Go back in time</a>{{end}}
      <button class="btn secondary" id="anotherBrowser" type="button">Use another browser…</button>
      <div class="note" id="linkNote" hidden></div>
      <div class="device-box" id="device" hidden>
        <div>To sign in, open <a id="deviceUrl" target="_blank" rel="noopener">this Microsoft page</a> and enter the code:</div>
        <div class="device-code" id="deviceCode"></div>
        <div class="card-sub" style="margin:10px 0 0">This sign-in will be saved for <b>{{.Account}}</b> on this computer. MailArchive shows a Microsoft sign-in code only in a window it opened from its shortcut.</div>
      </div>
      <div id="sec-activity" class="anchor"></div>
      <pre id="activity" hidden></pre>
      <div class="note" id="captureResult" hidden></div>
    </div>

    <div class="card" id="sec-settings">
      <h2>Settings</h2>
      <label for="outDir">Archive location</label>
      <input id="outDir" value="{{.OutDir}}" placeholder="e.g. C:\MailArchive, a mapped drive, or a UNC path" spellcheck="false">
      <label class="checkline"><input type="checkbox" id="keepRaw"{{if .KeepRaw}} checked{{end}}> Keep each message's original .eml too</label>
      <label class="checkline"><input type="checkbox" id="incDeleted"{{if .IncludeDeleted}} checked{{end}}> Include Deleted Items</label>
      <label class="checkline"><input type="checkbox" id="incJunk"{{if .IncludeJunk}} checked{{end}}> Include Junk Email</label>
      <div style="margin-top:14px"><button class="btn secondary" id="saveSettings">Save settings</button></div>
      <div class="note" id="settingsNote" hidden></div>
      <div class="note warnbox" id="settingsConfirm" role="alert" hidden>
        <div id="settingsConfirmText"></div>
        <div class="actions" style="margin-top:10px"><button class="btn" id="settingsCancel" type="button">Cancel</button>
          <button class="btn secondary" id="settingsKeep" type="button">Save and keep the backup</button>
          <button class="btn secondary" id="settingsRemoveBackup" type="button">Save and remove the weekly backup</button></div>
      </div>
    </div>

    <div class="card" id="sec-backup">
      <h2>Weekly backup</h2>
      <p class="card-sub">Run a capture automatically on a schedule. It runs even when this app is closed (as long as you stay signed in to Windows), using your saved Microsoft sign-in.</p>
      <div class="row2">
        <div><label for="schedInterval">How often</label>
          <select id="schedInterval"><option value="weekly"{{if eq .Interval "weekly"}} selected{{end}}>Weekly (Sunday)</option><option value="daily"{{if eq .Interval "daily"}} selected{{end}}>Daily</option></select></div>
        <div><label for="schedTime">At (local time)</label><input id="schedTime" type="time" value="{{.WeeklyTime}}"></div>
      </div>
      <div style="margin-top:14px"><button class="btn" id="schedInstall">Install / update</button> <button class="btn secondary" id="schedRemove">Remove</button></div>
      <div class="note" id="schedState" hidden></div>
    </div>

    <div class="card" id="sec-health">
      <h2>Status &amp; health</h2>
      <div id="healthPosture" class="small"></div>
      <pre id="healthBody" class="small" style="min-height:0;max-height:220px">Loading…</pre>
    </div>

    <div class="card">
      <h2>Maintenance</h2>
      <p class="card-sub">Occasional housekeeping. These share the archive with captures, so only one runs at a time — watch the Archive-now activity for progress.</p>
      <button class="btn secondary" id="verifyBtn">Check integrity</button>
      <button class="btn secondary" id="rebuildBtn">Rebuild search index</button>
    </div>

    <div class="card sec">
      <h2>Security &amp; privacy</h2>
      <ul>
        <li><b>Runs entirely on this PC.</b> The control panel and your archive are local; nothing here is served to the network.</li>
        <li><b>Only you can open it.</b> This page and your archive open only through a private link MailArchive makes each time it starts, so other accounts on this computer and web pages cannot open the dashboard or the archive. A program running as you can: it already has your files.</li>
        <li><b>Sign-in codes.</b> MailArchive shows a Microsoft sign-in code only in a window it opened from its shortcut — do not trust a code on a page you reached by typing or bookmarking an address.</li>
        {{if .ResidualOS}}<li><b>One residual on {{.ResidualOS}}.</b> Another account on this computer that can list running programs can read a link's address in the instant a browser is opened with it, and race to use it first (a link works once, and the opener's lives about a minute). If a page says a link was "already used" and you did not use it, quit MailArchive and start it again — that ends every session.</li>{{end}}
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
<script src="dashboard.js"></script>
{{if .Export}}<script src="signin.js"></script>{{end}}
</body></html>`))

// dashboardJS wires the action buttons (clear sign-in, settings, archive-now with
// device-code + activity polling). Served at /dashboard.js so it runs under
// script-src 'self'.
const dashboardJS = `const csrf=document.querySelector('meta[name=csrf]').content;
const $=id=>document.getElementById(id);
const post=path=>fetch(path,{method:'POST',headers:{'X-CSRF-Token':csrf}});

// "Use another browser…": a fresh one-time link to copy into a browser that is not
// the default one the shortcut opens.
const ab=$('anotherBrowser');
if(ab)ab.addEventListener('click',async()=>{
  const n=$('linkNote');
  try{const d=await(await post('api/browser-link')).json();
    if(n){n.hidden=false;n.textContent=d.ok?('Copy this link into the other browser — it works once, for ten minutes: '+d.link):(d.error||'Could not make a link.');}
  }catch(e){if(n){n.hidden=false;n.textContent='Error: '+e;}}
});

const clr=$('clearSignin');
if(clr)clr.addEventListener('click',async()=>{
  const n=$('note');
  try{const d=await(await post('api/clear-signin')).json();
    if(n){n.hidden=false;n.textContent=d.ok?'Signed out. The next capture will sign in again.':(d.error||'Could not clear the sign-in.');}
    if(d.ok)setTimeout(()=>location.reload(),900);
  }catch(e){if(n){n.hidden=false;n.textContent='Error: '+e;}}
});

// Saving a new archive location while the weekly backup is installed asks first (XC22): the
// backup reads the location when it runs, so it would archive the signed-in mailbox into the new
// folder. The answers are the server's own three: keep the backup, remove it, or cancel.
const save=$('saveSettings');
async function doSave(backup){
  const n=$('settingsNote'),c=$('settingsConfirm');
  const body={out:$('outDir').value,keepRaw:$('keepRaw').checked,includeDeleted:$('incDeleted').checked,includeJunk:$('incJunk').checked,backup:backup||''};
  try{const d=await(await fetch('api/settings',{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':csrf},body:JSON.stringify(body)})).json();
    if(d.confirm){
      if(n)n.hidden=true;
      if(c){c.hidden=false;$('settingsConfirmText').textContent=d.error+' Choose what happens to the weekly backup, or Cancel to leave your settings as they are.';}
      return;
    }
    if(c)c.hidden=true;
    if(n){n.hidden=false;n.textContent=d.ok?(backup==='remove'?'Settings saved, and the weekly backup removed. To run it again, click Install / update on the Weekly backup card.':'Settings saved.'):(d.error||'Could not save.');}
  }catch(e){if(n){n.hidden=false;n.textContent='Error: '+e;}}
}
if(save)save.addEventListener('click',()=>doSave(''));
const keepB=$('settingsKeep'),removeB=$('settingsRemoveBackup'),cancelB=$('settingsCancel');
if(keepB)keepB.addEventListener('click',()=>doSave('keep'));
if(removeB)removeB.addEventListener('click',()=>doSave('remove'));
if(cancelB)cancelB.addEventListener('click',()=>{
  const n=$('settingsNote'),c=$('settingsConfirm');
  if(c)c.hidden=true;
  if(n){n.hidden=false;n.textContent='Not saved: your settings are as they were.';}
});

let polling=false;
const btn=$('archiveBtn');
// Sidebar "Archive now" starts a capture as well as jumping to its card (an
// inline onclick would be blocked by script-src 'self').
const navArchive=$('navArchive');
if(navArchive)navArchive.addEventListener('click',()=>{if(btn&&!btn.disabled)btn.click();});
function markNav(){
  const h=location.hash||'';
  document.querySelectorAll('.nav a').forEach(a=>{
    const href=a.getAttribute('href')||'';
    a.classList.toggle('active',h?href===h:href==='./');
  });
}
window.addEventListener('hashchange',markNav);markNav();
if(btn)btn.addEventListener('click',async()=>{
  btn.disabled=true;const r=$('captureResult');if(r)r.hidden=true;
  try{const d=await(await post('api/capture')).json();
    if(!d.ok){btn.disabled=false;if(r){r.hidden=false;r.textContent=d.error||'Could not start.';}return;}
    start();
  }catch(e){btn.disabled=false;if(r){r.hidden=false;r.textContent='Error: '+e;}}
});
function start(){if(!polling){polling=true;poll();}}
async function poll(){
  try{const d=await(await fetch('api/activity')).json();render(d);
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
fetch('api/activity').then(r=>r.json()).then(d=>{if(d.running)start();}).catch(()=>{});

async function schedState(){
  const n=$('schedState');if(!n)return;
  try{const d=await(await fetch('api/schedule-state')).json();
    n.hidden=false;
    if(!d.schedulerAvailable)n.textContent='The OS scheduler is unavailable on this machine.';
    else if(d.state==='installed')n.textContent='Installed'+(d.interval?(' ('+d.interval+(d.at?' at '+d.at:'')+')'):'')+'.';
    else n.textContent='Not installed.';
  }catch(e){}
}
function schedPost(path){
  const n=$('schedState');
  const body={interval:$('schedInterval').value,time:$('schedTime').value};
  return fetch(path,{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':csrf},body:JSON.stringify(body)})
    .then(r=>r.json()).then(d=>{if(n){n.hidden=false;n.textContent=d.ok?'Saved.':(d.error||'Failed.');}
      return schedState().then(()=>{if(n&&d.left){n.hidden=false;n.textContent='Removed. Left in place: '+d.left+' cannot be read, so removal cannot tell which schedule it records - '+d.remedy+'.';}});});
}
const si=$('schedInstall'),sr=$('schedRemove');
if(si)si.addEventListener('click',()=>schedPost('api/schedule-install'));
if(sr)sr.addEventListener('click',()=>schedPost('api/schedule-remove'));
schedState();

async function health(){
  const p=$('healthPosture'),b=$('healthBody');if(!b)return;
  try{const d=await(await fetch('api/health')).json();
    if(p)p.textContent=d.posture?('Posture: '+d.posture):'';
    b.textContent=(d.summary||[]).join('\n')||'No archive yet.';
  }catch(e){b.textContent='Could not read status.';}
}
health();
function job(path){const r=$('captureResult');if(r)r.hidden=true;
  post(path).then(x=>x.json()).then(d=>{if(d.ok)start();else if(r){r.hidden=false;r.textContent=d.error||'Could not start.';}});}
const vb=$('verifyBtn'),rb=$('rebuildBtn');
if(vb)vb.addEventListener('click',()=>job('api/verify'));
if(rb)rb.addEventListener('click',()=>{if(confirm('Rebuild the search index from the archive files? This can take a while for a large archive.'))job('api/rebuild');});
`
