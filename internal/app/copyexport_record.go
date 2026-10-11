package app

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// The run record, the journal and the fingerprint ledger (design §4.16, §4.19, XC1).
// They live in the local, non-roaming state directory, 0600, append-only, and are
// the app's truth about every run: the export directory is the recipient's, and the
// app reads nothing from it but presence, the marker and identity.

// ceStateErr is a failure to write the local state directory (the run record, the
// journal or the fingerprint ledger). It is its own class (RR2-C7): never matched
// against the destination's errno table, and what stops a run with R-d2.
type ceStateErr struct{ err error }

func (e *ceStateErr) Error() string { return "the local state directory: " + e.err.Error() }
func (e *ceStateErr) Unwrap() error { return e.err }

func stateErr(err error) error {
	if err == nil {
		return nil
	}
	return &ceStateErr{err}
}

// ceFileRec is one file of a message as the run record, the manifest and the sums
// know it.
type ceFileRec struct {
	Path   string `json:"path"` // relative to the export root, forward slashes
	Role   string `json:"role"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// File roles (design §4.10).
const (
	roleOriginal        = "original"
	roleArchivedPage    = "archived-page"
	roleArchivedZip     = "archived-attachments-zip"
	roleDerivedHTML     = "derived-html"
	roleDerivedAttach   = "derived-attachment"
	roleQuarantinedPage = "quarantined-archived-page"
	statusExported      = "exported"
	statusFailed        = "failed"
	statusNotAttempted  = "not_attempted"
	statusWithoutOrig   = "exported-without-original"
	statusOriginalOnly  = "exported-original-only"
	classRetryable      = "retryable"
	classTerminal       = "terminal"
)

// ceHeaderLine is the first line of a run record.
type ceHeaderLine struct {
	T          string           `json:"t"`
	Format     int              `json:"format_version"`
	Run        string           `json:"run"`
	Chain      string           `json:"chain"`
	ChainParts []CopyChainRef   `json:"chain_parts,omitempty"`
	RetryOf    string           `json:"retry_of,omitempty"`
	Host       string           `json:"host"`
	Tool       string           `json:"tool,omitempty"`
	Started    time.Time        `json:"started"`
	Source     CopySourceInfo   `json:"source"`
	Dest       string           `json:"dest"`
	Root       string           `json:"root"`
	RootName   string           `json:"root_name"`
	Identity   ceIdentity       `json:"identity"`
	FSType     string           `json:"fs_type,omitempty"`
	Options    CopyOptions      `json:"options"`
	Folders    []CopyPlanFolder `json:"folders,omitempty"`
	Planned    int              `json:"planned"`
}

// cePlanLine is one planned message.
type cePlanLine struct {
	T string `json:"t"`
	N int    `json:"n"`
	CopyMessage
}

// ceMsgLine is a message's progress line: one fsynced line per finished message
// (design §4.19), written after its files are placed. An Amend line replaces the
// earlier line of the same N (the presence check and Finish as partial amend a
// message the disk no longer backs).
type ceMsgLine struct {
	T      string      `json:"t"`
	N      int         `json:"n"`
	Status string      `json:"status"`
	Class  string      `json:"class,omitempty"`
	Reason string      `json:"reason,omitempty"`
	Files  []ceFileRec `json:"files,omitempty"`
	Notes  []string    `json:"notes,omitempty"`
	Amend  bool        `json:"amend,omitempty"`
}

// ceStoppedLine records why a run stopped before its seal while the app ran on (a
// wedge, a failed finalization, a recovered panic).
type ceStoppedLine struct {
	T      string    `json:"t"`
	RID    string    `json:"rid,omitempty"`
	Class  string    `json:"class,omitempty"`
	Reason string    `json:"reason"`
	At     time.Time `json:"at"`
}

// cePostureLine is the realized posture of the root and the marker (RR2-C6).
type cePostureLine struct {
	T       string   `json:"t"`
	Kind    string   `json:"kind"`
	Readers []string `json:"readers,omitempty"`
	Line    string   `json:"line"`
}

// ceSealedLine is the seal (design §4.13 step 6, §4.19): the run's final status and
// counts, the fingerprint computed from the in-memory SUMS bytes, the stop and the
// chain's set state. It is the first part of the seal-and-finish step.
type ceSealedLine struct {
	T              string     `json:"t"`
	Status         CopyStatus `json:"status"`
	Counts         CopyCounts `json:"counts"`
	Fingerprint    string     `json:"fingerprint"`
	Chain          string     `json:"chain"`
	SetState       string     `json:"set_state,omitempty"`
	SetFingerprint string     `json:"set_fingerprint,omitempty"`
	Parts          []string   `json:"parts,omitempty"`
	Stop           *CopyStop  `json:"stop,omitempty"`
	At             time.Time  `json:"at"`
}

// ceGoneLine is the user's answer "This folder is gone".
type ceGoneLine struct {
	T  string    `json:"t"`
	At time.Time `json:"at"`
}

// ceLocatedLine records a folder the user located at a new path.
type ceLocatedLine struct {
	T        string     `json:"t"`
	Path     string     `json:"path"`
	Identity ceIdentity `json:"identity"`
	At       time.Time  `json:"at"`
}

// ceRecord is a run record open for appending.
type ceRecord struct {
	env  *CopyExportEnv
	path string
	f    *os.File
}

// recordsDir is where the run records live.
func (e *CopyExportEnv) recordsDir() string { return filepath.Join(e.StateDir, "exports") }

func (e *CopyExportEnv) recordPath(run string) string {
	return filepath.Join(e.recordsDir(), run+".jsonl")
}

func (e *CopyExportEnv) journalPath() string {
	return filepath.Join(e.StateDir, "desktop-exports.jsonl")
}
func (e *CopyExportEnv) ledgerPath() string {
	return filepath.Join(e.StateDir, "export-fingerprints.jsonl")
}

// createRecord writes a new run record: its header and the whole plan, fsynced
// before the marker is written (W3: "what was planned" survives any kill). The file
// is created exclusively at 0600.
func (e *CopyExportEnv) createRecord(h ceHeaderLine, plan []CopyMessage) (*ceRecord, error) {
	if err := os.MkdirAll(e.recordsDir(), 0o700); err != nil {
		return nil, stateErr(err)
	}
	path := e.recordPath(h.Run)
	// Append mode where the OS has it: a second handle of this record (WedgeStop's) must
	// write after whatever this one wrote, not over it at a stale offset (EW-AC1).
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|appendFlag, 0o600)
	if err != nil {
		return nil, stateErr(err)
	}
	r := &ceRecord{env: e, path: path, f: f}
	w := bufio.NewWriterSize(f, 256<<10)
	enc := func(v any) error {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		w.Write(b)
		return w.WriteByte('\n')
	}
	h.T, h.Format = "header", copyFormatVer
	werr := enc(h)
	for i := range plan {
		if werr != nil {
			break
		}
		werr = enc(cePlanLine{T: "plan", N: i + 1, CopyMessage: plan[i]})
	}
	if werr == nil {
		werr = w.Flush()
	}
	if werr == nil {
		werr = e.fsync(f)
	}
	if werr != nil {
		f.Close()
		os.Remove(path)
		return nil, stateErr(werr)
	}
	e.noteRecord(h.Chain, h.Run)
	return r, nil
}

// openRecord opens an existing run record for appending, truncating a torn tail
// first (a kill can leave half a line), as the journal does.
func (e *CopyExportEnv) openRecord(run string) (*ceRecord, error) {
	path := e.recordPath(run)
	f, err := openAppendable(path, false)
	if err != nil {
		return nil, stateErr(err)
	}
	return &ceRecord{env: e, path: path, f: f}, nil
}

// openAppendable opens path for appending after cutting a torn tail back to the last
// complete line. The cut is made through a read-write handle and the file is then
// reopened in append mode (a handle opened for append only cannot truncate on Windows),
// and every later write goes to the end of the file whatever other handle has written
// meanwhile (EW-AC1). create makes a missing file.
func openAppendable(path string, create bool) (*os.File, error) {
	flag := os.O_RDWR
	if create {
		flag |= os.O_CREATE
	}
	f, err := os.OpenFile(path, flag, 0o600)
	if err != nil {
		return nil, err
	}
	// Under the write mutex: the cut must not land inside a line another handle of this
	// process is writing (EW-AC1, the window the fresh-eyes check named).
	recordWriteMu.Lock()
	err = truncateTornTail(f)
	recordWriteMu.Unlock()
	f.Close()
	if err != nil {
		return nil, err
	}
	af, err := os.OpenFile(path, os.O_WRONLY|appendFlag, 0o600)
	if err != nil {
		return nil, err
	}
	if _, err := af.Seek(0, io.SeekEnd); err != nil {
		af.Close()
		return nil, err
	}
	return af, nil
}

// recordWriteMu orders the appends of this process to the run record, the journal and the
// ledger. Where the OS has an append mode (every OS but Windows, where an append-only handle
// cannot be flushed with the access Go gives it) the kernel puts each write at the end of
// the file whichever handle makes it; without it, a write seeks to the end first, and this
// mutex keeps two handles from seeking and writing across each other (EW-AC1).
var recordWriteMu sync.Mutex

// writeAtEnd writes b to f at the end of its file, in order with every other append of this
// process.
func writeAtEnd(f *os.File, b []byte) error {
	recordWriteMu.Lock()
	defer recordWriteMu.Unlock()
	if appendFlag == 0 {
		if _, err := f.Seek(0, io.SeekEnd); err != nil {
			return err
		}
	}
	_, err := f.Write(b)
	return err
}

// append writes one line and fsyncs it. A failure is a state-directory error.
func (r *ceRecord) append(v any) error {
	if f := r.env.faults; f != nil && f.appendErr != nil {
		if err := f.appendErr(r.path); err != nil {
			return stateErr(err)
		}
	}
	b, err := json.Marshal(v)
	if err != nil {
		return stateErr(err)
	}
	b = append(b, '\n')
	if err := writeAtEnd(r.f, b); err != nil {
		return stateErr(err)
	}
	return stateErr(r.env.fsync(r.f))
}

func (r *ceRecord) Close() error { return r.f.Close() }

// truncateTornTail cuts a file back to its last complete line, so an append after a
// kill never joins a half line to a whole one; it leaves the file positioned at its
// end.
func truncateTornTail(f *os.File) error {
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	size := fi.Size()
	end := size
	if size > 0 {
		var last [1]byte
		if _, err := f.ReadAt(last[:], size-1); err != nil {
			return err
		}
		if last[0] != '\n' {
			end = 0
			const chunk = 4096
			buf := make([]byte, chunk)
			for off := size; off > 0 && end == 0; {
				n := int64(chunk)
				if off < n {
					n = off
				}
				off -= n
				if _, err := f.ReadAt(buf[:n], off); err != nil && !errors.Is(err, io.EOF) {
					return err
				}
				if i := bytes.LastIndexByte(buf[:n], '\n'); i >= 0 {
					end = off + int64(i) + 1
				}
			}
			if err := f.Truncate(end); err != nil {
				return err
			}
		}
	}
	_, err = f.Seek(end, io.SeekStart)
	return err
}

// scanLines calls fn for each complete line of path; a last line without its
// newline is torn and is skipped. fn gets the line without its newline.
func scanLines(path string, fn func(line []byte) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	br := bufio.NewReaderSize(f, 256<<10)
	for {
		line, err := br.ReadBytes('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil // a torn tail is not a line
			}
			return err
		}
		if err := fn(bytes.TrimRight(line, "\r\n")); err != nil {
			return err
		}
	}
}

// ceRecSummary is what a pass over a run record learns, without holding its plan.
type ceRecSummary struct {
	Header     ceHeaderLine
	Planned    int
	Progress   int // message lines, amendments not counted twice
	Statuses   map[string]int
	Stopped    *ceStoppedLine
	Sealed     *ceSealedLine
	Gone       bool
	Located    []ceLocatedLine
	Realized   *cePostureLine
	NewerMajor bool // a header of a newer format_version
}

// summaryScans counts the full passes readSummary has made over run records: a test's
// measure of how often a page's poll rereads them (EW-AC8).
var summaryScans atomic.Int64

// messageWalks counts the passes walkMessages has made over run records, the other full
// read of a record: a test's measure of what a chain's retry offers cost a poll (EW-AC8).
var messageWalks atomic.Int64

// readSummary reads a run record's header, counts and last stop, seal, gone and
// located lines. A record of a newer format is refused with the update remedy.
func readSummary(path string) (*ceRecSummary, error) {
	summaryScans.Add(1)
	s := &ceRecSummary{Statuses: map[string]int{}}
	last := map[int]string{}
	err := scanLines(path, func(line []byte) error {
		switch lineKind(line) { // a malformed line has no kind: skipped, never reinterpreted
		case "header":
			if err := json.Unmarshal(line, &s.Header); err != nil {
				return err
			}
			if s.Header.Format > copyFormatVer {
				s.NewerMajor = true
			}
		case "plan":
			s.Planned++
		case "msg":
			var m ceMsgLine
			if json.Unmarshal(line, &m) != nil {
				return nil
			}
			if prev, ok := last[m.N]; ok {
				s.Statuses[prev]--
			} else {
				s.Progress++
			}
			last[m.N] = m.Status
			s.Statuses[m.Status]++
		case "stopped":
			var st ceStoppedLine
			if json.Unmarshal(line, &st) == nil {
				s.Stopped = &st
			}
		case "sealed":
			var sl ceSealedLine
			if json.Unmarshal(line, &sl) == nil {
				s.Sealed = &sl
			}
		case "gone":
			s.Gone = true
		case "located":
			var l ceLocatedLine
			if json.Unmarshal(line, &l) == nil {
				s.Located = append(s.Located, l)
			}
		case "posture":
			var p cePostureLine
			if json.Unmarshal(line, &p) == nil {
				s.Realized = &p
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if s.NewerMajor {
		return s, errCopyNewerFormat
	}
	if s.Header.Run == "" {
		return s, fmt.Errorf("%w: the run record %s has no header", ErrRecordDamaged, filepath.Base(path))
	}
	return s, nil
}

// ErrRecordDamaged is a run record that cannot be read (no header, say): the page words it as
// such, with what still works, instead of echoing the error and a file name (friction F20).
var ErrRecordDamaged = errors.New("this export's record is damaged")

// IsRecordDamaged reports whether err is, or wraps, ErrRecordDamaged.
func IsRecordDamaged(err error) bool { return errors.Is(err, ErrRecordDamaged) }

// lineKind is a record line's "t". Every line this package writes starts `{"t":"`, so
// the kind is read without decoding the line (a 200,000-message plan is read often);
// any other line is decoded.
func lineKind(line []byte) string {
	const p = `{"t":"`
	if bytes.HasPrefix(line, []byte(p)) {
		if rest := line[len(p):]; len(rest) > 0 {
			if i := bytes.IndexByte(rest, '"'); i > 0 {
				return string(rest[:i])
			}
		}
	}
	var k struct {
		T string `json:"t"`
	}
	if json.Unmarshal(line, &k) != nil {
		return ""
	}
	return k.T
}

// walkProgress calls fn with each message's effective progress line, in the order the
// lines were written: an amendment replaces the line it amends, and a message with no
// line is not visited. It decodes no plan line, so the passes that need only what was
// written (the presence check, the counts, the sums) stay cheap at 200,000 messages.
func walkProgress(path string, fn func(m *ceMsgLine) error) error {
	amend, err := amendments(path)
	if err != nil {
		return err
	}
	return scanLines(path, func(line []byte) error {
		if !bytes.HasPrefix(line, []byte(`{"t":"msg"`)) {
			return nil
		}
		var m ceMsgLine
		if json.Unmarshal(line, &m) != nil || m.Amend {
			return nil
		}
		if a, ok := amend[m.N]; ok {
			m = a
		}
		return fn(&m)
	})
}

// errCopyNewerFormat is the refusal of a run record made by a newer MailArchive
// Desktop (design §4.19 version rules).
var errCopyNewerFormat = errors.New("made by a newer MailArchive Desktop — update to retry")

// ceStreams walks a run record's plan and progress together, in plan order, without
// holding either: the manifest is streamed from it (XC1). Progress lines are written
// in plan order, and an amendment (which has an earlier N) is collected first so it
// can replace the line it amends.
type ceRecordWalk struct {
	path string
}

// amendments reads the Amend lines of a record: message number to its replacement.
func amendments(path string) (map[int]ceMsgLine, error) {
	out := map[int]ceMsgLine{}
	err := scanLines(path, func(line []byte) error {
		if !bytes.HasPrefix(line, []byte(`{"t":"msg"`)) {
			return nil
		}
		var m ceMsgLine
		if json.Unmarshal(line, &m) == nil && m.Amend {
			out[m.N] = m
		}
		return nil
	})
	return out, err
}

// walkMessages calls fn once per planned message, in plan order, with its plan line
// and its progress line (nil for a message with none: not attempted). Progress lines
// come in plan order, so a single pass over the plan section and one over the
// progress section are merged.
func walkMessages(path string, fn func(p *cePlanLine, m *ceMsgLine) error) error {
	messageWalks.Add(1)
	amend, err := amendments(path)
	if err != nil {
		return err
	}
	pf, err := os.Open(path)
	if err != nil {
		return err
	}
	defer pf.Close()
	mf, err := os.Open(path)
	if err != nil {
		return err
	}
	defer mf.Close()
	pr := bufio.NewReaderSize(pf, 256<<10)
	mr := bufio.NewReaderSize(mf, 256<<10)

	var pending *ceMsgLine
	nextMsg := func() (*ceMsgLine, error) {
		for {
			line, err := mr.ReadBytes('\n')
			if err != nil {
				return nil, nil
			}
			if !bytes.HasPrefix(line, []byte(`{"t":"msg"`)) {
				continue
			}
			var m ceMsgLine
			if json.Unmarshal(line, &m) != nil || m.Amend {
				continue
			}
			return &m, nil
		}
	}
	for {
		line, err := pr.ReadBytes('\n')
		if err != nil {
			return nil // end of the plan section, or a torn tail
		}
		if !bytes.HasPrefix(line, []byte(`{"t":"plan"`)) {
			continue
		}
		var p cePlanLine
		if err := json.Unmarshal(line, &p); err != nil {
			continue
		}
		if pending == nil {
			if pending, err = nextMsg(); err != nil {
				return err
			}
		}
		var m *ceMsgLine
		if pending != nil && pending.N == p.N {
			m, pending = pending, nil
		}
		if a, ok := amend[p.N]; ok {
			a := a
			m = &a
		}
		if err := fn(&p, m); err != nil {
			return err
		}
	}
}

// ---- the journal -----------------------------------------------------------

// ceJournalLine is one line of desktop-exports.jsonl (design §4.16): an index into
// the run records, with no subjects (privacy), at most 4 KB.
type ceJournalLine struct {
	V           int         `json:"v"`
	Host        string      `json:"host"`
	Run         string      `json:"run"`
	Event       string      `json:"event"` // start, stopped, end, reconciled, located
	At          time.Time   `json:"at"`
	SourceKind  string      `json:"source_kind,omitempty"`
	Mailbox     string      `json:"mailbox,omitempty"`
	Archive     string      `json:"archive,omitempty"`
	DestDir     string      `json:"dest_dir,omitempty"`
	RootID      *ceIdentity `json:"root_id,omitempty"`
	Planned     int         `json:"planned,omitempty"`
	Exported    int         `json:"exported,omitempty"`
	Failed      int         `json:"failed,omitempty"`
	Status      string      `json:"status,omitempty"`
	Fingerprint string      `json:"fingerprint,omitempty"`
	Reason      string      `json:"reason,omitempty"`
}

// journalAppend appends one line to the journal after truncating a torn tail,
// under the environment's journal lock, and fsyncs it.
func (e *CopyExportEnv) journalAppend(l ceJournalLine) error {
	l.V, l.Host = 1, e.Host
	if l.At.IsZero() {
		l.At = e.nowTime()
	}
	e.journalMu.Lock()
	defer e.journalMu.Unlock()
	return e.appendJSONL(e.journalPath(), l)
}

// Journal compaction (design §4.16, W12): once the journal passes this many lines it is
// rewritten to keep the last copyKeepRuns runs, and every unresolved run whatever its
// age, which §4.19's Lifetime defines.
var (
	copyJournalCompactAt = 1000
	copyKeepRuns         = 200
)

// CompactJournal rewrites the journal when it has passed copyJournalCompactAt lines: a
// temp file in the state directory, fsynced, renamed over the journal, under the
// journal's mutex, so an append never lands in a file that is being replaced. It keeps
// the last copyKeepRuns runs, every run whose record is unresolved (so it stays on the
// activity page with its actions until it is finished or its folder is said to be gone),
// and every line it cannot read or whose `v` it does not know, copied verbatim and
// never reinterpreted (XC3). A dropped run's record is deleted only after the new
// journal is in place, and only if the run is settled and its chain complete; a record
// left behind by a compaction stopped before its deletions is adopted again by the
// launch sweep and ages out again.
func (e *CopyExportEnv) CompactJournal() error {
	lines, err := e.journalRaw()
	if err != nil || len(lines) <= copyJournalCompactAt {
		return err
	}
	// What to keep is decided from the journal as it stands, before it is rewritten,
	// because a run's end is in the lines compaction drops; and without the journal's
	// lock, because deciding that a record is deletable takes the ledger's lock, which a
	// seal holds while it appends to the journal.
	var order []string
	seen := map[string]bool{}
	for _, l := range lines {
		if run, ok := lineRun(l); ok && !seen[run] {
			seen[run] = true
			order = append(order, run)
		}
	}
	keep := map[string]bool{}
	for i := len(order) - 1; i >= 0 && len(keep) < copyKeepRuns; i-- {
		keep[order[i]] = true
	}
	ledger, _ := e.readLedger()
	var deletable []string
	for _, run := range order {
		if keep[run] {
			continue
		}
		sum, err := readSummary(e.recordPath(run))
		if err == nil && !e.settledRun(sum, ledger) {
			keep[run] = true // unresolved: never dropped
			continue
		}
		if e.recordDeletable(run) {
			deletable = append(deletable, run)
		}
	}
	if err := e.rewriteJournal(seen, keep); err != nil {
		return err
	}
	// The records go only now, with the new journal in place.
	for _, run := range deletable {
		os.Remove(e.recordPath(run))
	}
	return nil
}

// journalRaw is the journal's complete lines, copied.
func (e *CopyExportEnv) journalRaw() ([][]byte, error) {
	var lines [][]byte
	err := scanLines(e.journalPath(), func(line []byte) error {
		lines = append(lines, append([]byte(nil), line...))
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return lines, stateErr(err)
}

// lineRun is the run a journal line of version 1 names; a line of another version, or
// one that is not JSON, names none and is kept verbatim.
func lineRun(l []byte) (string, bool) {
	var j ceJournalLine
	if json.Unmarshal(l, &j) != nil || j.V != 1 || j.Run == "" {
		return "", false
	}
	return j.Run, true
}

// rewriteJournal replaces the journal, under its mutex, by the lines compaction keeps: a
// line that names no run, a run it decided to keep, and any run it had not seen (one
// that began after the decision).
func (e *CopyExportEnv) rewriteJournal(considered, keep map[string]bool) error {
	e.journalMu.Lock()
	defer e.journalMu.Unlock()
	lines, err := e.journalRaw()
	if err != nil {
		return err
	}
	var out bytes.Buffer
	for _, l := range lines {
		if run, ok := lineRun(l); !ok || keep[run] || !considered[run] {
			out.Write(l)
			out.WriteByte('\n')
		}
	}
	tmp := e.journalPath() + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return stateErr(err)
	}
	if _, err := f.Write(out.Bytes()); err != nil {
		f.Close()
		os.Remove(tmp)
		return stateErr(err)
	}
	if err := e.fsync(f); err != nil {
		f.Close()
		os.Remove(tmp)
		return stateErr(err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return stateErr(err)
	}
	if err := os.Rename(tmp, e.journalPath()); err != nil {
		os.Remove(tmp)
		return stateErr(err)
	}
	e.step("W12")
	return nil
}

// appendJSONL appends v as one line to path, creating it 0600 and its directory
// 0700, truncating a torn tail first, and fsyncs. Errors are state-directory errors.
func (e *CopyExportEnv) appendJSONL(path string, v any) error {
	if f := e.faults; f != nil && f.appendErr != nil {
		if err := f.appendErr(path); err != nil {
			return stateErr(err)
		}
	}
	b, err := json.Marshal(v)
	if err != nil {
		return stateErr(err)
	}
	if len(b) > 4096 {
		b = b[:4096] // never reached by a line this package writes; bounds a hostile field
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return stateErr(err)
	}
	f, err := openAppendable(path, true)
	if err != nil {
		return stateErr(err)
	}
	defer f.Close()
	if err := writeAtEnd(f, append(b, '\n')); err != nil {
		return stateErr(err)
	}
	return stateErr(e.fsync(f))
}

// readJournal reads this host's journal lines (malformed lines skipped, strings
// control-stripped by the readers that show them).
func (e *CopyExportEnv) readJournal() ([]ceJournalLine, error) {
	var out []ceJournalLine
	err := scanLines(e.journalPath(), func(line []byte) error {
		var l ceJournalLine
		if json.Unmarshal(line, &l) == nil && l.Run != "" && (l.Host == "" || l.Host == e.Host) {
			out = append(out, l)
		}
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return out, err
}

// journalHas reports whether the journal holds an event for run.
func (e *CopyExportEnv) journalHas(run, event string) bool {
	lines, _ := e.readJournal()
	for _, l := range lines {
		if l.Run == run && l.Event == event {
			return true
		}
	}
	return false
}

// ---- the fingerprint ledger -----------------------------------------------

// ceLedgerLine is one line of export-fingerprints.jsonl (design §4.10, §4.16),
// never compacted: a sealed run's fingerprint, the retry chain's set state, and the
// `gone` or `refused` line of a run that never sealed.
type ceLedgerLine struct {
	V              int       `json:"v"`
	Run            string    `json:"run"`
	Chain          string    `json:"chain"`
	Kind           string    `json:"kind"` // sealed, gone, refused
	At             time.Time `json:"at"`
	Status         string    `json:"status,omitempty"`
	Fingerprint    string    `json:"fingerprint,omitempty"`
	SetState       string    `json:"set_state,omitempty"`
	SetFingerprint string    `json:"set_fingerprint,omitempty"`
	Parts          []string  `json:"parts,omitempty"`
}

func (e *CopyExportEnv) readLedger() ([]ceLedgerLine, error) {
	var out []ceLedgerLine
	err := scanLines(e.ledgerPath(), func(line []byte) error {
		var l ceLedgerLine
		if json.Unmarshal(line, &l) == nil && l.Run != "" {
			out = append(out, l)
		}
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return out, err
}

// ledgerHas reports whether run has a ledger line of any kind.
func ledgerHas(lines []ceLedgerLine, run string) bool {
	for _, l := range lines {
		if l.Run == run {
			return true
		}
	}
	return false
}

// ---- the chain index -------------------------------------------------------

// noteRecord adds run to the in-process index of run records by chain id (design
// §4.16: the launch sweep builds it, and this process keeps it current whenever a
// header is written), so a part that seals after launch is found by a later seal.
func (e *CopyExportEnv) noteRecord(chain, run string) {
	e.ledgerMu.Lock()
	defer e.ledgerMu.Unlock()
	e.noteRecordLocked(chain, run)
}

func (e *CopyExportEnv) noteRecordLocked(chain, run string) {
	if e.chains == nil {
		e.chains = map[string][]string{}
	}
	for _, r := range e.chains[chain] {
		if r == run {
			return
		}
	}
	e.chains[chain] = append(e.chains[chain], run)
}

// chainRuns lists the run ids the index holds for chain. The index is built from
// the record headers on disk the first time it is needed in a process.
func (e *CopyExportEnv) chainRunsLocked(chain string) []string {
	if !e.chainsBuilt {
		e.chainsBuilt = true
		if names, err := os.ReadDir(e.recordsDir()); err == nil {
			for _, n := range names {
				if run, ok := strings.CutSuffix(n.Name(), ".jsonl"); ok {
					if s, err := readHeaderOnly(e.recordPath(run)); err == nil && s.Chain != "" {
						e.noteRecordLocked(s.Chain, run)
					}
				}
			}
		}
	}
	return append([]string(nil), e.chains[chain]...)
}

// readHeaderOnly reads a run record's first line.
func readHeaderOnly(path string) (ceHeaderLine, error) {
	var h ceHeaderLine
	f, err := os.Open(path)
	if err != nil {
		return h, err
	}
	defer f.Close()
	line, err := bufio.NewReaderSize(f, 64<<10).ReadBytes('\n')
	if err != nil {
		return h, err
	}
	err = json.Unmarshal(line, &h)
	return h, err
}
