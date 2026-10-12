// Package source opens mail sources — Outlook .pst/.ost files and Thunderbird /
// mbox stores — and yields normalized messages behind a single Source interface,
// so the rest of the tool is independent of the on-disk format.
package source

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/util"
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
func Open(path string) (Source, error) { return OpenWith(path, OpenOptions{}) }

// OpenWith is Open with what the caller says about the input (the directory-link
// opt-in of a local store, OpenOptions).
func OpenWith(path string, opts OpenOptions) (Source, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	if info.IsDir() {
		// Evolution stores are maildir-based but arrange folders differently, so
		// they get a dedicated reader before the generic mbox/maildir path.
		if r, ok := openEvolution(path, opts); ok {
			return r, nil
		}
		return openMboxDir(path, opts)
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".pst", ".ost":
		return openPST(path)
	}
	// The operator's own spelling of the input may be a link; what is sniffed and read
	// is the file it names, resolved once.
	real := path
	if r, err := filepath.EvalSymlinks(path); err == nil {
		real = r
	}
	if looksLikeMbox(real) {
		return openMboxFile(path, opts)
	}
	return nil, fmt.Errorf("unrecognized mail source: %s (expected a .pst/.ost file, an mbox file, or a mail directory)", path)
}

// IsMailStoreDir reports whether dir looks like a Thunderbird/mbox mail store:
// it contains Mork index files (*.msf), an mbox-shaped file, or a nested folder
// directory (*.sbd). Used by input discovery to treat such a directory as a
// single source rather than expanding it into .pst/.ost files.
func IsMailStoreDir(dir string) bool {
	// Evolution stores (local Maildir++ or an IMAP disk cache) are single sources.
	if isEvolutionCacheStore(dir) || isEvolutionMaildirStore(dir) {
		return true
	}
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

// DataFileReadable reports whether the Outlook .pst/.ost file at path can be
// opened and parsed. It is used by auto-discovery to drop the orphaned or corrupt
// .ost stubs Outlook can leave behind (e.g. from a removed account) so they never
// reach the picker.
//
// It is deliberately lock-tolerant: a file that cannot be opened at all — most
// often because a running Outlook holds it, but also a permissions issue — is
// reported readable (true). Such a file is probably a real mailbox we simply
// can't read this instant (close Outlook, or use -outlook to have Outlook write
// a fresh PST; -copy-first only helps a file Outlook still allows a shared read
// of, not an exclusively locked one), and auto-discovery must never silently
// drop a real mailbox. Only a file that opens at the OS level but fails to parse
// is reported unreadable (false).
func DataFileReadable(path string) bool {
	f, err := util.OpenRegularFile(path)
	if err != nil {
		var nr *util.NotRegularError
		// A pipe, socket or device named like a data file is no mailbox (and a plain open
		// of a pipe would wait for a writer, FC2); any other failure to open is a lock or a
		// permission, and a possibly-valid mailbox is kept.
		return !errors.As(err, &nr)
	}
	f.Close()

	s, err := Open(path)
	if err != nil {
		return false // opened at the OS level but does not parse: orphaned/corrupt
	}
	s.Close()
	return true
}

// looksLikeMbox reports whether the file begins with an mbox "From " separator. It
// opens the file through util.OpenRegularFile, so a pipe named like a mailbox is not
// waited on (discovery runs before the lock is taken, FC2) and a link is not followed.
func looksLikeMbox(path string) bool {
	f, err := util.OpenRegularFile(path)
	if err != nil {
		return false
	}
	defer f.Close()
	ok, err := sniffMbox(f)
	return ok && err == nil
}

// isMailboxCandidate excludes obvious non-mailbox files (indexes, config).
func isMailboxCandidate(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".msf", ".dat", ".json", ".sqlite", ".html":
		return false
	}
	return !strings.HasPrefix(name, ".")
}
