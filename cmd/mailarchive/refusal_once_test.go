//go:build linux || darwin

package main

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// covers: MA-636, R12, S59
// A refusal the desktop already showed reaches each channel once, at process
// level. Under the one program, `mailarchive desktop -addr <a held port>` exits 1
// with §4.5.1's portBusyText on stderr exactly once and no "mailarchive: " line:
// refuse() shows it (stderr + a dialog), and the dispatcher suppresses its own
// reprint through desktop.RefusalShown (§4.1 rule 4). Per-account state is
// isolated and the browser suppressed, so the only thing exercised is the refusal.
// The dialog seam is a recording fake on PATH (zenity/qarma/matedialog on Linux,
// osascript on macOS — both PATH-resolved by ncruces/zenity) so the dialog does
// not block headless; on Linux the fake zenity is asserted to receive the text
// once, the form the catalog row names.
func TestMA636_ShownRefusalReachesEachChannelOnce(t *testing.T) {
	dialogDir := t.TempDir()
	rec := filepath.Join(t.TempDir(), "dialog.log")
	for _, name := range []string{"zenity", "qarma", "matedialog", "osascript"} {
		script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + rec + "\nexit 0\n"
		if err := os.WriteFile(filepath.Join(dialogDir, name), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	addr := busy.Addr().String()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, testBin, "desktop", "-addr", addr, "-no-browser")
	// Isolated per-account state, with the fake dialog dir ahead of the real PATH
	// (no duplicate PATH key, so the child resolves zenity/osascript to the fakes).
	env := isolatedEnv(t)
	kept := env[:0]
	for _, e := range env {
		if !strings.HasPrefix(e, "PATH=") {
			kept = append(kept, e)
		}
	}
	cmd.Env = append(kept, "PATH="+dialogDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	var stderr strings.Builder
	cmd.Stderr = &stderr
	runErr := cmd.Run()

	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("`mailarchive desktop -addr <held>` did not exit; it must refuse fast. stderr:\n%s", stderr.String())
	}
	code := 0
	if ee, ok := runErr.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if runErr != nil {
		t.Fatalf("running the desktop verb: %v", runErr)
	}
	if code != 1 {
		t.Errorf("exit code = %d, want 1 (a refusal)", code)
	}
	s := stderr.String()
	if n := strings.Count(s, "is in use by another program"); n != 1 {
		t.Errorf("portBusyText appears %d time(s) on stderr, want exactly once:\n%s", n, s)
	}
	if !strings.Contains(s, "mailarchive desktop: -addr "+addr) {
		t.Errorf("stderr does not carry §4.5.1's portBusyText for the held addr:\n%s", s)
	}
	if strings.Contains(s, "mailarchive: ") {
		t.Errorf("the dispatcher reprinted an already-shown refusal with a \"mailarchive: \" line (it must suppress it, MA-636):\n%s", s)
	}

	dialog, _ := os.ReadFile(rec)
	if len(strings.TrimSpace(string(dialog))) == 0 {
		t.Errorf("the refusal reached no dialog seam (the fake dialog program was never invoked)")
	}
	if runtime.GOOS == "linux" {
		// The catalog row's named form: the fake zenity receives the text once.
		if n := strings.Count(string(dialog), "is in use by another program"); n != 1 {
			t.Errorf("the fake zenity received the refusal text %d time(s), want once:\n%s", n, dialog)
		}
	}
}
