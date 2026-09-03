// Package state persists which messages have already been exported so that
// incremental runs only write new items.
package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// manifestVersion: 1 = paths only; 2 = completeness tracking (Missing/Terminal/
// Unresolved per record). Fields are additive; a version-1 file is migrated on
// load (every record becomes an "unknown" sentinel — see Load).
const manifestVersion = 2

// UnknownSentinel marks a record whose completeness predates tracking: it is
// fillable, so the next incremental run re-examines it once.
const UnknownSentinel = "unknown"

// MissingBody is the gap label for a message captured with no body at all.
const MissingBody = "body"

// keySeparator joins the folder path and message identity into a manifest key.
// The NUL byte cannot appear in either component, so it is an unambiguous
// delimiter. encoding/json escapes it within the on-disk key string.
const keySeparator = "\x00"

// Record is what we remember about a single exported message.
type Record struct {
	Path       string    `json:"path"`   // export path relative to the output root
	Folder     string    `json:"folder"` // human-readable source folder path
	ExportedAt time.Time `json:"exported_at"`

	// Completeness (version 2). Missing lists FILLABLE gaps — "body", an
	// attachment label, or UnknownSentinel — that an on-demand source may still
	// deliver, so incremental runs re-examine the record. Terminal lists the
	// same kinds of gap from a complete-at-fetch source (nothing will ever fill
	// them; recorded and reported, never retried). Unresolved lists cid: tokens
	// with no matching part (informational). Subject/Date are carried only when
	// one of the three is non-empty, so the manifest grows with issues, not with
	// messages.
	Missing    []string `json:"missing,omitempty"`
	Terminal   []string `json:"terminal,omitempty"`
	Unresolved []string `json:"unresolved,omitempty"`
	Subject    string   `json:"subject,omitempty"`
	Date       string   `json:"date,omitempty"` // RFC 3339 UTC
}

// Fillable reports whether an incremental run should re-examine the record.
func (r Record) Fillable() bool { return len(r.Missing) > 0 }

// Complete reports whether nothing at all is missing (fillable or terminal).
func (r Record) Complete() bool { return len(r.Missing) == 0 && len(r.Terminal) == 0 }

// HasIssues reports whether the record has anything to show in the report.
func (r Record) HasIssues() bool {
	return len(r.Missing) > 0 || len(r.Terminal) > 0 || len(r.Unresolved) > 0
}

// Unknown reports whether the record carries the legacy sentinel.
func (r Record) Unknown() bool {
	return len(r.Missing) == 1 && r.Missing[0] == UnknownSentinel
}

// Manifest is the set of exported messages, keyed by Key(folder, identity).
type Manifest struct {
	path string

	mu      sync.Mutex
	Version int               `json:"version"`
	Entries map[string]Record `json:"entries"`

	// Migrated counts the records converted to the unknown sentinel by this
	// Load (a version-1 file). Zero for a version-2 file.
	Migrated int `json:"-"`
}

// Key builds the manifest key for a message. Scoping the key by folder means
// the same email filed in two folders is exported to both locations, while a
// re-run still skips each (folder, message) pair it has already written.
func Key(folderPath, identity string) string {
	return folderPath + keySeparator + identity
}

// Load reads the manifest at path. A missing file yields an empty manifest
// bound to that path (so a later Save creates it).
func Load(path string) (*Manifest, error) {
	m := &Manifest{path: path, Version: manifestVersion, Entries: map[string]Record{}}

	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	if len(data) == 0 {
		return m, nil
	}
	if err := json.Unmarshal(data, m); err != nil {
		return nil, fmt.Errorf("manifest %s is corrupt or truncated (%v): restore it from a backup, or delete it to re-export everything (the archived files are untouched; incremental runs will rewrite them in place)", path, err)
	}
	if m.Entries == nil {
		m.Entries = map[string]Record{}
	}
	// A version-1 file — written before completeness tracking, or rewritten by
	// an older binary after a downgrade — cannot say which entries are
	// incomplete. Never assume "complete": mark every record with the unknown
	// sentinel so the next incremental run re-examines it once (R1).
	if m.Version < manifestVersion {
		for key, r := range m.Entries {
			if !r.HasIssues() {
				r.Missing = []string{UnknownSentinel}
				m.Entries[key] = r
				m.Migrated++
			}
		}
		m.Version = manifestVersion
	}
	return m, nil
}

// Has reports whether key has already been exported.
func (m *Manifest) Has(key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.Entries[key]
	return ok
}

// Get returns the record for key.
func (m *Manifest) Get(key string) (Record, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.Entries[key]
	return r, ok
}

// Add records key as exported.
func (m *Manifest) Add(key string, r Record) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Entries[key] = r
}

// Resolve clears the unknown sentinel from key without touching anything else:
// used when a complete-at-fetch source revisits a legacy record (nothing
// fillable can exist, so the record is complete as captured).
func (m *Manifest) Resolve(key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.Entries[key]
	if !ok || !r.Unknown() {
		return false
	}
	r.Missing = nil
	if !r.HasIssues() {
		r.Subject, r.Date = "", ""
	}
	m.Entries[key] = r
	return true
}

// Issues returns every record with something to report, sorted by folder,
// then date, then path (a stable order for the regenerated report).
func (m *Manifest) Issues() []Record {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Record
	for _, r := range m.Entries {
		if r.HasIssues() {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Folder != out[j].Folder {
			return out[i].Folder < out[j].Folder
		}
		if out[i].Date != out[j].Date {
			return out[i].Date < out[j].Date
		}
		return out[i].Path < out[j].Path
	})
	return out
}

// Counts returns how many records are fillable (excluding sentinels), terminal,
// and unknown (legacy sentinels).
func (m *Manifest) Counts() (fillable, terminal, unknown int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.Entries {
		switch {
		case r.Unknown():
			unknown++
		case r.Fillable():
			fillable++
		case len(r.Terminal) > 0:
			terminal++
		}
	}
	return
}

// Delete removes key from the manifest. Absent keys are a no-op. Used by the
// reindex self-repair to prune entries whose exported file no longer exists.
func (m *Manifest) Delete(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.Entries, key)
}

// Len returns the number of recorded entries.
func (m *Manifest) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.Entries)
}

// Save writes the manifest atomically (temp file + rename) so a crash mid-write
// cannot corrupt an existing manifest.
func (m *Manifest) Save() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Version = manifestVersion
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}

	dir := filepath.Dir(m.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create manifest dir: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".manifest-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp manifest: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp manifest: %w", err)
	}
	// Durability, not just namespace atomicity: the bytes must be on disk
	// before the rename makes them the manifest of record, and the directory
	// entry must be on disk before we report success (R5).
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync temp manifest: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp manifest: %w", err)
	}
	if err := os.Rename(tmpName, m.path); err != nil {
		return fmt.Errorf("replace manifest: %w", err)
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync() // best effort: not every filesystem supports fsync on a directory
		d.Close()
	}
	return nil
}
