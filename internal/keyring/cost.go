package keyring

import (
	"errors"
	"fmt"

	"mail-archive-tool/internal/vault"
)

// openAsking opens out with s under the derivation lock, asking about the cost at a
// folder this computer has not recorded (askingCost, K14).
func openAsking(out string, want vault.ID, s Secret, confirm func(vault.Params) (bool, error)) (*vault.Opened, error) {
	var opened *vault.Opened
	err := askingCost(out, want, confirm, func(limit *vault.Params) error {
		deriving.Lock()
		defer deriving.Unlock()
		var err error
		opened, err = openVault(out, want, s.vault(), limit)
		return err
	})
	return opened, err
}

// askingCost runs try limited to MailArchive's own cost at a folder this computer
// has not recorded (want zero; K14), unlimited otherwise. The vault refuses a key
// file asking more with *vault.CostError before deriving, on the very bytes it
// would derive from (the P3-1a pass, OUT-04), and the person is asked; a yes
// raises the limit to the most of each setting asked — never lowering another —
// and tries again (the P3-1b pass, REG-02). Without a yes, or past two questions,
// it is refused naming the cost: a raw CostError never leaves.
func askingCost(out string, want vault.ID, confirm func(vault.Params) (bool, error), try func(*vault.Params) error) error {
	var limit *vault.Params
	if want == (vault.ID{}) {
		floor := vault.CostFloor()
		limit = &floor
	}
	for asked := 0; ; asked++ {
		err := try(limit)
		var ce *vault.CostError
		if !errors.As(err, &ce) {
			return err
		}
		ok := false
		if confirm != nil && asked < 2 {
			if ok, err = confirm(ce.Params); err != nil {
				return err
			}
		}
		if !ok {
			return fmt.Errorf("%w: opening %s would use %s (MailArchive's own key files use %s), and this computer has not recorded this archive — open it at a terminal to confirm",
				ErrCostRefused, out, ce.Params, vault.CostFloor())
		}
		limit = &vault.Params{
			MemoryKiB: max(limit.MemoryKiB, ce.Params.MemoryKiB),
			Time:      max(limit.Time, ce.Params.Time),
			Threads:   max(limit.Threads, ce.Params.Threads),
		}
	}
}
