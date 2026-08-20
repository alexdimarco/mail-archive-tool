package app

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"

	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/pages"
	"mail-archive-tool/internal/state"
)

// Reindex reconciles the archive at out with what is actually on disk. Manually
// deleting, moving, or renaming exported files leaves dangling rows in the
// search index, stale entries in the manifest, and out-of-date folder pages;
// nothing else reconciles them. Reindex opens the index and manifest, prunes
// every entry whose exported file is gone, regenerates the browsable folder
// pages from the surviving set, and saves the manifest. Surviving files stay
// searchable; nothing on disk is deleted. It returns how many rows were kept and
// pruned.
func Reindex(out string, logger *log.Logger) (kept, pruned int, err error) {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}

	idxPath := filepath.Join(out, "search.db")
	// Refuse on a directory that was never exported to, rather than silently
	// creating an empty index and reporting "kept=0 pruned=0" (index.Open would
	// create the file). This mirrors serve/search naming the missing index.
	if _, statErr := os.Stat(idxPath); errors.Is(statErr, fs.ErrNotExist) {
		return 0, 0, fmt.Errorf("no search index at %s (run an export first)", idxPath)
	}
	idx, err := index.Open(idxPath)
	if err != nil {
		return 0, 0, fmt.Errorf("open search index at %s: %w", idxPath, err)
	}
	defer idx.Close()

	mpath := filepath.Join(out, ".mailarchive-manifest.json")
	manifest, err := state.Load(mpath)
	if err != nil {
		return 0, 0, err
	}

	// A dangling row: its exported file no longer exists on disk. Collect first —
	// EachRow holds the DB connection open for the walk, so we must not delete
	// until it returns.
	type dangling struct {
		id  int64
		key string
	}
	var gone []dangling
	err = idx.EachRow(func(id int64, key, relPath string) error {
		full := filepath.Join(out, filepath.FromSlash(relPath))
		switch _, statErr := os.Stat(full); {
		case statErr == nil:
			kept++
			return nil
		case errors.Is(statErr, fs.ErrNotExist):
			gone = append(gone, dangling{id: id, key: key})
			return nil
		default:
			return fmt.Errorf("stat %s: %w", relPath, statErr)
		}
	})
	if err != nil {
		return 0, 0, err
	}

	for _, g := range gone {
		if delErr := idx.DeleteByID(g.id); delErr != nil {
			return 0, 0, fmt.Errorf("prune index row %d: %w", g.id, delErr)
		}
		manifest.Delete(g.key)
		pruned++
	}
	if err := idx.Flush(); err != nil {
		return 0, 0, err
	}

	// Regenerate folder + root index pages from the reconciled index so they no
	// longer list the pruned messages.
	if err := pages.Generate(out, idx, logger); err != nil {
		return 0, 0, fmt.Errorf("regenerate folder pages: %w", err)
	}
	if err := manifest.Save(); err != nil {
		return 0, 0, err
	}
	return kept, pruned, nil
}
