package container

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"sync"
	"time"

	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/vault"
)

// historyChunkTarget is the plaintext size the open last history chunk grows to
// before a new one starts (design phase2b §5): earlier chunks are never re-sealed.
const historyChunkTarget = 256 << 10

// sealedTimeline is an encrypted archive's go-back timeline (state.Timeline): it
// buffers a run's events, and the record's next commit seals them (§5).
type sealedTimeline struct {
	mu     sync.Mutex
	events []state.HistoryEvent
	closed bool
}

var errTimelineClosed = errors.New("container: the sealed timeline is closed")

func (t *sealedTimeline) add(ev state.HistoryEvent) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return errTimelineClosed
	}
	t.events = append(t.events, ev)
	return nil
}

func (t *sealedTimeline) WriteRunHeader(run int64, at time.Time, mailboxes []string) error {
	return t.add(state.HistoryEvent{Run: run, At: at.UTC().Format(time.RFC3339), Mailboxes: mailboxes})
}
func (t *sealedTimeline) WriteFolder(key, folder string) error {
	return t.add(state.HistoryEvent{K: key, Folder: folder})
}
func (t *sealedTimeline) WriteGone(key string) error {
	return t.add(state.HistoryEvent{K: key, Gone: true})
}
func (t *sealedTimeline) WriteRename(from, to string) error {
	return t.add(state.HistoryEvent{From: from, To: to})
}
func (t *sealedTimeline) WriteRunFooter(run int64, at time.Time) error {
	return t.add(state.HistoryEvent{Run: run, Completed: at.UTC().Format(time.RFC3339)})
}

// Sync is a no-op: the buffered events are sealed by the record's commit, which
// commits them together with the manifest by one rename.
func (t *sealedTimeline) Sync() error { return nil }

// Close stops further events; it is idempotent. Events not yet committed are lost
// with the run, as an unsynced plaintext tail would be.
func (t *sealedTimeline) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.closed = true
	return nil
}

// take returns the buffered events, leaving them buffered (put back on failure by
// not calling done).
func (t *sealedTimeline) pending() []state.HistoryEvent {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]state.HistoryEvent(nil), t.events...)
}

// done drops the first n buffered events: they are sealed.
func (t *sealedTimeline) done(n int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.events = t.events[n:]
}

// Timeline is the archive's sealed timeline: a capture appends its go-back events
// here instead of the plaintext log.
func (s *Store) Timeline() state.Timeline {
	if s.timeline == nil {
		s.timeline = &sealedTimeline{}
	}
	return s.timeline
}

// encodeEvents is events as JSON lines, the plaintext log's own form.
func encodeEvents(events []state.HistoryEvent) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	for _, ev := range events {
		if err := enc.Encode(ev); err != nil {
			return nil, err
		}
	}
	return b.Bytes(), nil
}

// decodeEvents parses JSON lines, skipping a line that does not parse (as the
// plaintext reader does).
func decodeEvents(plain []byte) []state.HistoryEvent {
	var out []state.HistoryEvent
	sc := bufio.NewScanner(bytes.NewReader(plain))
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var ev state.HistoryEvent
		if json.Unmarshal(line, &ev) == nil {
			out = append(out, ev)
		}
	}
	return out
}

// lastHeader is the last run header (not a copy) among events.
func lastHeader(events []state.HistoryEvent, prev *state.HistoryEvent) *state.HistoryEvent {
	for i := range events {
		if events[i].Run > 0 && events[i].At != "" {
			h := events[i]
			h.Copy = true
			prev = &h
		}
	}
	return prev
}

// stagedHistory is a commit's history step, applied to the Store only once the
// commit's pointer has landed.
type stagedHistory struct {
	table    []Entry
	n        int                 // buffered events sealed
	tail     []byte              // the open last chunk's plaintext now
	header   *state.HistoryEvent // the run header a new chunk would copy
	clearBad bool                // an unreadable last chunk was left behind
}

// apply records a landed commit's history step.
func (s *Store) apply(h stagedHistory) {
	if h.n == 0 {
		return
	}
	s.timeline.done(h.n)
	s.histTail, s.histHeader = h.tail, h.header
	if h.clearBad {
		s.histTailBad = false
	}
	// The events a failed staging held back have landed, with a header that already
	// left their hold out (Commit).
	s.histGapStaging = false
}

// stageHistory seals the timeline's buffered events at gen into the history
// section, returning its new chunk table: the open last chunk re-sealed as a new
// file with the events appended, until it passes historyChunkTarget; then a new
// last chunk, starting with a copy of the run header current at its first event.
// A last chunk that cannot be read is kept as it is, its loss recorded, and a new
// one started. With nothing buffered the table is unchanged.
func (s *Store) stageHistory(gen uint64) (stagedHistory, error) {
	table := append([]Entry(nil), s.ptr.History...)
	if s.timeline == nil {
		return stagedHistory{table: table}, nil
	}
	events := s.timeline.pending()
	if len(events) == 0 {
		return stagedHistory{table: table}, nil
	}
	lines, err := encodeEvents(events)
	if err != nil {
		return stagedHistory{}, err
	}
	var tail []byte
	if n := len(table); n > 0 && !s.histTailLoaded {
		last := table[n-1]
		if sealed, _, rerr := readChunk(s.out, last); rerr == nil {
			if plain, oerr := s.opts.Key.OpenChunk(vault.ContainerHistory, last.Gen, last.I, sealed); oerr == nil {
				s.histTail = plain
				s.histHeader = lastHeader(decodeEvents(plain), s.histHeader)
			} else {
				s.histTailBad = true
			}
		} else {
			s.histTailBad = true
		}
		s.histTailLoaded = true
	}
	badTail := s.histTailBad
	if badTail {
		// The last chunk stays named as it is; every event it held is lost, and a new
		// chunk starts after it (§5, the review's L7-8.8). Its events reach back to the
		// generation after the chunk before it was last sealed: an open chunk is
		// re-sealed at every commit, so its own generation says only when it was last
		// written (the adversarial pass's OUT-01, CRIT-2).
		from := uint64(1)
		if n := len(table); n > 1 {
			from = table[n-2].Gen + 1
		}
		s.markHistoryLoss(from, gen)
		table = append(table, Entry{I: uint32(len(table))}) // placeholder for the new last
	} else if len(table) == 0 {
		table = append(table, Entry{I: 0})
		tail = nil
	} else {
		tail = s.histTail
	}
	pos := len(table) - 1
	if len(tail) > 0 && len(tail)+len(lines) > historyChunkTarget {
		// The open chunk is full: it stays as sealed; a new last chunk starts.
		table = append(table, Entry{I: uint32(len(table))})
		pos, tail = len(table)-1, nil
	}
	if len(tail) == 0 && s.histHeader != nil && !(events[0].Run > 0 && events[0].At != "") {
		head, err := encodeEvents([]state.HistoryEvent{*s.histHeader})
		if err != nil {
			return stagedHistory{}, err
		}
		tail = head
	}
	plain := append(append([]byte(nil), tail...), lines...)
	e, err := writeHistoryChunk(s, gen, uint32(pos), plain)
	if err != nil {
		return stagedHistory{}, err
	}
	table[pos] = e
	return stagedHistory{table: table, n: len(events), tail: plain, header: lastHeader(events, s.histHeader), clearBad: badTail}, nil
}

// writeHistoryChunk seals and writes one history chunk; a var so a test can fail
// the history step alone.
var writeHistoryChunk = func(s *Store, gen uint64, pos uint32, plain []byte) (Entry, error) {
	return s.writeChunk(vault.ContainerHistory, gen, pos, plain)
}

// The sealed header's history_gap says two things in one widening interval (a
// superset is the safe direction): history that is lost for good, and events a failed
// staging holds back for the next commit. Only the hold is cleared, when its events
// land; a loss stays (the adversarial pass's OUT-02). After a reload every gap is a
// loss: held events do not outlive their run.

// markHistoryLoss records that the events sealed at generations from..to are lost.
func (s *Store) markHistoryLoss(from, to uint64) {
	s.histLoss = widenGap(s.histLoss, from, to)
	s.hdr.HistoryGap = widenGap(s.hdr.HistoryGap, from, to)
	s.notice("part of the archive's go-back history could not be kept")
}

// holdHistory records that a failed staging holds the events of the commit at gen
// back to the next one.
func (s *Store) holdHistory(gen uint64) {
	s.histGapStaging = true
	s.hdr.HistoryGap = widenGap(s.hdr.HistoryGap, gen, gen)
	s.notice("part of the archive's go-back history could not be kept")
}

// widenGap is g widened to cover from..to, as a new value: a gap is never changed in
// place, so the header and the loss can share one.
func widenGap(g *Gap, from, to uint64) *Gap {
	if g == nil {
		return &Gap{From: from, To: to}
	}
	return &Gap{From: min(g.From, from), To: max(g.To, to)}
}

// Chunks is the archive-relative paths of the chunks the committed pointer names in
// the section of kind.
func (s *Store) Chunks(kind vault.ContainerKind) []string {
	table := map[vault.ContainerKind][]Entry{vault.ContainerManifest: s.ptr.Manifest, vault.ContainerHistory: s.ptr.History, vault.ContainerIndex: s.ptr.Index}[kind]
	out := make([]string, 0, len(table))
	for _, e := range table {
		out = append(out, e.Name.Path())
	}
	return out
}

// History reads the sealed timeline in order. A chunk that cannot be read stands
// as a Barrier event, so no event after it is dated by an earlier run (§5); it is
// never an error — damaged history only weakens the timeline.
func (s *Store) History() []state.HistoryEvent {
	out, _ := s.history(false)
	return out
}

// HistoryChecked is History for a reader (design-encrypted-archives-phase3 §7.3,
// §3.2): a chunk that is gone, or a key locked meanwhile, is an error — the reader
// reloads, or shows the locked page — never folded into a barrier; a chunk whose
// bytes are damaged still stands as a Barrier event.
func (s *Store) HistoryChecked() ([]state.HistoryEvent, error) { return s.history(true) }

func (s *Store) history(checked bool) ([]state.HistoryEvent, error) {
	var out []state.HistoryEvent
	for _, e := range s.ptr.History {
		sealed, _, err := readChunk(s.out, e)
		var plain []byte
		if err == nil {
			plain, err = s.opts.Key.OpenChunk(vault.ContainerHistory, e.Gen, e.I, sealed)
		}
		switch {
		case err == nil:
			out = append(out, decodeEvents(plain)...)
		case checked && (errors.Is(err, fs.ErrNotExist) || errors.Is(err, vault.ErrKeyDestroyed)):
			return nil, fmt.Errorf("container: the history chunk %s: %w", e.Name.Path(), err)
		default:
			out = append(out, state.HistoryEvent{Barrier: true})
		}
	}
	return out, nil
}

// HistoryGap is the part of the timeline the record says is lost — from, and to
// (zero: up to now) — when there is one: a reader says go-back is partial (§7.2).
func (s *Store) HistoryGap() (from, to uint64, ok bool) {
	if g := s.hdr.HistoryGap; g != nil {
		return g.From, g.To, true
	}
	return 0, 0, false
}
