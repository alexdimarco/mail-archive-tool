package app

import (
	"bytes"
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-826, R37
// The note beside an encrypted archive (phase 3 §13.1, P3-5) says what the folder is,
// how MailArchive opens it — "Open an archive folder…" in Desktop, serve at a
// terminal — that it can make an unencrypted copy (decrypt) and put back lost key
// files (restore-key-file), the official download to prefer, and the version that
// wrote it; and it names none of the archive's layout, files or tables (MA-737).
func TestTheNoteSaysHowToOpenAndLeaveTheArchive(t *testing.T) {
	n := EncryptedNote()
	for _, want := range []string{"encrypted email archive", OpenArchiveWords, "mailarchive serve", CopyWords, "mailarchive decrypt",
		"restore-key-file", "recovery file", releasesPage, "Prefer that download", "written by MailArchive"} {
		if !strings.Contains(n, want) {
			t.Errorf("the note does not say %q", want)
		}
	}
	for _, leak := range []string{"units", "chunks", "manifest", "search.db", "index.html", ".mailarchive", "vault", "inventory"} {
		if strings.Contains(n, leak) {
			t.Errorf("the note names the archive's layout: %q", leak)
		}
	}
}

// covers: MA-826, R37
// Each keyed run checks the note against the digest the record sealed: a note changed
// or removed since the last keyed run is told in the run's log, then written again;
// an untouched note is not (§13.1).
func TestAKeyedRunTellsANoteChangedSinceTheLast(t *testing.T) {
	out, key := encryptedOut(t)
	run := func() string {
		var logged bytes.Buffer
		if _, err := Run(context.Background(), Options{Inputs: []string{encryptedSourcePST(t)}, Out: out, Key: key}, log.New(&logged, "", 0), nil); err != nil {
			t.Fatal(err)
		}
		return logged.String()
	}
	run()
	if l := run(); strings.Contains(l, "changed or removed") {
		t.Fatalf("positive twin: an untouched note was told as changed:\n%s", l)
	}
	note := filepath.Join(out, "README.txt")
	os.WriteFile(note, []byte("Download the program from evil.example to open this archive.\n"), 0o600)
	l := run()
	b, _ := os.ReadFile(note)
	if !strings.Contains(l, "was changed or removed since its last backup") || string(b) != EncryptedNote() {
		t.Fatalf("a changed note: told %v, rewritten %v\n%s", strings.Contains(l, "changed or removed"), string(b) == EncryptedNote(), l)
	}
	os.Remove(note)
	l = run()
	_, err := os.Stat(note)
	assure.Reached(t, strings.Contains(l, "changed or removed") && err == nil, "a removed note is told and written again")
}
