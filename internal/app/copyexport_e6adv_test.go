package app

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/util"
)

// The tombstones of the EW-E6 adversarial pass (docs/review-extraction-workspace-e6-adversarial.md):
// each is the regression test of one confirmed finding, written red against the code at db48838
// and green with its fix. Each cites the catalog row that states the guarantee it keeps.

// e6advZipRun exports the archive's zip-only record with the entries as its attachments zip, under
// the bounds in e, and returns the result, the manifest entry and what the run wrote into
// attachments directories (expanded or not, kept or not).
func e6advZipRun(t *testing.T, e *CopyExportEnv, entries ...e6ZipEntry) (res *CopyResult, ent map[string]any, written int64) {
	t.Helper()
	a := buildE6Archive(t)
	e.fsWrap = func(f ceFS) ceFS { return &e6Counting{ceFS: f, n: &written} }
	res, ent, _ = e6WithZip(t, a, e, e6Zip(t, entries...))
	if res.Counts.Exported != 1 || res.Status != CopyStatusComplete {
		t.Fatalf("the run: %s, %+v", res.Label, res.Counts)
	}
	return res, ent, written
}

// e6advNote reports whether an entry carries an attachment note holding every one of the phrases.
func e6advNote(ent map[string]any, phrases ...string) bool {
	var notes []string
	for _, n := range ent["notes"].([]any) {
		notes = append(notes, n.(string))
	}
	return e6advHasNote(notes, phrases...)
}

// e6advHasNote is e6advNote for the notes of a run-record line.
func e6advHasNote(notes []string, phrases ...string) bool {
	for _, s := range notes {
		ok := strings.HasPrefix(s, attachNotePrefix)
		for _, p := range phrases {
			ok = ok && strings.Contains(s, p)
		}
		if ok {
			return true
		}
	}
	return false
}

// e6advDirectoryOnly is a zip that is only a central directory of n entries with empty names (46
// bytes each) and its end record: 23 MB for 500,000 entries, which Go's zip reader turns into
// about 100 MB of heap before the extraction's own cut at 1,000 entries is reached. The record's
// count is the 16 bits Go compares, so the reader takes the directory as well formed.
func e6advDirectoryOnly(n int) []byte {
	b := make([]byte, 0, 46*n+22)
	for i := 0; i < n; i++ {
		rec := make([]byte, 46)
		binary.LittleEndian.PutUint32(rec, 0x02014b50)
		b = append(b, rec...)
	}
	end := make([]byte, 22)
	binary.LittleEndian.PutUint32(end, 0x06054b50)
	binary.LittleEndian.PutUint16(end[10:], uint16(n))
	binary.LittleEndian.PutUint32(end[12:], uint32(46*n))
	return append(b, end...)
}

// e6advOnTemp is a contained writer that calls onTemp when a temp file is made in an attachments
// directory, before it is made: where a test presses Cancel, or the dashboard gives up on a run.
type e6advOnTemp struct {
	ceFS
	onTemp func()
}

func (h *e6advOnTemp) CreateTemp(dir string) (ceFile, string, error) {
	if strings.HasSuffix(dir, ".attachments") {
		h.onTemp()
	}
	return h.ceFS.CreateTemp(dir)
}

// covers: MA-428, MA-424, R5
// E6-AO3 (E6-AI3, E6-AC1): Cancel and Abandon are seen inside the message being extracted, not only
// between messages. A message of 40 attachments, and the run cancelled (or abandoned, as the
// dashboard does to a run still going 30 s after Cancel) as the third temp file is made: no more
// than three files reach the destination and no temp file stays; a cancelled run lists the files it
// placed and a note that says the rest were not extracted because the export was stopped; an
// abandoned run's result is the stop, and it writes nothing after the call, where the code went on to
// write all 40 files and was declared "not responding" while it did.
func TestCancelAndAbandonAreSeenInsideAMessagesExtraction(t *testing.T) {
	for _, abandon := range []bool{false, true} {
		name := "cancel"
		if abandon {
			name = "abandon"
		}
		t.Run(name, func(t *testing.T) {
			msgs := ceMsgs(2)
			var parts []e6Part
			for i := 0; i < 40; i++ {
				parts = append(parts, e6Part{name: fmt.Sprintf("f%02d.bin", i), body: []byte(fmt.Sprintf("part %d of the message", i))})
			}
			mime := e6Mime(msgs[0].SourceID, "forty files", parts...)
			e := newCEEnv(t)
			src := newFakeSrc()
			src.body = func(id string) []byte {
				if id == msgs[0].SourceID {
					return mime
				}
				return e6Mime(id, "second")
			}
			p := cePlan(t, e, src, msgs)
			p.Options.Attachments = true
			run := e.NewCopyRun(p, src)
			made := 0
			e.fsWrap = func(f ceFS) ceFS {
				return &e6advOnTemp{ceFS: f, onTemp: func() {
					if made++; made == 3 {
						if abandon {
							run.Abandon()
						} else {
							run.Cancel()
						}
					}
				}}
			}
			res, err := run.Run(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			onDisk := 0
			for p := range treeFiles(t, res.Root) {
				if strings.Contains(p, ".attachments/f") {
					onDisk++
				}
			}
			if onDisk > 3 {
				t.Errorf("%d attachments were written, %d of them after the call", onDisk, onDisk-3)
			}
			if made > 3 {
				t.Errorf("%d temp files were made: the extraction went on after the call", made)
			}
			if r := tempResidue(t, res.Root); len(r) != 0 {
				t.Errorf("temp files left: %v", r)
			}
			if abandon {
				if res.Label != "Stopped: the destination is not responding" {
					t.Errorf("the abandoned run ended %q", res.Label)
				}
				return
			}
			if res.Status != CopyStatusCancelled || res.Counts.Exported != 1 {
				t.Fatalf("the cancelled run: %s, %+v", res.Label, res.Counts)
			}
			first := assure.Reached(t, e5Files(t, e, res)[1], "the first message's line")
			if got := len(e6Derived(first)); got < 1 || got > 3 {
				t.Errorf("the line lists %d extracted files", got)
			}
			if !e6advHasNote(first.Notes, "export was stopped", "were not extracted", ".eml beside it") {
				t.Errorf("no note says the rest were not extracted because the export was stopped: %q", first.Notes)
			}
			if bad, missing, extra := sumsCheck(t, res.Root); len(bad)+len(missing)+len(extra) != 0 {
				t.Errorf("sums: changed %v, missing %v, unlisted %v", bad, missing, extra)
			}
		})
	}
}

// covers: MA-428, MA-424, R5
// E6-AO3, for the entries of an archived zip: a zip of 40 entries, the run cancelled as the third
// temp file is made, writes no more than three files and says the rest of its entries were not
// extracted because the export was stopped.
func TestCancelIsSeenInsideAZipsExtraction(t *testing.T) {
	var entries []e6ZipEntry
	for i := 0; i < 40; i++ {
		entries = append(entries, e6ZipEntry{name: fmt.Sprintf("f%02d.bin", i), data: []byte(fmt.Sprintf("entry %d of the zip", i))})
	}
	a := buildE6Archive(t)
	e := newCEEnv(t)
	zb := e6Zip(t, entries...)
	_, _, zipPath := a.files("zip-only")
	if err := os.WriteFile(zipPath, zb, 0o644); err != nil {
		t.Fatal(err)
	}
	a.setRec("zip-only", func(r *state.Record) { r.Fixity.Zip = digestOf(zb) })
	s := a.open()
	p := a.plan(e, s, []string{a.key("zip-only")})
	p.Options.Attachments = true
	run := e.NewCopyRun(p, s)
	made := 0
	e.fsWrap = func(f ceFS) ceFS {
		return &e6advOnTemp{ceFS: f, onTemp: func() {
			if made++; made == 3 {
				run.Cancel()
			}
		}}
	}
	res, err := run.Run(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if made > 3 {
		t.Errorf("%d temp files were made: the extraction went on after the call", made)
	}
	if res.Counts.Exported != 1 || res.Counts.Failed != 0 { // the cancel came in the last message: nothing follows it to stop
		t.Fatalf("the run: %s, %+v", res.Label, res.Counts)
	}
	first := assure.Reached(t, e5Files(t, e, res)[1], "the message's line")
	if got := len(e6Derived(first)); got < 1 || got > 3 {
		t.Errorf("the line lists %d extracted files", got)
	}
	if !e6advHasNote(first.Notes, "export was stopped", "the rest of its entries were not extracted", "the zip beside it") {
		t.Errorf("no note says the rest of the entries were not extracted because the export was stopped: %q", first.Notes)
	}
}

// covers: MA-424, R4
// E6-AO6 (E6-AC7): n parts with the same name take about n work, not n squared. ceUniqueName retried
// " (2)", " (3)" from 2 for every repeat and folded each candidate (NFC, case fold), so 8,000 parts
// of one name cost 7 s and 10,000 about 11 s, and 155 s with 200-byte names, from a message of 1.4 MB;
// the writer now remembers the next number to try for a name. The names are the same as before:
// report.pdf, report (2).pdf, report (3).pdf ... with a name that is already taken skipped.
func TestSameNamedPartsAreNamedInLinearTime(t *testing.T) {
	const n = 6000
	used, next := map[string]struct{}{}, map[string]int{}
	used[util.FoldKey("report (5).pdf")] = struct{}{} // taken by a part of that very name: the counter must step over it
	start := time.Now()
	got := make([]string, n)
	for i := range got {
		got[i] = ceUniqueNameFrom(used, next, "report.pdf")
	}
	if took := time.Since(start); took > 1500*time.Millisecond {
		t.Errorf("%d parts of one name took %v: the work is not linear", n, took)
	}
	want := []string{"report.pdf", "report (2).pdf", "report (3).pdf", "report (4).pdf", "report (6).pdf", "report (7).pdf"}
	if !slices.Equal(got[:len(want)], want) {
		t.Errorf("the first names are %q, want %q", got[:len(want)], want)
	}
	if last := got[n-1]; last != fmt.Sprintf("report (%d).pdf", n+1) {
		t.Errorf("the last name is %q", last)
	}
	seen := map[string]bool{}
	for _, g := range got {
		if seen[g] {
			t.Fatalf("%q was given twice", g)
		}
		seen[g] = true
	}
}

// covers: MA-424, R1, R29
// E6-AO4 (E6-AI1, E6-AC3): an extracted attachment that is gone, or damaged, when the export is
// finished is a note on a message that stays exported, as a readable copy is, never a failed message.
// The attachment is a convenience made from the .eml beside it, and the files antivirus products
// remove most are the ones a sender chose: the code listed the message as failed and retryable (a
// retry extracts the same file and fails the same way) and dropped its intact .eml and its other
// attachments from the sums. Now the file is dropped from the record, the message keeps its original
// and its other attachments in the manifest and the sums, and a note names the file. Finish as partial,
// which re-hashes what it finds, treats a damaged one the same way.
func TestAnExtractedAttachmentThatIsGoneIsANoteNotAFailedMessage(t *testing.T) {
	root := func(p *CopyPlan) string {
		ents, _ := os.ReadDir(p.Dest)
		if len(ents) != 1 {
			t.Fatalf("the destination holds %d entries", len(ents))
		}
		return filepath.Join(p.Dest, ents[0].Name())
	}
	withFiles := func(msgs []CopyMessage) func(string) []byte {
		return func(id string) []byte {
			return e6Mime(id, "two files", e6Part{name: "invoice.pdf", ctype: "application/pdf", body: []byte("%PDF invoice of " + id)},
				e6Part{name: "evil.exe", body: []byte("MZ the program of " + id)})
		}
	}
	setup := func(t *testing.T, msgs []CopyMessage) (*CopyExportEnv, *fakeSrc, *CopyPlan) {
		e, src := newCEEnv(t), newFakeSrc()
		src.body = withFiles(msgs)
		p := cePlan(t, e, src, msgs)
		p.Options.Attachments = true
		return e, src, p
	}

	// finalization: message 1's evil.exe is quarantined before the export is finished
	msgs := ceMsgs(2)
	e, src, p := setup(t, msgs)
	e.faults = &copyFaults{hook: func(step string) {
		if step == "W7pre" {
			r := root(p)
			os.Remove(filepath.Join(r, e5Find(t, r, "evil.exe", "of M00001")))
		}
	}}
	res := mustRun(t, e, p, src)
	if res.Counts.Exported != 2 || res.Counts.Failed != 0 || res.Status != CopyStatusComplete || res.Outcome != CopyOutcomeComplete {
		t.Fatalf("a quarantined attachment: %s %+v", res.Label, res.Counts)
	}
	man := manifestOf(t, res.Root)
	ent := man["messages"].([]any)[0].(map[string]any)
	if ent["status"] != statusExported || len(ent["files"].([]any)) != 2 || !e6advNote(ent, `"evil.exe"`, "no longer in the export folder", ".eml beside it") {
		t.Errorf("message 1: %v, want exported with its original and invoice.pdf, and a note naming evil.exe", ent)
	}
	if f := failureListed(man, msgs[0].SourceID); f != nil {
		t.Errorf("message 1 is listed as failed: %v", f)
	}
	if bad, missing, extra := sumsCheck(t, res.Root); len(bad)+len(missing)+len(extra) != 0 {
		t.Errorf("sums: changed %v, missing %v, unlisted %v: the intact original and attachment must be listed", bad, missing, extra)
	}
	if failed, _ := e.RunFailed(res.RunID); len(failed) != 0 {
		t.Errorf("a retry is offered for %v", failed)
	}
	if readme := readText(t, res.Root, copyReadmeName); !strings.Contains(readme, "Messages with an attachment that was not extracted, or only part of it: 1") {
		t.Errorf("README does not count the message with a lost attachment")
	}

	// Finish as partial, killed after message 2: message 1's evil.exe is gone, message 2's damaged
	msgs = ceMsgs(3)
	e, src, p = setup(t, msgs)
	e.faults = &copyFaults{killAt: "W6:2"}
	k := mustRun(t, e, p, src)
	e.faults = nil
	if k.Killed != "W6:2" {
		t.Fatalf("killed at %q", k.Killed)
	}
	os.Remove(filepath.Join(k.Root, e5Find(t, k.Root, "evil.exe", "of M00001")))
	damaged := e5Find(t, k.Root, "evil.exe", "of M00002")
	b, _ := os.ReadFile(filepath.Join(k.Root, damaged))
	b[len(b)/2] ^= 0x01
	os.WriteFile(filepath.Join(k.Root, damaged), b, 0o600)
	fin, err := e.FinishAsPartial(k.RunID)
	if err != nil {
		t.Fatalf("Finish as partial: %v", err)
	}
	if fin.Counts.Exported != 2 || fin.Counts.Failed != 0 || fin.Counts.NotAttempted != 1 {
		t.Fatalf("Finish as partial: %s %+v", fin.Label, fin.Counts)
	}
	man = manifestOf(t, k.Root)
	for i, want := range []string{"no longer in the export folder", "damaged when the computer stopped"} {
		ent := man["messages"].([]any)[i].(map[string]any)
		if ent["status"] != statusExported || len(ent["files"].([]any)) != 2 || !e6advNote(ent, `"evil.exe"`, want) {
			t.Errorf("message %d: %v, want exported with its original and invoice.pdf, and a note that says %q", i+1, ent, want)
		}
	}
	bad, missing, extra := sumsCheck(t, k.Root)
	if len(bad)+len(missing) != 0 || len(extra) != 1 || extra[0] != damaged {
		t.Errorf("sums: changed %v, missing %v, unlisted %v: the damaged attachment must be the one file the sums do not list", bad, missing, extra)
	}
	if failed, _ := e.RunFailed(k.RunID); len(failed) != 0 {
		t.Errorf("a lost attachment is offered for retry: %v", failed)
	}
}

// covers: MA-424, MA-520, R29
// E6-AO4 (E6-AI1), the check's half: Check this export names an extracted attachment that changed or
// is missing as what it is, a convenience made from the .eml beside it that says nothing about that
// .eml, with a remedy that says every original is as it was; a clean export with attachments is GREEN
// (the positive twin), and a changed original, alone or with a changed attachment, is the usual RED.
func TestCheckThisExportNamesAnExtractedAttachmentAsOne(t *testing.T) {
	fresh := func() (*CopyExportEnv, *CopyResult) {
		e, src := newCEEnv(t), newFakeSrc()
		src.body = func(id string) []byte {
			return e6Mime(id, "two files", e6Part{name: "a.pdf", ctype: "application/pdf", body: []byte("%PDF a of " + id)},
				e6Part{name: "b.pdf", ctype: "application/pdf", body: []byte("%PDF b of " + id)})
		}
		p := cePlan(t, e, src, ceMsgs(2))
		p.Options.Attachments = true
		return e, mustRun(t, e, p, src)
	}
	e, res := fresh()
	chk, err := e.CheckExport(res.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if !chk.OK || chk.Files != 8 || chk.DerivedOnly || len(chk.Problems) != 0 {
		t.Fatalf("a clean export with extracted attachments: %+v, want GREEN with 8 files (2 messages of three, README, manifest)", chk)
	}

	damage := func(t *testing.T, e *CopyExportEnv, res *CopyResult, attachments, eml bool) *CopyCheck {
		t.Helper()
		if attachments {
			changed, gone := e5Find(t, res.Root, "a.pdf", "of M00001"), e5Find(t, res.Root, "b.pdf", "of M00002")
			b, _ := os.ReadFile(filepath.Join(res.Root, changed))
			os.WriteFile(filepath.Join(res.Root, changed), append(b, '!'), 0o600)
			os.Remove(filepath.Join(res.Root, gone))
		}
		if eml {
			p := filepath.Join(res.Root, e5Find(t, res.Root, ".eml", "m00002@x"))
			b, _ := os.ReadFile(p)
			os.WriteFile(p, append(b, '!'), 0o600)
		}
		chk, err := e.CheckExport(res.RunID)
		if err != nil {
			t.Fatal(err)
		}
		return chk
	}
	e, res = fresh()
	chk = damage(t, e, res, true, false)
	said := strings.Join(chk.Problems, "\n")
	if chk.OK || !chk.DerivedOnly || chk.ReadableOnly || len(chk.Changed) != 1 || len(chk.Missing) != 1 || chk.Remedy != checkDerivedRemedy ||
		!strings.Contains(said, "the extracted attachment "+chk.Changed[0]+" has changed") || !strings.Contains(said, "the extracted attachment "+chk.Missing[0]+" is missing") ||
		!strings.Contains(said, "says nothing about that .eml") || !strings.Contains(chk.Remedy, "Every original in this folder is as it was") {
		t.Errorf("a changed and a missing attachment: %+v", chk)
	}

	e, res = fresh()
	chk = damage(t, e, res, false, true)
	if chk.OK || chk.DerivedOnly || chk.Remedy != checkRemedy || strings.Contains(strings.Join(chk.Problems, "\n"), "extracted attachment") {
		t.Errorf("a changed original: %+v, want the usual remedy and no word of attachments", chk)
	}

	e, res = fresh()
	chk = damage(t, e, res, true, true)
	if chk.OK || chk.DerivedOnly || chk.Remedy != checkRemedy {
		t.Errorf("a changed original with changed attachments: %+v, want the usual remedy", chk)
	}
}

// covers: MA-424, R4, R29
// E6-AO8, E6-AO9, through the engine: names a sender chose with C1 controls, line separators, bidi marks
// and a leading dash are written under the stripped and prefixed names, and SHA256SUMS.txt, the
// manifest and the directory hold those names only, so a line splitter reads the sums as the
// number of files it lists, with no line broken or forged, and no name is an option to `tar *`.
func TestHostileAttachmentNamesNeverReachTheSumsOrTheManifestRaw(t *testing.T) {
	c := func(r rune) string { return string(r) }
	hostile := []string{"x" + c(0x9b) + "2Jy.txt", "line" + c(0x85) + "two.txt", "sep" + c(0x2028) + "forged-line.txt", "invoice" + c(0x200f) + "fdp.exe", "--checkpoint-action=exec=sh shell.sh", "-rf"}
	real := originalParser
	t.Cleanup(func() { originalParser = real })
	originalParser = func(b []byte) *model.Message {
		m := real(b)
		for i := range m.Attachments {
			m.Attachments[i].Filename = hostile[i]
		}
		return m
	}
	var parts []e6Part
	for i := range hostile {
		parts = append(parts, e6Part{name: fmt.Sprintf("p%d.bin", i), body: []byte(fmt.Sprintf("part %d", i))})
	}
	msgs := ceMsgs(1)
	e := newCEEnv(t)
	res, _ := e6Run(t, e, msgs, false, func(id string) []byte { return e6Mime(id, "hostile names", parts...) })
	if res.Counts.Exported != 1 || res.Outcome != CopyOutcomeComplete {
		t.Fatalf("%s, %+v", res.Label, res.Counts)
	}
	forbidden := c(0x9b) + c(0x85) + c(0x2028) + c(0x2029) + c(0x200f) + c(0x200e) + c(0x61c) + c(0x200b) + c(0xfeff) + "\x7f"
	sums := readText(t, res.Root, copySumsName)
	manifest := readText(t, res.Root, copyManifestName)
	if strings.ContainsAny(sums, forbidden) || strings.ContainsAny(manifest, forbidden) {
		t.Errorf("a control or an invisible mark reached the sums or the manifest:\n%q", sums)
	}
	if got := len(strings.Split(strings.TrimSuffix(sums, "\n"), "\n")); got != 2+len(hostile)+1 { // the .eml, the README and manifest, the attachments
		t.Errorf("the sums have %d lines, want %d", got, 2+len(hostile)+1)
	}
	names := map[string]bool{}
	for p := range treeFiles(t, res.Root) {
		if i := strings.Index(p, ".attachments/"); i >= 0 {
			names[p[i+len(".attachments/"):]] = true
		}
	}
	for _, want := range []string{"x2Jy.txt", "linetwo.txt", "sepforged-line.txt", "invoicefdp.exe", "_--checkpoint-action=exec=sh shell.sh", "_-rf"} {
		if !names[want] {
			t.Errorf("no attachment named %q among %v", want, names)
		}
	}
	if bad, missing, extra := sumsCheck(t, res.Root); len(bad)+len(missing)+len(extra) != 0 {
		t.Errorf("sums: changed %v, missing %v, unlisted %v", bad, missing, extra)
	}
}

// covers: MA-424, MA-427, R29
// E6-AI4 (E6-AC2): extracted attachments never take the space the originals still to be exported need.
// The plan counts an extracted file as 0.8 of its message, but an 8-bit or plain-text part is about
// the message's own size and a zip expands more, and each file was checked only against the free space
// less the reserve, so the earlier messages' conveniences used what the later originals were to have:
// on a destination with the estimate, the reserve and 2,000 bytes, six messages with 25 KB text
// attachments stopped with "the destination is full" after five, the sixth original not exported
// (R-g), where without the option all six fit. A derived file is now checked against what is left less
// the size of every original after its own, so the originals all fit and the attachments that no
// longer do are notes (no space left, export this message again).
func TestExtractedAttachmentsNeverCrowdOutALaterOriginal(t *testing.T) {
	msgs := ceMsgs(6)
	body := map[string][]byte{}
	for i := range msgs {
		id := msgs[i].SourceID
		text := strings.Repeat("a line of the text attachment of "+id+"\r\n", 25000/36)
		body[id] = e6Mime(id, "text attachment", e6Part{name: "notes.txt", ctype: "text/plain; charset=utf-8", cte: "8bit", raw: text})
		msgs[i].SizeHint = int64(len(body[id]))
	}
	e := newCEEnv(t)
	src := newFakeSrc()
	src.body = func(id string) []byte { return body[id] }
	p := cePlan(t, e, src, msgs)
	p.Options.Attachments = true
	p.EstimateBytes, p.OverCap, p.UnknownSize, p.Over259, p.LongestPath, p.SamplePaths = 0, 0, 0, 0, 0, nil
	e.sizePlan(p)
	e.freeDest = e6Disk(p.Dest, p.EstimateBytes+p.Reserve+2000)
	res := mustRun(t, e, p, src)
	if res.Counts.Exported != 6 || res.Counts.NotAttempted != 0 || res.Stop != nil {
		t.Fatalf("%s, %+v, stop %+v: an original was crowded out by extracted attachments", res.Label, res.Counts, res.Stop)
	}
	if got := len(treeFilesWith(t, res.Root, ".eml")); got != 6 {
		t.Errorf("%d originals in the export, want 6", got)
	}
	lines := e5Files(t, e, res)
	noSpace := 0
	for n := 1; n <= 6; n++ {
		if e6advHasNote(assure.Reached(t, lines[n], "a message's line").Notes, "no space was left", "export this message again") {
			noSpace++
		}
	}
	if noSpace == 0 {
		t.Error("no attachment was left out for want of space, so the destination was not as tight as the test needs")
	}
	if bad, missing, extra := sumsCheck(t, res.Root); len(bad)+len(missing)+len(extra) != 0 {
		t.Errorf("sums: changed %v, missing %v, unlisted %v", bad, missing, extra)
	}
}

// covers: MA-424, R1
// E6-AO7 (E6-AI5, E6-AC6): the zip's list of entries is bounded before it is read whole. Go's reader
// allocates one record for every entry of the central directory (about 200 bytes each) before the
// extraction can cut the list at 1,000, and a zip's size bound (256 MiB) is not an entry bound, so a
// zip of 500,000 empty entries cost about 100 MB of heap and one of 256 MiB over a gigabyte, from a
// message the archive writer could never make (it keeps at most 10,000 parts). The directory is
// read through a budget (4 MiB, 10,000 entries with long names and room to spare), and a zip over it is not
// opened: one note says so, nothing is extracted, the message stays exported. A zip of 1,002
// entries (TestArchiveZipEntriesAreExtractedWithinBounds) still has its first 1,000 extracted.
func TestAZipsListOfEntriesIsBoundedBeforeItIsRead(t *testing.T) {
	data := e6advDirectoryOnly(500000)
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	zr, _, err := openZip(data)
	runtime.ReadMemStats(&after)
	if err == nil || zr != nil {
		t.Errorf("a zip of 500,000 entries was opened (%v)", err)
	}
	if grown := after.TotalAlloc - before.TotalAlloc; grown > 32<<20 {
		t.Errorf("opening it allocated %d MiB: the whole directory was read before it was refused", grown>>20)
	}

	// the positive twin: what the archive writer can make, 10,000 entries with names of 255 bytes, opens
	var legit []e6ZipEntry
	for i := 0; i < 10000; i++ {
		legit = append(legit, e6ZipEntry{name: strings.Repeat("n", 245) + fmt.Sprintf("%05d", i), store: true})
	}
	if zr, _, err := openZip(e6Zip(t, legit...)); err != nil || len(zr.File) != 10000 {
		t.Errorf("a zip of 10,000 entries with long names was refused: %v", err)
	}

	res, ent, _ := e6WithZip(t, buildE6Archive(t), newCEEnv(t), data)
	if res.Counts.Exported != 1 || res.Status != CopyStatusComplete {
		t.Fatalf("the run: %s, %+v", res.Label, res.Counts)
	}
	if got := e6Extracted(t, res.Root, ent); len(got) != 0 {
		t.Errorf("extracted %v", e6Sizes(got))
	}
	if !e6advNote(ent, "could not be opened", "list of entries is larger than this version reads", "the zip beside it") {
		t.Errorf("no note says the list of entries is too large: %v", ent["notes"])
	}
}

// covers: MA-424, R4, R1
// E6-AO2 (E6-AI2, E6-AC4): the 200:1 bound is judged on the compressed bytes the entry really has,
// not on the compressed size its header claims, which is the sender's to write. A deflate stream of
// 20 MiB of zeros (about 20 KB, 1000:1) whose header claims a compressed size that makes it look
// 200:1 or less, or one so large that 200 times it wraps a uint64, is not extracted and not written,
// and is a note that names a zip bomb; the entry beside it is extracted. The code judged the claim,
// extracted all 20 MiB and said nothing.
func TestAZipBombIsJudgedOnItsRealPackedSizeNotItsHeaders(t *testing.T) {
	bomb := make([]byte, 20<<20)
	for name, claim := range map[string]uint64{"a claim of 110,000 bytes": 110000, "a claim that wraps 200 times": 461168601842843648} {
		t.Run(name, func(t *testing.T) {
			res, ent, written := e6advZipRun(t, newCEEnv(t),
				e6ZipEntry{name: "liar.bin", data: bomb, raw: true, declared: uint64(len(bomb)), packed: claim},
				e6ZipEntry{name: "ok.txt", data: []byte("fine")})
			got := e6Extracted(t, res.Root, ent)
			if _, ok := got["liar.bin"]; ok || string(got["ok.txt"]) != "fine" || len(got) != 1 {
				t.Errorf("extracted (name: bytes) %v", e6Sizes(got))
			}
			if !e6advNote(ent, `"liar.bin"`, "zip bomb") {
				t.Errorf("no note says liar.bin is taken for a zip bomb: %v", ent["notes"])
			}
			if written > 1<<10+4 {
				t.Errorf("%d bytes were written for an entry that expands 1000:1: it was expanded to disk", written)
			}
		})
	}
}

// covers: MA-424, R1
// E6-AO1 (E6-AC5): an entry that streams its whole size and then fails (a damaged checksum) is
// charged against the message's total like one that succeeds, so the per-message bound limits what
// is written, not only what is kept. Eight entries of 512 KiB that all fail, under a 1.5 MiB total:
// no more than 1.5 MiB reaches the destination (three entries) and the rest are notes that the
// total would be passed, where the code charged only the entries that succeeded and wrote all 4 MiB.
func TestFailedZipEntriesCountTowardThePerMessageTotal(t *testing.T) {
	e := newCEEnv(t)
	e.zipLimit = attachBounds{totalBytes: 3 << 19}
	var entries []e6ZipEntry
	for i := 0; i < 8; i++ {
		entries = append(entries, e6ZipEntry{name: "bad" + string(rune('a'+i)) + ".bin", data: append([]byte{byte(i + 1)}, e5Image(512<<10-1)...), raw: true, declared: 512 << 10, crc: 12345})
	}
	_, ent, written := e6advZipRun(t, e, entries...)
	if written > 3<<19 {
		t.Errorf("%d bytes were written for entries that all fail, against a total of %d: a failed entry is not charged", written, 3<<19)
	}
	if !e6advNote(ent, "extracting it would take", "past 1536 KiB") {
		t.Errorf("no note says the total would be passed: %v", ent["notes"])
	}
}
