package schedule

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/vault"
)

// sealedFolder is a folder holding a committed vault: an encrypted archive.
func sealedFolder(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	p, err := vault.PrepareCreate(dir, "schedule test passphrase")
	if err != nil {
		t.Fatal(err)
	}
	k, err := p.Commit()
	if err != nil {
		t.Fatal(err)
	}
	k.Destroy()
	return dir
}

// covers: MA-740, MA-746, R37
// No schedule is installed for an encrypted archive yet (phase2a §5; its unattended
// key arrives in Phase 4): an install whose archive is encrypted, or sits inside one,
// or whose log would be inside one, is refused (ErrEncryptedArchive, naming the
// archive) before the scheduler gets an entry and before any descriptor is written —
// every surface (CLI, wizard, Desktop, repair) installs through Install. The positive
// twin: the same install over a plaintext archive writes its entry and descriptor.
func TestNoScheduleIsInstalledForAnEncryptedArchive(t *testing.T) {
	origList, origInstall := crontabList, crontabInstall
	t.Cleanup(func() { crontabList, crontabInstall = origList, origInstall })
	wrote := false
	crontabList = func() (string, error) { return "", nil }
	crontabInstall = func(string) error { wrote = true; return nil }

	plain := t.TempDir()
	spec := Spec{Name: "mailarchive-plain", Interval: Daily, At: "02:00", Exe: "/bin/x", Out: plain, Log: filepath.Join(plain, "x.log")}
	if err := installOn(spec, "linux"); err != nil || !wrote {
		t.Fatalf("the positive twin: a plaintext archive's schedule installs (%v, wrote %v)", err, wrote)
	}
	plainDesc := filepath.Join(plain, descriptorFileFor(spec))
	before, err := os.ReadFile(plainDesc)
	if err != nil {
		t.Fatalf("the positive twin writes its descriptor: %v", err)
	}

	sealed := sealedFolder(t)
	inner := filepath.Join(sealed, "inner")
	if err := os.Mkdir(inner, 0o700); err != nil {
		t.Fatal(err)
	}
	// covers MA-746 too: a link into the encrypted archive is seen through.
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(inner, link); err != nil {
		link = inner
	}
	// A refused install writes no descriptor: none appears in an archive that had
	// none, and the plain archive's (written by the twin above) stays byte-identical.
	descriptorUntouched := func(s Spec) bool {
		got, derr := os.ReadFile(filepath.Join(s.Out, descriptorFileFor(s)))
		if s.Out == plain {
			return derr == nil && bytes.Equal(got, before)
		}
		return os.IsNotExist(derr)
	}
	for label, s := range map[string]Spec{
		"a link into an encrypted archive":     {Name: "mailarchive-link", Interval: Daily, At: "02:00", Exe: "/bin/x", Out: link},
		"an encrypted archive":                 {Name: "mailarchive-enc", Interval: Daily, At: "02:00", Exe: "/bin/x", Out: sealed, Log: filepath.Join(sealed, "x.log")},
		"a folder inside an encrypted archive": {Name: "mailarchive-in", Interval: Daily, At: "02:00", Exe: "/bin/x", Out: inner},
		// The plain archive's own schedule, re-installed with its log moved into an
		// encrypted archive: under its own name no collision guard stands in for the
		// refusal, so only the refusal keeps its descriptor from being rewritten.
		"a log inside an encrypted archive": {Name: spec.Name, Interval: Daily, At: "02:00", Exe: "/bin/x", Out: plain, Log: filepath.Join(sealed, "x.log")},
	} {
		t.Run(label, func(t *testing.T) {
			wrote = false
			err := installOn(s, "linux")
			if !errors.Is(err, ErrEncryptedArchive) {
				t.Fatalf("want ErrEncryptedArchive, got %v", err)
			}
			assure.Refused(t, 1, err.Error(), assure.Code(1), assure.Names(sealed, "encrypted archive", "nothing was installed"),
				assure.NoSideEffect(func() bool {
					_, lerr := os.Stat(s.Log)
					return !wrote && descriptorUntouched(s) && (s.Log == "" || os.IsNotExist(lerr))
				}))
		})
	}
}
