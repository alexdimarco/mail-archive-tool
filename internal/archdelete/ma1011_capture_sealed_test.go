package archdelete

import (
	"os"
	"testing"
	"time"

	"mail-archive-tool/internal/state"
)

// covers: MA-1011, R36, R37
// An encrypted archive's sealed values, read from its sealed manifest and timeline while its key
// is open here, are stated by the plain archive's rules (§8.1 [O2-05]): the message count and the
// mail's date range from the records, and the first run header of the timeline — a header
// repeated at a chunk's start (Copy) is not a run, and a chunk that could not be read (a Barrier)
// before any header leaves the first run not known, never a later run's. Positive twin first: the
// plain archive's own capture of the same manifest gives the same count and dates.
func TestSealedValuesAreStatedByThePlainArchivesRules(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, 6, map[string]string{"path:/m": "Mailbox"}, []rec{
		{Path: "Mailbox/Inbox/2023-05-02_0800_a_11111111.html"},
		{Path: "Mailbox/Inbox/2024-02-10_1730_b_22222222.html"},
		{Path: "Mailbox/Inbox/odd.html", Date: "2022-12-31T23:00:00Z"},
	})
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	m, _, err := readManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	plain := capturePlain(root, m)
	from, to := time.Date(2022, 12, 31, 23, 0, 0, 0, time.UTC), time.Date(2024, 2, 10, 17, 30, 0, 0, time.UTC)
	if !plain.Known || plain.Messages != 3 || !plain.MailFrom.Equal(from) || !plain.MailTo.Equal(to) {
		t.Fatalf("positive twin: the plain capture: %+v", plain)
	}

	first := "2024-01-05T09:00:00Z"
	for _, c := range []struct {
		name    string
		history []state.HistoryEvent
		want    time.Time
	}{
		{"the first run header", []state.HistoryEvent{{K: "x"}, {Run: 3, At: first}, {Run: 4, At: "2024-02-01T09:00:00Z"}}, time.Date(2024, 1, 5, 9, 0, 0, 0, time.UTC)},
		{"a chunk's repeated header is not a run", []state.HistoryEvent{{Run: 2, At: "2023-01-01T00:00:00Z", Copy: true}, {Run: 3, At: first}}, time.Date(2024, 1, 5, 9, 0, 0, 0, time.UTC)},
		{"an unreadable chunk before any header", []state.HistoryEvent{{Barrier: true}, {Run: 3, At: first}}, time.Time{}},
		{"no timeline", nil, time.Time{}},
	} {
		got := CaptureSealed(m, c.history)
		if !got.Known || got.Messages != plain.Messages || !got.MailFrom.Equal(plain.MailFrom) || !got.MailTo.Equal(plain.MailTo) {
			t.Errorf("%s: count and dates %+v; want the plain capture's %+v", c.name, got, plain)
		}
		if !got.FirstRun.Equal(c.want) {
			t.Errorf("%s: first run %v; want %v", c.name, got.FirstRun, c.want)
		}
	}
}
