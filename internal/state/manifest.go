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
	"sync"
	"time"
)

// manifestVersion is bumped if the on-disk format changes incompatibly.
const manifestVersion = 1

// keySeparator joins the folder path and message identity into a manifest key.
// The NUL byte cannot appear in either component, so it is an unambiguous
// delimiter. encoding/json escapes it within the on-disk key string.
const keySeparator = "\x00"

// Record is what we remember about a single exported message.
type Record struct {
	Path       string    `json:"path"`   // export path relative to the output root
	Folder     string    `json:"folder"` // human-readable source folder path
	ExportedAt time.Time `json:"exported_at"`
}

// Manifest is the set of exported messages, keyed by Key(folder, identity).
type Manifest struct {
	path string

	mu      sync.Mutex
	Version int               `json:"version"`
	Entries map[string]Record `json:"entries"`
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
		return nil, fmt.Errorf("parse manifest %s: %w", path, err)
	}
	if m.Entries == nil {
		m.Entries = map[string]Record{}
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

// Add records key as exported.
func (m *Manifest) Add(key string, r Record) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Entries[key] = r
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
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp manifest: %w", err)
	}
	if err := os.Rename(tmpName, m.path); err != nil {
		return fmt.Errorf("replace manifest: %w", err)
	}
	return nil
}
