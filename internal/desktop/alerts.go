package desktop

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"

	"mail-archive-tool/internal/alert"
	"mail-archive-tool/internal/backupalert"
	"mail-archive-tool/internal/server"
)

// setAlertPassword stores (or, with "", clears) the backup-alert SMTP password in the OS
// secret store under backupalert.SecretAccount — the one place it lives, never the 0600
// settings file or a log. A build with no store refuses, so a password is never silently
// dropped.
func (cfg Config) setAlertPassword(pw string) error {
	if cfg.Store == nil {
		return errors.New("this computer has no secure place to keep the mail password")
	}
	if strings.TrimSpace(pw) == "" {
		return cfg.Store.Delete(backupalert.SecretAccount)
	}
	return cfg.Store.Set(backupalert.SecretAccount, pw)
}

// fireAlert sends the configured backup-result email for a completed SCHEDULED run,
// best-effort (a mail problem never fails the backup). It delegates to backupalert — the
// shared layer the wizard's local scheduled job also uses — so every scheduled backup
// alerts the same way, from the one configuration the dashboard saves.
func (cfg Config) fireAlert(result, out string, runErr error, messages int, logger *log.Logger) {
	backupalert.FireForRun(cfg.SettingsPath, cfg.Store, out, result, runErr, messages, logger)
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
	if err := backupalert.SendTest(d.cfg.SettingsPath, d.cfg.Store, d.cfg.effectiveOut(), nil); err != nil {
		writeJSON(w, map[string]any{"ok": false, "error": "Could not send the test email: " + err.Error()})
		return
	}
	to := backupalert.LoadConfig(d.cfg.SettingsPath).To
	writeJSON(w, map[string]any{"ok": true, "message": "Test email sent to " + to + " — check it arrived (and the spam folder)."})
}
