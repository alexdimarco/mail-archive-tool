package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/health"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/memguard"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/vault"
)

// verify -full's verdicts (design-encrypted-archives-phase3 §8, the owner's
// rulings; P3-2b, MA-799).

// fakeSealed is a keyed side whose findings a test sets.
type fakeSealed struct {
	m        *state.Manifest
	loadErr  error
	chunks   []container.ChunkProblem
	list     []container.ListProblem
	listOK   bool
	unitErr  error
	chunkErr error
	// gap and behind are the record's own marks (Marks).
	gap, behind bool
	// onChunks and onUnit run as those reads do, for a stop or a change mid-check.
	onChunks, onUnit func()
	// program and note are the record's program set and note digest (Program).
	program *container.ProgramSet
	note    string
}

func (f *fakeSealed) Marks() (bool, bool)                      { return f.gap, f.behind }
func (f *fakeSealed) Program() (*container.ProgramSet, string) { return f.program, f.note }

func (f *fakeSealed) Load() (*state.Manifest, error) { return f.m, f.loadErr }
func (f *fakeSealed) Chunks(context.Context) ([]container.ChunkProblem, error) {
	if f.onChunks != nil {
		f.onChunks()
	}
	return f.chunks, f.chunkErr
}
func (f *fakeSealed) FileList() ([]container.ListProblem, bool) { return f.list, f.listOK }
func (f *fakeSealed) Unit(_ context.Context, rec state.Record, kind vault.UnitKind, _ state.SealedUnit) (string, error) {
	if f.onUnit != nil {
		f.onUnit()
	}
	return "units/ab/" + rec.Unit, f.unitErr
}

// oneRecord is a manifest with one message of a page and an original.
func oneRecord(t *testing.T) *state.Manifest {
	t.Helper()
	m, err := state.Load(filepath.Join(t.TempDir(), "m.json"))
	if err != nil {
		t.Fatal(err)
	}
	u := &state.SealedUnit{Rev: 1, Size: 10, SHA256: strings.Repeat("0", 64)}
	m.Add("k", state.Record{Path: "s/Inbox/m.html", Folder: "Inbox", Unit: "ab" + strings.Repeat("0", 30), Sealed: &state.SealedSet{HTML: u, EML: u}})
	return m
}

// covers: MA-799, R37
// What tier 2 calls each finding (the owner's rulings): a message missing or
// damaged is red; a message too large to check here, a spare copy of the record, the
// search index, the go-back timeline and the file list are amber, each with its own
// remedy; a refused load keeps its own outcome; each finding is named by its file
// and its part. The verdict is recorded once, tier 2.
func TestAFullCheckCallsEachFindingByTheOwnersRules(t *testing.T) {
	for name, c := range map[string]struct {
		src     fakeSealed
		outcome string
		names   []string
	}{
		"intact":                 {fakeSealed{listOK: true}, state.OutcomeIntact, nil},
		"a message missing":      {fakeSealed{listOK: true, unitErr: fs.ErrNotExist}, state.OutcomeDamaged, []string{"missing", "a message", "restore the whole archive folder"}},
		"a message damaged":      {fakeSealed{listOK: true, unitErr: fmt.Errorf("%w: x", ErrUnitDoesNotOpen)}, state.OutcomeDamaged, []string{"unreadable", "a message's original"}},
		"not its record's bytes": {fakeSealed{listOK: true, unitErr: fmt.Errorf("%w: x", export.ErrUnitStore)}, state.OutcomeDamaged, []string{"unreadable"}},
		"a message too large":    {fakeSealed{listOK: true, unitErr: memguard.ErrTooLarge}, state.OutcomeNotAttested, []string{"too large=2", "memory limit"}},
		// Access or a drive error is not lost mail (the P3-2b pass, CRIT-05).
		"not readable here": {fakeSealed{listOK: true, unitErr: fs.ErrPermission}, state.OutcomeNotAttested, []string{"could not be read here=2", "fix that, then check again"}},
		// What the record itself says it lacks (VERD-05).
		"a go-back gap in the record": {fakeSealed{listOK: true, gap: true}, state.OutcomeNotAttested, []string{"go-back timeline was lost", "messages are intact"}},
		"an index behind its record":  {fakeSealed{listOK: true, behind: true}, state.OutcomeNotAttested, []string{"search index is missing messages", "reindex -rebuild"}},
		"a record copy":               {fakeSealed{listOK: true, chunks: []container.ChunkProblem{{Path: "chunks/aa/x", Section: vault.ContainerManifest, Position: 2, Copy: 2, Kind: "modified"}}}, state.OutcomeNotAttested, []string{"chunks/aa/x", "part 2, copy 2 of 2", "run a backup"}},
		"the search index":            {fakeSealed{listOK: true, chunks: []container.ChunkProblem{{Path: "chunks/bb/y", Section: vault.ContainerIndex, Position: 1, Kind: "missing"}}}, state.OutcomeNotAttested, []string{"the search index, part 1", "reindex -rebuild"}},
		"the go-back timeline":        {fakeSealed{listOK: true, chunks: []container.ChunkProblem{{Path: "chunks/cc/z", Section: vault.ContainerHistory, Position: 0, Kind: "missing"}}}, state.OutcomeNotAttested, []string{"the go-back timeline, part 0", "messages are intact"}},
		"the file list differs":       {fakeSealed{listOK: true, list: []container.ListProblem{{Path: "units/dd/w", Kind: container.ListUnlisted}}}, state.OutcomeNotAttested, []string{"units/dd/w", "file list"}},
		"no file list":                {fakeSealed{}, state.OutcomeNotAttested, []string{"file list could not be compared"}},
		"an unfinished save":          {fakeSealed{loadErr: fmt.Errorf("%w: x", container.ErrUnfinishedSave)}, state.OutcomeUnfinishedSave, nil},
		"a record unreadable":         {fakeSealed{loadErr: fmt.Errorf("%w: x", container.ErrRecord)}, state.OutcomeDamaged, []string{"restore the whole archive folder"}},
	} {
		t.Run(name, func(t *testing.T) {
			out := sealedArchive(t)
			src := c.src
			if src.m == nil {
				src.m = oneRecord(t)
			}
			rep, err := VerifySealed(out, VerifyOptions{}, discard(), nil, &src)
			if err != nil {
				t.Fatal(err)
			}
			summary := strings.Join(VerifySummary(rep), "\n")
			if rep.Outcome != c.outcome || rep.Tier != state.TierFull {
				t.Fatalf("outcome %s tier %d, want %s:\n%s", rep.Outcome, rep.Tier, c.outcome, summary)
			}
			for _, n := range c.names {
				if !strings.Contains(summary, n) {
					t.Fatalf("the summary does not say %q:\n%s", n, summary)
				}
			}
			v, st, _ := state.ReadLastVerify(out)
			assure.Reached(t, st == state.LastVerifyPresent && v.Outcome == c.outcome && v.Tier == state.TierFull && v.Status == state.VerifyDone, "one tier-2 verdict recorded")
		})
	}
}

// covers: MA-799, MA-765, R37
// A red found with the key stands (the owner's ruling): a keyless check afterwards
// that finds nothing writes it back unchanged, says only verify -full can clear it,
// and exits 2; status stays red, naming the check with the key. An amber found with
// the key does not stand: the keyless check's own verdict replaces it.
func TestAKeylessCheckKeepsAFullChecksRed(t *testing.T) {
	out := sealedArchive(t)
	when := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	red := state.LastVerify{Status: state.VerifyDone, Started: when, Finished: &when, Encrypted: true, Tier: state.TierFull, Outcome: state.OutcomeDamaged}
	if err := state.WriteLastVerify(out, red); err != nil {
		t.Fatal(err)
	}
	rep, err := Verify(out, VerifyOptions{}, discard(), nil)
	if err != nil {
		t.Fatal(err)
	}
	summary := strings.Join(VerifySummary(rep), "\n")
	if rep.Outcome != state.OutcomeIntact || rep.StandingOutcome != state.OutcomeDamaged || rep.ExitCode() != 2 || rep.Attested() ||
		!strings.Contains(summary, "on 2026-10-20") || !strings.Contains(summary, "cannot see inside the messages: it stands until `mailarchive verify -full` passes") {
		t.Fatalf("report %+v:\n%s", rep, summary)
	}
	v, _, _ := state.ReadLastVerify(out)
	if v.Tier != state.TierFull || v.Outcome != state.OutcomeDamaged || !v.Finished.Equal(when) {
		t.Fatalf("the red found with the key was replaced: %+v", v)
	}
	in := health.Gather(out, "")
	hr := health.Assess(in, time.Now())
	// The attention file beside the archive says the check read the messages.
	if b, _ := os.ReadFile(filepath.Join(out, state.IntegrityAttentionName)); !strings.Contains(string(b), "opened every file with the archive's key") {
		t.Fatalf("the attention file:\n%s", b)
	}
	if !strings.Contains(strings.Join(hr.Reasons, "\n"), "the last check with the key") ||
		!strings.Contains(strings.Join(hr.Reasons, "\n"), "messages in the archive are missing or damaged") ||
		!strings.Contains(strings.Join(health.Summary(in, hr), "\n"), "(a check with the key)") {
		t.Fatalf("status: %v\n%s", hr.Reasons, strings.Join(health.Summary(in, hr), "\n"))
	}
	if j := health.JSON(in, hr); j.LastVerify == nil || j.LastVerify.Tier != state.TierFull {
		t.Fatalf("status -json: %+v", j.LastVerify)
	}

	amber := red
	amber.Outcome = state.OutcomeNotAttested
	state.WriteLastVerify(out, amber)
	rep, err = Verify(out, VerifyOptions{}, discard(), nil)
	v, _, _ = state.ReadLastVerify(out)
	assure.Reached(t, err == nil && rep.StandingOutcome == "" && v.Tier == state.TierKeyless && v.Outcome == state.OutcomeIntact, "an amber does not stand")
}

// covers: MA-799, R37
// A full check stopped records stopped, carrying the last finished verdict; one
// whose key is locked mid-check records nothing — the last verdict as it was.
func TestAStoppedOrLockedFullCheckChangesNoVerdict(t *testing.T) {
	out := sealedArchive(t)
	when := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	prev := state.LastVerify{Status: state.VerifyDone, Started: when, Finished: &when, Encrypted: true, Tier: state.TierFull, Outcome: state.OutcomeIntact}
	state.WriteLastVerify(out, prev)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := VerifySealed(out, VerifyOptions{Ctx: ctx}, discard(), nil, &fakeSealed{m: oneRecord(t), listOK: true})
	v, _, _ := state.ReadLastVerify(out)
	if !errors.Is(err, ErrVerifyStopped) || v.Status != state.VerifyStopped || v.Previous == nil || v.Previous.Outcome != state.OutcomeIntact {
		t.Fatalf("stopped: %v %+v", err, v)
	}
	// A stop while the chunks are read, or between one message and the next, reads
	// on no further: stopped, not done.
	empty, _ := state.Load(filepath.Join(t.TempDir(), "m.json"))
	for name, src := range map[string]func(cancel func()) *fakeSealed{
		"in the chunks":    func(cancel func()) *fakeSealed { return &fakeSealed{m: empty, listOK: true, onChunks: cancel} },
		"between messages": func(cancel func()) *fakeSealed { return &fakeSealed{m: oneRecord(t), listOK: true, onUnit: cancel} },
	} {
		state.WriteLastVerify(out, prev)
		ctx, cancel := context.WithCancel(context.Background())
		_, err := VerifySealed(out, VerifyOptions{Ctx: ctx}, discard(), nil, src(cancel))
		v, _, _ := state.ReadLastVerify(out)
		if !errors.Is(err, ErrVerifyStopped) || v.Status != state.VerifyStopped {
			t.Fatalf("stopped %s: %v %+v", name, err, v)
		}
	}

	state.WriteLastVerify(out, prev)
	_, err = VerifySealed(out, VerifyOptions{}, discard(), nil, &fakeSealed{m: oneRecord(t), listOK: true, unitErr: ErrCheckLocked})
	v, _, _ = state.ReadLastVerify(out)
	assure.Reached(t, errors.Is(err, ErrCheckLocked) && v.Status == state.VerifyDone && v.Outcome == state.OutcomeIntact && v.Finished.Equal(when), "a lock records nothing")
}

// covers: MA-799, R37
// On a read-only medium the full check runs without the archive lock, judging the
// record before and after; a result it cannot save is told, the exit unchanged
// (§8).
func TestAFullCheckOnAReadOnlyMedium(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("a folder that cannot be written needs a Unix user other than root")
	}
	out := sealedArchive(t)
	orig := verifyLock
	verifyLock = func(path, verb string) (*lockfile.Lock, error) {
		return nil, &fs.PathError{Op: "open", Path: path, Err: syscall.EROFS}
	}
	defer func() { verifyLock = orig }()
	os.Chmod(out, 0o555)
	defer os.Chmod(out, 0o755)
	rep, err := VerifySealed(out, VerifyOptions{}, discard(), nil, &fakeSealed{m: oneRecord(t), listOK: true})
	if err != nil {
		t.Fatal(err)
	}
	summary := strings.Join(VerifySummary(rep), "\n")
	if !rep.NotSaved || rep.Outcome != state.OutcomeIntact || rep.ExitCode() != 0 || !strings.Contains(summary, "could not be saved in the archive (read-only)") {
		t.Fatalf("checked without the lock: %+v\n%s", rep, summary)
	}

	// Without the lock nothing keeps a writer out: a record that moved during the
	// check is not attested.
	mirror := filepath.Join(out, container.MirrorName)
	rep, err = VerifySealed(out, VerifyOptions{}, discard(), nil, &fakeSealed{m: oneRecord(t), listOK: true, onUnit: func() {
		b, _ := os.ReadFile(mirror)
		os.WriteFile(mirror, append(b, ' '), 0o600)
	}})
	assure.Reached(t, err == nil && rep.Outcome == state.OutcomeNotAttested && strings.Contains(strings.Join(VerifySummary(rep), "\n"), "changed while it was checked"),
		"a record that moved during a lock-free check is not attested")
}
