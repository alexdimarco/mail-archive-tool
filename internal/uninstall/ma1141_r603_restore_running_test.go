package uninstall

import (
	"errors"
	"os"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/tbrestore"
)

// covers: MA-1141, R40, S71
// R6-03 (settled 2026-11-07, the lead's call): uninstall refuses while a restore into
// Thunderbird is running — its restore lock held — and never removes restores.log under
// it. Positive twin: with no restore running the plan carries no refusal. Held: the dry
// run still gives its plan, with "Uninstall will refuse until…" (Waiting); the real run
// is refused before anything is removed, restores.log and the lock still there.
func TestUninstallRefusesWhileARestoreRuns(t *testing.T) {
	cfg, err := job.Dir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cfg, 0o700); err != nil {
		t.Fatal(err)
	}
	logPath, _ := tbrestore.LogPath()
	if err := tbrestore.NewLog(logPath).Append(tbrestore.LogLine{Event: tbrestore.EvStarted, ID: "running1"}); err != nil {
		t.Fatal(err)
	}
	was := restoreLeftovers
	restoreLeftovers = func() []string { return nil }
	defer func() { restoreLeftovers = was }()

	rep, err := Uninstall(cfg, nil, "", true)
	assure.Reached(t, err == nil && rep != nil, "positive twin: no restore running, no refusal")

	rl, err := tbrestore.AcquireRestoreLock(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer rl.Release()
	rep, err = Uninstall(cfg, nil, "", true)
	var w Waiting
	assure.Reached(t, rep != nil && errors.As(err, &w) && strings.Contains(w.Until(), "restore into Thunderbird that is running"),
		"the dry run gives the plan and says uninstall will refuse until the restore finishes")
	logBefore, _ := os.ReadFile(logPath)
	_, err = UninstallYes(cfg, nil, "", true)
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	assure.Refused(t, map[bool]int{true: 1}[err != nil], msg, assure.Code(1), assure.Names("A restore into Thunderbird is running", "restores.log", "removed nothing"),
		assure.NoSideEffect(func() bool { b, rerr := os.ReadFile(logPath); return rerr == nil && string(b) == string(logBefore) }))
}
