package keyring

import (
	"fmt"
	"path/filepath"

	"mail-archive-tool/internal/util"
	"mail-archive-tool/internal/vault"
)

// maxKeyFileBytes bounds a key file read to judge its cost (the vault's own cap).
const maxKeyFileBytes = 64 << 10

// highestCost is the most Argon2id work any copy of out's key file asks of s — the
// vault may try every copy — and whether any copy asks more than the floor. A copy
// that does not parse asks nothing: the vault refuses it before deriving.
func highestCost(out string, s Secret) (vault.Params, bool) {
	floor := vault.CostFloor()
	var top vault.Params
	above := false
	for _, name := range []string{vault.FileName, vault.BackupName} {
		raw, err := util.ReadRegularFile(filepath.Join(out, name), maxKeyFileBytes)
		if err != nil {
			continue
		}
		v, err := vault.Parse(raw)
		if err != nil {
			continue
		}
		p := v.Password.Params
		if s.how == ByRecoveryKey {
			p = v.Recovery.Params
		}
		if p.MemoryKiB > floor.MemoryKiB || p.Time > floor.Time || p.Threads > floor.Threads {
			above = true
			if uint64(p.MemoryKiB)*uint64(p.Time) > uint64(top.MemoryKiB)*uint64(top.Time) || top == (vault.Params{}) {
				top = p
			}
		}
	}
	return top, above
}

// confirmCost asks before a derivation above MailArchive's own cost, on a folder
// this computer has not recorded (K14): a key file there may come from anyone, and
// one can ask for up to the ceiling's memory and time. Without someone to ask it
// refuses, naming the cost.
func confirmCost(out string, s Secret, confirm func(vault.Params) (bool, error)) error {
	p, above := highestCost(out, s)
	if !above {
		return nil
	}
	if confirm != nil {
		ok, err := confirm(p)
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
	}
	return fmt.Errorf("%w: opening %s would use %s (MailArchive's own key files use %s), and this computer has not recorded this archive — open it at a terminal to confirm",
		ErrCostRefused, out, p, vault.CostFloor())
}
