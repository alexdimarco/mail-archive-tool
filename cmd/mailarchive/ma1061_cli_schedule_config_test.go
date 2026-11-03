package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/schedule"
)

// covers: MA-1061, R36
// `mailarchive schedule -install` takes -config FILE, in the flat form among its flags and in the
// job form before the "--" (the job form's allow-list gains it, G3-1), and then asks the deletion
// log beside FILE, before and after the install, and never MailArchive's own as well (G3,
// design-archive-delete §16). A delete in the -config log refuses it in MA-1036's words, nothing
// installed and no descriptor written, with the terminal's Delete again carrying the same -config;
// a delete only MailArchive's own log knows refuses it without -config and not with it. Run as the
// real program against stand-in scheduler programs. Positive twins first: another folder is
// installed with -config in both forms.
func TestMA1061_ScheduleInstallAsksTheConfigFoldersLog(t *testing.T) {
	sandboxScheduler(t)
	ownLog, err := archdelete.DefaultLogPath()
	if err != nil {
		t.Fatal(err)
	}
	cfg, cfgLog := g3Config(t)
	half := g3Archive(t, filepath.Join(t.TempDir(), "Begun from Desktop Two"))
	ownHalf := g3Archive(t, filepath.Join(t.TempDir(), "Begun from the usual Desktop"))
	other := g3Archive(t, filepath.Join(t.TempDir(), "Other"))
	flatOther := g3Archive(t, filepath.Join(t.TempDir(), "Other flat"))
	g3Started(t, cfgLog, half, "g3cfg1061")
	g3Started(t, ownLog, ownHalf, "g3own1061")
	installed := func(dir string) bool { return schedule.Query(schedule.DefaultNameFor(dir)) == schedule.Installed }
	described := func(dir string) bool {
		_, err := os.Stat(filepath.Join(dir, schedule.DescriptorName))
		return err == nil
	}

	// Positive twins: the job form with -config before "--", and the flat form.
	if code, stdout, stderr := runCLIOut(t, "schedule", "-config", cfg, "-install", "--", "-out", other, "-auto"); code != 0 || !installed(other) {
		t.Fatalf("positive twin: the job form with -config (%d): %s%s", code, stdout, stderr)
	}
	if code, stdout, stderr := runCLIOut(t, "schedule", "-out", flatOther, "-auto", "-install", "-config", cfg); code != 0 || !installed(flatOther) {
		t.Fatalf("positive twin: the flat form with -config (%d): %s%s", code, stdout, stderr)
	}

	words := "Delete not finished: click Delete again. This archive's delete did not finish, so its schedule was not installed; nothing was changed. (Folder: " + half + ")"
	hint := "At this terminal, Delete again is: mailarchive delete-archive -config " + encmark.CommandPath(cfg) + " -out " + encmark.CommandPath(half)
	nothing := assure.NoSideEffect(func() bool { return !installed(half) && !described(half) })
	for _, args := range [][]string{
		{"schedule", "-config", cfg, "-install", "--", "-out", half, "-auto"},
		{"schedule", "-out", half, "-auto", "-install", "-config", cfg},
	} {
		code, _, stderr := runCLIOut(t, args...)
		assure.Refused(t, code, stderr, assure.Code(1), assure.Names(words, hint),
			assure.Forbid("Delete again is: mailarchive delete-archive -out"), nothing)
	}

	// Never both: a delete only MailArchive's own log knows refuses without -config, not with it.
	code, _, stderr := runCLIOut(t, "schedule", "-install", "--", "-out", ownHalf, "-auto")
	assure.Refused(t, code, stderr, assure.Code(1),
		assure.Names("so its schedule was not installed; nothing was changed.", "At this terminal, Delete again is: mailarchive delete-archive -out "+encmark.CommandPath(ownHalf)),
		assure.Forbid("-config"), assure.NoSideEffect(func() bool { return !installed(ownHalf) && !described(ownHalf) }))
	if code, stdout, stderr := runCLIOut(t, "schedule", "-config", cfg, "-install", "--", "-out", ownHalf, "-auto"); code != 0 || !installed(ownHalf) {
		t.Fatalf("schedule -config asked MailArchive's own log too (%d): %s%s", code, stdout, stderr)
	}

	// The help names -config, and what it does not do.
	_, _, stderr = runCLIOut(t, "schedule", "-h")
	if !strings.Contains(stderr, "-config") || !strings.Contains(stderr, "-install asks the deletion log beside it whether a delete of the folder did not finish (default: MailArchive's own); the scheduled job itself does not take it") {
		t.Errorf("schedule -h does not name -config:\n%s", stderr)
	}
}
