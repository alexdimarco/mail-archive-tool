package desktop

import (
	"path/filepath"
	"runtime"
	"strings"

	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/schedule"
)

// Wire the capture leg of §4.6.5's kind guard: the program corroborates a capture
// for an archive from the desktop configuration, which internal/schedule reads
// through its CaptureCorroborator seam. internal/schedule never imports
// internal/desktop; this init is how it "receives the capture predicate from
// desktop" (§4.6.5b). Every binary that links internal/desktop (the one program
// and the former-name shims) sets it.
func init() { schedule.CaptureCorroborator = CaptureCorroborates }

// CaptureCorroborates reports whether the desktop configuration at the default
// paths corroborates a capture for archive out (§4.6.5b): Microsoft 365 is set up
// (a config with a tenant and client id — the check HeadlessCapture makes before
// its not-configured refusal) and the desktop's current archive location or its
// recorded BackupFor is out. It reads owner-side configuration only, never the
// archive's descriptor, so a forged descriptor cannot fabricate a capture.
func CaptureCorroborates(out string) bool {
	if strings.TrimSpace(out) == "" {
		return false
	}
	cfgPath, err := graphconfig.DefaultConfigPath()
	if err != nil {
		return false
	}
	cfg := Config{ConfigPath: cfgPath, SettingsPath: filepath.Join(filepath.Dir(cfgPath), "desktop-settings.json")}
	c := cfg.load()
	if c == nil || strings.TrimSpace(c.Tenant) == "" || strings.TrimSpace(c.ClientID) == "" {
		return false // Microsoft 365 not configured: no capture is corroborated
	}
	if eo := cfg.effectiveOut(); eo != "" && schedule.SameArchivePath(eo, out, runtime.GOOS) {
		return true
	}
	if bf := strings.TrimSpace(cfg.settings().BackupFor); bf != "" && schedule.SameArchivePath(bf, out, runtime.GOOS) {
		return true
	}
	return false
}
