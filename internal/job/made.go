package job

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/util"
)

// The made-list (docs/design-per-mailbox-archives.md §10 "The stand-alone wizard's archives", C10,
// C25, revision 2.1's F-02 and F-06; MA-1500): the stand-alone wizard records each archive folder
// it makes for a set, one line each, so MailArchive Desktop can list every archive of the set
// (last-archive.txt holds one path), and the wizard's re-run can find a folder it made but never
// finished (§7, C5). A line is the folder's absolute path, a tab, and the mailbox's folder name.

const (
	// MadeListName is the made-list's file in MailArchive's settings folder (Dir), beside
	// last-archive.txt.
	MadeListName = "wizard-made.txt"
	// MadeLockName is the lock file beside it that every change of it takes.
	MadeLockName = "wizard-made.lock"
	// MadeMaxLines and MadeMaxBytes bound the list: a change keeps at most this many lines and
	// bytes, the oldest dropped, and a read refuses a file over MadeMaxBytes.
	MadeMaxLines = 200
	MadeMaxBytes = 64 * 1024
)

// madeKind names the made-list in a refusal.
const madeKind = "the wizard's list of the archive folders it made"

// Made is one line of the made-list: an archive folder the wizard made, the folder name of the
// mailbox it was made for, and that mailbox's store id (the adversarial pass's OUT-03: §7's
// unfinished-folder rule continues a folder only for that store; a line written before keeps no
// store id, and the rule then matches it by the name, as it did).
type Made struct {
	Path  string
	Name  string
	Store string
}

// MadeListFile is the made-list in MailArchive's settings folder.
func MadeListFile() (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, MadeListName), nil
}

// MadeLockPath is the lock file beside the made-list at file.
func MadeLockPath(file string) string { return filepath.Join(filepath.Dir(file), MadeLockName) }

// MadeSetLockName is the lock file beside the made-list that a set's run holds from its start to its
// end (the adversarial pass's CON-03): never in the set's parent folder, where MailArchive writes
// nothing but the archive folders (§6).
const MadeSetLockName = "wizard-set.lock"

// ErrSetBusy refuses a set's run while another MailArchive window holds the set lock: two sets made
// at once into one folder would each make the other's archives again, as " (2)" copies with their
// own schedules.
var ErrSetBusy = errors.New("Another MailArchive window is making archives now, so nothing was made. When that window has finished, try again.")

// HoldSetLock takes the set lock beside the made-list at file, for the whole of one set's run, and
// returns its release. Held by another program (another wizard window, MailArchive Desktop's Make a
// new archive…) it is ErrSetBusy, at once. A settings folder that cannot hold the lock (inside an
// encrypted archive, not writable) gives a release that does nothing and no error: no set can record
// a new folder there either (RecordMade is refused the same way), so no two sets can make one
// mailbox's folder twice.
func HoldSetLock(file string) (release func(), err error) {
	none := func() {}
	if err := RefuseSettingsWrite(file, madeKind); err != nil {
		return none, nil
	}
	dir := filepath.Dir(file)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return none, nil
	}
	l, err := lockfile.AcquireAs(filepath.Join(dir, MadeSetLockName), "wizard-set")
	switch {
	case errors.Is(err, lockfile.ErrHeld):
		return none, ErrSetBusy
	case err != nil:
		return none, nil
	}
	return func() { _ = l.Release() }, nil
}

// madeLockWait is how long a change of the made-list waits for another program's change of it: a
// change holds the lock for one read and one write, so a wait this long means the holder is stuck.
var madeLockWait = 5 * time.Second

// MadeBusyError refuses a change of the made-list while another MailArchive program changes it for
// longer than the wait. Nothing was changed.
type MadeBusyError struct {
	Path string
	Err  error
}

func (e *MadeBusyError) Error() string {
	return fmt.Sprintf("%s (%s) is being changed by another MailArchive window; try again", madeKind, util.StripControl(e.Path))
}

func (e *MadeBusyError) Unwrap() error { return e.Err }

// ReadMade reads the made-list at file through the secure regular-file reader, inside an os.Root
// on its folder: a link at its name is refused and never followed, a pipe or a folder is refused
// and never waited on, and a file over MadeMaxBytes is refused, never read whole. A line holding a
// control character other than its one tab, a line with no tab, and a line whose path is not
// absolute are skipped (F-06); of two lines for one folder the later is kept. A list or a settings
// folder that is not there is an empty list, no error.
func ReadMade(file string) ([]Made, error) {
	root, err := os.OpenRoot(filepath.Dir(file))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, madeReadError(file, err)
	}
	defer root.Close()
	name := filepath.Base(file)
	if fi, err := root.Lstat(name); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, madeReadError(file, err)
	} else if fi.Mode()&fs.ModeSymlink != 0 {
		return nil, madeReadError(file, &util.NotRegularError{Path: file, Mode: fi.Mode()})
	}
	f, err := util.OpenRegularFileIn(root, name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, madeReadError(file, err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MadeMaxBytes+1))
	if err != nil {
		return nil, madeReadError(file, err)
	}
	if len(data) > MadeMaxBytes {
		return nil, fmt.Errorf("%s (%s) is over its %d-byte bound, so it was not read. %s", madeKind, util.StripControl(file), MadeMaxBytes, madeRemedy)
	}
	return parseMade(data), nil
}

// madeRemedy is what to do about a made-list that cannot be read (the adversarial pass's INS-08: the
// reason named the file but not the remedy; the builder's words). Only a program that can write the
// settings folder puts a link, a folder or an oversized file at its name.
const madeRemedy = "To fix it, move or delete that file (MailArchive makes a new one), then run this again."

func madeReadError(file string, err error) error {
	return fmt.Errorf("%s (%s) could not be read: %w. %s", madeKind, util.StripControl(file), err, madeRemedy)
}

// parseMade is the made-list's lines, as ReadMade keeps them.
func parseMade(data []byte) []Made {
	var out []Made
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		p, rest, ok := strings.Cut(line, "\t")
		// A line's third field, the store id, is there from round B of the adversarial pass on; a
		// line of two fields (written before) is read as it was.
		name, store, _ := strings.Cut(rest, "\t")
		if !ok || hasControl(p) || hasControl(name) || hasControl(store) || !filepath.IsAbs(p) {
			continue
		}
		out = withMade(out, Made{Path: filepath.Clean(p), Name: name, Store: store})
	}
	return out
}

// withMade is ms with m as its newest line: any line for m's folder is dropped first.
func withMade(ms []Made, m Made) []Made {
	kept := ms[:0:0]
	for _, o := range ms {
		if o.Path != m.Path {
			kept = append(kept, o)
		}
	}
	return append(kept, m)
}

// hasControl reports a control character as util.StripControl counts one: C0, DEL and C1. A tab
// is one, so a field holding a tab is refused too.
func hasControl(s string) bool { return util.StripControl(s) != s }

// encodeMade is the file's bytes for ms, one line each.
func encodeMade(ms []Made) []byte {
	var b strings.Builder
	for _, m := range ms {
		b.WriteString(m.Path)
		b.WriteByte('\t')
		b.WriteString(m.Name)
		if m.Store != "" {
			b.WriteByte('\t')
			b.WriteString(m.Store)
		}
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

// capMade drops the oldest lines until at most MadeMaxLines lines and MadeMaxBytes bytes are left.
func capMade(ms []Made) []Made {
	size := len(encodeMade(ms))
	for len(ms) > MadeMaxLines || size > MadeMaxBytes {
		size -= len(encodeMade(ms[:1]))
		ms = ms[1:]
	}
	return ms
}

// RecordMade records in the made-list at file that the wizard made the archive folder path for the
// mailbox whose folder name is name: one line, the newest, replacing any line for the same folder;
// an identical line already there writes nothing. The list keeps at most MadeMaxLines lines and
// MadeMaxBytes bytes, the oldest dropped. It is written owner-only (0600) by writing a new file and
// renaming it over the old one, under the lock beside it (MadeLockPath), and refused through
// RefuseSettingsWrite when the settings folder is inside an encrypted archive. A path that is not
// absolute, and a path or name holding a control character, are refused (the reader would skip
// such a line). Nothing is written in the archive folder itself.
func RecordMade(file, path, name string) error { return RecordMadeStore(file, path, name, "") }

// RecordMadeStore is RecordMade with the mailbox's store id, kept as the line's third field (the
// adversarial pass's OUT-03), so §7's unfinished-folder rule continues the folder only for that
// store. A store id holding a control character is refused, as a name is.
func RecordMadeStore(file, path, name, store string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("%s: %q is not an absolute path, so it was not recorded", madeKind, util.StripControl(path))
	}
	if hasControl(path) || hasControl(name) || hasControl(store) {
		return fmt.Errorf("%s: the folder %q or its mailbox's name holds a control character, so it was not recorded", madeKind, util.StripControl(path))
	}
	m := Made{Path: filepath.Clean(path), Name: name, Store: store}
	if len(encodeMade([]Made{m})) > MadeMaxBytes {
		return fmt.Errorf("%s: the folder's line is over %d bytes, so it was not recorded", madeKind, MadeMaxBytes)
	}
	return updateMade(file, true, func(ms []Made) ([]Made, bool) {
		for _, o := range ms {
			if o == m {
				return ms, false
			}
		}
		return capMade(withMade(ms, m)), true
	})
}

// StrikeMade removes from the made-list at file every line whose folder is one of paths (compared
// as filepath.Clean spells them), under its lock, as RecordMade writes it. A list that names none
// of them is not written, and no settings folder or lock file is made for it.
func StrikeMade(file string, paths []string) error {
	strike := map[string]bool{}
	for _, p := range paths {
		if strings.TrimSpace(p) != "" {
			strike[filepath.Clean(p)] = true
		}
	}
	if len(strike) == 0 {
		return nil
	}
	return updateMade(file, false, func(ms []Made) ([]Made, bool) {
		kept := ms[:0:0]
		for _, m := range ms {
			if !strike[m.Path] {
				kept = append(kept, m)
			}
		}
		return kept, len(kept) != len(ms)
	})
}

// updateMade changes the made-list at file under its lock: change gets the list as read under the
// lock and returns the list to write and whether to write it. create says a missing settings folder
// is made (a record); otherwise a missing folder is an empty list and nothing is changed.
func updateMade(file string, create bool, change func([]Made) ([]Made, bool)) error {
	if err := RefuseSettingsWrite(file, madeKind); err != nil {
		return err
	}
	dir := filepath.Dir(file)
	if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
		if !create {
			return nil
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	lock, err := lockfile.AcquireWait(MadeLockPath(file), "wizard-made", madeLockWait)
	if err != nil {
		if errors.Is(err, lockfile.ErrHeld) {
			return &MadeBusyError{Path: file, Err: err}
		}
		return fmt.Errorf("%s (%s) could not be locked to change it, so nothing was changed: %w", madeKind, util.StripControl(file), err)
	}
	defer lock.Release()
	ms, err := ReadMade(file)
	if err != nil {
		return err
	}
	next, write := change(ms)
	if !write {
		return nil
	}
	if err := lock.StillHeld(); err != nil {
		return err
	}
	return util.WriteFileAtomic0600(file, encodeMade(next))
}
