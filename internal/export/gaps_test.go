package export

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/state"
)

// gapMsg is a message the way a reader hands it over; mutate adjusts it.
func gapMsg(subject, id string, mutate func(*model.Message)) *model.Message {
	m := &model.Message{Subject: subject, SenderEmail: "a@example.com", Received: testDate, InternetMessageID: id, PlainBody: "body of " + subject}
	if mutate != nil {
		mutate(m)
	}
	return m
}

func withAtt(b *blob, name string) func(*model.Message) {
	return func(m *model.Message) { m.Attachments = []model.Attachment{b.att(name)} }
}

func read(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// covers: R1, R3, S46
// An item the reader could only partly read keeps what it read, as a record that
// names the gap (§4.7.3): each ReadGaps label is recorded in the list its class
// says (unreadable:* fillable → Missing, unparsed terminal), the page's subject and
// date are stored for the report, a partly-read record carries its Locator (and
// only that record: a healthy message's record never does, P14), and the run
// counts the partly-read and unparsed records it wrote.
func TestReadGapsAreRecordedOnTheRecord(t *testing.T) {
	out := t.TempDir()
	manifest := mustManifest(t)
	e := incExporter(out, manifest)
	att := &blob{data: []byte("pdf bytes")}

	partial := gapMsg("Partly read", "<p1@x>", func(m *model.Message) {
		m.ReadGaps, m.Locator, m.ReadError = []string{state.LabelUnreadableAttachments}, "pst-nid:uid/42", "attachment table: bad checksum"
	})
	unparsed := gapMsg("Stub", "<u1@x>", func(m *model.Message) {
		m.ReadGaps, m.Locator = []string{state.LabelUnparsed}, "maildir:Inbox/u1"
	})
	healthy := gapMsg("Healthy", "<h1@x>", func(m *model.Message) { withAtt(att, "a.pdf")(m); m.Locator = "pst-nid:uid/43" })
	for _, m := range []*model.Message{partial, unparsed, healthy} {
		if _, err := e.Export("store", []string{"Inbox"}, m); err != nil {
			t.Fatal(err)
		}
	}

	rp := assure.Reached(t, mustGet(t, manifest, "store", "Inbox", partial), "the partly-read record")
	if got := strings.Join(rp.Missing, ","); got != state.LabelUnreadableAttachments || len(rp.Terminal) != 0 {
		t.Errorf("partly-read record Missing=%q Terminal=%v; want [unreadable:attachments] fillable", got, rp.Terminal)
	}
	if rp.Locator != "pst-nid:uid/42" || rp.Subject != "Partly read" || rp.Date == "" {
		t.Errorf("partly-read record %+v; want its Locator, and the subject and date the report shows", rp)
	}
	ru := mustGet(t, manifest, "store", "Inbox", unparsed)
	if strings.Join(ru.Terminal, ",") != state.LabelUnparsed || len(ru.Missing) != 0 || ru.Locator != "" {
		t.Errorf("unparsed record Missing=%v Terminal=%v Locator=%q; want terminal [unparsed] and no Locator (it is not an unreadable:* record)", ru.Missing, ru.Terminal, ru.Locator)
	}
	rh := mustGet(t, manifest, "store", "Inbox", healthy)
	if rh.Locator != "" || rh.HasIssues() || rh.Subject != "" {
		t.Errorf("a healthy message's record %+v carries a Locator or issues; healthy input must be unchanged (P14)", rh)
	}
	if e.Stats.Unreadable != 1 || e.Stats.Unparsed != 1 || e.Stats.Exported != 3 {
		t.Errorf("stats exported=%d unreadable=%d unparsed=%d; want 3, 1, 1", e.Stats.Exported, e.Stats.Unreadable, e.Stats.Unparsed)
	}
	if fill, term, _ := manifest.Counts(); fill != 1 || term != 1 {
		t.Errorf("Counts fillable=%d terminal=%d, want 1 and 1", fill, term)
	}
}

func mustGet(t *testing.T, m *state.Manifest, store, folder string, msg *model.Message) state.Record {
	t.Helper()
	r, ok := m.Get(state.Key(store, folder, msg.Identity()))
	if !ok {
		t.Fatalf("no record for %q", msg.Subject)
	}
	return r
}

// covers: R1, R3, R5, S46
// P15 (the KEEP guard): a capture that carries any read-gap label never replaces
// an archived record that does not already carry those labels. A full-mode run
// over a PST copy whose attachment table now fails, on a message archived
// complete, leaves the page and the zip byte-for-byte what their recorded Fixity
// says, writes no #fp-split sibling, stamps the record seen, counts the capture as
// Kept and records one info refresh-failed gap naming the label. The twins: the
// same capture on a message that was never archived is recorded (the partly-read
// record), and an incremental run is held back the same way.
func TestKeepGuardNeverReplacesAnArchivedRecord(t *testing.T) {
	for _, mode := range []struct {
		name string
		make func(string, *state.Manifest) *Exporter
	}{{"full", fullExporter}, {"incremental", incExporter}} {
		t.Run(mode.name, func(t *testing.T) {
			out := t.TempDir()
			manifest := mustManifest(t)
			att := &blob{data: []byte("the real attachment bytes")}
			full := func() *model.Message {
				return gapMsg("Quarterly", "<q1@x>", func(m *model.Message) { withAtt(att, "q.pdf")(m); m.Locator = "pst-nid:uid/7" })
			}
			first := incExporter(out, manifest)
			if _, err := first.Export("store", []string{"Inbox"}, full()); err != nil {
				t.Fatal(err)
			}
			rec := mustGet(t, manifest, "store", "Inbox", full())
			html, zip := filepath.Join(out, filepath.FromSlash(rec.Path)), filepath.Join(out, strings.TrimSuffix(filepath.FromSlash(rec.Path), ".html")+"-attachments.zip")
			htmlBefore, zipBefore := read(t, html), read(t, zip)

			// The same item, read again: its attachment table now fails.
			degraded := gapMsg("Quarterly", "<q1@x>", func(m *model.Message) {
				m.ReadGaps, m.Locator, m.ReadError = []string{state.LabelUnreadableAttachments}, "pst-nid:uid/7", "table unreadable"
			})
			e := mode.make(out, manifest)
			ran, err := e.Export("store", []string{"Inbox"}, degraded)
			if err != nil || ran {
				t.Fatalf("a degraded capture over an archived record: written=%v err=%v; want held back (false, nil)", ran, err)
			}
			if got := read(t, html); string(got) != string(htmlBefore) {
				t.Error("the archived page was rewritten by a degraded capture")
			}
			if got := read(t, zip); string(got) != string(zipBefore) {
				t.Error("the archived attachment zip was changed or removed by a degraded capture")
			}
			if hd := rec.Fixity.HTML; hd == nil || sha(read(t, html)) != hd.SHA256 {
				t.Error("the page no longer matches its recorded fixity")
			}
			after := mustGet(t, manifest, "store", "Inbox", degraded)
			if len(after.Missing) != 0 || len(after.Terminal) != 0 || after.Locator != "" || after.Path != rec.Path {
				t.Errorf("the archived record changed: %+v", after)
			}
			if manifest.Len() != 1 || countSuffix(t, out, ".html") != 1 {
				t.Errorf("a split sibling was minted: %d records, %d pages; want 1 and 1", manifest.Len(), countSuffix(t, out, ".html"))
			}
			if e.Stats.Kept != 1 || e.Stats.Exported != 0 || e.Stats.Unreadable != 0 {
				t.Errorf("stats kept=%d exported=%d unreadable=%d; want 1, 0, 0", e.Stats.Kept, e.Stats.Exported, e.Stats.Unreadable)
			}
			gaps := manifest.CaptureGapsSorted()
			if len(gaps) != 1 {
				t.Fatalf("want exactly one info refresh-failed gap, got %+v", gaps)
			}
			g := gaps[0]
			if g.Reason != "refresh-failed" || g.Class != state.ClassInfo || g.Scope != state.GapScopeItem || g.Token != "store" ||
				g.Locator != "ident:"+degraded.Identity() || !strings.Contains(g.Detail, state.LabelUnreadableAttachments) || g.Subject != "Quarterly" {
				t.Errorf("refresh-failed gap = %+v; want info, item, ident locator, detail naming the label", g)
			}
		})
	}

	// The twin: an item never archived is recorded, partly read, as before.
	out := t.TempDir()
	manifest := mustManifest(t)
	e := incExporter(out, manifest)
	fresh := gapMsg("Fresh", "<f1@x>", func(m *model.Message) { m.ReadGaps = []string{state.LabelUnreadableBody} })
	if ran, err := e.Export("store", []string{"Inbox"}, fresh); err != nil || !ran {
		t.Fatalf("a degraded capture of an item with no record: written=%v err=%v; want it recorded", ran, err)
	}
	if e.Stats.Kept != 0 || e.Stats.Unreadable != 1 || manifest.GapCount() != 0 {
		t.Errorf("kept=%d unreadable=%d gaps=%d; want 0, 1, 0", e.Stats.Kept, e.Stats.Unreadable, manifest.GapCount())
	}
}

// covers: R1, R3, R5, S46
// The KEEP guard on the live path: a Graph full-mode run where an archived
// message's capture now carries a read gap leaves the record, page and zip
// unchanged, adopts no immutable id, stamps the record seen (the sweep must not
// call it gone) and names the immutable id in its info gap.
func TestKeepGuardOnTheLivePath(t *testing.T) {
	out := t.TempDir()
	manifest := mustManifest(t)
	att := &blob{data: []byte("bytes")}
	live := func(e *Exporter) *Exporter { e.DedupMailboxWide = true; e.SourceComplete = true; return e }
	good := gapMsg("Live", "<l1@x>", func(m *model.Message) { withAtt(att, "l.pdf")(m); m.PhysID = "AAMk-ONE" })
	first := live(incExporter(out, manifest))
	runAt := testDate.Add(time.Hour)
	first.RunAt = runAt
	if _, err := first.Export("tok", []string{"Inbox"}, good); err != nil {
		t.Fatal(err)
	}
	key := state.LiveKey("tok", good.Identity())
	before, _ := manifest.Get(key)
	html := filepath.Join(out, filepath.FromSlash(before.Path))
	htmlBytes := read(t, html)

	later := runAt.Add(24 * time.Hour)
	bad := gapMsg("Live", "<l1@x>", func(m *model.Message) { m.ReadGaps, m.PhysID = []string{state.LabelUnparsed}, "AAMk-TWO" })
	e := live(fullExporter(out, manifest))
	e.RunAt = later
	if ran, err := e.Export("tok", []string{"Inbox"}, bad); err != nil || ran {
		t.Fatalf("written=%v err=%v; want the degraded Graph capture held back", ran, err)
	}
	after, _ := manifest.Get(key)
	if string(read(t, html)) != string(htmlBytes) || after.Path != before.Path || after.PhysID != "AAMk-ONE" || len(after.AltPhysIDs) != 0 {
		t.Errorf("the archived record changed or adopted an id: %+v", after)
	}
	if !after.LastSeen.Equal(later) || !after.Present {
		t.Errorf("the record was not stamped seen at the run's time: LastSeen %v Present %v; want %v, true", after.LastSeen, after.Present, later)
	}
	gaps := manifest.CaptureGapsSorted()
	if len(gaps) != 1 || gaps[0].Locator != "graph:AAMk-TWO" || gaps[0].Reason != "refresh-failed" {
		t.Errorf("gaps = %+v; want one refresh-failed gap at graph:<the immutable id>", gaps)
	}
	if manifest.Len() != 1 {
		t.Errorf("%d records, want 1 (no split, no adoption)", manifest.Len())
	}
}

// covers: R1, R3, S46
// Healing (§4.7.4(1)): the fingerprint of an item whose attachment table failed is
// computed over zero attachments, so a later healthy read of the same item lands on
// the partly-read record's key — one record, filled in place, no #fp-split
// "Message-ID reuse" — in either order (healthy first is the KEEP guard's).
func TestPartlyReadItemHealsInPlace(t *testing.T) {
	out := t.TempDir()
	manifest := mustManifest(t)
	att := &blob{data: []byte("the attachment")}
	degraded := gapMsg("Heal me", "<heal@x>", func(m *model.Message) {
		m.ReadGaps, m.Locator = []string{state.LabelUnreadableAttachments}, "pst-nid:uid/9"
	})
	healthy := func() *model.Message {
		return gapMsg("Heal me", "<heal@x>", func(m *model.Message) { withAtt(att, "h.pdf")(m); m.Locator = "pst-nid:uid/9" })
	}
	e1 := incExporter(out, manifest)
	if _, err := e1.Export("store", []string{"Inbox"}, degraded); err != nil {
		t.Fatal(err)
	}
	if rec := mustGet(t, manifest, "store", "Inbox", degraded); rec.Fingerprint != degraded.FingerprintNoAttachments() || rec.Locator == "" {
		t.Fatalf("the partly-read record: %+v; want a fingerprint over zero attachments and its Locator", rec)
	}
	e2 := incExporter(out, manifest)
	if ran, err := e2.Export("store", []string{"Inbox"}, healthy()); err != nil || !ran {
		t.Fatalf("the healthy read: written=%v err=%v; want it to heal the record", ran, err)
	}
	if manifest.Len() != 1 || countSuffix(t, out, ".html") != 1 {
		t.Fatalf("%d records and %d pages after healing; want one of each (a #fp duplicate was minted)", manifest.Len(), countSuffix(t, out, ".html"))
	}
	rec := mustGet(t, manifest, "store", "Inbox", healthy())
	if rec.HasIssues() || rec.Locator != "" || e2.Stats.Filled != 1 || countSuffix(t, out, "-attachments.zip") != 1 {
		t.Errorf("healed record %+v filled=%d zips=%d; want complete, no Locator, filled once, its zip written", rec, e2.Stats.Filled, countSuffix(t, out, "-attachments.zip"))
	}
}

// covers: R1, R3, R5, S46
// Healing, metadata only (§4.7.4(3)): where the identity includes the body (no
// Message-ID), a healthy read of a partly-read item lands on a DIFFERENT key. The
// partly-read record is then re-classed to the info label "superseded" with
// RecoveredBy set to the new key, in one record update; no file is written,
// rendered or deleted, so its page still matches its fixity. A locator of another
// store (another PST uid) never matches.
func TestHealthyReadSupersedesAPartlyReadRecordWithoutTouchingFiles(t *testing.T) {
	out := t.TempDir()
	manifest := mustManifest(t)
	partial := &model.Message{Subject: "No id", SenderEmail: "a@example.com", Received: testDate,
		ReadGaps: []string{state.LabelUnreadableBody}, Locator: "pst-nid:uid-A/5"}
	if _, err := incExporter(out, manifest).Export("store", []string{"Inbox"}, partial); err != nil {
		t.Fatal(err)
	}
	pKey := state.Key("store", "Inbox", partial.Identity())
	pRec, _ := manifest.Get(pKey)
	pHTML := filepath.Join(out, filepath.FromSlash(pRec.Path))
	pBytes := read(t, pHTML)
	pStat := mtime(t, pHTML)

	// Another store's item at the same NID is not the same item.
	other := &model.Message{Subject: "No id", SenderEmail: "a@example.com", Received: testDate, PlainBody: "real text", Locator: "pst-nid:uid-B/5"}
	if _, err := incExporter(out, manifest).Export("store", []string{"Inbox"}, other); err != nil {
		t.Fatal(err)
	}
	if r, _ := manifest.Get(pKey); r.RecoveredBy != "" || !r.Unreadable() {
		t.Fatalf("a read from another store superseded the partly-read record: %+v", r)
	}

	healthy := &model.Message{Subject: "No id", SenderEmail: "a@example.com", Received: testDate, PlainBody: "the body arrived", Locator: "pst-nid:uid-A/5"}
	hKey := state.Key("store", "Inbox", healthy.Identity())
	if hKey == pKey {
		t.Fatal("fixture: the healthy read must land on a different key")
	}
	files := countSuffix(t, out, ".html")
	if _, err := incExporter(out, manifest).Export("store", []string{"Inbox"}, healthy); err != nil {
		t.Fatal(err)
	}
	if n := countSuffix(t, out, ".html"); n != files+1 {
		t.Fatalf("%d pages, want %d: exactly the healthy read's page is new", n, files+1)
	}
	after, _ := manifest.Get(pKey)
	if after.Unreadable() || strings.Join(after.Terminal, ",") != state.LabelSuperseded || after.RecoveredBy != hKey || after.Locator != "" || len(after.Missing) != 0 {
		t.Errorf("the partly-read record after the healthy read: %+v; want superseded (info), RecoveredBy %q, no Locator", after, hKey)
	}
	if string(read(t, pHTML)) != string(pBytes) || !mtime(t, pHTML).Equal(pStat) || sha(read(t, pHTML)) != pRec.Fixity.HTML.SHA256 {
		t.Error("supersede wrote to the partly-read record's page; it must be metadata only")
	}
	if fill, term, _ := manifest.Counts(); fill != 0 || term != 0 {
		t.Errorf("Counts fillable=%d terminal=%d; a superseded record is nothing missing", fill, term)
	}
}

// covers: R1, S46
// probeImproves: an unreadable:* or unparsed label improves when the new read no
// longer carries it (§4.7.4(2)); a read that still carries it improves nothing
// (the probe never renders or writes).
func TestProbeImprovesForReadGapLabels(t *testing.T) {
	still := &model.Message{PlainBody: "x", ReadGaps: []string{state.LabelUnreadableAttachments}}
	healed := &model.Message{PlainBody: "x"}
	for _, label := range []string{state.LabelUnreadableAttachments, state.LabelUnreadableBody, state.LabelUnparsed} {
		stillThis := &model.Message{PlainBody: "x", ReadGaps: []string{label}}
		if probeImproves(stillThis, []string{label}) {
			t.Errorf("a read that still carries %s improved", label)
		}
		if !probeImproves(healed, []string{label}) {
			t.Errorf("a read without %s did not improve it", label)
		}
	}
	if probeImproves(still, []string{state.LabelUnreadableBody}) == false {
		t.Error("a read that lost unreadable:body but carries unreadable:attachments must improve the unreadable:body record")
	}
	if !probeImproves(healed, []string{state.UnknownSentinel}) || probeImproves(&model.Message{}, []string{state.MissingBody}) {
		t.Error("the existing probe cases changed")
	}
}

// covers: R1, S46
// A partly-read record re-examined and still incomplete rewrites nothing, counts
// as a partly-read record seen this run (the run is partial), and restores a
// Locator that an older binary's save dropped (§4.2).
func TestStillPartlyReadRestoresLocator(t *testing.T) {
	out := t.TempDir()
	manifest := mustManifest(t)
	m := func() *model.Message {
		return gapMsg("Still", "<s@x>", func(m *model.Message) {
			m.ReadGaps, m.Locator = []string{state.LabelUnreadableBody}, "pst-nid:uid/3"
		})
	}
	if _, err := incExporter(out, manifest).Export("store", []string{"Inbox"}, m()); err != nil {
		t.Fatal(err)
	}
	key := state.Key("store", "Inbox", m().Identity())
	rec, _ := manifest.Get(key)
	rec.Locator = "" // what an older binary's save leaves
	manifest.Add(key, rec)

	e := incExporter(out, manifest)
	if ran, err := e.Export("store", []string{"Inbox"}, m()); err != nil || ran {
		t.Fatalf("written=%v err=%v; want a re-examine that writes nothing", ran, err)
	}
	if e.Stats.Retried != 1 || e.Stats.StillIncomplete != 1 || e.Stats.Unreadable != 1 || e.Stats.Exported != 0 {
		t.Errorf("stats %+v; want retried 1, still-incomplete 1, unreadable 1, exported 0", e.Stats)
	}
	if got, _ := manifest.Get(key); got.Locator != "pst-nid:uid/3" {
		t.Errorf("Locator after the re-examine = %q, want it restored", got.Locator)
	}
}

// covers: R1, R7, R19, S46
// The record notice (§4.7.5): a page for a message that carries read gaps says so,
// inside the tool's own header, in fixed escaped text naming what could not be read,
// the source, the folder and the reason; every interpolated value is escaped and
// control-stripped; an unparsed page says where the full bytes are; a healthy page
// has no notice; and the page keeps the layout reindex -rebuild anchors on.
func TestRecordNoticeOnAPartlyReadPage(t *testing.T) {
	m := gapMsg("Partly", "<n@x>", func(m *model.Message) {
		m.ReadGaps = []string{state.LabelUnreadableAttachments, state.LabelUnreadableBody}
		m.ReadError = "<script>alert(1)</script>\x1b[2J broken & \"torn\""
	})
	rr, err := RenderWith(m, RenderContext{Store: "old-pst", Folder: "Inbox/<b>2009</b>"})
	if err != nil {
		t.Fatal(err)
	}
	page := string(rr.HTML)
	i := strings.Index(page, `<div class="mailarchive-notice">`)
	if i < 0 {
		t.Fatalf("no notice on a page whose message carries ReadGaps")
	}
	end := strings.Index(page[i:], "</div>")
	notice := page[i : i+end]
	for _, want := range []string{"could only be partly read from the source", "its attachments", "its body", "Source: old-pst, folder Inbox/&lt;b&gt;2009&lt;/b&gt;", "Reason: &lt;script&gt;alert(1)&lt;/script&gt;"} {
		if !strings.Contains(notice, want) {
			t.Errorf("the notice lacks %q:\n%s", want, notice)
		}
	}
	if strings.Contains(notice, "<script") || strings.Contains(notice, "\x1b") || strings.Contains(notice, "<b>") {
		t.Errorf("the notice carries untrusted markup or a control character:\n%q", notice)
	}
	if rr.BodyFallback != "" {
		t.Errorf("the notice broke the page's layout: BodyFallback %q", rr.BodyFallback)
	}
	if !strings.Contains(page[:strings.Index(page, `<div class="mailarchive-body"`)], "mailarchive-notice") {
		t.Error("the notice is outside the tool's header")
	}

	stub, _ := RenderWith(gapMsg("Stub", "<s@x>", func(m *model.Message) { m.ReadGaps = []string{state.LabelUnparsed} }), RenderContext{Store: "s", Folder: "f"})
	if !strings.Contains(string(stub.HTML), "-raw") || !strings.Contains(string(stub.HTML), "structure (shown here as text)") {
		t.Error("an unparsed page's notice must say what was not read and where the full bytes are (the .eml under -raw)")
	}
	clean, _ := RenderWith(gapMsg("Clean", "<c@x>", nil), RenderContext{Store: "s", Folder: "f"})
	if strings.Contains(string(clean.HTML), "mailarchive-notice") {
		t.Error("a healthy page carries a notice")
	}
}

func sha(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
