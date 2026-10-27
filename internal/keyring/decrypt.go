package keyring

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/memguard"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/vault"
)

// DecryptCopy makes the unencrypted copy p planned of the encrypted archive u opened
// (design-encrypted-archives-phase3 §10): app.DecryptSealed through a sealed side on
// a lease of its own — never a heal, an adoption or an Apply — that holds the key only
// while it opens what it reads. An archive already locked when the copy begins is
// app.ErrCopyLocked, never damage. Once begun, the lease is the copy's own key: a
// lock of u does not stop it (§3.2) — the Desktop says so where the person acts —
// and only its context does (Ctrl-C, Shut down). Its ErrCopyLocked handling mid-copy
// is defence, for a lease destroyed under it.
func DecryptCopy(ctx context.Context, u *Unlocked, e *app.CopyExportEnv, p *app.DecryptPlan, archives, settings []string, logger *log.Logger, onProgress func(app.DecryptReport)) (app.DecryptReport, error) {
	l, err := u.Lease()
	if err != nil {
		return app.DecryptReport{}, copyLocked(err)
	}
	defer l.Destroy()
	return e.DecryptSealed(ctx, p, archives, settings, &sealedCopy{u: l, out: l.Out(), guard: memguard.Process}, logger, onProgress)
}

// sealedCopy is app.SealedCopy over a lease.
type sealedCopy struct {
	u     *Unlocked
	out   string
	guard *memguard.Guard
	store *container.Store
}

// copyLocked words a lock seen mid-copy as the copy's own error.
func copyLocked(err error) error {
	if errors.Is(err, ErrLocked) || errors.Is(err, vault.ErrKeyDestroyed) {
		return fmt.Errorf("%w: %w", app.ErrCopyLocked, err)
	}
	return err
}

// Load reads the record read-only (§7.3).
func (s *sealedCopy) Load() (*state.Manifest, error) {
	k, _, err := s.u.acquire()
	if err != nil {
		return nil, copyLocked(err)
	}
	defer s.u.release()
	m, st, err := container.LoadManifest(s.out, container.LoadOptions{Key: k, ReadOnly: true})
	if err != nil {
		return nil, copyLocked(err)
	}
	s.store = st
	return m, nil
}

// Unit reads one unit whole within the memory budget and hands its plaintext to use,
// holding the key only to name and to open it.
func (s *sealedCopy) Unit(ctx context.Context, rec state.Record, kind vault.UnitKind, su state.SealedUnit, use func([]byte) error) error {
	k, _, err := s.u.acquire()
	if err != nil {
		return copyLocked(err)
	}
	rel, err := (&export.Units{Root: s.out, Key: k}).Locate(rec.Unit, kind, su)
	s.u.release()
	if err != nil {
		return copyLocked(err)
	}
	release, err := s.guard.Reserve(ctx, 2*su.Size)
	if err != nil {
		return err
	}
	defer release()
	sealed, err := export.ReadSealedAt(s.out, rel, su)
	if err != nil {
		return err
	}
	if k, _, err = s.u.acquire(); err != nil {
		return copyLocked(err)
	}
	plain, err := (&export.Units{Root: s.out, Key: k}).Open(rec.Unit, kind, su, sealed)
	s.u.release()
	if err != nil {
		if locked := copyLocked(err); locked != err {
			return locked
		}
		return fmt.Errorf("%w: %w", app.ErrUnitDoesNotOpen, err)
	}
	defer clear(plain)
	return use(plain)
}

// History is the timeline for the copy, the key held while it is read.
func (s *sealedCopy) History() ([]state.HistoryEvent, int, error) {
	if _, _, err := s.u.acquire(); err != nil {
		return nil, 0, copyLocked(err)
	}
	defer s.u.release()
	ev, lost, err := s.store.HistoryForCopy()
	return ev, lost, copyLocked(err)
}

// HistoryGap says the record marks part of the timeline lost.
func (s *sealedCopy) HistoryGap() bool { _, _, ok := s.store.HistoryGap(); return ok }

// IndexBehind says the sealed index is marked as missing messages (MA-761).
func (s *sealedCopy) IndexBehind() bool { return s.store.IndexBehind() }

// IndexImage streams the sealed index image to w, the key held while it is read.
func (s *sealedCopy) IndexImage(w io.Writer) error {
	if _, _, err := s.u.acquire(); err != nil {
		return copyLocked(err)
	}
	defer s.u.release()
	return copyLocked(s.store.WriteIndexImage(w))
}

// IndexSize is the sealed index image's size.
func (s *sealedCopy) IndexSize() int64 { return s.store.IndexSize() }
