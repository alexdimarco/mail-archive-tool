package state

import (
	"path/filepath"
	"testing"
	"time"
)

// covers: MA-222, R5, R12, S39
// Manifest format v5 adds two Record fields for the option-D live-dedup (design
// rev-4): FpScheme (the fingerprint-scheme tag — 0/fpSchemeLegacy on any pre-v5
// or empty fp, which is NOT comparable to a current fingerprint, so the download
// #fp-split adopts-never-splits against it) and AlsoFiles (extra on-disk paths a
// collapse folded into this record, which redaction deletes). Both round-trip
// across save+load, and a pre-v5 record loads with FpScheme==fpSchemeLegacy and
// no AlsoFiles.
func TestV5SchemeAndAlsoFilesRoundTrip(t *testing.T) {
	t1 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	cur := LiveKey("mbox", "mid:<cur@x>")
	leg := LiveKey("mbox", "mid:<leg@x>")

	// A v5 archive: one current-scheme record with a loser AlsoFiles, one record
	// left explicitly legacy-scheme.
	path := filepath.Join(t.TempDir(), "v5.json")
	writeFullManifest(t, path, 5, map[string]Record{
		cur: {Path: "mbox/Inbox/cur.html", Folder: "Inbox", ExportedAt: t1, FirstFolder: "Inbox", FirstSeen: t1, LastSeen: t1, Present: true,
			Fingerprint: "cccccccccccccccc", FpScheme: FpSchemeCurrent, AlsoFiles: []string{"mbox/Trash/cur.html", "mbox/Trash/cur-attachments.zip"}},
		leg: {Path: "mbox/Inbox/leg.html", Folder: "Inbox", ExportedAt: t1, FirstFolder: "Inbox", FirstSeen: t1, LastSeen: t1, Present: true,
			Fingerprint: "llllllllllllllll"}, // FpScheme omitted → fpSchemeLegacy
	})
	m, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	rc, _ := m.Get(cur)
	if rc.FpScheme != FpSchemeCurrent {
		t.Errorf("current record FpScheme = %d, want %d", rc.FpScheme, FpSchemeCurrent)
	}
	if len(rc.AlsoFiles) != 2 || rc.AlsoFiles[0] != "mbox/Trash/cur.html" {
		t.Errorf("AlsoFiles did not load: %v", rc.AlsoFiles)
	}
	if rl, _ := m.Get(leg); rl.FpScheme != fpSchemeLegacy {
		t.Errorf("record with omitted FpScheme loaded as %d, want fpSchemeLegacy(%d)", rl.FpScheme, fpSchemeLegacy)
	}

	// Write round-trip: Save then reload preserves both fields.
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	m2, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	rc2, _ := m2.Get(cur)
	if rc2.FpScheme != FpSchemeCurrent || len(rc2.AlsoFiles) != 2 {
		t.Errorf("v5 fields did not survive save+load: FpScheme=%d AlsoFiles=%v", rc2.FpScheme, rc2.AlsoFiles)
	}
}
