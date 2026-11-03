package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/desktop"
	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/seal"
)

// g3Archive makes dir a folder holding an archive's manifest, as the delete's refusals need.
func g3Archive(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".mailarchive-manifest.json"), []byte(`{"version":1,"records":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// g3Started appends a delete's started line for folder to the deletion log at logPath, as a delete
// that stopped before step 6 leaves it (no marker in the folder), and checks the log says so.
func g3Started(t *testing.T, logPath, folder, run string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		t.Fatal(err)
	}
	line, err := json.Marshal(map[string]any{"at": "2026-11-03T10:00:00Z", "event": "started", "run": run, "folder": folder, "row": folder, "kind": "plain"})
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if u, err := archdelete.UnfinishedDelete(logPath, folder, ""); err != nil || u == nil {
		t.Fatalf("precondition: %s says the delete of %s did not finish: %v %v", logPath, folder, u, err)
	}
}

// g3Config is the config file of a MailArchive Desktop started with -config (the file itself is
// not made: that Desktop keeps its list beside it before writing it), and the deletion log the
// delete verbs given that -config write (desktop.NewCommandLineDeleteAt), which the three verbs must
// read.
func g3Config(t *testing.T) (cfg, cfgLog string) {
	t.Helper()
	cfg = filepath.Join(t.TempDir(), "Desktop Two", "desktop.json")
	if err := os.MkdirAll(filepath.Dir(cfg), 0o700); err != nil {
		t.Fatal(err)
	}
	return cfg, desktop.NewCommandLineDeleteAt(cfg, nil, nil, nil).LogPath()
}

// covers: MA-1060, R36
// `mailarchive pause` and `resume` take -config FILE and then ask the deletion log beside FILE —
// the one a MailArchive Desktop started with -config FILE, and `delete-archive -config FILE`,
// write — and never MailArchive's own as well (G3, design-archive-delete §16, the owner's ruling of
// 2 November 2026). A delete in the -config log refuses both in MA-1015's words, and the terminal's
// Delete again carries the same -config; a delete only MailArchive's own log knows does not refuse
// them with -config, and still does without it. pause's line names resume with the same -config.
// Positive twins first: another folder is paused and resumed with -config.
func TestMA1060_PauseAndResumeAskTheConfigFoldersLog(t *testing.T) {
	ownLog := ma1044PackageLog(t)
	cfg, cfgLog := g3Config(t)
	half := g3Archive(t, filepath.Join(t.TempDir(), "Begun from Desktop Two"))
	ownHalf := g3Archive(t, filepath.Join(t.TempDir(), "Begun from the usual Desktop"))
	other := g3Archive(t, filepath.Join(t.TempDir(), "Other"))
	g3Started(t, cfgLog, half, "g3cfg1060")
	g3Started(t, ownLog, ownHalf, "g3own1060")
	cfgArg := encmark.CommandPath(cfg)

	// Positive twins.
	code, stdout, stderr := runCLIOut(t, "pause", "-config", cfg, "-out", other)
	if code != 0 || !seal.IsSealed(other) {
		t.Fatalf("positive twin: pause -config of another folder (%d): %s%s", code, stdout, stderr)
	}
	if want := "Run `mailarchive resume -config " + cfgArg + " -out " + other + "` to undo."; !strings.Contains(stdout, want) {
		t.Errorf("pause's line does not name resume with the same -config: want %q in\n%s", want, stdout)
	}
	if code, stdout, stderr := runCLIOut(t, "resume", "-config", cfg, "-out", other); code != 0 || seal.IsSealed(other) {
		t.Fatalf("positive twin: resume -config of another folder (%d): %s%s", code, stdout, stderr)
	}
	// Without -config, pause's line is as it was.
	code, stdout, _ = runCLIOut(t, "pause", "-out", other)
	if code != 0 || !strings.Contains(stdout, "Run `mailarchive resume -out "+other+"` to undo.") || strings.Contains(stdout, "-config") {
		t.Fatalf("pause without -config (%d):\n%s", code, stdout)
	}
	if code, _, stderr := runCLIOut(t, "resume", "-out", other); code != 0 {
		t.Fatalf("resume without -config (%d): %s", code, stderr)
	}

	hint := "At this terminal, Delete again is: mailarchive delete-archive -config " + cfgArg + " -out " + encmark.CommandPath(half)
	noBareHint := assure.Forbid("Delete again is: mailarchive delete-archive -out")

	// A delete in the -config log refuses pause with -config.
	code, stdout, stderr = runCLIOut(t, "pause", "-config", cfg, "-out", half)
	assure.Refused(t, code, stderr, assure.Code(1),
		assure.Names("Delete not finished: click Delete again. This archive's delete did not finish, so it was not paused; nothing was changed. (Folder: "+half+")", hint),
		noBareHint, assure.NoSideEffect(func() bool { return !seal.IsSealed(half) && stdout == "" }))

	// Never both: without -config, MailArchive's own log is asked, which has no line for it.
	if code, stdout, stderr := runCLIOut(t, "pause", "-out", half); code != 0 || !seal.IsSealed(half) {
		t.Fatalf("pause without -config asked the -config log (%d): %s%s", code, stdout, stderr)
	}
	code, stdout, stderr = runCLIOut(t, "resume", "-config", cfg, "-out", half)
	assure.Refused(t, code, stderr, assure.Code(1),
		assure.Names("Delete not finished: click Delete again. This archive's delete did not finish, so it was not resumed; nothing was changed. (Folder: "+half+")", hint),
		noBareHint, assure.NoSideEffect(func() bool { return seal.IsSealed(half) && stdout == "" }))

	// Never both, the other way: a delete only MailArchive's own log knows refuses without -config
	// and not with it.
	code, _, stderr = runCLIOut(t, "pause", "-out", ownHalf)
	assure.Refused(t, code, stderr, assure.Code(1),
		assure.Names("so it was not paused; nothing was changed.", "At this terminal, Delete again is: mailarchive delete-archive -out "+encmark.CommandPath(ownHalf)),
		assure.Forbid("-config"), assure.NoSideEffect(func() bool { return !seal.IsSealed(ownHalf) }))
	if code, stdout, stderr := runCLIOut(t, "pause", "-config", cfg, "-out", ownHalf); code != 0 || !seal.IsSealed(ownHalf) {
		t.Fatalf("pause -config asked MailArchive's own log too (%d): %s%s", code, stdout, stderr)
	}
	if code, stdout, stderr := runCLIOut(t, "resume", "-config", cfg, "-out", ownHalf); code != 0 || seal.IsSealed(ownHalf) {
		t.Fatalf("resume -config asked MailArchive's own log too (%d): %s%s", code, stdout, stderr)
	}

	// The help names -config.
	for _, verb := range []string{"pause", "resume"} {
		_, _, stderr := runCLIOut(t, verb, "-h")
		if !strings.Contains(stderr, "-config") || !strings.Contains(stderr, "to ask the deletion log beside it whether a delete of the folder did not finish (default: MailArchive's own)") {
			t.Errorf("%s -h does not name -config:\n%s", verb, stderr)
		}
	}
}
