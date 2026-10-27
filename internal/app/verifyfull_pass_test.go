package app

import (
	"errors"
	"fmt"
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
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/vault"
)

// The P3-2b adversarial pass's fixes (review-encrypted-archives-phase3-p32b-adversarial;
// MA-803).

// fullSummary runs VerifySealed through src on out and returns the report and its
// summary.
func fullSummary(t *testing.T, out string, src *fakeSealed) (Report, string) {
	t.Helper()
	if src.m == nil {
		src.m = oneRecord(t)
	}
	rep, err := VerifySealed(out, VerifyOptions{}, discard(), nil, src)
	if err != nil {
		t.Fatal(err)
	}
	return rep, strings.Join(VerifySummary(rep), "\n")
}

// covers: MA-803, R37
// Tier 2 is red only when mail is lost (the owner's ruling; FID-01, VERD-01,
// VERD-03, FID-06): a missing copy of the record pointer, which the keyed load saw
// past, is amber with "run a backup"; a keyless red keeps its own words, never the
// tier-2 "messages are damaged"; two ambers are each named with their own remedy.
func TestATier2RedIsOnlyLostMail(t *testing.T) {
	out := sealedArchive(t)
	os.Remove(filepath.Join(out, container.MirrorName))
	rep, summary := fullSummary(t, out, &fakeSealed{listOK: true})
	if rep.Outcome != state.OutcomeNotAttested || !strings.Contains(summary, "rewrites the record pointer's damaged copy") ||
		strings.Contains(summary, "messages in the archive are missing") {
		t.Fatalf("a pointer copy: %s\n%s", rep.Outcome, summary)
	}

	out = sealedArchive(t)
	os.Remove(filepath.Join(out, filepath.FromSlash(aUnitOf(t, out))))
	rep, summary = fullSummary(t, out, &fakeSealed{listOK: true})
	if rep.Outcome != state.OutcomeDamaged || !strings.Contains(summary, state.OutcomeText(state.OutcomeDamaged)) ||
		strings.Contains(summary, "messages in the archive are missing or damaged, or its record") {
		t.Fatalf("a keyless red keeps its own words: %s\n%s", rep.Outcome, summary)
	}

	out = sealedArchive(t)
	_, summary = fullSummary(t, out, &fakeSealed{listOK: true, chunks: []container.ChunkProblem{
		{Path: "chunks/bb/y", Section: vault.ContainerIndex, Position: 1, Kind: "missing"},
		{Path: "chunks/cc/z", Section: vault.ContainerHistory, Position: 0, Kind: "missing"},
	}})
	assure.Reached(t, strings.Contains(summary, "part of the search index cannot be read; part of the go-back timeline cannot be read") &&
		strings.Contains(summary, "reindex -rebuild") && strings.Contains(summary, "nothing restores the go-back timeline"), "both ambers named, each with its remedy")
}

// covers: MA-803, R37
// A refused load opens no message and says so, with the keyless counts as files
// (VERD-02); its remedy is its own outcome's — never a restore for a newer
// MailArchive or an unfinished save (OUT-3); both copies of one part of the record
// gone is red (OUT-2); a chunk pass that ended early attests nothing (CRIT-04).
func TestARefusedLoadOrALostRecordPartIsTold(t *testing.T) {
	out := sealedArchive(t)
	rep, summary := fullSummary(t, out, &fakeSealed{loadErr: fmt.Errorf("%w: x", container.ErrRecord)})
	if rep.Outcome != state.OutcomeDamaged || !strings.Contains(summary, "no message was opened") || strings.Contains(summary, "message files") {
		t.Fatalf("a refused load: %s\n%s", rep.Outcome, summary)
	}
	for _, c := range []struct {
		err     error
		outcome string
	}{{container.ErrNewerFormat, state.OutcomeNewerFormat}, {container.ErrUnfinishedSave, state.OutcomeUnfinishedSave}} {
		// Each on an archive of its own: the red recorded above stands beneath a later
		// check that opens no message (MA-819), with its own restore advice.
		rep, summary = fullSummary(t, sealedArchive(t), &fakeSealed{loadErr: fmt.Errorf("%w: x", c.err)})
		if rep.Outcome != c.outcome || !strings.Contains(summary, state.OutcomeText(c.outcome)) || strings.Contains(summary, adviceLoad) {
			t.Fatalf("%s: %s\n%s", c.outcome, rep.Outcome, summary)
		}
	}
	rep, summary = fullSummary(t, out, &fakeSealed{listOK: true, chunks: []container.ChunkProblem{
		{Path: "chunks/aa/1", Section: vault.ContainerManifest, Position: 3, Copy: 1, Kind: "missing"},
		{Path: "chunks/aa/2", Section: vault.ContainerManifest, Position: 3, Copy: 2, Kind: "missing"},
	}})
	if rep.Outcome != state.OutcomeDamaged || !strings.Contains(summary, "both copies of a part of the archive's record") {
		t.Fatalf("both copies: %s\n%s", rep.Outcome, summary)
	}
	rep, summary = fullSummary(t, out, &fakeSealed{listOK: true, chunkErr: errors.New("a read failed")})
	assure.Reached(t, rep.Outcome == state.OutcomeNotAttested && strings.Contains(summary, "could not all be read"), "an early end attests nothing")
}

// covers: MA-803, R37
// A key locked part way records nothing: an archive that had no record has none
// after, and one whose record was a stopped check's keeps it byte for byte (KEY-01,
// REC-02).
func TestALockedCheckLeavesTheRecordAsItWas(t *testing.T) {
	out := sealedArchive(t)
	os.Remove(filepath.Join(out, state.LastVerifyName))
	_, err := VerifySealed(out, VerifyOptions{}, discard(), nil, &fakeSealed{m: oneRecord(t), listOK: true, unitErr: ErrCheckLocked})
	if _, st, _ := state.ReadLastVerify(out); !errors.Is(err, ErrCheckLocked) || st != state.LastVerifyAbsent {
		t.Fatalf("no record before, none after: %v %v", err, st)
	}
	when := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	state.WriteLastVerify(out, state.LastVerify{Status: state.VerifyStopped, Started: when, Finished: &when, Encrypted: true, Tier: state.TierFull})
	before, _ := os.ReadFile(filepath.Join(out, state.LastVerifyName))
	VerifySealed(out, VerifyOptions{}, discard(), nil, &fakeSealed{m: oneRecord(t), listOK: true, unitErr: ErrCheckLocked})
	after, _ := os.ReadFile(filepath.Join(out, state.LastVerifyName))
	if len(before) == 0 || string(before) != string(after) {
		t.Fatalf("the stopped record kept byte for byte:\n%s\n%s", before, after)
	}
	// Status names -full to finish a check with the key (REC-07).
	hr := health.Assess(health.Gather(out, ""), time.Now())
	assure.Reached(t, strings.Contains(strings.Join(hr.Reasons, "\n"), "mailarchive verify -full -out"), "the stopped check with the key names -full")
}

// covers: MA-803, R37
// On a read-only medium the check does not run beside a backup another path to the
// folder holds the lock for (OUT-4); and an archive that moves under a lock-free
// check is not attested, a red it found included (OUT-1).
func TestALockFreeCheckBesideAWriter(t *testing.T) {
	out := sealedArchive(t)
	lockPath := filepath.Join(out, lockfile.Name)
	orig := verifyLock
	verifyLock = func(path, verb string) (*lockfile.Lock, error) {
		return nil, &fs.PathError{Op: "open", Path: path, Err: syscall.EROFS}
	}
	defer func() { verifyLock = orig }()
	holder, err := lockfile.AcquireAs(lockPath, "")
	if err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(out, state.LastVerifyName))
	_, err = VerifySealed(out, VerifyOptions{}, discard(), nil, &fakeSealed{m: oneRecord(t), listOK: true})
	after, _ := os.ReadFile(filepath.Join(out, state.LastVerifyName))
	if !errors.Is(err, lockfile.ErrHeld) || string(before) != string(after) {
		t.Fatalf("beside a live holder: %v", err)
	}
	holder.Release()

	mirror := filepath.Join(out, container.MirrorName)
	rep, summary := fullSummary(t, out, &fakeSealed{listOK: true, unitErr: fs.ErrNotExist, onUnit: func() {
		b, _ := os.ReadFile(mirror)
		os.WriteFile(mirror, append(b, ' '), 0o600)
	}})
	assure.Reached(t, rep.Outcome == state.OutcomeNotAttested && strings.Contains(summary, "changed while it was checked") && rep.ExitCode() == 2,
		"a moving archive is not attested, its red not trusted")
}

// covers: MA-804, R37
// Status without the key (the P3-3 plan, 3b): an encrypted archive whose two key-file
// copies differ is owed a heal — a warning that says how it is repaired, the same in
// status and the Desktop (X10); a whole archive has none. How to see its sealed
// contents is said in the surface's own words (U16).
func TestStatusTellsAKeyCopyOwedAHeal(t *testing.T) {
	out := sealedArchive(t)
	in := health.Gather(out, "")
	if strings.Contains(strings.Join(health.Assess(in, time.Now()).Codes, " "), "encrypted_key_copy_differs") {
		t.Fatal("positive twin: a whole archive owes no heal")
	}
	if !strings.Contains(strings.Join(health.Summary(in, health.Assess(in, time.Now())), "\n"), "-password-file") {
		t.Fatal("the command line's words name its own way in")
	}
	os.Remove(filepath.Join(out, vault.BackupName))
	in = health.Gather(out, "")
	in.Surface = "desktop"
	hr := health.Assess(in, time.Now())
	reasons := strings.Join(hr.Reasons, "\n")
	assure.Reached(t, strings.Contains(strings.Join(hr.Codes, " "), "encrypted_key_copy_differs") && strings.Contains(reasons, "rewrites it from the good one") &&
		strings.Contains(reasons, "MailArchive Desktop with its password or recovery key"), "the heal owed is told, in the Desktop's words")
}

// covers: MA-803, MA-765, R37
// The standing red's record (REC-01, REC-03, REC-04, REC-05, REC-06): a keyless
// check that keeps it standing keeps a scheduled verify fresh; status and the
// attention file name verify -full; a newer keyless red is recorded with the
// standing one beneath it, both told, and the standing one returns to the top once
// the newer is gone; a check with the key that finds only amber removes the red
// notice; a standing red the keyless check can see is not called invisible to it.
func TestAStandingRedKeepsItsClockAndANewerRed(t *testing.T) {
	out := sealedArchive(t)
	tenDays := time.Now().UTC().Add(-10 * 24 * time.Hour)
	red := state.LastVerify{Status: state.VerifyDone, Started: tenDays, Finished: &tenDays, Encrypted: true, Tier: state.TierFull, Outcome: state.OutcomeDamaged}
	state.WriteLastVerify(out, red)
	if _, err := Verify(out, VerifyOptions{}, discard(), nil); err != nil {
		t.Fatal(err)
	}
	in := health.Gather(out, "")
	in.HasVerifyDescriptor = true
	in.VerifyDesc = schedule.Descriptor{Name: "v", Interval: "daily", InstalledAt: tenDays.Add(-24 * time.Hour)}
	hr := health.Assess(in, time.Now())
	reasons := strings.Join(hr.Reasons, "\n")
	attention, _ := os.ReadFile(filepath.Join(out, state.IntegrityAttentionName))
	if strings.Contains(reasons, "verify_stale") || strings.Contains(reasons, "stopped running") || strings.Contains(reasons, "days ago") ||
		!strings.Contains(reasons, "verify -full -out") || !strings.Contains(string(attention), "(mailarchive verify -full)") {
		t.Fatalf("status:\n%s\nattention:\n%s", reasons, attention)
	}

	unit := filepath.Join(out, filepath.FromSlash(aUnitOf(t, out)))
	b, _ := os.ReadFile(unit)
	os.Remove(unit)
	rep, _ := Verify(out, VerifyOptions{}, discard(), nil)
	v, _, _ := state.ReadLastVerify(out)
	hr = health.Assess(health.Gather(out, ""), time.Now())
	if rep.ExitCode() != 2 || v.Tier != state.TierKeyless || v.Outcome != state.OutcomeDamaged || v.Standing == nil || v.Standing.Tier != state.TierFull ||
		!strings.Contains(strings.Join(hr.Reasons, "\n"), "an earlier check with the key") {
		t.Fatalf("a newer red over the standing one: %+v\n%v", v, hr.Reasons)
	}
	os.WriteFile(unit, b, 0o600)
	Verify(out, VerifyOptions{}, discard(), nil)
	if v, _, _ := state.ReadLastVerify(out); v.Tier != state.TierFull || v.Outcome != state.OutcomeDamaged || v.Standing != nil {
		t.Fatalf("the standing red back on top: %+v", v)
	}

	unfinished := red
	unfinished.Outcome = state.OutcomeUnfinishedSave
	state.WriteLastVerify(out, unfinished)
	_, summary := func() (Report, string) {
		rep, _ := Verify(out, VerifyOptions{}, discard(), nil)
		return rep, strings.Join(VerifySummary(rep), "\n")
	}()
	if strings.Contains(summary, "cannot see inside") || !strings.Contains(summary, "it stands until `mailarchive verify -full` passes") {
		t.Fatalf("a standing red the keyless check can see:\n%s", summary)
	}

	state.WriteLastVerify(out, red)
	fullSummary(t, out, &fakeSealed{listOK: true, gap: true})
	_, err := os.Stat(filepath.Join(out, state.IntegrityAttentionName))
	assure.Reached(t, errors.Is(err, fs.ErrNotExist), "a check with the key that finds only amber removes the red notice")
}
