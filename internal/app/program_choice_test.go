package app

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/vault"
)

// sealedKeep is the sealed program set's keep choice: "on", "off", or "" when none.
func sealedKeep(t *testing.T, out string, key *vault.Key) string {
	t.Helper()
	_, s, err := container.LoadManifest(out, container.LoadOptions{Key: key, ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	ps, st, err := s.Program()
	if err != nil || st == container.ProgramAbsent || ps.Keep == nil {
		return ""
	}
	if *ps.Keep {
		return "on"
	}
	return "off"
}

// covers: MA-832, R37
// The choice to keep copies of MailArchive in an encrypted archive (program design
// §6, R21): a keyed run with ProgramCopies on or off seals that choice in its commit;
// a run with it unset leaves the sealed choice as it was, and with none sealed seals
// none — "not chosen".
func TestAKeyedRunSealsTheProgramCopiesChoice(t *testing.T) {
	out, key := encryptedOut(t)
	run := func(c ProgramChoice) {
		if _, err := Run(context.Background(), Options{Inputs: []string{encryptedSourcePST(t)}, Out: out, Key: key, ProgramCopies: c}, log.New(io.Discard, "", 0), nil); err != nil {
			t.Fatal(err)
		}
	}
	var got []string
	for _, c := range []ProgramChoice{ProgramCopiesUnset, ProgramCopiesOn, ProgramCopiesUnset, ProgramCopiesOff, ProgramCopiesUnset} {
		run(c)
		got = append(got, sealedKeep(t, out, key))
	}
	assure.Reached(t, fmt.Sprint(got) == "[ on on off off]", fmt.Sprintf("sealed after each run: %q", got))
}

// covers: MA-832, R37
// The choice changes only keep: the rest of the set — the high-water mark — stays as
// sealed (R7); the run's program step then finishes the update a stopped run left
// (state landed), a name being removed that is gone dropped.
func TestTheChoiceKeepsTheRestOfTheSet(t *testing.T) {
	out, key := encryptedOut(t)
	if _, err := Run(context.Background(), Options{Inputs: []string{encryptedSourcePST(t)}, Out: out, Key: key, ProgramCopies: ProgramCopiesOn}, log.New(io.Discard, "", 0), nil); err != nil {
		t.Fatal(err)
	}
	m, s, err := container.LoadManifest(out, container.LoadOptions{Key: key})
	if err != nil {
		t.Fatal(err)
	}
	ps, _, _ := s.Program()
	ps.State, ps.Version = container.ProgramIntent, "v1.5.0"
	ps.Removing = []string{"SHA256SUMS-v1.4.0"}
	if err := s.SetProgram(ps); err != nil {
		t.Fatal(err)
	}
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), Options{Inputs: []string{encryptedSourcePST(t)}, Out: out, Key: key, ProgramCopies: ProgramCopiesOff}, log.New(io.Discard, "", 0), nil); err != nil {
		t.Fatal(err)
	}
	_, s, _ = container.LoadManifest(out, container.LoadOptions{Key: key, ReadOnly: true})
	got, st, _ := s.Program()
	assure.Reached(t, st == container.ProgramOK && got.Keep != nil && !*got.Keep && got.State == container.ProgramLanded && got.Version == "v1.5.0" && len(got.Removing) == 0,
		fmt.Sprintf("the rest of the set kept: %+v", got))
}

// covers: MA-833, R37
// verify says, without the key, what in the archive's program folder is not
// MailArchive's — a note that never changes the outcome (program design §3).
func TestKeylessVerifyNotesWhatIsOddInProgram(t *testing.T) {
	out, key := encryptedOut(t)
	if _, err := Run(context.Background(), Options{Inputs: []string{encryptedSourcePST(t)}, Out: out, Key: key}, log.New(io.Discard, "", 0), nil); err != nil {
		t.Fatal(err)
	}
	before, err := Verify(out, VerifyOptions{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(out, "program", "unzipped"), 0o700)
	r, err := Verify(out, VerifyOptions{}, nil, nil)
	note := strings.Join(keylessSummary(r), "\n")
	assure.Reached(t, err == nil && r.ProgramOdd == 1 && r.Outcome == before.Outcome && before.ProgramOdd == 0 &&
		strings.Contains(note, "program folder are not copies MailArchive keeps there (program/unzipped)"), "noted, outcome unchanged:\n"+note)
}
