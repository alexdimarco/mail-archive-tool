package keyring

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/seal"
	"mail-archive-tool/internal/vault"
)

// The Desktop's write groundwork (design-encrypted-archives-phase3 §3.2, §3.3,
// §3.6, §6 E6; the P3-3 plan, sub-slice 3a).

// readOpen opens out to read, as the Desktop's unlock does.
func readOpen(t *testing.T, out string) *Unlocked {
	t.Helper()
	u, err := Open(out, Password(testPassword), OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(u.Destroy)
	return u
}

// covers: MA-800, R37
// A read open cannot back up; a write lease from it can, and records a folder that
// was not recorded (adoption). An archive the person said to forget is not
// recorded again without the question answered yes — Forgotten says so before any
// key is derived — and a record that cannot be used refuses the lease, nothing
// recorded either way.
func TestALeaseForAWriteRecordsTheFolder(t *testing.T) {
	dir := freshRecord(t)
	out, _, w := made(t, nil)
	id := w.ArchiveID().String()
	w.Destroy()
	change(t, func(r *encrecord.Record) error {
		if m, ok := r.Lookup(out); !ok || !r.ForgetPath(id, m.Path.Spelling) {
			t.Fatal("the folder was not unrecorded")
		}
		return nil
	})
	u := readOpen(t, out)
	if _, err := Apply(&app.Options{Out: out}, u); err == nil {
		t.Fatal("a read open must not back up")
	}

	l, err := u.LeaseForWrite(OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	done, err := Apply(&app.Options{Out: out}, l)
	if err != nil || !l.Adopted() || recordedID(t, out) != id {
		t.Fatalf("a write lease records the folder and may back up: %v adopted=%v recorded=%q", err, l.Adopted(), recordedID(t, out))
	}
	done()
	l.Destroy()

	change(t, func(r *encrecord.Record) error { r.ForgetID(id); return nil })
	if f, err := Forgotten(out); err != nil || !f {
		t.Fatalf("Forgotten after a forget: %v %v", f, err)
	}
	before := loadRecord(t).Seq()
	_, err = u.LeaseForWrite(OpenOptions{})
	code, msg := refusal(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("forget this encrypted archive"),
		assure.NoSideEffect(func() bool { return loadRecord(t).Seq() == before }))
	if !errors.Is(err, ErrReRecordRefused) {
		t.Fatalf("want ErrReRecordRefused, got %v", err)
	}
	l, err = u.LeaseForWrite(OpenOptions{ConfirmReRecord: func(string) (bool, error) { return true, nil }})
	if err != nil || recordedID(t, out) != id {
		t.Fatalf("recorded again when the person says yes: %v", err)
	}
	l.Destroy()
	if f, _ := Forgotten(out); f {
		t.Fatal("still forgotten after it was recorded again")
	}

	damageRecord(t, dir)
	_, err = u.LeaseForWrite(OpenOptions{})
	assure.Reached(t, errors.Is(err, encmark.ErrRecordUnusable), "a record that cannot be used refuses a write lease")
}

// covers: MA-800, R37
// A heal through a write lease — Run now heals before its run — rewrites the
// missing copy of the key file, and the Desktop's open it was leased from no
// longer says a heal is owed.
func TestAHealThroughALeaseHealsItsOpen(t *testing.T) {
	freshRecord(t)
	out, _, w := made(t, nil)
	w.Destroy()
	mirror := filepath.Join(out, vault.BackupName)
	os.Remove(mirror)
	u := readOpen(t, out)
	l, err := u.LeaseForWrite(OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Destroy()
	if !u.NeedsHeal() || !l.NeedsHeal() {
		t.Fatal("positive twin: the missing copy is owed a heal")
	}
	if err := Heal(l); err != nil {
		t.Fatal(err)
	}
	a, _ := os.ReadFile(filepath.Join(out, vault.FileName))
	b, _ := os.ReadFile(mirror)
	assure.Reached(t, !u.NeedsHeal() && !l.NeedsHeal() && len(a) > 0 && string(a) == string(b), "both copies the same bytes, the open healed too")
}

// covers: MA-801, R37
// The keyed pause note (E6): sealed into the record by a write lease, read back
// with the key, never written anywhere in the archive in plaintext, cleared by "";
// a read open cannot set it, and a paused archive is not written.
func TestThePauseNoteIsSealedInTheRecord(t *testing.T) {
	out, u := fullFixture(t)
	const note = "paused-note-marker-7d1c while the drive is away"
	if err := SetPauseNote(context.Background(), u, note, quiet); err == nil {
		t.Fatal("a read open must not write the note")
	}
	l, err := u.LeaseForWrite(OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer l.Destroy()
	if err := SetPauseNote(context.Background(), l, note, quiet); err != nil {
		t.Fatal(err)
	}
	got, err := PauseNote(u)
	if err != nil || got != note {
		t.Fatalf("read back %q %v", got, err)
	}
	filepath.WalkDir(out, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if b, _ := os.ReadFile(p); strings.Contains(string(b), "paused-note-marker-7d1c") {
				t.Fatalf("the note is in plaintext in %s", p)
			}
		}
		return nil
	})
	if err := SetPauseNote(context.Background(), l, "", quiet); err != nil {
		t.Fatal(err)
	}
	if got, _ := PauseNote(u); got != "" {
		t.Fatalf("cleared note reads %q", got)
	}
	if err := seal.Seal(out, ""); err != nil {
		t.Fatal(err)
	}
	err = SetPauseNote(context.Background(), l, note, quiet)
	got, _ = PauseNote(u)
	assure.Reached(t, errors.Is(err, seal.ErrSealed) && got == "", "a paused archive is not written")
}

// covers: MA-802, R37
// A key-file operation's lock names no verb: the lock file stays beside the
// archive in plaintext, so an encrypted archive's says only host= (A18) — a heal,
// a password change and a new recovery key alike.
func TestAKeyFileOperationsLockNamesNoVerb(t *testing.T) {
	freshRecord(t)
	out, _, w := made(t, nil)
	defer w.Destroy()
	lock := filepath.Join(out, lockfile.Name)
	if err := ChangePassword(w, testPassword+"-2"); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(lock)
	if err != nil || !strings.Contains(string(b), "host=") {
		t.Fatalf("positive twin: the lock file was written: %v %q", err, b)
	}
	os.Remove(filepath.Join(out, vault.BackupName))
	if err := Heal(w); err != nil {
		t.Fatal(err)
	}
	b, _ = os.ReadFile(lock)
	assure.Reached(t, !strings.Contains(string(b), "verb="), "no verb in the lock file")
}
