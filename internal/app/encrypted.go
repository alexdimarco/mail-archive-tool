package app

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/lockfile"
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

// openSealedIndex opens an encrypted archive's search index from its sealed record
// (phase2b §6) — or, fresh, a new empty one in its place — and registers it with the
// store, whose commits seal its image when it changed. The sealed index is loaded
// only on first use, so a run with nothing new never decrypts it (P2b-C15): into
// memory, or above the memory budget into a scratch file (indexScratch). An index
// that cannot be loaded is replaced by an empty one and marked behind — a backup never
// stops for its index (P2b-C16); one a newer MailArchive wrote is refused, as Open
// refuses a newer search.db. nil, with the index marked behind, when the scratch
// file's drive has no room for it: the run goes on without its index. A scratch file
// a crashed run left was removed by openEncrypted.
func openSealedIndex(store *container.Store, out string, records int, fresh bool, logger *log.Logger) (*sealedIndex, error) {
	path, err := indexScratchPath(out, store.ArchiveID())
	if err != nil {
		return nil, err
	}
	fresh = fresh || !store.HasIndex()
	version, rows := store.IndexMeta()
	load, write := store.IndexImage, store.WriteIndexImage
	image := store.IndexSize()
	si := &sealedIndex{path: path}
	if fresh {
		load, write, version, rows, image, si.sealed = func() ([]byte, error) { return nil, nil }, nil, 0, 0, 0, -1 // captured at the first commit
	}
	onBad := func(err error) {
		logger.Printf("warning: %v", err)
		store.MarkIndexBehind()
		si.damaged = true
	}
	if need := indexScratchNeed(image, records); need == 0 {
		si.idx, err = index.OpenLazy(load, rows, version, onBad)
	} else {
		if free, short := scratchSpaceShort(filepath.Dir(path), need); short {
			logger.Printf("warning: the search index is not updated this run: working on it needs about %d MB on the drive that holds %s, which has %d MB free — free space on that drive; the next run with room catches it up", need>>20, filepath.Dir(path), free>>20)
			store.MarkIndexBehind()
			return nil, nil
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, fmt.Errorf("create MailArchive's working folder %s: %w", filepath.Dir(path), err)
		}
		si.inFile = true
		si.idx, err = index.OpenScratch(path, write, rows, version, onBad)
	}
	if err != nil {
		return nil, err
	}
	si.register(store)
	return si, nil
}

// indexMemoryBudget is the largest index image worked on in memory: about 3× the
// image is held while it is captured, within a 768 MiB budget (phase2b §6, P2b-C15).
// indexBytesPerRecord estimates an index's growth per message (the spike's 120 MB per
// 20,000 messages, rounded up). Vars so a test can lower them.
var (
	indexMemoryBudget   int64 = 256 << 20
	indexBytesPerRecord int64 = 6 << 10
)

// indexScratchNeed is the scratch space an encrypted archive's index needs — 0 when
// it is worked on in memory: an image, or an estimate for the archive's records, over
// the memory budget.
func indexScratchNeed(image int64, records int) int64 {
	est := max(image, int64(records)*indexBytesPerRecord)
	if est <= indexMemoryBudget {
		return 0
	}
	return est + scratchMargin
}

// indexScratchPath is where the index of the archive id at out is worked on above the
// memory budget: its scratch folder's index/search.db, outside the archive.
func indexScratchPath(out string, id vault.ID) (string, error) {
	d, err := archiveScratch(out, id)
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "index", "search.db"), nil
}

// ReaderScratchName matches a reader's scratch index, reader-<pid>-<n>.db, and the
// lock file beside it that says it is in use, reader-<pid>-<n>.db.lock.
var ReaderScratchName = regexp.MustCompile(`^reader-([0-9]+)-[0-9]+\.db(\.lock)?$`)

// ReaderScratch is where an encrypted archive's reader works on an index above the
// memory budget (design-encrypted-archives-phase3 §7.4, A33): a file of its own in
// the archive's scratch folder, named by the process that holds it and the load n,
// so neither a backup's index/search.db, nor another reader's, nor this reader's
// previous load's is touched. Its folder is made, owner-only. A file whose process
// is gone is a crash's leftover, removed by SweepReaderScratch.
func ReaderScratch(out string, id vault.ID, n uint64) (string, error) {
	d, err := archiveScratch(out, id)
	if err != nil {
		return "", err
	}
	dir := filepath.Join(d, "index")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(dir, fmt.Sprintf("reader-%d-%d.db", os.Getpid(), n)), nil
}

// SweepReaderScratch removes the scratch indexes readers of the archive id at out
// left when their process ended without closing them: plaintext, they end with the
// next keyed open (§7.4). A live reader holds an OS lock on the lock file beside its
// index for as long as it uses it (HoldReaderScratch) — beside, since the index's
// own file carries SQLite's locks — so an index whose lock file can be held, or has
// none, is a gone reader's, whatever pid its name carries, a pid reused since
// included.
func SweepReaderScratch(out string, id vault.ID) {
	d, err := archiveScratch(out, id)
	if err != nil {
		return
	}
	dir := filepath.Join(d, "index")
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		m := ReaderScratchName.FindStringSubmatch(e.Name())
		if m == nil || m[2] != "" || !e.Type().IsRegular() {
			continue
		}
		db := filepath.Join(dir, e.Name())
		f, err := lockfile.HoldFile(db + ".lock")
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			continue // a live reader's, or one that cannot be judged: kept
		}
		os.Remove(db)
		if f != nil {
			f.Close()
			os.Remove(db + ".lock")
		}
	}
	// A lock file whose index is gone, and that no reader holds.
	entries, _ = os.ReadDir(dir)
	for _, e := range entries {
		if m := ReaderScratchName.FindStringSubmatch(e.Name()); m != nil && m[2] != "" {
			p := filepath.Join(dir, e.Name())
			if f, err := lockfile.HoldFile(p); err == nil {
				f.Close()
				os.Remove(p)
			}
		}
	}
}

// HoldReaderScratch is a reader's OS lock on the lock file beside its scratch index
// at db, made now and kept for as long as the index is open; closing it releases
// the lock and removes the lock file.
func HoldReaderScratch(db string) (io.Closer, error) {
	f, err := os.OpenFile(db+".lock", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	f.Close()
	h, err := lockfile.HoldFile(db + ".lock")
	if err != nil {
		os.Remove(db + ".lock")
		return nil, err
	}
	return heldScratch{h, db + ".lock"}, nil
}

type heldScratch struct {
	f    *os.File
	path string
}

func (h heldScratch) Close() error {
	err := h.f.Close()
	os.Remove(h.path)
	return err
}

// removeScratchIndex removes the scratch index a crashed run of the archive id at out
// left: every keyed run does, under the archive lock and whatever its -index, so the
// plaintext it holds ends with the next run (the adversarial pass's LEAK-01).
func removeScratchIndex(out string, id vault.ID, logger *log.Logger) {
	path, err := indexScratchPath(out, id)
	if err == nil {
		if err = os.Remove(path); errors.Is(err, fs.ErrNotExist) {
			err = nil
		}
	}
	if err != nil {
		logger.Printf("warning: MailArchive's working copy of the search index could not be removed: %v", err)
	}
}

// unitFiles lists the unit files records name, for the sealed record's inventory.
// UnitLister is unitFiles for a keyed check's load (phase3 §8): the record's unit
// files and their digests, so the record can be compared with the file list.
func UnitLister(units *export.Units) container.UnitFiles { return unitFiles(units) }

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
	if err := vault.VerifyKey(opts.Out, recordedID(opts), opts.Key); err != nil {
		return encRefusal(opts.Out, "the key does not open this archive's key file", err)
	}
	return nil
}

// recordedID is the id a keyed run's key file must name: the one this computer
// recorded for the folder, or — only in a test that built the key directly — the
// key's own.
func recordedID(opts Options) vault.ID {
	if opts.RecordedID != (vault.ID{}) {
		return opts.RecordedID
	}
	return opts.Key.ArchiveID()
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
	if err := vault.VerifyKey(opts.Out, recordedID(opts), opts.Key); err != nil {
		return nil, nil, nil, encRefusal(opts.Out, "the key does not open this archive's key file", err)
	}
	removeScratchIndex(opts.Out, opts.Key.ArchiveID(), logger)
	SweepReaderScratch(opts.Out, opts.Key.ArchiveID())
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
	// The note beside the archive, checked against the digest the record sealed and
	// written again, its new digest sealed by this run's commit (§13.1). A setting's
	// change leaves it to the next backup, whose log tells a note changed meanwhile (the
	// PG4 pass, DUR-3).
	if !opts.settingOnly {
		refreshNote(opts.Out, store, logger, manifest.Save)
	}
	choice := opts.ProgramCopies
	if ps, st, _ := store.Program(); choice == ProgramCopiesUnset && (st == container.ProgramAbsent || st == container.ProgramOK && ps.Keep == nil) {
		choice = opts.ProgramCopiesIfUnchosen
	}
	sealProgramChoice(store, choice, logger)
	return manifest, units, store, nil
}

// ProgramChoice is the choice to keep copies of MailArchive in an encrypted archive.
type ProgramChoice int

const (
	ProgramCopiesUnset ProgramChoice = iota // leave the sealed choice as it is
	ProgramCopiesOn
	ProgramCopiesOff
)

// sealProgramChoice records the run's choice for its commit to seal (R21), keeping
// the rest of the set — an update under way included (R7). A set this MailArchive
// cannot read is kept as it is, with a warning (R6).
func sealProgramChoice(store *container.Store, choice ProgramChoice, logger *log.Logger) {
	if err := setProgramChoice(store, choice); err != nil {
		logger.Printf("warning: the choice to keep copies of MailArchive in this archive was not recorded: %v", err)
	}
}

// setProgramChoice sets the choice in the record, for its next save to seal.
func setProgramChoice(store *container.Store, choice ProgramChoice) error {
	if choice == ProgramCopiesUnset {
		return nil
	}
	ps, st, _ := store.Program()
	if st == container.ProgramAbsent {
		ps = container.ProgramSet{Format: container.ProgramFormat}
	}
	keep := choice == ProgramCopiesOn
	ps.Keep = &keep
	return store.SetProgram(ps)
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
