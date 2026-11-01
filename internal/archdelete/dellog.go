package archdelete

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/util"
)

// LogName is the deletion log's name in MailArchive's settings folder (§5.4): a
// journal for recovery, removed by uninstall (uninstall.ConfigTargets).
const LogName = "deletions.log"

// DefaultLogPath is deletions.log in MailArchive's settings folder (job.Dir()).
func DefaultLogPath() (string, error) {
	d, err := job.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, LogName), nil
}

// logCap bounds a read of deletions.log: three or four short lines per delete.
const logCap = 32 << 20

// The log's events (§5.3 steps 0, 7, 8, 10).
const (
	evStarted       = "started"
	evFolderRemoved = "folder removed"
	evFolderKept    = "folder kept"
	evKeyForgotten  = "key forgotten"
	evFinished      = "finished"
)

// newRunID is a delete's own id, written on each of its lines.
func newRunID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("t%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

// The finished line's outcomes.
const (
	outComplete = "complete"
	outPartial  = "partial"
	outRefused  = "refused"
)

// logLine is one line of deletions.log: one JSON object, so a folder name holding a
// line break or a quote stays one line and reads back whole.
type logLine struct {
	At    string `json:"at"`
	Event string `json:"event"`
	// Run is the delete's own id, on every line it writes: a folder, key or finished
	// line belongs to its own started line, so two deletes of one folder that overlap,
	// or a folder named by another spelling than the row's, keep their state apart
	// (del-s1b-A2-02, A2-03, A3-02). A line without one (none is written so) belongs to
	// the folder's last delete.
	Run    string `json:"run,omitempty"`
	Folder string `json:"folder"`
	Row    string `json:"row,omitempty"`
	// Identity (started) is the folder's identity at the plan, or for a re-run of a
	// folder gone the original's: an unfinished delete is the folder's only while the
	// folder there now is that folder (del-s1b-A1-01, A1-06, A2-04, A3-03).
	Identity *Identity `json:"identity,omitempty"`
	// KeyForgotten (started) marks a re-run of a delete whose step 8 already forgot the
	// archive's record (del-s1b-A2-01).
	KeyForgotten bool `json:"key_forgotten,omitempty"`

	// started: the plan's counts, §4.3's choice, the kind, and an encrypted archive's
	// record as the plan found it (or "not recorded") [R3:NF-NF2-RERUN,
	// R4:NF-R4-RERUN-PLAIN]. Rerun marks a §5.4 re-run's own started line, which
	// carries the original's record and folder-removed state forward.
	Kind             string `json:"kind,omitempty"`
	Files            *int   `json:"files,omitempty"`
	Bytes            *int64 `json:"bytes,omitempty"`
	Unrecorded       *int   `json:"unrecorded,omitempty"`
	DeleteUnrecorded string `json:"delete_unrecorded,omitempty"`
	RecordID         string `json:"record_id,omitempty"`
	RecordSpelling   string `json:"record_spelling,omitempty"`
	NotRecorded      bool   `json:"not_recorded,omitempty"`
	Rerun            string `json:"rerun,omitempty"`

	// finished: the outcome, whether the row is left "Delete not finished" (a partial
	// written after step 2 began, §5.4), what was left and kept, and the deletion
	// record's digest [O2-03].
	Outcome      string `json:"outcome,omitempty"`
	Unfinished   bool   `json:"unfinished,omitempty"`
	Left         *int   `json:"left,omitempty"`
	Kept         *int   `json:"kept,omitempty"`
	Detail       string `json:"detail,omitempty"`
	RecordSHA256 string `json:"record_sha256,omitempty"`
}

// appendLog appends one line to the log at path and flushes it to disk before
// returning (§5.3 step 0 "append and fsync"): owner-only (0600, as util's secure
// files are, an existing file made so), never through a link, never a file other
// than a regular one.
func appendLog(path string, l logLine) error {
	data, err := json.Marshal(l)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	_, statErr := os.Lstat(path)
	if statErr == nil {
		if fi, _ := os.Lstat(path); !fi.Mode().IsRegular() {
			return fmt.Errorf("%s is not a regular file", path)
		}
	} else if !errors.Is(statErr, fs.ErrNotExist) {
		return statErr
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_APPEND|os.O_CREATE|logNoFollow, 0o600)
	if err != nil {
		return err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	if !fi.Mode().IsRegular() {
		f.Close()
		return fmt.Errorf("%s is not a regular file", path)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm()&0o077 != 0 {
		if err := f.Chmod(0o600); err != nil {
			f.Close()
			return err
		}
	}
	// A line torn by a power cut has no line break: this line starts on a line of its
	// own, so it is not joined to the fragment and lost with it (del-s1b-A2-05).
	if n := fi.Size(); n > 0 {
		last := make([]byte, 1)
		if _, err := f.ReadAt(last, n-1); err != nil {
			f.Close()
			return err
		}
		if last[0] != '\n' {
			data = append([]byte{'\n'}, data...)
		}
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if statErr != nil {
		util.SyncDir(dir) // a new file: its name in the folder survives power loss too
	}
	return nil
}

// readLog reads every line of the log; an absent log has none. A line that does
// not parse is passed over.
func readLog(path string) ([]logLine, error) {
	data, err := util.ReadSecureFile(path, "MailArchive's deletion log", logCap)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []logLine
	for _, b := range bytes.Split(data, []byte("\n")) {
		var l logLine
		if len(bytes.TrimSpace(b)) == 0 || json.Unmarshal(b, &l) != nil || l.Event == "" {
			continue
		}
		out = append(out, l)
	}
	return out, nil
}

// Unfinished is a delete of a folder that did not finish (§5.4): a started line
// with no finished line (a crash, a power cut), or a finished partial written after
// step 2 began. Its row shows "Delete not finished: click Delete again" in place of
// Switch, Run now and Archive now.
type Unfinished struct {
	// Folder and Row are the folder as the started line names it and the row.
	Folder string
	Row    string
	// FolderRemoved: the last folder line is `folder removed`.
	FolderRemoved bool
	Kind          Kind
	// Record is the started line's record of an encrypted archive; RecordKnown says
	// the line gives its ID and Spelling, or says "not recorded".
	Record      RecordRef
	RecordKnown bool
	Started     time.Time
	// Identity is the folder's identity as the started line records it (zero when it
	// records none); KeyForgotten: the delete's step 8 forgot the archive's record (its
	// `key forgotten` line, or a re-run's started line carrying it forward).
	Identity     Identity
	KeyForgotten bool
}

// UnfinishedDelete reads the log at logPath for folder's last delete that did not
// finish: nil when there is none. A delete is found by its started line, which names
// the folder or the row (by file identity when both can be read, else by spelling,
// folded where goos's file system ignores case); its later lines are its own by the
// delete's run id, so another delete of the folder that overlaps it or was refused
// (refused: it changed nothing) leaves its state alone, and only a delete that
// finished (complete, or not fully deleted with the row removed) ends the earlier
// ones (del-s1b-A2-02, A3-02, conformance 1). A delete is not this folder's when a
// folder is there now that is not the one it deleted: another identity than its
// started line records, or any folder after its `folder removed` line — a new
// folder, as planUnfinished says (del-s1b-A1-01, A1-06, A2-04, A3-03).
func UnfinishedDelete(logPath, folder, goos string) (*Unfinished, error) {
	if goos == "" {
		goos = runtime.GOOS
	}
	lines, err := readLog(logPath)
	if err != nil {
		return nil, err
	}
	names := func(l logLine) bool {
		return sameFolder(l.Folder, folder, goos) || (l.Row != "" && sameFolder(l.Row, folder, goos))
	}
	var open []*Unfinished
	byRun := map[string]*Unfinished{}
	latest := func() *Unfinished {
		if len(open) == 0 {
			return nil
		}
		return open[len(open)-1]
	}
	isOpen := func(u *Unfinished) bool {
		for _, o := range open {
			if o == u {
				return true
			}
		}
		return false
	}
	for _, l := range lines {
		if l.Event == evStarted {
			if !names(l) {
				continue
			}
			u := &Unfinished{Folder: l.Folder, Row: l.Row, Kind: kindOf(l.Kind), KeyForgotten: l.KeyForgotten}
			u.Started, _ = time.Parse(time.RFC3339Nano, l.At)
			if l.Identity != nil {
				u.Identity = *l.Identity
			}
			switch {
			case l.RecordID != "" && l.RecordSpelling != "":
				u.Record = RecordRef{Recorded: true, ID: l.RecordID, Spelling: l.RecordSpelling}
				u.RecordKnown = true
			case l.NotRecorded:
				u.RecordKnown = true
			}
			// A re-run's own started line keeps the state it re-runs: the folder was
			// removed by the delete it finishes.
			if l.Rerun == rerunFolderGoneWord {
				u.FolderRemoved = true
				if prev := latest(); prev != nil {
					u.FolderRemoved = prev.FolderRemoved
				}
			}
			open = append(open, u)
			if l.Run != "" {
				byRun[l.Run] = u
			}
			continue
		}
		var u *Unfinished
		if l.Run != "" {
			u = byRun[l.Run]
		} else if names(l) {
			u = latest()
		}
		if u == nil || !isOpen(u) {
			continue
		}
		switch l.Event {
		case evFolderRemoved, evFolderKept:
			u.FolderRemoved = l.Event == evFolderRemoved
		case evKeyForgotten:
			u.KeyForgotten = true
		case evFinished:
			switch {
			case l.Outcome == outPartial && l.Unfinished:
				// the row stays "Delete not finished"
			case l.Outcome == outRefused:
				for i, o := range open {
					if o == u {
						open = append(open[:i], open[i+1:]...)
						break
					}
				}
			default:
				open = nil
			}
		}
	}
	now, there := folderIdentity(folder)
	for i := len(open) - 1; i >= 0; i-- {
		u := open[i]
		if there && (u.FolderRemoved || (u.Identity != Identity{} && u.Identity != now)) {
			continue
		}
		return u, nil
	}
	return nil, nil
}

// rerunFolderGoneWord marks a folder-gone re-run's started line.
const rerunFolderGoneWord = "folder gone"

func kindOf(s string) Kind {
	switch s {
	case Plain.String():
		return Plain
	case Encrypted.String():
		return Encrypted
	}
	return 0
}

func intp(n int) *int       { return &n }
func int64p(n int64) *int64 { return &n }
