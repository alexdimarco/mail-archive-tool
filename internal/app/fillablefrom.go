package app

import (
	"path/filepath"
	"strings"

	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/thunderbird"
)

// fillableFrom is the kind of mail a local run read (a state.From* value), so the words for
// its messages not downloaded in full name a mail app only when the run read a mail app's own
// store (the 1.6.1 friction review's P7, MA-1157): every source Thunderbird's store, every one
// a mail app's store (Thunderbird's among them), or every one a mail file no mail app keeps.
// A mix of a mail app's store and a plain file is "" (not known): the words then blame nobody.
func fillableFrom(sources []string) string {
	kinds := map[string]bool{}
	for _, p := range sources {
		kinds[mailKindOf(p)] = true
	}
	switch {
	case len(kinds) == 1:
		for k := range kinds {
			return k
		}
	case len(kinds) == 2 && kinds[state.FromThunderbird] && kinds[state.FromMailApp]:
		return state.FromMailApp
	}
	return ""
}

// mailKindOf says what kind of mail one source path is: Thunderbird's own store (an account
// under its profile, IMAP or Local Folders), another mail app's own store (an Evolution IMAP
// cache, an Outlook .ost, the Outlook export -outlook rebuilds), else a mail file or folder that
// holds only what was copied into it.
func mailKindOf(p string) string {
	slash := strings.ToLower(filepath.ToSlash(p))
	switch {
	case state.SourceKind("path:"+p) == state.SourceOutlookScratch, strings.HasSuffix(slash, ".ost"):
		return state.FromMailApp
	case thunderbird.IsImapStore(p):
		return state.FromThunderbird
	case strings.Contains(slash, "/evolution/mail/") && strings.Contains(slash, "cache/evolution/"):
		return state.FromMailApp
	}
	if _, ok := thunderbird.FindProfileDir(p); ok {
		return state.FromThunderbird
	}
	return state.FromMailFile
}
