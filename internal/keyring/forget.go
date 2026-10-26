package keyring

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/util"
	"mail-archive-tool/internal/vault"
)

// ErrForgetChanged refuses a forget whose record or folder changed after the
// person confirmed it.
var ErrForgetChanged = errors.New("this computer's record of the archive changed since you confirmed; run forget-archive again")

// ForgetPlan is what forgetting a folder would remove, shown before anything is
// asked (§4.6). Nothing is held while the person answers.
type ForgetPlan struct {
	// Out is the folder asked about; Spelling is the exact stored entry removed.
	Out      string
	Spelling string
	ID       string
	// Case is how the folder stands now; Strong asks for the path to be typed back
	// (cases L, 1 and 2: it may have been tampered with).
	Case   encmark.Case
	Strong bool
	// Others are the id's other recorded paths, which stay protected.
	Others []encrecord.PathEntry
	// Last: this is the id's last path, so forgetting the whole id is offered.
	Last bool
	// Remembered: this computer remembers the id's key.
	Remembered bool

	entry encrecord.Entry
}

// PlanForget reads what forgetting out would remove. An unrecorded folder, an
// unproven one (§4.5 case 0: not until it is proven or dropped), and a record that
// cannot be used are refused.
func PlanForget(out string, s vault.Store) (ForgetPlan, error) {
	rec, err := encrecord.Load()
	if err != nil {
		return ForgetPlan{}, refuseUnusableRecord(out, err)
	}
	m, ok := rec.Lookup(out)
	if !ok || !atRecordedFolder(out, m) {
		return ForgetPlan{}, fmt.Errorf("%s is not a folder this computer recorded as an encrypted archive; nothing to forget", out)
	}
	// An unproven path (§4.5 case 0) is proven by opening it — unless its key files
	// are gone, when nothing can prove it: then it may be forgotten, with the strong
	// confirmation (the P3-1a pass, CRIT-02; kept by the owner, 2026-10-26).
	unprovenGone := m.Path.Unproven && !vault.Claims(out).Present
	if m.Path.Unproven && !unprovenGone {
		return ForgetPlan{}, fmt.Errorf("%s was rebuilt by a repair of this computer's record and is not proven yet; open it with its password first", out)
	}
	res, _ := encmark.At(out)
	e := rec.Archives[m.ID]
	p := ForgetPlan{Out: out, Spelling: m.Path.Spelling, ID: m.ID, Case: res.Case, entry: cloneEntry(e)}
	p.Strong = unprovenGone || res.Case == encmark.Link || res.Case == encmark.Foreign || res.Case == encmark.Other
	for _, pe := range e.Paths {
		if pe.Spelling != m.Path.Spelling {
			p.Others = append(p.Others, pe)
		}
	}
	p.Last = len(p.Others) == 0
	if s != nil {
		if id, err := parseID(m.ID); err == nil {
			if _, err := s.Get(vault.Account(id)); err == nil {
				p.Remembered = true
			}
		}
	}
	return p, nil
}

func cloneEntry(e *encrecord.Entry) encrecord.Entry {
	if e == nil {
		return encrecord.Entry{}
	}
	return encrecord.Entry{Paths: slices.Clone(e.Paths), Forgotten: e.Forgotten}
}

// ForgetOptions are the person's answers.
type ForgetOptions struct {
	// Whole forgets the id (its last path): a tombstone, every path dropped.
	Whole bool
	// Keys also forgets the remembered key; only with Whole.
	Keys  bool
	Store vault.Store
}

// Forget carries out a confirmed plan (§4.6): under the archive lock when the
// folder is there, then the record's lock, it re-reads the folder's case and the
// record and refuses if either changed since the plan; only then does it forget
// the remembered key, when asked, and then write the record — keys first, so a
// crash between leaves the record still naming the archive, but never before the
// re-check (the P3-1a pass, CON-05, FID-01).
func Forget(p ForgetPlan, o ForgetOptions) error {
	if o.Keys && !o.Whole {
		return errors.New("the key is forgotten only with the whole archive")
	}
	if o.Whole && !p.Last {
		return errors.New("the whole archive is forgotten only from its last folder")
	}
	var keyID vault.ID
	if o.Keys {
		if o.Store == nil {
			return errors.New("no secret store to forget the key from")
		}
		id, err := parseID(p.ID)
		if err != nil {
			return err
		}
		keyID = id
	}
	l, err := lockFolder(p.Out)
	if err != nil {
		return err
	}
	if l != nil {
		defer l.Release()
	}
	if res, _ := encmark.At(p.Out); res.Case != p.Case {
		return ErrForgetChanged
	}
	_, err = update("forget-archive", func(rec *encrecord.Record) error {
		m, ok := rec.Lookup(p.Out)
		e := rec.Archives[p.ID]
		if !ok || m.ID != p.ID || m.Path.Spelling != p.Spelling || e == nil || !sameEntry(*e, p.entry) {
			return ErrForgetChanged
		}
		if o.Keys {
			if err := vault.Forget(o.Store, keyID); err != nil && p.Remembered {
				return fmt.Errorf("the remembered key could not be forgotten, so nothing was changed: %w", err)
			}
		}
		if o.Whole {
			rec.ForgetID(p.ID)
		} else {
			rec.ForgetPath(p.ID, p.Spelling)
		}
		return nil
	})
	return err
}

// lockFolder takes the archive lock of a folder that is there and is no link (the
// lock order: the archive's before the record's, §4.3). A folder that is a link,
// or absent, is not written into. A lock another run holds refuses the forget —
// try again when it is free (the P3-1a pass, FID-02); a lock that cannot be taken
// for another reason (a read-only drive) lets it proceed on the record alone.
func lockFolder(out string) (*lockfile.Lock, error) {
	if link, _ := util.IsLinkLike(out); link {
		return nil, nil
	}
	if fi, err := os.Stat(out); err != nil || !fi.IsDir() {
		return nil, nil
	}
	l, err := lockfile.AcquireAs(filepath.Join(out, lockfile.Name), "forget-archive")
	if errors.Is(err, lockfile.ErrHeld) {
		return nil, fmt.Errorf("%s is in use by another MailArchive run; nothing was changed — try again when it is free: %w", out, err)
	}
	if err != nil {
		return nil, nil
	}
	return l, nil
}

func sameEntry(a, b encrecord.Entry) bool {
	return a.Forgotten == b.Forgotten && slices.EqualFunc(a.Paths, b.Paths, func(x, y encrecord.PathEntry) bool {
		return x.Spelling == y.Spelling && x.Source == y.Source && x.Recorded.Equal(y.Recorded) && x.Unproven == y.Unproven
	})
}

// ForgetKey removes the key this computer remembers for the archive at out (§9.3):
// the recorded id's, or the one its key files claim. Nothing in the folder changes.
func ForgetKey(out string, s vault.Store) (vault.ID, error) {
	if s == nil {
		return vault.ID{}, errors.New("no secret store to forget the key from")
	}
	r, err := lookup(out)
	if err != nil {
		return vault.ID{}, err
	}
	id := r.want
	if id == (vault.ID{}) {
		info, ok, derr := vault.Detect(out)
		if derr != nil || !ok {
			return vault.ID{}, fmt.Errorf("which archive %s is cannot be told (its key file is missing or unreadable), so no remembered key was forgotten", out)
		}
		id = info.ArchiveID
	}
	if err := vault.Forget(s, id); err != nil {
		return id, err
	}
	return id, nil
}
