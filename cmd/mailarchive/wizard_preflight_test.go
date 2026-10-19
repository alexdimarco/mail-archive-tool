//go:build linux || darwin

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// wizardNoDialogText is the exact §4.4 stderr text; it must match the string in
// internal/wizard (package main cannot import that unexported constant).
const wizardNoDialogText = "mailarchive wizard: the wizard cannot open its windows. Why: it is made of native dialogs, which need the zenity tool and a desktop session. Do this: install the zenity package and run mailarchive wizard again from your desktop, or use the commands in the README (mailarchive -h)."

// covers: MA-639, R12, S59
// The built program's wizard verb refuses when it cannot open a native dialog
// (§4.4), at process level. Linux-only: the preflight refuses only on Linux and
// the BSDs, where the dialogs are zenity; macOS (osascript) and Windows (a native
// MessageBox) always have one, so CI's Ubuntu leg is the gate. The pure decision
// over every (OS, tool, display) case is internal/wizard's
// TestMA639_DialogPreflight (run everywhere). Here: with no dialog tool on PATH
// (a display set) the verb exits 1 with the exact text; with a tool on PATH but
// no display it exits 1 with the exact text AND never invokes the tool
// (assure.Refused — the preflight looks up a file and an env var and executes
// nothing); with both present the first dialog is shown (the tool is invoked),
// the positive twin.
func TestMA639_WizardRefusesWithoutADialog(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the wizard's no-dialog preflight refuses only on Linux/BSD; macOS/Windows always have a dialog (CI Ubuntu is the gate)")
	}

	// wizardRun runs `mailarchive wizard` with a controlled PATH (a recording fake
	// zenity is written when withTool) and DISPLAY (set when withDisplay), and
	// reports the exit code, stderr, and whether the fake zenity was invoked.
	wizardRun := func(t *testing.T, withTool, withDisplay bool) (code int, stderr string, toolInvoked bool) {
		t.Helper()
		pathDir := t.TempDir()
		rec := filepath.Join(t.TempDir(), "zenity.invoked")
		if withTool {
			// Record, then cancel (exit 1): if the wizard proceeds, the first
			// dialog returns ErrCanceled and the run ends cleanly without work.
			script := "#!/bin/sh\necho invoked >> " + rec + "\nexit 1\n"
			if err := os.WriteFile(filepath.Join(pathDir, "zenity"), []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		base := isolatedEnv(t)
		kept := base[:0]
		for _, e := range base {
			if !strings.HasPrefix(e, "PATH=") && !strings.HasPrefix(e, "DISPLAY=") && !strings.HasPrefix(e, "WAYLAND_DISPLAY=") {
				kept = append(kept, e)
			}
		}
		env := append(kept, "PATH="+pathDir)
		if withDisplay {
			env = append(env, "DISPLAY=:0")
		}
		cmd := exec.Command(testBin, "wizard")
		cmd.Env = env
		var errb strings.Builder
		cmd.Stderr = &errb
		err := cmd.Run()
		code = 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatalf("running the wizard verb: %v", err)
		}
		_, statErr := os.Stat(rec)
		return code, errb.String(), statErr == nil
	}

	if code, stderr, _ := wizardRun(t, false, true); code != 1 || !strings.Contains(stderr, wizardNoDialogText) {
		t.Errorf("no tool, display set: code=%d stderr=%q, want exit 1 and the exact §4.4 text", code, stderr)
	}
	if code, stderr, invoked := wizardRun(t, true, false); code != 1 || !strings.Contains(stderr, wizardNoDialogText) || invoked {
		t.Errorf("tool, no display: code=%d invoked=%v stderr=%q, want exit 1, the exact §4.4 text, and the tool NOT invoked", code, invoked, stderr)
	}
	if _, _, invoked := wizardRun(t, true, true); !invoked {
		t.Error("tool and display present: the wizard did not reach its first dialog; the preflight should have proceeded")
	}
}
