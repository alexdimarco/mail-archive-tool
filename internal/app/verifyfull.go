package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/memguard"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/vault"
)

// verify -full: an encrypted archive's check with its key (tier 2,
// design-encrypted-archives-phase3 §8, and the owner's rulings there). It runs the
// keyless check first, then reads the record with the key, opens every chunk and
// every unit, and compares the record with the file list — naming each damaged
// file and its part on the terminal, recording only the one verdict (A18). It is
// read-only: it heals nothing.

// SealedCheck is an encrypted archive's side of verify -full, which internal/keyring
// provides so that no key leaves it: each call holds the key only while it reads.
type SealedCheck interface {
	// Load reads the record read-only, with its unit files listed for the
	// cross-check. A refusal is the check's outcome.
	Load() (*state.Manifest, error)
	// Chunks opens every chunk the record names.
	Chunks(ctx context.Context) ([]container.ChunkProblem, error)
	// FileList compares the files the record names with the archive's file list; ok
	// false when it cannot be compared.
	FileList() (problems []container.ListProblem, ok bool)
	// Unit reads and opens one unit within the memory budget: nil when it opens.
	Unit(ctx context.Context, rec state.Record, kind vault.UnitKind, s state.SealedUnit) (path string, err error)
}

// verifyLock takes the archive lock for verify -full; a var so a test can present
// a read-only medium.
var verifyLock = lockfile.AcquireAs

// ErrCheckLocked is a check whose key was locked before it finished: nothing is
// recorded of it.
var ErrCheckLocked = errors.New("the archive was locked during the check")

// The advice for each amber finding (the owner's rulings, §8).
const (
	adviceRecordCopy = "run a backup now: it rewrites the damaged copy of the record from the good one, then check again"
	adviceIndex      = "run `mailarchive reindex -rebuild` at a terminal: it rebuilds the search index from the messages"
	adviceHistory    = "nothing restores it but a backup copy of the whole folder: your messages are intact, and go-back to past dates stays partial"
	adviceFileList   = "run a backup now: it writes the archive's file list again, then check again"
	adviceTooLarge   = "check it on a computer with more memory"
	adviceLoad       = "restore the whole archive folder from your newest backup copy"
	adviceUnits      = "restore the whole archive folder from your newest backup copy: the messages named above are missing or damaged"
)

// outcomeRank orders outcomes from best to worst: one run records the worse.
func outcomeRank(o string) int {
	switch {
	case o == state.OutcomeIntact:
		return 0
	case o == state.OutcomeNotAttested:
		return 1
	case o == state.OutcomeNewerFormat:
		return 2
	case state.OutcomeIsRed(o):
		return 3
	}
	return 1
}

// unitPart names a unit's part.
func unitPart(kind vault.UnitKind) string {
	switch kind {
	case vault.UnitAttachment:
		return "a message's attachments"
	case vault.UnitOriginal:
		return "a message's original"
	}
	return "a message"
}

// chunkPart names a chunk's part of the record.
func chunkPart(p container.ChunkProblem) string {
	switch p.Section {
	case vault.ContainerHistory:
		return fmt.Sprintf("the go-back timeline, part %d", p.Position)
	case vault.ContainerIndex:
		return fmt.Sprintf("the search index, part %d", p.Position)
	}
	return fmt.Sprintf("the record, part %d, copy %d of 2", p.Position, p.Copy)
}

// loadOutcome is a refused load's outcome: its own where the keyless list has one,
// damaged otherwise.
func loadOutcome(err error) string {
	switch {
	case errors.Is(err, container.ErrUnfinishedSave):
		return state.OutcomeUnfinishedSave
	case errors.Is(err, container.ErrRestoredOlder):
		return state.OutcomeRestoredOlder
	case errors.Is(err, container.ErrNewerFormat):
		return state.OutcomeNewerFormat
	}
	return state.OutcomeDamaged
}

// VerifySealed is verify -full: the check with the key, through src. Under the
// archive lock, or — on a read-only medium, where the lock cannot be taken —
// without it, judging the record's pointers before and after. A stop is recorded as
// stopped, carrying the last finished verdict; a lock of the key records nothing.
func VerifySealed(out string, opts VerifyOptions, logger *log.Logger, onProgress func(Report), src SealedCheck) (Report, error) {
	ctx := opts.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	if src == nil {
		return Report{}, errors.New("app: VerifySealed needs the archive's sealed side")
	}
	if !encryptedArchiveAt(out) {
		return Report{}, fmt.Errorf("%s is not an encrypted archive this computer can check with a key", out)
	}
	lockFree := false
	lock, lerr := verifyLock(filepath.Join(out, lockfile.Name), "verify")
	switch {
	case lerr == nil:
		defer lock.Release()
	case export.FatalKindAt(lerr, out) == export.FatalReadOnly:
		lockFree = true // a read-only medium: nothing can write it, the check included
	default:
		return Report{}, lerr
	}
	var before container.Peeked
	if lockFree {
		before, _ = container.PeekFile(out)
	}
	started := time.Now().UTC()
	previous := state.LastVerdict(out)
	_ = state.WriteLastVerify(out, state.LastVerify{Status: state.VerifyRunning, Started: started, Encrypted: true, Tier: state.TierFull, Previous: previous})
	stopped := func() (Report, error) {
		finished := time.Now().UTC()
		_ = state.WriteLastVerify(out, state.LastVerify{Status: state.VerifyStopped, Started: started, Finished: &finished, Encrypted: true, Tier: state.TierFull, Previous: previous})
		return Report{Out: out, Encrypted: true, Tier: state.TierFull}, fmt.Errorf("%w: %s — nothing was changed; the last finished check's result still stands", ErrVerifyStopped, out)
	}
	restore := func() {
		// A lock of the key mid-check: the last finished verdict is put back as it was.
		if previous != nil {
			_ = state.WriteLastVerify(out, *previous)
		}
	}

	c, cerr := container.CheckKeylessContext(ctx, out, 0)
	if cerr != nil {
		return stopped()
	}
	rep := keylessReport(out, c)
	rep.Tier = state.TierFull
	f := &fullCheck{rep: &rep}
	f.raise(c.Outcome, c.Reason, c.Advice)

	m, err := src.Load()
	switch {
	case errors.Is(err, ErrCheckLocked):
		restore()
		return Report{}, err
	case err != nil:
		f.raise(loadOutcome(err), err.Error(), adviceLoad)
	default:
		chunks, err := src.Chunks(ctx)
		if ctx.Err() != nil {
			return stopped()
		}
		if errors.Is(err, ErrCheckLocked) {
			restore()
			return Report{}, err
		}
		for _, p := range chunks {
			f.chunk(p)
		}
		if lp, ok := src.FileList(); !ok {
			f.raise(state.OutcomeNotAttested, "the archive's file list could not be compared with its record", adviceFileList)
		} else {
			for _, p := range lp {
				f.problem(p.Path, "file list: "+p.Kind, "")
				f.raise(state.OutcomeNotAttested, "the archive's file list does not match its record", adviceFileList)
			}
		}
		if err := f.units(ctx, m, src, onProgress); err != nil {
			if errors.Is(err, ErrCheckLocked) {
				restore()
				return Report{}, err
			}
			return stopped()
		}
	}
	if lockFree {
		if after, _ := container.PeekFile(out); after != before {
			f.raise(state.OutcomeNotAttested, "the archive changed while it was checked", "check it again")
		}
	}
	if onProgress != nil {
		onProgress(rep)
	}
	finished := time.Now().UTC()
	if err := state.WriteLastVerify(out, state.LastVerify{Status: state.VerifyDone, Started: started, Finished: &finished, Encrypted: true, Tier: state.TierFull, Outcome: rep.Outcome}); err != nil {
		rep.NotSaved = true // a read-only medium: the result is told, not kept (§8)
	}
	return rep, nil
}

// fullCheck accumulates a tier-2 run's verdict.
type fullCheck struct {
	rep   *Report
	shown int
}

// raise records outcome when it is worse than the run's so far, with its reason and
// advice; an equal one adds its advice.
func (f *fullCheck) raise(outcome, reason, advice string) {
	switch r := outcomeRank(outcome); {
	case f.rep.Outcome == "" || r > outcomeRank(f.rep.Outcome):
		f.rep.Outcome, f.rep.Reason, f.rep.Advice = outcome, reason, advice
	case r == outcomeRank(f.rep.Outcome) && advice != "" && f.rep.Advice != advice && r > 0:
		if f.rep.Advice == "" {
			f.rep.Advice = advice
		} else if !strings.Contains(f.rep.Advice, advice) {
			f.rep.Advice += "; " + advice
		}
	}
}

// problem names a damaged file and its part, within the report's cap.
func (f *fullCheck) problem(path, kind, part string) {
	if f.shown >= maxVerifyDetail {
		if f.rep.Truncated == nil {
			f.rep.Truncated = map[string]int{}
		}
		f.rep.Truncated[kind]++
		return
	}
	f.shown++
	f.rep.Problems = append(f.rep.Problems, Problem{Path: path, Kind: kind, Detail: part})
}

// chunk judges a chunk the check could not read (the owner's rulings): a copy of the
// record, the search index and the go-back timeline are amber, each with its remedy.
func (f *fullCheck) chunk(p container.ChunkProblem) {
	f.problem(p.Path, p.Kind, chunkPart(p))
	switch p.Section {
	case vault.ContainerHistory:
		f.raise(state.OutcomeNotAttested, "part of the go-back timeline cannot be read", adviceHistory)
	case vault.ContainerIndex:
		f.raise(state.OutcomeNotAttested, "part of the search index cannot be read", adviceIndex)
	default:
		f.raise(state.OutcomeNotAttested, "a copy of a part of the archive's record cannot be read; the other copy is good", adviceRecordCopy)
	}
}

// units opens every unit of every record, one at a time within the memory budget:
// a message missing or damaged is red; one too large for this computer is amber.
func (f *fullCheck) units(ctx context.Context, m *state.Manifest, src SealedCheck, onProgress func(Report)) error {
	all := m.All()
	keys := make([]string, 0, len(all))
	for k := range all {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	f.rep.Checked, f.rep.OK, f.rep.Missing, f.rep.Modified = 0, 0, 0, 0
	for _, k := range keys {
		rec := all[k]
		if rec.Sealed == nil {
			continue
		}
		for _, u := range []struct {
			kind vault.UnitKind
			s    *state.SealedUnit
		}{{vault.UnitMessage, rec.Sealed.HTML}, {vault.UnitAttachment, rec.Sealed.Zip}, {vault.UnitOriginal, rec.Sealed.EML}} {
			if u.s == nil {
				continue
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			path, err := src.Unit(ctx, rec, u.kind, *u.s)
			f.rep.Checked++
			switch {
			case err == nil:
				f.rep.OK++
			case errors.Is(err, ErrCheckLocked):
				return err
			case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
				return err
			case errors.Is(err, memguard.ErrTooLarge):
				f.problem(path, "too large to check here", unitPart(u.kind))
				f.raise(state.OutcomeNotAttested, "a message is too large to check on this computer", adviceTooLarge)
			case errors.Is(err, fs.ErrNotExist):
				f.rep.Missing++
				f.problem(path, "missing", unitPart(u.kind))
				f.raise(state.OutcomeDamaged, "messages are missing from the archive", adviceUnits)
			default:
				f.rep.Modified++
				f.problem(path, "unreadable", unitPart(u.kind))
				f.raise(state.OutcomeDamaged, "messages in the archive are damaged", adviceUnits)
			}
			if onProgress != nil && f.rep.Checked%256 == 0 {
				onProgress(*f.rep)
			}
		}
	}
	return nil
}
