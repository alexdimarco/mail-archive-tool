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
	"strings"
	"sync"
	"time"

	"mail-archive-tool/internal/delmark"
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

// logReserve is the room a delete's started line leaves under logCap for that delete's later
// lines (a few short lines each), so MailArchive never writes the log past what it reads.
const logReserve = 1 << 20

// The log's events (§5.3 steps 0, 7, 8, 10; `marked` is step 6's, §15.2 DM-27: the delete
// marker created, or this delete's own found, just before the first removal).
const (
	evStarted       = "started"
	evMarked        = "marked"
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
	// FirstRun and FirstAt (started) are the delete this one finishes, as its first run's id and
	// started time; EarlierFiles and EarlierBytes what its runs removed before this one, and
	// EarlierUnknown that one of them stopped with no finished line, so its count is not known
	// (the 1.5.3 friction review's D1: the record of the run that finishes a delete names the
	// first and carries its totals).
	FirstRun       string `json:"first_run,omitempty"`
	FirstAt        string `json:"first_at,omitempty"`
	EarlierFiles   *int   `json:"earlier_files,omitempty"`
	EarlierBytes   *int64 `json:"earlier_bytes,omitempty"`
	EarlierUnknown bool   `json:"earlier_unknown,omitempty"`
	// FromMarker (started): the delete this run finishes was planned from the folder's marker, no line
	// of this log knowing its start (§15.4): carried forward, so a later run's plan keeps only
	// MailArchive's own names (DM-19) and the record's labels (DM-8).
	FromMarker bool `json:"from_marker,omitempty"`
	// Captured (started) is what the deletion record says of the archive, as the first run of the
	// delete read it while the archive was whole, carried forward by each run that finishes it: a
	// later run finds the history and the manifest the first one removed gone (the 1.5.3 re-walk's
	// N3, N5). Never an encrypted archive's sealed values: only its last backup and where its run
	// history begins, which are not sealed.
	Captured *loggedCapture `json:"captured,omitempty"`

	// finished: the outcome, whether the row is left "Delete not finished" (a partial
	// written after step 2 began, §5.4), what was left and kept, and the deletion
	// record's digest [O2-03].
	Outcome      string `json:"outcome,omitempty"`
	Unfinished   bool   `json:"unfinished,omitempty"`
	Left         *int   `json:"left,omitempty"`
	Kept         *int   `json:"kept,omitempty"`
	Detail       string `json:"detail,omitempty"`
	RecordSHA256 string `json:"record_sha256,omitempty"`
	// Removed and RemovedBytes (finished) are the archive's files this run removed (D1).
	Removed      *int   `json:"removed,omitempty"`
	RemovedBytes *int64 `json:"removed_bytes,omitempty"`
}

// errLogFull is appendLog's refusal of a started line the log has no room for.
var errLogFull = errors.New("the deletion log is as large as MailArchive reads")

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
	// A new delete starts only while the log keeps room for its lines under what readLog
	// reads; past that, every read of the log — a row's, the plan's, uninstall's — would fail
	// and lose the unfinished deletes it holds (core-plan-record finding 4).
	if l.Event == evStarted && fi.Size()+int64(len(data))+1+logReserve > logCap {
		f.Close()
		return errLogFull
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

// Why a deletion log could not be read, beside util.ErrReadableByOthers and fs.ErrPermission, so
// a caller can say what to do about it (uninstall, del-s5 re-verification residual 5): a log
// larger than MailArchive reads, and a name that is not an ordinary file (a link, a folder, a
// device). errors.Is finds them in readLog's errors; the error's text is unchanged.
var (
	ErrLogTooLarge   = errors.New("the deletion log is larger than MailArchive reads")
	ErrLogNotRegular = errors.New("the deletion log is not an ordinary file")
)

// logReadError is a read of the log that failed, with its kind when one is known.
type logReadError struct{ err, kind error }

func (e *logReadError) Error() string   { return e.err.Error() }
func (e *logReadError) Unwrap() []error { return []error{e.err, e.kind} }

// readLog reads every line of the log; an absent log has none. A line that does
// not parse is passed over.
func readLog(path string) ([]logLine, error) {
	data, err := readLogData(path)
	if err != nil {
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

// readLogData reads the log's bytes as readLog does; an absent log is empty.
func readLogData(path string) ([]byte, error) {
	data, err := util.ReadSecureFile(path, "MailArchive's deletion log", logCap)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		if fi, lerr := os.Lstat(path); lerr == nil {
			switch {
			case !fi.Mode().IsRegular():
				return nil, &logReadError{err, ErrLogNotRegular}
			case fi.Size() > logCap:
				return nil, &logReadError{err, ErrLogTooLarge}
			}
		}
		return nil, err
	}
	return data, nil
}

// LogHoldsLines says whether the log at path holds any line at all, blank lines aside, whether
// or not MailArchive can parse it: uninstall -yes asks it before removing the log (the 1.5.3
// friction review's D2). An absent log holds none; one that cannot be read is an error, as
// readLog's.
func LogHoldsLines(path string) (bool, error) {
	data, err := readLogData(path)
	if err != nil {
		return false, err
	}
	return len(bytes.TrimSpace(data)) > 0, nil
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
	// Run is the started line's run id; FirstRun and FirstStarted the first run of the delete it
	// belongs to (itself, unless it finishes an earlier one). Removed and RemovedBytes are what
	// every run of that delete removed so far; RemovedKnown is false when one of them stopped with
	// no finished line, so its count is not known (D1).
	Run          string
	FirstRun     string
	FirstStarted time.Time
	Removed      int
	RemovedBytes int64
	RemovedKnown bool
	// Captured is what the first run of the delete read of the archive (its started line's
	// captured values, carried forward), or nil when the log has none (an older line).
	Captured *Captured
	// Marker is what is at the delete marker's name in the folder (§15.3; UnfinishedAt asks it,
	// UnfinishedDelete leaves it Absent). MarkerOnly: no line of this computer's deletion log
	// knows the delete (or the log could not be read), only the marker; Folder and Row are then
	// the folder asked about. MarkerDetails are the marker's run and began, for words only (a
	// regular marker's bounded read; DM-8).
	Marker        delmark.State
	MarkerOnly    bool
	MarkerDetails delmark.Details
	// LogUnreadable is why this computer's deletion log could not be read when the marker answered
	// in its place (MarkerOnly), in words fit to show; "" when the log was read or none was asked.
	// The log may then hold the delete's started line, so no words say it "has no record of it"
	// (the slice-2 adversarial pass's LOW-4 (a)).
	LogUnreadable string
	// FromMarker: the delete was planned from the folder's marker, no line of this computer's log
	// knowing its start (§15.4): the plan's own Finishes, or a started line that carries it
	// (`from_marker`) for a run of this computer that finishes such a delete. Its plan removes only
	// MailArchive's own names when the anchor is gone (DM-19), and its record's lines from the marker
	// say so (DM-8).
	FromMarker bool
	// Marked: a run of this delete (one whose started line names its first run, FirstRun) wrote its
	// `marked` line — it created the folder's marker at step 6, or found this delete's own there
	// (DM-27). With the anchor and the marker both gone, step 6 had finished (DeletePlan.NoteGone;
	// the slice-4 adversarial pass's MED-1).
	Marked bool
	// counted: this run's own finished line was read.
	counted bool
}

// loggedCapture is Captured as a started line keeps it: each time in RFC 3339, "" when not known.
type loggedCapture struct {
	Known      bool   `json:"known,omitempty"`
	Messages   int    `json:"messages,omitempty"`
	MailFrom   string `json:"mail_from,omitempty"`
	MailTo     string `json:"mail_to,omitempty"`
	FirstRun   string `json:"first_run_at,omitempty"`
	LastBackup string `json:"last_backup,omitempty"`
	OldestRun  string `json:"oldest_run,omitempty"`
}

// loggedCaptureOf is c as a started line keeps it for an archive of kind k, nil when there is
// nothing to keep. An encrypted archive's message count, mail dates and first run are sealed, so
// they never reach the log, which is a plain file: only its last backup and where its run history
// begins, read from its plain run history.
func loggedCaptureOf(c Captured, k Kind) *loggedCapture {
	at := func(t time.Time) string {
		if t.IsZero() {
			return ""
		}
		return t.UTC().Format(time.RFC3339Nano)
	}
	l := loggedCapture{LastBackup: at(c.LastBackup), OldestRun: at(c.OldestRun)}
	if k != Encrypted && c.Known {
		l.Known, l.Messages, l.MailFrom, l.MailTo, l.FirstRun = true, c.Messages, at(c.MailFrom), at(c.MailTo), at(c.FirstRun)
	}
	if l == (loggedCapture{}) {
		return nil
	}
	return &l
}

// captured is l read back.
func (l *loggedCapture) captured() Captured {
	at := func(s string) time.Time {
		t, _ := time.Parse(time.RFC3339Nano, s)
		return t
	}
	return Captured{Known: l.Known, Messages: l.Messages, MailFrom: at(l.MailFrom), MailTo: at(l.MailTo),
		FirstRun: at(l.FirstRun), LastBackup: at(l.LastBackup), OldestRun: at(l.OldestRun)}
}

// withFirstRun is own, the values a plan read now, with those the first run of the delete it
// finishes read (first) filling what own cannot read: the history, the run history and, for a
// folder an earlier run removed, the manifest may be gone by now (N3, N5). What the archive still
// records wins (the settle pass's MED-1: a delete stopped before it removed anything, and a backup
// ran into the archive since): the message count and mail dates are own's when own read them (the
// manifest is removed last, so while it is there it is the archive's own), the last backup is the
// later of the two, and the first run and where the run history begins the earlier of the two.
func withFirstRun(own, first Captured) Captured {
	out := own
	if !own.Known && first.Known {
		out.Known, out.Messages, out.MailFrom, out.MailTo = true, first.Messages, first.MailFrom, first.MailTo
	}
	pick := func(to *time.Time, from time.Time, later bool) {
		if from.IsZero() {
			return
		}
		if to.IsZero() || (later && from.After(*to)) || (!later && from.Before(*to)) {
			*to = from
		}
	}
	pick(&out.LastBackup, first.LastBackup, true)
	pick(&out.FirstRun, first.FirstRun, false)
	pick(&out.OldestRun, first.OldestRun, false)
	return out
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
	l, err := ReadDeleteLog(logPath)
	if err != nil {
		return nil, err
	}
	return l.Unfinished(folder, goos), nil
}

// DeleteLog is deletions.log read once, for a caller that asks it of many folders: a page of
// Your archives (del-s3a-A3-04, A4-05). Asking UnfinishedDelete for each row read the log again
// for each and took the file identity of every folder a started line names, for each row — a
// network folder deleted long ago and offline since (\\nas\share\Mail, a mapped drive letter
// gone) was then asked of the network rows × lines times on every load, each ask able to wait
// for the network's time-out on Windows. Read once, a log in which every delete finished is
// answered without asking the file system anything; otherwise each path is asked once for the
// DeleteLog's life (a page load: a snapshot).
type DeleteLog struct {
	lines []logLine
	// pending: some delete in the log may not have finished — a started line whose own run has
	// no finished line that ends it (a crash, a power cut, a partial written after step 2
	// began), or a started line with no run id (none is written so).
	pending bool
	stat    *statMemo
}

// ReadDeleteLog reads the log at path once; an absent log has no lines.
func ReadDeleteLog(path string) (*DeleteLog, error) {
	lines, err := readLog(path)
	if err != nil {
		return nil, err
	}
	return newDeleteLog(lines, newStatMemo()), nil
}

func newDeleteLog(lines []logLine, stat *statMemo) *DeleteLog {
	l := &DeleteLog{lines: lines, stat: stat}
	// A run whose own finished line ends it (any outcome but a partial that leaves the row
	// unfinished) is out of UnfinishedDelete's open list by the end of the log, whatever
	// folder it names: refused removes it, any other ends every open delete of its folder.
	ended := map[string]bool{}
	for _, ln := range lines {
		if ln.Event == evFinished && ln.Run != "" && !(ln.Outcome == outPartial && ln.Unfinished) {
			ended[ln.Run] = true
		}
	}
	for _, ln := range lines {
		if ln.Event == evStarted && (ln.Run == "" || !ended[ln.Run]) {
			l.pending = true
			break
		}
	}
	return l
}

// PendingDeletes are the deletes in the log at logPath that are under way or did not finish
// (§5.4), by the row each names (its folder when it names none), for uninstall: removing the
// log under a running delete loses its started line (its later lines would make the file
// again), and removing it under an unfinished one leaves that delete with no way to finish —
// Plan's §5.4 re-run reads only this log (del-s5-A1-06, A2-02). An absent log has none.
func PendingDeletes(logPath, goos string) ([]string, error) {
	l, err := ReadDeleteLog(logPath)
	if err != nil {
		return nil, err
	}
	return l.Pending(goos), nil
}

// Pending is PendingDeletes against this read of the log: each started line's folder whose
// delete Unfinished still finds (a folder made again at the path since is a new folder, not
// that delete's), once per folder. A started line naming no folder names nothing to finish.
func (dl *DeleteLog) Pending(goos string) []string {
	var out []string
	for _, p := range dl.PendingKinds(goos) {
		out = append(out, p.Row)
	}
	return out
}

// PendingDelete is a delete under way or not finished: the row it names (its folder when it names
// none) and whether its started line says an encrypted archive — or says no kind, so it may be one
// (uninstall's refusal says a key stays remembered only then; the 1.5.3 re-walk's N9).
type PendingDelete struct {
	Row       string
	Encrypted bool
}

// PendingDeleteKinds is PendingDeletes with each delete's kind.
func PendingDeleteKinds(logPath, goos string) ([]PendingDelete, error) {
	l, err := ReadDeleteLog(logPath)
	if err != nil {
		return nil, err
	}
	return l.PendingKinds(goos), nil
}

// PendingKinds is Pending with each delete's kind.
func (dl *DeleteLog) PendingKinds(goos string) []PendingDelete {
	if dl == nil || !dl.pending {
		return nil
	}
	var out []PendingDelete
	seen := map[string]bool{}
	for _, l := range dl.lines {
		if l.Event != evStarted || strings.TrimSpace(l.Folder) == "" || seen[l.Folder] {
			continue
		}
		seen[l.Folder] = true
		if u := dl.Unfinished(l.Folder, goos); u != nil {
			name := u.Row
			if name == "" {
				name = u.Folder
			}
			if !seen["\x00"+name] {
				seen["\x00"+name] = true
				out = append(out, PendingDelete{Row: name, Encrypted: u.Kind != Plain})
			}
		}
	}
	return out
}

// Unfinished is UnfinishedDelete of folder against this read of the log.
func (dl *DeleteLog) Unfinished(folder, goos string) *Unfinished {
	if dl == nil || !dl.pending {
		return nil
	}
	if goos == "" {
		goos = runtime.GOOS
	}
	lines := dl.lines
	stat := dl.stat.stat
	names := func(l logLine) bool {
		return sameFolderStat(stat, l.Folder, folder, goos) || (l.Row != "" && sameFolderStat(stat, l.Row, folder, goos))
	}
	var open []*Unfinished
	byRun := map[string]*Unfinished{}
	// markedFirst: the first runs of the deletes of this folder one of whose runs wrote a `marked`
	// line (Unfinished.Marked).
	markedFirst := map[string]bool{}
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
			u := &Unfinished{Folder: l.Folder, Row: l.Row, Kind: kindOf(l.Kind), KeyForgotten: l.KeyForgotten, Run: l.Run, FromMarker: l.FromMarker}
			u.Started, _ = time.Parse(time.RFC3339Nano, l.At)
			// The delete it belongs to (D1): an earlier one it finishes, with what that removed,
			// or itself.
			u.FirstRun, u.FirstStarted, u.RemovedKnown = l.Run, u.Started, true
			if l.FirstRun != "" {
				u.FirstRun = l.FirstRun
				u.FirstStarted, _ = time.Parse(time.RFC3339Nano, l.FirstAt)
				if l.EarlierFiles != nil {
					u.Removed = *l.EarlierFiles
				}
				if l.EarlierBytes != nil {
					u.RemovedBytes = *l.EarlierBytes
				}
				u.RemovedKnown = !l.EarlierUnknown
			}
			if l.Captured != nil {
				c := l.Captured.captured()
				u.Captured = &c
			}
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
			if l.Rerun == rerunFolderGoneWord || l.Rerun == rerunElsewhereWord {
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
		case evMarked:
			markedFirst[u.FirstRun] = true
		case evKeyForgotten:
			u.KeyForgotten = true
		case evFinished:
			switch {
			case l.Outcome == outPartial && l.Unfinished:
				// the row stays "Delete not finished"; what this run removed counts toward the
				// delete that finishes it (D1); a line with no count (an older one) is not known.
				u.counted = true
				if l.Removed != nil {
					u.Removed += *l.Removed
					if l.RemovedBytes != nil {
						u.RemovedBytes += *l.RemovedBytes
					}
				} else {
					u.RemovedKnown = false
				}
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
	if len(open) == 0 {
		return nil
	}
	now, there := folderIdentity(folder)
	for i := len(open) - 1; i >= 0; i-- {
		u := open[i]
		if there && (u.FolderRemoved || (u.Identity != Identity{} && u.Identity != now)) {
			continue
		}
		if !u.counted {
			u.RemovedKnown = false // it stopped with no finished line: what it removed is not known
		}
		u.Marked = markedFirst[u.FirstRun]
		return u
	}
	return nil
}

// statMemo is os.Stat taken once per path for its life: a page of rows asks the same folders
// (a row's, a logged one's, a protected one's ancestors) many times (del-s3a-A3-04, A4-05).
type statMemo struct {
	mu sync.Mutex
	m  map[string]statResult
}

type statResult struct {
	fi  os.FileInfo
	err error
}

func newStatMemo() *statMemo { return &statMemo{m: map[string]statResult{}} }

// statSeam is the file system's stat as the memo asks it: a test counts the asks.
var statSeam = os.Stat

func (s *statMemo) stat(p string) (os.FileInfo, error) {
	if s == nil {
		return statSeam(p)
	}
	s.mu.Lock()
	r, ok := s.m[p]
	s.mu.Unlock()
	if ok {
		return r.fi, r.err
	}
	fi, err := statSeam(p)
	s.mu.Lock()
	s.m[p] = statResult{fi, err}
	s.mu.Unlock()
	return fi, err
}

// rerunFolderGoneWord marks a folder-gone re-run's started line; rerunElsewhereWord one the person
// said was finished elsewhere (MA-1057), whose folder no `folder removed` line says was removed.
const (
	rerunFolderGoneWord = "folder gone"
	rerunElsewhereWord  = "finished elsewhere"
)

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
