package keyring

import (
	"errors"
	"fmt"

	"mail-archive-tool/internal/vault"
)

// openAsking opens out with s under the derivation lock. At a folder this computer
// has not recorded (want zero) the open is limited to MailArchive's own cost
// (K14): a key file asking more is refused by the vault before deriving, on the
// very bytes it would derive from (vault.OpenWithin; the P3-1a pass, OUT-04), and
// the person is asked; a yes raises the limit to that cost and opens again. Without
// someone to ask, it is refused, naming the cost.
func openAsking(out string, want vault.ID, s Secret, confirm func(vault.Params) (bool, error)) (*vault.Opened, error) {
	var limit *vault.Params
	if want == (vault.ID{}) {
		floor := vault.CostFloor()
		limit = &floor
	}
	for asked := 0; ; asked++ {
		deriving.Lock()
		opened, err := openVault(out, want, s.vault(), limit)
		deriving.Unlock()
		var ce *vault.CostError
		if !errors.As(err, &ce) || asked >= 2 {
			return opened, err
		}
		ok := false
		if confirm != nil {
			if ok, err = confirm(ce.Params); err != nil {
				return nil, err
			}
		}
		if !ok {
			return nil, fmt.Errorf("%w: opening %s would use %s (MailArchive's own key files use %s), and this computer has not recorded this archive — open it at a terminal to confirm",
				ErrCostRefused, out, ce.Params, vault.CostFloor())
		}
		p := ce.Params
		limit = &p
	}
}
