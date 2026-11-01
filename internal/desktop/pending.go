package desktop

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"mail-archive-tool/internal/app"
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
	key  string // the registry's key of the archive (keyOf)
	out  string // the archive's folder, as it was prepared for
	rec  *keyring.PendingRecovery
	res  *keyring.PendingRestore // a key file to put back (§9.4, §9.5), instead of rec
	plan *keyring.ForgetPlan     // a forget shown and awaiting its confirmation (§4.6)
	add  *wizard.PendingAdd      // a new archive the wizard's questions chose (§5.2)
	make *keyring.PendingCreate  // its key, made and awaiting the recovery key typed back (K2)
	// programOff: the person unticked keeping copies of MailArchive in the new archive.
	programOff bool
	// copyPlan is an unencrypted copy planned and awaiting its confirmation (§10, X14).
	copyPlan *app.DecryptPlan
	// deletePlan is a delete planned from a row of Your archives and awaiting its typed name
	// (design-archive-delete §3.1, MA-972): its key is deletePlanKey's, never an archive's own,
	// so it neither replaces nor is replaced by a key change of that archive.
	deletePlan *heldPlan
	lease      *keyring.Unlocked
	// file is the recovery file saved for it, "" while none: written only while the
	// item is in the store, under its lock, so a discard always sees it.
	file  string
	timer *time.Timer
}

// Kinds of item, so an endpoint takes only its own (a token of another kind is
// left where it is).
func isKeyChange(it *pendingItem) bool  { return it.rec != nil || it.res != nil || it.make != nil }
func isForgetPlan(it *pendingItem) bool { return it.plan != nil }
func isCopyPlan(it *pendingItem) bool   { return it.copyPlan != nil }
func isDeletePlan(it *pendingItem) bool { return it.deletePlan != nil }
func anyKind(*pendingItem) bool         { return true } // its ten minutes are up, whatever it is

// isKeyCancel is what the new recovery key's Cancel ends: a key change, or a new encrypted
// archive whose questions were answered and whose key is being made (the create flow shares that
// Cancel); never a forget, a copy or a delete planned (del-s3a-A4-02).
func isKeyCancel(it *pendingItem) bool { return isKeyChange(it) || it.add != nil }

// ofCurrentArchive is an item prepared on the current archive's own card — a new
// recovery key, a key file to put back, a copy planned — which a switch away
// discards. A delete planned from any row is not one: its key is never an archive's, so
// discardExcept would end it at every save; a switch ends it by its own kind
// (afterCurrentMoved, saveSettings; del-s3a-A3-06).
func ofCurrentArchive(it *pendingItem) bool {
	return it.rec != nil || it.res != nil || it.copyPlan != nil
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
	// A delete plan (deletePlan) holds no secret and changed nothing: dropping the item from
	// the store, which every path to here has done, is its whole discard.
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
	it.timer = time.AfterFunc(p.life, func() { p.discard(token, anyKind) })
	p.items[token] = it
	p.mu.Unlock()
	// Replaced items end off the caller's locks: voiding a recovery file is disk
	// work, and a hung disk must never hold the registry (the P3-3 follow-up pass,
	// LIFE-02).
	for _, o := range old {
		o.timer.Stop()
		go o.discard()
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

// discard ends the item of the kind is under token, if it is still pending: an item of another
// kind is left where it is (del-s3a-A4-02).
func (p *pendingSecrets) discard(token string, is func(*pendingItem) bool) {
	if it := p.take(token, is); it != nil {
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
