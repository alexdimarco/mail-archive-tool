package export

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/state"
)

var testDate = time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

// blob is an attachment whose bytes the test can change between runs — the
// on-demand IMAP shape: declared now, downloaded later.
type blob struct{ data []byte }

func (b *blob) att(name string) model.Attachment {
	return model.Attachment{Filename: name, WriteTo: func(w io.Writer) (int64, error) {
		n, err := w.Write(b.data)
		return int64(n), err
	}}
}

func incExporter(out string, m *state.Manifest) *Exporter {
	return &Exporter{OutDir: out, Manifest: m, Mode: Incremental, Log: log.New(io.Discard, "", 0)}
}

func mtime(t *testing.T, p string) time.Time {
	t.Helper()
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return fi.ModTime()
}

// covers: MA-66, R1, R2, S6
// A capture missing content is recorded in the manifest as a fillable gap naming
// the item; an incremental re-run re-examines it (even under a -since window that
// would exclude the message) and rewrites NOTHING while the source still lacks
// the content (R2: zero exports over an unchanged source); once the content is
// there the entry is filled (html + zip rewritten, record complete) and never
// re-examined again. Growth is bounded: subject/date live only on records with
// issues.
func TestIncrementalFillsGaps(t *testing.T) {
	out := t.TempDir()
	manifest := mustManifest(t)
	pdf := &blob{} // not downloaded yet
	msg := func() *model.Message {
		return &model.Message{Subject: "Report", Received: testDate, InternetMessageID: "<r@x>",
			HTMLBody: "<p>hi</p>", Attachments: []model.Attachment{pdf.att("report.pdf")}}
	}
	key := state.Key("store", "Inbox", msg().Identity())

	// Run 1: the attachment is empty → exported, recorded fillable, no zip.
	e1 := incExporter(out, manifest)
	if _, err := e1.Export("store", []string{"Inbox"}, msg()); err != nil {
		t.Fatal(err)
	}
	rec, ok := manifest.Get(key)
	if !ok || !rec.Fillable() || rec.Complete() || strings.Join(rec.Missing, ",") != "report.pdf" {
		t.Fatalf("run 1 record = %+v (ok=%v), want fillable Missing=[report.pdf]", rec, ok)
	}
	if rec.Subject != "Report" || rec.Date == "" {
		t.Errorf("an incomplete record must carry subject/date for the report: %+v", rec)
	}
	if n := countSuffix(t, out, "-attachments.zip"); n != 0 {
		t.Fatalf("empty attachment produced a zip")
	}
	htmlPath := filepath.Join(out, filepath.FromSlash(rec.Path))
	before := mtime(t, htmlPath)
	time.Sleep(20 * time.Millisecond)

	// Run 2: still not downloaded → re-examined, nothing rewritten.
	e2 := incExporter(out, manifest)
	if _, err := e2.Export("store", []string{"Inbox"}, msg()); err != nil {
		t.Fatal(err)
	}
	if e2.Stats.Exported != 0 || e2.Stats.Retried != 1 || e2.Stats.StillIncomplete != 1 || e2.Stats.Filled != 0 {
		t.Errorf("run 2 stats = %+v, want exported=0 retried=1 still=1 filled=0", e2.Stats)
	}
	if !mtime(t, htmlPath).Equal(before) {
		t.Error("run 2 rewrote the html although nothing improved")
	}

	// Run 2b: a -since window that excludes the message does not stop the retry (B7).
	e2b := incExporter(out, manifest)
	e2b.Since = testDate.Add(48 * time.Hour)
	if _, err := e2b.Export("store", []string{"Inbox"}, msg()); err != nil {
		t.Fatal(err)
	}
	if e2b.Stats.Retried != 1 || e2b.Stats.SkippedDate != 0 {
		t.Errorf("run 2b stats = %+v, want retried=1 skipped(date)=0", e2b.Stats)
	}

	// Run 3: the content arrived → filled.
	pdf.data = []byte("PDFDATA")
	e3 := incExporter(out, manifest)
	if _, err := e3.Export("store", []string{"Inbox"}, msg()); err != nil {
		t.Fatal(err)
	}
	if e3.Stats.Exported != 1 || e3.Stats.Filled != 1 || e3.Stats.Retried != 1 {
		t.Errorf("run 3 stats = %+v, want exported=1 filled=1 retried=1", e3.Stats)
	}
	if n := countSuffix(t, out, "-attachments.zip"); n != 1 {
		t.Errorf("filled message has %d zips, want 1", n)
	}
	rec, _ = manifest.Get(key)
	if !rec.Complete() || rec.Fillable() || rec.Subject != "" || rec.Date != "" {
		t.Errorf("run 3 record = %+v, want complete with no subject/date", rec)
	}

	// Run 4: complete → plain skip.
	e4 := incExporter(out, manifest)
	if _, err := e4.Export("store", []string{"Inbox"}, msg()); err != nil {
		t.Fatal(err)
	}
	if e4.Stats.SkippedManifest != 1 || e4.Stats.Retried != 0 {
		t.Errorf("run 4 stats = %+v, want skipped=1 retried=0", e4.Stats)
	}
}

// covers: MA-66, R1, S6
// Promotion is a set relation, not a count: when any previously-missing item is
// now present the retry is committed and the record carries the freshly computed
// set — even if another item regressed meanwhile — and a retry that recovers
// nothing is discarded.
func TestRetryPromotesOnAnyRecoveredItem(t *testing.T) {
	out := t.TempDir()
	manifest := mustManifest(t)
	body := ""
	att := &blob{data: []byte("present")}
	msg := func() *model.Message {
		return &model.Message{Subject: "Mixed", Received: testDate, InternetMessageID: "<mx@x>",
			HTMLBody: body, Attachments: []model.Attachment{att.att("a.txt")}}
	}
	key := state.Key("store", "Inbox", msg().Identity())

	e1 := incExporter(out, manifest)
	if _, err := e1.Export("store", []string{"Inbox"}, msg()); err != nil {
		t.Fatal(err)
	}
	if rec, _ := manifest.Get(key); strings.Join(rec.Missing, ",") != "body" {
		t.Fatalf("run 1 Missing = %v, want [body]", rec.Missing)
	}

	// The body arrives but the attachment is now empty: old {body} ⊄ new {a.txt} → promote.
	body, att.data = "<p>now here</p>", nil
	e2 := incExporter(out, manifest)
	if _, err := e2.Export("store", []string{"Inbox"}, msg()); err != nil {
		t.Fatal(err)
	}
	rec, _ := manifest.Get(key)
	if e2.Stats.Filled != 1 || strings.Join(rec.Missing, ",") != "a.txt" {
		t.Errorf("run 2: filled=%d Missing=%v, want filled=1 Missing=[a.txt]", e2.Stats.Filled, rec.Missing)
	}
	data, _ := os.ReadFile(filepath.Join(out, filepath.FromSlash(rec.Path)))
	if !strings.Contains(string(data), "now here") {
		t.Error("run 2 did not write the recovered body")
	}

	// Nothing recovered (old {a.txt} ⊆ new {a.txt}) → discarded.
	e3 := incExporter(out, manifest)
	if _, err := e3.Export("store", []string{"Inbox"}, msg()); err != nil {
		t.Fatal(err)
	}
	if e3.Stats.Filled != 0 || e3.Stats.StillIncomplete != 1 {
		t.Errorf("run 3 stats = %+v, want filled=0 still=1", e3.Stats)
	}
}

// covers: MA-67, R1, S6
// A missing body is a fillable gap ("body") that fills when the body appears; a
// message with a body and no attachments is complete; and a source that is
// complete-at-fetch (SourceComplete, e.g. Graph) records its gaps as TERMINAL:
// reported, never fillable, never re-examined.
func TestMissingBodyAndTerminalClass(t *testing.T) {
	out := t.TempDir()
	manifest := mustManifest(t)

	// A. No body at all from an on-demand source.
	body := ""
	msg := func() *model.Message {
		return &model.Message{Subject: "Empty", Received: testDate, InternetMessageID: "<e@x>", PlainBody: body}
	}
	key := state.Key("store", "Inbox", msg().Identity())
	e1 := incExporter(out, manifest)
	if _, err := e1.Export("store", []string{"Inbox"}, msg()); err != nil {
		t.Fatal(err)
	}
	if rec, _ := manifest.Get(key); strings.Join(rec.Missing, ",") != "body" || e1.Stats.NoBody != 1 {
		t.Fatalf("A: record=%+v stats=%+v, want Missing=[body] NoBody=1", rec, e1.Stats)
	}
	body = "the body arrived"
	e2 := incExporter(out, manifest)
	if _, err := e2.Export("store", []string{"Inbox"}, msg()); err != nil {
		t.Fatal(err)
	}
	if rec, _ := manifest.Get(key); e2.Stats.Filled != 1 || !rec.Complete() {
		t.Errorf("A: body did not fill: stats=%+v record=%+v", e2.Stats, rec)
	}

	// B. A body and no attachments is complete on first capture.
	ok := &model.Message{Subject: "Fine", Received: testDate, InternetMessageID: "<f@x>", PlainBody: "hello"}
	if _, err := e2.Export("store", []string{"Inbox"}, ok); err != nil {
		t.Fatal(err)
	}
	if rec, _ := manifest.Get(state.Key("store", "Inbox", ok.Identity())); !rec.Complete() {
		t.Errorf("B: complete message recorded as %+v", rec)
	}

	// C. The same empty message from a complete-at-fetch source is terminal.
	term := incExporter(out, manifest)
	term.SourceComplete = true
	tm := &model.Message{Subject: "Receipt", Received: testDate, InternetMessageID: "<t@x>"}
	if _, err := term.Export("graph", []string{"Inbox"}, tm); err != nil {
		t.Fatal(err)
	}
	tkey := state.Key("graph", "Inbox", tm.Identity())
	rec, _ := manifest.Get(tkey)
	if strings.Join(rec.Terminal, ",") != "body" || rec.Fillable() || len(rec.Missing) != 0 {
		t.Fatalf("C: record=%+v, want Terminal=[body] and not fillable", rec)
	}
	again := incExporter(out, manifest)
	again.SourceComplete = true
	if _, err := again.Export("graph", []string{"Inbox"}, tm); err != nil {
		t.Fatal(err)
	}
	if again.Stats.SkippedManifest != 1 || again.Stats.Retried != 0 {
		t.Errorf("C: terminal entry was re-examined: %+v", again.Stats)
	}
}

// covers: MA-70, MA-66, R1
// A legacy record (migrated to the "unknown" sentinel) is re-captured once by the
// next incremental run and then carries the truth.
func TestUnknownSentinelIsResolvedByRecapture(t *testing.T) {
	out := t.TempDir()
	manifest := mustManifest(t)
	m := &model.Message{Subject: "Old", Received: testDate, InternetMessageID: "<old@x>", PlainBody: "text"}
	key := state.Key("store", "Inbox", m.Identity())
	manifest.Add(key, state.Record{Path: "store/Inbox/old.html", Folder: "Inbox", Missing: []string{"unknown"}})

	e := incExporter(out, manifest)
	if _, err := e.Export("store", []string{"Inbox"}, m); err != nil {
		t.Fatal(err)
	}
	rec, _ := manifest.Get(key)
	if e.Stats.Filled != 1 || !rec.Complete() {
		t.Errorf("sentinel not resolved: stats=%+v record=%+v", e.Stats, rec)
	}
	if n := countSuffix(t, out, ".html"); n != 1 {
		t.Errorf("re-capture wrote %d html files, want 1", n)
	}
}
