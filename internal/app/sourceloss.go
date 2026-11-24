package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"mail-archive-tool/internal/autoplace"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/thunderbird"
)

// sourceReach is a run's verdict about whether it REACHED the archive's source (§5.7, S3-C2).
type sourceReach int

const (
	// sourceInconclusive: the run failed for a reason that says nothing about the source (a
	// search-index refusal, a config error) or was interrupted — leave the streak unchanged,
	// never falsely reset or extend it.
	sourceInconclusive sourceReach = iota
	// sourceReached: the run read the source (even an empty/quiet one is a reach) — reset.
	sourceReached
	// sourceUnreachable: the source could not be reached — extend the streak.
	sourceUnreachable
)

// sourceUnreachableError marks a run failure caused by the archive's SOURCE being unreachable
// at input discovery — a local mail-store folder that is gone or on an unmounted drive. The
// message stays the operator-facing one; the type lets classifySourceReach recognise it
// (S3-C2) so the streak climbs, and the streak's run count then sorts a transient absence (a
// drive briefly unplugged) from a permanent loss (S3-C3).
//
// movedTo, when set, is where Thunderbird now keeps the gone input's account, which was moved with
// its Local Directory setting, not removed (MA-1481; the owner's Q7): the words say so.
// sameServer: only the server-name rule found the move (RF2-01's other words).
//
// one: an archive of one listed Thunderbird folder, auto-discovery off, whose account now keeps its
// mail in movedTo, with the folder still there (C2) or gone (F-08): autoplace.NoLongerUsedText's
// words. removed: such an archive's folder is gone with its account (C13): autoplace.RemovedText's
// words (docs/design-per-mailbox-archives.md §11; MA-1504, MA-1506).
type sourceUnreachableError struct {
	path, movedTo string
	sameServer    bool
	one, removed  bool
}

// The reason codes a plaintext refused record keeps (state.LastRun.SourceReason; revision 2.1's
// F-03): every moved stop, and C13's removed account. No name is in either.
func (e *sourceUnreachableError) sourceReason() string {
	switch {
	case e.removed:
		return state.SourceReasonRemoved
	case e.movedTo != "":
		return state.SourceReasonMoved
	}
	return ""
}

// sourceReasonOf is the reason code of a run's refusal cause: "" unless it is a moved or removed
// account's typed stop.
func sourceReasonOf(cause error) string {
	var sue *sourceUnreachableError
	if errors.As(cause, &sue) {
		return sue.sourceReason()
	}
	return ""
}

// pathLeadsNowhere reports whether a look at a listed input failed because the path, as spelled,
// leads to nothing: a component that is not a folder (ENOTDIR), or on Windows ERROR_DIRECTORY (267,
// "The directory name is invalid"), which a dangling link spelled with a trailing "\" gives. A
// permission error or any other failure to look is not this: it says nothing about whether the
// source is there (classifySourceReach).
func pathLeadsNowhere(err error) bool {
	if errors.Is(err, syscall.ENOTDIR) {
		return true
	}
	var errno syscall.Errno
	return runtime.GOOS == "windows" && errors.As(err, &errno) && errno == 267
}

// Error is autoplace.GoneText, unchanged, so whatever compares a record's error with an input's
// refusal (health.GoneMailboxRepairable) compares one text; for a moved account, autoplace.MovedText
// (or, for an archive of one folder, NoLongerUsedText); for a removed one (C13), RemovedText.
func (e *sourceUnreachableError) Error() string {
	if e.removed {
		return autoplace.RemovedText(e.path)
	}
	if e.movedTo != "" && e.one {
		return autoplace.NoLongerUsedText(e.path, e.movedTo)
	}
	if e.movedTo != "" && e.sameServer {
		return autoplace.MovedSameServerText(e.path, e.movedTo)
	}
	if e.movedTo != "" {
		return autoplace.MovedText(e.path, e.movedTo)
	}
	return autoplace.GoneText(e.path)
}

// classifySourceReach decides, from a finished run's error, whether the run reached the
// archive's SOURCE. A clean run reached it (even an empty mailbox is a reach, so a healthy
// quiet source never reads as source-loss). An M365 breaker outage is unreachable. A
// cancellation, a search-index refusal, or any other failure is inconclusive about the source.
//
// A gone LOCAL source (a Thunderbird/mbox folder that is missing or on an unmounted drive) is
// unreachable too, via the typed sourceUnreachableError that DiscoverInputs returns (S3-C2);
// the streak's run count then sorts a transient absence from a permanent loss (S3-C3). A
// present-but-unreadable source (a lock or permission error) is NOT "gone", so it stays
// inconclusive and never false-positives as a lost source.
func classifySourceReach(err error) (sourceReach, string) {
	switch {
	case err == nil:
		return sourceReached, ""
	case errors.Is(err, context.Canceled):
		return sourceInconclusive, ""
	default:
		var outage *outageError
		if errors.As(err, &outage) {
			return sourceUnreachable, "m365-unreachable"
		}
		var sue *sourceUnreachableError
		if errors.As(err, &sue) {
			return sourceUnreachable, "local-source-unreachable"
		}
		return sourceInconclusive, ""
	}
}

// FoundSourceUnreachable reports whether a scheduled run that ended with err found the archive's
// source unreachable — the verdict that extends the source streak. Auto-pause acts only after
// such a run (MA-890): a failure that says nothing about the source never pauses on an old streak.
func FoundSourceUnreachable(err error) bool {
	reach, _ := classifySourceReach(err)
	return reach == sourceUnreachable
}

// skippedInputs are the inputs DiscoverInputs skipped as gone, each with the account's name when
// the Thunderbird profile's settings still name an account for that folder (MA-1471), read as the
// wizard's list reads it (thunderbird.FindProfileDir, ReadStoreFacts), cleaned and capped as the
// list says it; "" for a folder no account names (an account removed on purpose) or one outside a
// profile.
func skippedInputs(gone []string) []SkippedInput {
	out := make([]SkippedInput, 0, len(gone))
	for _, p := range gone {
		name := goneAccountName(p)
		s := SkippedInput{Path: p, Name: name, Listed: name != ""}
		// Settings that cannot be read for certain tell no move (the re-verify's RV-04): said.
		if profile, ok := thunderbird.FindProfileDir(p); ok && thunderbird.SettingsUncertain(profile) {
			s.SettingsUnsure = true
		}
		out = append(out, s)
	}
	return out
}

// SkippedLines are the end-of-run summaries' words for the inputs a run that finished skipped
// (docs/design-1.7.5-fixes.md §1.3, MA-1471; X6: the count status says): how many, that they are
// gone and their saved mail stays, and that the other mailboxes were backed up (true when said:
// only a run that finished is summed up); then one line per input, "NAME (PATH)"; then what to do
// if one was not removed on purpose, naming the mail program whose place they lie in when they all
// lie in one's. names false says each by its path alone: a summary written to a log file never
// holds the name Thunderbird shows for an account (MA-1288). None when nothing was skipped. The
// wizard's summary, Run now's notes (plaintext and encrypted, in memory only) and the command
// line's summary all say these.
func SkippedLines(skipped []SkippedInput, names bool) []string {
	n := len(skipped)
	if n == 0 {
		return nil
	}
	lines := []string{fmt.Sprintf("Not found, so skipped: %d mailboxes. They are no longer on this computer, and the mail already saved from them stays in the archive; the other mailboxes were backed up:", n)}
	if n == 1 {
		lines[0] = "Not found, so skipped: 1 mailbox. It is no longer on this computer, and the mail already saved from it stays in the archive; the other mailboxes were backed up:"
	}
	program := autoplace.ProgramOf(skipped[0].Path)
	for _, s := range skipped {
		label := s.Path
		if names && s.Name != "" {
			label = s.Label()
		}
		lines = append(lines, label)
		if autoplace.ProgramOf(s.Path) != program {
			program = ""
		}
	}
	where := "in its mail program"
	if program != "" {
		where = "in " + program
	}
	// The 1.7.5 friction review's F4 and F9, as status says them: accounts Thunderbird still lists
	// are told to let it download their mail; an Outlook data file (.pst) moved away, to put it back
	// or choose it in its new place.
	listed := program == autoplace.Thunderbird
	for _, s := range skipped {
		listed = listed && s.Listed
	}
	switch {
	case listed && n == 1:
		return append(lines, "Thunderbird still lists this account: open Thunderbird and let it download the account's mail; the next backup finds it again.")
	case listed:
		return append(lines, "Thunderbird still lists these accounts: open Thunderbird and let it download their mail; the next backup finds them again.")
	case n == 1 && program == autoplace.Outlook && strings.EqualFold(filepath.Ext(skipped[0].Path), ".pst"):
		return append(lines, autoplace.PstMovedText(skipped[0].Path)) // RF-05, as status says it
	case n == 1:
		return append(lines, "If you didn't remove it on purpose, add its account back "+where+".")
	}
	return append(lines, "If you didn't remove one on purpose, add its account back "+where+".")
}

// resolveGoneSince sets each skipped input's Since: when it was first found gone (MA-1472),
// resolved once, straight after discovery, from what is on disk before this run writes anything
// (docs/design-1.7.5-fixes.md §1.3):
//
//  1. the previous last-run record's mailbox_gone warning for the same input, compared by
//     autoplace.GoneSourceID; a warning whose path is not a plain local one (autoplace.NonLocal)
//     is passed over before any look at it (MA-869, OUT-01);
//  2. else, when the source-loss streak's last kind is local-source-unreachable and the previous
//     record is a failed run whose error is this input's own refusal (autoplace.GoneText), the
//     streak's FirstUnreachable: when the archive that was failing first found it gone;
//  3. else this run's start.
//
// A time before state.MinPlausibleTime (a clock that was not set) becomes start and is never
// carried; a time later than start is kept as recorded (clampGoneSince, CON-02, Q8). The
// previous record is read before the lock: it is written whole, by a rename, and a run that holds
// the lock meanwhile makes this one refuse at the lock, writing nothing. An encrypted archive's
// record keeps no paths: its one time is resolveEncryptedGone's.
func resolveGoneSince(out string, skipped []SkippedInput, start time.Time) {
	prev, st, _ := state.ReadLastRun(out)
	plain := st == state.LastRunPresent && !prev.Encrypted
	streak, serr := readSourceStreak(out)
	for i := range skipped {
		var since time.Time
		found := false
		if plain {
			id := autoplace.GoneSourceID(skipped[i].Path)
			read := 0 // at most state.MaxGoneWarnings of a planted record's warnings (INS-06)
			for _, w := range prev.Warnings {
				if w.Code == state.MailboxGoneCode {
					if read++; read > state.MaxGoneWarnings {
						break
					}
				}
				// The record is untrusted (C-17): a path in it that is not a plain local one is
				// passed over before GoneSourceID looks at it on disk (MA-869; the 1.7.5 adversarial
				// pass's INS-01).
				if w.Code == state.MailboxGoneCode && w.Path != "" && !autoplace.NonLocal(w.Path) && autoplace.InPlace(w.Path) && autoplace.GoneSourceID(w.Path) == id { // InPlace: RV2-06
					since, found = w.Since, true
					// The friction review's F3: a name the settings no longer give is carried from
					// the previous warning (cleaned at read).
					if skipped[i].Name == "" {
						skipped[i].Name = w.Name
					}
					break
				}
			}
			if skipped[i].Name == "" {
				skipped[i].Name = recordedAccountName(prev, skipped[i].Path)
			}
			if !found && serr == nil && streak.LastKind == "local-source-unreachable" &&
				prev.Status == state.RunFailed && prev.Error == autoplace.GoneText(skipped[i].Path) {
				since = streak.FirstUnreachable
			}
		}
		skipped[i].Since = clampGoneSince(since, start)
	}
}

// maxGoneCount is the most mailboxes a record's mailbox_gone_count is believed for: a count past
// it is not one any run could hold (a planted or damaged record), so it counts as 1.
const maxGoneCount = 1_000_000

// resolveEncryptedGone sets every skipped input's Since to the ENCRYPTED archive's one first-found
// time (MA-1478; owner, 2026-11-20, Q5 and Q6), resolved once, straight after discovery, from what
// is on disk before this run writes anything: when this run skips more mailboxes than the previous
// record's mailbox_gone_count, one went since that record, so the time restarts at start;
// otherwise (rule 1) the previous record's mailbox_gone_since; else (rule 3) start. Rule 2 does not
// apply: an encrypted record keeps no error text and its streak no kind. A previous record with no
// count, or a count below 1 or past maxGoneCount, counts as 1, the least its mailbox_gone says. The
// same clamps as clampGoneSince.
func resolveEncryptedGone(out string, skipped []SkippedInput, start time.Time) {
	var since time.Time
	if prev, st, _ := state.ReadLastRun(out); st == state.LastRunPresent && prev.Encrypted && prev.MailboxGone {
		count := prev.MailboxGoneCount
		if count < 1 || count > maxGoneCount {
			count = 1
		}
		if len(skipped) <= count {
			since = prev.MailboxGoneSince
		}
	}
	since = clampGoneSince(since, start)
	for i := range skipped {
		skipped[i].Since = since
	}
}

// goneOf is how many mailbox_gone warnings ws holds and the first one's Since: an encrypted
// run's record keeps only these (newRunRecord), never the warnings themselves.
func goneOf(ws []state.RunWarning) (n int, since time.Time) {
	for _, w := range ws {
		if w.Code == state.MailboxGoneCode {
			if n == 0 {
				since = w.Since
			}
			n++
		}
	}
	return n, since
}

// clampGoneSince is since as a first-found time a record may keep: start for a time that is
// zero or before state.MinPlausibleTime (a clock that was not set).
//
// Amended by the 1.7.5 adversarial pass (CON-02; the owner, 2026-11-20, Q8: "Yes, change it
// (Recommended)"): a time after start is kept as recorded, no longer moved to start. From the
// records alone a clock that was ahead cannot be told from this run's clock being behind, and
// moving the date to start in the second case moved it earlier for good, cutting the WARN short.
// Kept, it can only make the WARN last longer (status counts a time after the run's start as no
// time, so it WARNs), the direction §1.3 accepts.
func clampGoneSince(since, start time.Time) time.Time {
	if since.IsZero() || since.Before(state.MinPlausibleTime) {
		return start
	}
	return since
}

// goneWarnings are the run's mailbox_gone warnings, one per skipped input, which join the run's
// own warnings straight after discovery so every record the run writes keeps them: the running
// record, a refused one, a cancelled or failed one, and the final one (MA-1472). The Message is a
// whole sentence; status never shows it for this code, and builds its words from Path, Name and
// Since.
func goneWarnings(skipped []SkippedInput) []state.RunWarning {
	out := make([]state.RunWarning, 0, len(skipped))
	for _, s := range skipped {
		out = append(out, state.RunWarning{
			Code:    state.MailboxGoneCode,
			Message: "Not found, so skipped: " + s.Label() + ". It is no longer on this computer; the mail already saved from it stays in the archive.",
			Path:    s.Path, Name: s.Name, Since: s.Since, Listed: s.Listed, SettingsUnsure: s.SettingsUnsure,
		})
	}
	return out
}

// recordedAccountName is the name the previous plaintext record prev gave the account whose store
// is the gone input p (the 1.7.5 friction review's F3): an account removed on purpose in
// Thunderbird is gone from its settings, but the last backup that read it recorded its name beside
// the store's id (state.TBAccount, storeID: a hash of the profile and the store's path, computable
// for a folder that is gone). "" when p is in no profile or no entry has its id. Said on screen and
// in status, never in a log file (MA-1288).
//
// Amended by the 1.7.5 re-verify, round 3 (RV3-01): also from the names a record whose run had no
// completeness entries of its own carried (state.TBNameOf), so a refused run's record still names it.
func recordedAccountName(prev state.LastRun, p string) string {
	if len(prev.Thunderbird) == 0 && len(prev.ThunderbirdNames) == 0 {
		return ""
	}
	profile, ok := thunderbird.FindProfileDir(p)
	if !ok {
		return ""
	}
	return state.TBNameOf(prev, storeID(profile, p))
}

// goneAccountName is the name Thunderbird shows for the account whose folder the gone input p
// was, or "".
func goneAccountName(p string) string {
	profile, ok := thunderbird.FindProfileDir(p)
	if !ok {
		return ""
	}
	f, err := thunderbird.ReadStoreFacts(profile, p)
	if err != nil || f.Account == nil || !f.Whole {
		return ""
	}
	return state.CleanFolderName(f.Name)
}

// Seams so a test moves the streak without touching a real archive-local file.
var (
	readSourceStreak  = state.ReadSourceStreak
	writeSourceStreak = state.WriteSourceStreak
)

// updateSourceStreak folds a finished SCHEDULED run's outcome into the archive's source-loss
// streak (§5.7, Slice 3b): a definitive reach resets it, an unreachable verdict extends it, an
// inconclusive outcome leaves it. Best effort — a read or write problem never fails the run
// and never falsely moves the streak.
func updateSourceStreak(out string, runErr error, now time.Time, encrypted bool, logger *log.Logger) {
	reach, kind := classifySourceReach(runErr)
	if reach == sourceInconclusive {
		return
	}
	if encrypted {
		kind = "" // an encrypted archive's streak does not say what kind of source it has (A6)
	}
	prev, err := readSourceStreak(out)
	if err != nil {
		return // a read error leaves the streak unchanged rather than resetting it
	}
	next := prev.Observe(reach == sourceReached, kind, now)
	if werr := writeSourceStreak(out, next); werr != nil && logger != nil {
		logger.Printf("warning: could not record the source-reachability streak in %s (%v)", filepath.Join(out, state.SourceStreakName), werr)
	}
}
