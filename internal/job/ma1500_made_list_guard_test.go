package job_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/vault"
)

// covers: MA-1500, R37
// The made-list is a settings file: a record or a strike of it is refused through
// encmark.RefuseSettingsWrite, as the wizard's last-archive note is, when MailArchive's settings
// folder is inside an encrypted archive (it names the archive folders a person made), with words
// naming the list, the archive and the remedy, and nothing is written: no settings folder is made,
// and a list already there is left as it was. Positive twin first: in an ordinary folder both work.
func TestTheMadeListIsNeverWrittenInsideAnEncryptedArchive(t *testing.T) {
	t.Cleanup(encrecord.SetDirForTest(t.TempDir()))
	parent := filepath.Join(t.TempDir(), "Mail archives")
	a := filepath.Join(parent, "qzsam.walker@gmail.com")
	plain := filepath.Join(t.TempDir(), "settings", job.MadeListName)
	if err := job.RecordMade(plain, a, "imap.gmail.com"); err != nil {
		t.Fatalf("the positive twin, a record in an ordinary folder: %v", err)
	}
	if err := job.StrikeMade(plain, []string{a}); err != nil {
		t.Fatalf("the positive twin, a strike: %v", err)
	}

	enc := t.TempDir()
	if err := os.WriteFile(filepath.Join(enc, vault.FileName), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	settings := filepath.Join(enc, "settings")
	inside := filepath.Join(settings, job.MadeListName)
	refused := func(t *testing.T, err error, unchanged func() bool) {
		t.Helper()
		rc, msg := 0, ""
		if errors.Is(err, encmark.ErrSettingsInEncrypted) {
			rc, msg = 1, err.Error()
		}
		assure.Refused(t, rc, msg, assure.Code(1),
			assure.Names("the wizard's list of the archive folders it made", inside, enc, "outside every archive"),
			assure.NoSideEffect(unchanged))
	}
	t.Run("a record", func(t *testing.T) {
		refused(t, job.RecordMade(inside, a, "imap.gmail.com"), func() bool {
			_, err := os.Stat(settings)
			return os.IsNotExist(err)
		})
	})
	t.Run("a strike", func(t *testing.T) {
		if err := os.MkdirAll(settings, 0o700); err != nil {
			t.Fatal(err)
		}
		line := []byte(a + "\timap.gmail.com\n")
		if err := os.WriteFile(inside, line, 0o600); err != nil {
			t.Fatal(err)
		}
		refused(t, job.StrikeMade(inside, []string{a}), func() bool {
			data, err := os.ReadFile(inside)
			_, lerr := os.Lstat(job.MadeLockPath(inside))
			return err == nil && string(data) == string(line) && os.IsNotExist(lerr)
		})
	})
}
