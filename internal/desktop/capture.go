package desktop

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/graph"
	"mail-archive-tool/internal/server"
)

// HeadlessCapture runs one incremental capture with NO dashboard and NO prompt —
// the command a SCHEDULED weekly backup runs (design DC1: a scheduled capture must
// succeed whether or not the dashboard app is open). It reads the saved config +
// settings + sign-in token (Credential Manager on Windows / the file cache else)
// and refuses if there is no saved sign-in (Unattended=true never prompts). Logs to
// logw; returns the run error.
func HeadlessCapture(cfg Config, logw io.Writer) error {
	c := cfg.load()
	if c == nil || strings.TrimSpace(c.Tenant) == "" || strings.TrimSpace(c.ClientID) == "" {
		return errors.New("not configured — run `mailarchive setup` or open MailArchive Desktop and sign in first")
	}
	out := cfg.effectiveOut()
	if out == "" {
		return errors.New("no archive location set — choose one in MailArchive Desktop first")
	}
	s := cfg.settings()
	tcfg := cfg.tokenCfg(c)
	g := app.GraphOptions{
		Auth: "device", Tenant: c.Tenant, ClientID: c.ClientID,
		TokenStore: tcfg.TokenStore, TokenCachePath: tcfg.TokenCachePath,
		BaseURL: cfg.BaseURL, TokenURL: cfg.TokenURL, DeviceAuthURL: cfg.DeviceAuthURL,
		Unattended:     true, // a scheduled run never prompts; it uses the saved sign-in
		IncludeDeleted: s.IncludeDeleted, IncludeJunk: s.IncludeJunk,
	}
	opts := app.Options{Out: out, Mode: export.Incremental, Index: true, Pages: true, KeepRaw: s.KeepRaw}
	_, err := app.RunGraph(context.Background(), g, opts, log.New(logw, "", log.LstdFlags))
	return err
}

// logRingMax bounds the in-memory activity log so a long run never grows without
// limit (design DC3).
const logRingMax = 400

// captureState is the live state of an in-process "Archive now": whether one is
// running, the device code to show while signing in, a bounded tail of the run
// log, and the last result. It is an io.Writer so app.RunGraph's logger streams
// straight into the ring.
type captureState struct {
	mu      sync.Mutex
	running bool
	device  *graph.DeviceAuth
	lines   []string
	result  string
	errMsg  string
}

func (c *captureState) Write(p []byte) (int, error) {
	for _, ln := range strings.Split(strings.TrimRight(string(p), "\n"), "\n") {
		c.appendLine(ln)
	}
	return len(p), nil
}

func (c *captureState) appendLine(s string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lines = append(c.lines, s)
	if len(c.lines) > logRingMax { // DC3: drop the oldest, keep the tail
		c.lines = c.lines[len(c.lines)-logRingMax:]
	}
}

func (c *captureState) setDevice(d graph.DeviceAuth) {
	c.mu.Lock()
	c.device = &d
	c.mu.Unlock()
}

func (c *captureState) finish(result, errMsg string) {
	c.mu.Lock()
	c.running, c.device, c.result, c.errMsg = false, nil, result, errMsg
	c.mu.Unlock()
}

// capture starts an in-process "Archive now": a loopback-only, CSRF-guarded POST.
func (d *dashboard) capture(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if !server.GuardLocalPOST(r, d.csrf) {
		http.Error(w, "refused: same-origin, tokened, local requests only", http.StatusForbidden)
		return
	}
	if err := d.startCapture(); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// activity reports the live capture state for the UI to poll (GET; no side effect).
func (d *dashboard) activity(w http.ResponseWriter, r *http.Request) {
	c := d.cap
	c.mu.Lock()
	out := map[string]any{"running": c.running, "log": append([]string(nil), c.lines...), "result": c.result, "error": c.errMsg}
	if c.device != nil {
		out["device"] = map[string]any{"code": c.device.UserCode, "url": c.device.VerificationURI}
	}
	c.mu.Unlock()
	writeJSON(w, out)
}

func (d *dashboard) startCapture() error {
	cfg := d.cfg.load()
	if cfg == nil || strings.TrimSpace(cfg.Tenant) == "" || strings.TrimSpace(cfg.ClientID) == "" {
		return fmt.Errorf("not configured — open Microsoft 365 setup to add your tenant and application ID first")
	}
	out, ok := d.requireOut()
	if !ok {
		return fmt.Errorf("choose an archive location first")
	}
	s := d.cfg.settings()
	tcfg := d.cfg.tokenCfg(cfg)
	return d.runJob(func(logger *log.Logger) (string, error) {
		g := app.GraphOptions{
			Auth: "device", Tenant: cfg.Tenant, ClientID: cfg.ClientID,
			TokenStore: tcfg.TokenStore, TokenCachePath: tcfg.TokenCachePath,
			BaseURL: d.cfg.BaseURL, TokenURL: d.cfg.TokenURL, DeviceAuthURL: d.cfg.DeviceAuthURL,
			IncludeDeleted: s.IncludeDeleted, IncludeJunk: s.IncludeJunk,
			Prompt: func(da graph.DeviceAuth) { d.cap.setDevice(da) },
		}
		opts := app.Options{Out: out, Mode: export.Incremental, Index: true, Pages: true, KeepRaw: s.KeepRaw}
		res, err := app.RunGraph(context.Background(), g, opts, logger)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Archived — %d message(s) exported, %d indexed.", res.Stats.Exported, res.Indexed), nil
	})
}
