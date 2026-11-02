package uninstall

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/assure"
)

// covers: MA-1031, R36
// A deletions.log that is not an ordinary file — a folder, a link to a log holding a delete that
// did not finish, a link to nothing — stops the real run of `uninstall -yes` (UninstallYes) and of
// the Desktop's Remove (Uninstall), removing nothing (the 1.5.3 re-walk's adversarial pass, F4):
// ConfigTargets lists only ordinary files, so before, the run saw no log, skipped both of its
// checks and removed every settings file, known-archives.json with them. Now run() asks the name
// itself (Lstat) and stops with LogUnreadableError, in its "not an ordinary file" words; the plan
// (a dry run, the Desktop's preview and `uninstall -dry-run`) says the same, as for any log it
// cannot read. Positive twin first: with an ordinary empty log, -yes removes the settings and the
// log.
func TestUninstallStopsOnADeletionLogThatIsNotAFile(t *testing.T) {
	noSchedulesNoSignIn(t)
	setup := func(t *testing.T) (cfg, settings, list, log string) {
		cfg = t.TempDir()
		settings = filepath.Join(cfg, "desktop-settings.json")
		list = filepath.Join(cfg, "known-archives.json")
		writeFile(t, settings, "{}")
		writeFile(t, list, `{"version":1,"entries":[]}`)
		return cfg, settings, list, filepath.Join(cfg, archdelete.LogName)
	}

	// Positive twin: an ordinary empty log goes with -yes alone.
	cfg, settings, list, log := setup(t)
	writeFile(t, log, "")
	if _, err := UninstallYes(cfg, nil, "", false); err != nil || exists(settings) || exists(list) || exists(log) {
		t.Fatalf("positive twin: -yes with an ordinary empty log: %v (settings kept %v, list kept %v, log kept %v)", err, exists(settings), exists(list), exists(log))
	}

	kinds := map[string]func(t *testing.T, log string){
		"a folder": func(t *testing.T, log string) {
			if err := os.Mkdir(log, 0o700); err != nil {
				t.Fatal(err)
			}
		},
		"a link to a log holding a delete that did not finish": func(t *testing.T, log string) {
			gone := filepath.Join(t.TempDir(), "Old mail")
			target := logLines(t, t.TempDir(), map[string]any{"at": "2026-11-01T10:00:00Z", "event": "started", "run": "r9", "folder": gone, "row": gone, "kind": "plain"})
			if err := os.Symlink(target, log); err != nil {
				t.Skipf("this computer does not let this account make a link: %v", err)
			}
		},
		"a link to nothing": func(t *testing.T, log string) {
			if err := os.Symlink(filepath.Join(t.TempDir(), "missing.log"), log); err != nil {
				t.Skipf("this computer does not let this account make a link: %v", err)
			}
		},
	}
	runs := map[string]func(cfg string) (*Report, error){
		"uninstall -yes":       func(cfg string) (*Report, error) { return UninstallYes(cfg, nil, "", false) },
		"the Desktop's Remove": func(cfg string) (*Report, error) { return Uninstall(cfg, nil, "", false) },
		"the plan (a dry run)": func(cfg string) (*Report, error) { return Uninstall(cfg, nil, "", true) },
	}
	for kind, plant := range kinds {
		for run, do := range runs {
			t.Run(run+" with "+kind, func(t *testing.T) {
				cfg, settings, list, log := setup(t)
				plant(t, log)
				before, lerr := os.Lstat(log)
				if lerr != nil {
					t.Fatal(lerr)
				}
				rep, err := do(cfg)
				var lu *LogUnreadableError
				code := 0
				if errors.As(err, &lu) && errors.Is(err, archdelete.ErrLogNotRegular) && rep == nil {
					code = 1
				}
				assure.Refused(t, code, errText(err), assure.Code(1),
					assure.Names(log, "so uninstall stopped before removing anything", "it is not an ordinary file (a link, a folder or a device)", "then uninstall again"),
					assure.NoSideEffect(func() bool {
						after, aerr := os.Lstat(log)
						return exists(settings) && exists(list) && aerr == nil && after.Mode() == before.Mode()
					}))
			})
		}
	}
}
