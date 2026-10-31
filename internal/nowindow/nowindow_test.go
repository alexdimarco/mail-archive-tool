package nowindow

import (
	"os/exec"
	"reflect"
	"runtime"
	"testing"
)

// covers: MA-951
// Hide hands back the same command with its program, arguments and input as
// they were; off Windows it changes nothing at all (Windows' flag is checked on
// the lab VM, nowindow_windows_test.go).
func TestHideKeepsTheCommand(t *testing.T) {
	cmd := exec.Command("schtasks", "/Query", "/TN", "MailArchive Backup")
	path, args := cmd.Path, append([]string(nil), cmd.Args...)
	got := Hide(cmd)
	if got != cmd {
		t.Fatal("Hide returned another command")
	}
	if got.Path != path || !reflect.DeepEqual(got.Args, args) {
		t.Errorf("Hide changed the program or arguments: %q %q, want %q %q", got.Path, got.Args, path, args)
	}
	if runtime.GOOS != "windows" && got.SysProcAttr != nil {
		t.Errorf("off Windows Hide set start attributes: %+v", got.SysProcAttr)
	}
}
