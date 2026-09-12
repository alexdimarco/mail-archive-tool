package state

// The append-only history log (.mailarchive-history.jsonl) is the go-back
// timeline (design §3.3): one JSON object per line, recording each run as the
// changes it observed. The manifest is this log's fold-to-now projection; the
// log lets `serve` reconstruct the mailbox as it stood on any past date.
//
// Line kinds (all HistoryEvent, discriminated by which fields are set):
//   - run header       {"run":id,"at":RFC3339,"mailboxes":[…]}
//   - folder assertion {"k":key,"folder":"Inbox"}   (newly-seen / moved / present-again)
//   - gone             {"k":key,"gone":true}        (present last run, absent now)
//   - folder rename    {"from":"Old","to":"New"}    (all messages under one path, one line)
//   - run footer       {"run":id,"completed":RFC3339}
//
// A per-message event inherits the timestamp of the run header above it, so the
// log is written once and each event costs one line. Runs happen in order, so
// the file is chronological. Crash safety (§3.5) pins the write order
// history-append+fsync → index → manifest; a crash therefore costs at most a
// duplicate event (idempotent under the fold), never a lost move. Two defences
// keep a torn tail from poisoning the log: the writer truncates a torn trailing
// partial line on open-for-append (so a torn line is always genuinely last), and
// the reader skips an unparseable line (so a crash mid-line is tolerated).

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"sync"
	"time"
)

// HistoryName is the archive-local timeline log, under -out beside the manifest.
const HistoryName = ".mailarchive-history.jsonl"

// HistoryEvent is one line of the history log. Fields carry omitempty so each
// line holds only its kind's fields; the reader classifies by which are set.
type HistoryEvent struct {
	// Run header (Run>0 && At!="") and run-completed footer (Run>0 && Completed!="").
	Run       int64    `json:"run,omitempty"`
	At        string   `json:"at,omitempty"`
	Mailboxes []string `json:"mailboxes,omitempty"`
	Completed string   `json:"completed,omitempty"`

	// Per-message event: a folder assertion (K!="" && !Gone — newly-seen, moved,
	// or present-again) or a gone marker (K!="" && Gone).
	K      string `json:"k,omitempty"`
	Folder string `json:"folder,omitempty"`
	Gone   bool   `json:"gone,omitempty"`

	// Folder-rename event (From!="" && To!="" && K==""): rewrites the folder of
	// every message currently under From — exactly, or as a path prefix — in one
	// line, so a folder rename is not a mass of per-message moves (GB-5).
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`
}

// HistoryWriter appends events to the log. It is safe for concurrent use.
type HistoryWriter struct {
	mu sync.Mutex
	f  *os.File
}

// OpenHistory opens the log for append, creating it if absent. If the file does
// not end in a newline — a run crashed mid-line — the torn trailing partial line
// is truncated first, so every appended event is a clean whole line and the only
// ever-torn line is genuinely the last (the read side then skips it) — GB-4.
func OpenHistory(path string) (*HistoryWriter, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open history log %s: %w", path, err)
	}
	if err := truncateTornTail(f); err != nil {
		f.Close()
		return nil, fmt.Errorf("repair history log %s: %w", path, err)
	}
	return &HistoryWriter{f: f}, nil
}

// truncateTornTail truncates the file to just past its last newline (dropping a
// torn trailing partial line) and leaves the write cursor at the end. A file
// that already ends in a newline is unchanged; a file with no newline at all is
// wholly a torn line and is truncated to empty.
func truncateTornTail(f *os.File) error {
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	size := fi.Size()
	if size == 0 {
		_, err = f.Seek(0, io.SeekEnd)
		return err
	}
	off, err := lastNewlineEnd(f, size)
	if err != nil {
		return err
	}
	if off < size {
		if err := f.Truncate(off); err != nil {
			return err
		}
	}
	_, err = f.Seek(off, io.SeekStart)
	return err
}

// lastNewlineEnd returns the offset one past the last '\n' in the first size
// bytes of f, or 0 when there is none. It scans backward in bounded chunks, so
// its memory cost is fixed regardless of the log's size.
func lastNewlineEnd(f *os.File, size int64) (int64, error) {
	const chunk = 8192
	buf := make([]byte, chunk)
	pos := size
	for pos > 0 {
		n := int64(chunk)
		if pos < n {
			n = pos
		}
		start := pos - n
		if _, err := f.ReadAt(buf[:n], start); err != nil && !errors.Is(err, io.EOF) {
			return 0, err
		}
		for i := n - 1; i >= 0; i-- {
			if buf[i] == '\n' {
				return start + i + 1, nil
			}
		}
		pos = start
	}
	return 0, nil
}

func (w *HistoryWriter) append(ev HistoryEvent) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return errors.New("history log is closed")
	}
	b, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = w.f.Write(b)
	return err
}

// WriteRunHeader opens a run: its id, its wall-clock time (the timestamp every
// per-message event of the run inherits), and the mailboxes it walked.
func (w *HistoryWriter) WriteRunHeader(run int64, at time.Time, mailboxes []string) error {
	return w.append(HistoryEvent{Run: run, At: at.UTC().Format(time.RFC3339), Mailboxes: mailboxes})
}

// WriteFolder records that key is present under folder (newly-seen, moved, or
// present-again — all one shape, resolved by the fold).
func (w *HistoryWriter) WriteFolder(key, folder string) error {
	return w.append(HistoryEvent{K: key, Folder: folder})
}

// WriteGone records that key, present in an earlier run, is absent now.
func (w *HistoryWriter) WriteGone(key string) error {
	return w.append(HistoryEvent{K: key, Gone: true})
}

// WriteRename records that a folder path was renamed from → to, moving every
// message under it in one line.
func (w *HistoryWriter) WriteRename(from, to string) error {
	return w.append(HistoryEvent{From: from, To: to})
}

// WriteRunFooter marks a clean run end.
func (w *HistoryWriter) WriteRunFooter(run int64, at time.Time) error {
	return w.append(HistoryEvent{Run: run, Completed: at.UTC().Format(time.RFC3339)})
}

// Sync flushes the appended events to stable storage (the crash-order fsync).
func (w *HistoryWriter) Sync() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return errors.New("history log is closed")
	}
	return w.f.Sync()
}

// Close closes the underlying file. It is idempotent.
func (w *HistoryWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return nil
	}
	err := w.f.Close()
	w.f = nil
	return err
}

// ReadHistory returns every parseable event in order. A missing file is not an
// error (an archive with no timeline yet yields nil). An unparseable line — a
// torn tail from a crashed run, or a hand-corruption — is skipped rather than
// failing the read, so the timeline degrades gracefully.
func ReadHistory(path string) ([]HistoryEvent, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open history log %s: %w", path, err)
	}
	defer f.Close()

	var out []HistoryEvent
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var ev HistoryEvent
		if json.Unmarshal(line, &ev) != nil {
			continue // tolerate a torn/corrupt line
		}
		out = append(out, ev)
	}
	if err := sc.Err(); err != nil {
		return out, fmt.Errorf("read history log %s: %w", path, err)
	}
	return out, nil
}

// FoldState is a message's state at a folded date: the folder it sat under and
// whether it was present in the mailbox then (a gone message keeps its last
// known folder but Present=false, so a consumer hides it at that date).
type FoldState struct {
	Folder  string
	Present bool
}

// FoldHistory replays the log to upTo and returns each message's state at that
// date (T2/T4). Each per-message event carries the time of the run header above
// it; the latest transition — folder assertion, gone, or present-again — with a
// time ≤ upTo wins (GB-03/F). A folder-rename with a time ≤ upTo rewrites the
// folder of every message currently under the old path. A message with no event
// at or before upTo is absent from the result (it did not yet exist then).
func FoldHistory(path string, upTo time.Time) (map[string]FoldState, error) {
	events, err := ReadHistory(path)
	if err != nil {
		return nil, err
	}
	state := map[string]FoldState{}
	var curAt time.Time
	haveAt := false
	for _, ev := range events {
		switch {
		case ev.Run > 0 && ev.At != "":
			if t, perr := time.Parse(time.RFC3339, ev.At); perr == nil {
				curAt, haveAt = t, true
			}
		case ev.K != "":
			if !haveAt || curAt.After(upTo) {
				continue
			}
			if ev.Gone {
				s := state[ev.K]
				s.Present = false // keep the last known Folder
				state[ev.K] = s
			} else {
				state[ev.K] = FoldState{Folder: ev.Folder, Present: true}
			}
		case ev.From != "" && ev.To != "":
			if !haveAt || curAt.After(upTo) {
				continue
			}
			applyRename(state, ev.From, ev.To)
		}
	}
	return state, nil
}

// applyRename rewrites the folder of every message under from (exactly, or as a
// path prefix) to to, in the folded state.
func applyRename(state map[string]FoldState, from, to string) {
	for k, s := range state {
		switch {
		case s.Folder == from:
			s.Folder = to
			state[k] = s
		case strings.HasPrefix(s.Folder, from+"/"):
			s.Folder = to + s.Folder[len(from):]
			state[k] = s
		}
	}
}
