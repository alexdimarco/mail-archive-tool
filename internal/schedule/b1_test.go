package schedule

import (
	"runtime"
	"strings"
	"testing"
)

// covers: MA-49, R14, R12, S28
// A `-remove` for an archive that never had a schedule removes nothing and does
// NOT rewrite the crontab (which would materialize an empty one); a genuinely
// installed schedule (descriptor + managed block) is removed and the crontab is
// rewritten. The crontab side effects are sandboxed so the developer's real
// crontab is never touched (friction #9).
func TestRemoveIfInstalledNoOp(t *testing.T) {
	switch runtime.GOOS {
	case "linux", "freebsd", "openbsd", "netbsd":
	default:
		t.Skip("the crontab sandbox exercises cron platforms")
	}
	origList, origInstall := crontabList, crontabInstall
	defer func() { crontabList, crontabInstall = origList, origInstall }()

	wrote := false
	crontabList = func() (string, error) { return "", nil } // an empty crontab, no error
	crontabInstall = func(string) error { wrote = true; return nil }

	// No descriptor + empty crontab → nothing to remove, no write.
	out := t.TempDir()
	spec := Spec{Name: "mailarchive-none", Interval: Daily, At: "02:00", Exe: "/bin/x", Out: out}
	removed, err := RemoveIfInstalled(spec)
	if err != nil {
		t.Fatal(err)
	}
	if removed {
		t.Error("reported a removal when nothing was installed")
	}
	if wrote {
		t.Error("a no-op remove wrote the crontab")
	}

	// A real, installed schedule → removed, crontab rewritten, descriptor gone.
	if err := WriteDescriptor(out, DescribeSpec(spec)); err != nil {
		t.Fatal(err)
	}
	block, err := CronBlock(spec)
	if err != nil {
		t.Fatal(err)
	}
	crontabList = func() (string, error) { return block + "\n", nil }
	wrote = false
	removed, err = RemoveIfInstalled(spec)
	if err != nil {
		t.Fatal(err)
	}
	if !removed {
		t.Error("a real remove reported nothing removed")
	}
	if !wrote {
		t.Error("a real remove did not rewrite the crontab")
	}
	if _, derr := ReadDescriptor(out); derr == nil {
		t.Error("the descriptor survived a real remove")
	}
}

// covers: MA-45, R14, S14
// The cron preview carries a plain-language gloss above the raw schedule line
// and the note that a failed run surfaces only via `status` or cron's MAILTO; a
// -mode full job adds the note that every run re-exports everything, while an
// incremental job does not (friction #15).
func TestCronPreviewGloss(t *testing.T) {
	daily, err := cronPreview(sampleSpec(Daily, "02:00"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(daily, "# daily at 02:00") {
		t.Errorf("daily preview lacks the gloss:\n%s", daily)
	}
	if !strings.Contains(daily, "0 2 * * * ") {
		t.Errorf("daily preview lacks the raw cron line:\n%s", daily)
	}
	if !strings.Contains(daily, "mailarchive status") || !strings.Contains(daily, "MAILTO") {
		t.Errorf("daily preview lacks the failed-run note:\n%s", daily)
	}
	if strings.Contains(daily, "re-exports everything") {
		t.Errorf("an incremental preview must not warn about re-export:\n%s", daily)
	}

	weekly, err := cronPreview(sampleSpec(Weekly, "03:30"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(weekly, "# weekly on Sunday at 03:30") {
		t.Errorf("weekly preview gloss wrong:\n%s", weekly)
	}

	full := sampleSpec(Daily, "02:00")
	full.Args = []string{"-out", "/data/backup", "-mode", "full", "-log", "/data/backup/x.log"}
	text, err := cronPreview(full)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "re-exports everything") {
		t.Errorf("a full-mode preview lacks the re-export note:\n%s", text)
	}
}
