package source

import "testing"

// MIME fidelity (docs/design-mime-fidelity.md §9, §10), slice S0: the catalog
// rows MA-330..MA-349 land as skip encoders. S0 changes no parser or production
// behaviour, so none of them can pass yet. Each names what is missing and the slice
// that turns it into the real test; its covering marker stays on it until then.
// MA-343 is tier L (pending, a lab story in the catalog) and has no encoder.

// covers: MA-332, R2, R7, R27
// A corpus message with no fidelity note has the content golden's fields, renders
// as the current renderer renders the v1 message, and carries IdentityTerms equal
// to its content fields.
func TestNoNoteMessagesKeepTheContentGolden(t *testing.T) {
	t.Skip("MA-332: ParseNotes, IdentityTerms and replayV1 do not exist, so no message can be told to carry no fidelity note; encoder lands in MF-S0, turns green in MF-S2")
}

// covers: MA-333, R1, R10, R26
// F24 charsets: every label, first and after the body, keeps the later parts and
// decodes the text with no gap.
func TestUnknownCharsetsKeepLaterPartsAndDecode(t *testing.T) {
	t.Skip("MA-333: the parse loop still ends at the first unknown charset (mbox.go:286-288); the page assertion also waits for the renderer; encoder lands in MF-S0, turns green in MF-S2 (non-page) and MF-S4 (page)")
}

// covers: MA-334, R1, R10, R26
// F24 transfer encodings: 8-bit and quoted-printable (encoded) decode with
// lenient-cte; unknown encodings keep their bytes as <name>.undecoded with a
// terminal undecoded-part gap; a truncated base64 part is fillable on an on-demand
// source.
func TestUnknownTransferEncodingsAreKeptAndRecorded(t *testing.T) {
	t.Skip("MA-334: the part loop drops every part after an unknown encoding and there is no parse-gap channel (Attachment.GapKind, undecoded-part, corrupt-part); encoder lands in MF-S0, turns green in MF-S2")
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

// covers: MA-337, R1, R5, R26
// C1-1: a text attachment's zip entry is its transfer-decoded source bytes, never
// charset-converted, and fixity is taken over them.
func TestTextAttachmentsKeepTheirSourceBytes(t *testing.T) {
	t.Skip("MA-337: go-message converts every text/* attachment that declares a charset (entity.go:48-57) and the zip digest is taken over the converted bytes; encoder lands in MF-S0, turns green in MF-S2")
}

// covers: MA-338, R1, R10, R26
// C1-4: a malformed Content-Type parameter does not lose the multipart boundary,
// the body or the attachment names.
func TestMalformedContentTypeParametersAreRecovered(t *testing.T) {
	t.Skip("MA-338: go-message returns no parameters on any mime.ParseMediaType error, so the boundary becomes empty and the parse ends with no fallback; there is no lenient tokenizer or unsplit-multipart gap; encoder lands in MF-S0, turns green in MF-S2")
}

// covers: MA-339, R7, R1, R19, R26
// C1-5: a cid text placeholder or an a href cid link does not consume the
// attachment; image contexts embed and are kept out of the zip; data URIs are
// allowlisted.
func TestCidTextReferencesKeepTheAttachment(t *testing.T) {
	t.Skip("MA-339: embedInlineImages still replaces every textual cid reference and marks the attachment consumed (html.go:640-668), and there is no data-URI allowlist; encoder lands in MF-S0, turns green in MF-S4")
}

// covers: MA-340, R7, R8, R26, R3
// C1-8: raw Latin-1 headers and unlabelled or us-ascii-labelled 8-bit bodies are
// valid UTF-8 on the page and in the index, and the identity equals the golden.
func TestLegacy8BitTextBecomesValidUTF8(t *testing.T) {
	t.Skip("MA-340: only the PST reader has the valid-UTF-8-else-Windows-1252 step (reader.go:517-525); raw MIME passes the bytes through unchanged (charset.go:38-42); encoder lands in MF-S0, turns green in MF-S2 (bodies) and MF-S3 (headers)")
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

// covers: MA-349, R10, R1, R19, R26
// Bounds and isolation: every hostile shape completes within its deadline, records
// a structure-limit or corrupt-part gap or reverts the message to v1 content with a
// visible fidelity-fallback gap, and never yields a stub.
func TestHostileMIMEIsBoundedAndNeverYieldsAStub(t *testing.T) {
	t.Skip("MA-349: the depth, leaf, parameter and header bounds, the fidelity-superset check and the fallback gap do not exist; encoder lands in MF-S0, turns green in MF-S2 (parser bounds), MF-S4 (render) and MF-S7 (repair)")
}
