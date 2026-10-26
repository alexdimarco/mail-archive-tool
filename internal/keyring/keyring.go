// Package keyring is the one way MailArchive turns a secret into an encrypted
// archive's key (design-encrypted-archives-phase3 §3, A32). It is the only
// production package that calls the vault's key producers and the only one that
// sets app.Options.Key: the reachability meta-test (MA-726) allows this package
// and no other. No key leaves it — it exports no key accessor, and builds what
// needs a key itself (Apply hands one to a keyed run).
//
// Every open consults this computer's record of encrypted archives
// (internal/encrecord): a recorded folder's key file must name the recorded id
// (§3.3), and an open for a write or a remember records an unrecorded folder
// (adoption). While the record cannot be used, a read still opens, with a
// warning, and a write, a create or an adoption is refused (§4.3).
package keyring

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/vault"
)

// How says which secret opened an archive.
type How int

const (
	ByPassword How = iota + 1
	ByRecoveryKey
	ByRememberedKey
	ByCreate
)

// Secret is what a person typed or a secret file held: the password or the
// recovery key. It prints redacted.
type Secret struct {
	how  How
	text string
}

// Password and RecoveryKey name the secret supplied.
func Password(pw string) Secret      { return Secret{how: ByPassword, text: pw} }
func RecoveryKey(code string) Secret { return Secret{how: ByRecoveryKey, text: code} }

// Format prints a secret without its text, whatever the verb.
func (s Secret) Format(f fmt.State, _ rune) {
	fmt.Fprintf(f, "keyring.Secret{%s, redacted}", map[How]string{ByPassword: "password", ByRecoveryKey: "recovery key"}[s.how])
}

func (s Secret) vault() vault.Secret {
	if s.how == ByRecoveryKey {
		return vault.RecoverySecret(s.text)
	}
	return vault.PasswordSecret(s.text)
}

var (
	// ErrLocked refuses a use of an archive that was locked (or destroyed).
	ErrLocked = errors.New("this encrypted archive was locked; open it again")
	// ErrCostRefused refuses an open whose key file asks for more work than a key
	// file MailArchive writes, when no one confirmed it (K14).
	ErrCostRefused = errors.New("this archive's key file asks for more work to open than MailArchive's own key files do")
	// ErrNotEmpty refuses making an encrypted archive in a folder that holds files.
	ErrNotEmpty = errors.New("an encrypted archive is made in a new or empty folder")
	// ErrRecordBusy is a record change that waited for another MailArchive and gave up.
	ErrRecordBusy = errors.New("MailArchive is busy updating its record of encrypted archives")
	// ErrReRecordRefused refuses recording an archive the person told MailArchive to forget,
	// when they did not confirm recording it again (§4.2).
	ErrReRecordRefused = errors.New("you told MailArchive to forget this encrypted archive")
)

// deriving lets one key derivation run at a time in this process (§3.7): each
// can take up to the cost ceiling's memory.
var deriving sync.Mutex

// Seams for tests: the vault's open and create, so a test can prove a refused open
// or create never reaches a derivation; and the clock.
var (
	openVault     = vault.Open
	prepareCreate = vault.PrepareCreate
	now           = time.Now
)

// Unlocked is one open encrypted archive: its key, which archive and folder it
// is, how it was opened, and whether it is locked. Its key never leaves the
// package. Lease, Lock and Destroy are safe from several goroutines.
type Unlocked struct {
	mu     sync.Mutex
	key    *vault.Key
	out    string
	id     vault.ID
	how    How
	write  bool // opened for a write: the folder is recorded under id
	locked bool
	epoch  uint64
	refs   int

	needsHeal bool
	forked    bool
	warnings  []string
}

// ArchiveID is the opened archive's id.
func (u *Unlocked) ArchiveID() vault.ID { return u.id }

// Out is the archive folder.
func (u *Unlocked) Out() string { return u.out }

// How says which secret opened it.
func (u *Unlocked) How() How { return u.how }

// NeedsHeal reports that one copy of the key file differs from the one trusted
// and was not (or could not be) rewritten.
func (u *Unlocked) NeedsHeal() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.needsHeal
}

// Forked reports two different key files saved at the same time on two computers.
func (u *Unlocked) Forked() bool { return u.forked }

// Warnings are what the open found that does not stop it, in words for a person.
func (u *Unlocked) Warnings() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]string(nil), u.warnings...)
}

func (u *Unlocked) warn(s string) {
	u.mu.Lock()
	u.warnings = append(u.warnings, s)
	u.mu.Unlock()
}

// forkWarning is what a fork means for the person (vault.Opened.Forked).
const forkWarning = "this archive's key file was changed on two computers at once; the next password or recovery-key change made here settles it, and the change made on the other computer then stops working"

// newUnlocked wraps an opened vault.
func newUnlocked(out string, o *vault.Opened, how How, write bool) *Unlocked {
	u := &Unlocked{key: o.Key, out: out, id: o.Key.ArchiveID(), how: how, write: write, needsHeal: o.NeedsHeal, forked: o.Forked}
	if o.Forked {
		u.warnings = append(u.warnings, forkWarning)
	}
	return u
}

// acquire takes a reference on u's key for one use, or refuses a locked u.
func (u *Unlocked) acquire() (*vault.Key, uint64, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.locked {
		return nil, 0, ErrLocked
	}
	u.refs++
	return u.key, u.epoch, nil
}

// release drops a reference; the last one to go after a lock destroys the key.
func (u *Unlocked) release() {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.refs--
	if u.refs == 0 && u.locked {
		u.key.Destroy()
	}
}

// leaseHook runs while a lease's open is in flight, outside the lock: a test
// locks the archive there.
var leaseHook = func() {}

// Lease is an independent key for one job (Run now, a plaintext copy, a check,
// a reindex), copied from u without a derivation or a heal (§3.2). It opens
// outside u's lock, so a slow disk never blocks Lock; a lock that lands while it
// is in flight wins — the new key is destroyed and the lease fails. The job
// destroys its lease when it ends.
func (u *Unlocked) Lease() (*Unlocked, error) {
	k, epoch, err := u.acquire()
	if err != nil {
		return nil, err
	}
	o, err := openVault(u.out, u.id, vault.KeySecret(k))
	leaseHook()
	u.mu.Lock()
	stale := u.locked || u.epoch != epoch
	u.mu.Unlock()
	u.release()
	if err != nil {
		return nil, err
	}
	if stale {
		o.Key.Destroy()
		return nil, ErrLocked
	}
	l := newUnlocked(u.out, o, u.how, u.write)
	return l, nil
}

// Lock ends u: no new use starts, and its key is destroyed as soon as the uses in
// progress end. It never waits for them.
func (u *Unlocked) Lock() {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.locked {
		return
	}
	u.locked = true
	u.epoch++
	if u.refs == 0 {
		u.key.Destroy()
	}
}

// Destroy is Lock: the key is zeroed once nothing uses it (MA-718).
func (u *Unlocked) Destroy() { u.Lock() }

// OpenOptions say what an open is for.
type OpenOptions struct {
	// Write: the open is for a backup or a key change. The record must be usable,
	// and an unrecorded folder is recorded under the archive's id (adoption, §3.3).
	Write bool
	// Remember files the archive's key in Store (O3) and, like Write, records the
	// folder.
	Remember bool
	// Store is the OS secret store remembered keys live in.
	Store vault.Store
	// MayHeal rewrites a damaged or stale copy of the key file right after the
	// open (§3.6). A failed heal is a warning, never a refusal.
	MayHeal bool
	// ConfirmCost is asked before a derivation above MailArchive's own cost on a
	// folder this computer has not recorded (K14). Nil refuses.
	ConfirmCost func(vault.Params) bool
	// ConfirmReRecord is asked when the person once told MailArchive to forget this
	// archive (§4.2). Nil refuses.
	ConfirmReRecord func(id string) bool
}

// recorded is what this computer's record says of out.
type recorded struct {
	rec      *encrecord.Record
	recErr   error
	match    encrecord.Match
	ok       bool // out is recorded (proven or not)
	want     vault.ID
	unproven bool
}

// lookup reads the record for out. A folder inside a recorded archive is refused:
// an archive is opened at its own folder.
func lookup(out string) (recorded, error) {
	var r recorded
	r.rec, r.recErr = encrecord.Load()
	if r.recErr != nil {
		return r, nil
	}
	m, ok := r.rec.Lookup(out)
	if !ok {
		return r, nil
	}
	if encrecord.Spelling(m.Folder) != encrecord.Spelling(out) {
		return r, fmt.Errorf("%s is inside the encrypted archive %s; open the archive at its own folder", out, m.Folder)
	}
	r.match, r.ok, r.unproven = m, true, m.Path.Unproven
	if !r.unproven {
		id, err := parseID(m.ID)
		if err != nil {
			return r, err
		}
		r.want = id
	}
	return r, nil
}

// parseID turns a recorded id's hex into a vault.ID.
func parseID(s string) (vault.ID, error) {
	var id vault.ID
	b, err := hex.DecodeString(s)
	if err != nil || len(b) != len(id) {
		return id, fmt.Errorf("the record holds %q, which is not an archive id", s)
	}
	copy(id[:], b)
	return id, nil
}

// refuseUnusableRecord refuses a write, create or adoption while the record
// cannot be used (§4.3).
func refuseUnusableRecord(out string, err error) error {
	return fmt.Errorf("%w: %s cannot be written to while it cannot be used: %w", encmark.ErrRecordUnusable, out, err)
}

// refuseCase refuses a write at a recorded folder that is not intact, in its
// case's words (§4.5). Intact and a missing key file are the vault's to judge.
func refuseCase(out string) error {
	res, err := encmark.At(out)
	if err != nil || !res.Recorded {
		return nil
	}
	switch res.Case {
	case encmark.Link, encmark.Other, encmark.Foreign:
		return fmt.Errorf("%w: %s", vault.ErrTampered, res.Why)
	}
	return nil
}

// Open unlocks the encrypted archive at out with the password or recovery key.
func Open(out string, s Secret, o OpenOptions) (*Unlocked, error) {
	r, err := lookup(out)
	if err != nil {
		return nil, err
	}
	adopting := o.Write || o.Remember
	if r.recErr != nil && adopting {
		return nil, refuseUnusableRecord(out, r.recErr)
	}
	if adopting && r.ok {
		if err := refuseCase(out); err != nil {
			return nil, err
		}
	}
	if r.want == (vault.ID{}) {
		if err := confirmCost(out, s, o.ConfirmCost); err != nil {
			return nil, err
		}
	}
	deriving.Lock()
	opened, err := openVault(out, r.want, s.vault())
	deriving.Unlock()
	if err != nil {
		return nil, explain(out, r, err)
	}
	u := newUnlocked(out, opened, s.how, adopting)
	if err := finishOpen(u, r, o); err != nil {
		u.Destroy()
		return nil, err
	}
	return u, nil
}

// Remembered unlocks the encrypted archive at out with the key this computer
// remembered for it (O3): the recorded id's key at a recorded folder, otherwise
// the key of the id the folder claims, proven by the key file's own check (§3.3).
func Remembered(out string, o OpenOptions) (*Unlocked, error) {
	if o.Store == nil {
		return nil, errors.New("no secret store to find a remembered key in")
	}
	r, err := lookup(out)
	if err != nil {
		return nil, err
	}
	adopting := o.Write || o.Remember
	if r.recErr != nil && adopting {
		return nil, refuseUnusableRecord(out, r.recErr)
	}
	if adopting && r.ok {
		if err := refuseCase(out); err != nil {
			return nil, err
		}
	}
	want := r.want
	if want == (vault.ID{}) {
		info, ok, derr := vault.Detect(out)
		switch {
		case derr != nil:
			return nil, explain(out, r, derr)
		case !ok:
			return nil, explain(out, r, fmt.Errorf("%w: %s", vault.ErrNoVault, out))
		}
		want = info.ArchiveID
	}
	opened, err := vault.RequireRetrievableKey(out, want, o.Store)
	if err != nil {
		return nil, explain(out, r, err)
	}
	u := newUnlocked(out, opened, ByRememberedKey, adopting)
	if err := finishOpen(u, r, o); err != nil {
		u.Destroy()
		return nil, err
	}
	return u, nil
}

// HasRemembered reports whether this computer remembers a key for the archive at
// out: the recorded id's, or — unrecorded — the id its key files claim. It only
// chooses whether to ask for the password; Remembered proves the key.
func HasRemembered(out string, s vault.Store) bool {
	if s == nil {
		return false
	}
	r, err := lookup(out)
	if err != nil {
		return false
	}
	id := r.want
	if id == (vault.ID{}) {
		info, ok, derr := vault.Detect(out)
		if derr != nil || !ok {
			return false
		}
		id = info.ArchiveID
	}
	_, err = s.Get(vault.Account(id))
	return err == nil
}

// explain adds the recorded folder's case to a vault refusal at a recorded path:
// "no key file" there is case 3 or 4, never "not encrypted" (§4.5).
func explain(out string, r recorded, err error) error {
	if !r.ok || !errors.Is(err, vault.ErrNoVault) {
		return err
	}
	res, aerr := encmark.At(out)
	if aerr == nil && res.Recorded && res.Why != "" {
		return fmt.Errorf("%w: %s", vault.ErrVaultMissing, res.Why)
	}
	return fmt.Errorf("%w: %s is an encrypted archive this computer recorded, and its key file is missing", vault.ErrVaultMissing, out)
}

// finishOpen does what follows a successful open: the record (adoption, or the
// lost-record mark), remembering the key, and the heal.
func finishOpen(u *Unlocked, r recorded, o OpenOptions) error {
	if r.recErr != nil {
		u.warn("this computer's record of encrypted archives cannot be used (" + r.recErr.Error() + "), so MailArchive could not check this is the archive it recorded here")
	}
	if u.write {
		if err := adopt(u, r, o.ConfirmReRecord); err != nil {
			return err
		}
	} else if r.recErr == nil && o.Store != nil {
		noteLost(u, r.rec, o.Store)
	}
	if o.Remember {
		if err := remember(u, o.Store); err != nil {
			return err
		}
	}
	if o.MayHeal && u.NeedsHeal() {
		if err := Heal(u); err != nil {
			u.warn("one copy of this archive's key file is damaged and could not be rewritten: " + err.Error())
		}
	}
	return nil
}

// adopt records u's folder under its id (§3.3, §4.4), asking first when the id
// was forgotten. An unproven path for the same id becomes proven; an unproven
// path for another id is replaced by the opened one.
func adopt(u *Unlocked, r recorded, confirm func(string) bool) error {
	id := u.id.String()
	if e := r.rec.Archives[id]; e != nil && e.Forgotten && !(confirm != nil && confirm(id)) {
		return fmt.Errorf("%w (archive %s); it was not recorded again", ErrReRecordRefused, id)
	}
	if r.ok && !r.unproven && r.match.ID == id {
		clearLost(u, id)
		return nil
	}
	_, err := update("record "+id, func(rec *encrecord.Record) error {
		if e := rec.Archives[id]; e != nil && e.Forgotten && r.rec.Archives[id] == nil {
			// Forgotten since the question was asked (or not asked): ask again.
			return fmt.Errorf("%w (archive %s); it was not recorded again", ErrReRecordRefused, id)
		}
		if m, ok := rec.Lookup(u.out); ok && m.Path.Unproven && m.ID != id {
			rec.ForgetPath(m.ID, m.Path.Spelling)
		}
		return rec.Add(id, u.out, false, now())
	})
	if err != nil {
		return err
	}
	clearLost(u, id)
	return nil
}

// update is encrecord.Update with the busy wording of §4.3.
func update(verb string, fn func(*encrecord.Record) error) (string, error) {
	notice, err := encrecord.Update(verb, fn)
	if err != nil && strings.Contains(err.Error(), "busy updating") {
		return notice, fmt.Errorf("%w — try again when it is free: %w", ErrRecordBusy, err)
	}
	return notice, err
}

// noteLost marks a lost record (§4.3): a read open of an archive whose key this
// computer remembers, with no entry and no tombstone for its id.
func noteLost(u *Unlocked, rec *encrecord.Record, s vault.Store) {
	id := u.id.String()
	if rec.Archives[id] != nil {
		return
	}
	if _, err := s.Get(vault.Account(u.id)); err != nil {
		return
	}
	if err := encrecord.MarkLost(id); err != nil {
		u.warn("this computer remembers this archive's key but its record of encrypted archives does not list it, and that could not be noted: " + err.Error())
		return
	}
	u.warn("this computer remembers this archive's key but its record of encrypted archives does not list it; the next backup into it, or remembering its key, records it again")
}

// clearLost clears a lost-record mark for id once it is recorded again.
func clearLost(u *Unlocked, id string) {
	if err := encrecord.ClearLost(id); err != nil {
		u.warn("could not clear the note that this archive was missing from this computer's record: " + err.Error())
	}
}

// remember files u's key in s (O3).
func remember(u *Unlocked, s vault.Store) error {
	if s == nil {
		return errors.New("no secret store to remember the key in")
	}
	k, _, err := u.acquire()
	if err != nil {
		return err
	}
	defer u.release()
	return vault.Remember(s, k)
}

// Heal rewrites the key file's damaged or stale copy from the trusted one, with
// the key itself, never the password (§3.6). It takes the archive lock, so a
// caller heals before its run takes that lock.
func Heal(u *Unlocked) error {
	k, _, err := u.acquire()
	if err != nil {
		return err
	}
	defer u.release()
	if err := vault.Heal(u.out, u.id, vault.KeySecret(k)); err != nil {
		return err
	}
	u.mu.Lock()
	u.needsHeal = false
	u.mu.Unlock()
	return nil
}
