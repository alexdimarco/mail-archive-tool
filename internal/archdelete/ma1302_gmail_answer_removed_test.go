package archdelete

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/gmailanswer"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/state"
)

// covers: MA-1302, R45
// Delete's step 3 and Remove from list's (removeNameFiles, which both run) remove the Gmail answer
// this computer keeps for the folder, as they remove the alert files (the B3 adversarial pass's A1,
// turned from its probe TestB3AdvDeleteStep3KeepsTheGmailAnswer), whichever spelling of the
// folder they are given. Positive twin first: the alert streak beside it is removed, and another
// archive's answer is left alone.
func TestDeleteAndRemoveFromListRemoveTheGmailAnswer(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	dir := smallArchive(t, t.TempDir())
	cfg, err := job.Dir()
	if err != nil {
		t.Fatal(err)
	}
	spelling := dir
	if ignoresCase(dir) {
		spelling = strings.ToUpper(dir) // the same folder, another spelling (A3)
	}
	streak := filepath.Join(cfg, schedule.DefaultNameFor(spelling)+alertStreakSuffix)
	writeFile(t, streak, "x")
	if err := gmailanswer.KeepHere(dir, gmailanswer.Answer{Archive: state.NewArchiveID(), Yes: []string{"imap.gmail.com"}}); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(home, "other")
	if err := gmailanswer.KeepHere(other, gmailanswer.Answer{Archive: state.NewArchiveID(), NotNow: true}); err != nil {
		t.Fatal(err)
	}
	ap, _ := gmailanswer.Path(dir)
	op, _ := gmailanswer.Path(other)
	if failed, err := removeNameFiles([]string{schedule.DefaultNameFor(dir)}, []string{spelling}, ""); err != nil {
		t.Fatalf("%s: %v", failed, err)
	}
	if exists(streak) {
		t.Fatal("positive twin: the alert streak was not removed")
	}
	if !exists(op) {
		t.Fatal("positive twin: another archive's Gmail answer was removed")
	}
	if _, err := os.Lstat(ap); err == nil {
		t.Fatalf("step 3 left the Gmail answer %s", ap)
	}
}

// ignoresCase says whether dir's file system finds it spelled in upper case.
func ignoresCase(dir string) bool {
	a, err1 := os.Stat(dir)
	b, err2 := os.Stat(strings.ToUpper(dir))
	return err1 == nil && err2 == nil && os.SameFile(a, b)
}
