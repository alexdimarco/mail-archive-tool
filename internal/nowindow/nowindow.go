// Package nowindow starts the computer's own console programs without a console
// window.
//
// On Windows, MailArchive Desktop and the scheduled backup run as
// mailarchivew.exe, a program built with no console (-H=windowsgui). When a
// program with no console starts a console program — schtasks.exe, which the
// Desktop asks about each archive's scheduled backup as it starts, or clip.exe —
// Windows gives the child a console of its own, and that console opens a window:
// the Command Prompt or Windows Terminal window that flashes for a moment. Hide
// asks Windows for no window (the CREATE_NO_WINDOW creation flag) and changes
// nothing else: the program, its arguments, its input and its output stay as
// they were.
//
// Hide sets only that flag, never HideWindow: HideWindow hands the program a
// "hidden" show state, which a program with windows of its own would pass on (the
// browser opener, rundll32, would open the browser hidden), while Windows ignores
// CREATE_NO_WINDOW for a program that is not a console program. So the browser
// opener and the folder opener (rundll32, explorer.exe) go through Hide too and
// show as before.
//
// Every exec.Command and exec.CommandContext in the program is wrapped by Hide
// where it is written (MA-951). The one process start that is not is
// Thunderbird's own (internal/presync, MA-923), a CreateProcess call that asks
// for Thunderbird's minimized-not-active window itself.
package nowindow

import "os/exec"

// Hide marks cmd to start with no console window on Windows and returns it; on
// other systems it returns cmd unchanged. Wrap the exec.Command expression
// itself, so the start is visibly hidden where it is written:
//
//	out, err := nowindow.Hide(exec.Command("schtasks", args...)).CombinedOutput()
func Hide(cmd *exec.Cmd) *exec.Cmd {
	hide(cmd)
	return cmd
}
