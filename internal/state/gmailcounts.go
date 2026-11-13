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

// Line is the status and run summary's Gmail line: "Gmail: 233,322 messages. 79,200 extra folder
// copies recorded without storing them again. 34,800 folder entries taken from Thunderbird's
// folder lists." (§8.1); "" when no store is switched on. A count of zero is left out.
func (c GmailCounts) Line() string {
	if c.Stores == 0 {
		return ""
	}
	s := "Gmail: " + Count(c.Messages, "message", "messages") + "."
	if c.Copies > 0 {
		s += " " + Count(c.Copies, "extra folder copy", "extra folder copies") + " recorded without storing " + pick(c.Copies, "it", "them") + " again."
	}
	if c.Listed > 0 {
		s += " " + Count(c.Listed, "folder entry", "folder entries") + " taken from Thunderbird's folder lists."
	}
	return s
}
