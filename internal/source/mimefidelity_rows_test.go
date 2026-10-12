package source

import "testing"

// MIME fidelity (docs/design-mime-fidelity.md §9, §10), slice S0: the catalog
// rows MA-330..MA-349 land as skip encoders. S0 changes no parser or production
// behaviour, so none of them can pass yet. Each names what is missing and the slice
// that turns it into the real test; its covering marker stays on it until then.
// MA-343 is tier L (pending, a lab story in the catalog) and has no encoder.

// covers: MA-333, R26
// F24 charsets, the page half of MA-333: a part decoded after a charset fallback
// carries a charset-fallback note on the page.
func TestUnknownCharsetNoteShowsOnThePage(t *testing.T) {
	t.Skip("MA-333 (page): the page has no collapsed notes block to show charset-fallback on (renderHeader holds none); lands in MF-S4")
}

// covers: MA-335, R1, R7, R26
// F25 bodies: Apple Mail's split html, the placeholder and the zip, mutt's named
// text parts and Mailman's footer are all kept in document order.
func TestBodiesKeepEveryInlineTextPartInOrder(t *testing.T) {
	t.Skip("MA-335: parseMessage keeps only the first html and first plain part (mbox.go:295-301) and names inline files inline; encoder lands in MF-S0, turns green in MF-S3")
}

// covers: MA-336, R1, R4, R26
// F25 names: filename, then Content-ID, then inline plus the extension, with no
// host dependency, and the page, placeholders and gaps block print the zip names.
func TestZipNamesAreFinalAndDoNotDependOnTheHost(t *testing.T) {
	t.Skip("MA-336: Attachment.ZipName and the naming pass do not exist, and every inline non-text part is still named by its Content-ID or inline (mbox.go:302-311); encoder lands in MF-S0, turns green in MF-S3")
}

// covers: MA-339, R7, R1, R19, R26
// C1-5: a cid text placeholder or an a href cid link does not consume the
// attachment; image contexts embed and are kept out of the zip; data URIs are
// allowlisted.
func TestCidTextReferencesKeepTheAttachment(t *testing.T) {
	t.Skip("MA-339: embedInlineImages still replaces every textual cid reference and marks the attachment consumed (html.go:640-668), and there is no data-URI allowlist; encoder lands in MF-S0, turns green in MF-S4")
}

// covers: MA-340, R7, R8, R26, R3
// C1-8, the headers half of MA-340: a raw Latin-1 Subject, From name and To are valid
// UTF-8 on the page and in the index.
func TestLegacy8BitHeadersBecomeValidUTF8(t *testing.T) {
	t.Skip("MA-340 (headers): Subject and the address fields are still decoded as v1 decodes them, which keeps an 8-bit value as raw bytes (header decoding is S3's, P7); lands in MF-S3")
}

// covers: MA-341, R7, R8, R3, R26
// C1-10: a From header net/mail rejects still gives the page, the folder page and
// sender search a sender, and the fingerprint equals the golden.
func TestUnparsableFromHeadersKeepTheSender(t *testing.T) {
	t.Skip("MA-341: the sender is set only when AddressList(From) succeeds (mbox.go:258-261), so these headers yield no sender; encoder lands in MF-S0, turns green in MF-S3")
}

// covers: MA-342, R1, R10, R26
// F26 interim on support.pst with an attachment retyped to PtypObject in memory:
// no junk zip entry, a terminal embedded-item gap, and the outer message exported.
func TestPSTAttachedItemIsAGapNotJunkBytes(t *testing.T) {
	t.Skip("MA-342: collectAttachments never reads PidTagAttachMethod and wires go-pst's type-blind WriteTo (reader.go:527-563), and there is no nil-WriteTo guard in WriteZip; encoder lands in MF-S0, turns green in MF-S5")
}

// covers: MA-344, R2, R3, R5, R13, R17, R27
// Repair: an archive written through the frozen v1 path is upgraded once, with
// Repaired and Restamped counted, keys and fingerprints unchanged and fixity fresh.
func TestRepairUpgradesAnArchiveWrittenByV1(t *testing.T) {
	t.Skip("MA-344: Record.ParseScheme, V1Inventory and the repair branch do not exist; the row also waits on ESC-2 and ESC-3 (the commit that decides the repaired files' names, ruling J6); encoder lands in MF-S0, turns green in MF-S7")
}

// covers: MA-345, R1, R13, R4, R21, R27
// Repair is never to less and a withheld repair is legible: each of cases (a) to
// (j) leaves old page, zip, record and fixity unchanged and names its reason.
func TestRepairIsNeverToLessAndWithheldLegibly(t *testing.T) {
	t.Skip("MA-345: the V1C check, RepairWithheld and the repair counters do not exist, and the 'never to less' clause is re-capture integrity's R28; encoder lands in MF-S0, turns green in MF-S7")
}

// covers: MA-348, R1, R18, R7, X6, R26, R27
// Legibility: the summary, status, report, GUI and Desktop card show repaired,
// restamped and the cannot-decode counts; the page's gaps and notes blocks are
// escaped.
func TestRepairAndCannotDecodeAreLegibleOnEverySurface(t *testing.T) {
	t.Skip("MA-348: no repair counters, undecodable counts, fidelity_fallback, or page gaps and notes blocks exist to show; encoder lands in MF-S0, turns green in MF-S2 (counts), MF-S4 (page blocks) and MF-S7 (repair)")
}

// covers: MA-349, R10, R19
// Bounds and isolation, the export side of MA-349: a panic in an embedded render, in
// the cid tree embedding or in a repair attempt costs that feature or attempt, and
// the outer message is exported; the </style> breakout stays out of the page (MC23); a
// fallback message with a cid text reference repairs 0 on the second run (MC19).
func TestHostileMIMEExportSideIsIsolated(t *testing.T) {
	t.Skip("MA-349 (export side): the cid tree embedding and the embedded render do not exist yet (render-fallback and the data-URI allowlist land in MF-S4) and neither does the repair attempt (withheld repair-error and the fallback-never-loops case, MF-S7); lands in MF-S4 and MF-S7")
}
