package archdelete

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/vault"
)

const recordedID = "0123456789abcdef0123456789abcdef"

// recordFolder records folder as encrypted archive id in this computer's record
// (TestMain points the record at a throwaway folder).
func recordFolder(t *testing.T, id, folder string) {
	t.Helper()
	if _, err := encrecord.Update("test", func(r *encrecord.Record) error { return r.Add(id, folder, false, time.Now()) }); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		encrecord.Update("test", func(r *encrecord.Record) error { r.ForgetID(id); return nil })
	})
}

// covers: MA-961, R36
// RecordLookup gives the record's ID and Spelling for a folder this computer
// recorded, and nothing for a folder inside a recorded one: only a record whose
// folder is this folder, not one above it (del-s1a-A2-08).
func TestRecordLookupGivesThisFoldersRecordAndNotOneAbove(t *testing.T) {
	dir := encryptedArchive(t)
	recordFolder(t, recordedID, dir)
	ref, err := RecordLookup(dir)
	if err != nil || !ref.Recorded || ref.ID != recordedID || ref.Spelling == "" || !encrecord.Names(dir, ref.Spelling) {
		t.Fatalf("a recorded folder: %+v %v", ref, err)
	}
	inner := filepath.Join(dir, "units")
	ref, err = RecordLookup(inner)
	if err != nil || ref.Recorded {
		t.Fatalf("a folder inside a recorded one is not recorded: %+v %v", ref, err)
	}
}

// covers: MA-961, MA-963, R36
// With no vault file, an encrypted archive is still recognised by its sealed
// record: the first of its files present is the anchor, removed last, and the
// saved plan names the digest as that file's, never as a vault file's
// (del-s1a-A2-07).
func TestAnEncryptedArchiveWithNoVaultFileHasItsPointerLast(t *testing.T) {
	dir := encryptedArchive(t)
	// Positive twin first: with its vault, the vault file is last and named so.
	p := mustPlan(t, dir, testDeps())
	if last := p.Remove[len(p.Remove)-1]; last.Rel != vault.FileName || !strings.Contains(string(RecordText(RecordInput{Status: StatusPlanned, Plan: p})), "\nVault file SHA-256: ") {
		t.Fatalf("last = %+v", last)
	}
	os.Remove(filepath.Join(dir, vault.FileName))
	os.Remove(filepath.Join(dir, vault.BackupName))
	p = mustPlan(t, dir, testDeps())
	if p.Kind != Encrypted {
		t.Fatalf("kind = %v", p.Kind)
	}
	last := p.Remove[len(p.Remove)-1]
	if last.Rel != container.PointerName || last.Kind != Anchor || p.Anchor.Name != container.PointerName {
		t.Fatalf("last = %+v, anchor %+v", last, p.Anchor)
	}
	text := string(RecordText(RecordInput{Status: StatusPlanned, Plan: p}))
	if !containsLine(text, "Manifest pointer SHA-256: "+p.Anchor.SHA256) || strings.Contains(text, "Vault file SHA-256") {
		t.Fatalf("record:\n%s", text)
	}
}

// covers: MA-961, MA-963, R36
// The saved plan gives an encrypted archive's id whether or not this computer
// recorded it: its vault file's, read without the key, else its manifest
// envelope's (conformance gap 3).
func TestTheArchiveIdIsGivenForAnUnrecordedEncryptedArchive(t *testing.T) {
	vault.UseTestCost()
	v, _, _, err := vault.Create("a long enough test password")
	if err != nil {
		t.Fatal(err)
	}
	data, err := v.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	dir := encryptedArchive(t)
	writeFile(t, filepath.Join(dir, vault.FileName), string(data))
	// Positive twin first: recorded, the record's ID.
	deps := testDeps()
	deps.Record = func(string) (RecordRef, error) { return RecordRef{Recorded: true, ID: recordedID, Spelling: dir}, nil }
	if text := string(RecordText(RecordInput{Status: StatusPlanned, Plan: mustPlan(t, dir, deps)})); !containsLine(text, "Archive id: "+recordedID) {
		t.Fatalf("record:\n%s", text)
	}
	p := mustPlan(t, dir, testDeps())
	if p.ArchiveID != v.ArchiveID.String() {
		t.Fatalf("archive id %q, want the vault's %q", p.ArchiveID, v.ArchiveID)
	}
	if text := string(RecordText(RecordInput{Status: StatusPlanned, Plan: p})); !containsLine(text, "Archive id: "+v.ArchiveID.String()) {
		t.Fatalf("record:\n%s", text)
	}
	// No vault: the envelope's.
	os.Remove(filepath.Join(dir, vault.FileName))
	os.Remove(filepath.Join(dir, vault.BackupName))
	writeFile(t, filepath.Join(dir, container.EnvelopeName), fmt.Sprintf(`{"version":%d,"encrypted":true,"archive_id":"%s","pointer":"x"}`, vault.EncryptedManifestVersion, recordedID))
	if p := mustPlan(t, dir, testDeps()); p.ArchiveID != recordedID {
		t.Fatalf("archive id %q, want the envelope's", p.ArchiveID)
	}
}

// covers: MA-961, R36
// An encrypted archive's root names are encmark's reserved names, one list: a root
// holding every one of them is planned, never refused as mixed, and each file among
// them is listed (conformance gap 6).
func TestEveryReservedNameOfAnEncryptedRootIsTheArchives(t *testing.T) {
	dir := encryptedArchive(t)
	folders := map[string]bool{"units": true, "chunks": true, "inventory": true, "program": true}
	for _, n := range encmark.ReservedNames() {
		if folders[n] {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, n)); err != nil {
			writeFile(t, filepath.Join(dir, n), "x")
		}
	}
	p := mustPlan(t, dir, testDeps())
	for _, n := range encmark.ReservedNames() {
		if folders[n] || n == lockName {
			continue
		}
		if !contains(removeRels(p), n) {
			t.Fatalf("reserved name %s must be listed: %q", n, removeRels(p))
		}
	}
	// Positive twin of the refusal: a name outside the list is mixed.
	writeFile(t, filepath.Join(dir, ".mailarchive-something-new.json"), "x")
	rc, msg, opts := refusedCase(t, dir, testDeps(), CaseMixed, "more than one kind")
	assure.Refused(t, rc, msg, opts...)
}

// covers: MA-961, R36
// This computer's record of encrypted archives that cannot be read refuses the
// plan with its own case, naming the cause: the plan could not say whether step 8
// must forget the key (conformance 4; not in §7, for the operator).
func TestAnUnreadableRecordOfEncryptedArchivesRefuses(t *testing.T) {
	dir := encryptedArchive(t)
	deps := testDeps()
	deps.Record = func(string) (RecordRef, error) { return RecordRef{}, nil }
	mustPlan(t, dir, deps) // positive twin
	deps.Record = func(string) (RecordRef, error) { return RecordRef{}, errors.New("the record is damaged") }
	rc, msg, opts := refusedCase(t, dir, deps, CaseRecord, "This computer's record of encrypted archives cannot be read", "forget this archive's key", "the record is damaged")
	assure.Refused(t, rc, msg, opts...)
}

// covers: MA-961, R36
// Whether a folder is encrypted is encmark's answer (MA-770's gate), so a folder
// this computer recorded as an encrypted archive, stripped of its key files and
// replanted with a plaintext manifest, is mixed — never planned as a plain archive
// whose deletion would leave this computer's record and key behind.
func TestARecordedEncryptedFolderReplantedWithPlaintextIsMixed(t *testing.T) {
	dir := smallArchive(t, t.TempDir())
	mustPlan(t, dir, testDeps()) // positive twin: not recorded, a plain archive
	recordFolder(t, recordedID, dir)
	rc, msg, opts := refusedCase(t, dir, testDeps(), CaseMixed, "This folder holds files of more than one kind. Delete it by hand.")
	assure.Refused(t, rc, msg, opts...)
}

// covers: MA-961, R36
// A name Windows reserves for a device at an encrypted archive's root is a name the
// archive never holds: mixed, never "reconnect" (del-s1a-A1-01).
func TestADeviceNameAtAnEncryptedRootIsMixed(t *testing.T) {
	dir := encryptedArchive(t)
	mustPlan(t, dir, testDeps()) // positive twin
	writeAnyName(t, filepath.Join(dir, "nul"), "")
	_, err := Plan(dir, testDeps())
	r := AsRefusal(err)
	if r == nil || r.Case != CaseMixed {
		t.Fatalf("want mixed, got %v", err)
	}
	assure.Refused(t, r.ExitCode(), r.Message, assure.Code(1), assure.Names("This folder holds files of more than one kind. Delete it by hand."))
}
