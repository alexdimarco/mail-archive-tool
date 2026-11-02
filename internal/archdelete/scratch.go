package archdelete

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
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
func scratchTargets(dir string) ([]string, error) { return scratchTargetsOwn(dir, false) }

// scratchTargetsOwn is scratchTargets where, with ownOK, a reader lock held by this
// process (the pid in its name is this process's) does not refuse: the plan's exception
// for the page's own reader, which step 1 closes (§3.1 [R3:NF-C7-OWN-READER]).
func scratchTargetsOwn(dir string, ownOK bool) ([]string, error) {
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
				if errors.Is(herr, lockfile.ErrHeld) && !(ownOK && ownReaderLock(e.Name())) {
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

// ownReaderLock reports a reader lock file named for this process's pid.
func ownReaderLock(name string) bool {
	m := readerScratchRe.FindStringSubmatch(name)
	return m != nil && m[1] == strconv.Itoa(os.Getpid())
}

// scratchDirsOf are an encrypted archive's scratch folders, one per spelling of its folder —
// the plan's, the row's, the record's Spelling and each entry's recorded -out (§3.1
// [R3:NF-C7-SCRATCH-NAME]) — through scratchDir (app.archiveScratch's rule). None when the
// archive's id is not known.
func scratchDirsOf(p *DeletePlan, row string, scratchDir func(spelling, archiveID string) (string, error)) ([]string, error) {
	id := p.Record.ID
	if id == "" {
		id = p.ArchiveID
	}
	if id == "" {
		return nil, nil
	}
	seen := map[string]bool{}
	var out []string
	for _, s := range append([]string{p.Folder, row, p.Record.Spelling}, p.Schedules.Spellings...) {
		if s == "" {
			continue
		}
		d, err := scratchDir(s, id)
		if err != nil {
			return nil, err
		}
		if d != "" && !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	return out, nil
}

// scratchNotFound is the refusal when an archive's scratch folder cannot be named.
func scratchNotFound(err error) *Refusal {
	return &Refusal{Case: CaseScratch, Message: fmt.Sprintf("MailArchive's working folder for this archive cannot be found (%s), so nothing was deleted. Click Delete again.", cleanErr(err))}
}

// checkScratchAtPlan is §3.1's scratch listing at the plan: every scratch folder of the
// archive listed with the single-archive rule, where a reader another program holds, or an
// entry MailArchive did not put there, refuses before anything is logged or locked — except a
// reader of this process's while this page has the archive open (openHere), which step 1
// closes [R3:NF-C7-OWN-READER]. Without deps.ScratchDir it lists nothing (Execute's re-check
// under the lock skips the listing; step 1 (c) lists again).
func checkScratchAtPlan(p *DeletePlan, deps Deps) error {
	if p.Kind != Encrypted || deps.ScratchDir == nil {
		return nil
	}
	dirs, err := scratchDirsOf(p, p.Folder, deps.ScratchDir)
	if err != nil {
		return scratchNotFound(err)
	}
	own := deps.OpenHere != nil && deps.OpenHere(p.Folder)
	for _, d := range dirs {
		if _, err := scratchTargetsOwn(d, own); err != nil {
			return scratchRefusal(err)
		}
	}
	return nil
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
