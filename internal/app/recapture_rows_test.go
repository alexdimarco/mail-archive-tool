package app

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/export"
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
func TestLiveFullRunKeepsAnAttachmentRemovedUnderTheSameImmutableID(t *testing.T) {
	t.Skip("MA-356 (§1.6, R1): placement by immutable id bypasses the fingerprint guard and the commit deletes the zip; encoder lands in RI-S1, turns green in RI-S5")
}

// covers: MA-357, R28, R20, R5, S50, S34
// §1.5: a full run without -raw over an archive captured with it keeps the
// preserved original and its recorded fixity, in place and when the message moves.
func TestRecaptureWithoutRawKeepsThePreservedOriginal(t *testing.T) {
	t.Skip("MA-357 (§1.5, R20): a re-capture without -raw un-fixes or deletes the preserved .eml; encoder lands in RI-S1, turns green in RI-S5")
}

// covers: MA-358, R28, R5, R8, S51, S2
// N2: a re-capture that moves a message never leaves the persisted manifest naming
// a file it deleted before the manifest was saved.
func TestMovingRecaptureNeverLeavesARecordNamingAMissingFile(t *testing.T) {
	t.Skip("MA-358 (N2, R5): the old files are deleted before the new path is durable; encoder lands in RI-S1, turns green in RI-S4")
}

// covers: MA-358, R28, R5, R8, S51, S2
// §1.7: a re-capture at an unchanged path never leaves files whose bytes no longer
// match the persisted fixity. The result of this encoder on today's code goes with
// the owner's ruling ESC-3 (design Q1).
func TestSamePathRecaptureNeverBreaksTheRecordedFixity(t *testing.T) {
	t.Skip("MA-358 (§1.7, R5): a re-capture renames new bytes over the recorded files before the manifest is saved; encoder lands in RI-S1, turns green in RI-S4 once ESC-3 is answered")
}

// covers: MA-359, R5, R28, S51
// N3: a failed page write leaves the earlier page, zip and original byte-unchanged
// and attested.
func TestPageWriteFailureLeavesTheEarlierFilesAttested(t *testing.T) {
	t.Skip("MA-359 (N3, R5): the zip is replaced or removed before the page is written; encoder lands in RI-S1, turns green in RI-S4")
}

// covers: MA-361, R13, R8, S51, S13
// F48: reindex meeting rows that name earlier files of messages whose current
// files are intact deletes nothing and re-points every row.
func TestReindexKeepsACurrentRecordsFilesWhenItsIndexRowIsStale(t *testing.T) {
	t.Skip("MA-361 (F48, R13): reindex deletes a message's current files when its index row names an earlier, missing path; encoder lands in RI-S1, turns green in RI-S2")
}
