package app

import (
	"bytes"
	"os"
	"runtime"
	"testing"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/state"
)

// covers: MA-420, MA-435, R20, R29, S52
// Known item K1 (a test gap, closed here): a record whose recorded .eml digest exists but whose .eml is
// gone is skipped by the redaction belt, counted, and never served from its archived page (§4.6 item 5):
// the page of a message whose original was redacted is the redacted message's content too. A record
// that recorded no digest for its original, and has none on disk, is a message the archive kept no
// original of, and is served from its page and zip, as before (the positive twin).
func TestArchiveRecordWithARecordedOriginalThatIsGoneIsSkippedNotServedFromItsPage(t *testing.T) {
	a := buildCEArchive(t)
	hello, second := a.key("hello"), a.key("second")
	_, helloEML, _ := a.files("hello")
	_, secondEML, _ := a.files("second")
	if err := os.Remove(helloEML); err != nil { // a redaction: the original is deleted, its record and page are not
		t.Fatal(err)
	}
	if err := os.Remove(secondEML); err != nil {
		t.Fatal(err)
	}
	a.setRec("second", func(r *state.Record) { r.Fixity.EML = nil }) // the archive recorded no digest for this one's original
	s := a.open()
	res, err := s.Messages(t.Context(), []string{hello, second}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped != 1 || len(res.Vanished) != 1 || res.Vanished[0] != hello || len(res.Messages) != 1 {
		t.Fatalf("resolution: skipped %d, vanished %v, %d messages; want hello (original recorded, now gone) skipped and counted", res.Skipped, res.Vanished, len(res.Messages))
	}
	if m := res.Messages[0]; m.SourceID != second || m.NoOrig == "" {
		t.Errorf("the record with no recorded original: %+v, want it served from its page (no original)", m)
	}
}

// covers: MA-420, R19, R29, R4, S52
// Known item K2 (a test gap, closed here): an archived page above export.ArchivedPageMaxBytes is quarantined
// unread. The preview does not read it into memory for the gate, and the run streams it as text: both
// together allocate far less than the page weighs (a read of a 32 MiB page for the gate is 32 MiB and a copy of
// it for each parse).
func TestArchiveSourceNeverReadsAPageAboveTheGatesBound(t *testing.T) {
	a := buildCEArchive(t)
	e := newCEEnv(t)
	good := a.read(mustPagePath(a, "plain-page"))
	page := append(append([]byte(nil), good...), []byte("\n<!--")...)
	page = append(page, bytes.Repeat([]byte{' '}, export.ArchivedPageMaxBytes+1-len(page)-len("-->"))...)
	page = append(page, []byte("-->")...)
	a.replacePage("plain-page", page)
	s := a.open()
	var m0, m1 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m0)
	p, res := a.planWith(e, s, []string{a.key("plain-page")}, nil)
	if c := p.Archive; c == nil || c.Quarantined != 1 {
		t.Fatalf("the preview counts %+v, want the page quarantined", c)
	}
	run := a.run(e, p, s)
	runtime.ReadMemStats(&m1)
	if run.Counts.Exported != 1 || len(res.Messages) != 1 {
		t.Fatalf("the run: %+v", run.Counts)
	}
	if got := m1.TotalAlloc - m0.TotalAlloc; got > 12<<20 {
		t.Errorf("a preview and a run of a %d MiB page allocated %d MiB: the page was read whole for the gate", len(page)>>20, got>>20)
	}
}
