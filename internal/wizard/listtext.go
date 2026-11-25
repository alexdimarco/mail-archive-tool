package wizard

import (
	"runtime"

	"github.com/ncruces/zenity"
)

// messageDialog and listDialog are the dialogs textThenList opens; a test replaces them with
// recorders. questionDialog and folderDialog are the question and the folder chooser a set's steps
// open (docs/design-per-mailbox-archives.md §3, §6, §8), replaced by recorders in their tests too.
// entryDialog and errorDialog are the date window's question and its note (askSince), and
// progressDialog the progress window of a set's exports and of the download wait (§9), so a test of
// a set's whole flow opens no real window. The one-archive export (runExport) opens its progress
// window, its cancel note and its summary through progressDialog, messageDialog and questionDialog
// too (round D), so a test of its run log opens none either.
var (
	messageDialog  = zenity.Info
	listDialog     = zenity.List
	questionDialog = zenity.Question
	folderDialog   = zenity.SelectFile
	entryDialog    = zenity.Entry
	errorDialog    = zenity.Error
	// progressDialog opens a pulsing progress window titled title.
	progressDialog = func(title string) (zenity.ProgressDialog, error) {
		return zenity.Progress(zenity.Title(title), zenity.Pulsate())
	}
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
