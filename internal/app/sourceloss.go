package app

import (
	"context"
	"errors"
	"log"
	"path/filepath"
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
type sourceUnreachableError struct{ path string }

// Error is autoplace.GoneText, unchanged, so whatever compares a record's error with an input's
// refusal (health.GoneMailboxRepairable) compares one text.
func (e *sourceUnreachableError) Error() string {
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
		out = append(out, SkippedInput{Path: p, Name: goneAccountName(p)})
	}
	return out
}

// resolveGoneSince sets each skipped input's Since: when it was first found gone (MA-1472),
// resolved once, straight after discovery, from what is on disk before this run writes anything
// (docs/design-1.7.5-fixes.md §1.3):
//
//  1. the previous last-run record's mailbox_gone warning for the same input, compared by
//     autoplace.GoneSourceID;
//  2. else, when the source-loss streak's last kind is local-source-unreachable and the previous
//     record is a failed run whose error is this input's own refusal (autoplace.GoneText), the
//     streak's FirstUnreachable: when the archive that was failing first found it gone;
//  3. else this run's start.
//
// A time later than start (a clock that was ahead) becomes start, and one before
// state.MinPlausibleTime (a clock that was not set) becomes start and is never carried. The
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
			for _, w := range prev.Warnings {
				if w.Code == state.MailboxGoneCode && w.Path != "" && autoplace.GoneSourceID(w.Path) == id {
					since, found = w.Since, true
					break
				}
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
// zero, before state.MinPlausibleTime, or after start.
func clampGoneSince(since, start time.Time) time.Time {
	if since.IsZero() || since.Before(state.MinPlausibleTime) || since.After(start) {
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
			Path:    s.Path, Name: s.Name, Since: s.Since,
		})
	}
	return out
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
