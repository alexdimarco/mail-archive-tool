package app

import (
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/model"
)

// covers: MA-480, R13, R8, S33
// Pages written by v0.1.1–v0.2.3 stamp the header's Date row with the zone's
// ABBREVIATION ("Wed, 02 Apr 2025 10:30:00 UTC"), where later releases write a
// numeric offset. `reindex -rebuild` reads that stamp back, so an old record
// keeps its date (and date-bounded search still finds it) — on the fragment
// template and on a kept full document alike. An abbreviation that names no
// known offset is counted unrecovered, never indexed as a guess hours off. A
// numeric offset (a zone with no name) and today's stamp read as before.
func TestReadArchivedHTMLRecoversLegacyDateStamp(t *testing.T) {
	when := time.Date(2025, 4, 2, 10, 30, 0, 0, time.UTC)
	msg := func(subject string, at time.Time, body string) *model.Message {
		return &model.Message{Subject: subject, SenderName: "Old Sender", SenderEmail: "old@example.com",
			To: "rcpt@example.com", Received: at, HTMLBody: body}
	}
	frag := msg("legacy fragment", when, "<p>fragment body</p>")
	full := msg("legacy full document", when, "<html><head></head><body><p>full body</p></body></html>")

	for _, m := range []*model.Message{frag, full} {
		page := renderV023(m)
		if !strings.Contains(string(page), "<dd>Wed, 02 Apr 2025 10:30:00 UTC</dd>") {
			t.Fatalf("fixture sanity: %q is not a v0.2.3 page with an abbreviated stamp:\n%s", m.Subject, page)
		}
		got, unrecovered := readArchivedHTML(page)
		if !got.Received.Equal(when) || unrecovered != 0 {
			t.Errorf("%q: date = %v (unrecovered=%d), want %v and 0 — the v0.2 stamp was not read back", m.Subject, got.Received, unrecovered, when)
		}
		if got.Subject != m.Subject || got.SenderEmail != "old@example.com" {
			t.Errorf("%q: subject=%q sender=%q, want the page's own header", m.Subject, got.Subject, got.SenderEmail)
		}
	}

	// A zone with no name was written as a bare offset: read exactly.
	offset := msg("legacy offset", time.Date(2025, 4, 2, 12, 30, 0, 0, time.FixedZone("", 2*3600)), "<p>x</p>")
	if page := renderV023(offset); !strings.Contains(string(page), "+0200</dd>") {
		t.Fatalf("fixture sanity: no numeric offset in %s", page)
	} else if got, u := readArchivedHTML(page); !got.Received.Equal(when) || u != 0 {
		t.Errorf("numeric-offset stamp: date = %v (unrecovered=%d), want %v", got.Received, u, when)
	}

	// An abbreviation with no known offset is honest-unrecovered, not a guess.
	odd := msg("legacy odd zone", time.Date(2025, 4, 2, 15, 30, 0, 0, time.FixedZone("ZZT", 5*3600)), "<p>x</p>")
	if page := renderV023(odd); !strings.Contains(string(page), "15:30:00 ZZT</dd>") {
		t.Fatalf("fixture sanity: no ZZT stamp in %s", page)
	} else if got, u := readArchivedHTML(page); !got.Received.IsZero() || u != 1 {
		t.Errorf("unknown abbreviation: date = %v (unrecovered=%d), want zero and 1 (counted, never guessed at offset 0)", got.Received, u)
	}

	// Positive twin: today's renderer's stamp still reads.
	cur, _, err := export.Render(msg("current page", when, "<p>x</p>"))
	if err != nil {
		t.Fatal(err)
	}
	if got, u := readArchivedHTML(cur); !got.Received.Equal(when) || u != 0 {
		t.Errorf("current page: date = %v (unrecovered=%d), want %v", got.Received, u, when)
	}

	// End to end: the rebuilt index carries the date, so a date-bounded search
	// finds the old record.
	out := tmpDir(t)
	buildOnDiskArchive(t, out, []diskPage{{frag, renderV023(frag)}})
	rep := rebuildOnDisk(t, out, 1)
	if rep.Unrecovered != 0 {
		t.Errorf("rebuild unrecovered-fields = %d, want 0", rep.Unrecovered)
	}
	r := firstResult(t, out, "fragment")
	if !r.Date.Equal(when) {
		t.Errorf("rebuilt record date = %v, want %v", r.Date, when)
	}
	assure.Reached(t, searchTotal(t, out, "after:2025-04-02 before:2025-04-03 fragment"), "date-bounded hits for the old record")
}
