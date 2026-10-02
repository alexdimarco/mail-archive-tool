package desktop

import (
	"encoding/json"
	"os"
	"strings"

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
	WeeklyDay      string `json:"weeklyDay,omitempty"`  // "Sunday".."Saturday"
	WeeklyTime     string `json:"weeklyTime,omitempty"` // "03:00"
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
