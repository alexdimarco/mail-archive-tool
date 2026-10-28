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
	"time"

	"mail-archive-tool/internal/alert"
	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/backupalert"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/graph"
	"mail-archive-tool/internal/seal"
	"mail-archive-tool/internal/server"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/wizard"
)

// notConfiguredMsg is the refusal text shown when Microsoft 365 is not set up —
// shared by HeadlessCapture (a scheduled capture's refusal) and the dashboard's
// capture install/repair (§4.6.3, MA-617), so both name the same remedy.
const notConfiguredMsg = "not configured — set up Microsoft 365 on MailArchive Desktop's Microsoft 365 connection card (or run `mailarchive setup`), then sign in"

// m365Ready reports nil when Microsoft 365 is set up (a config with a tenant and
// a client id), else the notConfiguredMsg refusal: the check a capture install or
// repair makes before schedule.Install (§4.6.3), the same one HeadlessCapture makes.
func (cfg Config) m365Ready() error {
	c := cfg.load()
	if c == nil || strings.TrimSpace(c.Tenant) == "" || strings.TrimSpace(c.ClientID) == "" {
		return errors.New(notConfiguredMsg)
	}
	return nil
}

// HeadlessCapture runs one incremental capture with NO dashboard and NO prompt —
// the command a SCHEDULED weekly backup runs (design DC1: a scheduled capture must
// succeed whether or not the dashboard app is open). It reads the saved config +
// settings + sign-in token (Credential Manager on Windows / the file cache else)
// and refuses if there is no saved sign-in (Unattended=true never prompts). Logs to
// logw; returns the run error.
//
// Every failure exit but the paused refusal goes through the alert streak (reportRun): the
// failure email goes once per failing streak, plus one recovery email (design-desktop-
// settings-card 3.7, R3C-10). A start while another run holds the archive's lock, which
// started under a day ago, skips quietly: no failure, alert, streak or history entry.
func HeadlessCapture(cfg Config, logw io.Writer) error {
	out := cfg.effectiveOut()
	key := backupalert.KeyFor(out) // the archive folder the capture backs up into
	// A paused archive is hard read-only: refuse before recordStartFailure or any
	// write (S2-C2). The caller logs this to the capture log (config dir), never into
	// the archive.
	if err := seal.RefuseWrite(out); err != nil {
		return err
	}
	// An encrypted archive is captured into only with its key (phase2a §5); the
	// scheduled capture's refusal is recorded so status and the banner show it (A14).
	if err := app.RefuseKeyless("capture into it", out); err != nil {
		// Recorded like every scheduled refusal: the minimal failed run and one
		// history entry, the failure kind and never its text (A14, P2a-C16).
		app.RecordRefusedRun(app.Options{Out: out, Unattended: true}, err)
		cfg.reportRun(key, out, alert.ResultFailed, err, 0, nil)
		return err
	}
	// §4.6.3/MA-617 + OP-2/MA-646: refuse when Microsoft 365 is not set up, via the
	// shared m365Ready check (the dashboard's capture install/repair uses it too),
	// AND record a FAILED last-run so a scheduled capture that refuses at start does
	// not stay silently GREEN until it is twice its period late (R18).
	if err := cfg.m365Ready(); err != nil {
		recordStartFailure(out, err)
		cfg.reportRun(key, out, alert.ResultFailed, err, 0, nil)
		return err
	}
	c := cfg.load()
	if out == "" {
		err := errors.New("no archive location set — choose one in MailArchive Desktop first")
		cfg.reportRun(key, out, alert.ResultFailed, err, 0, nil)
		return err
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
	logger := log.New(logw, "", log.LstdFlags)
	// Unattended marks this as the SCHEDULED backup: it activates the PM4 out-exists guard
	// (never recreate the archive on the boot disk when the backup drive is gone) and the
	// source-loss streak (§5.7). The interactive dashboard capture leaves it unset.
	opts := app.Options{Out: out, Mode: export.Incremental, Index: true, Pages: true, KeepRaw: s.KeepRaw, Unattended: true}
	// An archive location that holds the desktop's settings and sign-in copies
	// them with the archive (design-local-confidentiality P10). The scheduled
	// capture does not refuse — that would silently stop an installed backup —
	// it warns, records the warning for `status`, and captures [CC16].
	if dir := cfg.settingsInArchive(out); dir != "" {
		msg := fmt.Sprintf("Your MailArchive settings and sign-in are stored inside the archive folder %s. Why: the archive is meant to be copied and synced. Do this: choose an archive location that does not contain %s (Settings).", out, dir)
		logger.Printf("warning: %s", msg)
		opts.Warnings = []state.RunWarning{{Code: "config_in_archive", Message: msg}}
	}
	_, err := app.RunGraph(context.Background(), g, opts, logger)
	// Another run holds the archive (Archive now, a verify, a rebuild) and started under a day
	// ago: skip quietly, naming it (R3C-12). Nothing was recorded.
	if line, quiet := app.ScheduledLockSkip(out, err, time.Now()); quiet {
		logger.Printf("%s", line)
		return nil
	}
	// Built-in email alert for this scheduled run's result (MA-698), best-effort: a mail
	// failure never fails the backup. On failure the reason travels in the body; it goes once
	// per failing streak, and the first success after one sends a recovery email (MA-870).
	result := alert.ResultOK
	if err != nil {
		result = alert.ResultFailed
	}
	cfg.reportRun(key, out, result, err, 0, logger)
	// After a scheduled run, auto-pause the backup if the source has been unreachable for too
	// long (§5.7, S3-C5): stop the schedule only — reversible, never seals. Best effort; it
	// never fails the run. Its email goes whatever the streak said, and the streak ends. Only a
	// run that itself found the source unreachable pauses, and only a pause that stopped this
	// archive's own entry is told (MA-890).
	paused, perr := wizard.AutoPauseIfDue(out, app.FoundSourceUnreachable(err))
	if perr != nil {
		logger.Printf("auto-pause: %v", perr)
	}
	if paused {
		logger.Printf("auto-paused the scheduled backup: the source has been unreachable for too long; the schedule was stopped (reversible — reconnect the source and re-enable it)")
		backupalert.RecordAutoPause(cfg.SettingsPath, cfg.Store, key, out, errors.New(wizard.AutoPauseWhy), logger)
	}
	return err
}

// recordStartFailure writes a FAILED last-run when a scheduled capture refuses
// before app.RunGraph (not configured), so status/health read RED instead of
// staying GREEN until the run is twice the period late (OP-2, R18). With no
// archive location there is nowhere to record; the caller returns the error
// alone. Best effort: a scheduled run must not fail on its own bookkeeping.
func recordStartFailure(out string, cause error) {
	if strings.TrimSpace(out) == "" {
		return
	}
	_ = state.WriteLastRun(out, state.LastRun{
		Status:  state.RunFailed,
		Started: time.Now().UTC(),
		Error:   cause.Error(),
	})
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
	// A keyed backup's own cancel and skip, nil for any other job (jobcontrol.go).
	cancel  context.CancelFunc
	skip    chan struct{}
	skipped bool
	waiting bool // the program step waits for its download: the skip is offered
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
	c.cancel, c.skip, c.skipped, c.waiting = nil, nil, false, false
	c.mu.Unlock()
}

// capture starts an in-process "Archive now": a loopback-only, CSRF-guarded POST.
func (d *dashboard) capture(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if !server.GuardLocalPOST(r, d.csrf) {
		http.Error(w, csrfRefused, http.StatusForbidden)
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
	out := map[string]any{"running": c.running, "log": append([]string(nil), c.lines...), "result": c.result, "error": c.errMsg,
		"cancellable": c.cancel != nil, "skippable": c.waiting && !c.skipped}
	if c.device != nil {
		out["device"] = map[string]any{"code": c.device.UserCode, "url": c.device.VerificationURI}
	}
	c.mu.Unlock()
	writeJSON(w, out)
}

func (d *dashboard) startCapture() error {
	if off := d.cfg.archiveNowOffLine(); off != "" { // the page shows no button; a direct request is told the same, with the steps
		return errors.New(off)
	}
	cfg := d.cfg.load()
	if cfg == nil || strings.TrimSpace(cfg.Tenant) == "" || strings.TrimSpace(cfg.ClientID) == "" {
		return fmt.Errorf("not configured — open Microsoft 365 setup to add your tenant and application ID first")
	}
	out, ok := d.requireOut()
	if !ok {
		return fmt.Errorf("choose an archive location first")
	}
	if err := app.RefuseKeyless("capture into it", out); err != nil {
		return err
	}
	s := d.cfg.settings()
	tcfg := d.cfg.tokenCfg(cfg)
	// A capture that has to sign in holds the process-wide sign-in mutex for its whole run,
	// so two device flows never run at once: the workspace's sign-in is refused meanwhile,
	// and this one is refused while the workspace signs in (XC8).
	needsFlow := false
	if _, signed := d.cfg.signInState(); !signed {
		if !d.flow.hold("capture") {
			return fmt.Errorf("a Microsoft sign-in is already in progress (started from the Extraction workspace) — finish it first")
		}
		needsFlow = true
	}
	err := d.runJob(func(logger *log.Logger) (string, error) {
		if needsFlow {
			defer d.flow.release("capture")
		}
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
		return archivedLine(res), nil
	})
	if err != nil && needsFlow {
		d.flow.release("capture")
	}
	return err
}

// archivedLine is the result text of a finished capture. When more messages
// were exported than the archive grew by — a key exported again replaces its
// earlier copy — it says so, so "4 exported, 3 indexed" is not a puzzle (F13).
func archivedLine(res app.Result) string {
	line := fmt.Sprintf("Archived — %d message(s) exported, %d indexed.", res.Stats.Exported, res.Indexed)
	if added := res.ManifestSize - res.ManifestBefore; added >= 0 {
		if replaced := res.Stats.Exported - added - res.Stats.Filled; replaced > 0 {
			line += fmt.Sprintf(" (%d of the exports replaced an earlier copy of the same message, so the archive holds %d new.)", replaced, added)
		}
	}
	// The messages with a parse gap, as the CLI and mailarchive-gui count them (res.Undecodable is
	// every record with one): in words true of each kind, since a fallback or an unsplit message
	// has no part in a zip. The report says which part and why (friction F12).
	if res.Undecodable > 0 {
		line += fmt.Sprintf(" %d message(s) carry a gap in how they were read (a part kept as received or cut short, or a text read by an earlier version); attachments-report.tsv in the archive folder says which.", res.Undecodable)
	}
	return line
}
