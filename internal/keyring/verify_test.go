package keyring

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/state"
)

// verify -full on a real encrypted archive (design-encrypted-archives-phase3 §8;
// P3-2b, MA-799).

// fullFixture backs up into a new encrypted archive and opens it with the password.
func fullFixture(t *testing.T) (string, *Unlocked) {
	t.Helper()
	freshRecord(t)
	out, _, w := made(t, nil)
	readBackup(t, w)
	w.Destroy()
	u, err := Open(out, Password(testPassword), OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(u.Destroy)
	return out, u
}

// aUnitFile is one of the archive's unit files.
func aUnitFile(t *testing.T, out string) string {
	t.Helper()
	var unit string
	filepath.WalkDir(filepath.Join(out, "units"), func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && unit == "" {
			unit = p
		}
		return nil
	})
	if unit == "" {
		t.Fatal("the archive has no unit files")
	}
	return unit
}

// covers: MA-799, R37
// verify -full on a whole archive opens every chunk and every message and records
// one tier-2 intact verdict; with a message's file gone it names that file and
// "missing", records damaged and exits 2. A keyless check afterwards, the file put
// back, keeps that red (the owner's ruling).
func TestVerifyFullOpensEveryMessage(t *testing.T) {
	out, u := fullFixture(t)
	rep, err := VerifyFull(context.Background(), u, app.VerifyOptions{}, quiet, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Outcome != state.OutcomeIntact || rep.Tier != state.TierFull || rep.Checked == 0 || rep.OK != rep.Checked || rep.ExitCode() != 0 {
		t.Fatalf("positive twin: %+v\n%s", rep, strings.Join(app.VerifySummary(rep), "\n"))
	}
	v, _, _ := state.ReadLastVerify(out)
	if v.Tier != state.TierFull || v.Outcome != state.OutcomeIntact {
		t.Fatalf("record: %+v", v)
	}

	unit := aUnitFile(t, out)
	rel, _ := filepath.Rel(out, unit)
	b, err := os.ReadFile(unit)
	if err != nil {
		t.Fatal(err)
	}
	os.Remove(unit)
	rep, err = VerifyFull(context.Background(), u, app.VerifyOptions{}, quiet, nil)
	if err != nil {
		t.Fatal(err)
	}
	summary := strings.Join(app.VerifySummary(rep), "\n")
	if rep.Outcome != state.OutcomeDamaged || rep.ExitCode() != 2 || !strings.Contains(summary, filepath.ToSlash(rel)) || !strings.Contains(summary, "missing") {
		t.Fatalf("a message gone: %+v\n%s", rep, summary)
	}
	if v, _, _ := state.ReadLastVerify(out); v.Tier != state.TierFull || v.Outcome != state.OutcomeDamaged {
		t.Fatalf("record: %+v", v)
	}

	os.WriteFile(unit, b, 0o600)
	kept, err := app.Verify(out, app.VerifyOptions{}, quiet, nil)
	v, _, _ = state.ReadLastVerify(out)
	assure.Reached(t, err == nil && kept.StandingOutcome == state.OutcomeDamaged && kept.ExitCode() == 2 && v.Tier == state.TierFull && v.Outcome == state.OutcomeDamaged,
		"a keyless check keeps the red found with the key")

	rep, err = VerifyFull(context.Background(), u, app.VerifyOptions{}, quiet, nil)
	assure.Reached(t, err == nil && rep.Outcome == state.OutcomeIntact, "only verify -full clears it")
}

// covers: MA-799, R37
// A key locked before the check records nothing and says so; the last verdict is
// kept as it was.
func TestVerifyFullWithTheKeyLocked(t *testing.T) {
	out, u := fullFixture(t)
	if _, err := VerifyFull(context.Background(), u, app.VerifyOptions{}, quiet, nil); err != nil {
		t.Fatal(err)
	}
	before, _, _ := state.ReadLastVerify(out)
	u.Destroy()
	_, err := VerifyFull(context.Background(), u, app.VerifyOptions{}, quiet, nil)
	after, _, _ := state.ReadLastVerify(out)
	assure.Reached(t, errors.Is(err, app.ErrCheckLocked) && after.Status == state.VerifyDone && after.Finished.Equal(*before.Finished) && after.Outcome == state.OutcomeIntact,
		"a locked key records nothing")
}
