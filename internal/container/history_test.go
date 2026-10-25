package container

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/state"
)

// runEvents writes one run's header and n folder events to tl.
func runEvents(t *testing.T, tl state.Timeline, run int, at time.Time, n int) {
	t.Helper()
	if err := tl.WriteRunHeader(int64(run), at, []string{"a@example.com"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		if err := tl.WriteFolder(fmt.Sprintf("s\x00msg-%d-%d", run, i), "Inbox/"+strings.Repeat("f", 40)); err != nil {
			t.Fatal(err)
		}
	}
}

// covers: MA-756, R37
// The go-back history is sealed in the record (Phase 2b-2, design phase2b §5): a
// capture's events, written to the store's timeline, are committed by the record's
// save and read back in order, in the plaintext log's own form; no history file is
// written; and the timeline's Sync seals nothing by itself — the record's commit does.
func TestTheHistoryIsSealedInTheRecord(t *testing.T) {
	out := fresh(t)
	m, s := open(t, out, opts(1))
	tl := s.Timeline()
	runEvents(t, tl, 1, runAt(1), 3)
	tl.WriteGone("s\x00msg-0-0")
	tl.WriteRename("Inbox", "Inbox2")
	tl.WriteRunFooter(1, runAt(1).Add(time.Minute))
	if err := tl.Sync(); err != nil {
		t.Fatal(err)
	}
	if len(s.ptr.History) != 0 {
		t.Fatal("Sync sealed history by itself; only the record's commit may")
	}
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	if exists(out, state.HistoryName) {
		t.Fatal("a sealed record's capture wrote a plaintext history file")
	}
	_, s = open(t, out, opts(2))
	got := s.History()
	want := []state.HistoryEvent{
		{Run: 1, At: runAt(1).UTC().Format(time.RFC3339), Mailboxes: []string{"a@example.com"}},
		{K: "s\x00msg-1-0", Folder: "Inbox/" + strings.Repeat("f", 40)},
		{K: "s\x00msg-1-1", Folder: "Inbox/" + strings.Repeat("f", 40)},
		{K: "s\x00msg-1-2", Folder: "Inbox/" + strings.Repeat("f", 40)},
		{K: "s\x00msg-0-0", Gone: true},
		{From: "Inbox", To: "Inbox2"},
		{Run: 1, Completed: runAt(1).Add(time.Minute).UTC().Format(time.RFC3339)},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("history read back\n %+v\nwant\n %+v", got, want)
	}
}

// covers: MA-756, R37
// Each commit re-seals only the open last chunk, as a new file (never in place),
// until it passes its target; then a new last chunk starts with a copy of the run
// header current at its first event, marked as a copy, so the chunk reads on its own
// and the run is counted once. Earlier chunks are never re-sealed.
func TestHistoryChunksGrowThenRollOverWithAHeaderCopy(t *testing.T) {
	out := fresh(t)
	m, s := open(t, out, opts(1))
	tl := s.Timeline()
	runEvents(t, tl, 7, runAt(1), 10)
	m.Save()
	first := s.ptr.History
	if len(first) != 1 {
		t.Fatalf("%d history chunks after one small commit, want 1", len(first))
	}
	for i := 0; i < 20; i++ {
		tl.WriteFolder(fmt.Sprintf("s\x00more-%d", i), "Inbox")
	}
	m.Save()
	if len(s.ptr.History) != 1 || s.ptr.History[0].Name == first[0].Name {
		t.Fatal("a commit must re-seal the open last chunk as a new file, keeping one chunk")
	}
	before := s.ptr.History[0]
	// Enough events to pass the chunk target.
	for i := 0; i < 3000; i++ {
		tl.WriteFolder(fmt.Sprintf("s\x00big-%d", i), "Inbox/"+strings.Repeat("x", 80))
	}
	m.Save()
	if len(s.ptr.History) != 2 || s.ptr.History[0] != before {
		t.Fatalf("the full chunk must stay as sealed and a new one start: %d chunks", len(s.ptr.History))
	}
	_, s = open(t, out, opts(2))
	events := s.History()
	copies, headers := 0, 0
	for _, ev := range events {
		if ev.Run == 7 && ev.At != "" {
			headers++
			if ev.Copy {
				copies++
			}
		}
	}
	if headers != 2 || copies != 1 {
		t.Fatalf("want the run's header once plus one marked copy, got %d headers, %d copies", headers, copies)
	}
	stat := statOf(t, events)
	if stat.Runs != 1 {
		t.Fatalf("a copied header counted the run %d times", stat.Runs)
	}
}

// statOf counts runs the way HistoryStatus does, over sealed events.
func statOf(t *testing.T, events []state.HistoryEvent) state.HistoryStat {
	t.Helper()
	p := filepath.Join(t.TempDir(), "h.jsonl")
	b, err := encodeEvents(events)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(p, b, 0o600)
	st, err := state.HistoryStatus(p)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// covers: MA-756, R37
// A history chunk that cannot be read never blocks a backup and never dates an event
// by an earlier run: reading stands a barrier in its place, and the fold gives the
// events after it no run time until the next header (#3).
func TestADamagedHistoryChunkIsABarrier(t *testing.T) {
	out := fresh(t)
	m, s := open(t, out, opts(1))
	tl := s.Timeline()
	runEvents(t, tl, 1, runAt(1), 1)
	m.Save()
	for i := 0; i < 3000; i++ {
		tl.WriteFolder(fmt.Sprintf("s\x00big-%d", i), "Inbox/"+strings.Repeat("x", 80))
	}
	m.Save() // a second chunk, starting with run 1's header copy
	tl.WriteFolder("s\x00late", "Moved")
	m.Save()
	if len(s.ptr.History) < 2 {
		t.Fatalf("fixture: %d chunks", len(s.ptr.History))
	}
	// Damage the FIRST chunk: the later ones still read, each from its own header copy.
	first := s.ptr.History[0]
	p := filepath.Join(out, filepath.FromSlash(first.Name.Path()))
	b, _ := os.ReadFile(p)
	b[len(b)-1] ^= 1
	os.WriteFile(p, b, 0o600)
	m, s = open(t, out, opts(2))
	events := s.History()
	if !events[0].Barrier {
		t.Fatalf("a damaged chunk must read as a barrier, got %+v", events[0])
	}
	fold := state.FoldEvents(events, runAt(9))
	if f, ok := fold["s\x00late"]; !ok || f.Folder != "Moved" {
		t.Fatalf("the events after the damaged chunk must still fold (their chunk carries its header): %+v", f)
	}
	// A barrier alone, then events with no header: they are not dated.
	alone := state.FoldEvents([]state.HistoryEvent{{Run: 1, At: runAt(1).Format(time.RFC3339)}, {Barrier: true}, {K: "x", Folder: "F"}}, runAt(9))
	if _, ok := alone["x"]; ok {
		t.Fatal("an event after a barrier was dated by the run before it")
	}
	// The backup itself is unaffected.
	tl = s.Timeline()
	tl.WriteFolder("s\x00after", "Inbox")
	if err := m.Save(); err != nil {
		t.Fatalf("a damaged history chunk blocked the backup: %v", err)
	}
}

// covers: MA-756, R37
// An open last chunk that cannot be read is kept named as it is, its loss recorded
// in the sealed header, and a new last chunk started; and a history step that fails
// keeps its events for the next commit, recording the gap in that commit's header,
// cleared when they land.
func TestHistoryDamageAndFailureAreRecordedNotFatal(t *testing.T) {
	out := fresh(t)
	m, s := open(t, out, opts(1))
	runEvents(t, s.Timeline(), 1, runAt(1), 2)
	m.Save()
	last := s.ptr.History[0]
	p := filepath.Join(out, filepath.FromSlash(last.Name.Path()))
	b, _ := os.ReadFile(p)
	b[len(b)-1] ^= 1
	os.WriteFile(p, b, 0o600)

	m, s = open(t, out, opts(2))
	runEvents(t, s.Timeline(), 2, runAt(2), 1)
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	if len(s.ptr.History) != 2 || s.ptr.History[0] != last || s.hdr.HistoryGap == nil {
		t.Fatalf("the unreadable last chunk must stay named, a new one start, a gap be recorded: %d chunks, gap %v", len(s.ptr.History), s.hdr.HistoryGap)
	}

	out2 := fresh(t)
	m, s = open(t, out2, opts(1))
	runEvents(t, s.Timeline(), 1, runAt(1), 2)
	orig := writeHistoryChunk
	writeHistoryChunk = func(*Store, uint64, uint32, []byte) (Entry, error) { return Entry{}, errors.New("the disk refused") }
	err := m.Save()
	writeHistoryChunk = orig
	if err != nil {
		t.Fatalf("a failed history step failed the record's save: %v", err)
	}
	if len(s.ptr.History) != 0 || s.hdr.HistoryGap == nil {
		t.Fatalf("a failed history step must leave the table and record the gap: %d chunks, gap %v", len(s.ptr.History), s.hdr.HistoryGap)
	}
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	if len(s.ptr.History) != 1 || s.hdr.HistoryGap != nil {
		t.Fatalf("the held events must land at the next commit and the gap clear: %d chunks, gap %v", len(s.ptr.History), s.hdr.HistoryGap)
	}
	if got := len(s.History()); got != 3 {
		t.Fatalf("%d events landed, want the 3 held back", got)
	}
}
