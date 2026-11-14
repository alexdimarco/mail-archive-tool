package desktop

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/ncruces/zenity"

	"mail-archive-tool/internal/nowindow"
)

// dialogTitle is the title of every dialog the desktop shows.
const dialogTitle = "MailArchive Desktop"

// noLinkDialogText is the log line when the browser did not open and no dialog can show
// its link (no display or dialog tool, MB-9).
const noLinkDialogText = "mailarchive desktop: the browser did not open, and no dialog can show its link here (no display or dialog tool). Open MailArchive from your desktop session."

// showRefusal shows a refusal where the user will see it. The binary is a
// -H=windowsgui build with no console, so a refusal printed to stderr alone is
// invisible: it also goes to a native dialog (MessageBox on Windows, an osascript
// dialog on macOS, zenity elsewhere), in the words of design §7.6 [CC20].
func showRefusal(text string) {
	fmt.Fprintln(os.Stderr, text)
	_ = zenity.Error(text, zenity.Title(dialogTitle))
}

// showNotice shows an informational notice (a handoff to another version) on
// stderr and in a dialog; unlike a refusal, the launcher exits 0 (§4.5.3).
func showNotice(text string) {
	fmt.Fprintln(os.Stderr, text)
	_ = zenity.Info(text, zenity.Title(dialogTitle))
}

// tellNotice shows a hand-off's outcome (the choice or the wizard already open, another
// or an unknown sign-in, a copy closing) on stderr and in a dialog headed "MailArchive",
// the name of the window it points to (the S1 friction review, F6).
func tellNotice(text string) {
	fmt.Fprintln(os.Stderr, text)
	_ = zenity.Info(text, zenity.Title(choiceTitle))
}

// showLink shows a fresh one-time link in a dialog with a Copy button, for a
// browser that did not open or a launch that asked for a link [CC20]. The link is
// in the dialog and nowhere else: not on stderr, which may be a log. Where no dialog
// can show, the log says so instead of naming one (the S1 friction review, F14).
func showLink(link string) {
	if !canShowDialogs() {
		fmt.Fprintln(os.Stderr, noLinkDialogText)
		return
	}
	fmt.Fprintln(os.Stderr, "mailarchive desktop: a dialog shows a link to open the dashboard in your browser")
	_, err := zenity.Entry("Your browser did not open. Copy this link into your browser:",
		zenity.Title(dialogTitle), zenity.EntryText(link), zenity.OKLabel("Copy"), zenity.CancelLabel("Close"))
	if err == nil {
		copyToClipboard(link)
	}
}

// copyToClipboard puts text on the clipboard through the OS's own tool, best
// effort: the link stays selectable in the dialog when none is there. clip.exe
// is a console program, so it runs with no window (MA-951).
func copyToClipboard(text string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = nowindow.Hide(exec.Command("clip"))
	case "darwin":
		cmd = nowindow.Hide(exec.Command("pbcopy"))
	default:
		for _, c := range [][]string{{"wl-copy"}, {"xclip", "-selection", "clipboard"}, {"xsel", "--clipboard", "--input"}} {
			if _, err := exec.LookPath(c[0]); err == nil {
				cmd = nowindow.Hide(exec.Command(c[0], c[1:]...))
				break
			}
		}
	}
	if cmd == nil {
		return
	}
	cmd.Stdin = strings.NewReader(text)
	_ = cmd.Run()
}
