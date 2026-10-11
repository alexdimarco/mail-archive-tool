package state

import (
	"sort"
	"strings"
	"time"
)

// What a Graph mailbox's gone-detection leaves alone when a fault kept the run from
// seeing an item (fault-isolation §4.8.3, FC4, owner ruling OR10). SweepGone marks
// gone every record of a walked folder that this run did not observe; a fault
// whose item the run could not tell from an archived record would otherwise make
// that record look unobserved. Deferred, never wrong: the next run that meets no
// such fault sweeps.

// SweepSpare says which records of a mailbox a sweep spares.
type SweepSpare struct {
	// NoPhysID spares every record that carries no immutable id: a fault whose
	// identity matched no record (no same-token key for its listing Message-ID and
	// no immutable-id match) could be the message of any of them (OR10).
	NoPhysID bool
	// ShaIdentity spares every record whose identity is a content hash (sha:): when
	// the listing's ids are not immutable, a fault with no Message-ID cannot be told
	// from any content-hash record, whatever its id (§4.8.3 rule 3).
	ShaIdentity bool
	// AllRecords spares every record of the mailbox: when this run's listing did not
	// confirm immutable ids, a fault whose identity matched no record can be the
	// message of any record at all, one that holds an immutable id included (its
	// listing and parsed Message-IDs may differ, so no Message-ID names it), and the
	// run cannot match ids to tell (S3-AI1).
	AllRecords bool
	// LegacyPlaceholders spares the records an adoption re-classed to the info label
	// legacy-placeholder: such a record stands for a message that now has a record of
	// its own, and it is never marked gone because its identity changed (FC27).
	LegacyPlaceholders bool
}

func (s SweepSpare) spares(key string, r Record) bool {
	switch {
	case s.AllRecords:
		return true
	case s.NoPhysID && r.PhysID == "":
		return true
	case s.ShaIdentity && strings.HasPrefix(identityOf(key), "sha:"):
		return true
	case s.LegacyPlaceholders && r.Subject == LegacyPlaceholderSubject && hasLabel(r.Terminal, LabelLegacyPlaceholder):
		return true
	}
	return false
}

func hasLabel(list []string, label string) bool {
	for _, l := range list {
		if l == label {
			return true
		}
	}
	return false
}

// SweepGoneSparing is SweepGone that leaves alone the records spare names.
func (m *Manifest) SweepGoneSparing(token string, walked *WalkedFolders, thisRun time.Time, spare SweepSpare) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	prefix := token + keySeparator
	var gone []string
	for key, r := range m.Entries {
		if !strings.HasPrefix(key, prefix) {
			continue // another mailbox's record — out of this sweep's scope
		}
		if !r.Present || !r.LastSeen.Before(thisRun) {
			continue // already gone, or observed this run
		}
		if !walked.Has(r.Folder) {
			continue // excluded/unwalked folder — retain its last state
		}
		if spare.spares(key, r) {
			continue // a fault kept the run from seeing it: deferred, never wrong
		}
		gone = append(gone, key)
	}
	sort.Strings(gone)
	for _, key := range gone {
		r := m.Entries[key]
		r.Present = false // keep the last known Folder/LastSeen; the file stays (R13)
		m.Entries[key] = r
	}
	return gone
}

// PhysIDKeys is a snapshot of the token's records by immutable id: every key whose
// record carries the id as its primary or an alternate one. A Graph walk reads it
// on its first fault, to stamp the records a faulting item's immutable id names
// (FC4(b)): the id survives a move, which a Message-ID the listing lacks does not.
func (m *Manifest) PhysIDKeys(token string) map[string][]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	prefix := token + keySeparator
	out := map[string][]string{}
	for key, r := range m.Entries {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		if r.PhysID != "" {
			out[r.PhysID] = append(out[r.PhysID], key)
		}
		for _, id := range r.AltPhysIDs {
			out[id] = append(out[id], key)
		}
	}
	return out
}

// AdoptLegacyGraphStubs adopts the Graph stub records an older build left for a
// message it could not parse (FC27). Such a stub has the subject
// "(unreadable message)", Terminal ["body"] and an identity that is a content hash
// of that subject and the message's received time: this build gives the stub the
// listing's Message-ID instead, so the old record's identity changes. A record of
// token matches when its immutable id is physID, or its Date is received. Each
// match is stamped seen (so this run's sweep does not mark it gone) and re-classed
// to the info label legacy-placeholder, as the local legacy stubs are (§6); the
// sweep then spares it for good. It returns how many it adopted.
//
// legacyIdentity is the identity an older build gave the stub of a message received
// at t (the content hash of the placeholder subject and t, nothing else): a record
// is adopted only when its identity IS that one for this message's received time, so
// a real archived message that merely looks like a stub (the placeholder subject, no
// Message-ID, no body, the same second) is no match (S3-AI8). The records with that
// identity are read from the identity index, so a walk's every unparseable message
// costs a lookup, not a scan of the archive (S3-AI11). A nil legacyIdentity checks no
// identity and looks at every record of the token.
func (m *Manifest) AdoptLegacyGraphStubs(token, physID string, received, seen time.Time, legacyIdentity func(time.Time) string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	prefix := token + keySeparator
	when := ""
	if !received.IsZero() {
		when = received.UTC().Format(time.RFC3339)
	}
	var candidates []string
	wantIdentity := ""
	if legacyIdentity == nil {
		candidates = make([]string, 0, len(m.Entries))
		for key := range m.Entries {
			candidates = append(candidates, key)
		}
	} else {
		wantIdentity = legacyIdentity(received)
		if m.byIdent == nil {
			m.buildIdentIndexLocked()
		}
		for _, ref := range m.byIdent[wantIdentity] {
			candidates = append(candidates, ref.Key)
		}
	}
	n := 0
	for _, key := range candidates {
		r, ok := m.Entries[key]
		if !ok || !strings.HasPrefix(key, prefix) || r.Subject != LegacyPlaceholderSubject || !strings.HasPrefix(identityOf(key), "sha:") ||
			len(r.Terminal) != 1 || r.Terminal[0] != MissingBody || len(r.Missing) != 0 {
			continue
		}
		if legacyIdentity != nil && identityOf(key) != wantIdentity {
			continue
		}
		if !(physID != "" && recordHasPhysID(r, physID)) && !(when != "" && r.Date == when) {
			continue
		}
		r.LastSeen = seen
		r.Missing = nil
		r.Terminal = []string{LabelLegacyPlaceholder}
		m.Entries[key] = r
		n++
	}
	return n
}
