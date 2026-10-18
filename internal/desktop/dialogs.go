package desktop

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/ncruces/zenity"
)

// dialogTitle is the title of every dialog the desktop shows.
const dialogTitle = "MailArchive Desktop"

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

// showLink shows a fresh one-time link in a dialog with a Copy button, for a
// browser that did not open or a launch that asked for a link [CC20]. The link is
// in the dialog and nowhere else: not on stderr, which may be a log.
func showLink(link string) {
	fmt.Fprintln(os.Stderr, "mailarchive desktop: a dialog shows a link to open the dashboard in your browser")
	_, err := zenity.Entry("Your browser did not open. Copy this link into your browser:",
		zenity.Title(dialogTitle), zenity.EntryText(link), zenity.OKLabel("Copy"), zenity.CancelLabel("Close"))
	if err == nil {
		copyToClipboard(link)
	}
}

// copyToClipboard puts text on the clipboard through the OS's own tool, best
// effort: the link stays selectable in the dialog when none is there.
func copyToClipboard(text string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("clip")
	case "darwin":
		cmd = exec.Command("pbcopy")
	default:
		for _, c := range [][]string{{"wl-copy"}, {"xclip", "-selection", "clipboard"}, {"xsel", "--clipboard", "--input"}} {
			if _, err := exec.LookPath(c[0]); err == nil {
				cmd = exec.Command(c[0], c[1:]...)
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
