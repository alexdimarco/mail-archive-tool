package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/health"
	"mail-archive-tool/internal/state"
)

// loadManifest loads the archive manifest for a test, failing on error.
func loadManifest(t *testing.T, out string) *state.Manifest {
	t.Helper()
	m, err := state.Load(filepath.Join(out, ".mailarchive-manifest.json"))
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	return m
}

// covers: MA-194, R20, S34
// On a -raw archive the three extractability surfaces AGREE: ExtractableCount's
// withEML equals what `verify` reports (WithEML) and what `extract` emits
// (Emitted) on the same archive — one shared file-presence signal, so status,
// verify and extract cannot report different numbers (design K4/QC1).
func TestExtractableCountAgreesWithVerifyAndExtract(t *testing.T) {
	out := buildRawArchive(t, map[string][][]byte{
		"Alpha": {msg("One", "one", "b1"), msg("Two", "two", "b2")},
		"Beta":  {msg("Three", "three", "b3")},
	})
	m := loadManifest(t, out)

	withEML, total := ExtractableCount(out, m)
	if total != 3 {
		t.Fatalf("total = %d, want 3 records", total)
	}
	if withEML != 3 {
		t.Fatalf("a -raw archive of 3 messages should have 3 extractable, got %d", withEML)
	}

	// verify's WithEML is the SAME code path (the shared emlPresent predicate).
	rep, err := Verify(out, VerifyOptions{}, discard(), nil)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if rep.WithEML != withEML {
		t.Fatalf("verify WithEML=%d disagrees with ExtractableCount withEML=%d", rep.WithEML, withEML)
	}

	// extract emits on the identical file-presence signal.
	dest := tmpDir(t)
	xrep, err := Extract(out, FormatEML, dest, false, discard(), nil)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if xrep.Emitted != withEML {
		t.Fatalf("extract Emitted=%d disagrees with ExtractableCount withEML=%d", xrep.Emitted, withEML)
	}
	if xrep.Skipped != 0 {
		t.Fatalf("a healthy -raw archive should skip nothing, skipped=%d", xrep.Skipped)
	}
}

// covers: MA-195, R20, S34, R18, S29
// The extractability count is FILE PRESENCE, not a recorded digest: on a
// fixity-era archive (every .eml baselined with `verify -record`) deleting one
// preserved .eml drops the count by one — verify's WithEML and status's line
// drop with it — even though the manifest STILL records that record's
// Fixity.EML, and status's line then reads "N-1 of N", never claiming all N
// records "have" a preserved original (a stale-digest count would).
func TestExtractableCountIsFilePresenceNotStaleDigest(t *testing.T) {
	out := buildRawArchive(t, map[string][][]byte{
		"Alpha": {msg("One", "one", "b1"), msg("Two", "two", "b2")},
	})
	// Baseline fixity so every .eml carries a recorded digest (a fixity-era
	// archive: a digest-based count would keep counting a since-deleted file).
	if _, err := Verify(out, VerifyOptions{Record: true}, discard(), nil); err != nil {
		t.Fatalf("verify -record: %v", err)
	}
	m := loadManifest(t, out)

	before, total := ExtractableCount(out, m)
	if total != 2 || before != 2 {
		t.Fatalf("fixture: total=%d withEML=%d, want 2/2", total, before)
	}
	recordedEML := 0
	for _, rec := range m.All() {
		if rec.Fixity != nil && rec.Fixity.EML != nil {
			recordedEML++
		}
	}
	if recordedEML != 2 {
		t.Fatalf("a fixity-era fixture should record 2 Fixity.EML, got %d", recordedEML)
	}

	// Delete one preserved .eml on disk; the manifest still records its digest.
	emls := collectFiles(t, out, ".eml")
	if len(emls) != 2 {
		t.Fatalf("fixture wrote %d .eml, want 2", len(emls))
	}
	var victim string
	for rel := range emls {
		victim = rel
		break
	}
	if err := os.Remove(filepath.Join(out, filepath.FromSlash(victim))); err != nil {
		t.Fatalf("delete .eml %s: %v", victim, err)
	}

	// The count drops by one (file presence), computed against the SAME manifest
	// that still records every digest.
	after, _ := ExtractableCount(out, m)
	if after != before-1 {
		t.Fatalf("deleting one .eml should drop the count to %d, got %d (a stale-digest count would stay %d)", before-1, after, before)
	}
	// verify agrees (both use the shared predicate).
	rep, err := Verify(out, VerifyOptions{}, discard(), nil)
	if err != nil {
		t.Fatalf("verify after delete: %v", err)
	}
	if rep.WithEML != after {
		t.Fatalf("verify WithEML=%d disagrees with ExtractableCount=%d after the delete", rep.WithEML, after)
	}
	// The manifest STILL records the deleted record's Fixity.EML — proving the
	// drop is file presence, not the recorded digest.
	stillRecorded := 0
	for _, rec := range m.All() {
		if rec.Fixity != nil && rec.Fixity.EML != nil {
			stillRecorded++
		}
	}
	if stillRecorded != 2 {
		t.Fatalf("the manifest should still record 2 Fixity.EML after the on-disk delete, got %d", stillRecorded)
	}

	// status's line reads "N-1 of N" and does not claim all N have an original.
	in := health.Input{Out: out, HasManifest: true, Messages: total, Extractable: health.Extractable{WithEML: after, Total: total}}
	line := strings.Join(health.Summary(in, health.Report{Posture: "GREEN"}), "\n")
	if !strings.Contains(line, fmt.Sprintf("Extractable: %d of %d records have a preserved original", after, total)) {
		t.Fatalf("status line does not report %d of %d:\n%s", after, total, line)
	}
	if strings.Contains(line, fmt.Sprintf("Extractable: %d of %d records have a preserved original", total, total)) {
		t.Fatalf("status must not claim all %d records have a preserved original:\n%s", total, line)
	}
}

// covers: MA-194, R20, S34
// emlPresent applies the identical path gate extract uses, so a tampered manifest
// record whose Path is not a valid in-archive .html is excluded from the
// extractable count exactly as extract would skip it — status, verify and extract
// cannot disagree even on a hand-edited manifest (QC1).
func TestEmlPresentRejectsNonHTMLPath(t *testing.T) {
	out := t.TempDir()
	// A single-segment .html path whose derived .eml is actually present on disk:
	// the weak gate (validRelPath only) would count it, the extract-identical gate
	// (len(segs) >= 2 && .html suffix) rejects it. Placing the .eml proves the
	// exclusion is the GATE, not a missing file.
	if err := os.WriteFile(filepath.Join(out, "x.eml"), []byte("m"), 0o600); err != nil {
		t.Fatal(err)
	}
	if emlPresent(out, state.Record{Path: "x.html"}) {
		t.Error("emlPresent counted a single-segment record (x.html) that extract's gate rejects")
	}
	// A non-.html path likewise must not count even if a sibling exists.
	if err := os.WriteFile(filepath.Join(out, "y.txt.eml"), []byte("m"), 0o600); err != nil {
		t.Fatal(err)
	}
	if emlPresent(out, state.Record{Path: "s/y.txt"}) {
		t.Error("emlPresent counted a non-.html record path that extract's gate rejects")
	}
	// A traversal path is always rejected.
	if emlPresent(out, state.Record{Path: "../escape.html"}) {
		t.Error("emlPresent accepted a traversal path")
	}
}
