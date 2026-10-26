package encmark_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/desktop"
	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/graph"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/vault"
)

// covers: MA-771, R37
// graphconfig and graph receive the settings guard from this package's init, since
// neither can import it; so every MailArchive program must link it. Each shipped
// program's dependencies include internal/encmark.
func TestEveryProgramLinksTheSettingsGuard(t *testing.T) {
	root, _ := filepath.Abs("../..")
	for _, prog := range []string{"./cmd/mailarchive", "./cmd/mailarchive-desktop", "./cmd/mailarchive-gui"} {
		cmd := exec.Command("go", "list", "-deps", prog)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local")
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("go list %s: %v", prog, err)
		}
		if !strings.Contains(string(out), "mail-archive-tool/internal/encmark\n") {
			t.Errorf("%s does not link internal/encmark, so its settings writes go unguarded", prog)
		}
	}
}

// covers: MA-771, R37
// Every settings write outside a run — Desktop settings, the Microsoft 365
// settings, a stored secret, the sign-in cache, the list of archives — refuses a
// file inside an encrypted archive, naming the file and the archive, and writes
// nothing (P2a-C14 extended to plaintext users' settings, phase3 §6). The positive
// twin: each writes in an ordinary folder.
func TestNoSettingsAreWrittenInsideAnEncryptedArchive(t *testing.T) {
	t.Cleanup(encrecord.SetDirForTest(t.TempDir()))
	writers := map[string]func(dir string) error{
		"Desktop settings": func(dir string) error {
			return desktop.SaveSettings(filepath.Join(dir, "desktop-settings.json"), desktop.Settings{})
		},
		"Microsoft 365 settings": func(dir string) error {
			return graphconfig.Save(filepath.Join(dir, "graph-config.json"), &graphconfig.Config{})
		},
		"a stored secret": func(dir string) error {
			return graphconfig.NewFileSecretStore(dir).Set("graph:abc", "s3cret")
		},
		"the sign-in cache": func(dir string) error {
			return graph.PersistSignIn(graph.Config{TokenCachePath: filepath.Join(dir, "token.json")}, []byte("{}"))
		},
		"the list of archives": func(dir string) error {
			return archindex.WriteFile(filepath.Join(dir, "known-archives.json"), archindex.Index{})
		},
	}
	for name, write := range writers {
		t.Run(name, func(t *testing.T) {
			plain := t.TempDir()
			if err := write(plain); err != nil {
				t.Fatalf("the positive twin, an ordinary folder: %v", err)
			}
			enc := t.TempDir()
			os.WriteFile(filepath.Join(enc, vault.FileName), []byte("{}"), 0o600)
			settings := filepath.Join(enc, "settings")
			err := write(settings)
			if !errors.Is(err, encmark.ErrSettingsInEncrypted) {
				t.Fatalf("want ErrSettingsInEncrypted, got %v", err)
			}
			assure.Refused(t, 1, err.Error(), assure.Code(1), assure.Names(enc, "outside every archive"),
				assure.NoSideEffect(func() bool { _, serr := os.Stat(settings); return os.IsNotExist(serr) }))
		})
	}
}
