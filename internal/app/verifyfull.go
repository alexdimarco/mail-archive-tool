package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/memguard"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/util"
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
	// Marks is what the loaded record says it lacks: a lost stretch of the go-back
	// timeline, a search index behind its records (the P3-2b pass, VERD-05).
	Marks() (historyGap, indexBehind bool)
	// Program is the record's program set as it stands, nil when none is kept or this
	// MailArchive cannot read it, and the note's sealed digest (program design §6, C18).
	Program() (set *container.ProgramSet, noteDigest string)
	// Unit reads and opens one unit within the memory budget: nil when it opens. A
	// unit whose bytes are its record's but do not open with the key is
	// ErrUnitDoesNotOpen.
	Unit(ctx context.Context, rec state.Record, kind vault.UnitKind, s state.SealedUnit) (path string, err error)
}

// MessageNamer is an optional side of a SealedCheck: a message's date, sender and
// subject as the person knows them, read with the key — from the search index, or
// from another part of the message that opens — so a damaged message is named by
// what it is, not only by its sealed file (P3-8 friction, B19). ok is false when
// nothing could be read; the check then names it by what its record holds.
type MessageNamer interface {
	NameMessage(ctx context.Context, rec state.Record) (MessageName, bool)
}

// MessageName is a message as the person knows it. Any field may be empty.
type MessageName struct {
	Date    string // as the person reads it, e.g. "2026-03-14 09:30 UTC"
	From    string
	Subject string
}

// verifyLock takes the archive lock for verify -full; a var so a test can present
// a read-only medium.
var verifyLock = lockfile.AcquireAs

// ErrCheckLocked is a check whose key was locked before it finished: nothing is
// recorded of it.
var ErrCheckLocked = errors.New("the archive was locked during the check")

// ErrUnitDoesNotOpen is a unit whose bytes are the ones its record names but which
// does not open with the archive's key: a damaged message.
var ErrUnitDoesNotOpen = errors.New("a message's file does not open with the archive's key")

// The advice for each amber finding (the owner's rulings, §8).
const (
	adviceRecordCopy = "run a backup now: it rewrites the damaged copy of the record from the good one, then check again"
	advicePointer    = "run a backup now: it rewrites the record pointer's damaged copy from the good one, then check again"
	adviceIndex      = "run `mailarchive reindex -rebuild` at a terminal: it rebuilds the search index from the messages"
	adviceIndexMark  = "run a backup or `mailarchive reindex -rebuild` at a terminal: either brings the search index up to date"
	adviceHistory    = "nothing restores the go-back timeline but a backup copy of the whole folder: your messages are intact, and go-back to past dates stays partial"
	adviceFileList   = "run a backup now: it writes the archive's file list again, then check again"
	adviceTooLarge   = "this version of MailArchive cannot open a message larger than its memory limit for one message; the rest of the archive was checked"
	adviceUnreadable = "the messages named above could not be read on this computer (access to the drive, or a drive error): fix that, then check again"
	adviceChunksRead = "check again; if it happens again, run a backup"
	adviceMoved      = "check it again once nothing is writing to it"
	adviceLoad       = "restore the whole archive folder from your newest backup copy"
)

// adviceUnits is the remedy for messages missing or damaged: first the cheaper repair
// — a backup with -mode full from the mail they came from writes every message again,
// the damaged ones included — then the restore (P3-8 friction, B19).
func adviceUnits(out string) string {
	return fmt.Sprintf("the messages named above are missing or damaged. If you still have the mail they came from, a backup with -mode full from that mail writes them again: run `mailarchive -input <your mail> -out %s -mode full`, then `mailarchive verify -full -out %s` to check. Otherwise restore the whole archive folder from your newest backup copy", encmark.CommandPath(out), encmark.CommandPath(out))
}

// genericAdvice says advice is outcome's fixed sentence — a check's general remedy,
// which a finding's own remedy replaces, so the remedy is said once (P3-8 friction,
// B19).
func genericAdvice(outcome, advice string) bool {
	return advice == state.OutcomeText(outcome) || advice == state.OutcomeTextFor(outcome, state.TierFull)
}

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

// loadAdvice is a refused load's remedy: its outcome's own sentence, and the
// restore only for a record that is damaged (the P3-2b pass, OUT-3).
func loadAdvice(outcome string) string {
	if outcome == state.OutcomeDamaged {
		return adviceLoad
	}
	return state.OutcomeText(outcome)
}

// VerifySealed is verify -full: the check with the key, through src. Under the
// archive lock, or — on a read-only medium, where the lock cannot be taken and no
// one else holds it — without it, judging the record's pointers before and after.
// A stop is recorded as stopped, carrying the last finished verdict; a lock of the
// key records nothing: the record is put back as it was.
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
	lockPath := filepath.Join(out, lockfile.Name)
	lockFree := false
	lock, lerr := verifyLock(lockPath, "verify")
	switch {
	case lerr == nil:
		defer lock.Release()
	case export.FatalKindAt(lerr, out) == export.FatalReadOnly:
		// A read-only medium: nothing can write it through this path, the check
		// included. Another path to the same folder may be writing it — a backup on
		// another computer holds the lock there (the P3-2b pass, OUT-4).
		if lockfile.HeldReadOnly(lockPath) {
			return Report{}, fmt.Errorf("%w: %s is being written by another MailArchive; check it once that finishes", lockfile.ErrHeld, out)
		}
		lockFree = true
	default:
		return Report{}, lerr
	}
	var before container.Peeked
	if lockFree {
		before, _ = container.PeekFile(out)
	}
	started := time.Now().UTC()
	// The record as it is, to put back exactly if the key is locked mid-check — none
	// included (the P3-2b pass, KEY-01, REC-02).
	putBack := state.SnapshotLastVerify(out)
	previous := state.LastVerdict(out)
	_ = state.WriteLastVerify(out, state.LastVerify{Status: state.VerifyRunning, Started: started, Encrypted: true, Tier: state.TierFull, Previous: previous})
	stopped := func() (Report, error) {
		finished := time.Now().UTC()
		_ = state.WriteLastVerify(out, state.LastVerify{Status: state.VerifyStopped, Started: started, Finished: &finished, Encrypted: true, Tier: state.TierFull, Previous: previous})
		return Report{Out: out, Encrypted: true, Tier: state.TierFull}, fmt.Errorf("%w: %s — nothing was changed; the last finished check's result still stands", ErrVerifyStopped, out)
	}
	locked := func(err error) (Report, error) {
		_ = putBack()
		return Report{}, err
	}

	c, cerr := container.CheckKeylessContext(ctx, out, 0)
	if cerr != nil {
		return stopped()
	}
	rep := keylessReport(out, c)
	rep.Tier = state.TierFull
	f := &fullCheck{rep: &rep, shown: len(rep.Problems)}
	rep.Outcome, rep.Reason, rep.Advice = "", "", ""

	// The keyless verdict, with its own remedy: tier 2 never puts its "messages are
	// damaged" sentence on a red no message caused (the P3-2b pass, VERD-01, VERD-03).
	keylessAdvice := c.Advice
	if keylessAdvice == "" && c.Outcome != state.OutcomeIntact {
		keylessAdvice = state.OutcomeText(c.Outcome)
	}

	m, err := src.Load()
	switch {
	case errors.Is(err, ErrCheckLocked):
		return locked(err)
	case err != nil:
		f.raise(c.Outcome, c.Reason, keylessAdvice)
		o := loadOutcome(err)
		f.raise(o, err.Error(), loadAdvice(o))
	default:
		rep.KeyedLoad = true
		// A copy of the record pointer the keyless check could not match: the keyed
		// load authenticated a pointer, so it is a spare copy, amber (FID-01, CRIT-01).
		if c.PointerCopyOnly() {
			f.raise(state.OutcomeNotAttested, c.Reason, advicePointer)
		} else {
			f.raise(c.Outcome, c.Reason, keylessAdvice)
		}
		chunks, err := src.Chunks(ctx)
		switch {
		case ctx.Err() != nil:
			return stopped()
		case errors.Is(err, ErrCheckLocked):
			return locked(err)
		case err != nil:
			// A pass that ended early attests none of the chunks it did not open
			// (the P3-2b pass, CRIT-04).
			f.raise(state.OutcomeNotAttested, "the record's files could not all be read: "+err.Error(), adviceChunksRead)
			f.chunksUnread = true
		}
		copies := map[uint32]int{}
		for _, p := range chunks {
			f.chunk(p)
			if p.Section == vault.ContainerManifest {
				copies[p.Position]++
			}
		}
		for _, n := range copies {
			if n == 2 {
				// Both copies of a part of the record: it no longer loads (OUT-2).
				f.raise(state.OutcomeDamaged, "both copies of a part of the archive's record are missing or damaged", adviceLoad)
			}
		}
		if lp, ok := src.FileList(); !ok {
			f.raise(state.OutcomeNotAttested, "the archive's file list could not be compared with its record", adviceFileList)
		} else {
			for _, p := range lp {
				f.problem(p.Path, "file list: "+p.Kind, "")
				f.raise(state.OutcomeNotAttested, "the archive's file list does not match its record", adviceFileList)
			}
		}
		gap, behind := src.Marks()
		if gap {
			f.raise(state.OutcomeNotAttested, "part of the go-back timeline was lost", adviceHistory)
		}
		if behind {
			f.raise(state.OutcomeNotAttested, "the search index is missing messages", adviceIndexMark)
		}
		if err := f.units(ctx, m, src, onProgress); err != nil {
			if errors.Is(err, ErrCheckLocked) {
				return locked(err)
			}
			return stopped()
		}
		set, note := src.Program()
		rep.ProgramFindings = checkProgramFolder(out, set, note)
	}
	moved := false
	if lockFree {
		if after, _ := container.PeekFile(out); after != before {
			// The archive moved under the check: nothing it found can be trusted, a
			// red included (the P3-2b pass, OUT-1).
			rep.Outcome, rep.Reason, rep.Advice = state.OutcomeNotAttested, "the archive changed while it was checked", adviceMoved
			moved = true
		}
	}
	// A red found with the key stands until a check with the key passes (§8): an amber
	// check that did not open every message — the record not loaded, a message unread
	// or too large, the record's files not all read, the archive moved — keeps it
	// standing beneath, as a check without the key does (the P3-3 pass, CRIT-04).
	var standing *state.LastVerify
	if !state.OutcomeIsRed(rep.Outcome) && (!rep.KeyedLoad || rep.Unread > 0 || rep.TooLarge > 0 || f.chunksUnread || moved) {
		if standing = state.StandingRed(previous); standing != nil {
			rep.StandingOutcome, rep.StandingSince = standing.Outcome, standing.Finished
		}
	}
	if onProgress != nil {
		onProgress(rep)
	}
	finished := time.Now().UTC()
	if err := state.WriteLastVerify(out, state.LastVerify{Status: state.VerifyDone, Started: started, Finished: &finished, Encrypted: true, Tier: state.TierFull, Outcome: rep.Outcome, Standing: standing}); err != nil {
		rep.NotSaved = true // a read-only medium: the result is told, not kept (§8)
	}
	return rep, nil
}

// fullCheck accumulates a tier-2 run's verdict.
type fullCheck struct {
	rep          *Report
	shown        int
	chunksUnread bool // the pass over the record's files ended early
}

// raise records outcome when it is worse than the run's so far, with its reason and
// advice; an equal one adds its reason and its advice, so each finding is named
// with its own remedy (the P3-2b pass, FID-06).
func (f *fullCheck) raise(outcome, reason, advice string) {
	if outcome == "" {
		return
	}
	switch r := outcomeRank(outcome); {
	case f.rep.Outcome == "" || r > outcomeRank(f.rep.Outcome):
		f.rep.Outcome, f.rep.Reason, f.rep.Advice = outcome, reason, advice
	case r == outcomeRank(f.rep.Outcome) && r > 0:
		if reason != "" && !strings.Contains(f.rep.Reason, reason) {
			f.rep.Reason += "; " + reason
		}
		switch {
		case advice == "" || strings.Contains(f.rep.Advice, advice):
		case genericAdvice(outcome, advice) && f.rep.Advice != "":
			// The general remedy beside a finding's own: said once, by the finding.
		case f.rep.Advice == "" || genericAdvice(outcome, f.rep.Advice):
			f.rep.Advice = advice
		default:
			f.rep.Advice += "; " + advice
		}
	}
}

// messageLabel names rec's message as the person knows it (P3-8 friction, B19): its
// date, sender and subject as src reads them with the key, else what its record
// holds — the date and subject its file name was made from — and its folder.
func (f *fullCheck) messageLabel(ctx context.Context, src SealedCheck, rec state.Record) string {
	var n MessageName
	if namer, ok := src.(MessageNamer); ok {
		n, _ = namer.NameMessage(ctx, rec)
	}
	date, subject := recordDateSubject(rec)
	if n.Date == "" {
		n.Date = date
	}
	var parts []string
	if d := plainLabel(n.Date, 40); d != "" {
		parts = append(parts, "dated "+d)
	}
	if from := plainLabel(n.From, 80); from != "" {
		parts = append(parts, "from "+from)
	}
	switch {
	case plainLabel(n.Subject, 80) != "":
		parts = append(parts, "subject “"+plainLabel(n.Subject, 80)+"”")
	case rec.Subject != "":
		parts = append(parts, "subject “"+plainLabel(rec.Subject, 80)+"”")
	case subject != "":
		parts = append(parts, "subject like “"+subject+"”")
	}
	if folder := plainLabel(rec.Folder, 80); folder != "" {
		parts = append(parts, "in "+folder)
	}
	if len(parts) == 0 {
		return "a message whose date, sender and subject could not be read"
	}
	return "the message " + strings.Join(parts, ", ")
}

// plainLabel is s as one line for the terminal: no control characters and no
// right-to-left overrides — it is echoed from mail (F16; the P3-8 pass,
// LEAK-B19-BIDI) — at most max runes.
func plainLabel(s string, max int) string {
	s = strings.Join(strings.Fields(util.StripBidi(util.StripControl(s))), " ")
	if r := []rune(s); len(r) > max {
		s = string(r[:max-1]) + "…"
	}
	return s
}

// recordDateSubject is the date and the subject a record's own fields, or else its
// file name (<date>_<time>_<subject>_<hash>.html), hold.
func recordDateSubject(rec state.Record) (date, subject string) {
	if t, err := time.Parse(time.RFC3339, rec.Date); err == nil {
		date = t.UTC().Format("2006-01-02 15:04") + " UTC"
	}
	base := strings.TrimSuffix(path.Base(filepath.ToSlash(rec.Path)), ".html")
	parts := strings.Split(base, "_")
	if len(parts) < 3 {
		return date, ""
	}
	if t, err := time.Parse("2006-01-02_1504", parts[0]+"_"+parts[1]); date == "" && err == nil {
		date = t.Format("2006-01-02 15:04") + " UTC"
	}
	if len(parts) >= 4 {
		subject = plainLabel(strings.ReplaceAll(strings.Join(parts[2:len(parts)-1], "_"), "-", " "), 80)
	}
	return date, subject
}

// slashPath is an archive-relative path with forward slashes, whatever separator
// its maker used: a unit's or chunk's path holds only hex names and slashes, so a
// path the keyless check named and one the keyed side named are one string for one
// file (FID-04 on Windows; P3-8 friction).
func slashPath(p string) string { return strings.ReplaceAll(filepath.ToSlash(p), `\`, "/") }

// problem names a damaged file and its part, within the report's cap; a file the
// keyless check named already is named once, with its part (FID-04).
func (f *fullCheck) problem(path, kind, part string) {
	path = slashPath(path)
	for i := range f.rep.Problems {
		if slashPath(f.rep.Problems[i].Path) == path {
			f.rep.Problems[i].Kind, f.rep.Problems[i].Detail = kind, part
			return
		}
	}
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
// a message missing, or whose file is not its record's or does not open, is red;
// one too large for the budget, or that this computer could not read, is amber.
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
			// A message the check could not open is named as the person knows it too
			// (P3-8 friction, B19), read only for such a message.
			part := func() string { return unitPart(u.kind) + ": " + f.messageLabel(ctx, src, rec) }
			switch {
			case err == nil:
				f.rep.OK++
			case errors.Is(err, ErrCheckLocked):
				return err
			case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
				return err
			case errors.Is(err, memguard.ErrTooLarge):
				f.rep.TooLarge++
				f.problem(path, "too large to check here", part())
				f.raise(state.OutcomeNotAttested, "a message is too large to check", adviceTooLarge)
			case errors.Is(err, fs.ErrNotExist):
				f.rep.Missing++
				f.problem(path, "missing", part())
				f.raise(state.OutcomeDamaged, "messages are missing from the archive", adviceUnits(f.rep.Out))
			case errors.Is(err, export.ErrUnitStore), errors.Is(err, ErrUnitDoesNotOpen):
				f.rep.Modified++
				f.problem(path, "unreadable", part())
				f.raise(state.OutcomeDamaged, "messages in the archive are damaged", adviceUnits(f.rep.Out))
			default:
				// Access or a drive error is not lost mail: a red here would stand
				// after the drive is back (the P3-2b pass, CRIT-05).
				f.rep.Unread++
				f.problem(path, "could not be read here", part())
				f.raise(state.OutcomeNotAttested, "messages could not be read on this computer", adviceUnreadable)
			}
			if onProgress != nil && f.rep.Checked%256 == 0 {
				onProgress(*f.rep)
			}
		}
	}
	return nil
}
