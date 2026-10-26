package keyring

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/util"
	"mail-archive-tool/internal/vault"
)

const newPassword = "a brand new passphrase"

// covers: MA-783, R37
func TestThePasswordIsChangedByAnyOfTheArchivesSecrets(t *testing.T) {
	freshRecord(t)
	s := newStore(t)
	out, rk, _ := made(t, s)
	openers := map[string]func() (*Unlocked, error){
		"the password":       func() (*Unlocked, error) { return Open(out, Password(testPassword), OpenOptions{Write: true}) },
		"the recovery key":   func() (*Unlocked, error) { return Open(out, RecoveryKey(rk), OpenOptions{Write: true}) },
		"the remembered key": func() (*Unlocked, error) { return Remembered(out, OpenOptions{Write: true, Store: s}) },
	}
	current := testPassword
	for _, name := range []string{"the password", "the recovery key", "the remembered key"} {
		u, err := openers[name]()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		next := newPassword + " " + strings.Fields(name)[1]
		if err := ChangePassword(u, next); err != nil {
			t.Fatalf("changed by %s: %v", name, err)
		}
		u.Destroy()
		if _, err := Open(out, Password(current), OpenOptions{}); !errors.Is(err, vault.ErrWrongSecret) {
			t.Fatalf("after a change by %s the old password still opens: %v", name, err)
		}
		v, err := Open(out, Password(next), OpenOptions{})
		if err != nil {
			t.Fatalf("after a change by %s the new password does not open: %v", name, err)
		}
		v.Destroy()
		current = next
		// The password used to open before the next change.
		openers["the password"] = func() (*Unlocked, error) { return Open(out, Password(current), OpenOptions{Write: true}) }
	}
	assure.Reached(t, current, "the last password set")

	r, err := Open(out, Password(current), OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer r.Destroy()
	err = ChangePassword(r, "another passphrase!")
	code, msg := refusal(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("opened to read"))
	if _, err := Open(out, Password(current), OpenOptions{}); err != nil {
		t.Fatalf("a refused change changed the password: %v", err)
	}
}

// covers: MA-783, R37
func TestANewRecoveryKeyReplacesTheOldOnlyAtCommit(t *testing.T) {
	freshRecord(t)
	out, oldRK, _ := made(t, nil)
	u, err := Open(out, Password(testPassword), OpenOptions{Write: true})
	if err != nil {
		t.Fatal(err)
	}
	defer u.Destroy()
	p, err := NewRecoveryKey(u)
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%v %+v %#v %s", p, p, p, p); strings.Contains(got, p.RecoveryKey()) || !strings.Contains(got, "redacted") {
		t.Fatalf("a pending recovery key must print redacted: %s", got)
	}
	// Before Commit the old key still opens and the new one does not.
	if v, err := Open(out, RecoveryKey(oldRK), OpenOptions{}); err != nil {
		t.Fatalf("before Commit the old recovery key must still open: %v", err)
	} else {
		v.Destroy()
	}
	if _, err := Open(out, RecoveryKey(p.RecoveryKey()), OpenOptions{}); err == nil {
		t.Fatal("the new recovery key opened before Commit")
	}
	if err := p.Commit(); err != nil {
		t.Fatal(err)
	}
	v, err := Open(out, RecoveryKey(p.RecoveryKey()), OpenOptions{})
	assure.Reached(t, v, "the archive opened by the new recovery key")
	if err != nil {
		t.Fatal(err)
	}
	v.Destroy()
	if _, err := Open(out, RecoveryKey(oldRK), OpenOptions{}); !errors.Is(err, vault.ErrWrongSecret) {
		t.Fatalf("after Commit the old recovery key still opens: %v", err)
	}
}

// covers: MA-783
func TestTheRecoveryKeyEnteredAgainIsCheckedByItsValue(t *testing.T) {
	freshRecord(t)
	_, rk, _ := made(t, nil)
	for _, typed := range []string{rk, strings.ToLower(rk), strings.ReplaceAll(rk, "-", " "), "  " + rk + "  "} {
		if err := CheckRecoveryKey(rk, typed); err != nil {
			t.Fatalf("%q must match %q: %v", typed, rk, err)
		}
	}
	freshRecord(t)
	_, other, _ := made(t, nil)
	err := CheckRecoveryKey(rk, other)
	code, msg := refusal(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("not the recovery key shown"))
	typo := []byte(rk)
	typo[0] ^= 1
	if err := CheckRecoveryKey(rk, string(typo)); err == nil {
		t.Fatal("a typing mistake matched")
	}
	// A password pasted there is refused without quoting any of it: not a character,
	// its place or its length (the P3-1a pass, SEC-07).
	err = CheckRecoveryKey(rk, "hunter2 is+my secret")
	code, msg = refusal(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("not a recovery key"), assure.Forbid("+", "character", "hunter2"))
}

// covers: MA-784, R37
func TestForgettingAFolderRemovesOnlyItsEntryAndAsksForTheWholeAtTheLast(t *testing.T) {
	freshRecord(t)
	s := newStore(t)
	out, _, a := made(t, s)
	id := a.ArchiveID().String()
	second := filepath.Join(t.TempDir(), "second-drive")
	change(t, func(r *encrecord.Record) error { return r.Add(id, second, false, now()) })

	p, err := PlanForget(out, s)
	if err != nil {
		t.Fatal(err)
	}
	if p.Last || len(p.Others) != 1 || p.Others[0].Spelling != encrecord.Spelling(util.ResolveExisting(second)) || !p.Remembered || p.Strong {
		t.Fatalf("the plan for one of two paths: %+v", p)
	}
	if err := Forget(p, ForgetOptions{Whole: true}); err == nil {
		t.Fatal("the whole archive was forgotten from a path that is not its last")
	}
	if err := Forget(p, ForgetOptions{}); err != nil {
		t.Fatal(err)
	}
	if recordedID(t, out) != "" || recordedID(t, second) != id {
		t.Fatalf("forgetting one path: out %q, second %q", recordedID(t, out), recordedID(t, second))
	}

	p, err = PlanForget(second, s)
	if err != nil {
		t.Fatal(err)
	}
	assure.Reached(t, p.Last, "the last path's plan")
	if err := Forget(p, ForgetOptions{Whole: true, Keys: true, Store: s}); err != nil {
		t.Fatal(err)
	}
	e := loadRecord(t).Archives[id]
	if e == nil || !e.Forgotten || len(e.Paths) != 0 {
		t.Fatalf("the whole id forgotten: %+v", e)
	}
	if _, err := s.Get(vault.Account(a.ArchiveID())); err == nil {
		t.Fatal("the remembered key was kept")
	}
}

// covers: MA-784, R37
func TestAForgetIsRefusedWhenTheRecordChangedAfterTheConfirmation(t *testing.T) {
	freshRecord(t)
	out, _, a := made(t, nil)
	p, err := PlanForget(out, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Another path is recorded for the id while the person answers.
	change(t, func(r *encrecord.Record) error {
		return r.Add(a.ArchiveID().String(), filepath.Join(t.TempDir(), "x"), false, now())
	})
	before := loadRecord(t).Seq()
	err = Forget(p, ForgetOptions{})
	code, msg := refusal(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("changed", "forget-archive again"),
		assure.NoSideEffect(func() bool { return loadRecord(t).Seq() == before && recordedID(t, out) != "" }))
}

// covers: MA-784, R37
func TestAFolderThatMayHaveBeenTamperedWithAsksTheStrongConfirmation(t *testing.T) {
	freshRecord(t)
	out, _, _ := made(t, nil)
	if err := os.WriteFile(filepath.Join(out, "index.html"), []byte("planted"), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := PlanForget(out, nil)
	if err != nil {
		t.Fatal(err)
	}
	assure.Reached(t, p.Strong && p.Case == encmark.Other, "case 2's strong confirmation")

	// An unproven path still holding its key files (open it to prove it), and an
	// unrecorded folder, are refused; an unproven path whose key files are gone
	// can be forgotten, with the strong confirmation (the P3-1a pass, CRIT-02).
	freshRecord(t)
	change(t, func(r *encrecord.Record) error { return r.Add(strings.Repeat("ab", 16), out, true, now()) })
	for name, path := range map[string]string{"unproven with its key files": out, "unrecorded": t.TempDir()} {
		_, err := PlanForget(path, nil)
		code, msg := refusal(err)
		assure.Refused(t, code, msg, assure.Code(1), assure.Names(path))
		_ = name
	}
	gone := filepath.Join(t.TempDir(), "rebuilt")
	change(t, func(r *encrecord.Record) error { return r.Add(strings.Repeat("cd", 16), gone, true, now()) })
	pg, err := PlanForget(gone, nil)
	if err != nil || !pg.Strong {
		t.Fatalf("an unproven path whose archive is gone: %+v %v", pg, err)
	}
	if res, _ := encmark.At(gone); !strings.Contains(res.Why, "forget-archive") {
		t.Fatalf("an unproven path whose archive is gone is told how to forget it: %s", res.Why)
	}
	if err := Forget(pg, ForgetOptions{Whole: pg.Last}); err != nil || recordedID(t, gone) != "" {
		t.Fatalf("forgetting it: %v, still recorded %q", err, recordedID(t, gone))
	}
	dir := freshRecord(t)
	damageRecord(t, dir)
	_, err = PlanForget(out, nil)
	if !errors.Is(err, encmark.ErrRecordUnusable) {
		t.Fatalf("an unusable record: %v", err)
	}
}

// covers: MA-783
func TestForgetKeyRemovesTheRememberedKeyOnly(t *testing.T) {
	freshRecord(t)
	s := newStore(t)
	out, _, a := made(t, s)
	id, err := ForgetKey(out, s)
	if err != nil {
		t.Fatal(err)
	}
	if id != a.ArchiveID() {
		t.Fatalf("forgot %s, want %s", id, a.ArchiveID())
	}
	if _, err := s.Get(vault.Account(id)); err == nil {
		t.Fatal("the key is still remembered")
	}
	assure.Reached(t, recordedID(t, out), "the folder still recorded")
	if _, err := Remembered(out, OpenOptions{Store: s}); !errors.Is(err, vault.ErrKeyNotRetrievable) {
		t.Fatalf("a forgotten key still opened the archive: %v", err)
	}
}
