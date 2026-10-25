package state

import (
	"errors"
	"fmt"
	"sort"
)

// SealedUnit is one sealed unit of an encrypted archive's record on disk: its
// revision (every write is a new revision under a new name, never an in-place
// rewrite) and the SHA-256 and size of its ciphertext — the keyed per-unit
// rollback check (design-encrypted-archives A2, phase2a §1).
type SealedUnit struct {
	Rev    uint32 `json:"rev"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// SealedSet holds the sealed units a record has, one per kind: its page, its
// attachments, its preserved original.
type SealedSet struct {
	HTML *SealedUnit `json:"html,omitempty"`
	Zip  *SealedUnit `json:"zip,omitempty"`
	EML  *SealedUnit `json:"eml,omitempty"`
}

// ErrUnitRecord refuses a manifest whose encrypted-archive unit records are
// malformed or shared between records.
var ErrUnitRecord = errors.New("manifest: an encrypted archive's unit record is damaged")

// UnitIDLen is the length of a record's unit id: 128 bits as lowercase hex.
const UnitIDLen = 32

// ValidUnitID says s is a unit id: exactly 32 lowercase hex characters.
func ValidUnitID(s string) bool {
	if len(s) != UnitIDLen {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// checkUnits refuses, at load, a unit id of the wrong form, two records sharing
// one, and a Sealed entry with no unit or with an impossible digest or size: each
// would let one message's units be read as another's (phase2a P2a-C3, C5c).
func checkUnits(name string, m *Manifest) error {
	owner := map[string]string{}
	keys := make([]string, 0, len(m.Entries))
	for k := range m.Entries {
		keys = append(keys, k)
	}
	sort.Strings(keys) // a deterministic refusal names the same records every time
	for _, k := range keys {
		r := m.Entries[k]
		if r.Unit == "" {
			if r.Sealed != nil {
				return fmt.Errorf("%w: %s holds sealed units but no unit id", ErrUnitRecord, name)
			}
			continue
		}
		if !ValidUnitID(r.Unit) {
			return fmt.Errorf("%w: %s holds a unit id that is not %d lowercase hex characters", ErrUnitRecord, name, UnitIDLen)
		}
		if other, dup := owner[r.Unit]; dup && other != k {
			return fmt.Errorf("%w: %s gives two records the same unit id", ErrUnitRecord, name)
		}
		owner[r.Unit] = k
		if r.Sealed != nil {
			for _, s := range []*SealedUnit{r.Sealed.HTML, r.Sealed.Zip, r.Sealed.EML} {
				if s != nil && (s.Rev == 0 || !validDigest(s.SHA256) || s.Size <= 0) {
					return fmt.Errorf("%w: %s holds a sealed unit with no revision, digest or size", ErrUnitRecord, name)
				}
			}
		}
	}
	return nil
}

// HasUnits reports whether any record is placed as sealed units: the manifest
// belongs to an encrypted archive.
func (m *Manifest) HasUnits() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.Entries {
		if r.Unit != "" {
			return true
		}
	}
	return false
}

// UnitInUse reports whether some record already holds unit id u, so a draw of a
// new id can retry rather than give two messages one unit.
func (m *Manifest) UnitInUse(u string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.unitSet == nil {
		m.unitSet = map[string]bool{}
		for _, r := range m.Entries {
			if r.Unit != "" {
				m.unitSet[r.Unit] = true
			}
		}
	}
	return m.unitSet[u]
}
