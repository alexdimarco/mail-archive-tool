package desktop

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

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

	// The extraction workspace's remembered destination and options (design §4.17).
	// nil means the default: Preserve folder structure ON, Readable HTML OFF until slice
	// E5 lands (XC28), extracted attachments OFF. They are saved when an export starts.
	ExportDest        string `json:"exportDest,omitempty"`
	ExportPreserve    *bool  `json:"exportPreserve,omitempty"`
	ExportHTML        *bool  `json:"exportHTML,omitempty"`
	ExportAttachments *bool  `json:"exportAttachments,omitempty"`
}

// Preserve is the remembered "Preserve folder structure": ON when never set.
func (s Settings) Preserve() bool { return s.ExportPreserve == nil || *s.ExportPreserve }

// ReadableHTML is the remembered "Readable HTML": OFF when never set, and hidden, until
// slice E5 lands (XC28).
func (s Settings) ReadableHTML() bool { return s.ExportHTML != nil && *s.ExportHTML }

// ExtractAttachments is the remembered "Extract attachments": OFF when never set.
func (s Settings) ExtractAttachments() bool {
	return s.ExportAttachments != nil && *s.ExportAttachments
}

// settingsMu serializes every settings load-modify-save, the /api/settings save and an
// export start alike (XC34): each reloads the file under the lock and changes only the
// fields its caller owns, so two tabs, or a settings save racing an export start, can
// never write back a stale Out that the scheduled backup would then read.
var settingsMu sync.Mutex

// settingsHook, when set, is called with the caller's name after the file is loaded and
// before it is saved, with the lock held: the interleaving point a test uses (MA-524).
var settingsHook func(who string)

// UpdateSettings reloads the settings file under the lock, lets fn change the fields
// its caller owns, and saves the result atomically at 0600.
func UpdateSettings(path, who string, fn func(*Settings)) error {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	s := LoadSettings(path)
	if settingsHook != nil {
		settingsHook(who)
	}
	fn(&s)
	return SaveSettings(path, s)
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
