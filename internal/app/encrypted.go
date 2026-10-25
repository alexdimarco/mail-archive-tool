package app

import (
	"errors"
	"fmt"
	"log"
	"time"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/util"
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
	refuse := func(why string, cause error) error { return encRefusal(opts.Out, why, cause) }
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

// encRefusal is an ErrEncryptedArchive refusal naming out, why, and its cause.
func encRefusal(out, why string, cause error) error {
	if cause != nil {
		return fmt.Errorf("%w: %s: %s: %w", ErrEncryptedArchive, out, why, cause)
	}
	return fmt.Errorf("%w: %s: %s", ErrEncryptedArchive, out, why)
}

// preflightEncrypted is a keyed run's check before discovery, any folder, lock or
// record (P2a-C10): it refuses a relocated manifest, an out inside another
// encrypted archive (P2a-C8, whose parent walk the keyless guard does not run for
// a keyed run) and a key that does not open the vault in out. prepareEncrypted
// checks the key again under the lock (a conversion's swap, P2a-C9).
func preflightEncrypted(opts Options, credentials ...string) error {
	if opts.Manifest != "" {
		return encRefusal(opts.Out, "its manifest cannot be moved out of the archive folder (-manifest)", nil)
	}
	if insideEncrypted(opts.Out) {
		return encRefusal(opts.Out, "it is inside another encrypted archive, and an archive cannot be kept inside one", nil)
	}
	// MailArchive's settings, secrets, sign-in and working copies are plaintext: none
	// may be kept in the encrypted archive (P2a-C14). No exemption for an archive that
	// holds the settings folder (unlike a plaintext archive's warning, P10).
	paths := append(secretDirs(), opts.CredentialPaths...)
	for _, p := range append(paths, credentials...) {
		if p == "" {
			continue
		}
		if util.Within(util.ResolveExisting(opts.Out), util.ResolveExisting(p)) {
			return encRefusal(opts.Out, "it holds "+p+", which keeps MailArchive's settings, sign-in or working copies in plaintext — move the archive, or the settings, so neither is inside the other", nil)
		}
	}
	if err := vault.VerifyKey(opts.Out, opts.Key.ArchiveID(), opts.Key); err != nil {
		return encRefusal(opts.Out, "the key does not open this archive's key file", err)
	}
	return nil
}

// refuseOwedCollapse refuses a keyed capture into an archive that still owes the
// one-time identity collapse for one of its mailboxes: the collapse reads .eml files
// by logical path, which an encrypted archive has none of, so it would merge distinct
// messages (P2a-C7). A fresh encrypted archive (no records) owes nothing real and is
// simply marked collapsed by the run; one with records was encrypted wrongly — only
// the A12 conversion, which collapses first, encrypts an existing archive.
func refuseOwedCollapse(opts Options, manifest *state.Manifest, mailboxes []string) error {
	if manifest.Len() == 0 {
		return nil
	}
	for _, mbx := range mailboxes {
		if manifest.OwesCollapse(manifest.Token(state.MailboxSourceID(mbx), mbx)) {
			return encRefusal(opts.Out, "it still owes the one-time identity collapse, which cannot run on an encrypted archive", nil)
		}
	}
	return nil
}

// secretDirs are MailArchive's own folders that hold plaintext: its settings folder
// (job files, settings, the file secret store, sign-in caches) and its working
// folder (scratch copies); a var so a test can place them.
var secretDirs = func() []string {
	var dirs []string
	if d, err := job.Dir(); err == nil {
		dirs = append(dirs, d)
	}
	if d, err := scratchRoot(); err == nil {
		dirs = append(dirs, d)
	}
	return dirs
}

// openEncrypted is a keyed run's setup under the lock and BEFORE its "running"
// record: the manifest load and prepareEncrypted, so a refusal leaves no record of
// a manual run (P2a-C10). It returns the loaded manifest and the unit store.
func openEncrypted(opts Options, mpath string, runStart time.Time, logger *log.Logger) (*state.Manifest, *export.Units, error) {
	manifest, err := state.Load(mpath)
	if err != nil {
		return nil, nil, withKind(state.FailManifestUnreadable, err)
	}
	units, err := prepareEncrypted(opts, manifest, runStart, logger)
	if err != nil {
		return nil, nil, err
	}
	return manifest, units, nil
}
