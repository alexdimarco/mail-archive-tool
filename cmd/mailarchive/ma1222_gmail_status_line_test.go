package main

import (
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/state"
)

// covers: MA-1222, R45
// `status` and the backup's run summary say the switched-on Gmail stores' counts, as the real
// program (docs/design-gmail-dedup.md §8.1, slice B3): counts only, never a folder name.
// Positive twin first: an archive nobody switched on has no Gmail line in either.
func TestStatusAndTheRunSummarySayTheGmailCounts(t *testing.T) {
	// The words amended 2026-11-13 by the B3 friction review's F12 (MA-1300).
	const line = "Gmail: 1 email saved; 1 of them is also listed in another folder without a second copy."
	src := filepath.Join(t.TempDir(), "Inbox")
	writeMaildirMsg(t, src, "1.eml", sampleMsg)
	dir := filepath.Join(t.TempDir(), "archive")
	if code, stderr := runCLI("-out", dir, "-input", src); code != 0 || strings.Contains(stderr, "Gmail:") {
		t.Fatalf("positive twin: the run: exit %d, %s", code, stderr)
	}
	if got, code := statusOf(t, dir); code != 0 || strings.Contains(got, "Gmail:") {
		t.Fatalf("positive twin: status (exit %d):\n%s", code, got)
	}

	mpath := filepath.Join(dir, ".mailarchive-manifest.json")
	m, err := state.Load(mpath)
	if err != nil {
		t.Fatal(err)
	}
	for k, r := range m.All() {
		r.Folders = []state.Membership{{Folder: "Secret Label", Kind: state.MemberCopy, FirstSeen: r.ExportedAt}}
		m.Add(k, r)
		tok, _, _ := strings.Cut(k, "\x00")
		m.SwitchOnGmail(tok)
	}
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	got, code := statusOf(t, dir)
	if code != 0 || !strings.Contains(got, line) || strings.Contains(got, "Secret Label") {
		t.Fatalf("status (exit %d) does not say %q, or names a folder:\n%s", code, line, got)
	}
	code, stderr := runCLI("-out", dir, "-input", src)
	if code != 0 || !strings.Contains(stderr, line) || strings.Contains(stderr, "Secret Label") {
		t.Fatalf("the run's summary (exit %d) does not say %q, or names a folder:\n%s", code, line, stderr)
	}
}
