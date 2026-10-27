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
	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/memguard"
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

// covers: MA-847, R37
// verify -full on a real archive keeping copies of MailArchive (program design C18):
// the sealed set reaches the comparison — the copies as the backup wrote them tell
// nothing; one removed is told, a warning that leaves the mail's verdict intact.
func TestVerifyFullComparesTheKeptCopies(t *testing.T) {
	freshRecord(t)
	out, _, w := made(t, nil)
	readBackup(t, w)
	if err := SetProgramCopies(context.Background(), w, true, quiet); err != nil {
		t.Fatal(err)
	}
	readBackup(t, w) // writes this computer's own copy
	w.Destroy()
	u, err := Open(out, Password(testPassword), OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer u.Destroy()
	clean, err := VerifyFull(context.Background(), u, app.VerifyOptions{}, quiet, nil)
	if err != nil || len(clean.ProgramFindings) != 0 || clean.Outcome != state.OutcomeIntact {
		t.Fatalf("positive twin: %v %v", err, clean.ProgramFindings)
	}
	entries, _ := os.ReadDir(filepath.Join(out, container.ProgramDir))
	if len(entries) == 0 {
		t.Fatal("no copy was written")
	}
	os.Remove(filepath.Join(out, container.ProgramDir, entries[0].Name()))
	rep, err := VerifyFull(context.Background(), u, app.VerifyOptions{}, quiet, nil)
	assure.Reached(t, err == nil && strings.Contains(strings.Join(rep.ProgramFindings, " "), "is missing") && rep.Outcome == state.OutcomeIntact,
		"a removed copy told, the mail intact")
}

// covers: MA-803, R37
// What the key's own reads find, on a real archive (the P3-2b pass, FID-02): a file
// both checks see is listed once, by the key's finding — a message file gone or
// changed in place is "unreadable"/"missing" with its part, red; a chunk changed in
// place is named with its part, amber (FID-04); a search index behind its records,
// which no chunk shows, is amber (VERD-05).
func TestVerifyFullReadsWhatTheKeylessCheckCannot(t *testing.T) {
	out, u := fullFixture(t)
	unit := aUnitFile(t, out)
	rel, _ := filepath.Rel(out, unit)
	b, _ := os.ReadFile(unit)
	os.Remove(unit)
	rep, err := VerifyFull(context.Background(), u, app.VerifyOptions{}, quiet, nil)
	summary := strings.Join(app.VerifySummary(rep), "\n")
	if err != nil || strings.Count(summary, filepath.ToSlash(rel)) != 1 || !strings.Contains(summary, "missing: "+filepath.ToSlash(rel)+" (a message") {
		t.Fatalf("a file both checks name is listed once, with its part: %v\n%s", err, summary)
	}
	os.WriteFile(unit, b, 0o600)
	changed := append([]byte(nil), b...)
	changed[len(changed)/2] ^= 1
	os.WriteFile(unit, changed, 0o600)
	rep, err = VerifyFull(context.Background(), u, app.VerifyOptions{}, quiet, nil)
	summary = strings.Join(app.VerifySummary(rep), "\n")
	if err != nil || rep.Outcome != state.OutcomeDamaged || !strings.Contains(summary, "unreadable: "+filepath.ToSlash(rel)) {
		t.Fatalf("a message changed in place: %v\n%s", err, summary)
	}
	os.WriteFile(unit, b, 0o600)

	// A chunk the record names, changed in place (chunks/ may also hold superseded
	// ones the next backup removes, which the record does not name): each is tried
	// until one is named.
	var chunks []string
	filepath.WalkDir(filepath.Join(out, "chunks"), func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			chunks = append(chunks, p)
		}
		return nil
	})
	named := false
	for _, chunk := range chunks {
		cb, _ := os.ReadFile(chunk)
		cb[len(cb)/2] ^= 1
		os.WriteFile(chunk, cb, 0o600)
		crel, _ := filepath.Rel(out, chunk)
		rep, err = VerifyFull(context.Background(), u, app.VerifyOptions{}, quiet, nil)
		summary = strings.Join(app.VerifySummary(rep), "\n")
		cb[len(cb)/2] ^= 1
		os.WriteFile(chunk, cb, 0o600)
		if strings.Contains(summary, filepath.ToSlash(crel)) {
			if err != nil || rep.Outcome != state.OutcomeNotAttested || !strings.Contains(summary, "part ") {
				t.Fatalf("a chunk changed in place: %v\n%s", err, summary)
			}
			named = true
			break
		}
	}
	if !named {
		t.Fatal("no chunk the record names was found changed")
	}

	// A backup with indexing off leaves the index behind its records.
	w, err := Open(out, Password(testPassword), OpenOptions{Write: true})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Destroy()
	box := filepath.Join(t.TempDir(), "Later")
	os.WriteFile(box, []byte(strings.Replace(sampleMbox, "abc@example.com", "later@example.com", 1)), 0o600)
	opts := app.Options{Inputs: []string{box}, Out: out, Index: false, KeepRaw: true}
	done, err := Apply(&opts, w)
	if err != nil {
		t.Fatal(err)
	}
	_, err = app.Run(context.Background(), opts, quiet, nil)
	done()
	if err != nil {
		t.Fatal(err)
	}
	rep, err = VerifyFull(context.Background(), u, app.VerifyOptions{}, quiet, nil)
	summary = strings.Join(app.VerifySummary(rep), "\n")
	assure.Reached(t, err == nil && rep.Outcome == state.OutcomeNotAttested && strings.Contains(summary, "search index is missing messages"), "an index behind is amber")
}

// covers: MA-803, R37
// The chunk pass holds the key per chunk: a lock that lands after the record was
// read ends the pass with the lock, never damage and never a pass on a locked key
// (the P3-2b pass, KEY-02).
func TestALockAfterTheLoadEndsTheChunkPass(t *testing.T) {
	_, u := fullFixture(t)
	l, err := u.Lease()
	if err != nil {
		t.Fatal(err)
	}
	s := &sealedCheck{u: l, out: l.Out(), guard: memguard.Process}
	if _, err := s.Load(); err != nil {
		t.Fatal(err)
	}
	if p, err := s.Chunks(context.Background()); err != nil || len(p) != 0 {
		t.Fatalf("positive twin: %v %v", p, err)
	}
	l.Lock()
	_, err = s.Chunks(context.Background())
	assure.Reached(t, errors.Is(err, app.ErrCheckLocked), "a lock after the load ends the chunk pass")
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
