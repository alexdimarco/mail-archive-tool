package main

import (
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/state"
)

// covers: MA-1301, R45
// `search` names every folder a Gmail email is in, "In: HOME, OTHER", as the reader does (the B3
// friction review's F17); the names are the stored folder paths, which `-folder` takes. Positive
// twin first: an email in one folder shows its folder alone, with no "In:".
func TestSearchNamesEveryFolderAnEmailIsIn(t *testing.T) {
	acct := t.TempDir()
	writeMaildirMsg(t, filepath.Join(acct, ".Inbox"), "1.eml", sampleMsg)
	writeMaildirMsg(t, acct, "0.eml", strings.Replace(sampleMsg, "Message-ID: <", "Message-ID: <top-", 1))
	dir := filepath.Join(t.TempDir(), "archive")
	if code, stderr := runCLI("-out", dir, "-input", acct); code != 0 {
		t.Fatalf("the run: exit %d, %s", code, stderr)
	}
	code, stdout, stderr := runCLIOut(t, "search", "-out", dir, "-folder", "Inbox")
	if code != 0 || !strings.Contains(stdout, "1 match") || strings.Contains(stdout, "In: ") {
		t.Fatalf("positive twin: search (exit %d):\n%s\n%s", code, stdout, stderr)
	}

	m, err := state.Load(filepath.Join(dir, ".mailarchive-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	ix, err := index.Open(filepath.Join(dir, "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	for k := range m.All() {
		tok, rest, _ := strings.Cut(k, "\x00")
		if folder, _, _ := strings.Cut(rest, "\x00"); folder != "Inbox" {
			continue
		}
		if err := ix.SetMembers(k, tok, []string{"Receipts"}); err != nil {
			t.Fatal(err)
		}
	}
	ix.Flush()
	ix.Close()
	code, stdout, stderr = runCLIOut(t, "search", "-out", dir, "-folder", "Receipts")
	if code != 0 || !strings.Contains(stdout, "1 match") || !strings.Contains(stdout, "In: Inbox, Receipts") {
		t.Fatalf("search -folder Receipts (exit %d) does not say \"In: Inbox, Receipts\":\n%s\n%s", code, stdout, stderr)
	}
}
