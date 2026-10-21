package desktop

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"mail-archive-tool/internal/alert"
	"mail-archive-tool/internal/server"
)

// alertSecretAccount is the account key the backup-alert SMTP password is kept under
// in the OS secret store (Windows Credential Manager, or a 0600 file elsewhere) — the
// same store the Microsoft 365 secret uses, so the password never touches the 0600
// settings file or a log (MA-698).
const alertSecretAccount = "mailarchive-alert-smtp"

// sendAlert is the mail transport, a seam a test replaces so the wiring is exercised
// without an SMTP server.
var sendAlert = alert.Send

// alertConfig builds the alert engine's config from the saved settings (the password
// is read separately, from the vault).
func (cfg Config) alertConfig() alert.Config {
	s := cfg.settings()
	return alert.Config{
		Enabled:   s.AlertEnabled,
		OnFailure: s.AlertOnFailure,
		OnSuccess: s.AlertOnSuccess,
		OnPause:   s.AlertOnPause,
		Provider:  s.AlertProvider,
		Host:      s.AlertHost,
		Port:      s.AlertPort,
		Security:  s.AlertSecurity,
		From:      s.AlertFrom,
		To:        s.AlertTo,
		Username:  s.AlertUsername,
	}
}

// alertPassword reads the SMTP password from the OS secret store ("" when none or no
// store).
func (cfg Config) alertPassword() string {
	if cfg.Store == nil {
		return ""
	}
	pw, _ := cfg.Store.Get(alertSecretAccount)
	return pw
}

// setAlertPassword stores (or, with "", clears) the SMTP password in the OS secret
// store. A build with no store refuses, so a password is never silently dropped.
func (cfg Config) setAlertPassword(pw string) error {
	if cfg.Store == nil {
		return errors.New("this computer has no secure place to keep the mail password")
	}
	if strings.TrimSpace(pw) == "" {
		return cfg.Store.Delete(alertSecretAccount)
	}
	return cfg.Store.Set(alertSecretAccount, pw)
}

// fireAlert sends the configured backup-result email for a completed SCHEDULED run,
// best-effort: a mail failure is logged, never propagated, so it can never fail the
// backup itself (R-y style). It reads the password from the vault, so the settings
// file and the log never carry it.
func (cfg Config) fireAlert(result, out string, runErr error, messages int, logger *log.Logger) {
	ac := cfg.alertConfig()
	if !ac.ShouldSend(result) {
		return
	}
	host, _ := os.Hostname()
	ev := alert.Event{Result: result, Archive: out, Messages: messages, Host: host, When: time.Now()}
	if runErr != nil {
		ev.Error = runErr.Error()
	}
	if err := sendAlert(ac, cfg.alertPassword(), ev); err != nil {
		if logger != nil {
			logger.Printf("backup alert email could not be sent: %v", err)
		}
		return
	}
	if logger != nil {
		logger.Printf("backup alert email sent (%s) to %s", result, ac.To)
	}
}

// saveAlertSettings persists the backup-alert configuration (loopback+CSRF guarded).
// The SMTP password goes to the OS secret store, never the 0600 settings file: a blank
// password leaves the stored one unchanged (so re-saving other fields keeps it), and
// ClearPassword removes it.
func (d *dashboard) saveAlertSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if !server.GuardLocalPOST(r, d.csrf) {
		http.Error(w, "refused: same-origin, tokened, local requests only", http.StatusForbidden)
		return
	}
	var in struct {
		Enabled, OnFailure, OnSuccess, OnPause                 bool
		Provider, Host, Security, From, To, Username, Password string
		Port                                                   int
		ClearPassword                                          bool
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&in); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	// A known provider fills in any host/port/security it was not given.
	host, port, security := strings.TrimSpace(in.Host), in.Port, strings.TrimSpace(in.Security)
	if ph, pp, ps := alert.Preset(strings.TrimSpace(in.Provider)); ph != "" {
		if host == "" {
			host = ph
		}
		if port == 0 {
			port = pp
		}
		if security == "" {
			security = ps
		}
	}
	// The password is a secret: it goes to the vault, never the settings file.
	switch {
	case strings.TrimSpace(in.Password) != "":
		if err := d.cfg.setAlertPassword(in.Password); err != nil {
			writeJSON(w, map[string]any{"ok": false, "error": err.Error()})
			return
		}
	case in.ClearPassword:
		_ = d.cfg.setAlertPassword("")
	}
	if err := UpdateSettings(d.cfg.SettingsPath, "alert", func(s *Settings) {
		s.AlertEnabled = in.Enabled
		s.AlertOnFailure = in.OnFailure
		s.AlertOnSuccess = in.OnSuccess
		s.AlertOnPause = in.OnPause
		s.AlertProvider = strings.TrimSpace(in.Provider)
		s.AlertHost = host
		s.AlertPort = port
		s.AlertSecurity = security
		s.AlertFrom = strings.TrimSpace(in.From)
		s.AlertTo = strings.TrimSpace(in.To)
		s.AlertUsername = strings.TrimSpace(in.Username)
	}); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": "The alert settings could not be saved: " + err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// alertTest sends a test email with the SAVED alert configuration, so IT can confirm
// the server, credentials and recipient before relying on it. A test ignores the
// on/off toggle; it validates first so a missing field is named, not a silent drop.
func (d *dashboard) alertTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if !server.GuardLocalPOST(r, d.csrf) {
		http.Error(w, "refused: same-origin, tokened, local requests only", http.StatusForbidden)
		return
	}
	ac := d.cfg.alertConfig()
	ac.Enabled = true
	if err := ac.Validate(); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": "The alert settings are not complete: " + err.Error()})
		return
	}
	host, _ := os.Hostname()
	ev := alert.Event{Result: alert.ResultOK, Archive: "— MailArchive alert test (no backup ran) —", Host: host, When: time.Now()}
	if err := sendAlert(ac, d.cfg.alertPassword(), ev); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": "The test email could not be sent: " + err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true, "message": "Test email sent to " + ac.To + " — check it arrived (and the spam folder)."})
}
