//go:build windows

package nowindow

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"
)

// helperEnv puts the test binary in one of its helper roles (TestMain):
// "report" says whether it has a console window; "parent" plays MailArchive
// Desktop, a program with no console, and starts two "report" children.
const helperEnv = "MAILARCHIVE_NOWINDOW_HELPER"

func TestMain(m *testing.M) {
	switch os.Getenv(helperEnv) {
	case "report":
		fmt.Print(consoleWindowState())
		os.Exit(0)
	case "parent":
		os.Exit(playDesktop())
	}
	os.Exit(m.Run())
}

// consoleWindowState is "window" when this process's console has a window and
// "none" when it has no console window.
func consoleWindowState() string {
	h, _, _ := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetConsoleWindow").Call()
	if h != 0 {
		return "window"
	}
	return "none"
}

// playDesktop is the "parent" role: started with no console (as the Desktop,
// built -H=windowsgui, has none), it starts this binary as a console program
// twice, once as a plain exec.Command and once through Hide, and prints what
// each child reported: "plain=STATE hidden=STATE".
func playDesktop() int {
	self, err := os.Executable()
	if err != nil {
		fmt.Print(err)
		return 1
	}
	run := func(cmd *exec.Cmd) string {
		cmd.Env = append(os.Environ(), helperEnv+"=report")
		out, err := cmd.Output()
		if err != nil {
			return "error:" + err.Error()
		}
		return string(out)
	}
	fmt.Printf("plain=%s hidden=%s", run(exec.Command(self)), run(Hide(exec.Command(self))))
	return 0
}

// covers: MA-951
// Hide asks Windows for no console window and nothing else: a plain command has
// no start attributes (the positive twin: the flag comes from Hide), Hide adds
// CREATE_NO_WINDOW, keeps a flag already asked for, and never sets HideWindow
// (which would hide a windowed program such as the browser opener).
func TestHideSetsCreateNoWindow(t *testing.T) {
	plain := exec.Command("schtasks", "/Query")
	if plain.SysProcAttr != nil {
		t.Fatalf("a plain command already has start attributes: %+v", plain.SysProcAttr)
	}
	got := Hide(exec.Command("schtasks", "/Query"))
	if got.SysProcAttr == nil || got.SysProcAttr.CreationFlags != windows.CREATE_NO_WINDOW {
		t.Fatalf("Hide's start attributes %+v, want only CREATE_NO_WINDOW (%#x)", got.SysProcAttr, windows.CREATE_NO_WINDOW)
	}
	if got.SysProcAttr.HideWindow {
		t.Error("Hide set HideWindow, which would hide a windowed program it starts")
	}
	kept := exec.Command("clip")
	kept.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
	if f := Hide(kept).SysProcAttr.CreationFlags; f != windows.CREATE_NEW_PROCESS_GROUP|windows.CREATE_NO_WINDOW {
		t.Errorf("Hide over an earlier flag gave %#x, want %#x", f, windows.CREATE_NEW_PROCESS_GROUP|windows.CREATE_NO_WINDOW)
	}
}

// covers: MA-951
// A real start, as MailArchive Desktop makes it: a parent with no console
// (DETACHED_PROCESS, like a -H=windowsgui program) starts a console program. The
// plain start gets a console with a window — the window that flashed on screen
// (positive twin: the check can see a window) — and the start through Hide gets
// none, with its output still read back. Then the real schtasks.exe, through
// Hide, still answers with its exit code and its words.
func TestHiddenChildHasNoConsoleWindow(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	parent := exec.Command(self)
	parent.Env = append(os.Environ(), helperEnv+"=parent")
	parent.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.DETACHED_PROCESS}
	out, err := parent.Output()
	if err != nil {
		t.Fatalf("the windowless parent failed: %v (%s)", err, out)
	}
	if got, want := string(out), "plain=window hidden=none"; got != want {
		t.Fatalf("console windows of the children: %q, want %q", got, want)
	}

	name := "MailArchiveNoSuchTask-" + strconv.Itoa(os.Getpid())
	sout, serr := Hide(exec.Command("schtasks", "/Query", "/TN", name)).CombinedOutput()
	var ee *exec.ExitError
	if !errors.As(serr, &ee) || ee.ExitCode() == 0 {
		t.Errorf("schtasks /Query of a missing task through Hide: err %v, want a non-zero exit", serr)
	}
	if strings.TrimSpace(string(sout)) == "" {
		t.Error("schtasks through Hide answered with no words; its output must still be read")
	}
}
