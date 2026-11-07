package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/state"
)

// covers: MA-1129, MA-1124, R40
// The terminal's progress line quotes the folder it is writing (settled 2026-11-07, the
// lead's call, owner-delegated words), as the plan's changed-name list does: a folder
// named "100%" reads ("100%"), never as a percentage (100%), and a name holding a line
// break (as the archive's record keeps it) shows the break as \n inside its quotes, so it
// cannot start a line of its own. An encrypted archive's progress stays counts only
// (MA-1129's own test).
func TestTheTerminalProgressQuotesTheFolder(t *testing.T) {
	e := newTBCLI(t, "")
	src := filepath.Join(t.TempDir(), "Mail")
	writeTB(t, filepath.Join(src, "100%"), tagged("pct"))
	writeTB(t, filepath.Join(src, "Linebreak"), tagged("break"))
	e.archive = tbArchiveOf(t, src)
	tbRefolder(t, e.archive, "Linebreak", "Line\nbreak")
	was := restoreTBEvery
	restoreTBEvery = 0 // every message's progress is said, so the test reads each
	defer func() { restoreTBEvery = was }()

	code, _, stderr := e.run(&scripted{}, nil, "-out", e.archive, "-profile", e.profile, "-yes")
	assure.Reached(t, code == 0 && e.restored(), fmt.Sprintf("the restore finished: %d %q", code, stderr))
	assure.Reached(t, strings.Contains(stderr, ` messages ("100%")`+"\n") && !strings.Contains(stderr, "(100%)"),
		fmt.Sprintf(`the progress names the folder "100%%" quoted: %q`, stderr))
	assure.Reached(t, strings.Contains(stderr, ` messages ("Line\nbreak")`+"\n") && !strings.Contains(stderr, "Line\nbreak"),
		fmt.Sprintf("the name's line break shows as \\n, never as a new line: %q", stderr))
}

// tagged is tbMbox with its subjects and Message-IDs marked by tag, so each folder's
// messages are its own.
func tagged(tag string) string {
	return strings.NewReplacer("Subject: ", "Subject: "+tag+" ", "Message-ID: <", "Message-ID: <"+tag+".").Replace(tbMbox)
}

// tbArchiveOf backs the Thunderbird-style folder src up into a new plain archive named
// "Kept mail", its originals kept.
func tbArchiveOf(t *testing.T, src string) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "Kept mail")
	if _, err := app.Run(context.Background(), app.Options{Inputs: []string{src}, Out: out, Mode: export.Incremental, Index: true, KeepRaw: true}, log.New(io.Discard, "", 0), nil); err != nil {
		t.Fatal(err)
	}
	return out
}

// tbRefolder moves the records of folder from to folder to in the archive's own record, as
// an archive made elsewhere (or from Microsoft 365) can keep a name no Windows file holds.
func tbRefolder(t *testing.T, out, from, to string) {
	t.Helper()
	man, err := state.Load(filepath.Join(out, ".mailarchive-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var seen []string
	n := 0
	for k, r := range man.All() {
		seen = append(seen, r.Folder)
		if r.Folder == from {
			man.MergeFields(k, to, r.LastSeen, r.Present)
			n++
		}
	}
	if n == 0 {
		t.Fatalf("no record in %q: %q", from, seen)
	}
	if err := man.Save(); err != nil {
		t.Fatal(err)
	}
}
