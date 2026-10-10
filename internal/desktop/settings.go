package desktop

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/util"
)

// Settings are the dashboard's own preferences (not Graph credentials): where to
// archive, whether to keep raw .eml, whether to include Deleted/Junk, and the
// weekly schedule choice. Stored as a 0600 JSON file under the OS config dir.
type Settings struct {
	Version        int    `json:"version"`
	Out            string `json:"out,omitempty"`
	KeepRaw        bool   `json:"keepRaw,omitempty"`
	IncludeDeleted bool   `json:"includeDeleted,omitempty"`
	IncludeJunk    bool   `json:"includeJunk,omitempty"`
	WeeklyDay      string `json:"weeklyDay,omitempty"`  // "Sunday".."Saturday" (weekly runs Sunday for now)
	WeeklyTime     string `json:"weeklyTime,omitempty"` // "03:00"
	Interval       string `json:"interval,omitempty"`   // "daily" | "weekly" (default weekly)
}

// LoadSettings reads the settings file, returning the zero value (all defaults) on
// a missing or unreadable file — the dashboard always has usable settings.
func LoadSettings(path string) Settings {
	data, err := os.ReadFile(path)
	if err != nil {
		return Settings{}
	}
	var s Settings
	if json.Unmarshal(data, &s) != nil {
		return Settings{}
	}
	return s
}

// SaveSettings writes the settings atomically at 0600.
func SaveSettings(path string, s Settings) error {
	s.Version = 1
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return util.WriteFileAtomic0600(path, data)
}

func (cfg Config) settings() Settings { return LoadSettings(cfg.SettingsPath) }

// effectiveOut is the archive location: the saved setting if present, else the
// launch -out.
func (cfg Config) effectiveOut() string {
	if o := strings.TrimSpace(cfg.settings().Out); o != "" {
		return o
	}
	return strings.TrimSpace(cfg.Out)
}

// settingsDirs are the folders MailArchive Desktop keeps its settings and
// sign-in in: its config file's folder (the settings file sits beside it) and
// the default one under the OS config dir, where the file token cache lives
// (design-local-confidentiality P10).
func (cfg Config) settingsDirs() []string {
	var dirs []string
	if strings.TrimSpace(cfg.ConfigPath) != "" {
		dirs = append(dirs, filepath.Dir(cfg.ConfigPath))
	}
	if d, err := graphconfig.DefaultConfigDir(); err == nil {
		dirs = append(dirs, d)
	}
	return dirs
}

// settingsOverlap refuses an archive location that is, contains or sits inside
// a settings folder — the Settings POST's check, the operator being present
// (P10). nil when out is apart from them all.
func (cfg Config) settingsOverlap(out string) error {
	for _, d := range cfg.settingsDirs() {
		if overlap, how := util.Overlap(out, d); overlap {
			return fmt.Errorf("The archive location %s %s %s, the folder MailArchive keeps your settings and sign-in in. Why: the archive is meant to be copied and synced, so they would travel with it. Do this: choose an archive location that does not contain %s and is not inside it.", out, how, d, d)
		}
	}
	return nil
}

// settingsInArchive is the scheduled capture's check (P10, CC16): the settings
// folder the archive location out is or contains, or "" when it holds none.
func (cfg Config) settingsInArchive(out string) string {
	ro := util.ResolveExisting(out)
	for _, d := range cfg.settingsDirs() {
		if util.Within(ro, util.ResolveExisting(d)) {
			return d
		}
	}
	return ""
}
