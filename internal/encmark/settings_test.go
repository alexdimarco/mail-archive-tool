package encmark_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/desktop"
	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/graph"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/instance"
	"mail-archive-tool/internal/job"
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
	recordDir := t.TempDir()
	t.Cleanup(encrecord.SetDirForTest(recordDir))
	// Each writer and the file it writes in a settings folder dir. The P3-0
	// adversarial pass added the scheduled job, the instance file, the record itself
	// and the settings file's kept copy (OUT-03, SEC-02).
	type writer struct {
		file  string
		write func(dir string) error
		// untouched, when set, is the side effect's absence for a writer that finds a
		// settings folder already there; otherwise the folder must never appear.
		untouched func(dir string) bool
	}
	writers := map[string]writer{
		"Desktop settings": {file: "desktop-settings.json", write: func(dir string) error {
			return desktop.SaveSettings(filepath.Join(dir, "desktop-settings.json"), desktop.Settings{})
		}},
		// An unreadable settings file already in the folder: the copy kept of it is a
		// settings write too (the mutation sweep showed the folder never held one).
		"Desktop settings' kept copy": {file: "desktop-settings.json", write: func(dir string) error {
			p := filepath.Join(dir, "desktop-settings.json")
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return err
			}
			if err := os.WriteFile(p, []byte("not settings"), 0o600); err != nil {
				return err
			}
			return desktop.UpdateSettings(p, "test", func(*desktop.Settings) {})
		}, untouched: func(dir string) bool {
			entries, err := os.ReadDir(dir)
			return err == nil && len(entries) == 1 && entries[0].Name() == "desktop-settings.json"
		}},
		"Microsoft 365 settings": {file: "graph-config.json", write: func(dir string) error {
			return graphconfig.Save(filepath.Join(dir, "graph-config.json"), &graphconfig.Config{})
		}},
		"a stored secret": {file: "graph_abc.secret", write: func(dir string) error {
			return graphconfig.NewFileSecretStore(dir).Set("graph:abc", "s3cret")
		}},
		"the sign-in cache": {file: "token.json", write: func(dir string) error {
			return graph.PersistSignIn(graph.Config{TokenCachePath: filepath.Join(dir, "token.json")}, []byte("{}"))
		}},
		"the list of archives": {file: "known-archives.json", write: func(dir string) error {
			return archindex.WriteFile(filepath.Join(dir, "known-archives.json"), archindex.Index{})
		}},
		"a scheduled job": {file: "nightly.json", write: func(dir string) error {
			return job.Write(filepath.Join(dir, "nightly.json"), job.Job{Out: "/x", Mode: "incremental"})
		}},
		"the Desktop instance file": {file: "desktop-instance.json", write: func(dir string) error {
			return instance.Write(filepath.Join(dir, "desktop-instance.json"), instance.Info{})
		}},
		"the record of encrypted archives": {file: encrecord.FileName, write: func(dir string) error {
			restore := encrecord.SetDirForTest(dir)
			defer restore()
			_, err := encrecord.Update("test", func(*encrecord.Record) error { return nil })
			return err
		}},
	}
	// The archive is known by its key file, or only by this computer's record — its
	// key files gone (the second proves the record reaches the guard, FID-08).
	marked := func(t *testing.T) string {
		enc := t.TempDir()
		os.WriteFile(filepath.Join(enc, vault.FileName), []byte("{}"), 0o600)
		return enc
	}
	recordedOnly := func(t *testing.T) string {
		enc := t.TempDir()
		restore := encrecord.SetDirForTest(recordDir)
		defer restore()
		encrecord.Update("test", func(r *encrecord.Record) error {
			return r.Add("0123456789abcdef0123456789abcdef", enc, false, time.Now())
		})
		t.Cleanup(func() {
			restore := encrecord.SetDirForTest(recordDir)
			defer restore()
			encrecord.Update("test", func(r *encrecord.Record) error { r.ForgetID("0123456789abcdef0123456789abcdef"); return nil })
		})
		return enc
	}
	for name, w := range writers {
		for how, make := range map[string]func(*testing.T) string{"marked": marked, "recorded only": recordedOnly} {
			if name == "the record of encrypted archives" && how == "recorded only" {
				// The record lives in the settings folder it guards: inside a recorded
				// archive, it is that record which names the archive, which this test
				// (moving the record's folder) cannot set up.
				continue
			}
			t.Run(name+"/"+how, func(t *testing.T) {
				if err := w.write(t.TempDir()); err != nil {
					t.Fatalf("the positive twin, an ordinary folder: %v", err)
				}
				enc := make(t)
				settings := filepath.Join(enc, "settings")
				err := w.write(settings)
				if !errors.Is(err, encmark.ErrSettingsInEncrypted) {
					t.Fatalf("want ErrSettingsInEncrypted, got %v", err)
				}
				assure.Refused(t, 1, err.Error(), assure.Code(1),
					assure.Names(filepath.Join(settings, w.file), enc, "outside every archive"),
					assure.NoSideEffect(func() bool {
						if w.untouched != nil {
							return w.untouched(settings)
						}
						_, serr := os.Stat(settings)
						return os.IsNotExist(serr)
					}))
			})
		}
	}
}
