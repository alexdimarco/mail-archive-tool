package main

import (
	"flag"
	"fmt"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
)

// covers: MA-832, R37
// -program-copies (program design §6, R21): on or off is the run's choice; with
// -encrypt it is on unless given off (on by default, O1); on a keyed backup without
// it the sealed choice is left as it is. Any other value is refused before the key, as
// is the flag on a plaintext archive's run.
func TestProgramCopiesFlag(t *testing.T) {
	choice := func(creating bool, args ...string) (app.ProgramChoice, error) {
		fs := flag.NewFlagSet("t", flag.ContinueOnError)
		k := addKeyFlags(fs)
		if err := fs.Parse(args); err != nil {
			t.Fatal(err)
		}
		if err := refuseStrayKeyFlags(fs, k, !creating, creating); err != nil {
			return 0, err
		}
		return k.programChoice(creating), nil
	}
	got := func(creating bool, args ...string) string {
		c, err := choice(creating, args...)
		if err != nil {
			return "refused"
		}
		return fmt.Sprint(c)
	}
	want := fmt.Sprint(app.ProgramCopiesOn, app.ProgramCopiesOff, app.ProgramCopiesOn, app.ProgramCopiesOff, app.ProgramCopiesUnset)
	have := fmt.Sprint([]string{got(true), got(true, "-program-copies", "off"), got(false, "-program-copies", "on"), got(false, "-program-copies", "off"), got(false)})
	want = "[" + want + "]"
	if have != want {
		t.Fatalf("choices %q, want %q", have, want)
	}
	_, err := choice(false, "-program-copies", "yes")
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	k := addKeyFlags(fs)
	fs.Parse([]string{"-program-copies", "on"})
	stray := refuseStrayKeyFlags(fs, k, false, false)
	assure.Refused(t, 1, errText(err)+" | "+errText(stray), assure.Code(1), assure.Names("-program-copies", "on or off", "encrypted archive"),
		assure.NoSideEffect(func() bool { return err != nil && stray != nil }))
}
