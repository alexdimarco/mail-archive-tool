package app

import "testing"

// Re-capture integrity (docs/design-recapture-integrity.md §9, §10), slice S1,
// interim: the catalog rows of the confirmed-open gaps (F46, N2, N3, F48 and
// §1.5-§1.7) land as skip encoders, each citing the existing invariant its defect
// breaks. S1 changes no product code, so none of them can pass yet. Each names the
// finding and the slice that turns it into a permanent guard; its covering marker
// stays on it until then. The rows that specify new behaviour (MA-353, MA-354,
// MA-360, MA-362..MA-364) land with the slice that builds them (RC32).

// covers: MA-350, R28, R1, S50
// F46: a full run over an on-demand local source whose same-name attachment is now
// zero bytes keeps the archived page, zip and original as recorded.
func TestFullRunKeepsAnAttachmentTheSourceNowHoldsEmpty(t *testing.T) {
	t.Skip("MA-350 (F46, R1): a full run commits a degraded capture over a good archived copy and deletes its zip; encoder lands in RI-S1, turns green in RI-S5")
}

// covers: MA-351, R28, R1, S50
// F46: a body that is now missing is kept in a full run, and in a fill retry the
// verdict is taken at the probe, before any temp file is written.
func TestFullRunAndFillRetryKeepABodyTheSourceNowLacks(t *testing.T) {
	t.Skip("MA-351 (F46, R1): a full run and a fill retry overwrite an archived body with an empty one; encoder lands in RI-S1, turns green in RI-S5")
}

// covers: MA-352, R28, R1, S50
// F46: an attachment stream that now errors is kept in a full run, and a healthy
// stream on the next full run heals the record.
func TestFullRunKeepsAnAttachmentWhoseStreamNowErrors(t *testing.T) {
	t.Skip("MA-352 (F46, R1): a torn attachment read in a full run removes the archived zip and records a fillable gap; encoder lands in RI-S1, turns green in RI-S5")
}

// covers: MA-355, R28, R1, R5, S50, S6
// F46 on legacy records: a re-examination that would degrade leaves the files as
// archived, and a legacy record is never claimed complete or fillable for ever.
func TestLegacyReExaminationKeepsWhatItWouldDegrade(t *testing.T) {
	t.Skip("MA-355 (F46, R1): a legacy or unknown record's first re-examination commits whatever the source holds now; encoder lands in RI-S1, turns green in RI-S5")
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
