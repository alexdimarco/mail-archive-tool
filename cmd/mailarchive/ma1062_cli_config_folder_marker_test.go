package main

import (
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/delmark"
	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/seal"
)

// covers: MA-1062, R36
// A -config whose folder does not exist is refused by `pause`, `resume` and every `schedule` form
// (-install in both forms, -remove, the preview) before anything is done, in the delete verbs'
// words (MA-1024), so a mistyped -config never reads a log no Desktop writes (G3,
// design-archive-delete §16.2). Run as the real program against stand-in scheduler programs.
// Positive twins first: the same commands with a -config whose folder exists act.
func TestMA1062_AConfigWhoseFolderIsMissingIsRefused(t *testing.T) {
	sandboxScheduler(t)
	cfg, _ := g3Config(t)
	missing := filepath.Join(t.TempDir(), "No such folder", "desktop.json")
	dir := g3Archive(t, filepath.Join(t.TempDir(), "An archive"))
	installed := func() bool { return schedule.Query(schedule.DefaultNameFor(dir)) == schedule.Installed }

	// Positive twins.
	if code, stdout, stderr := runCLIOut(t, "schedule", "-config", cfg, "--", "-out", dir, "-auto"); code != 0 || installed() {
		t.Fatalf("positive twin: the preview with -config (%d): %s%s", code, stdout, stderr)
	}
	if code, stdout, stderr := runCLIOut(t, "schedule", "-config", cfg, "-install", "--", "-out", dir, "-auto"); code != 0 || !installed() {
		t.Fatalf("positive twin: -install with -config (%d): %s%s", code, stdout, stderr)
	}
	if code, stdout, stderr := runCLIOut(t, "schedule", "-config", cfg, "-out", dir, "-remove"); code != 0 || installed() {
		t.Fatalf("positive twin: -remove with -config (%d): %s%s", code, stdout, stderr)
	}
	// Pause and resume on a folder with no schedule (a resume that puts a schedule back asks
	// whether the program's folder is a temporary one, a desktop question).
	plain := g3Archive(t, filepath.Join(t.TempDir(), "No schedule"))
	if code, stdout, stderr := runCLIOut(t, "pause", "-config", cfg, "-out", plain); code != 0 || !seal.IsSealed(plain) {
		t.Fatalf("positive twin: pause with -config (%d): %s%s", code, stdout, stderr)
	}
	if code, stdout, stderr := runCLIOut(t, "resume", "-config", cfg, "-out", plain); code != 0 || seal.IsSealed(plain) {
		t.Fatalf("positive twin: resume with -config (%d): %s%s", code, stdout, stderr)
	}

	words := assure.Names("There are no MailArchive Desktop settings beside " + encmark.CommandPath(missing) + ": its folder " + encmark.CommandPath(filepath.Dir(missing)) + " does not exist, so nothing was changed. Give -config the config file MailArchive Desktop was started with.")
	descBefore, _ := os.ReadFile(filepath.Join(dir, schedule.DescriptorName))
	unchanged := func(sealed, live bool) assure.RefuseOption {
		return assure.NoSideEffect(func() bool {
			desc, _ := os.ReadFile(filepath.Join(dir, schedule.DescriptorName))
			_, err := os.Stat(filepath.Dir(missing))
			return seal.IsSealed(dir) == sealed && installed() == live && string(desc) == string(descBefore) && os.IsNotExist(err)
		})
	}

	for _, args := range [][]string{
		{"pause", "-config", missing, "-out", dir},
		{"schedule", "-config", missing, "-install", "--", "-out", dir, "-auto"},
		{"schedule", "-out", dir, "-auto", "-install", "-config", missing},
		{"schedule", "-config", missing, "--", "-out", dir, "-auto"},
	} {
		code, _, stderr := runCLIOut(t, args...)
		assure.Refused(t, code, stderr, assure.Code(1), words, unchanged(false, false))
	}

	// With a schedule installed and the archive paused: -remove and resume are refused too.
	if code, stdout, stderr := runCLIOut(t, "schedule", "-install", "--", "-out", dir, "-auto"); code != 0 || !installed() {
		t.Fatalf("the schedule again (%d): %s%s", code, stdout, stderr)
	}
	descBefore, _ = os.ReadFile(filepath.Join(dir, schedule.DescriptorName))
	code, _, stderr := runCLIOut(t, "schedule", "-config", missing, "-out", dir, "-remove")
	assure.Refused(t, code, stderr, assure.Code(1), words, unchanged(false, true))
	if err := seal.Seal(dir, "parked"); err != nil {
		t.Fatal(err)
	}
	code, _, stderr = runCLIOut(t, "resume", "-config", missing, "-out", dir)
	assure.Refused(t, code, stderr, assure.Code(1), words, unchanged(true, true))
}

// covers: MA-1062, R36
// A folder holding the delete marker, whose delete the -config log has no line for, is refused by
// `pause`, `resume` and `schedule -install` with -config, in the marker-only words, and the way to
// finish it carries the same -config (design-archive-delete §16.3, §16.4; DM-11: never "again").
// Positive twin first: the folder beside it, without the marker, is paused with that -config.
func TestMA1062_AMarkerFolderUnderConfigNamesTheConfigsDelete(t *testing.T) {
	sandboxScheduler(t)
	cfg, _ := g3Config(t)
	half := g3Archive(t, filepath.Join(t.TempDir(), "Begun elsewhere"))
	other := g3Archive(t, filepath.Join(t.TempDir(), "Other"))
	ma1048Plant(t, half)

	// Positive twin.
	if code, stdout, stderr := runCLIOut(t, "pause", "-config", cfg, "-out", other); code != 0 || !seal.IsSealed(other) {
		t.Fatalf("positive twin (%d): %s%s", code, stdout, stderr)
	}

	finish := "To finish it at this terminal: mailarchive delete-archive -config " + encmark.CommandPath(cfg) + " -out " + encmark.CommandPath(half)
	forbid := assure.Forbid("Delete again", "mailarchive delete-archive -out")
	unchanged := func(sealed bool) assure.RefuseOption {
		return assure.NoSideEffect(func() bool {
			_, err := os.Stat(filepath.Join(half, schedule.DescriptorName))
			return seal.IsSealed(half) == sealed && os.IsNotExist(err) && schedule.Query(schedule.DefaultNameFor(half)) != schedule.Installed
		})
	}

	code, _, stderr := runCLIOut(t, "pause", "-config", cfg, "-out", half)
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names(delmark.OnlyMarkerWords("it was not paused")+" (Folder: "+half+")", finish), forbid, unchanged(false))

	code, _, stderr = runCLIOut(t, "schedule", "-config", cfg, "-install", "--", "-out", half, "-auto")
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names(delmark.OnlyMarkerWords("its schedule was not installed")+" (Folder: "+half+")", finish), forbid, unchanged(false))

	if err := seal.Seal(half, "parked"); err != nil {
		t.Fatal(err)
	}
	code, _, stderr = runCLIOut(t, "resume", "-config", cfg, "-out", half)
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names(delmark.OnlyMarkerWords("it was not resumed")+" (Folder: "+half+")", finish), forbid, unchanged(true))
}
