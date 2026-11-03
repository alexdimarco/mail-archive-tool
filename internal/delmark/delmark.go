// Package delmark is the delete marker (docs/design-archive-delete.md §15): the small
// file a delete writes into the archive folder before it removes anything there, and
// removes as the last of the archive's files. Every backup refuses a folder that
// holds it, on any computer and under any account, whatever that computer's
// deletions.log knows. Its PRESENCE is the signal; its content is only for words
// (and, with a second proof, §15.5's run-id route: DM-8).
//
// It is a LEAF, as internal/seal is: the standard library and internal/util only, so
// the engine (internal/app) asks it even where archdelete's check is not installed.
// It holds the name, the refusal kinds and the marker-only words, the one copy of
// each (DM-24). It never writes the marker: that is the delete's (§15.2).
//
// Presence is read FAIL-CLOSED: only a confirmed "not there" is Absent; a regular
// file (DM-20: util.DescriptorReadable and not util.IsLinkLike, so a Windows cloud
// placeholder counts) is Regular whatever it holds; a link, a folder, a device or a
// pipe is NotRegular and is never followed or opened. A lookup that fails otherwise
// is Unknown with a *LookupError, unless the folder itself cannot be reached (a dead
// drive or share), which is Absent so that the archive-absent refusal speaks (DM-22).
package delmark

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"time"

	"mail-archive-tool/internal/util"
)

// Name is the marker, at the archive folder's root and nowhere else. It is frozen:
// no later version renames it, and every version refuses on its presence (DM-24).
const Name = ".mailarchive-deleting.json"

// The content's fixed fields (§15.1). Version is the only version this package reads.
const (
	What    = "MailArchive delete marker"
	Version = 1
	Note    = "A MailArchive delete of this archive is under way or did not finish. MailArchive does not back up into this folder until the delete is finished: click Delete on its row in MailArchive Desktop."
)

// MaxRead bounds the read of the marker (§15.3): a larger file's details are not known.
const MaxRead = 4 << 10

// The refusal kinds (state.FailDeleteUnfinished, state.FailArchiveUnwritable; a test
// pins them equal, since this leaf cannot import state).
const (
	KindUnfinished    = "delete_unfinished"
	KindCouldNotCheck = "archive_unwritable"
)

// The marker-only words (§15.3, builder's words; DM-11, DM-24). A sink that does not
// show the folder adds " (Folder: DIR)", as for MA-1044's words.
const (
	// MsgOnlyMarker refuses a backup when only the marker knows of the delete.
	MsgOnlyMarker = "Delete not finished: a delete of this archive was begun on another computer or user account, or this computer's deletion log has no record of it, and it did not finish, so it was not backed up; nothing was changed. To finish it, click Delete… on its row in MailArchive Desktop."
	// MsgNotRegular refuses a backup when the name is not a regular file.
	MsgNotRegular = "This archive's folder holds " + Name + ", which MailArchive did not write (it is a link, a folder or a device), so it was not backed up; nothing was changed. If no delete of this archive is under way, remove it by hand, then try again."
	// MsgPlanNotRegular refuses Delete's plan when the name is a folder or a device
	// (a link there is refused by the plan's link row).
	MsgPlanNotRegular = "This archive's folder holds " + Name + ", which MailArchive did not write (it is a folder or a device): remove it by hand, then click Delete again."
	// WhatNotBackedUp is what a backup's "could not check" refusal says was not done.
	WhatNotBackedUp = "it was not backed up"
)

// OnlyMarkerTerminal is the terminal's line after MsgOnlyMarker; dir is the folder as
// the person types it (quoted for their command line).
func OnlyMarkerTerminal(dir string) string {
	return "To finish it at this terminal: mailarchive delete-archive -out " + dir
}

// State is what is at the marker's name.
type State int

const (
	// Absent: confirmed not there (or the folder itself cannot be reached, DM-22).
	Absent State = iota
	// Regular: a marker, whatever it holds.
	Regular
	// NotRegular: a link, a folder, a device or a pipe at the name — still a marker
	// for every refusal, never followed or opened.
	NotRegular
	// Unknown: the lookup failed at a reachable folder; Present returns a *LookupError.
	Unknown
)

func (s State) String() string {
	switch s {
	case Absent:
		return "absent"
	case Regular:
		return "regular"
	case NotRegular:
		return "not regular"
	}
	return "unknown"
}

// LookupError is a lookup of the marker that failed, for a reason other than "not
// there", in a folder that is a reachable folder (DM-22, DM-23): the caller refuses,
// kind KindCouldNotCheck.
type LookupError struct {
	Folder string
	Err    error
}

func (e *LookupError) Error() string {
	return fmt.Sprintf("could not look up %s in %s: %v", Name, e.Folder, e.Err)
}

func (e *LookupError) Unwrap() error { return e.Err }

// Kind is the failure kind of a refusal for this error.
func (e *LookupError) Kind() string { return KindCouldNotCheck }

// Refusal is the "could not check" words (§15.3, DM-23), what saying what was not done
// ("it was not backed up", "it was not paused", …).
func (e *LookupError) Refusal(what string) string {
	cause := e.Err
	var pe *fs.PathError
	if errors.As(cause, &pe) {
		cause = pe.Err
	}
	return fmt.Sprintf("MailArchive could not check this archive's folder for a delete that did not finish (%s), so %s; nothing was changed. Check that the drive or folder can be read, then try again.",
		util.StripBidi(util.StripControl(cause.Error())), what)
}

// lstat is the marker's lookup, a var so a test can make it fail as a dead drive does.
var lstat = os.Lstat

// Present says what is at the marker's name in folder, by a directory lookup that
// needs no read permission on the file and never opens it. An empty folder name is
// no folder: Absent.
func Present(folder string) (State, error) {
	if folder == "" {
		return Absent, nil
	}
	p := filepath.Join(folder, Name)
	fi, err := lstat(p)
	if err == nil {
		return stateOf(runtime.GOOS, p, fi.Mode(), util.IsLinkLike), nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return Absent, nil
	}
	// DM-22: a dead drive or share is not "could not check" but "not there".
	if dfi, derr := os.Stat(folder); derr != nil || !dfi.IsDir() {
		return Absent, nil
	}
	return Unknown, &LookupError{Folder: folder, Err: err}
}

// stateOf is what an entry of mode at path is on goos (DM-20, lister.fileLike's rule):
// regular when util.DescriptorReadable and not a link — on Windows an irregular entry
// is asked linkLike (a junction is, a cloud placeholder is not; a tag that cannot be
// read is not regular: fail closed).
func stateOf(goos, path string, mode fs.FileMode, linkLike func(string) (bool, error)) State {
	if mode&fs.ModeSymlink != 0 || !util.DescriptorReadable(goos, mode) {
		return NotRegular
	}
	if goos == "windows" && mode&fs.ModeIrregular != 0 {
		if link, err := linkLike(path); err != nil || link {
			return NotRegular
		}
	}
	return Regular
}

// Details are what a marker says, for words only (and §15.5's run-id route, with a
// second proof: DM-8). Known is false when the marker is not there, not a regular
// file, unreadable, over MaxRead, or not of Version's shape; then Run and Began are "".
type Details struct {
	Known bool
	// Run is the first run's id: 16 lower-case hex digits.
	Run string
	// Began is when that run started, UTC to the second, as written (the other
	// computer's clock): "2006-01-02T15:04:05Z".
	Began string
}

// BeganAt is Began as a time (the zero time when not known).
func (d Details) BeganAt() time.Time {
	t, _ := time.Parse(beganLayout, d.Began)
	return t
}

const beganLayout = "2006-01-02T15:04:05Z"

var runRe = regexp.MustCompile(`^[0-9a-f]{16}$`)

// ValidRun reports a run id's shape: 16 lower-case hex digits.
func ValidRun(s string) bool { return runRe.MatchString(s) }

// validBegan reports began's shape: a UTC time to the second, nothing else.
func validBegan(s string) bool {
	t, err := time.Parse(beganLayout, s)
	return err == nil && t.Format(beganLayout) == s
}

// Read is the bounded read of folder's marker (§15.3): through an os.Root on the
// folder, only a regular file that is not a link (util.OpenRegularFileIn: no link, no
// pipe, a Windows placeholder accepted), at most MaxRead bytes. Nothing it returns is
// anything but a run id of its shape and a time of its shape.
func Read(folder string) Details {
	if st, err := Present(folder); err != nil || st != Regular {
		return Details{}
	}
	root, err := os.OpenRoot(folder)
	if err != nil {
		return Details{}
	}
	defer root.Close()
	fi, err := root.Lstat(Name)
	if err != nil || stateOf(runtime.GOOS, filepath.Join(folder, Name), fi.Mode(), util.IsLinkLike) != Regular {
		return Details{}
	}
	f, err := util.OpenRegularFileIn(root, Name)
	if err != nil {
		return Details{}
	}
	defer f.Close()
	data, ok := readBounded(f)
	if !ok {
		return Details{}
	}
	return parse(data)
}

// readBounded reads r to its end, at most MaxRead bytes: ok is false when r holds
// more (MaxRead+1 bytes are read to tell) or fails.
func readBounded(r io.Reader) ([]byte, bool) {
	data, err := io.ReadAll(io.LimitReader(r, MaxRead+1))
	if err != nil || len(data) > MaxRead {
		return nil, false
	}
	return data, true
}

// parse is a marker's bytes as Details: one JSON object, nothing after it, of
// Version, with a run and a began of their shapes.
func parse(data []byte) Details {
	var m struct {
		Version *int    `json:"version"`
		Run     *string `json:"run"`
		Began   *string `json:"began"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&m); err != nil {
		return Details{}
	}
	if _, err := dec.Token(); err != io.EOF {
		return Details{}
	}
	if m.Version == nil || *m.Version != Version || m.Run == nil || m.Began == nil || !ValidRun(*m.Run) || !validBegan(*m.Began) {
		return Details{}
	}
	return Details{Known: true, Run: *m.Run, Began: *m.Began}
}
