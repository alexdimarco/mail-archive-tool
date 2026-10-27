package state

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// covers: MA-810, R37
// WriteHistory writes the log's lines exactly as the reader reads them back — every
// kind of event, the sealed timeline's repeated headers and barriers among them — so
// an unencrypted copy's history folds to the same go-back states as the sealed
// timeline it came from, and a repeated header is not counted as a second run.
func TestWriteHistoryRoundTripsEveryEvent(t *testing.T) {
	at := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	events := []HistoryEvent{
		{Run: 1, At: at.Format(time.RFC3339), Mailboxes: []string{"a"}},
		{K: "s\x00Inbox\x00m1", Folder: "Inbox"},
		{K: "s\x00Inbox\x00m2", Folder: "Inbox"},
		{Run: 1, At: at.Format(time.RFC3339), Copy: true},
		{From: "Inbox", To: "Kept"},
		{Run: 1, Completed: at.Add(time.Minute).Format(time.RFC3339)},
		{Barrier: true},
		{Run: 2, At: at.Add(24 * time.Hour).Format(time.RFC3339)},
		{K: "s\x00Inbox\x00m2", Gone: true},
		{Run: 2, Completed: at.Add(25 * time.Hour).Format(time.RFC3339)},
	}
	var buf bytes.Buffer
	if err := WriteHistory(&buf, events); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), HistoryName)
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	back, err := ReadHistory(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, events) {
		t.Fatalf("read back\n %+v\nwant\n %+v", back, events)
	}
	if lines := bytes.Count(buf.Bytes(), []byte("\n")); lines != len(events) {
		t.Fatalf("%d lines for %d events", lines, len(events))
	}
	late := at.Add(48 * time.Hour)
	if got, want := FoldEvents(back, late), FoldEvents(events, late); !reflect.DeepEqual(got, want) {
		t.Fatalf("the written log folds to %v, the events to %v", got, want)
	}
	st, err := HistoryStatus(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Runs != 2 {
		t.Fatalf("the written log counts %d runs, want 2 (a repeated header is not a run)", st.Runs)
	}
}
