package keyring

import (
	"errors"
	"fmt"
	"log"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/vault"
)

// Extract copies the preserved originals of the encrypted archive u opened out to
// dest (design-encrypted-archives-phase3 §7.5), as app.Extract does a plaintext
// archive's: the same formats, exit codes, lock and -dest rules. It writes nothing
// into the archive and records nothing.
func Extract(u *Unlocked, format app.ExtractFormat, dest string, overwrite bool, logger *log.Logger, onProgress func(app.ExtractReport)) (app.ExtractReport, error) {
	return app.ExtractSealed(u.Out(), format, dest, overwrite, logger, onProgress, &sealedOriginals{r: &Reader{u: u, out: u.Out()}})
}

// sealedOriginals is app.SealedOriginals over the archive u opened.
type sealedOriginals struct{ r *Reader }

// Load reads the record read-only (§7.3), under extract's lock.
func (s *sealedOriginals) Load() (*state.Manifest, error) {
	k, _, err := s.r.u.acquire()
	if err != nil {
		return nil, s.locked(err)
	}
	defer s.r.u.release()
	m, _, err := container.LoadManifest(s.r.out, container.LoadOptions{Key: k, ReadOnly: true})
	return m, s.locked(err)
}

// Original is a record's preserved original, decrypted, holding the key only to
// name and to open it.
func (s *sealedOriginals) Original(rec state.Record) ([]byte, error) {
	if rec.Sealed == nil || rec.Sealed.EML == nil || rec.Unit == "" {
		return nil, app.ErrNoOriginal
	}
	data, err := s.r.readUnit(rec.Unit, vault.UnitOriginal, *rec.Sealed.EML)
	return data, s.locked(err)
}

// locked words a lock seen mid-run as the archive needing its key.
func (s *sealedOriginals) locked(err error) error {
	if errors.Is(err, ErrLocked) || errors.Is(err, vault.ErrKeyDestroyed) {
		return fmt.Errorf("%w: %s was locked during the extract: %w", app.ErrEncryptedNeedsKey, s.r.out, err)
	}
	return err
}
