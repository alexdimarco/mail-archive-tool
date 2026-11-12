package wizard

import (
	"runtime"

	"github.com/ncruces/zenity"
)

// messageDialog and listDialog are the dialogs textThenList opens; a test replaces them with
// recorders.
var (
	messageDialog = zenity.Info
	listDialog    = zenity.List
)

// healthListPrompt is the launch health view's list prompt on Linux and the BSDs, where the health
// text itself is shown in the message before it (textThenList). Fixed, plain words: no & < > " ' \.
const healthListPrompt = "Backup health — what would you like to do next?"

// textThenList shows text, which may hold words a person or a mail server chose (account and folder
// names, paths, commands to copy), and then asks for one of choices (MA-1286, amended for the C3
// name confirm's R2-01). prompt is fixed, plain words, the list's text where text cannot be.
func textThenList(text, prompt string, choices []string, opts ...zenity.Option) (string, error) {
	return textThenListOn(runtime.GOOS, text, prompt, choices, opts...)
}

// textThenListOn is textThenList on goos.
//
// On macOS (JXA) and Windows (a native dialog) a list shows its text as written, so text is the
// list's own text, as before.
//
// On Linux and the BSDs the ncruces/zenity module (v0.10.12) runs the first of qarma, zenity and
// matedialog it finds (internal/zenutil/run_unix.go), and no one text shows as written in all three
// lists: zenity and matedialog set the list's text with gtk_label_set_markup(g_strcompress(text))
// with no --no-markup, so `&` or `<` breaks it and a backslash then n becomes a line break, while
// qarma shows the same text as plain text, so any escaping shows up literally. So text is shown
// first in a message dialog, which the module always opens with --no-markup (msg_unix.go): plain
// text, exactly as written, in all three, so a copied command works. The list then carries only
// prompt. A dismissed message ends the step as a dismissed list does: its error comes back and no
// list is shown.
func textThenListOn(goos, text, prompt string, choices []string, opts ...zenity.Option) (string, error) {
	switch goos {
	case "darwin", "windows":
		return listDialog(text, choices, opts...)
	}
	msgOpts := append(append([]zenity.Option(nil), opts...), zenity.OKLabel("Next"))
	if err := messageDialog(text, msgOpts...); err != nil {
		return "", err
	}
	return listDialog(prompt, choices, opts...)
}
