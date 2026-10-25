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
	Previous  *encryptedLastVerify `json:"previous,omitempty"`
}

func minimisedVerify(v LastVerify) *encryptedLastVerify {
	e := &encryptedLastVerify{Version: v.Version, Status: v.Status, Started: v.Started, Finished: v.Finished, Outcome: v.Outcome, Encrypted: true}
	if v.Previous != nil {
		p := *v.Previous
		p.Version, p.Previous = v.Version, nil
		e.Previous = minimisedVerify(p)
	}
	return e
}

// marshalLastVerify is the bytes WriteLastVerify writes: v as it is, or — for an
// encrypted archive — only its allow-listed fields, refusing an outcome outside the
// closed list.
func marshalLastVerify(v LastVerify) ([]byte, error) {
	if !v.Encrypted {
		return json.MarshalIndent(v, "", "  ")
	}
	for _, o := range []*LastVerify{&v, v.Previous} {
		if o != nil && o.Outcome != "" && !KnownOutcome(o.Outcome) {
			return nil, fmt.Errorf("state: verify outcome %q is not in the closed list", o.Outcome)
		}
	}
	return json.MarshalIndent(minimisedVerify(v), "", "  ")
}

// encryptedIntegrityAttention is the integrity notice beside an encrypted archive:
// the time and the outcome's fixed sentence, no path, count or file name.
func encryptedIntegrityAttention(v LastVerify) string {
	when := ""
	if v.Finished != nil {
		when = v.Finished.UTC().Format(time.RFC3339)
	}
	return fmt.Sprintf("This email archive needs your attention.\n\n"+
		"Checked:  %s\n"+
		"Finding:  %s.\n\n"+
		"This archive is encrypted. The check looked at its files without the password:\n"+
		"it can tell whether they are all present and unchanged, not read the messages.\n\n"+
		"What to do: open MailArchive and look at this archive's status. This file is\n"+
		"removed automatically once a check finds every file intact.\n",
		when, OutcomeText(v.Outcome))
}
