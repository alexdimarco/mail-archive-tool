package desktop

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

// covers: MA-430, R29, R12, S52
// "Open folder" takes no path from the request and reveals only the run's recorded root after
// the identity check: the verified folder is revealed with the reveal argv of the system
// (`open -R` and `explorer.exe /select,` on the README, `xdg-open` on the directory elsewhere),
// a path in the request is ignored, and a run id it does not know, a swapped symlink, a swapped
// `.app` bundle and a different-inode directory are each refused with the opener not invoked.
// The positive twin first.
func TestOpenFolderRevealsOnlyTheVerifiedFolder(t *testing.T) {
	e := newWsEnv(t)
	ids := e.folders()
	run := e.startExport(e.selection(entry(ids["Sent Items"], false)))
	root := e.waitRun()["last"].(map[string]any)["rootName"].(string)
	rootPath := filepath.Join(e.dest, root)

	var revealed [][]string
	e.d.reveal = func(name string, args []string) error {
		revealed = append(revealed, append([]string{name}, args...))
		return nil
	}

	// the argv of each system, asserted through the table the handler uses
	readme := filepath.Join("r", "README.txt")
	for goos, want := range map[string][]string{
		"darwin":  {"open", "-R", readme},
		"windows": {"explorer.exe", "/select," + readme},
		"linux":   {"xdg-open", "r"},
	} {
		name, args := revealArgv(goos, "r")
		if got := append([]string{name}, args...); !reflect.DeepEqual(got, want) {
			t.Errorf("the reveal argv on %s is %q, want %q", goos, got, want)
		}
	}

	// the positive twin: the verified folder is revealed, whatever path the request carries
	name, args := revealArgv(runtime.GOOS, rootPath)
	want := append([]string{name}, args...)
	e.ok("/api/exports/open-folder", map[string]any{"runId": run, "path": e.dir, "dest": e.dir})
	if len(revealed) != 1 || !reflect.DeepEqual(revealed[0], want) {
		t.Fatalf("Open folder revealed %q, want %q", revealed, want)
	}

	refuse := func(what, id string, body map[string]any, names ...string) {
		t.Helper()
		before := len(revealed)
		code, out := e.post("/api/exports/open-folder", body)
		if id != "" {
			refusedBody(t, code, out, id, names...)
		} else if out["ok"] != false {
			t.Errorf("%s: %d %v, want a refusal", what, code, out)
		}
		if len(revealed) != before {
			t.Errorf("%s: the opener was invoked: %q", what, revealed[before:])
		}
	}
	refuse("a run it does not know", "", map[string]any{"runId": "not-a-run"})

	away := rootPath + ".away"
	if err := os.Rename(rootPath, away); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(away, rootPath); err == nil {
		refuse("a swapped symlink", "R-r", map[string]any{"runId": run}, "moved or replaced", "Locate")
		os.Remove(rootPath)
	}
	// a swapped .app bundle, README and all: another directory at the path
	bundle := filepath.Join(rootPath, "Evil.app", "Contents", "MacOS")
	if err := os.MkdirAll(bundle, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(bundle, "payload"), []byte("#!/bin/sh\n"), 0o755)
	if b, err := os.ReadFile(filepath.Join(away, "README.txt")); err == nil {
		os.WriteFile(filepath.Join(rootPath, "README.txt"), b, 0o600)
	}
	refuse("a swapped .app bundle", "R-r", map[string]any{"runId": run}, "moved or replaced", "Locate")
	// a different-inode directory with nothing in it
	os.RemoveAll(rootPath)
	os.Mkdir(rootPath, 0o700)
	refuse("a different-inode directory", "R-r", map[string]any{"runId": run}, "moved or replaced")
}

// covers: MA-444, R29, R4, R12
// Start checks the destination against the archive again, by path, and not only that the folder
// is the one previewed: with the destination itself untouched (the same folder, the same
// identity), the archive location turned into a link to it between the preview and Start is
// refused with R-f and nothing is written into the destination. The positive twin, the same
// preview with the archive where it was, starts.
func TestStartChecksTheArchiveAgainWithTheDestinationUnchanged(t *testing.T) {
	e := newWsEnv(t)
	ids := e.folders()
	sel := e.selection(entry(ids["Sent Items"], false))

	id, _ := e.preview(sel)
	e.ok("/api/exports/start", map[string]any{"previewId": id})
	e.waitRun()

	id, _ = e.preview(sel)
	if err := os.RemoveAll(e.out); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(e.dest, e.out); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	before := treeState(t, e.dest)
	code, out := e.post("/api/exports/start", map[string]any{"previewId": id})
	refusedBody(t, code, out, "R-f", "inside your archive", "Choose a folder outside")
	if got := treeState(t, e.dest); !reflect.DeepEqual(got, before) {
		t.Errorf("a refused start wrote into the destination: %v", diffKeys(before, got))
	}
	if e.d.exportLive() {
		t.Error("a refused start left an export running")
	}
}
