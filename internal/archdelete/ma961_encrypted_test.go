package archdelete

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/vault"
)

const unitName = "ab0123456789abcdef0123456789abcd"

// encryptedArchive is an encrypted archive's folder in its validated layouts, with
// a few names those layouts do not hold.
func encryptedArchive(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range map[string]string{
		vault.FileName:                  `{"id":"x"}`,
		vault.BackupName:                `{"id":"x"}`,
		container.EnvelopeName:          fmt.Sprintf(`{"version":%d,"encrypted":true}`, vault.EncryptedManifestVersion),
		container.PointerName:           "ptr",
		container.MirrorName:            "ptr",
		container.InventoryName:         "inv",
		".mailarchive-lastrun.json":     "{}",
		".mailarchive-runhistory.json":  `[{"when":"2024-02-02T02:00:00Z","status":"ok","encrypted":true}]`,
		".mailarchive-paused.json":      "{}",
		"README.txt":                    "readme",
		".mailarchive-manifest-12.tmp":  "temp",
		".mailarchive.lock":             "",
		".DS_Store":                     "litter",
		"units/ab/" + unitName:          "unit",
		"units/ab/.mailarchive-7.tmp":   "temp",
		"units/ab/stray.txt":            "not a unit",
		"chunks/cd/cd" + unitName[2:]:   "chunk",
		"inventory/0a.1":                "shard",
		"inventory/notes.txt":           "not a shard",
		"program/mailarchive-macos.zip": "copy",
	} {
		writeFile(t, filepath.Join(dir, filepath.FromSlash(name)), content)
	}
	return dir
}

// covers: MA-961, R36
func TestAnEncryptedPlanListsItsLayoutsAndTheVaultLast(t *testing.T) {
	dir := encryptedArchive(t)
	before := tree(t, dir)
	p := mustPlan(t, dir, testDeps())
	if p.Kind != Encrypted {
		t.Fatalf("kind = %v", p.Kind)
	}
	if d := setDiff("removed", removeRels(p), []string{
		container.EnvelopeName, container.PointerName, container.MirrorName, container.InventoryName,
		".mailarchive-lastrun.json", ".mailarchive-runhistory.json", ".mailarchive-paused.json", "README.txt", ".mailarchive-manifest-12.tmp",
		"units/ab/" + unitName, "units/ab/.mailarchive-7.tmp", "chunks/cd/cd" + unitName[2:], "inventory/0a.1", "program/mailarchive-macos.zip",
		vault.BackupName, vault.FileName,
	}); d != "" {
		t.Fatal(d)
	}
	n := len(p.Remove)
	if p.Remove[n-2].Rel != vault.BackupName || p.Remove[n-1].Rel != vault.FileName {
		t.Fatalf("the vault must be last: %q", removeRels(p)[n-2:])
	}
	if d := setDiff("kept", p.Kept, []string{"units/ab/stray.txt", "inventory/notes.txt"}); d != "" {
		t.Fatal(d)
	}
	if d := setDiff("litter", p.Litter, []string{".DS_Store"}); d != "" {
		t.Fatal(d)
	}
	if !p.Paused || p.Anchor.Name != vault.FileName || p.Anchor.SHA256 == "" {
		t.Fatalf("paused %v, anchor %+v", p.Paused, p.Anchor)
	}
	if !sameTree(before, tree(t, dir)) {
		t.Fatal("planning changed the folder")
	}
}

// covers: MA-961, MA-963, R36
func TestAnEncryptedPlanRecordsTheRecordAndSaysWhatIsSealed(t *testing.T) {
	dir := encryptedArchive(t)
	deps := testDeps()
	// Positive twin first: recorded, key open here.
	deps.Record = func(f string) (RecordRef, error) {
		return RecordRef{Recorded: true, ID: "0123456789abcdef0123456789abcdef", Spelling: f}, nil
	}
	deps.Sealed = func(string, string) (Captured, bool) {
		return Captured{Messages: 42, MailFrom: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), MailTo: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)}, true
	}
	p := mustPlan(t, dir, deps)
	if !p.Record.Recorded || p.Record.Spelling != dir || !p.Captured.Known || !p.Captured.KeyOpen || p.Captured.Messages != 42 {
		t.Fatalf("record %+v captured %+v", p.Record, p.Captured)
	}
	text := string(RecordText(RecordInput{Status: StatusPlanned, Plan: p}))
	if !strings.Contains(text, "Archive id: 0123456789abcdef0123456789abcdef") || !strings.Contains(text, "Messages: 42 (as recorded in the archive)") {
		t.Fatalf("record:\n%s", text)
	}

	// Key not open here: the three sealed values are not known, the last backup is.
	deps.Sealed = func(string, string) (Captured, bool) { return Captured{}, false }
	p = mustPlan(t, dir, deps)
	text = string(RecordText(RecordInput{Status: StatusPlanned, Plan: p}))
	for _, want := range []string{"Messages: not known: encrypted, key not open", "Mail dates: not known: encrypted, key not open",
		"First run: not known: encrypted, key not open", "Last backup: " + localStamp(time.Date(2024, 2, 2, 2, 0, 0, 0, time.UTC)) + " (as recorded in the archive)"} { // one form (MA-1028, B9)
		if !strings.Contains(text, want+"\n") {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
	if strings.Contains(text, "Messages: 0") {
		t.Fatal("a sealed value is never estimated")
	}
}

// covers: MA-961, R36
func TestAnEncryptedFolderHoldingOtherFilesIsMixed(t *testing.T) {
	dir := encryptedArchive(t)
	writeFile(t, filepath.Join(dir, "taxes.pdf"), "the person's")
	{
		rc, msg, opts := refusedCase(t, dir, testDeps(), CaseMixed, "more than one kind")
		assure.Refused(t, rc, msg, opts...)
	}
}

// covers: MA-961, R36
func TestRecordLookupFindsOnlyThisFoldersRecord(t *testing.T) {
	dir := encryptedArchive(t)
	ref, err := RecordLookup(dir)
	if err != nil || ref.Recorded {
		t.Fatalf("an unrecorded folder: %+v %v", ref, err)
	}
}
