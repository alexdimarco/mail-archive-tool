package desktop

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/wizard"
)

// The key changes the Encryption card makes in two steps — a new recovery key shown,
// then typed back — keep what they prepared here between the two requests
// (design-encrypted-archives-phase3 §5.2; the P3-3 plan, 3e): under a random token
// the page holds, never the key itself; used once; discarded after ten minutes, on
// Cancel, on a second one for the same archive, when that archive locks, and at Shut
// down. A discarded new recovery key never takes effect, and the recovery file saved
// for it is voided.

// pendingLife is how long a prepared key change waits for its second step.
const pendingLife = 10 * time.Minute

// pendingItem is one prepared key change.
type pendingItem struct {
	key   string // the registry's key of the archive (keyOf)
	rec   *keyring.PendingRecovery
	res   *keyring.PendingRestore // a key file to put back (§9.4, §9.5), instead of rec
	plan  *keyring.ForgetPlan     // a forget shown and awaiting its confirmation (§4.6)
	add   *wizard.PendingAdd      // a new archive the wizard's questions chose (§5.2)
	make  *keyring.PendingCreate  // its key, made and awaiting the recovery key typed back (K2)
	lease *keyring.Unlocked
	file  string // the recovery file saved for it, "" while none
	timer *time.Timer
}

// recoveryKey is the new recovery key it shows.
func (it *pendingItem) recoveryKey() string {
	switch {
	case it.res != nil:
		return it.res.RecoveryKey()
	case it.make != nil:
		return it.make.RecoveryKey()
	case it.rec != nil:
		return it.rec.RecoveryKey()
	}
	return ""
}

// recoveryFile is the new recovery key's recovery file.
func (it *pendingItem) recoveryFile() ([]byte, error) {
	switch {
	case it.res != nil:
		return it.res.RecoveryFile()
	case it.make != nil:
		return it.make.RecoveryFile()
	case it.rec != nil:
		return it.rec.RecoveryFile()
	}
	return nil, errors.New("there is no new recovery key waiting")
}

// discard ends an item that was not committed: its new key forgotten, its lease
// destroyed, its recovery file voided.
func (it *pendingItem) discard() {
	if it.rec != nil {
		it.rec.Discard()
	}
	if it.res != nil {
		it.res.Discard()
	}
	if it.make != nil {
		it.make.Discard()
	}
	if it.lease != nil {
		it.lease.Destroy()
	}
	if it.file != "" {
		_ = keyring.VoidFile(it.file)
	}
}

// pendingSecrets holds the prepared key changes.
type pendingSecrets struct {
	mu    sync.Mutex
	items map[string]*pendingItem
}

func newPendingSecrets() *pendingSecrets { return &pendingSecrets{items: map[string]*pendingItem{}} }

// put keeps it under a new token, discarding any other prepared for the same archive.
func (p *pendingSecrets) put(it *pendingItem) string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	token := hex.EncodeToString(b)
	p.mu.Lock()
	var old []*pendingItem
	for t, o := range p.items {
		if o.key == it.key {
			old = append(old, o)
			delete(p.items, t)
		}
	}
	it.timer = time.AfterFunc(pendingLife, func() { p.discard(token) })
	p.items[token] = it
	p.mu.Unlock()
	for _, o := range old {
		o.timer.Stop()
		o.discard()
	}
	return token
}

// get is the item under token, still pending; nil when there is none.
func (p *pendingSecrets) get(token string) *pendingItem {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.items[token]
}

// take removes and returns the item under token: it is used once.
func (p *pendingSecrets) take(token string) *pendingItem {
	p.mu.Lock()
	it := p.items[token]
	delete(p.items, token)
	p.mu.Unlock()
	if it != nil {
		it.timer.Stop()
	}
	return it
}

// discard ends the item under token, if it is still pending.
func (p *pendingSecrets) discard(token string) {
	if it := p.take(token); it != nil {
		it.discard()
	}
}

// discardFor ends every item prepared for the archive key names: it locked.
func (p *pendingSecrets) discardFor(key string) {
	p.mu.Lock()
	var gone []*pendingItem
	for t, it := range p.items {
		if key == "" || it.key == key {
			gone = append(gone, it)
			delete(p.items, t)
		}
	}
	p.mu.Unlock()
	for _, it := range gone {
		it.timer.Stop()
		it.discard()
	}
}
