package desktop

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"

	"github.com/ncruces/zenity"
)

// zenityChoose shows the launcher choice (design-menubar §6.1): the same native list
// the wizard's first step uses, so no new UI technology. Cancel (or closing the
// window) is pickNone; the Quit item (MB-14) is pickQuit. OK needs a picked item, so
// OK with nothing picked is never read as Cancel (the S1 friction review, F11); the
// window closes when the instance stops (ctx), so no dialog is left on the screen
// with nothing behind it.
func zenityChoose(ctx context.Context, v choiceView) (string, error) {
	opts := []zenity.Option{zenity.Title(choiceTitle), zenity.DefaultItems(v.DefaultItem), zenity.DisallowEmpty(), zenity.Context(ctx)}
	items := []string{choiceWizardItem, choiceDesktopItem}
	if v.Quit {
		// Quit is a third item, not an extra button: macOS's list has no extra button
		// (zenity: "unsupported option: extra button", the S1 Mac lab, 2026-11-13).
		items = append(items, choiceQuitItem)
	}
	pick, err := textThenListOn(runtime.GOOS, v.Text(), choiceText, items, opts...)
	switch {
	case errors.Is(err, zenity.ErrCanceled):
		return pickNone, nil
	case err != nil:
		return pickNone, err
	}
	switch pick {
	case choiceWizardItem:
		return pickWizard, nil
	case choiceDesktopItem:
		return pickDesktop, nil
	case choiceQuitItem:
		return pickQuit, nil
	}
	return pickNone, nil
}

// textThenListOn is the wizard's rule for a list whose text can vary (MA-1286): on macOS
// and Windows the list shows its text as written; on Linux and the BSDs zenity and
// matedialog read a list's text as markup and qarma does not, so a text other than the
// fixed prompt is shown first in a message dialog (plain text in all three) and the
// list carries only the prompt. A dismissed message ends the choice as a dismissed list
// does.
func textThenListOn(goos, text, prompt string, choices []string, opts ...zenity.Option) (string, error) {
	switch goos {
	case "darwin", "windows":
		return zenity.List(text, choices, opts...)
	}
	if text != prompt {
		msgOpts := append(append([]zenity.Option(nil), opts...), zenity.OKLabel("Next"))
		if err := zenity.Info(text, msgOpts...); err != nil {
			return "", err
		}
	}
	return zenity.List(prompt, choices, opts...)
}

// zenityInfo shows one line from the running MailArchive.
func zenityInfo(text string) { _ = zenity.Info(text, zenity.Title(choiceTitle)) }

// zenityAskDesktop asks a yes-or-no question with OK and Cancel (a message box: it can
// open beside the wizard's list); true is OK.
func zenityAskDesktop(q string) bool {
	return zenity.Question(q, zenity.Title(choiceTitle)) == nil
}

// zenityQuitQuestion asks the Quit question (Q4): Keep running is the default and
// Cancel's answer; true means Quit.
func zenityQuitQuestion(q string) bool {
	err := zenity.Question(q, zenity.Title(choiceTitle), zenity.OKLabel(quitLabel), zenity.CancelLabel(keepRunningLabel), zenity.DefaultCancel())
	return err == nil
}

// dialogsPossible reports whether this process can show its native dialogs: on Linux
// and the BSDs they need a zenity-compatible tool on PATH and a desktop session (the
// wizard's own preflight); macOS (osascript) and Windows always can.
func dialogsPossible() bool {
	switch runtime.GOOS {
	case "darwin", "windows":
		return true
	}
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		return false
	}
	for _, name := range []string{"zenity", "qarma", "matedialog"} {
		if _, err := exec.LookPath(name); err == nil {
			return true
		}
	}
	return false
}
