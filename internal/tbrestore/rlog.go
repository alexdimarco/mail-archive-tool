package tbrestore

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"mail-archive-tool/internal/util"
)

// restores.log (design §7.4 as folded): one JSON line per step of each restore, keyed
// by the restore's ID, written with the deletion log's append discipline
// (util.AppendLogLine): owner-only, never through a link, fsynced after every line, a
// torn last line kept apart.

// The events, in the order a restore writes them (§7.4 notes).
const (
	EvStarted   = "started"   // before the first create
	EvName      = "name"      // the first name was taken at Mkdir; the one used
	EvChecked   = "checked"   // the post-write check passed
	EvTaken     = "taken"     // the parent file's name was taken at the commit
	EvEnded     = "ended"     // closing: finished, or its own cleanup removed everything
	EvCleaned   = "cleaned"   // closing: a later Start removed its leftover
	EvAbandoned = "abandoned" // closing: a cleanup removed nothing (it is left whole)
)

// The cap of a read of the log, and the room a started line leaves under it for its
// restore's later lines, as the deletion log keeps (archdelete logCap, logReserve).
const (
	logCap     = 32 << 20
	logReserve = 1 << 20
)

// LogLine is one line of restores.log.
type LogLine struct {
	At    string `json:"at"`
	Event string `json:"event"`
	ID    string `json:"id"`
	// started: the profile (its key and folder), the Local Folders path and identity, the
	// archive folder, the container's parent name chosen, whether the archive is
	// encrypted, and the planned counts. No mail folder name for an encrypted archive
	// (§6.2): the parent name holds only the archive folder's own name.
	Profile    string    `json:"profile,omitempty"`
	ProfileDir string    `json:"profile_dir,omitempty"`
	Local      string    `json:"local,omitempty"`
	LocalID    *Identity `json:"local_id,omitempty"`
	Archive    string    `json:"archive,omitempty"`
	Name       string    `json:"name,omitempty"` // started, name: "Restored — X" or "Restored — X (n)"
	Encrypted  bool      `json:"encrypted,omitempty"`
	Messages   *int      `json:"messages,omitempty"`
	Folders    *int      `json:"folders,omitempty"`
	Bytes      *int64    `json:"bytes,omitempty"`
	// closing lines: the result and what was restored.
	Result   string `json:"result,omitempty"`
	Detail   string `json:"detail,omitempty"`
	Restored *int   `json:"restored,omitempty"`
}

// NewID is a restore's ID: 128 random bits in hex (§3.4 note), on its marker and every
// log line.
func NewID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// Log is restores.log at a path (LogPath in production; a temp file in tests).
type Log struct {
	Path string
	now  func() time.Time
}

// NewLog is the log at path.
func NewLog(path string) *Log { return &Log{Path: path, now: time.Now} }

// Append writes one line, fsynced. A started line that would leave less than the
// reserve under the read cap is refused, W-LOGFULL, so the log never grows past what
// MailArchive reads.
func (l *Log) Append(line LogLine) error {
	if line.At == "" {
		line.At = l.now().UTC().Format(time.RFC3339Nano)
	}
	data, err := json.Marshal(line)
	if err != nil {
		return err
	}
	return util.AppendLogLine(l.Path, data, func(size, n int64) error {
		if line.Event == EvStarted && size+n+1+logReserve > logCap {
			return refuse("W-LOGFULL", fill(wLogFull, "FOLDER", filepath.Dir(l.Path)), nil)
		}
		return nil
	})
}

// errTorn marks a line cut short by a power cut.
var errTorn = errors.New("torn")

// Read reads every line. An absent log has none. A line cut short (a JSON object that
// ends early: a power cut mid-append) is passed over, wherever it is — the append
// discipline starts the next line on a line of its own, so a torn line can sit in the
// middle. Any other line that does not parse, or a log that cannot be read at all,
// refuses: W-LOGBAD, since whether an earlier restore left anything could not be told.
func (l *Log) Read() ([]LogLine, error) {
	data, err := util.ReadSecureFile(l.Path, "MailArchive's restore record", logCap)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, refuse("W-LOGBAD", fill(wLogBad, "FOLDER", filepath.Dir(l.Path)), err)
	}
	var out []LogLine
	for n, b := range bytes.Split(data, []byte("\n")) {
		if len(bytes.TrimSpace(b)) == 0 {
			continue
		}
		var ln LogLine
		if err := json.Unmarshal(b, &ln); err != nil {
			if torn(b, err) {
				continue
			}
			return nil, refuse("W-LOGBAD", fill(wLogBad, "FOLDER", filepath.Dir(l.Path)), fmt.Errorf("line %d: %w", n+1, err))
		}
		if ln.Event == "" || ln.ID == "" {
			return nil, refuse("W-LOGBAD", fill(wLogBad, "FOLDER", filepath.Dir(l.Path)), fmt.Errorf("line %d: no event or id", n+1))
		}
		out = append(out, ln)
	}
	return out, nil
}

// torn reports a line that is the start of a JSON object cut short.
func torn(b []byte, err error) bool {
	var se *json.SyntaxError
	return bytes.HasPrefix(bytes.TrimSpace(b), []byte("{")) && errors.As(err, &se) && strings.Contains(err.Error(), "unexpected end of JSON input")
}

// Restore is one restore as the log tells it.
type Restore struct {
	Started LogLine
	// Name is the parent name it used: the latest "name" line's, else the started line's.
	Name    string
	Checked bool
	Taken   bool
	// Closed is its closing line's event ("ended", "cleaned", "abandoned"), "" while open.
	Closed string
	// closingResult is the closing line's Result.
	closingResult string
}

// Container is the restore's container name, "Restored — X.sbd".
func (r Restore) Container() string { return r.Name + ".sbd" }

// Restores groups the log's lines by restore ID, in the order they started. Lines for
// an ID with no started line are passed over.
func Restores(lines []LogLine) []*Restore {
	var out []*Restore
	by := map[string]*Restore{}
	for _, ln := range lines {
		if ln.Event == EvStarted {
			if _, dup := by[ln.ID]; dup {
				continue
			}
			r := &Restore{Started: ln, Name: ln.Name}
			by[ln.ID] = r
			out = append(out, r)
			continue
		}
		r := by[ln.ID]
		if r == nil {
			continue
		}
		switch ln.Event {
		case EvName:
			if ln.Name != "" {
				r.Name = ln.Name
			}
		case EvChecked:
			r.Checked = true
		case EvTaken:
			r.Taken = true
		case EvEnded, EvCleaned, EvAbandoned:
			if r.Closed == "" {
				r.Closed, r.closingResult = ln.Event, ln.Result
			}
		}
	}
	return out
}

// Open are the restores into the profile (its key) with no closing line.
func Open(rs []*Restore, profileKey string) []*Restore {
	var out []*Restore
	for _, r := range rs {
		if r.Closed == "" && r.Started.Profile == profileKey {
			out = append(out, r)
		}
	}
	return out
}

func intp(n int) *int       { return &n }
func int64p(n int64) *int64 { return &n }
