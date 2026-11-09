package wizard

import (
	"runtime"
	"strings"
)

// listText is the text of a zenity list dialog as zenity is given it (MA-1286, the C3 account-name
// pass's N-01). Every zenity.List call passes its text through it (a test scans for that).
func listText(s string) string { return listTextFor(runtime.GOOS, s) }

// listTextFor is listText on goos. On Linux and the BSDs, zenity is the GNOME zenity program, whose
// list dialog sets its text with gtk_label_set_markup(g_strcompress(text)) and has no
// --no-markup (src/tree.c; ncruces/zenity v0.10.12 passes the text as is, list_unix.go:8). So the
// text is made to show as written there: every backslash doubled, so g_strcompress gives each
// back and no "\n" or "\t" in a name or path becomes a line break or a tab, then &, <, >, " and '
// escaped as Pango markup. It covers account names, folder names and paths alike. macOS (JXA) and
// Windows (a native dialog) read no markup, so the text is passed unchanged there.
func listTextFor(goos, s string) string {
	switch goos {
	case "darwin", "windows":
		return s
	}
	return markupEscaper.Replace(s)
}

// markupEscaper is g_markup_escape_text's escapes, after the backslash for g_strcompress. One
// pass: each character is replaced once, so the escapes are never escaped again.
var markupEscaper = strings.NewReplacer(`\`, `\\`, "&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#39;")
