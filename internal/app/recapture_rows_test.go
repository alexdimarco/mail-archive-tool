package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/state"
)

// Re-capture integrity (docs/design-recapture-integrity.md §9, §10), slice S1,
// interim: the catalog rows of the confirmed-open gaps (F46, N2, N3, F48 and
// §1.5-§1.7) land as skip encoders, each citing the existing invariant its defect
// breaks. S1 changes no product code, so none of them can pass yet. Each names the
// finding and the slice that turns it into a permanent guard; its covering marker
// stays on it until then. The rows that specify new behaviour (MA-353, MA-354,
// MA-360, MA-362..MA-364) land with the slice that builds them (RC32).

// covers: MA-350, R28, R1, S50
// F46: a full run over an on-demand local source whose same-name attachment is now
// zero bytes keeps the archived page, zip and original as recorded. Today the run
// commits the degraded capture: it deletes the zip, overwrites the original with the
// degraded bytes and lists the attachment as a fillable gap. The row's kept
// assertions (Kept, Stats.Kept, the report row, the summary line) are added by the
// slice that builds them. The healthy twin runs first and is green today; the gap
// half runs without the skip to show it red.
func TestFullRunKeepsAnAttachmentTheSourceNowHoldsEmpty(t *testing.T) {
	src, out := rcSource(t), tmpDir(t)
	good := rcMail("rep@x", "Report", "<p>the quarterly report</p>", "report.pdf", []byte("%PDF-1.4 the report"))
	rcPut(t, src, good)
	rcRun(t, src, out, export.Full, true, nil)
	_, rec1 := recordForID(t, out, "rep@x")
	if !rec1.Complete() || rec1.Fixity == nil || rec1.Fixity.Zip == nil || rec1.Fixity.EML == nil {
		t.Fatalf("the first capture should be complete, with a page, a zip and an original on record: %+v", rec1)
	}

	// Positive twin: a full run over the intact source leaves what was recorded.
	rcRun(t, src, out, export.Full, true, nil)
	_, twin := recordForID(t, out, "rep@x")
	if bad := rcAsRecorded(t, out, rec1); len(bad) != 0 || !twin.Complete() || twin.Path != rec1.Path {
		t.Fatalf("a full run over an intact source changed the archive: %v (record %+v)", bad, twin)
	}

	t.Skip("MA-350 (F46, R1): a full run commits a degraded capture over a good archived copy and deletes its zip; encoder lands in RI-S1, turns green in RI-S5")

	// The attachment is now declared but delivers nothing.
	rcPut(t, src, rcMail("rep@x", "Report", "<p>the quarterly report</p>", "report.pdf", nil))
	rcRun(t, src, out, export.Full, true, nil)
	_, rec2 := recordForID(t, out, "rep@x")
	for _, bad := range rcAsRecorded(t, out, rec1) {
		t.Errorf("a full run over a source that holds less changed the archive: %s", bad)
	}
	if rcHas(rec2.Missing, "report.pdf") || rcHas(rec2.Terminal, "report.pdf") {
		t.Errorf("the held attachment is listed as a gap after the run: missing=%v terminal=%v", rec2.Missing, rec2.Terminal)
	}
	if rep := rcVerify(t, out); !rep.Attested() {
		t.Errorf("verify does not attest the archive after the run: %+v", rep.Problems)
	}
}

// covers: MA-351, R28, R1, S50
// F46: a body that is now missing is kept in a full run, and in a fill retry the
// verdict is taken at the probe, before any temp file is written. The row's hook
// counter (no temp file is ever created, no held attachment is streamed by a
// non-improving probe) needs the probe's seam, which RI-S5 adds.
func TestFullRunAndFillRetryKeepABodyTheSourceNowLacks(t *testing.T) {
	// Twin of the fill retry: the body is there and the attachment arrives, so the
	// retry commits and the record is complete.
	srcT, outT := rcSource(t), tmpDir(t)
	rcPut(t, srcT, rcMail("fill@x", "Invoice", "<p>invoice text</p>", "invoice.pdf", nil))
	rcRun(t, srcT, outT, export.Incremental, false, nil)
	if _, r := recordForID(t, outT, "fill@x"); strings.Join(r.Missing, ",") != "invoice.pdf" {
		t.Fatalf("the first capture should record the undelivered attachment as fillable: %+v", r)
	}
	rcPut(t, srcT, rcMail("fill@x", "Invoice", "<p>invoice text</p>", "invoice.pdf", []byte("%PDF invoice")))
	if res := rcRun(t, srcT, outT, export.Incremental, false, nil); res.Stats.Filled != 1 {
		t.Fatalf("the retry that finds the attachment should fill it: %+v", res.Stats)
	}
	if _, r := recordForID(t, outT, "fill@x"); !r.Complete() {
		t.Fatalf("the filled record should be complete: %+v", r)
	}

	// Twin of the full run: the body is present on both passes.
	srcF, outF := rcSource(t), tmpDir(t)
	rcPut(t, srcF, rcMail("body@x", "Minutes", "<p>the minutes</p>", "", nil))
	rcRun(t, srcF, outF, export.Full, false, nil)
	_, full1 := recordForID(t, outF, "body@x")
	rcRun(t, srcF, outF, export.Full, false, nil)
	if bad := rcAsRecorded(t, outF, full1); len(bad) != 0 {
		t.Fatalf("a full run over an intact source changed the archive: %v", bad)
	}

	t.Skip("MA-351 (F46, R1): a full run and a fill retry overwrite an archived body with an empty one; encoder lands in RI-S1, turns green in RI-S5")

	// Full mode: the same message, its body now empty.
	rcPut(t, srcF, rcMail("body@x", "Minutes", "", "", nil))
	rcRun(t, srcF, outF, export.Full, false, nil)
	_, full2 := recordForID(t, outF, "body@x")
	for _, bad := range rcAsRecorded(t, outF, full1) {
		t.Errorf("a full run over a source with no body changed the archive: %s", bad)
	}
	if rcHas(full2.Missing, state.MissingBody) {
		t.Errorf("the held body is listed as missing after the full run: %v", full2.Missing)
	}

	// Fill retry: the attachment arrived, the body vanished.
	srcR, outR := rcSource(t), tmpDir(t)
	rcPut(t, srcR, rcMail("fill@x", "Invoice", "<p>invoice text</p>", "invoice.pdf", nil))
	rcRun(t, srcR, outR, export.Incremental, false, nil)
	_, fill1 := recordForID(t, outR, "fill@x")
	rcPut(t, srcR, rcMail("fill@x", "Invoice", "", "invoice.pdf", []byte("%PDF invoice")))
	rcRun(t, srcR, outR, export.Incremental, false, nil)
	_, fill2 := recordForID(t, outR, "fill@x")
	for _, bad := range rcAsRecorded(t, outR, fill1) {
		t.Errorf("a fill retry that found the body gone changed the archive: %s", bad)
	}
	if rcHas(fill2.Missing, state.MissingBody) {
		t.Errorf("the held body is listed as missing after the retry: %v", fill2.Missing)
	}
}

// covers: MA-352, R28, R1, S50
// F46: an attachment stream that now errors is kept in a full run, and a healthy
// stream on the next full run heals the record. The `unreadable` wording of the
// KEEP is the row's own and lands with the summary.
func TestFullRunKeepsAnAttachmentWhoseStreamNowErrors(t *testing.T) {
	out := tmpDir(t)
	pdf := &rcBlob{data: []byte("%PDF-1.4 the report, whole")}
	msg := func() *model.Message {
		return rcMsg("tear@x", "Report", "<p>the report</p>", nil, pdf.att("a.pdf"))
	}
	if _, err := rcExport(t, out, export.Full, false, msg()); err != nil {
		t.Fatal(err)
	}
	_, rec1 := recordForID(t, out, "tear@x")
	if !rec1.Complete() || rec1.Fixity == nil || rec1.Fixity.Zip == nil {
		t.Fatalf("the first capture should be complete, with a zip on record: %+v", rec1)
	}

	// Twin: a healthy stream throughout leaves what was recorded.
	if _, err := rcExport(t, out, export.Full, false, msg()); err != nil {
		t.Fatal(err)
	}
	if bad := rcAsRecorded(t, out, rec1); len(bad) != 0 {
		t.Fatalf("a full run over a healthy stream changed the archive: %v", bad)
	}

	t.Skip("MA-352 (F46, R1): a torn attachment read in a full run removes the archived zip and records a fillable gap; encoder lands in RI-S1, turns green in RI-S5")

	pdf.err = errors.New("read error mid-stream")
	if _, err := rcExport(t, out, export.Full, false, msg()); err != nil {
		t.Fatal(err)
	}
	_, rec2 := recordForID(t, out, "tear@x")
	for _, bad := range rcAsRecorded(t, out, rec1) {
		t.Errorf("a full run over a torn stream changed the archive: %s", bad)
	}
	if rcHas(rec2.Missing, "a.pdf") {
		t.Errorf("the held attachment is listed as missing after the torn read: %v", rec2.Missing)
	}

	// The fault was transient: the next full run with a healthy stream is complete.
	pdf.err = nil
	if _, err := rcExport(t, out, export.Full, false, msg()); err != nil {
		t.Fatal(err)
	}
	_, rec3 := recordForID(t, out, "tear@x")
	if !rec3.Complete() || len(rcAsRecorded(t, out, rec3)) != 0 {
		t.Errorf("the next full run did not heal the record: %+v", rec3)
	}
}

// covers: MA-355, R28, R1, R5, S50, S6
// F46 on legacy records: a re-examination that would degrade leaves the files as
// archived, and a legacy record is never claimed complete or fillable for ever.
// Cases (a) a v6 record with an equal fingerprint, (b) a version-1 `unknown`
// record, (c) `unknown` with an inline image in the earlier page, and the twin (d)
// `unknown` over an intact source. The UNVERIFIED field, its report row and the
// presumed wording are the row's own, added with the slice that builds them.
func TestLegacyReExaminationKeepsWhatItWouldDegrade(t *testing.T) {
	// (a) twin: a full run over an intact source leaves a v6 record as recorded.
	outA := tmpDir(t)
	pdfA := &rcBlob{data: []byte("%PDF-1.4 legacy a")}
	msgA := func() *model.Message { return rcMsg("lega@x", "Old A", "<p>old a</p>", nil, pdfA.att("old.pdf")) }
	if _, err := rcExport(t, outA, export.Full, false, msgA()); err != nil {
		t.Fatal(err)
	}
	_, recA := recordForID(t, outA, "lega@x")
	if _, err := rcExport(t, outA, export.Full, false, msgA()); err != nil {
		t.Fatal(err)
	}
	if bad := rcAsRecorded(t, outA, recA); len(bad) != 0 || recA.Fingerprint == "" {
		t.Fatalf("a full run over an intact source changed a v6 record's files: %v", bad)
	}

	// (d) twin: an `unknown` record over an intact source commits, with real gaps
	// (here none) recorded.
	outD := tmpDir(t)
	pdfD := &rcBlob{data: []byte("%PDF-1.4 legacy d")}
	msgD := func() *model.Message { return rcMsg("legd@x", "Old D", "<p>old d</p>", nil, pdfD.att("old.pdf")) }
	if _, err := rcExport(t, outD, export.Incremental, false, msgD()); err != nil {
		t.Fatal(err)
	}
	rcLegacy(t, outD, "legd@x")
	if st, err := rcExport(t, outD, export.Incremental, false, msgD()); err != nil || st.Retried != 1 {
		t.Fatalf("the re-examination of an unknown record over an intact source: stats %+v, err %v", st, err)
	}
	if _, r := recordForID(t, outD, "legd@x"); !r.Complete() || r.Unknown() || len(rcAsRecorded(t, outD, r)) != 0 {
		t.Fatalf("an unknown record over an intact source should resolve complete with fixity: %+v", r)
	}

	// (c) twin: the inline image is embedded in the page while the source delivers it.
	outC := tmpDir(t)
	logo := &rcBlob{data: rcPNG, cid: "logo", ctype: "image/png"}
	msgC := func() *model.Message {
		return rcMsg("legc@x", "Old C", `<p>logo</p><img src="cid:logo">`, nil, logo.att("logo.png"))
	}
	if _, err := rcExport(t, outC, export.Incremental, false, msgC()); err != nil {
		t.Fatal(err)
	}
	_, recC := recordForID(t, outC, "legc@x")
	pageC := rcRead(t, outC, recC.Path)
	if !strings.Contains(string(pageC), "data:image/png;base64") {
		t.Fatalf("the first capture should embed the inline image in the page")
	}
	rcLegacy(t, outC, "legc@x")

	// (b) setup: an `unknown` record over files it has no fixity for.
	outB := tmpDir(t)
	pdfB := &rcBlob{data: []byte("%PDF-1.4 legacy b")}
	msgB := func() *model.Message { return rcMsg("legb@x", "Old B", "<p>old b</p>", nil, pdfB.att("old.pdf")) }
	if _, err := rcExport(t, outB, export.Incremental, false, msgB()); err != nil {
		t.Fatal(err)
	}
	_, recB := recordForID(t, outB, "legb@x")
	stemB := strings.TrimSuffix(recB.Path, ".html")
	pageB, zipB := rcRead(t, outB, stemB+".html"), rcRead(t, outB, stemB+"-attachments.zip")
	rcLegacy(t, outB, "legb@x")

	t.Skip("MA-355 (F46, R1): a legacy or unknown record's first re-examination commits whatever the source holds now; encoder lands in RI-S1, turns green in RI-S5")

	// (a) the attachment is now empty in a full run.
	pdfA.data = nil
	if _, err := rcExport(t, outA, export.Full, false, msgA()); err != nil {
		t.Fatal(err)
	}
	for _, bad := range rcAsRecorded(t, outA, recA) {
		t.Errorf("(a) a full run over a source that holds less changed a v6 record's files: %s", bad)
	}

	// (b) the attachment is now empty at an unknown record's first re-examination:
	// the files stay byte for byte, and the record is not left fillable for ever.
	pdfB.data = nil
	if _, err := rcExport(t, outB, export.Incremental, false, msgB()); err != nil {
		t.Fatal(err)
	}
	if got, _ := rcDigest(t, filepath.Join(outB, filepath.FromSlash(stemB+"-attachments.zip"))); got != rcSum(zipB) {
		t.Errorf("(b) the archived zip of an unknown record changed or was removed")
	}
	if !bytes.Equal(rcRead(t, outB, stemB+".html"), pageB) {
		t.Errorf("(b) the archived page of an unknown record was rewritten")
	}
	if st, err := rcExport(t, outB, export.Incremental, false, msgB()); err != nil || st.Retried != 0 {
		t.Errorf("(b) the next incremental run re-examined the record (retried=%d, err %v): an unverified record is never fillable", st.Retried, err)
	}

	// (c) the inline image is now empty: the page must keep the embedded image.
	logo.data = nil
	if _, err := rcExport(t, outC, export.Incremental, false, msgC()); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(rcRead(t, outC, recC.Path), pageC) {
		t.Errorf("(c) the page lost its embedded inline image to a re-examination that found it empty")
	}
}

// covers: MA-356, R28, R21, R17, R1, S50
// §1.6: on the live path a full run over a message whose attachment was removed
// server-side under the same immutable id keeps the archived zip.
//
// The Graph service is answered in-process through the client's own transport seam
// (GraphOptions.WrapTransport), so no socket is opened. The row's DEMOTE half (a
// legacy-scheme record), the timeline stamps of a KEEP and the live-path UNCHANGED
// twins of the first full run after an upgrade are RI-S5's, with the verdicts that
// produce them.
func TestLiveFullRunKeepsAnAttachmentRemovedUnderTheSameImmutableID(t *testing.T) {
	g := &rcGraph{}
	gopts := GraphOptions{Tenant: "t", ClientID: "c", ClientSecret: "s", Mailboxes: []string{"u1"},
		BaseURL: "https://graph.test", TokenURL: "https://graph.test/token",
		WrapTransport: func(http.RoundTripper) http.RoundTripper { return g }}
	run := func(t *testing.T, out string) {
		t.Helper()
		_, err := RunGraph(context.Background(), gopts, Options{Out: out, Mode: export.Full, KeepRaw: true}, rcLogger())
		var partial *PartialError
		if err != nil && !errors.As(err, &partial) {
			t.Fatalf("graph run: %v", err)
		}
	}
	withZip := rcMail("live@x", "Contract", "<p>the contract</p>", "contract.pdf", []byte("%PDF-1.4 the contract"))
	without := rcMail("live@x", "Contract", "<p>the contract</p>", "", nil)

	// Twin: the attachment is intact on the second full run, so the archive keeps
	// what was recorded, at the same path.
	outT := tmpDir(t)
	g.set(withZip)
	run(t, outT)
	_, t1 := recordForID(t, outT, "live@x")
	if t1.PhysID != "M1" || t1.Fixity == nil || t1.Fixity.Zip == nil || t1.Fixity.EML == nil {
		t.Fatalf("the first capture should record the immutable id, a zip and an original: %+v", t1)
	}
	run(t, outT)
	_, t2 := recordForID(t, outT, "live@x")
	if bad := rcAsRecorded(t, outT, t1); len(bad) != 0 || t2.Path != t1.Path {
		t.Fatalf("a full run over an intact message changed the archive: %v (path %s then %s)", bad, t1.Path, t2.Path)
	}

	t.Skip("MA-356 (§1.6, R1): placement by immutable id bypasses the fingerprint guard and the commit deletes the zip; encoder lands in RI-S1, turns green in RI-S5")

	// The attachment is removed server-side; the message keeps its immutable id.
	out := tmpDir(t)
	g.set(withZip)
	run(t, out)
	_, rec1 := recordForID(t, out, "live@x")
	g.set(without)
	run(t, out)
	_, rec2 := recordForID(t, out, "live@x") // fails the test unless the placement adopted the one record
	if rec2.Path != rec1.Path {
		t.Fatalf("the run did not adopt the record by its immutable id (path %s then %s): the case is vacuous", rec1.Path, rec2.Path)
	}
	for _, bad := range rcAsRecorded(t, out, rec1) {
		t.Errorf("a full run over a message whose attachment was removed server-side changed the archive: %s", bad)
	}
	if !rec2.Present {
		t.Errorf("the record is not present after the full run: %+v", rec2)
	}
}

// rcGraph is an in-process Microsoft Graph with one mailbox, one folder and one
// message, M1, whose $value the test sets. It honours the immutable-id preference,
// so the listing's id is the message's PhysID.
type rcGraph struct {
	mu   sync.Mutex
	mime []byte
}

func (g *rcGraph) set(b []byte) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.mime = b
}

func (g *rcGraph) RoundTrip(r *http.Request) (*http.Response, error) {
	h := http.Header{}
	reply := func(code int, ctype, body string) (*http.Response, error) {
		h.Set("Content-Type", ctype)
		return &http.Response{StatusCode: code, Status: http.StatusText(code), Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1,
			Header: h, Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body)), Request: r}, nil
	}
	switch p := r.URL.Path; {
	case p == "/token":
		return reply(200, "application/json", `{"access_token":"t","token_type":"Bearer","expires_in":3600}`)
	case p == "/users/u1/mailFolders":
		return reply(200, "application/json", `{"value":[{"id":"F_IN","displayName":"Inbox","childFolderCount":0}]}`)
	case p == "/users/u1/mailFolders/F_IN/messages":
		if strings.Contains(strings.ToLower(r.Header.Get("Prefer")), "immutableid") {
			h.Set("Preference-Applied", `IdType="ImmutableId"`)
		}
		return reply(200, "application/json", `{"value":[{"id":"M1","internetMessageId":"<live@x>","subject":"Contract","from":{"emailAddress":{"address":"alice@example.com"}},"receivedDateTime":"2025-03-03T09:00:00Z"}]}`)
	case p == "/users/u1/messages/M1/$value":
		g.mu.Lock()
		defer g.mu.Unlock()
		return reply(200, "message/rfc822", string(g.mime))
	}
	return reply(404, "application/json", `{}`) // the well-known folder resolutions, and anything else
}

// covers: MA-357, R28, R20, R5, S50, S34
// §1.5: a full run without -raw over an archive captured with it keeps the
// preserved original and its recorded fixity, in place and when the message moves.
// The row's (c) and (d) (the merge and the flipped earlier original) specify new
// behaviour and land with RI-S5.
func TestRecaptureWithoutRawKeepsThePreservedOriginal(t *testing.T) {
	mail := func(body string) []byte { return rcMail("raw@x", "Original", body, "a.pdf", []byte("%PDF a")) }
	eml := func(rec state.Record) string { return strings.TrimSuffix(rec.Path, ".html") + ".eml" }

	// Twin: -raw over an intact source whose message changed writes the new original
	// and records it.
	srcT, outT := rcSource(t), tmpDir(t)
	rcPut(t, srcT, mail("<p>first</p>"))
	rcRun(t, srcT, outT, export.Full, true, nil)
	_, t1 := recordForID(t, outT, "raw@x")
	rcPut(t, srcT, mail("<p>second, a longer body</p>"))
	rcRun(t, srcT, outT, export.Full, true, nil)
	_, t2 := recordForID(t, outT, "raw@x")
	got, ok := rcDigest(t, filepath.Join(outT, filepath.FromSlash(eml(t2))))
	if !ok || t2.Fixity == nil || t2.Fixity.EML == nil || got != *t2.Fixity.EML || got == *t1.Fixity.EML {
		t.Fatalf("a -raw full run over a changed source should write and record the new original: file %+v, record %+v", got, t2.Fixity)
	}

	// A -raw archive, then the same source.
	src, out := rcSource(t), tmpDir(t)
	rcPut(t, src, mail("<p>first</p>"))
	rcRun(t, src, out, export.Full, true, nil)
	_, rec1 := recordForID(t, out, "raw@x")
	if rec1.Fixity == nil || rec1.Fixity.EML == nil {
		t.Fatalf("the -raw capture should record its original: %+v", rec1)
	}
	srcB, outB := rcSource(t), tmpDir(t)
	rcPut(t, srcB, mail("<p>first</p>"))
	rcRun(t, srcB, outB, export.Full, true, nil)
	keyB, recB := recordForID(t, outB, "raw@x")
	moved := rcRenameStem(t, outB, keyB, "2025-03-03_0900_Original_OLDRULE_92544786")
	if bad := rcAsRecorded(t, outB, moved); len(bad) != 0 {
		t.Fatalf("the archive under the earlier naming rule should attest: %v", bad)
	}

	t.Skip("MA-357 (§1.5, R20): a re-capture without -raw un-fixes or deletes the preserved .eml; encoder lands in RI-S1, turns green in RI-S5")

	// (a) A full run without -raw at an unchanged path: the original stays as
	// recorded, and the record still carries its fixity.
	rcRun(t, src, out, export.Full, false, nil)
	_, rec2 := recordForID(t, out, "raw@x")
	if rec2.Fixity == nil || rec2.Fixity.EML == nil {
		t.Errorf("(a) the record lost the fixity of its preserved original: %+v", rec2.Fixity)
	}
	for _, bad := range rcAsRecorded(t, out, rec1) {
		t.Errorf("(a) a full run without -raw changed the archive: %s", bad)
	}
	if rep := rcVerify(t, out); !rep.Attested() {
		t.Errorf("(a) verify does not attest the archive after the run: %+v", rep.Problems)
	}

	// (b) The same with a forced naming change: the original moves with the message,
	// byte-identical, with the same digest.
	rcRun(t, srcB, outB, export.Full, false, nil)
	_, rec2B := recordForID(t, outB, "raw@x")
	if rec2B.Path == moved.Path {
		t.Fatalf("(b) the run did not rename the message: the case is vacuous")
	}
	gotB, okB := rcDigest(t, filepath.Join(outB, filepath.FromSlash(eml(rec2B))))
	if !okB || gotB != *recB.Fixity.EML {
		t.Errorf("(b) the preserved original did not move with the message, byte-identical (present=%v)", okB)
	}
	if rec2B.Fixity == nil || rec2B.Fixity.EML == nil || *rec2B.Fixity.EML != *recB.Fixity.EML {
		t.Errorf("(b) the moved record does not carry the original's fixity: %+v", rec2B.Fixity)
	}
}

// covers: MA-358, R28, R5, R8, S51, S2
// N2: a re-capture that moves a message never leaves the persisted manifest naming
// a file it deleted before the manifest was saved.
//
// The run is observed from inside, from its progress callback: after the message
// is committed and before the next checkpoint save, the state a hard kill there
// leaves (RC31). The archive is copied then and verified as a fresh process would
// find it. The per-step kill points C1..C8 of the design need the exporter's
// kill-point seam, which RI-S4 adds with the write path; the finder's numbers are
// reproduced here through the callback. The design's "stale lock and uncommitted
// index batch are present" witness is not asserted: this run keeps no index, and
// the lock is left out of the copy, as a dead holder's is stale.
func TestMovingRecaptureNeverLeavesARecordNamingAMissingFile(t *testing.T) {
	src, out := rcSource(t), tmpDir(t)
	rcPut(t, src, rcMail("mv@x", "Move me", "<p>text</p>", "a.pdf", []byte("%PDF a")))
	rcRun(t, src, out, export.Full, true, nil)
	key, _ := recordForID(t, out, "mv@x")
	// The archive as a release under an earlier naming rule left it.
	old := rcRenameStem(t, out, key, "2025-03-03_0900_Move-me_OLDRULE_92544786")
	if rep := rcVerify(t, out); !rep.Attested() {
		t.Fatalf("the archive under the earlier naming rule should attest: %+v", rep.Problems)
	}

	// The full re-export the README recommends, observed after its first message.
	var snap string
	var exported int
	rcRun(t, src, out, export.Full, true, func(s export.Stats) {
		if snap == "" {
			snap, exported = rcSnapshot(t, out), s.Exported
		}
	})
	assure.Reached(t, snap, "the snapshot taken inside the run")
	if exported != 1 {
		t.Fatalf("the snapshot was taken after %d exports, want the first message committed", exported)
	}
	_, rec2 := recordForID(t, out, "mv@x")
	if rec2.Path == old.Path {
		t.Fatalf("the run did not rename the message (%s): the case is vacuous", rec2.Path)
	}

	// Twin: a run that finishes leaves a consistent archive.
	if rep := rcVerify(t, out); !rep.Attested() {
		t.Fatalf("a finished run should leave the archive attested: %+v", rep.Problems)
	}

	t.Skip("MA-358 (N2, R5): the old files are deleted before the new path is durable; encoder lands in RI-S1, turns green in RI-S4")

	rep := rcVerify(t, snap)
	if !rep.Attested() || rep.Missing != 0 {
		t.Errorf("after a kill right after the message committed, verify of the archive finds missing=%d modified=%d unexpected=%d unrecorded=%d (attested=%v): the persisted manifest names files the run deleted: %+v",
			rep.Missing, rep.Modified, rep.Unexpected, rep.Unrecorded, rep.Attested(), rep.Problems)
	}
}

// covers: MA-358, R28, R5, R8, S51, S2
// §1.7: a re-capture at an unchanged path never leaves files whose bytes no longer
// match the persisted fixity. The result of this encoder on today's code goes with
// the owner's ruling ESC-3 (design Q1).
//
// Observed as the N2 encoder above is: from inside the run, after the changed
// message is committed and before the next checkpoint save.
func TestSamePathRecaptureNeverBreaksTheRecordedFixity(t *testing.T) {
	src, out := rcSource(t), tmpDir(t)
	rcPut(t, src, rcMail("same@x", "Same place", "<p>first text</p>", "a.pdf", []byte("%PDF a")))
	rcRun(t, src, out, export.Full, true, nil)
	_, rec1 := recordForID(t, out, "same@x")

	// The message changed at the source: its body is longer, its envelope the same.
	rcPut(t, src, rcMail("same@x", "Same place", "<p>second text, now longer</p>", "a.pdf", []byte("%PDF a")))
	var snap string
	var exported int
	rcRun(t, src, out, export.Full, true, func(s export.Stats) {
		if snap == "" {
			snap, exported = rcSnapshot(t, out), s.Exported
		}
	})
	assure.Reached(t, snap, "the snapshot taken inside the run")
	if exported != 1 {
		t.Fatalf("the snapshot was taken after %d exports, want the first message committed", exported)
	}
	_, rec2 := recordForID(t, out, "same@x")
	if rec2.Path != rec1.Path || rec2.Fixity == nil || rec2.Fixity.HTML == nil || *rec2.Fixity.HTML == *rec1.Fixity.HTML {
		t.Fatalf("the run should re-capture the message at its own path with new content: %+v then %+v", rec1, rec2)
	}

	// Twin: a run that finishes leaves a consistent archive.
	if rep := rcVerify(t, out); !rep.Attested() {
		t.Fatalf("a finished run should leave the archive attested: %+v", rep.Problems)
	}

	t.Skip("MA-358 (§1.7, R5): a re-capture renames new bytes over the recorded files before the manifest is saved; encoder lands in RI-S1, turns green in RI-S4 once ESC-3 is answered")

	rep := rcVerify(t, snap)
	if rep.Modified != 0 || !rep.Attested() {
		t.Errorf("after a kill right after the message committed, verify of the archive finds modified=%d missing=%d (attested=%v): the files' bytes no longer match the persisted fixity: %+v",
			rep.Modified, rep.Missing, rep.Attested(), rep.Problems)
	}
}

// covers: MA-359, R5, R28, S51
// N3: a failed page write leaves the earlier page, zip and original byte-unchanged
// and attested.
//
// The fault is a rename refusal on the page: while the new zip is being built, the
// page's path becomes a directory with a file in it (the earlier page is set aside),
// so the rename of the staged page over it fails, as a sharing violation on
// Windows does; the earlier page is put back afterwards. The injected ENOSPC on the
// page's temp and the W3 twin (a refused rename after the original and zip renames)
// need the write path's fault seam, which RI-S4 adds.
func TestPageWriteFailureLeavesTheEarlierFilesAttested(t *testing.T) {
	pdf := &rcBlob{}
	msg := func(v string) *model.Message {
		return rcMsg("n3@x", "Contract", "<p>the contract</p>", []byte("raw bytes, "+v), pdf.att("c.pdf"))
	}
	first := func() (string, state.Record) {
		out := tmpDir(t)
		pdf.data, pdf.hook = []byte("%PDF-1.4 version one"), nil
		if _, err := rcExport(t, out, export.Full, true, msg("one")); err != nil {
			t.Fatal(err)
		}
		_, rec := recordForID(t, out, "n3@x")
		return out, rec
	}

	// Twin: with no fault the changed message commits and the archive attests.
	outT, _ := first()
	pdf.data = []byte("%PDF-1.4 version two, a longer attachment")
	if _, err := rcExport(t, outT, export.Full, true, msg("two")); err != nil {
		t.Fatal(err)
	}
	if rep := rcVerify(t, outT); !rep.Attested() {
		t.Fatalf("a changed message without a fault should commit and attest: %+v", rep.Problems)
	}

	t.Skip("MA-359 (N3, R5): the zip is replaced or removed before the page is written; encoder lands in RI-S1, turns green in RI-S4")

	out, rec1 := first()
	page := filepath.Join(out, filepath.FromSlash(rec1.Path))
	pdf.data = []byte("%PDF-1.4 version two, a longer attachment")
	pdf.hook = func() {
		if err := os.Rename(page, page+".keep"); err != nil {
			t.Error(err)
		}
		if err := os.MkdirAll(page, 0o755); err != nil {
			t.Error(err)
		}
		if err := os.WriteFile(filepath.Join(page, "occupied"), []byte("x"), 0o644); err != nil {
			t.Error(err)
		}
	}
	_, err := rcExport(t, out, export.Full, true, msg("two"))
	pdf.hook = nil
	if rerr := os.RemoveAll(page); rerr != nil {
		t.Fatal(rerr)
	}
	if rerr := os.Rename(page+".keep", page); rerr != nil {
		t.Fatal(rerr)
	}

	rc, text := 0, ""
	if err != nil {
		rc, text = 1, err.Error()
	}
	assure.Refused(t, rc, text, assure.Code(1), assure.Names(page), assure.NoSideEffect(func() bool {
		bad := rcAsRecorded(t, out, rec1)
		for _, b := range bad {
			t.Logf("after the refused page write: %s", b)
		}
		var temps []string
		filepath.WalkDir(out, func(p string, d fs.DirEntry, werr error) error {
			if werr == nil && !d.IsDir() && strings.HasPrefix(d.Name(), ".mailarchive-") && strings.HasSuffix(d.Name(), ".tmp") {
				temps = append(temps, p)
			}
			return nil
		})
		for _, p := range temps {
			t.Logf("a temp file remains: %s", p)
		}
		return len(bad) == 0 && len(temps) == 0
	}))
	if rep := rcVerify(t, out); !rep.Attested() {
		t.Errorf("verify does not attest the earlier files after the refused page write: missing=%d modified=%d %+v", rep.Missing, rep.Modified, rep.Problems)
	}

	// The fault is gone: the next run commits.
	if _, err := rcExport(t, out, export.Full, true, msg("two")); err != nil {
		t.Fatalf("the next run did not commit: %v", err)
	}
	if rep := rcVerify(t, out); !rep.Attested() {
		t.Errorf("the next run did not leave the archive attested: %+v", rep.Problems)
	}
}

// covers: MA-361, R13, R8, S51, S13
// F48: reindex meeting rows that name earlier files of messages whose current
// files are intact deletes nothing and re-points every row.
//
// The stale rows are built as the finder's PoC does: the records name new files (a
// message renamed by a re-capture) while the index rows, whose update was lost, still
// name the earlier, missing ones. The row's repaired count and its repair lines are
// the slice's own; the case asserts what is checkable today: nothing is deleted,
// nothing is pruned, every record stays and every row names its record's file. The
// symlink, EACCES, suffix-less-row and MA-227 variants, and the two ESC-2 twins
// (the `-v2` orphan and the N2 victim's adoption), land with RI-S2.
func TestReindexKeepsACurrentRecordsFilesWhenItsIndexRowIsStale(t *testing.T) {
	plant := func(t *testing.T, out, rel string) {
		t.Helper()
		stem := filepath.Join(out, filepath.FromSlash(strings.TrimSuffix(rel, ".html")))
		for _, suffix := range []string{"-attachments.zip", ".eml"} {
			if err := os.WriteFile(stem+suffix, []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	exists := func(out, rel string) bool {
		_, err := os.Stat(filepath.Join(out, filepath.FromSlash(rel)))
		return err == nil
	}

	// Twin: deleting a record's current page still prunes it and deletes its zip and
	// original, while the others stay (MA-40 and the redaction-reach rows).
	outT := tmpDir(t)
	relsT := buildArchive(t, outT)
	for _, rel := range relsT {
		plant(t, outT, rel)
	}
	gone := relsT["prune-beta"]
	if err := os.Remove(filepath.Join(outT, filepath.FromSlash(gone))); err != nil {
		t.Fatal(err)
	}
	if kept, pruned, err := Reindex(outT, rcLogger()); err != nil || kept != 2 || pruned != 1 {
		t.Fatalf("reindex after deleting one page: kept=%d pruned=%d err=%v, want 2, 1", kept, pruned, err)
	}
	stemGone := strings.TrimSuffix(gone, ".html")
	if exists(outT, stemGone+".eml") || exists(outT, stemGone+"-attachments.zip") {
		t.Fatalf("redaction left the pruned message's original or zip behind")
	}
	for subj, rel := range relsT {
		if subj != "prune-beta" && !(exists(outT, rel) && exists(outT, strings.TrimSuffix(rel, ".html")+".eml")) {
			t.Fatalf("reindex deleted the files of %s, which was not redacted", subj)
		}
	}

	t.Skip("MA-361 (F48, R13): reindex deletes a message's current files when its index row names an earlier, missing path; encoder lands in RI-S1, turns green in RI-S2")

	out := tmpDir(t)
	rels := buildArchive(t, out)
	man, err := state.Load(filepath.Join(out, ".mailarchive-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	current := map[string]bool{} // the records' new paths
	for key, rec := range man.All() {
		newRel := strings.TrimSuffix(rec.Path, ".html") + "-v2.html"
		if err := os.Rename(filepath.Join(out, filepath.FromSlash(rec.Path)), filepath.Join(out, filepath.FromSlash(newRel))); err != nil {
			t.Fatal(err)
		}
		plant(t, out, newRel)
		rec.Path = newRel
		man.Add(key, rec)
		current[newRel] = true
	}
	if err := man.Save(); err != nil {
		t.Fatal(err)
	}
	assure.Reached(t, current, "the records' current paths")
	if len(current) != len(rels) {
		t.Fatalf("expected %d current paths, got %d", len(rels), len(current))
	}

	_, pruned, err := Reindex(out, rcLogger())
	if err != nil {
		t.Fatalf("reindex: %v", err)
	}
	if pruned != 0 {
		t.Errorf("reindex pruned %d record(s) whose current files are intact", pruned)
	}
	for rel := range current {
		stem := strings.TrimSuffix(rel, ".html")
		for _, suffix := range []string{".html", "-attachments.zip", ".eml"} {
			if !exists(out, stem+suffix) {
				t.Errorf("reindex deleted %s, a file of a record that is current and intact", stem+suffix)
			}
		}
	}
	man2, err := state.Load(filepath.Join(out, ".mailarchive-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if man2.Len() != len(rels) {
		t.Errorf("the manifest holds %d records after reindex, want %d kept", man2.Len(), len(rels))
	}
	ix, err := index.OpenReadonly(filepath.Join(out, "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	rows := map[string]bool{}
	if err := ix.EachRow(func(_ int64, _, path string) error { rows[path] = true; return nil }); err != nil {
		t.Fatal(err)
	}
	for rel := range current {
		if !rows[rel] {
			t.Errorf("no index row names %s: searches do not hit the record's current file", rel)
		}
	}
}
