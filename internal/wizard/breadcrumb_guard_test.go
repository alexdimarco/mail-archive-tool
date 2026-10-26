package wizard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/vault"
)

// covers: MA-775, MA-771, R37
// The wizard's last-archive note is a settings write: with MailArchive's settings
// folder inside an encrypted archive, neither writer of the note puts it there, and
// the archive is left exactly as it was (phase3 §6 E5; the P3-0 adversarial pass,
// OUT-03). The positive twin: with an ordinary settings folder each writes the note.
func TestTheLastArchiveNoteIsNeverWrittenInsideAnEncryptedArchive(t *testing.T) {
	// home points the per-user settings folder under dir, on every system.
	home := func(t *testing.T, dir string) string {
		t.Setenv("HOME", dir)
		t.Setenv("XDG_CONFIG_HOME", "")
		t.Setenv("AppData", filepath.Join(dir, "AppData"))
		p, err := lastArchiveFile()
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	out := filepath.Join(t.TempDir(), "Archive")
	for name, write := range map[string]func(string){"rememberArchive": rememberArchive, "reconcileBreadcrumb": reconcileBreadcrumb} {
		t.Run(name, func(t *testing.T) {
			p := home(t, t.TempDir())
			write(out)
			if data, err := os.ReadFile(p); err != nil || strings.TrimSpace(string(data)) != out {
				t.Fatalf("the positive twin: the note reads %q (%v), want %q", data, err, out)
			}

			enc := t.TempDir()
			if err := os.WriteFile(filepath.Join(enc, vault.FileName), []byte("{}"), 0o600); err != nil {
				t.Fatal(err)
			}
			settingsHome := filepath.Join(enc, "home")
			home(t, settingsHome)
			write(out)
			if _, err := os.Stat(settingsHome); !os.IsNotExist(err) {
				t.Fatalf("%s wrote into the encrypted archive (%v)", name, err)
			}
		})
	}
}
