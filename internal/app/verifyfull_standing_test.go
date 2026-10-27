package app

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/health"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/vault"
)

// covers: MA-818, R37
// status tells a heal owed when the two key-file copies' bytes differ, not only when
// one is missing (FID-09): the case §16's call names.
func TestStatusTellsAHealOwedForKeyCopiesThatDiffer(t *testing.T) {
	out := sealedArchive(t)
	in := health.Gather(out, "")
	if strings.Contains(strings.Join(health.Assess(in, time.Now()).Codes, " "), "encrypted_key_copy_differs") {
		t.Fatal("positive twin: a whole archive owes no heal")
	}
	b, _ := os.ReadFile(filepath.Join(out, vault.BackupName))
	os.WriteFile(filepath.Join(out, vault.BackupName), append(b, '\n'), 0o600)
	in = health.Gather(out, "")
	assure.Reached(t, strings.Contains(strings.Join(health.Assess(in, time.Now()).Codes, " "), "encrypted_key_copy_differs"), "copies whose bytes differ owe a heal")
}

// aStandingRed writes a red a check with the key found, with its attention file.
func aStandingRed(t *testing.T, out string) string {
	t.Helper()
	when := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	red := state.LastVerify{Status: state.VerifyDone, Started: when, Finished: &when, Encrypted: true, Tier: state.TierFull, Outcome: state.OutcomeDamaged}
	if err := state.WriteLastVerify(out, red); err != nil {
		t.Fatal(err)
	}
	attention := filepath.Join(out, state.IntegrityAttentionName)
	if _, err := os.Stat(attention); err != nil {
		t.Fatalf("fixture: the red's attention file: %v", err)
	}
	return attention
}

// covers: MA-819, R37
// A red found with the key stands until a check with the key passes (§8; the P3-3
// pass, CRIT-04): an amber check with the key that did not open every message — the
// messages unreadable on this computer, or the archive moving under a lock-free
// check — keeps the red standing beneath it, says so, and leaves the attention file.
// The positive twin: an amber whose messages all opened (only the index is behind)
// replaces the red and removes the file.
func TestAnAmberThatOpenedNoMessageKeepsTheRedStanding(t *testing.T) {
	t.Run("unreadable here", func(t *testing.T) {
		out := sealedArchive(t)
		attention := aStandingRed(t, out)
		rep, summary := fullSummary(t, out, &fakeSealed{listOK: true, unitErr: fs.ErrPermission})
		v, _, _ := state.ReadLastVerify(out)
		_, aerr := os.Stat(attention)
		if rep.Outcome != state.OutcomeNotAttested || state.StandingRed(&v) == nil || aerr != nil || rep.StandingOutcome != state.OutcomeDamaged {
			t.Fatalf("an unreadable check erased the standing red: %s, standing %v, file kept %v\n%s", rep.Outcome, state.StandingRed(&v) != nil, aerr == nil, summary)
		}
	})
	t.Run("moved under a lock-free check", func(t *testing.T) {
		out := sealedArchive(t)
		attention := aStandingRed(t, out)
		orig := verifyLock
		verifyLock = func(path, verb string) (*lockfile.Lock, error) {
			return nil, &fs.PathError{Op: "open", Path: path, Err: syscall.EROFS}
		}
		defer func() { verifyLock = orig }()
		mirror := filepath.Join(out, container.MirrorName)
		rep, _ := fullSummary(t, out, &fakeSealed{listOK: true, unitErr: fs.ErrNotExist, onUnit: func() {
			b, _ := os.ReadFile(mirror)
			os.WriteFile(mirror, append(b, ' '), 0o600)
		}})
		v, _, _ := state.ReadLastVerify(out)
		_, aerr := os.Stat(attention)
		if state.StandingRed(&v) == nil || aerr != nil {
			t.Fatalf("a moved check (%s) erased the standing red (%v) or its attention file (kept %v)", rep.Outcome, state.StandingRed(&v) != nil, aerr == nil)
		}
	})
	t.Run("every message opened", func(t *testing.T) {
		out := sealedArchive(t)
		attention := aStandingRed(t, out)
		rep, _ := fullSummary(t, out, &fakeSealed{listOK: true, behind: true})
		v, _, _ := state.ReadLastVerify(out)
		_, aerr := os.Stat(attention)
		if rep.Outcome != state.OutcomeNotAttested || state.StandingRed(&v) != nil || aerr == nil || rep.StandingOutcome != "" {
			t.Fatalf("an amber whose messages all opened did not replace the red: %s, standing %v, file kept %v", rep.Outcome, state.StandingRed(&v) != nil, aerr == nil)
		}
	})
}
