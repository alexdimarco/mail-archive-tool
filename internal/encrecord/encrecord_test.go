package encrecord

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/instance"
)

const idA = "0123456789abcdef0123456789abcdef"
const idB = "fedcba9876543210fedcba9876543210"

// sandbox points the record at a fresh folder for this test.
func sandbox(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Cleanup(SetDirForTest(dir))
	return dir
}

func add(t *testing.T, id, path string) {
	t.Helper()
	if _, err := Update("test", func(r *Record) error { return r.Add(id, path, false, time.Now()) }); err != nil {
		t.Fatal(err)
	}
}

func code(err error) int {
	if err == nil {
		return 0
	}
	return 1
}

// covers: MA-769, R37
// The record's place and shape: beside the instance files (the settings folder, or
// the non-roaming per-computer folder on Windows), never a *.json name, owner-only;
// a header with the format and sequence, and a trailer; a record never written is
// empty; what Update writes Load reads back, primary and mirror alike, with the
// sequence rising on each write.
func TestTheRecordLivesOutsideEveryArchiveAndReadsBack(t *testing.T) {
	want, err := instance.Dir()
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := defaultDir(); got != want {
		t.Fatalf("the record's folder %s is not the instance folder %s", got, want)
	}
	for _, name := range []string{FileName, MirrorName} {
		if strings.HasSuffix(name, ".json") {
			t.Fatalf("%s is a *.json name, which uninstall deletes", name)
		}
	}
	dir := sandbox(t)
	rec, err := Load()
	if err != nil || len(rec.Archives) != 0 {
		t.Fatalf("a record never written: %+v %v", rec, err)
	}
	folder := t.TempDir()
	add(t, idA, folder)
	add(t, idB, folder+"-other")
	for _, name := range []string{FileName, MirrorName} {
		p := filepath.Join(dir, name)
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
			t.Fatalf("%s is mode %o, want 0600", name, fi.Mode().Perm())
		}
		raw, _ := os.ReadFile(p)
		if !bytes.HasPrefix(raw, []byte("MAILARCHIVE-RECORD 1 seq=2\n")) || !bytes.Contains(raw, []byte("\nsha256=")) {
			t.Fatalf("%s's shape: %q", name, raw)
		}
	}
	rec, err = Load()
	if err != nil || rec.Seq() != 2 {
		t.Fatalf("read back: seq %d, %v", rec.Seq(), err)
	}
	if m, ok := rec.Lookup(folder); !ok || m.ID != idA {
		t.Fatalf("the recorded folder is not found: %+v", m)
	}
}

// covers: MA-769, R37
// The two copies: the higher sequence wins and the other is healed at the next
// write; a copy whose trailer fails is moved aside, never written over, and when its
// readable sequence was the higher the write says a recent change may have been lost.
func TestTheHigherCopyWinsAndADamagedOneIsSetAside(t *testing.T) {
	dir := sandbox(t)
	folder := t.TempDir()
	add(t, idA, folder)
	staleMirror, _ := os.ReadFile(filepath.Join(dir, MirrorName))
	add(t, idB, folder+"-b")
	os.WriteFile(filepath.Join(dir, MirrorName), staleMirror, 0o600)
	rec, err := Load()
	if err != nil || rec.Seq() != 2 || rec.Archives[idB] == nil {
		t.Fatalf("the higher copy did not win: %+v %v", rec, err)
	}

	// The primary rots after its last write; the mirror lags one change behind.
	p := filepath.Join(dir, FileName)
	raw, _ := os.ReadFile(p)
	raw[len(raw)-5] ^= 1
	os.WriteFile(p, raw, 0o600)
	notice, err := Update("test", func(r *Record) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(notice, "may have been lost") {
		t.Fatalf("losing the newer copy's change went unsaid: %q", notice)
	}
	aside, _ := filepath.Glob(p + ".damaged-*")
	if len(aside) != 1 {
		t.Fatalf("the damaged copy was not set aside: %v", aside)
	}
	if got, _ := os.ReadFile(aside[0]); !bytes.Equal(got, raw) {
		t.Fatal("the damaged copy was changed when set aside")
	}
	if rec, err := Load(); err != nil || rec.Seq() != 3 {
		t.Fatalf("after the heal: %+v %v", rec, err)
	}
}

// covers: MA-769, R37
// A record this computer cannot use is refused by case, each naming the file and
// its remedy, and is never rewritten: a copy from a newer MailArchive (recognised
// from its header even when over the size limit), a copy other users can read,
// and both copies damaged. The positive twin is the healthy record above.
func TestARecordThatCannotBeUsedIsNeverRewritten(t *testing.T) {
	for _, tc := range []struct {
		name  string
		plant func(t *testing.T, p string)
		want  Case
		names []string
	}{
		{"newer", func(t *testing.T, p string) {
			os.WriteFile(p, append([]byte("MAILARCHIVE-RECORD 9 seq=7\n"), bytes.Repeat([]byte("x"), MaxBytes+10)...), 0o600)
		}, CaseNewer, []string{FileName, "newer MailArchive", "upgrade"}},
		{"permissions", func(t *testing.T, p string) {
			if runtime.GOOS == "windows" {
				t.Skip("the mode bits are not Windows' access control")
			}
			os.Chmod(p, 0o644)
		}, CasePermissions, []string{FileName, "chmod 600"}},
		{"damaged", func(t *testing.T, p string) {
			os.WriteFile(p, []byte("not a record\n"), 0o600)
			os.WriteFile(p+".bak", []byte("not one either\n"), 0o600)
		}, CaseDamaged, []string{"damaged", "repair"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := sandbox(t)
			add(t, idA, t.TempDir())
			p := filepath.Join(dir, FileName)
			tc.plant(t, p)
			before, _ := os.ReadFile(p)
			_, err := Load()
			var ue *UnusableError
			if !errors.As(err, &ue) || ue.Case != tc.want {
				t.Fatalf("want case %d, got %v", tc.want, err)
			}
			_, err = Update("test", func(r *Record) error { return r.Add(idB, t.TempDir(), false, time.Now()) })
			assure.Refused(t, code(err), err.Error(), assure.Code(1), assure.Names(tc.names...),
				assure.NoSideEffect(func() bool { after, _ := os.ReadFile(p); return bytes.Equal(before, after) }))
		})
	}
}

// covers: MA-769, R37
// A change that would take the record past its limit is refused, writing nothing.
func TestARecordPastItsLimitIsRefused(t *testing.T) {
	dir := sandbox(t)
	add(t, idA, t.TempDir())
	before, _ := os.ReadFile(filepath.Join(dir, FileName))
	_, err := Update("test", func(r *Record) error {
		for i := 0; i < 6000; i++ {
			r.Archives[idA].Paths = append(r.Archives[idA].Paths, PathEntry{Spelling: strings.Repeat("p", 200)})
		}
		return nil
	})
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("want ErrTooLarge, got %v", err)
	}
	if after, _ := os.ReadFile(filepath.Join(dir, FileName)); !bytes.Equal(before, after) {
		t.Fatal("a refused change wrote the record")
	}
}

// covers: MA-769, R37
// A change re-proves its lock before writing: a lock file replaced while the change
// was being made (another writer could hold a fresh one) stops it, writing nothing.
func TestARecordChangeWritesOnlyUnderItsLock(t *testing.T) {
	dir := sandbox(t)
	add(t, idA, t.TempDir())
	before, _ := os.ReadFile(filepath.Join(dir, FileName))
	_, err := Update("test", func(r *Record) error {
		os.Remove(filepath.Join(dir, LockName))
		os.WriteFile(filepath.Join(dir, LockName), []byte("pid=1 nonce=someone-else\n"), 0o600)
		return r.Add(idB, t.TempDir(), false, time.Now())
	})
	if err == nil {
		t.Fatal("a change whose lock was replaced was written")
	}
	if after, _ := os.ReadFile(filepath.Join(dir, FileName)); !bytes.Equal(before, after) {
		t.Fatal("the record was written without its lock")
	}
}

// covers: MA-769, R37
// Forgetting: one path goes and the id stays, even with no paths left; forgetting
// the whole id sets its tombstone, and recording the id again clears it.
func TestForgettingKeepsTheIdAndATombstoneClears(t *testing.T) {
	sandbox(t)
	folder, second := t.TempDir(), t.TempDir()
	add(t, idA, folder)
	add(t, idA, second)
	forget := func(p string) {
		t.Helper()
		if _, err := Update("test", func(r *Record) error {
			if !r.ForgetPath(idA, Spelling(p)) {
				t.Error("the exact stored spelling was not forgotten")
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	forget(second)
	rec, _ := Load()
	if e := rec.Archives[idA]; e == nil || len(e.Paths) != 1 || e.Paths[0].Spelling != Spelling(folder) {
		t.Fatalf("forgetting one path took the other with it: %+v", e)
	}
	forget(folder)
	rec, _ = Load()
	if e := rec.Archives[idA]; e == nil || len(e.Paths) != 0 {
		t.Fatalf("an id with no paths left was dropped: %+v", e)
	}
	Update("test", func(r *Record) error { r.ForgetID(idA); return nil })
	rec, _ = Load()
	if !rec.Archives[idA].Forgotten {
		t.Fatal("forgetting the whole id set no tombstone")
	}
	add(t, idA, folder)
	rec, _ = Load()
	if rec.Archives[idA].Forgotten || len(rec.Archives[idA].Paths) != 1 {
		t.Fatalf("recording again did not clear the tombstone: %+v", rec.Archives[idA])
	}
}

// covers: MA-769, R37
// Matching: a folder inside a recorded one matches it; a folder reached through a
// symlinked parent matches; a recorded folder that is itself a link is recorded by
// its target; letter case folds only where PathSourceID folds.
func TestTheRecordMatchesEverySpellingOfARecordedFolder(t *testing.T) {
	sandbox(t)
	base := t.TempDir()
	real := filepath.Join(base, "Real")
	os.Mkdir(real, 0o700)
	add(t, idA, real)
	rec, _ := Load()
	if m, ok := rec.Lookup(filepath.Join(real, "units", "ab")); !ok || m.Folder != real {
		t.Fatalf("a folder inside the recorded one: %+v %v", m, ok)
	}
	link := filepath.Join(base, "via")
	if err := os.Symlink(base, link); err == nil {
		if _, ok := rec.Lookup(filepath.Join(link, "Real")); !ok {
			t.Fatal("a symlinked parent hid the recorded folder")
		}
		linked := filepath.Join(base, "linked")
		os.Symlink(real, linked)
		Update("test", func(r *Record) error { return r.Add(idB, linked, false, time.Now()) })
		rec, _ = Load()
		target, _ := filepath.EvalSymlinks(real)
		if got := rec.Archives[idB].Paths[0].Spelling; got != Spelling(target) {
			t.Fatalf("a linked folder was recorded as %s, want its target %s", got, Spelling(target))
		}
	}
	other := filepath.Join(base, "real")
	_, ok := rec.Lookup(other)
	switch runtime.GOOS {
	case "windows", "darwin":
		if !ok {
			t.Fatal("another letter case did not match on a case-insensitive system")
		}
	default:
		if ok {
			t.Fatal("another letter case matched on a case-sensitive system")
		}
	}
}
