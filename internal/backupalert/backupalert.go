// Package backupalert fires the built-in backup-result email for a completed scheduled
// run. It is the shared layer both run paths use — the Desktop's Microsoft 365 capture
// and the wizard's local scheduled job (which cannot import internal/desktop) — so a
// local backup alerts exactly as the Microsoft 365 one does, from the one alert
// configuration the dashboard saves. It reads that config from the Desktop settings file
// and the SMTP password from the OS secret store, and sends with internal/alert (pure Go,
// no outside program). Best-effort throughout: an alert problem never fails a backup.
package backupalert

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"time"

	"mail-archive-tool/internal/alert"
	"mail-archive-tool/internal/graphconfig"
)

// SecretAccount is the OS-secret-store account key the alert SMTP password is kept under
// (the one place it lives; never the settings file or a log).
const SecretAccount = "mailarchive-alert-smtp"

// Result values a caller passes to FireForRun, re-exported so a caller (the wizard's local
// job) need not also import internal/alert.
const (
	ResultOK     = alert.ResultOK
	ResultFailed = alert.ResultFailed
	ResultPaused = alert.ResultPaused
)

// sendFn is the engine send; a seam a test replaces via SetSenderForTest.
var sendFn = alert.Send

// SetSenderForTest replaces the mail sender and returns a restore function, so a test can
// exercise the fire path without an SMTP server.
func SetSenderForTest(f func(alert.Config, string, alert.Event) error) (restore func()) {
	prev := sendFn
	sendFn = f
	return func() { sendFn = prev }
}

// DefaultSettingsPath is where the Desktop keeps its settings (desktop-settings.json under
// the OS config dir) — where a scheduled run that was not handed a path reads the alert
// config. A var so a test can redirect it.
var DefaultSettingsPath = func() string {
	dir, err := graphconfig.DefaultConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "desktop-settings.json")
}

// defaultStore is the OS secret store; a var so a test can redirect it.
var defaultStore = func() graphconfig.SecretStore {
	s, _ := graphconfig.DefaultSecretStore()
	return s
}

// fileConfig mirrors the alert fields of the Desktop settings file; the JSON keys are the
// contract between the Desktop (which writes them) and any scheduled run (which reads them).
// A desktop_test asserts these keys stay in step with the Desktop's Settings struct.
type fileConfig struct {
	AlertEnabled   bool   `json:"alertEnabled"`
	AlertOnFailure bool   `json:"alertOnFailure"`
	AlertOnSuccess bool   `json:"alertOnSuccess"`
	AlertOnPause   bool   `json:"alertOnPause"`
	AlertProvider  string `json:"alertProvider"`
	AlertHost      string `json:"alertHost"`
	AlertPort      int    `json:"alertPort"`
	AlertSecurity  string `json:"alertSecurity"`
	AlertFrom      string `json:"alertFrom"`
	AlertTo        string `json:"alertTo"`
	AlertUsername  string `json:"alertUsername"`
}

// LoadConfig reads the alert configuration from a Desktop settings file. A missing or
// unreadable file yields a zero (disabled) config, never an error — a scheduled run must
// not fail because it could not read an optional alert setting.
func LoadConfig(settingsPath string) alert.Config {
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return alert.Config{}
	}
	var f fileConfig
	if json.Unmarshal(data, &f) != nil {
		return alert.Config{}
	}
	return alert.Config{
		Enabled: f.AlertEnabled, OnFailure: f.AlertOnFailure, OnSuccess: f.AlertOnSuccess, OnPause: f.AlertOnPause,
		Provider: f.AlertProvider, Host: f.AlertHost, Port: f.AlertPort, Security: f.AlertSecurity,
		From: f.AlertFrom, To: f.AlertTo, Username: f.AlertUsername,
	}
}

// Password reads the SMTP password from the store ("" when none or no store).
func Password(store graphconfig.SecretStore) string {
	if store == nil {
		return ""
	}
	pw, _ := store.Get(SecretAccount)
	return pw
}

// FireForRun sends the configured alert for a completed run, best-effort: a disabled alert
// or a result the config does not want sends nothing, and a mail failure is logged, never
// returned, so it can never fail the backup.
func FireForRun(settingsPath string, store graphconfig.SecretStore, out, result string, runErr error, messages int, logger *log.Logger) {
	cfg := LoadConfig(settingsPath)
	if !cfg.ShouldSend(result) {
		return
	}
	host, _ := os.Hostname()
	ev := alert.Event{Result: result, Archive: out, Messages: messages, Host: host, When: time.Now()}
	if runErr != nil {
		ev.Error = runErr.Error()
	}
	if err := sendFn(cfg, Password(store), ev); err != nil {
		if logger != nil {
			logger.Printf("backup alert email could not be sent: %v", err)
		}
		return
	}
	if logger != nil {
		logger.Printf("backup alert email sent (%s) to %s", result, cfg.To)
	}
}

// FireForRunDefault fires using the default Desktop settings path and OS secret store, for
// a scheduled run (the wizard's local job) that reads the one dashboard-saved config.
func FireForRunDefault(out, result string, runErr error, messages int, logger *log.Logger) {
	FireForRun(DefaultSettingsPath(), defaultStore(), out, result, runErr, messages, logger)
}

// SendTest sends one test email with the saved configuration regardless of the on/off
// toggle, so the dashboard's "Send test email" button can confirm the setup. It validates
// first, so a missing field is named rather than silently dropped.
func SendTest(settingsPath string, store graphconfig.SecretStore, out string, logger *log.Logger) error {
	cfg := LoadConfig(settingsPath)
	cfg.Enabled = true
	if err := cfg.Validate(); err != nil {
		return err
	}
	host, _ := os.Hostname()
	ev := alert.Event{Result: alert.ResultOK, Archive: "— MailArchive alert test (no backup ran) —", Host: host, When: time.Now()}
	return sendFn(cfg, Password(store), ev)
}
