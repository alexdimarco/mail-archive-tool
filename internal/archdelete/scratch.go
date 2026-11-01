package archdelete

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/lockfile"
)

// readerScratchRe is a reader's scratch index and its lock file (app.ReaderScratch,
// uninstall.ScratchTargets' rule).
var readerScratchRe = regexp.MustCompile(`^reader-([0-9]+)-[0-9]+\.db(\.lock)?$`)

// errScratchInUse and errScratchUnexpected are the two ways a scratch folder
// refuses (§7's reader and scratch rows [C7]).
var (
	errScratchInUse      = errors.New("a reader of this archive has its working copy open")
	errScratchUnexpected = errors.New("MailArchive's working folder holds what it did not put there")
)

// scratchError names the folder a scratch listing refused for.
type scratchError struct {
	folder string
	err    error
}

func (e *scratchError) Error() string { return e.err.Error() + ": " + e.folder }
func (e *scratchError) Unwrap() error { return e.err }

// scratchTargets is uninstall.ScratchTargets' rule for one archive's scratch folder
// dir (§5.3 step 5 [C7, R3:NF-C7-SCRATCH-NAME]): only the folders snap/,
// _outlook-pst/ and index/, each holding only the files MailArchive puts there —
// -copy-first snapshots, the Outlook app's PSTs, a backup's search.db and a reader's
// scratch index — never a link, never anything else, never the scratch root or
// another archive's folder. A reader lock held now refuses (errScratchInUse). It
// removes nothing; it returns the files, then the folders, deepest first, then dir.
// An absent dir is nothing.
func scratchTargets(dir string) ([]string, error) {
	fi, err := os.Lstat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, &scratchError{dir, err}
	}
	if !fi.IsDir() {
		return nil, &scratchError{dir, errScratchUnexpected}
	}
	kinds, err := os.ReadDir(dir)
	if err != nil {
		return nil, &scratchError{dir, err}
	}
	var files, dirs []string
	for _, k := range kinds {
		sub := filepath.Join(dir, k.Name())
		var owned func(string) bool
		switch {
		case k.Type().IsDir() && k.Name() == "snap":
			owned = func(n string) bool { return strings.HasPrefix(n, export.SnapshotPrefix) }
		case k.Type().IsDir() && k.Name() == outlookPSTDir:
			owned = func(n string) bool { return strings.EqualFold(filepath.Ext(n), ".pst") }
		case k.Type().IsDir() && k.Name() == "index":
			owned = func(n string) bool { return n == "search.db" || readerScratchRe.MatchString(n) }
		default:
			return nil, &scratchError{dir, errScratchUnexpected}
		}
		entries, err := os.ReadDir(sub)
		if err != nil {
			return nil, &scratchError{dir, err}
		}
		for _, e := range entries {
			p := filepath.Join(sub, e.Name())
			if !e.Type().IsRegular() || !owned(e.Name()) {
				return nil, &scratchError{dir, errScratchUnexpected}
			}
			// A live reader holds its scratch index under an OS lock on the lock file
			// beside it: taken here for the instant, never kept.
			if strings.HasSuffix(e.Name(), ".db.lock") {
				f, herr := lockfile.HoldFile(p)
				if errors.Is(herr, lockfile.ErrHeld) {
					return nil, &scratchError{dir, errScratchInUse}
				}
				if f != nil {
					f.Close()
				}
			}
			files = append(files, p)
		}
		dirs = append(dirs, sub)
	}
	sort.Strings(files)
	sort.Strings(dirs)
	return append(append(files, dirs...), dir), nil
}

// scratchRefusal is §7's refusal for a scratch listing's error.
func scratchRefusal(err error) *Refusal {
	var se *scratchError
	folder := ""
	if errors.As(err, &se) {
		folder = se.folder
	}
	if errors.Is(err, errScratchInUse) {
		return &Refusal{Case: CaseReader, Message: msgReaderOpen}
	}
	return &Refusal{Case: CaseScratch, Message: "MailArchive's working folder for this archive holds files it did not put there: " + folder + ". Remove them, then click Delete again."}
}
