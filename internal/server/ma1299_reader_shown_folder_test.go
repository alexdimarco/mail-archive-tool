package server

import (
	"regexp"
	"strings"
	"testing"
)

// covers: MA-1299, R45
// The reader names Gmail's folders as Gmail does, "All Mail" and "Sent Mail", in a hit's "In:"
// line, its folder pill and the Folder filter's choices (the B3 friction review's F11); the
// filter's value stays the stored folder, so filtering is unchanged. Positive twin first: the rule
// drops only a leading "[Gmail]/" or "[Google Mail]/", which the Go side (state.ShownFolder) shares.
func TestTheReaderNamesGmailFoldersByTheirOwnNames(t *testing.T) {
	def := `function shownFolder(f){return (f||'').replace(/^\[(Gmail|Google Mail)\]\//,'');}`
	if !strings.Contains(appJS, def) {
		t.Fatal("the reader has no shownFolder rule")
	}
	re := regexp.MustCompile(`^\[(Gmail|Google Mail)\]/`)
	for in, want := range map[string]string{"INBOX": "INBOX", "Work/[Gmail]/x": "Work/[Gmail]/x", "[Gmail]/All Mail": "All Mail", "[Google Mail]/Sent Mail": "Sent Mail"} {
		if got := re.ReplaceAllString(in, ""); got != want {
			t.Fatalf("the rule names %q as %q, want %q", in, got, want)
		}
	}
	for _, use := range []string{
		`'In: '+[m.folder].concat(m.also).map(shownFolder).map(esc).join(', ')`,
		`:esc(shownFolder(m.folder))`,
		`o.value=f.folder;o.textContent=shownFolder(f.folder)+' ('+f.count+')'`,
	} {
		if !strings.Contains(appJS, use) {
			t.Fatalf("the reader does not use the rule in %q", use)
		}
	}
}
