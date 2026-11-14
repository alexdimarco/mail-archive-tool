package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pagesUnder is the text of every message page under dir.
func pagesUnder(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".html") && filepath.Base(p) != "index.html" {
			b, _ := os.ReadFile(p)
			out = append(out, string(b))
		}
		return nil
	})
	return out
}

// covers: MA-1298, R45
// A backup of a switched-on Gmail store writes message pages whose "This folder" link names the
// folder in Gmail's words ("This folder: All Mail"; design GD-18b, the B3 friction review's F10).
// Positive twin first: a store that is not Gmail keeps "This folder" exactly.
func TestAGmailBackupsPagesNameTheirFolder(t *testing.T) {
	_, plain := imapProfile(t, "imap.example.org", nil, tbFolder{path: "INBOX", listed: 1, msgs: tbMsgs("p", 1)})
	out := tmpDir(t)
	runTB(t, Options{Inputs: []string{plain}, Out: out})
	pages := pagesUnder(t, filepath.Join(out, "imap.example.org"))
	if len(pages) == 0 {
		t.Fatal("positive twin: no page written")
	}
	for _, p := range pages {
		if !strings.Contains(p, `">This folder</a>`) || strings.Contains(p, "This folder:") {
			t.Fatal("positive twin: a page of a store that is not Gmail changed its link")
		}
	}

	_, store := imapProfile(t, "imap.gmail.com", map[string]string{"is_gmail": "true"}, gmailFolders(6)...)
	out = tmpDir(t)
	runTB(t, Options{Inputs: []string{store}, Out: out}) // a new Gmail archive is switched on
	all := pagesUnder(t, filepath.Join(out, "imap.gmail.com", "[Gmail]", "All Mail"))
	if len(all) == 0 {
		t.Fatal("no All Mail page written")
	}
	for _, p := range all {
		if !strings.Contains(p, `">This folder: All Mail</a>`) {
			t.Fatal("an All Mail page of a switched-on Gmail store does not say \"This folder: All Mail\"")
		}
	}
}
