package app

import (
	"errors"
	"fmt"
	"log"
	"time"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/vault"
)

// ErrEncryptedArchive refuses an encrypted run that cannot proceed safely: the
// key is not this archive's, the folder is not an encrypted archive, its manifest
// is missing beside sealed units, or the setup would leave plaintext behind
// (design-encrypted-archives-phase2a §5, P2a-C4, C9c).
var ErrEncryptedArchive = errors.New("this encrypted archive cannot be written")

// prepareEncrypted is an encrypted run's setup, under the archive lock, after
// the manifest load and before the first write. It proves the key belongs to
// the archive in out (an unlock of its vault with the key, P2a-C9c), refuses a
// relocated manifest, an existing plaintext archive (records with no unit — only
// the A12 conversion encrypts one) and a missing or empty manifest beside sealed
// units, then runs the keyed inventory sweep (P2a-C4). It returns the run's unit
// store.
func prepareEncrypted(opts Options, manifest *state.Manifest, runStart time.Time, logger *log.Logger) (*export.Units, error) {
	refuse := func(why string, cause error) error {
		if cause != nil {
			return fmt.Errorf("%w: %s: %s: %w", ErrEncryptedArchive, opts.Out, why, cause)
		}
		return fmt.Errorf("%w: %s: %s", ErrEncryptedArchive, opts.Out, why)
	}
	if opts.Manifest != "" {
		return nil, refuse("its manifest cannot be moved out of the archive folder (-manifest)", nil)
	}
	if err := vault.VerifyKey(opts.Out, opts.Key.ArchiveID(), opts.Key); err != nil {
		return nil, refuse("the key does not open this archive's key file", err)
	}
	units := &export.Units{Root: opts.Out, Key: opts.Key, RunStart: runStart}
	if manifest.Len() == 0 {
		if units.HasUnitFiles() {
			return nil, refuse("its manifest is missing or empty but sealed units are present — restore the manifest; nothing was written", nil)
		}
		return units, nil
	}
	for _, r := range manifest.All() {
		if r.Unit == "" {
			return nil, refuse("it holds records that are not encrypted (an existing plaintext archive is encrypted only by converting it)", nil)
		}
	}
	if n, err := units.SweepUnits(manifest, runStart); err != nil {
		return nil, refuse("its unit store is damaged", err)
	} else if n > 0 {
		logger.Printf("swept %d unit file(s) no record names, from an interrupted run", n)
	}
	return units, nil
}
