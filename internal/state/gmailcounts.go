package state

import "strings"

// GmailCounts is what an archive's switched-on Gmail stores hold (docs/design-gmail-dedup.md §8.1,
// the status line; slice B3): their emails, the extra folder copies recorded on them without
// storing them again (copy memberships), and the folder entries taken from Thunderbird's folder
// lists (listed memberships). Counts only, never a folder name.
type GmailCounts struct {
	Stores   int
	Messages int
	Copies   int
	Listed   int
	// InOther is how many of the emails are also listed in a folder other than their own (one
	// or more memberships): the line's "N of them are also listed in another folder" (F12).
	InOther int
}

// GmailCountsOf counts m's records in its switched-on Gmail stores. A manifest with none gives
// the zero value.
func GmailCountsOf(m *Manifest) GmailCounts {
	m.mu.Lock()
	defer m.mu.Unlock()
	var c GmailCounts
	for _, v := range m.DedupStores {
		if v == DedupGmail {
			c.Stores++
		}
	}
	if c.Stores == 0 {
		return c
	}
	for key, r := range m.Entries {
		tok, _, _ := strings.Cut(key, keySeparator)
		if m.DedupStores[tok] != DedupGmail {
			continue
		}
		c.Messages++
		if len(r.Folders) > 0 {
			c.InOther++
		}
		for _, f := range r.Folders {
			switch f.Kind {
			case MemberCopy:
				c.Copies++
			case MemberListed:
				c.Listed++
			}
		}
	}
	return c
}

// Line is the status and run summary's Gmail line, in the B3 friction review's plain words (F12,
// replacing §8.1's "messages … extra folder copies … folder entries"): "Gmail: 19 emails saved; 4
// of them are also listed in another folder without a second copy."; "" when no store is switched
// on. With none listed elsewhere, only how many are saved.
func (c GmailCounts) Line() string {
	if c.Stores == 0 {
		return ""
	}
	s := "Gmail: " + Count(c.Messages, "email", "emails") + " saved"
	if c.InOther > 0 {
		s += "; " + thousands(c.InOther) + " of them " + pick(c.InOther, "is", "are") + " also listed in another folder without a second copy"
	}
	return s + "."
}
