package keyring

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/vault"
)

// covers: MA-777, R37
func TestAnOpenChecksTheKeyFileNamesTheRecordedArchive(t *testing.T) {
	freshRecord(t)
	outA, rkA, a := made(t, nil)
	outB, _, b := made(t, nil)

	// Positive twins: each archive opens with its password and its recovery key.
	u, err := Open(outA, Password(testPassword), OpenOptions{})
	if err != nil {
		t.Fatalf("positive twin: open A with its password: %v", err)
	}
	assure.Reached(t, u.ArchiveID() == a.ArchiveID() && u.How() == ByPassword, "A opened by its password")
	u.Destroy()
	u, err = Open(outA, RecoveryKey(rkA), OpenOptions{})
	if err != nil {
		t.Fatalf("positive twin: open A with its recovery key: %v", err)
	}
	assure.Reached(t, u.How() == ByRecoveryKey, "A opened by its recovery key")
	u.Destroy()

	// B's key files moved into A's recorded folder are refused, though B's password
	// opens them: the folder is recorded as A.
	for _, name := range []string{vault.FileName, vault.BackupName} {
		raw, err := os.ReadFile(filepath.Join(outB, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(outA, name), raw, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// A read is refused by the key file's check against the recorded id.
	_, err = Open(outA, Password(testPassword), OpenOptions{})
	code, msg := refusal(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names(a.ArchiveID().String(), b.ArchiveID().String(), outA))
	if !errors.Is(err, vault.ErrTampered) {
		t.Fatalf("a swapped key file at a recorded folder is tampering, got %v", err)
	}
	// A write is refused first, in the recorded folder's own words (case 1, §4.5).
	_, err = Open(outA, Password(testPassword), OpenOptions{Write: true})
	code, msg = refusal(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names(outA, "different encrypted archive"))
	if !errors.Is(err, vault.ErrTampered) {
		t.Fatalf("a swapped key file at a recorded folder is tampering, got %v", err)
	}
}

// covers: MA-777, R37
func TestOnlyAnOpenForAWriteOrARememberRecordsTheFolder(t *testing.T) {
	freshRecord(t)
	out, _, a := made(t, nil)
	id := a.ArchiveID().String()
	// The folder is now unrecorded: a copy handed over, say.
	change(t, func(r *encrecord.Record) error { delete(r.Archives, id); return nil })

	u, err := Open(out, Password(testPassword), OpenOptions{})
	if err != nil {
		t.Fatalf("a read open: %v", err)
	}
	u.Destroy()
	if got := recordedID(t, out); got != "" {
		t.Fatalf("a read-only open recorded the folder under %s (§3.3: a read is not an adoption)", got)
	}

	u, err = Open(out, Password(testPassword), OpenOptions{Write: true})
	if err != nil {
		t.Fatalf("a write open: %v", err)
	}
	u.Destroy()
	assure.Reached(t, recordedID(t, out), "the folder recorded by a write open")
	if got := recordedID(t, out); got != id {
		t.Fatalf("the write open recorded %s, want %s", got, id)
	}

	change(t, func(r *encrecord.Record) error { delete(r.Archives, id); return nil })
	s := newStore(t)
	u, err = Open(out, Password(testPassword), OpenOptions{Remember: true, Store: s})
	if err != nil {
		t.Fatalf("a remember open: %v", err)
	}
	u.Destroy()
	if got := recordedID(t, out); got != id {
		t.Fatalf("a remember open recorded %q, want %s", got, id)
	}
	stored, err := s.Get(vault.Account(a.ArchiveID()))
	assure.Reached(t, stored, "the remembered key")
	if err != nil {
		t.Fatal(err)
	}
}

// covers: MA-777, R37
func TestAForgottenArchiveIsRecordedAgainOnlyWhenConfirmed(t *testing.T) {
	freshRecord(t)
	out, _, a := made(t, nil)
	id := a.ArchiveID().String()
	change(t, func(r *encrecord.Record) error { r.ForgetID(id); return nil })
	before := loadRecord(t).Seq()

	asked := ""
	_, err := Open(out, Password(testPassword), OpenOptions{Write: true, ConfirmReRecord: func(got string) bool { asked = got; return false }})
	code, msg := refusal(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("forget", id),
		assure.NoSideEffect(func() bool { return loadRecord(t).Seq() == before && recordedID(t, out) == "" }))
	if asked != id {
		t.Fatalf("the question named %q, want %s", asked, id)
	}

	u, err := Open(out, Password(testPassword), OpenOptions{Write: true, ConfirmReRecord: func(string) bool { return true }})
	if err != nil {
		t.Fatalf("confirmed: %v", err)
	}
	u.Destroy()
	e := loadRecord(t).Archives[id]
	assure.Reached(t, e != nil && !e.Forgotten && len(e.Paths) == 1, "the id recorded again, its tombstone cleared")
}

// covers: MA-777, R37
func TestAnUnprovenPathIsProvenOrReplacedByTheArchiveOpened(t *testing.T) {
	freshRecord(t)
	out, _, a := made(t, nil)
	id := a.ArchiveID().String()
	other := strings.Repeat("ab", 16)

	// The same id, unproven (rebuilt by repair): a write open proves it.
	change(t, func(r *encrecord.Record) error { delete(r.Archives, id); return r.Add(id, out, true, now()) })
	u, err := Open(out, Password(testPassword), OpenOptions{Write: true})
	if err != nil {
		t.Fatalf("open at an unproven path of the same id: %v", err)
	}
	u.Destroy()
	m, _ := loadRecord(t).Lookup(out)
	assure.Reached(t, m.ID == id && !m.Path.Unproven, "the unproven path proven")

	// Another id, unproven: it never serves as the id the key file must name, and a
	// write open replaces it with the archive opened.
	change(t, func(r *encrecord.Record) error { delete(r.Archives, id); return r.Add(other, out, true, now()) })
	u, err = Open(out, Password(testPassword), OpenOptions{Write: true})
	if err != nil {
		t.Fatalf("open at another id's unproven path: %v", err)
	}
	u.Destroy()
	rec := loadRecord(t)
	m, _ = rec.Lookup(out)
	assure.Reached(t, m.ID == id && !m.Path.Unproven && len(rec.Archives[other].Paths) == 0, "the other id's unproven path replaced")
}

// covers: MA-777, R37
func TestWhileTheRecordCannotBeUsedAReadWarnsAndAWriteIsRefused(t *testing.T) {
	dir := freshRecord(t)
	out, _, _ := made(t, nil)
	damageRecord(t, dir)
	opens := countOpens(t)

	u, err := Open(out, Password(testPassword), OpenOptions{})
	if err != nil {
		t.Fatalf("a read must still open (the escape hatch, §4.3): %v", err)
	}
	ws := strings.Join(u.Warnings(), "; ")
	u.Destroy()
	assure.Reached(t, strings.Contains(ws, "cannot be used"), "the read open's warning about the record")

	*opens = 0
	for name, o := range map[string]OpenOptions{"write": {Write: true}, "remember": {Remember: true, Store: newStore(t)}} {
		_, err := Open(out, Password(testPassword), o)
		code, msg := refusal(err)
		assure.Refused(t, code, msg, assure.Code(1), assure.Names(out, "record of encrypted archives"),
			assure.NoSideEffect(func() bool { return *opens == 0 }))
		if !errors.Is(err, encmark.ErrRecordUnusable) {
			t.Fatalf("%s: want ErrRecordUnusable, got %v", name, err)
		}
	}
}

// covers: MA-777, R37
func TestAnUnrecordedKeyFileAskingForMoreWorkIsConfirmedFirst(t *testing.T) {
	freshRecord(t)
	out, _, a := made(t, nil)
	change(t, func(r *encrecord.Record) error { delete(r.Archives, a.ArchiveID().String()); return nil })
	opens := countOpens(t)

	// Positive twin: MailArchive's own cost is not asked about.
	u, err := Open(out, Password(testPassword), OpenOptions{ConfirmCost: func(vault.Params) bool { t.Fatal("asked about the floor cost"); return false }})
	if err != nil {
		t.Fatalf("positive twin: %v", err)
	}
	u.Destroy()

	raisePasswordCost(t, out)
	*opens = 0
	_, err = Open(out, Password(testPassword), OpenOptions{})
	code, msg := refusal(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names(out, "memory", "has not recorded"),
		assure.NoSideEffect(func() bool { return *opens == 0 }))
	if !errors.Is(err, ErrCostRefused) {
		t.Fatalf("want ErrCostRefused, got %v", err)
	}
	var asked vault.Params
	_, err = Open(out, Password(testPassword), OpenOptions{ConfirmCost: func(p vault.Params) bool { asked = p; return true }})
	assure.Reached(t, *opens, "a derivation once confirmed")
	if asked.MemoryKiB != 2*vault.CostFloor().MemoryKiB {
		t.Fatalf("the question named %s", asked)
	}
	if err == nil {
		t.Fatal("the altered key file opened")
	}
}

// covers: MA-777, R37
func TestARememberedKeyMissingFromTheRecordIsNotedUntilRecordedAgain(t *testing.T) {
	freshRecord(t)
	s := newStore(t)
	out, _, a := made(t, s)
	id := a.ArchiveID().String()
	change(t, func(r *encrecord.Record) error { delete(r.Archives, id); return nil })

	u, err := Open(out, Password(testPassword), OpenOptions{Store: s})
	if err != nil {
		t.Fatal(err)
	}
	ws := strings.Join(u.Warnings(), "; ")
	u.Destroy()
	lost, err := encrecord.Lost()
	if err != nil {
		t.Fatal(err)
	}
	assure.Reached(t, len(lost) == 1 && lost[0] == id && strings.Contains(ws, "does not list it"), "the lost record noted and told")

	u, err = Remembered(out, OpenOptions{Store: s, Write: true})
	if err != nil {
		t.Fatalf("a write open by the remembered key: %v", err)
	}
	u.Destroy()
	lost, _ = encrecord.Lost()
	if len(lost) != 0 || recordedID(t, out) != id {
		t.Fatalf("recording the archive again must clear the note: lost %v, recorded %q", lost, recordedID(t, out))
	}
	assure.Reached(t, !exists(filepath.Join(filepath.Dir(encrecordPath(t)), encrecord.LostName)), "the emptied lost list removed")
}

// encrecordPath is the record's primary file.
func encrecordPath(t *testing.T) string {
	t.Helper()
	dir, err := encrecord.Dir()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, encrecord.FileName)
}

// covers: MA-777, R37
func TestARememberedKeyOpensByTheRecordedIDOrTheProvenClaim(t *testing.T) {
	freshRecord(t)
	s := newStore(t)
	out, _, a := made(t, s)

	u, err := Remembered(out, OpenOptions{Store: s})
	if err != nil {
		t.Fatalf("recorded: %v", err)
	}
	assure.Reached(t, u.How() == ByRememberedKey && u.ArchiveID() == a.ArchiveID(), "opened by the remembered key")
	u.Destroy()

	change(t, func(r *encrecord.Record) error { delete(r.Archives, a.ArchiveID().String()); return nil })
	u, err = Remembered(out, OpenOptions{Store: s})
	if err != nil {
		t.Fatalf("unrecorded, by the folder's claimed id: %v", err)
	}
	u.Destroy()

	_, err = Remembered(out, OpenOptions{Store: newStore(t)})
	code, msg := refusal(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names(out, "no key is stored"))
	if !errors.Is(err, vault.ErrKeyNotRetrievable) {
		t.Fatalf("want ErrKeyNotRetrievable, got %v", err)
	}
}

// covers: MA-777, R37
func TestAnOpenThatMayHealRewritesTheDamagedCopy(t *testing.T) {
	freshRecord(t)
	out, _, _ := made(t, nil)
	mirror := filepath.Join(out, vault.BackupName)
	if err := os.Remove(mirror); err != nil {
		t.Fatal(err)
	}

	// Positive twin: an open that may not heal leaves it, and says it is owed.
	u, err := Open(out, Password(testPassword), OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !u.NeedsHeal() || exists(mirror) {
		t.Fatal("an open that may not heal must leave the copy missing and report NeedsHeal")
	}
	u.Destroy()

	u, err = Open(out, Password(testPassword), OpenOptions{Write: true, MayHeal: true})
	if err != nil {
		t.Fatal(err)
	}
	defer u.Destroy()
	assure.Reached(t, exists(mirror) && !u.NeedsHeal(), "the mirror rewritten by the heal")
}

// covers: MA-777, R37
func TestAMissingKeyFileAtARecordedFolderIsNeverNotEncrypted(t *testing.T) {
	freshRecord(t)
	out, _, _ := made(t, nil)
	for _, name := range []string{vault.FileName, vault.BackupName} {
		if err := os.Remove(filepath.Join(out, name)); err != nil {
			t.Fatal(err)
		}
	}
	_, err := Open(out, Password(testPassword), OpenOptions{})
	code, msg := refusal(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names(out, "encrypted archive this computer made"), assure.Forbid("no vault found"))
	if !errors.Is(err, vault.ErrVaultMissing) || errors.Is(err, vault.ErrNoVault) {
		t.Fatalf("a recorded folder without its key file is ErrVaultMissing, never ErrNoVault: %v", err)
	}
}

// covers: MA-777, R37
func TestAFolderInsideARecordedArchiveIsNotOpenedAsOne(t *testing.T) {
	freshRecord(t)
	out, _, _ := made(t, nil)
	inner := filepath.Join(out, "units")
	if err := os.MkdirAll(inner, 0o700); err != nil {
		t.Fatal(err)
	}
	opens := countOpens(t)
	_, err := Open(inner, Password(testPassword), OpenOptions{Write: true})
	code, msg := refusal(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names(inner, "inside the encrypted archive"),
		assure.NoSideEffect(func() bool { return *opens == 0 && recordedID(t, inner) == recordedID(t, out) }))
}
