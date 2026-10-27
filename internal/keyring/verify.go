package keyring

import (
	"context"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/memguard"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/vault"
)

// VerifyFull is verify -full on the encrypted archive u opened
// (design-encrypted-archives-phase3 §3.1, §8): app.VerifySealed through a sealed
// side that holds the key only while it reads, reads each unit within the memory
// budget, and heals nothing.
func VerifyFull(ctx context.Context, u *Unlocked, o app.VerifyOptions, logger *log.Logger, onProgress func(app.Report)) (app.Report, error) {
	o.Ctx = ctx
	return app.VerifySealed(u.Out(), o, logger, onProgress, &sealedCheck{u: u, out: u.Out(), guard: memguard.Process})
}

// sealedCheck is app.SealedCheck over the archive u opened.
type sealedCheck struct {
	u     *Unlocked
	out   string
	guard *memguard.Guard
	store *container.Store

	// names are the sealed search index's rows by message path, read once, the first
	// time a damaged message is named (NameMessage); nil when there are none to read.
	names     map[string]index.PageRow
	namesRead bool
}

// NameMessage is app.MessageNamer: rec's date, sender and subject from its row in
// the sealed search index, else what its record holds (P3-8 friction, B19). The
// index is read once, holding the key only while its image is opened, and within
// the memory budget; one that cannot be read leaves the record's words.
func (s *sealedCheck) NameMessage(ctx context.Context, rec state.Record) (app.MessageName, bool) {
	if !s.namesRead {
		s.namesRead = true
		s.names = s.readNames(ctx)
	}
	var n app.MessageName
	if row, ok := s.names[filepath.ToSlash(rec.Path)]; ok {
		n.Subject = row.Subject
		if !row.Date.IsZero() {
			n.Date = row.Date.UTC().Format("2006-01-02 15:04") + " UTC"
		}
		switch {
		case row.SenderName != "" && row.SenderEmail != "":
			n.From = row.SenderName + " <" + row.SenderEmail + ">"
		case row.SenderName != "":
			n.From = row.SenderName
		default:
			n.From = row.SenderEmail
		}
	}
	if n.Subject == "" {
		n.Subject = rec.Subject
	}
	if t, err := time.Parse(time.RFC3339, rec.Date); n.Date == "" && err == nil {
		n.Date = t.UTC().Format("2006-01-02 15:04") + " UTC"
	}
	return n, n != app.MessageName{}
}

// readNames reads the sealed search index's rows by path: nil when the record keeps
// no index, it is larger than a reader would load into memory, the memory budget
// cannot hold it, or it does not open.
func (s *sealedCheck) readNames(ctx context.Context) map[string]index.PageRow {
	if s.store == nil || !s.store.HasIndex() || s.store.IndexSize() > readerIndexBudget {
		return nil
	}
	release, err := s.guard.Reserve(ctx, 2*s.store.IndexSize())
	if err != nil {
		return nil
	}
	defer release()
	if _, _, err := s.u.acquire(); err != nil {
		return nil
	}
	img, err := s.store.IndexImage()
	s.u.release()
	if err != nil {
		return nil
	}
	ix, err := index.OpenMemory(img)
	if err != nil {
		return nil
	}
	defer ix.Close()
	names := map[string]index.PageRow{}
	if err := ix.EachDoc(func(r index.PageRow) error {
		names[filepath.ToSlash(r.Path)] = r
		return nil
	}); err != nil {
		return nil
	}
	return names
}

// locked words a lock of the key seen mid-check as the check's own error.
func checkLocked(err error) error {
	if errors.Is(err, ErrLocked) || errors.Is(err, vault.ErrKeyDestroyed) {
		return fmt.Errorf("%w: %w", app.ErrCheckLocked, err)
	}
	return err
}

// Load reads the record read-only (§7.3), its unit files listed for the cross-check.
func (s *sealedCheck) Load() (*state.Manifest, error) {
	k, _, err := s.u.acquire()
	if err != nil {
		return nil, checkLocked(err)
	}
	defer s.u.release()
	m, st, err := container.LoadManifest(s.out, container.LoadOptions{Key: k, ReadOnly: true, Units: app.UnitLister(&export.Units{Root: s.out, Key: k})})
	if err != nil {
		return nil, checkLocked(err)
	}
	s.store = st
	return m, nil
}

// Chunks opens every chunk the record names, holding the key only while each one is
// opened, never across the reads (the P3-2b pass, KEY-02).
func (s *sealedCheck) Chunks(ctx context.Context) ([]container.ChunkProblem, error) {
	p, err := s.store.CheckChunks(ctx, func() (func(), error) {
		if _, _, err := s.u.acquire(); err != nil {
			return nil, err
		}
		return s.u.release, nil
	})
	return p, checkLocked(err)
}

// FileList compares the record with the archive's file list.
func (s *sealedCheck) FileList() ([]container.ListProblem, bool) { return s.store.CrossCheck() }

// Marks is what the loaded record says it lacks.
func (s *sealedCheck) Marks() (historyGap, indexBehind bool) { return s.store.RecordMarks() }

// Program is the record's program set and the note's sealed digest (C18).
func (s *sealedCheck) Program() (*container.ProgramSet, string) {
	ps, st, _ := s.store.Program()
	if st != container.ProgramOK {
		return nil, s.store.NoteDigest()
	}
	return &ps, s.store.NoteDigest()
}

// Unit reads one unit whole within the memory budget, holding the key only to name
// and to open it (§3.2).
func (s *sealedCheck) Unit(ctx context.Context, rec state.Record, kind vault.UnitKind, su state.SealedUnit) (string, error) {
	k, _, err := s.u.acquire()
	if err != nil {
		return "", checkLocked(err)
	}
	rel, err := (&export.Units{Root: s.out, Key: k}).Locate(rec.Unit, kind, su)
	s.u.release()
	if err != nil {
		return "", checkLocked(err)
	}
	release, err := s.guard.Reserve(ctx, 2*su.Size)
	if err != nil {
		return rel, err
	}
	defer release()
	sealed, err := export.ReadSealedAt(s.out, rel, su)
	if err != nil {
		return rel, err
	}
	if k, _, err = s.u.acquire(); err != nil {
		return rel, checkLocked(err)
	}
	defer s.u.release()
	if _, err = (&export.Units{Root: s.out, Key: k}).Open(rec.Unit, kind, su, sealed); err != nil {
		if locked := checkLocked(err); locked != err {
			return rel, locked
		}
		return rel, fmt.Errorf("%w: %s: %w", app.ErrUnitDoesNotOpen, rel, err)
	}
	return rel, nil
}
