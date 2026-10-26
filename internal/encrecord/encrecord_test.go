package encrecord

import (
	"bytes"
	"errors"
	"fmt"
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
		}, CaseDamaged, []string{FileName, "damaged", "repair"}},
		{"newer with a longer header beside a valid older copy", func(t *testing.T, p string) {
			// A future format may add header fields or lengthen the line: still newer,
			// never damaged, and the valid format-1 mirror is not used past it (CRIT-01).
			body := []byte("MAILARCHIVE-RECORD 2 seq=9 gen=4 " + strings.Repeat("x", 300) + "\n{}\n")
			os.WriteFile(p, body, 0o600)
		}, CaseNewer, []string{FileName, "newer MailArchive"}},
		{"owned by another account", func(t *testing.T, p string) {
			was := instance.OwnerReader
			instance.OwnerReader = func(string) (string, []string, error) { return "4242", []string{"501"}, nil }
			t.Cleanup(func() { instance.OwnerReader = was })
		}, CasePermissions, []string{FileName, "not yours"}},
		{"a planted link", func(t *testing.T, p string) {
			outside := filepath.Join(t.TempDir(), "elsewhere")
			os.WriteFile(outside, []byte("x"), 0o600)
			os.Remove(p)
			os.Remove(p + ".bak")
			if err := os.Symlink(outside, p); err != nil {
				t.Skip("no symlinks here:", err)
			}
		}, CaseDamaged, []string{FileName, "damaged"}},
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

// covers: MA-775, R37
// A record whose folder cannot be found or searched is its own case — not damage:
// no settings folder at all (no home folder), or a folder MailArchive may not read.
// It names the cause; repair is not offered. A change is refused, writing nothing,
// and a folder that cannot be written is not reported as "busy" (REG-03, CON-05).
func TestARecordThatCannotBeLookedForIsItsOwnCase(t *testing.T) {
	was := dirFn
	dirFn = func() (string, error) { return "", errors.New("no home folder") }
	_, err := Load()
	var ue *UnusableError
	if !errors.As(err, &ue) || ue.Case != CaseUnreadable || !strings.Contains(err.Error(), "no home folder") {
		t.Fatalf("no settings folder: %v", err)
	}
	dirFn = was
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("mode bits do not stop this user here")
	}
	dir := sandbox(t)
	add(t, idA, t.TempDir())
	os.Chmod(dir, 0o000)
	t.Cleanup(func() { os.Chmod(dir, 0o700) })
	_, err = Load()
	if !errors.As(err, &ue) || ue.Case != CaseUnreadable || strings.Contains(err.Error(), "repair") {
		t.Fatalf("a folder that cannot be searched: %v", err)
	}
	// A copy MailArchive may not read, in a folder it may: the same case, not damage
	// (the mutation sweep showed no test reached it).
	os.Chmod(dir, 0o700)
	p := filepath.Join(dir, FileName)
	os.Chmod(p, 0o000)
	_, err = Load()
	if !errors.As(err, &ue) || ue.Case != CaseUnreadable || strings.Contains(err.Error(), "repair") {
		t.Fatalf("a copy that cannot be read: %v", err)
	}
	os.Chmod(p, 0o600)
	// No lock file yet, in a folder that cannot be written: the lock cannot be taken,
	// and that is not "busy" — it names the folder (CON-05).
	os.Remove(filepath.Join(dir, LockName))
	os.Chmod(dir, 0o500)
	_, err = Update("test", func(r *Record) error { return nil })
	assure.Refused(t, code(err), err.Error(), assure.Code(1), assure.Names("cannot lock", dir),
		assure.NoSideEffect(func() bool { return !strings.Contains(err.Error(), "busy") }))
}

// covers: MA-775, R37
// A refused change leaves a damaged copy where it was: the copy is moved aside, and
// the lost-change notice given, only by the change that writes (CON-03). No notice
// is given when the damaged copy held the older change; and the new sequence steps
// past a damaged copy's higher one.
func TestADamagedCopyIsSetAsideOnlyByAWrite(t *testing.T) {
	dir := sandbox(t)
	add(t, idA, t.TempDir())
	stale, _ := os.ReadFile(filepath.Join(dir, MirrorName))
	add(t, idB, t.TempDir())
	os.WriteFile(filepath.Join(dir, MirrorName), stale, 0o600) // the mirror lags one change
	p := filepath.Join(dir, FileName)
	raw, _ := os.ReadFile(p)
	raw[len(raw)-5] ^= 1
	os.WriteFile(p, raw, 0o600)
	if _, err := Update("test", func(*Record) error { return errors.New("not this time") }); err == nil {
		t.Fatal("a failing change succeeded")
	}
	if aside, _ := filepath.Glob(p + ".damaged-*"); len(aside) != 0 {
		t.Fatalf("a refused change moved the damaged copy aside: %v", aside)
	}
	notice, err := Update("test", func(*Record) error { return nil })
	if err != nil || !strings.Contains(notice, "may have been lost") {
		t.Fatalf("the change that writes: %q %v", notice, err)
	}
	if rec, _ := Load(); rec.Seq() <= 2 {
		t.Fatalf("the new sequence %d did not step past the damaged copy's 2", rec.Seq())
	}

	// The mirror damaged while holding the OLDER change: no notice.
	add(t, idA, t.TempDir())
	m := filepath.Join(dir, MirrorName)
	mraw, _ := os.ReadFile(m)
	os.WriteFile(m, []byte(strings.Replace(string(mraw), "seq=", "seq=0", 1)[:20]+"\n"), 0o600)
	if notice, err := Update("test", func(*Record) error { return nil }); err != nil || notice != "" {
		t.Fatalf("an older damaged copy: %q %v", notice, err)
	}
}

// covers: MA-775, R37
// A record entry whose key is not an archive id makes the copy damaged.
func TestARecordWithAForeignKeyIsDamaged(t *testing.T) {
	dir := sandbox(t)
	add(t, idA, t.TempDir())
	rec, _ := Load()
	rec.Archives["NOT-AN-ID"] = &Entry{}
	data, _ := encode(rec, 5)
	os.WriteFile(filepath.Join(dir, FileName), data, 0o600)
	os.WriteFile(filepath.Join(dir, MirrorName), data, 0o600)
	var ue *UnusableError
	if _, err := Load(); !errors.As(err, &ue) || ue.Case != CaseDamaged {
		t.Fatalf("a foreign key: %v", err)
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
			if !r.ForgetPath(idA, recordedForm(p)) {
				t.Error("the exact stored spelling was not forgotten")
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	forget(second)
	rec, _ := Load()
	if e := rec.Archives[idA]; e == nil || len(e.Paths) != 1 || e.Paths[0].Spelling != recordedForm(folder) {
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
	// A link into a subfolder of the recorded archive is seen through (OUT-01).
	into := filepath.Join(t.TempDir(), "into")
	os.MkdirAll(filepath.Join(real, "sub"), 0o700)
	if err := os.Symlink(filepath.Join(real, "sub"), into); err == nil {
		for _, q := range []string{into, filepath.Join(into, "new")} {
			if _, ok := rec.Lookup(q); !ok {
				t.Fatalf("%s, a link into the recorded archive, was not matched", q)
			}
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

// covers: MA-775, R37
// On macOS one folder has more spellings than letter case: a composed and a
// decomposed accent, σ and ς, and the firmlink spelling under /System/Volumes/Data.
// Each reaches the recorded folder (OUT-02, SEC-01).
func TestTheRecordMatchesEveryMacSpellingOfOneFolder(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS file-system spellings")
	}
	sandbox(t)
	base := t.TempDir()
	for _, tc := range []struct{ recorded, other string }{
		{"Café", "Café"},
		{"Café", "Café"},
		{"Archσ", "Archς"},
	} {
		dir := filepath.Join(base, tc.recorded)
		os.Mkdir(dir, 0o700)
		a, _ := os.Stat(dir)
		b, err := os.Stat(filepath.Join(base, tc.other))
		if err != nil || !os.SameFile(a, b) {
			t.Skipf("this volume does not treat %q and %q as one folder", tc.recorded, tc.other)
		}
		id := fmt.Sprintf("%032x", len(tc.recorded)+len(tc.other)*7)
		Update("test", func(r *Record) error { return r.Add(id, dir, false, time.Now()) })
		rec, _ := Load()
		if m, ok := rec.Lookup(filepath.Join(base, tc.other, "sub")); !ok || m.ID != id {
			t.Fatalf("%q recorded, %q not matched: %+v", tc.recorded, tc.other, m)
		}
		// Gone, so no folder on disk can vouch for it: the spelling alone must match —
		// composed with decomposed, and σ with ς by full case folding (the mutation
		// sweep showed the folder's own identity hid a weaker fold).
		os.Remove(dir)
		if m, ok := rec.Lookup(filepath.Join(base, tc.other)); !ok || m.ID != id {
			t.Fatalf("%q recorded and gone, %q not matched by its spelling: %+v", tc.recorded, tc.other, m)
		}
	}
	os.Mkdir(filepath.Join(base, "Café"), 0o700) // the firmlink match is by the folder itself
	firm := filepath.Join("/System/Volumes/Data", mustResolve(t, base))
	if _, err := os.Stat(firm); err != nil {
		t.Skip("no firmlink spelling here:", err)
	}
	rec, _ := Load()
	if _, ok := rec.Lookup(filepath.Join(firm, "Café")); !ok {
		t.Fatal("the firmlink spelling of a recorded folder was not matched")
	}
}

func mustResolve(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
