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
	out   string // the archive's folder, as it was prepared for
	rec   *keyring.PendingRecovery
	res   *keyring.PendingRestore // a key file to put back (§9.4, §9.5), instead of rec
	plan  *keyring.ForgetPlan     // a forget shown and awaiting its confirmation (§4.6)
	add   *wizard.PendingAdd      // a new archive the wizard's questions chose (§5.2)
	make  *keyring.PendingCreate  // its key, made and awaiting the recovery key typed back (K2)
	lease *keyring.Unlocked
	// file is the recovery file saved for it, "" while none: written only while the
	// item is in the store, under its lock, so a discard always sees it.
	file  string
	timer *time.Timer
}

// Kinds of item, so an endpoint takes only its own (a token of another kind is
// left where it is).
func isKeyChange(it *pendingItem) bool  { return it.rec != nil || it.res != nil || it.make != nil }
func isForgetPlan(it *pendingItem) bool { return it.plan != nil }

// ofCurrentArchive is an item prepared on the current archive's own card — a new
// recovery key, or a key file to put back — which a switch away discards.
func ofCurrentArchive(it *pendingItem) bool { return it.rec != nil || it.res != nil }

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
	mu     sync.Mutex
	items  map[string]*pendingItem
	closed bool // Shut down drained it: nothing more is kept
	life   time.Duration
}

func newPendingSecrets() *pendingSecrets {
	return &pendingSecrets{items: map[string]*pendingItem{}, life: pendingLife}
}

// put keeps it under a new token, discarding any other prepared for the same archive.
// After Shut down it keeps nothing: it is discarded at once and "" returned.
func (p *pendingSecrets) put(it *pendingItem) string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	token := hex.EncodeToString(b)
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		it.discard()
		return ""
	}
	var old []*pendingItem
	for t, o := range p.items {
		if o.key == it.key {
			old = append(old, o)
			delete(p.items, t)
		}
	}
	it.timer = time.AfterFunc(p.life, func() { p.discard(token) })
	p.items[token] = it
	p.mu.Unlock()
	for _, o := range old {
		o.timer.Stop()
		o.discard()
	}
	return token
}

// get is the item of the kind is under token, still pending; nil when there is none.
func (p *pendingSecrets) get(token string, is func(*pendingItem) bool) *pendingItem {
	p.mu.Lock()
	defer p.mu.Unlock()
	if it := p.items[token]; it != nil && is(it) {
		return it
	}
	return nil
}

// take removes and returns the item of the kind is under token: it is used once. An
// item of another kind stays where it is.
func (p *pendingSecrets) take(token string, is func(*pendingItem) bool) *pendingItem {
	p.mu.Lock()
	it := p.items[token]
	if it == nil || !is(it) {
		p.mu.Unlock()
		return nil
	}
	delete(p.items, token)
	p.mu.Unlock()
	it.timer.Stop()
	return it
}

// withItem runs fn under the store's lock while it is still the item under token,
// so a discard cannot pass between them; false when it is no longer waiting.
func (p *pendingSecrets) withItem(token string, it *pendingItem, fn func()) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.items[token] != it {
		return false
	}
	fn()
	return true
}

// discard ends the item under token, if it is still pending.
func (p *pendingSecrets) discard(token string) {
	if it := p.take(token, func(*pendingItem) bool { return true }); it != nil {
		it.discard()
	}
}

// discardFor ends every item prepared for the archive key names ("" every item): it
// locked, or every archive was forgotten.
func (p *pendingSecrets) discardFor(key string) {
	p.discardWhere(func(it *pendingItem) bool { return key == "" || it.key == key })
}

// discardExcept ends every item prepared on another archive's card than the one key
// names: the current archive changed.
func (p *pendingSecrets) discardExcept(key string) {
	p.discardWhere(func(it *pendingItem) bool { return ofCurrentArchive(it) && it.key != key })
}

// close ends every item and keeps none after it: Shut down.
func (p *pendingSecrets) close() {
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()
	p.discardFor("")
}

func (p *pendingSecrets) discardWhere(match func(*pendingItem) bool) {
	p.mu.Lock()
	var gone []*pendingItem
	for t, it := range p.items {
		if match(it) {
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
