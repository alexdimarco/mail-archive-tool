package desktop

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
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
//
// A file that cannot be read (a sharing violation, an antivirus scan, a permission error)
// is left as it is and the update is refused: the next call finds it again. A file that
// reads but does not parse as settings (damaged, or written by a newer build in a shape
// this one does not know) is set aside as "<name>.unreadable" before the new file is
// written, so what it held is not lost (EW-AC14). A file that parses keeps the keys this
// build has no field for, and a newer version number, through the save (SaveSettings). A
// missing file starts from the defaults.
func UpdateSettings(path, who string, fn func(*Settings)) error {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	var s Settings
	data, rerr := os.ReadFile(path)
	switch {
	case rerr == nil:
		if json.Unmarshal(data, &s) != nil {
			s = Settings{}
			if err := util.WriteFileAtomic0600(path+".unreadable", data); err != nil {
				return fmt.Errorf("the settings file can't be read and a copy of it could not be kept, so it was left as it is: %w", err)
			}
		}
	case errors.Is(rerr, fs.ErrNotExist):
	default:
		return fmt.Errorf("the settings file can't be read right now, so it was left as it is: %w", rerr)
	}
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

// SaveSettings writes the settings atomically at 0600. A key the file holds that this build
// has no field for (written by a newer build) is kept as it was, and so is a version above
// this build's own: saving never turns a newer build's file into an older one's (EW-AC14).
func SaveSettings(path string, s Settings) error {
	if s.Version < 1 {
		s.Version = 1
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return util.WriteFileAtomic0600(path, keepUnknownSettings(path, data))
}

// settingsKeys is the JSON keys Settings has a field for.
var settingsKeys = func() map[string]bool {
	keys := map[string]bool{}
	t := reflect.TypeOf(Settings{})
	for i := 0; i < t.NumField(); i++ {
		if name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ","); name != "" && name != "-" {
			keys[name] = true
		}
	}
	return keys
}()

// keepUnknownSettings adds to the encoded settings the keys of the file at path that
// Settings has no field for. A file that is missing, unreadable or not a JSON object has
// none to keep.
func keepUnknownSettings(path string, encoded []byte) []byte {
	old, err := os.ReadFile(path)
	if err != nil {
		return encoded
	}
	var oldKeys map[string]json.RawMessage
	if json.Unmarshal(old, &oldKeys) != nil {
		return encoded
	}
	var merged map[string]json.RawMessage
	if json.Unmarshal(encoded, &merged) != nil {
		return encoded
	}
	kept := false
	for k, v := range oldKeys {
		if !settingsKeys[k] {
			merged[k] = v
			kept = true
		}
	}
	if !kept {
		return encoded
	}
	out, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return encoded
	}
	return out
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
