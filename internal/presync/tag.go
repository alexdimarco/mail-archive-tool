package presync

// The short plain phrases a recorded sync outcome is shown in (design-presync-
// attended §4.5, C-28, C-37). It is the ONE closed map from an outcome's name to
// words: the Recent backups card's tag uses it, and the Status line and the
// "not synced" warning reason (slice AS5) are to use it too. A name is never shown
// itself, so a hand-edited record cannot put its own text on any surface.
const (
	phraseSynced      = "Thunderbird synced first"
	phraseNoChange    = "Thunderbird started, but nothing arrived in 20 min"
	phraseLeftOpen    = "Thunderbird synced, but was left open"
	phraseCannotStart = "Thunderbird not synced: it could not be started"
	phraseNotSynced   = "Thunderbird not synced"
)

var tagPhrases = map[string]string{
	OutcomeOK.Name:                       phraseSynced,
	OutcomeWatchTruncated.Name:           phraseSynced,
	OutcomeNoActivity.Name:               phraseNoChange, // never "synced first" (C-68)
	OutcomeWatchTruncatedNoActivity.Name: phraseNoChange,
	OutcomeNotQuiet.Name:                 "Thunderbird synced (still fetching at 20 min)",
	OutcomeWaitEnded.Name:                "Thunderbird sync stopped early, as you asked", // OQ-6
	OutcomeQuitNotPermitted.Name:         phraseLeftOpen,
	OutcomeQuitTimeout.Name:              phraseLeftOpen,
	OutcomeQuitSkippedOthers.Name:        phraseLeftOpen,
	OutcomeQuitNoWindow.Name:             phraseLeftOpen, // C-60
	OutcomeOwnerActive.Name:              phraseLeftOpen,
	OutcomeIdentityUnavailable.Name:      phraseLeftOpen,
	OutcomeAlreadyRunning.Name:           "Thunderbird was already open",
	OutcomeOwnerActiveSkipped.Name:       "Thunderbird not synced: you were using the computer",
	OutcomeOtherProfileRunning.Name:      "Thunderbird not synced: open with another profile",
	OutcomeProfileBusy.Name:              "Thunderbird not synced: another backup was syncing it",
	OutcomeLeftoverFound.Name:            "Thunderbird not synced: an earlier copy is still open",
	OutcomeAppNotFound.Name:              "Thunderbird not synced: it was not found", // friction review O6, 2026-10-31
	OutcomeRefusedElevated.Name:          "Thunderbird not synced: the backup ran as administrator",
	OutcomeProfileUnusable.Name:          "Thunderbird not synced: its profile could not be used",
	OutcomeCannotStart.Name:              phraseCannotStart,
	OutcomeDidNotStart.Name:              phraseCannotStart,
	OutcomeExitedEarly.Name:              "Thunderbird not synced: it closed before its mail stopped arriving", // it did start (MA-939)
	OutcomeSuccessorFound.Name:           "Thunderbird not synced: the copy that was started went away",        // it did start (MA-939)
	OutcomeStartBlocked.Name:             phraseCannotStart,                                                    // C-65
	OutcomeNoDesktopSession.Name:         "Thunderbird not synced: no one was logged in",
	// not-set-up: the sync hook could not be prepared for a sync job (C-34, slice AS5).
	OutcomeNotSetUp.Name:      "Thunderbird not synced: the sync could not be prepared",
	OutcomeInternalError.Name: phraseNotSynced,
}

// TagPhrase is the phrase for a recorded outcome name: "" (no tag) for no name and
// for a cancelled sync, the map's phrase for a known name, and "Thunderbird not
// synced" for any other — the name itself is never echoed (§4.5). The side-only
// outcomes (state-unreadable, state-unsaved) are never a session's own outcome, so
// they are deliberately not in the map.
func TagPhrase(name string) string {
	if name == "" || name == OutcomeCancelled.Name {
		return ""
	}
	if p, ok := tagPhrases[name]; ok {
		return p
	}
	return phraseNotSynced
}

// Outcomes is every outcome a session can report, in the vocabulary's order: a copy,
// for the completeness checks of the packages that show or record them.
func Outcomes() []Outcome { return append([]Outcome(nil), allOutcomes...) }
