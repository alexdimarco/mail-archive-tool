package keyring

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/vault"
)

// keyShapedPassword is a word passphrase that also reads as a recovery key: 27
// letters, with a check character that happens to match (the P3-1a pass, SEC-03).
const keyShapedPassword = "mango umbrella umbrella hammer"

// covers: MA-790, R37
// A new password that reads as a recovery key is refused before any key is made —
// at create and at a password change — since a prompt would take it for one.
func TestAPasswordThatReadsAsARecoveryKeyIsRefused(t *testing.T) {
	if _, err := vault.ParseRecoveryKey(keyShapedPassword); err != nil {
		t.Fatalf("the fixture no longer reads as a recovery key: %v", err)
	}
	freshRecord(t)
	creates := countCreates(t)
	out := filepath.Join(t.TempDir(), "archive")
	_, err := Create(out, keyShapedPassword, CreateOptions{})
	code, msg := refusal(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("reads as a recovery key"),
		assure.NoSideEffect(func() bool { return *creates == 0 }))

	_, _, u := made(t, nil)
	w, err := Open(u.Out(), Password(testPassword), OpenOptions{Write: true})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Destroy()
	if err := ChangePassword(w, keyShapedPassword); !errors.Is(err, ErrPasswordLooksLikeKey) {
		t.Fatalf("ChangePassword to a key-shaped password: %v", err)
	}
}

// covers: MA-790, MA-777, R37
// K14 judges a recovery key by the recovery entry's cost: raised above the floor
// at an unrecorded folder, it is asked about (or refused) before any derivation
// (the P3-1a pass, FID-03).
func TestTheCostQuestionJudgesTheRecoveryEntryForARecoveryKey(t *testing.T) {
	freshRecord(t)
	out, rk, a := made(t, nil)
	change(t, func(r *encrecord.Record) error { delete(r.Archives, a.ArchiveID().String()); return nil })
	raiseCost(t, out, "recovery")
	opens := countOpens(t)
	_, err := Open(out, RecoveryKey(rk), OpenOptions{})
	code, msg := refusal(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("has not recorded"),
		assure.NoSideEffect(func() bool { return *opens == 0 }))
	// The password entry is unchanged: a password open is not asked.
	u, err := Open(out, Password(testPassword), OpenOptions{ConfirmCost: func(vault.Params) (bool, error) { t.Fatal("asked for the password entry"); return false, nil }})
	if err == nil {
		u.Destroy()
	}
	// An interrupted question ends the open with the interruption.
	stop := errors.New("interrupted")
	if _, err := Open(out, RecoveryKey(rk), OpenOptions{ConfirmCost: func(vault.Params) (bool, error) { return false, stop }}); !errors.Is(err, stop) {
		t.Fatalf("an interrupted cost question: %v", err)
	}
}

// raiseCost rewrites both key files' kind entry to ask for twice the floor memory.
func raiseCost(t *testing.T, out, kind string) {
	t.Helper()
	for _, name := range []string{vault.FileName, vault.BackupName} {
		path := filepath.Join(out, name)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		for _, e := range m["entries"].([]any) {
			if e := e.(map[string]any); e["kind"] == kind {
				e["memory_kib"] = float64(vault.CostFloor().MemoryKiB * 2)
			}
		}
		b, _ := json.Marshal(m)
		if err := os.WriteFile(path, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// covers: MA-790, MA-777, R37
// A write by the remembered key is refused, as by a password: while the record
// cannot be used (no derivation), and at a recorded folder that holds other files
// or is now a link — in the case's own words, not as an altered key file (the P3-1a
// pass, FID-03, FID-12).
func TestARememberedKeyWriteIsRefusedWhereAPasswordOneIs(t *testing.T) {
	dir := freshRecord(t)
	s := newStore(t)
	out, _, _ := made(t, s)

	// Positive twin.
	u, err := Remembered(out, OpenOptions{Store: s, Write: true})
	if err != nil {
		t.Fatalf("positive twin: %v", err)
	}
	u.Destroy()

	if err := os.WriteFile(filepath.Join(out, "index.html"), []byte("planted"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, open := range map[string]func() (*Unlocked, error){
		"remembered": func() (*Unlocked, error) { return Remembered(out, OpenOptions{Store: s, Write: true}) },
		"password":   func() (*Unlocked, error) { return Open(out, Password(testPassword), OpenOptions{Write: true}) },
	} {
		_, err := open()
		code, msg := refusal(err)
		assure.Refused(t, code, msg, assure.Code(1), assure.Names(out, "now holds other files"), assure.Forbid("key file was altered"))
		if !errors.Is(err, ErrNotTheArchive) {
			t.Fatalf("%s: want ErrNotTheArchive, got %v", name, err)
		}
	}
	os.Remove(filepath.Join(out, "index.html"))

	// Now a link to a copy elsewhere: case L.
	moved := filepath.Join(t.TempDir(), "moved")
	if err := os.Rename(out, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(moved, out); err != nil {
		t.Fatal(err)
	}
	_, err = Remembered(out, OpenOptions{Store: s, Write: true})
	code, msg := refusal(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names(out, "now a link"))
	os.Remove(out)
	os.Rename(moved, out)

	damageRecord(t, dir)
	opens := countOpens(t)
	_, err = Remembered(out, OpenOptions{Store: s, Write: true})
	code, msg = refusal(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("record of encrypted archives"),
		assure.NoSideEffect(func() bool { return *opens == 0 }))
	if !errors.Is(err, encmark.ErrRecordUnusable) {
		t.Fatalf("want ErrRecordUnusable, got %v", err)
	}
}

// copyArchive copies an archive's key files to a new folder: a second copy of the
// same archive.
func copyArchive(t *testing.T, out string) string {
	t.Helper()
	dst := filepath.Join(t.TempDir(), "copy")
	if err := os.MkdirAll(dst, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{vault.FileName, vault.BackupName} {
		b, err := os.ReadFile(filepath.Join(out, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dst, name), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dst
}

// covers: MA-790, MA-777, R37
// An archive forgotten as a whole while a write open of a second copy is under
// way is not recorded again without the question (the P3-1a pass, CON-04).
func TestAForgetDuringAnOpenIsNotOverriddenSilently(t *testing.T) {
	freshRecord(t)
	out, _, a := made(t, nil)
	second := copyArchive(t, out)
	id := a.ArchiveID().String()
	orig := openVault
	openVault = func(o string, want vault.ID, s vault.Secret, limit *vault.Params) (*vault.Opened, error) {
		change(t, func(r *encrecord.Record) error { r.ForgetID(id); return nil })
		return orig(o, want, s, limit)
	}
	defer func() { openVault = orig }()
	asked := 0
	_, err := Open(second, Password(testPassword), OpenOptions{Write: true, ConfirmReRecord: func(string) (bool, error) { asked++; return true, nil }})
	code, msg := refusal(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("forget"),
		assure.NoSideEffect(func() bool { e := loadRecord(t).Archives[id]; return e != nil && e.Forgotten && len(e.Paths) == 0 }))
	if asked != 0 {
		t.Fatalf("asked %d times about a forget made after the record was read", asked)
	}
}

// covers: MA-790, MA-779, R37
// Commit judges the folder again: files dropped in, a link put in its place, and a
// folder recorded for another archive meanwhile are each refused, with nothing
// written there (the P3-1a pass, OUT-03, FID-03).
func TestCommitJudgesTheFolderAgain(t *testing.T) {
	freshRecord(t)
	cases := map[string]func(t *testing.T, out string){
		"files dropped in": func(t *testing.T, out string) {
			os.WriteFile(filepath.Join(out, "planted.eml"), []byte("x"), 0o600)
		},
		"a link put in its place": func(t *testing.T, out string) {
			elsewhere := filepath.Join(t.TempDir(), "elsewhere")
			os.MkdirAll(elsewhere, 0o700)
			os.Remove(out)
			os.Symlink(elsewhere, out)
		},
		"recorded for another archive": func(t *testing.T, out string) {
			change(t, func(r *encrecord.Record) error { return r.Add(strings.Repeat("ef", 16), out, false, now()) })
		},
	}
	for name, meddle := range cases {
		t.Run(name, func(t *testing.T) {
			out := filepath.Join(t.TempDir(), "archive")
			p, err := Create(out, testPassword, CreateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer p.Discard()
			meddle(t, out)
			_, err = p.Commit()
			code, msg := refusal(err)
			assure.Refused(t, code, msg, assure.Code(1), assure.Names(out),
				assure.NoSideEffect(func() bool { return !exists(filepath.Join(out, vault.FileName)) }))
		})
	}
}

// covers: MA-790, MA-784, R37
// A refused forget forgets no key: the key is removed only after the record is
// re-checked under its lock (the P3-1a pass, CON-05, FID-01). A forget while a
// backup holds the folder's lock is refused, and one whose folder's case changed
// since the plan too (FID-02).
func TestARefusedForgetForgetsNothing(t *testing.T) {
	freshRecord(t)
	s := newStore(t)
	out, _, a := made(t, s)
	p, err := PlanForget(out, s)
	if err != nil {
		t.Fatal(err)
	}
	change(t, func(r *encrecord.Record) error {
		return r.Add(a.ArchiveID().String(), filepath.Join(t.TempDir(), "x"), false, now())
	})
	err = Forget(p, ForgetOptions{Whole: true, Keys: true, Store: s})
	code, msg := refusal(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("changed"),
		assure.NoSideEffect(func() bool { _, gerr := s.Get(vault.Account(a.ArchiveID())); return gerr == nil }))

	freshRecord(t)
	out, _, _ = made(t, nil)
	p, err = PlanForget(out, nil)
	if err != nil {
		t.Fatal(err)
	}
	held, err := lockfile.AcquireAs(filepath.Join(out, lockfile.Name), "export")
	if err != nil {
		t.Fatal(err)
	}
	err = Forget(p, ForgetOptions{})
	held.Release()
	code, msg = refusal(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names(out, "in use", "try again"),
		assure.NoSideEffect(func() bool { return recordedID(t, out) != "" }))

	os.WriteFile(filepath.Join(out, "index.html"), []byte("planted"), 0o600)
	if err := Forget(p, ForgetOptions{}); !errors.Is(err, ErrForgetChanged) {
		t.Fatalf("a forget whose folder's case changed since the plan: %v", err)
	}
}

// covers: MA-790, MA-784, R37
// A removed archive typed through a linked parent folder is still its own folder:
// it is forgotten, not "inside" itself (the P3-1a pass, CRIT-06).
func TestARemovedArchiveTypedThroughALinkedParentIsForgotten(t *testing.T) {
	freshRecord(t)
	base := t.TempDir()
	real := filepath.Join(base, "real")
	os.MkdirAll(real, 0o700)
	via := filepath.Join(base, "via")
	if err := os.Symlink(real, via); err != nil {
		t.Skipf("no symlinks: %v", err)
	}
	p, err := Create(filepath.Join(real, "archive"), testPassword, CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	u, err := p.Commit()
	if err != nil {
		t.Fatal(err)
	}
	u.Destroy()
	if err := os.RemoveAll(filepath.Join(real, "archive")); err != nil {
		t.Fatal(err)
	}
	typed := filepath.Join(via, "archive")
	plan, err := PlanForget(typed, nil)
	assure.Reached(t, plan.ID, "the plan for the removed archive")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(typed, Password(testPassword), OpenOptions{}); err == nil || strings.Contains(err.Error(), "is inside the encrypted archive") {
		t.Fatalf("opening the removed archive's own folder: %v", err)
	}
}

// covers: MA-790, MA-777, R37
// A recorded folder replaced by a link to another recorded archive is itself, in
// case L — never the other archive, whatever the ids' order (the P3-1a pass,
// OUT-01).
func TestARecordedFolderLinkedToAnotherArchiveIsCaseL(t *testing.T) {
	freshRecord(t)
	s := newStore(t)
	for i := 0; i < 3; i++ { // ids are random: several pairs cover both orders
		victim, _, _ := made(t, s)
		target, _, _ := made(t, s)
		if err := os.RemoveAll(victim); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, victim); err != nil {
			t.Skipf("no symlinks: %v", err)
		}
		res, _ := encmark.At(victim)
		if res.Case != encmark.Link {
			t.Fatalf("a recorded folder now a link to another archive reads case %v (%s)", res.Case, res.Why)
		}
		if _, err := Remembered(victim, OpenOptions{Store: s, Write: true}); !errors.Is(err, ErrNotTheArchive) {
			t.Fatalf("a write through the planted link: %v", err)
		}
		p, err := PlanForget(victim, s)
		if err != nil || !p.Strong || p.Spelling == encrecordSpelling(target) {
			t.Fatalf("the forget plan for the linked folder: %+v %v", p, err)
		}
	}
	assure.Reached(t, true, "three pairs")
}

// covers: MA-790, MA-777
// A forked key file opens with the fork's warning and no "damaged" one: a fork is
// not healed (the P3-1a pass, CRIT-07).
func TestAForkIsNotCalledDamage(t *testing.T) {
	freshRecord(t)
	out, _, _ := made(t, nil)
	second := copyArchive(t, out)
	for i, dir := range []string{out, second} {
		u, err := Open(dir, Password(testPassword), OpenOptions{Write: true})
		if err != nil {
			t.Fatal(err)
		}
		if err := ChangePassword(u, fmt.Sprintf("password number %d here", i)); err != nil {
			t.Fatal(err)
		}
		u.Destroy()
	}
	b, err := os.ReadFile(filepath.Join(second, vault.FileName))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, vault.BackupName), b, 0o600); err != nil {
		t.Fatal(err)
	}
	u, err := Open(out, Password("password number 0 here"), OpenOptions{Write: true, MayHeal: true})
	if err != nil {
		t.Fatal(err)
	}
	defer u.Destroy()
	ws := strings.Join(u.Warnings(), "\n")
	assure.Reached(t, u.Forked() && strings.Contains(ws, "two computers"), "the fork's warning")
	if strings.Contains(ws, "damaged") {
		t.Fatalf("a fork was also called damaged: %s", ws)
	}
}

// covers: MA-790, MA-782
// The file -recovery-key-out writes reads back through -recovery-key-file (the
// P3-1a pass, SEC-02); a file holding something else is refused without quoting it
// (SEC-07); a UTF-8 byte-order mark is dropped and a UTF-16 file refused (CRIT-09).
func TestSecretFilesReadWhatMailArchiveAndEditorsWrite(t *testing.T) {
	freshRecord(t)
	out, rk, a := made(t, nil)
	dir := t.TempDir()
	write := func(name string, b []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, b, 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	s, err := RecoveryKeyFile(write("rk", []byte(RecoveryKeyFileText(a.ArchiveID(), rk, out, time.Now()))))
	if err != nil {
		t.Fatalf("MailArchive's own recovery-key file: %v", err)
	}
	u, err := Open(out, s, OpenOptions{})
	assure.Reached(t, u, "opened by the round-tripped recovery key")
	if err != nil {
		t.Fatal(err)
	}
	u.Destroy()

	_, err = RecoveryKeyFile(write("pw", []byte("hunter2 is+my secret\n")))
	code, msg := refusal(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("not a recovery key"), assure.Forbid("hunter2", "+", "characters"))

	s, err = PasswordFile(write("bom", append([]byte{0xEF, 0xBB, 0xBF}, []byte(testPassword+"\r\n")...)))
	if err != nil {
		t.Fatal(err)
	}
	if u, err := Open(out, s, OpenOptions{}); err != nil {
		t.Fatalf("a UTF-8 password file with a byte-order mark: %v", err)
	} else {
		u.Destroy()
	}
	utf16 := []byte{0xFF, 0xFE}
	for _, r := range testPassword {
		utf16 = append(utf16, byte(r), 0)
	}
	_, err = PasswordFile(write("u16", utf16))
	code, msg = refusal(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("UTF-8"))
}

// covers: MA-790, MA-783
// A locked archive changes no key, and forget-key works on a folder this computer
// never recorded, by the id its key files claim (the P3-1a pass, FID-13).
func TestKeyChangesOnALockedOrUnrecordedArchive(t *testing.T) {
	freshRecord(t)
	s := newStore(t)
	out, _, a := made(t, s)
	u, err := Open(out, Password(testPassword), OpenOptions{Write: true})
	if err != nil {
		t.Fatal(err)
	}
	u.Lock()
	if err := ChangePassword(u, "a brand new passphrase"); !errors.Is(err, ErrLocked) {
		t.Fatalf("ChangePassword on a locked archive: %v", err)
	}
	if _, err := NewRecoveryKey(u); !errors.Is(err, ErrLocked) {
		t.Fatalf("NewRecoveryKey on a locked archive: %v", err)
	}
	change(t, func(r *encrecord.Record) error { delete(r.Archives, a.ArchiveID().String()); return nil })
	id, err := ForgetKey(out, s)
	assure.Reached(t, id == a.ArchiveID(), "the claimed id's key forgotten")
	if err != nil {
		t.Fatal(err)
	}
	if HasRemembered(out, s) {
		t.Fatal("the key is still remembered")
	}
}
