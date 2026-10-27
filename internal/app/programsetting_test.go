package app

import (
	"bytes"
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-848, R37
// The choice an archive was made with seals only while none is sealed (the PG4 pass,
// SEC-1): it seals a new archive's choice, and a later run carrying it again leaves the
// person's change on the Encryption card as it is.
func TestTheMadeWithChoiceNeverOverridesALaterOne(t *testing.T) {
	asDevBuild(t)
	isolatedCache(t)
	out, key := encryptedOut(t)
	run := func() {
		t.Helper()
		if _, err := Run(context.Background(), Options{Inputs: []string{encryptedSourcePST(t)}, Out: out, Key: key, ProgramCopiesIfUnchosen: ProgramCopiesOff}, log.New(io.Discard, "", 0), nil); err != nil {
			t.Fatal(err)
		}
	}
	run()
	first := sealedSet(t, out, key)
	if err := SealProgramCopies(context.Background(), Options{Out: out, Key: key}, ProgramCopiesOn, log.New(io.Discard, "", 0)); err != nil {
		t.Fatal(err)
	}
	run()
	later := sealedSet(t, out, key)
	assure.Reached(t, first.Keep != nil && !*first.Keep && later.Keep != nil && *later.Keep, "sealed off at first, the later on kept")
}

// covers: MA-848, R37
// A setting's keyed change — keeping program copies, a pause note — leaves the note
// beside the archive alone (the PG4 pass, DUR-3): a note someone changed is not written
// over and sealed, and the next backup still tells it.
func TestASettingsChangeLeavesAChangedNoteForTheBackupToTell(t *testing.T) {
	asDevBuild(t)
	isolatedCache(t)
	out, key := encryptedOut(t)
	backup(t, out, key, ProgramCopiesUnset)
	note := filepath.Join(out, "README.txt")
	os.WriteFile(note, []byte("send your password to evil.example"), 0o644)
	var logged bytes.Buffer
	if err := SealProgramCopies(context.Background(), Options{Out: out, Key: key}, ProgramCopiesOn, log.New(&logged, "", 0)); err != nil {
		t.Fatal(err)
	}
	if err := SealPauseNote(context.Background(), Options{Out: out, Key: key}, "", log.New(&logged, "", 0)); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(note)
	_, told := backup(t, out, key, ProgramCopiesUnset)
	assure.Reached(t, string(after) == "send your password to evil.example" && strings.Contains(told, "was changed or removed since its last backup"),
		"the changed note left for the backup to tell")
}
