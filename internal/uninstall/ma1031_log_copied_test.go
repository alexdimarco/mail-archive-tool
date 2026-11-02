package uninstall

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/assure"
)

// covers: MA-1031, R36
// `uninstall -yes` asks CheckLogCopied before it removes anything (the 1.5.3 friction review's
// D2, the main session's decision): a deletion log holding any line — even one MailArchive
// cannot parse — refuses with LogNotCopiedError, naming the log, where to copy it and the
// command that goes on (-log-copied), and the log is left as it was. Positive twin first: no
// log, an absent log, a log of blank lines, and a log holding lines with -log-copied each go
// ahead.
func TestUninstallYesRefusesUntilTheDeletionLogIsCopied(t *testing.T) {
	cfg := t.TempDir()
	log := filepath.Join(cfg, archdelete.LogName)

	// Positive twins.
	if err := CheckLogCopied("", false); err != nil {
		t.Fatalf("no deletion log: %v", err)
	}
	if err := CheckLogCopied(log, false); err != nil {
		t.Fatalf("an absent deletion log holds no line: %v", err)
	}
	writeFile(t, log, "\n  \n")
	if err := CheckLogCopied(log, false); err != nil {
		t.Fatalf("a deletion log of blank lines holds no line: %v", err)
	}
	for _, body := range []string{`{"event":"finished","run":"r1"}` + "\n", "not a line MailArchive wrote\n"} {
		writeFile(t, log, body)
		if err := CheckLogCopied(log, true); err != nil {
			t.Fatalf("with -log-copied a log holding lines goes ahead: %v", err)
		}
	}

	// Refused: any line, whether or not it parses.
	for _, body := range []string{`{"event":"finished","run":"r1"}` + "\n", "not a line MailArchive wrote\n"} {
		writeFile(t, log, body)
		err := CheckLogCopied(log, false)
		var nc *LogNotCopiedError
		code := 0
		if errors.As(err, &nc) {
			code = 1
		}
		assure.Refused(t, code, errText(err), assure.Code(1),
			assure.Names(log, "so nothing was removed", "Copy it somewhere you keep records", "then run mailarchive uninstall -yes -log-copied."),
			assure.NoSideEffect(func() bool { b, _ := os.ReadFile(log); return string(b) == body }))
	}
}

// covers: MA-1031, R36
// A deletion log that cannot be read stops `uninstall -yes` (fail closed, as Uninstall's own
// check): it is LogUnreadableError, naming the log and "stopped before removing anything", and
// what is there is left (amended 2026-11-02, the uninstall-docs group's adversarial pass, finding
// 4). Here a folder stands where the log should be (archdelete.ErrLogNotRegular), on every
// system. Positive twin first: an ordinary empty log goes ahead.
func TestUninstallYesStopsOnADeletionLogItCannotRead(t *testing.T) {
	cfg := t.TempDir()
	log := filepath.Join(cfg, archdelete.LogName)
	writeFile(t, log, "")
	if err := CheckLogCopied(log, false); err != nil {
		t.Fatalf("positive twin: an empty log goes ahead: %v", err)
	}
	if err := os.Remove(log); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(log, 0o700); err != nil {
		t.Fatal(err)
	}
	err := CheckLogCopied(log, false)
	var lu *LogUnreadableError
	code := 0
	if errors.As(err, &lu) && errors.Is(err, archdelete.ErrLogNotRegular) {
		code = 1
	}
	assure.Refused(t, code, errText(err), assure.Code(1),
		assure.Names(log, "stopped before removing anything", "not an ordinary file"),
		assure.NoSideEffect(func() bool { fi, serr := os.Stat(log); return serr == nil && fi.IsDir() }))
}

// covers: MA-1031, R36
// `uninstall -yes` asks the log again on its real run (UninstallYes), not only at the plan
// (amended 2026-11-02, the uninstall-docs group's adversarial pass, finding 1): a log that was
// absent or empty when the plan was printed, and that a delete started and finished in since
// (another Desktop tab, another terminal), refuses before anything is removed, "so nothing was
// removed"; and one filled after the first check, right before the settings files would go,
// stops there, "so it and the settings files were kept". Positive twins first: an empty log goes
// with -yes alone, and a log holding lines with -log-copied.
func TestUninstallYesAsksTheLogAgainOnItsRealRun(t *testing.T) {
	noSchedulesNoSignIn(t)
	cfg := t.TempDir()
	settings := filepath.Join(cfg, "desktop-settings.json")
	gone := filepath.Join(t.TempDir(), "Old mail")
	started := map[string]any{"at": "2026-11-01T10:00:00Z", "event": "started", "run": "r9", "folder": gone, "row": gone, "kind": "plain"}
	finished := map[string]any{"at": "2026-11-01T10:00:02Z", "event": "finished", "run": "r9", "folder": gone, "outcome": "complete"}

	// Positive twins.
	writeFile(t, settings, "{}")
	log := logLines(t, cfg)
	if _, err := UninstallYes(cfg, nil, "", false); err != nil || exists(log) || exists(settings) {
		t.Fatalf("positive twin: an empty log goes with -yes: %v (log %v, settings %v)", err, exists(log), exists(settings))
	}
	writeFile(t, settings, "{}")
	log = logLines(t, cfg, started, finished)
	if _, err := UninstallYes(cfg, nil, "", true); err != nil || exists(log) || exists(settings) {
		t.Fatalf("positive twin: a log holding lines goes with -log-copied: %v (log %v, settings %v)", err, exists(log), exists(settings))
	}

	notCopied := func(err error) int {
		var nc *LogNotCopiedError
		if errors.As(err, &nc) {
			return 1
		}
		return 0
	}
	// Absent or empty at the plan; a whole delete since; then the real run.
	for _, atPlan := range []string{"absent", "empty"} {
		t.Run(atPlan, func(t *testing.T) {
			writeFile(t, settings, "{}")
			os.Remove(log)
			if atPlan == "empty" {
				logLines(t, cfg)
			}
			rep, err := Uninstall(cfg, nil, "", true) // the plan uninstall -yes prints
			if err != nil || CheckLogCopied(rep.DeletionsLog, false) != nil {
				t.Fatalf("the plan goes ahead: %v", err)
			}
			log = logLines(t, cfg, started, finished)
			before, _ := os.ReadFile(log)
			done, err := UninstallYes(cfg, nil, "", false)
			assure.Refused(t, notCopied(err), errText(err), assure.Code(1),
				assure.Names(log, "so nothing was removed", "then run mailarchive uninstall -yes -log-copied."),
				assure.Forbid("before the settings files were removed"),
				assure.NoSideEffect(func() bool {
					after, _ := os.ReadFile(log)
					return done == nil && string(after) == string(before) && exists(settings)
				}))
		})
	}

	// Filled after the real run's first check, before the settings files would go.
	writeFile(t, settings, "{}")
	log = logLines(t, cfg)
	orig := pendingDeletes
	t.Cleanup(func() { pendingDeletes = orig })
	calls := 0
	pendingDeletes = func(path, goos string) ([]string, error) {
		calls++
		if calls == 2 { // a delete began and finished meanwhile
			logLines(t, cfg, started, finished)
		}
		return orig(path, goos)
	}
	_, err := UninstallYes(cfg, nil, "", false)
	assure.Refused(t, notCopied(err), errText(err), assure.Code(1),
		assure.Names(log, "before the settings files were removed", "so it and the settings files were kept", "then run mailarchive uninstall -yes -log-copied."),
		assure.Forbid("so nothing was removed"),
		assure.NoSideEffect(func() bool {
			b, _ := os.ReadFile(log)
			return exists(settings) && strings.Contains(string(b), `"run":"r9"`)
		}))
	assure.Reached(t, calls == 2, "the real run asked the log again before removing the settings")
}
