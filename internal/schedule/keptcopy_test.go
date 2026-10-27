package schedule

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/vault"
)

// covers: MA-834, R37
// Scheduling from a copy of MailArchive kept in an encrypted archive is refused at the
// one chokepoint every caller uses (program design §6, C15): a program inside an
// encrypted archive — its program folder, an unzipped app there, anywhere in it — and
// a self-copy by name (mailarchive-self-…) wherever it was moved, with ErrKeptCopy and
// no path. A release program outside any archive still schedules (the positive twin).
func TestASchedulesProgramIsNeverAKeptCopy(t *testing.T) {
	orig := SelfResolver
	t.Cleanup(func() { SelfResolver = orig })
	resolve := func(p string) (string, error) {
		SelfResolver = func() (string, error) { return p, nil }
		exe, _, err := ProgramForSchedule(false)
		return exe, err
	}
	plain := filepath.Join(t.TempDir(), "mailarchive")
	os.WriteFile(plain, []byte("x"), 0o700)
	if exe, err := resolve(plain); err != nil || exe != plain {
		t.Fatalf("positive twin: a program outside any archive: %q %v", exe, err)
	}
	archive := t.TempDir()
	os.WriteFile(filepath.Join(archive, vault.FileName), []byte("{}"), 0o600)
	for name, p := range map[string]string{
		"in program/":           filepath.Join(archive, "program", "mailarchive-linux-amd64"),
		"an app unzipped there": filepath.Join(archive, "program", "MailArchive.app", "Contents", "MacOS", "mailarchive"),
		"elsewhere in it":       filepath.Join(archive, "copied", "mailarchive"),
		"a self-copy moved":     filepath.Join(t.TempDir(), "mailarchive-self-darwin-arm64"),
		"a self-copy's folder":  filepath.Join(t.TempDir(), "mailarchive-self-darwin-universal", "MailArchive.app", "Contents", "MacOS", "mailarchive"),
	} {
		t.Run(name, func(t *testing.T) {
			os.MkdirAll(filepath.Dir(p), 0o700)
			os.WriteFile(p, []byte("x"), 0o700)
			exe, err := resolve(p)
			assure.Refused(t, 1, errMsg(err), assure.Code(1), assure.Names("kept inside an encrypted archive", "Do this"),
				assure.NoSideEffect(func() bool { return errors.Is(err, ErrKeptCopy) && exe == "" }))
		})
	}
}

func errMsg(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
