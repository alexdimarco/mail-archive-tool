package app

import (
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"time"

	"mail-archive-tool/internal/container"
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
// the sealed record's load and before the first write. It refuses an existing
// plaintext archive's records (records with no unit — only the A12 conversion
// encrypts one), makes a fresh archive's first commit (P2b-C3), and runs the
// start sweep of units and container files (P2a-C4, P2b-C9), which the store
// allows only beside a whole inventory.
func prepareEncrypted(opts Options, manifest *state.Manifest, store *container.Store, units *export.Units, runStart time.Time, logger *log.Logger) error {
	refuse := func(why string, cause error) error { return encRefusal(opts.Out, why, cause) }
	for _, r := range manifest.All() {
		if r.Unit == "" {
			return refuse("it holds records that are not encrypted (an existing plaintext archive is encrypted only by converting it)", nil)
		}
	}
	if store.Fresh() {
		err := store.Init(manifest)
		if errors.Is(err, container.ErrInventoryWrite) && !store.Fresh() {
			// The first record landed; only its file list did not, and the next commit
			// retries it (P2b-C7, the adversarial pass's CON-2).
			logger.Printf("warning: %v", err)
			return nil
		}
		if err != nil {
			return manifestSaveFailure(opts.Out, err)
		}
		return nil
	}
	units.OrphanAfter = store.SweepAfter()
	if !store.SweepAllowed() {
		return nil
	}
	n, err := units.SweepUnits(manifest, runStart, store.MayRemove)
	if err != nil {
		return refuse("its unit store is damaged", err)
	}
	c, err := store.SweepContainers(runStart)
	if err != nil {
		return refuse("its record's files are damaged", err)
	}
	if n+c > 0 {
		logger.Printf("swept %d unit and %d record file(s) nothing names: superseded during the last run, or left by an interrupted one", n, c)
	}
	return nil
}

// unitFiles lists the unit files records name, for the sealed record's inventory.
func unitFiles(units *export.Units) container.UnitFiles {
	return func(entries map[string]state.Record) (map[string]container.Digest, error) {
		out := map[string]container.Digest{}
		for _, r := range entries {
			if r.Sealed == nil {
				continue
			}
			for kind, s := range map[vault.UnitKind]*state.SealedUnit{vault.UnitMessage: r.Sealed.HTML, vault.UnitAttachment: r.Sealed.Zip, vault.UnitOriginal: r.Sealed.EML} {
				if s == nil {
					continue
				}
				rel, err := units.Path(r.Unit, kind, s.Rev)
				if err != nil {
					return nil, err
				}
				var d container.Digest
				if _, err := hex.Decode(d.SHA256[:], []byte(s.SHA256)); err != nil {
					return nil, fmt.Errorf("%w: a record's sealed digest is malformed", export.ErrUnitStore)
				}
				d.Size = s.Size
				out[rel] = d
			}
		}
		return out, nil
	}
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
// record: the key proven against the vault (P2a-C9c), the sealed record's strict
// load with the F12 checks (phase2b §2, §3), and prepareEncrypted — so a refusal
// leaves no record of a manual run (P2a-C10). check, when given, is the caller's own
// refusal on the loaded manifest, run before prepareEncrypted's first commit or
// sweep, so every refusal writes and deletes nothing. It returns the loaded
// manifest, the unit store and the sealed record's store.
func openEncrypted(opts Options, stillHeld func() error, runStart time.Time, logger *log.Logger, check func(*state.Manifest) error) (*state.Manifest, *export.Units, *container.Store, error) {
	if opts.Manifest != "" {
		return nil, nil, nil, encRefusal(opts.Out, "its manifest cannot be moved out of the archive folder (-manifest)", nil)
	}
	if err := vault.VerifyKey(opts.Out, opts.Key.ArchiveID(), opts.Key); err != nil {
		return nil, nil, nil, encRefusal(opts.Out, "the key does not open this archive's key file", err)
	}
	units := &export.Units{Root: opts.Out, Key: opts.Key, RunStart: runStart}
	manifest, store, err := container.LoadManifest(opts.Out, container.LoadOptions{
		Key: opts.Key, RecordedGen: opts.RecordedGen, RunStart: runStart, StillHeld: stillHeld, Units: unitFiles(units),
		Logf: logger.Printf,
	})
	if err != nil {
		return nil, nil, nil, recordRefusal(opts.Out, err)
	}
	if check != nil {
		if err := check(manifest); err != nil {
			return nil, nil, nil, err
		}
	}
	if err := prepareEncrypted(opts, manifest, store, units, runStart, logger); err != nil {
		return nil, nil, nil, err
	}
	return manifest, units, store, nil
}

// recordRefusal is a sealed record's load refusal as the run's refusal, worded by
// what the record is; its failure kind follows from the container error it wraps
// (FailureKind: record_older, manifest_unreadable).
func recordRefusal(out string, err error) error {
	switch {
	case errors.Is(err, container.ErrNewerFormat):
		return encRefusal(out, "it was written by a newer MailArchive — upgrade MailArchive", err)
	case errors.Is(err, container.ErrRestoredOlder), errors.Is(err, container.ErrUnfinishedSave), errors.Is(err, container.ErrInventoryDamaged):
		return encRefusal(out, "its record is older than the archive around it", err)
	case errors.Is(err, container.ErrNotSealed):
		return encRefusal(out, "it holds records that are not encrypted (an existing plaintext archive is encrypted only by converting it)", err)
	}
	return encRefusal(out, "its record cannot be read", err)
}
