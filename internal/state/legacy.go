package state

import "strings"

// LegacyPlaceholderSubject is the subject every stub of an older build carried.
const LegacyPlaceholderSubject = "(unreadable message)"

// ReclassLegacyPlaceholders re-classes the records of an older build's collapsed
// stub (F33): that build gave every unreadable local item the same identity, so
// they merged into one record per folder that stands for no message in
// particular. A record qualifies when its identity component equals identity (the
// constant identity of the empty-field stub, model.LegacyStubIdentity) and its
// Subject is LegacyPlaceholderSubject. It goes from Missing:["body"] to
// Terminal:["legacy-placeholder"] (class info: nothing is missing from it) and
// stays a record, so no page or file changes. It returns how many records it
// re-classed; a second call finds none. It is a run's job (FC27): `status` and Load
// never call it, so reading an archive never writes it.
func (m *Manifest) ReclassLegacyPlaceholders(identity string) int {
	if identity == "" {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for key, r := range m.Entries {
		if r.Subject != LegacyPlaceholderSubject || !r.Fillable() || identityOfKey(key) != identity {
			continue
		}
		// Only the collapsed stub's own gap is replaced: a record that also carries
		// other labels is not that stub.
		if len(r.Missing) != 1 || r.Missing[0] != MissingBody || len(r.Terminal) != 0 {
			continue
		}
		r.Missing = nil
		r.Terminal = []string{LabelLegacyPlaceholder}
		m.Entries[key] = r
		n++
	}
	return n
}

// identityOfKey is the identity component of a folder-scoped record key
// (token \x00 folder \x00 identity, optionally \x00 fingerprint); "" for any
// other shape.
func identityOfKey(key string) string {
	parts := strings.Split(key, keySeparator)
	if len(parts) < 3 {
		return ""
	}
	return parts[2]
}
