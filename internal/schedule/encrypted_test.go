package schedule

import (
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

// covers: MA-740, R37
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
	if _, err := os.Stat(filepath.Join(plain, descriptorFileFor(spec))); err != nil {
		t.Fatalf("the positive twin writes its descriptor: %v", err)
	}

	sealed := sealedFolder(t)
	inner := filepath.Join(sealed, "inner")
	if err := os.Mkdir(inner, 0o700); err != nil {
		t.Fatal(err)
	}
	for label, s := range map[string]Spec{
		"an encrypted archive":                 {Name: "mailarchive-enc", Interval: Daily, At: "02:00", Exe: "/bin/x", Out: sealed, Log: filepath.Join(sealed, "x.log")},
		"a folder inside an encrypted archive": {Name: "mailarchive-in", Interval: Daily, At: "02:00", Exe: "/bin/x", Out: inner},
		"a log inside an encrypted archive":    {Name: "mailarchive-log", Interval: Daily, At: "02:00", Exe: "/bin/x", Out: plain, Log: filepath.Join(sealed, "x.log")},
	} {
		t.Run(label, func(t *testing.T) {
			wrote = false
			err := installOn(s, "linux")
			if !errors.Is(err, ErrEncryptedArchive) {
				t.Fatalf("want ErrEncryptedArchive, got %v", err)
			}
			assure.Refused(t, 1, err.Error(), assure.Code(1), assure.Names(sealed, "encrypted archive", "nothing was installed"),
				assure.NoSideEffect(func() bool {
					_, derr := os.Stat(filepath.Join(s.Out, descriptorFileFor(s)))
					_, lerr := os.Stat(s.Log)
					return !wrote && (s.Out == plain || os.IsNotExist(derr)) && (s.Log == "" || os.IsNotExist(lerr))
				}))
		})
	}
}
