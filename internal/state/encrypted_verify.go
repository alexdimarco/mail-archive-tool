package state

import (
	"encoding/json"
	"fmt"
	"time"
)

// The outcomes of a keyless check of an encrypted archive
// (design-encrypted-archives-phase2b §4, §13): a CLOSED list, and the only verdict
// its last-verify record carries — no counts, no paths (A18).
const (
	OutcomeIntact         = "intact"          // every file the record lists is present and unchanged
	OutcomeNotAttested    = "not_attested"    // what the check needs is not there yet; a backup provides it
	OutcomeDamaged        = "damaged"         // a listed file or the record's own files are missing or changed
	OutcomeRestoredOlder  = "restored_older"  // the record is older than its file list, or than this computer's last save
	OutcomeUnfinishedSave = "unfinished_save" // the record is one save behind its file list (I = P + 1)
	OutcomeNewerFormat    = "newer_format"    // written by a newer MailArchive
)

// outcomeText is each outcome's one fixed sentence: the only words status and the
// integrity notice say about an encrypted archive's check.
var outcomeText = map[string]string{
	OutcomeIntact:         "every file the archive's record lists is present and unchanged",
	OutcomeNotAttested:    "the archive could not be fully checked without its key — run a backup, then check again",
	OutcomeDamaged:        "files the archive's record lists are missing, changed or unreadable — restore the whole archive folder from your newest backup copy",
	OutcomeRestoredOlder:  "the archive's record is older than its file list — an older copy may have been restored; restore the newest copy of the whole archive folder",
	OutcomeUnfinishedSave: "the archive's last save did not finish, or an older copy of its record was restored — if you restored an older copy, restore the newest copy of the whole archive folder",
	OutcomeNewerFormat:    "the archive was written by a newer MailArchive — upgrade MailArchive, then check again",
}

// OutcomeText is outcome's fixed sentence; "" for an outcome not in the list.
func OutcomeText(outcome string) string { return outcomeText[outcome] }

// fullOutcomeText is a check with the key's sentence where it says more than the
// keyless one (design-encrypted-archives-phase3 §8; the owner's rulings): amber is
// a part the archive can live without — a spare copy of its record, its search
// index, part of its go-back timeline — with the mail intact; red is mail itself.
var fullOutcomeText = map[string]string{
	OutcomeIntact:      "every file of the archive opens with its key and is intact",
	OutcomeNotAttested: "a part of the archive needs attention — a spare copy of its record, its search index or part of its go-back timeline; the messages themselves are intact — run mailarchive verify -full to see what to do",
	OutcomeDamaged:     "messages in the archive are missing or damaged, or its record cannot be read — restore the whole archive folder from your newest backup copy",
}

// OutcomeTextFor is outcome's fixed sentence for a check of tier (1: without the
// key; 2: with it).
func OutcomeTextFor(outcome string, tier int) string {
	if t, ok := fullOutcomeText[outcome]; ok && tier == 2 {
		return t
	}
	return outcomeText[outcome]
}

// The tiers of an encrypted archive's check (§8): without the key, and with it. A
// record from before tiers were recorded is tier 1.
const (
	TierKeyless = 1
	TierFull    = 2
)

// TierOf is v's tier, a record that names none being tier 1.
func TierOf(v *LastVerify) int {
	if v == nil || v.Tier == 0 {
		return TierKeyless
	}
	return v.Tier
}

// KnownOutcome reports whether outcome is in the closed list.
func KnownOutcome(outcome string) bool { _, ok := outcomeText[outcome]; return ok }

// OutcomeIsRed reports whether outcome is a RED posture: damage, or a record that
// the next backup refuses.
func OutcomeIsRed(outcome string) bool {
	switch outcome {
	case OutcomeDamaged, OutcomeRestoredOlder, OutcomeUnfinishedSave:
		return true
	}
	return false
}

// encryptedLastVerify is the ONLY shape an encrypted archive's last-verify record
// takes on disk: times, the status and the outcome, and the last finished verdict a
// running record carries in the same shape (A18).
type encryptedLastVerify struct {
	Version   int                  `json:"version"`
	Status    string               `json:"status"`
	Started   time.Time            `json:"started"`
	Finished  *time.Time           `json:"finished"`
	Outcome   string               `json:"outcome,omitempty"`
	Encrypted bool                 `json:"encrypted"`
	Tier      int                  `json:"tier,omitempty"`
	Previous  *encryptedLastVerify `json:"previous,omitempty"`
}

// minimisedVerify is v in the allow-listed shape. A carried verdict with no outcome
// of the closed list — a plaintext-shaped record found in the folder — is not
// carried: it would read as a finished check that found nothing (the adversarial
// pass, LEAK-03).
func minimisedVerify(v LastVerify) *encryptedLastVerify {
	e := &encryptedLastVerify{Version: v.Version, Status: v.Status, Started: v.Started, Finished: v.Finished, Outcome: v.Outcome, Encrypted: true, Tier: v.Tier}
	if v.Previous != nil && KnownOutcome(v.Previous.Outcome) {
		p := *v.Previous
		p.Version, p.Previous = v.Version, nil
		e.Previous = minimisedVerify(p)
	}
	return e
}

// marshalLastVerify is the bytes WriteLastVerify writes: v as it is, or — for an
// encrypted archive — only its allow-listed fields, refusing what ReadLastVerify
// would not read back (checkVerdicts).
func marshalLastVerify(v LastVerify) ([]byte, error) {
	if !v.Encrypted {
		return json.MarshalIndent(v, "", "  ")
	}
	e := minimisedVerify(v)
	back := LastVerify{Status: e.Status, Outcome: e.Outcome, Encrypted: true, Tier: e.Tier}
	if e.Previous != nil {
		back.Previous = &LastVerify{Status: e.Previous.Status, Outcome: e.Previous.Outcome, Encrypted: true, Tier: e.Previous.Tier}
	}
	if err := checkVerdicts(back); err != nil {
		return nil, fmt.Errorf("state: an encrypted archive's verify record: %w", err)
	}
	return json.MarshalIndent(e, "", "  ")
}

// encryptedIntegrityAttention is the integrity notice beside an encrypted archive:
// the time and the outcome's fixed sentence, no path, count or file name.
func encryptedIntegrityAttention(v LastVerify) string {
	when := ""
	if v.Finished != nil {
		when = v.Finished.UTC().Format(time.RFC3339)
	}
	how := "The check looked at its files without the password:\n" +
		"it can tell whether they are all present and unchanged, not read the messages."
	if TierOf(&v) == TierFull {
		how = "The check opened every file with the archive's key: it read the messages\n" +
			"themselves, as well as checking that every file is present."
	}
	return fmt.Sprintf("This email archive needs your attention.\n\n"+
		"Checked:  %s\n"+
		"Finding:  %s.\n\n"+
		"This archive is encrypted. %s\n\n"+
		"What to do: open MailArchive and look at this archive's status. This file is\n"+
		"removed automatically once a check finds every file intact.\n",
		when, OutcomeTextFor(v.Outcome, TierOf(&v)), how)
}
