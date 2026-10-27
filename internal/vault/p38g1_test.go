package vault

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-850, R37
// No refusal a person can meet starts with the package's own "vault:" (P3-8
// friction review, B14): the wrong secret, the key file states, the recovery key
// and recovery file, the restore's refusals.
func TestRefusalsCarryNoVaultPrefix(t *testing.T) {
	errs := []error{ErrWrongSecret, ErrTampered, ErrNewerVault, ErrCorrupt, ErrRecoveryFormat, ErrRecoveryCheck,
		ErrStoreInsideArchive, ErrKeyNotRetrievable, ErrVaultMissing, ErrNoVault, ErrExists, ErrConflict,
		ErrPartiallySaved, ErrPendingUsed, ErrRecoveryFile, ErrKeyFileOpens, ErrNotProven, ErrClock,
		ErrKeyFileUnreadable, ErrUnconfirmed, ErrAuth, ErrKeyDestroyed, ErrParams, ErrWeakPassword, &CostError{Params: floor}}
	for _, e := range assure.Reached(t, errs, "the refusals") {
		if strings.Contains(e.Error(), "vault:") || strings.Contains(e.Error(), "no vault") {
			t.Errorf("%q carries the package's own words", e.Error())
		}
	}
}
