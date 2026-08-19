// Package source opens mail sources — Outlook .pst/.ost files and Thunderbird /
// mbox stores — and yields normalized messages behind a single Source interface,
// so the rest of the tool is independent of the on-disk format.
package source

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"mail-archive-tool/internal/model"
)

// MessageHandler receives each mail item together with its mirrored folder path
// (already sanitized).
type MessageHandler func(folderPath []string, m *model.Message) error

// Source is an open mail store that can be walked folder-by-folder.
type Source interface {
	// Walk visits every message, invoking handler with its folder path.
	Walk(handler MessageHandler) error
	// StoreName is a human-readable label for the store (used as the top output dir).
	StoreName() string
	// Close releases any resources.
	Close() error
}

// Open opens a mail source, picking the reader by what path is:
//   - a .pst/.ost file            → Outlook reader
//   - a mail-store directory      → mbox reader (e.g. a Thunderbird account dir)
//   - any other file that is mbox → mbox reader (single mbox file)
func Open(path string) (Source, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	if info.IsDir() {
		return openMboxDir(path)
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".pst", ".ost":
		return openPST(path)
	}
	if looksLikeMbox(path) {
		return openMboxFile(path)
	}
	return nil, fmt.Errorf("unrecognized mail source: %s (expected a .pst/.ost file, an mbox file, or a mail directory)", path)
}

// IsMailStoreDir reports whether dir looks like a Thunderbird/mbox mail store:
// it contains Mork index files (*.msf), an mbox-shaped file, or a nested folder
// directory (*.sbd). Used by input discovery to treat such a directory as a
// single source rather than expanding it into .pst/.ost files.
func IsMailStoreDir(dir string) bool {
	if m, _ := filepath.Glob(filepath.Join(dir, "*.msf")); len(m) > 0 {
		return true
	}
	if isMaildir(dir) { // the directory is itself a maildir folder
		return true
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		name := e.Name()
		full := filepath.Join(dir, name)
		if e.IsDir() {
			if strings.HasSuffix(name, ".sbd") || isMaildir(full) {
				return true
			}
			continue
		}
		if isMailboxCandidate(name) && looksLikeMbox(full) {
			return true
		}
	}
	return false
}

// looksLikeMbox reports whether the file begins with an mbox "From " separator.
func looksLikeMbox(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 5)
	n, _ := f.Read(buf)
	return n == 5 && string(buf) == "From "
}

// isMailboxCandidate excludes obvious non-mailbox files (indexes, config).
func isMailboxCandidate(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".msf", ".dat", ".json", ".sqlite", ".html":
		return false
	}
	return !strings.HasPrefix(name, ".")
}
