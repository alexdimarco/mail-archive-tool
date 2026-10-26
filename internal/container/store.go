package container

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/vault"
)

// EnvelopeName is the cleartext A4 envelope at the manifest's name.
const EnvelopeName = ".mailarchive-manifest.json"

// maxEnvelopeSize bounds the envelope read: it is four short fields.
const maxEnvelopeSize = 4096

// The typed refusals of a keyed load (design-encrypted-archives-phase2b §2, §3).
// Each names the archive and its remedy where it is raised; a refusal writes
// nothing and deletes nothing.
var (
	// ErrRecord: the sealed record is missing, damaged or not this archive's.
	ErrRecord = errors.New("container: the encrypted archive's record is missing or damaged")
	// ErrNotSealed: the record file holds something other than a sealed record's
	// envelope — a plaintext manifest (an archive that is not encrypted, or one a
	// pre-release build wrote) or a planted downgrade. Never migrated.
	ErrNotSealed = errors.New("container: the archive's record is not a sealed record")
	// ErrRestoredOlder: the record is older than the archive around it (F12).
	ErrRestoredOlder = errors.New("container: the archive's record is older than its file list")
	// ErrUnfinishedSave: the last save's pointer rename was lost, or an older
	// copy of the record was restored (I = P + 1).
	ErrUnfinishedSave = errors.New("container: the archive's last save did not finish, or an older copy of its record was restored")
	// ErrInventoryDamaged: the inventory that existed is missing or damaged.
	ErrInventoryDamaged = errors.New("container: the archive's file list is missing or damaged")
	// ErrInventoryWrite: a commit landed but its inventory could not be saved;
	// the next commit retries (P2b-C7). Not a failed save of the record.
	ErrInventoryWrite = errors.New("container: this backup's file list could not be saved; the next backup retries")
	// ErrLockLost: the archive lock was lost before the commit point; nothing was
	// renamed.
	ErrLockLost = errors.New("container: the archive lock was lost before the record was saved")
	// ErrReadOnly: a record a reader loaded (LoadOptions.ReadOnly) is never saved,
	// initialised or swept.
	ErrReadOnly = errors.New("container: this record was opened to read; nothing was written")
)

// UnitFiles lists the unit files a manifest's records name: archive-relative
// path → ciphertext digest. The unit store provides it (it derives the names).
type UnitFiles func(entries map[string]state.Record) (map[string]Digest, error)

// LoadOptions are a keyed load's inputs. (Not named Options: the MA-726
// reachability scan reads an Options literal that sets Key as app.Options's.)
type LoadOptions struct {
	Key *vault.Key
	// RecordedGen is the highest generation this machine committed, recorded
	// outside the archive (A25; persisted in Phases 3-4). Zero: none recorded.
	RecordedGen uint64
	// RunStart is the run's start: the sweep's age bound and the mtime base.
	RunStart time.Time
	// StillHeld re-proves the archive lock right before the commit point.
	StillHeld func() error
	// Units lists the unit files the records name.
	Units UnitFiles
	// Logf, when given, logs each notice as it is raised — at load and at every
	// commit (a mirror that could not be written, an envelope healed).
	Logf func(format string, args ...any)
	// ReadOnly is a reader's load (design-encrypted-archives-phase3 §7.3), taken
	// without the archive lock: Commit, Init and SweepContainers refuse with
	// ErrReadOnly, and reading the index records nothing in the Store, so two reads
	// may share it.
	ReadOnly bool
}

// Store is an encrypted archive's sealed record as a run holds it: the state.Persister
// its manifest commits through, the last committed pointer, and the inventory.
type Store struct {
	out  string
	opts LoadOptions

	ptr       Pointer             // the last committed pointer (Gen 0: none yet)
	attempted uint64              // the highest generation a rename was attempted at
	sections  map[uint32][32]byte // plaintext digest per manifest section position
	reseal    map[uint32]bool     // positions loaded from their second copy
	hdr       header

	inv         *Top              // the last durable inventory (nil: none)
	shards      map[string][]byte // its shard bytes by prefix, as written
	sweepOK     bool              // whether the start sweep may run
	invLive     map[string]bool   // paths the inventory lists as live
	invRetained map[string]bool   // paths the inventory lists as retained
	retained    map[string]Digest // files superseded during this run
	prevLive    map[string]Digest // the live files of the last commit

	commits   int       // commits this run
	fixedTime time.Time // the last mtime given to the pointer pair and top file
	runStamp  time.Time // this run's LastSeen stamp
	walkDone  map[string]time.Time
	fresh     bool
	envHeal   bool // the envelope was missing or unreadable: the next commit rewrites it
	// invTolerated: a shard was damaged under a current top file; this run sweeps
	// nothing and rewrites every shard.
	invTolerated bool

	// The sealed timeline (§5): the run's buffered events, the open last chunk's
	// plaintext once read, the run header a new chunk copies, whether the last chunk
	// could not be read, whether a failed staging holds events back, and the part of
	// the header's gap that is lost for good (markHistoryLoss).
	indexSource  func() (IndexCapture, bool, error) // the run's in-memory index (SetIndex)
	indexDigests [][32]byte                         // plaintext digest per sealed index chunk, once loaded or committed

	timeline       *sealedTimeline
	histTail       []byte
	histHeader     *state.HistoryEvent
	histTailLoaded bool
	histTailBad    bool
	histGapStaging bool
	histLoss       *Gap
	Notices        []string // what the load healed or accepted, for the run log
}

func (s *Store) refuse(base error, format string, args ...any) error {
	return fmt.Errorf("%w: %s: %s", base, s.out, fmt.Sprintf(format, args...))
}

// LoadManifest opens the sealed record of the encrypted archive in out with
// o.Key, under the archive lock: it reads the envelope, the pointer pair, the
// inventory and the manifest chunks, applies every row of the strict-load table
// and the F12 rule, and returns the manifest bound to the Store it commits
// through. It writes nothing; a fresh archive's first commit is Init.
func LoadManifest(out string, o LoadOptions) (*state.Manifest, *Store, error) {
	s := &Store{out: out, opts: o, sections: map[uint32][32]byte{}, reseal: map[uint32]bool{}, retained: map[string]Digest{}, walkDone: map[string]time.Time{}}
	env, err := s.readEnvelope()
	if err != nil {
		return nil, nil, err
	}
	ptr, ptrFound, newer, err := s.readPointers()
	if err != nil {
		return nil, nil, err
	}
	// The envelope is judged only now: an authentic pointer proves the archive, so a
	// damaged envelope beside one is healed, never trusted over it (the adversarial
	// pass, CRIT-1). A plaintext manifest at its name is refused either way.
	switch {
	case env == envPlain:
		return nil, nil, s.refuse(ErrNotSealed, "it holds a manifest that is not a sealed record — an archive that is not encrypted is encrypted only by converting it; nothing was changed")
	case ptrFound:
	case newer || env == envNewer:
		return nil, nil, s.refuse(ErrNewerFormat, "its record was written by a newer MailArchive — upgrade MailArchive")
	case env == envOther:
		return nil, nil, s.refuse(ErrRecord, "its record names another archive or another pointer; nothing was changed")
	}
	if !ptrFound {
		if err := s.checkFresh(env); err != nil {
			return nil, nil, err
		}
		s.fresh = true
		m, err := state.FromSealed(filepath.Join(out, EnvelopeName), state.View{Version: state.ManifestVersion}, s)
		return m, s, err
	}
	s.ptr, s.attempted = ptr, ptr.Gen
	if err := s.readInventory(); err != nil {
		return nil, nil, err
	}
	sections, err := s.readSections()
	if err != nil {
		return nil, nil, err
	}
	v, h, err := decodeSections(s.opts.Key, sections, s.ptr.Buckets)
	if errors.Is(err, ErrNewerFormat) {
		return nil, nil, s.refuse(ErrNewerFormat, "%v — upgrade MailArchive", err)
	}
	if err != nil {
		return nil, nil, s.refuse(ErrRecord, "%v", err)
	}
	s.hdr, s.histLoss = h, h.HistoryGap
	if err := s.checkF12(v); err != nil {
		return nil, nil, err
	}
	if env != envPresent {
		s.notice("the record's envelope was missing or damaged; the next save rewrites it")
	}
	s.envHeal = env != envPresent
	m, err := state.FromSealed(filepath.Join(out, EnvelopeName), v, s)
	var nf *state.NewerFormatError
	if errors.As(err, &nf) {
		return nil, nil, s.refuse(ErrNewerFormat, "%v", err)
	}
	if err != nil {
		return nil, nil, s.refuse(ErrRecord, "%v", err)
	}
	live, err := s.liveFiles(v)
	if err != nil {
		return nil, nil, err
	}
	s.prevLive = live
	return m, s, nil
}

type envelopeState int

const (
	envAbsent     envelopeState = iota
	envPresent                  // this archive's envelope, exactly
	envUnreadable               // not JSON, or not an envelope's shape: healable beside an authentic pointer
	envNewer                    // an envelope from a newer MailArchive
	envOther                    // an envelope naming another archive or another pointer
	envPlain                    // a plaintext manifest at the envelope's name: never a sealed record
)

// readEnvelope classifies the envelope; LoadManifest decides once the pointers are
// read. Version first (newer is its own class), then the strict fields. A file too
// large for an envelope, or JSON carrying records, is a plaintext manifest.
func (s *Store) readEnvelope() (envelopeState, error) {
	raw, _, err := readFile(s.out, EnvelopeName, maxEnvelopeSize)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return envAbsent, nil
	case errors.Is(err, errTooLarge):
		// An envelope is four short fields: anything larger at its name is a manifest
		// file — an archive that is not encrypted — never a damaged envelope.
		return envPlain, nil
	case errors.Is(err, ErrStore):
		return envAbsent, s.refuse(ErrRecord, "%v", err)
	case err != nil:
		return envUnreadable, nil
	}
	var probe struct {
		Version int             `json:"version"`
		Entries json.RawMessage `json:"entries"`
	}
	if json.Unmarshal(raw, &probe) != nil {
		return envUnreadable, nil
	}
	if probe.Entries != nil {
		return envPlain, nil
	}
	if probe.Version > vault.EncryptedManifestVersion {
		return envNewer, nil
	}
	var e vault.ManifestEnvelope
	if strictJSON(raw, &e) != nil || e.Version != vault.EncryptedManifestVersion || !e.Encrypted {
		return envUnreadable, nil
	}
	if e.ArchiveID != s.opts.Key.ArchiveID().String() || e.Pointer != PointerName {
		return envOther, nil
	}
	return envPresent, nil
}

// readPointers opens both pointer copies; the highest generation that
// authenticates wins. A copy in a newer format decides only when it is not
// outranked: newer reports a newer-format copy whose cleartext generation is
// above every authenticating copy's (a newer MailArchive's later commit), while
// one at or below it is damage the good copy covers for (the adversarial pass,
// CRIT-2).
func (s *Store) readPointers() (best Pointer, found, newer bool, err error) {
	failed := 0
	var newerGen uint64
	for _, name := range []string{PointerName, MirrorName} {
		raw, fi, rerr := readFile(s.out, name, MaxPointerSize)
		if errors.Is(rerr, fs.ErrNotExist) {
			continue
		}
		if rerr == nil {
			if _, gen, perr := Peek(raw); errors.Is(perr, ErrNewerFormat) {
				newerGen = max(newerGen, gen)
				continue
			}
			if p, oerr := Open(s.opts.Key, raw); oerr == nil {
				if fi.ModTime().After(s.fixedTime) {
					s.fixedTime = fi.ModTime()
				}
				if !found || p.Gen > best.Gen {
					best, found = p, true
				}
				continue
			}
		}
		failed++
	}
	switch {
	case newerGen > 0 && (!found || newerGen > best.Gen):
		return Pointer{}, false, true, nil
	case newerGen > 0:
		s.notice("a copy of the record pointer is damaged; the next save rewrites it")
	case !found && failed > 0:
		return Pointer{}, false, false, s.refuse(ErrRecord, "both copies of its record pointer (%s, %s) are damaged — restore them from your newest backup; nothing was changed", PointerName, MirrorName)
	}
	return best, found, false, nil
}

// checkFresh decides whether a folder with no record pointer is a new archive whose
// first commit is owed: units/ and inventory/ empty, chunks/ holding at most an
// interrupted first commit's leftovers (chunk-shaped files, temps, empty shard
// folders — no pointer names them, CON-1), no sign of an earlier life (a search
// index or a go-back history), and no generation this machine recorded committing
// (the adversarial pass, CRIT-3). Anything else is a record that went missing.
func (s *Store) checkFresh(env envelopeState) error {
	missing := func(why string) error {
		return s.refuse(ErrRecord, "%s — restore the archive's record files (%s, %s, %s/) from your newest backup; nothing was changed", why, PointerName, MirrorName, ChunksDir)
	}
	if s.opts.RecordedGen > 0 {
		return missing(fmt.Sprintf("this computer saved this archive's record %d times but its record is missing", s.opts.RecordedGen))
	}
	for _, name := range []string{"search.db", ".mailarchive-history.jsonl"} {
		if exists(s.out, name) {
			return missing("the archive has been used (it holds " + name + ") but its record is missing")
		}
	}
	for _, d := range []string{"units", InventoryDir} {
		if n, err := s.countEntries(d); err != nil {
			return err
		} else if n > 0 {
			return missing("sealed files are present but the record is missing")
		}
	}
	return s.walkLeftovers()
}

// countEntries is how many entries the folder d holds (0 when absent), refusing a
// planted non-folder.
func (s *Store) countEntries(d string) (int, error) {
	dir := filepath.Join(s.out, d)
	ok, err := realDir(dir)
	if err != nil {
		return 0, s.refuse(ErrRecord, "%v", err)
	}
	if !ok {
		return 0, nil
	}
	entries, err := os.ReadDir(dir)
	return len(entries), err
}

// walkLeftovers accepts a chunks/ folder holding only what an interrupted first
// commit leaves: shard folders of chunk-shaped files and in-flight temps.
func (s *Store) walkLeftovers() error {
	root := filepath.Join(s.out, ChunksDir)
	ok, err := realDir(root)
	if err != nil || !ok {
		if err != nil {
			return s.refuse(ErrRecord, "%v", err)
		}
		return nil
	}
	shards, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, sh := range shards {
		if _, err := realDir(filepath.Join(root, sh.Name())); err != nil || len(sh.Name()) != 2 {
			return s.refuse(ErrRecord, "sealed files are present but the record is missing — restore the archive's record files from your newest backup; nothing was changed")
		}
		files, err := os.ReadDir(filepath.Join(root, sh.Name()))
		if err != nil {
			return err
		}
		for _, f := range files {
			rel := ChunksDir + "/" + sh.Name() + "/" + f.Name()
			if !f.Type().IsRegular() || !(filePath.MatchString(rel) || tempName.MatchString(f.Name())) {
				return s.refuse(ErrRecord, "sealed files are present but the record is missing — restore the archive's record files from your newest backup; nothing was changed")
			}
		}
	}
	return nil
}

// tempName is an in-flight write's name (tempPattern).
var tempName = regexp.MustCompile(`^\.mailarchive-[0-9]+\.tmp$`)

// notice records what a load or commit healed or accepted, and logs it at once when
// the caller gave a logger (the adversarial pass, LEAK-3).
func (s *Store) notice(msg string) {
	s.Notices = append(s.Notices, msg)
	if s.opts.Logf != nil {
		s.opts.Logf("note: %s", msg)
	}
}

// readInventory reads the top file and its shards. A newer version asks for an
// upgrade; a damaged shard under an otherwise current top file is tolerated (no
// sweep, every shard rewritten); anything else unreadable is "no inventory".
func (s *Store) readInventory() error {
	raw, _, err := readFile(s.out, InventoryName, int64(maxShards+5)*256)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return nil
	}
	top, err := ParseTop(raw)
	if err != nil {
		// A newer version is no exception: an inventory format change comes with a
		// pointer format change, which asks for the upgrade; a top line that alone
		// reads as newer is damage (the adversarial pass, CRIT-2).
		s.notice("the archive's file list could not be read")
		return nil
	}
	// A file list written for this very generation must name the pointer on disk: a
	// top file at I = P from another copy of the archive (two writers on one synced
	// folder) is not this record's (OUT-2).
	if top.Gen == s.ptr.Gen && !s.pointerMatches(top.Pointer) && (top.Mirror == nil || !s.pointerMatches(*top.Mirror)) {
		s.notice("the archive's file list belongs to another copy of its record")
		return nil
	}
	shards := map[string][]byte{}
	live, retained := map[string]bool{}, map[string]bool{}
	intact := true
	for _, ref := range top.Shards {
		// Bounded by what a writer produces for this many lines, judged before the
		// read; the first bad shard decides, so a planted list cannot make the load
		// read without end (OUT-3).
		limit := int64(ref.Entries) * maxShardLine
		if fi, err := os.Lstat(filepath.Join(s.out, filepath.FromSlash(ref.Name))); err != nil || fi.Size() > limit {
			intact = false
			break
		}
		b, _, err := readShard(s.out, ref.Name, limit)
		if err != nil || sha256.Sum256(b) != ref.SHA256 {
			intact = false
			break
		}
		sh, err := ParseShard(ref.Prefix, b)
		if err != nil || len(sh.Files) != ref.Entries {
			intact = false
			break
		}
		shards[ref.Prefix] = b
		for _, f := range sh.Files {
			if f.State == Live {
				live[f.Path] = true
			} else {
				retained[f.Path] = true
			}
		}
	}
	if !intact {
		if top.Gen == s.ptr.Gen {
			s.inv, s.shards, s.invLive, s.invRetained, s.sweepOK = &top, nil, nil, nil, false
			s.notice("a damaged part of the archive's file list is rewritten by this backup; nothing is swept this run")
			s.invTolerated = true
		}
		return nil
	}
	s.inv, s.shards, s.invLive, s.invRetained, s.sweepOK = &top, shards, live, retained, true
	return nil
}

// maxShardLine bounds one shard line: a unit or chunk path, a hex digest, a size
// and a state.
const maxShardLine = 160

// readShard reads an inventory shard; a var so a test can see what each read asks for.
var readShard = readFile

// pointerMatches reports whether d is the digest of either pointer copy on disk.
func (s *Store) pointerMatches(d Digest) bool {
	for _, name := range []string{PointerName, MirrorName} {
		raw, _, err := readFile(s.out, name, MaxPointerSize)
		if err == nil && int64(len(raw)) == d.Size && sha256.Sum256(raw) == d.SHA256 {
			return true
		}
	}
	return false
}

// readSections opens every manifest section from whichever of its two copies
// authenticates. Both copies failing refuses, naming the file and its date.
func (s *Store) readSections() ([][]byte, error) {
	byPos := map[uint32][]Entry{}
	for _, e := range s.ptr.Manifest {
		byPos[e.I] = append(byPos[e.I], e)
	}
	n := posFirst + int(s.ptr.Buckets)
	if len(s.ptr.Manifest) == 0 {
		n = 0
	}
	if len(byPos) != n {
		return nil, s.refuse(ErrRecord, "its record pointer lists %d sections, want %d", len(byPos), n)
	}
	out := make([][]byte, n)
	for pos := 0; pos < n; pos++ {
		copies := byPos[uint32(pos)]
		if len(copies) != 2 {
			return nil, s.refuse(ErrRecord, "its record pointer lists %d copies of section %d, want 2", len(copies), pos)
		}
		var date time.Time
		for j, e := range copies {
			sealed, fi, err := readChunk(s.out, e)
			if fi != nil && date.IsZero() {
				date = fi.ModTime()
			}
			if err == nil {
				plain, oerr := s.opts.Key.OpenChunk(vault.ContainerManifest, e.Gen, e.I, sealed)
				if oerr == nil {
					out[pos] = plain
					s.sections[uint32(pos)] = sha256.Sum256(plain)
					if j > 0 {
						s.reseal[uint32(pos)] = true
						s.notice("a copy of a record chunk is damaged; the next save writes both copies again")
					} else if _, _, cerr := readChunk(s.out, copies[1]); cerr != nil {
						// The second copy is checked too — size and digest, no decryption —
						// so a lost or damaged mirror chunk is healed, never silently one
						// copy (OUT-1).
						s.reseal[uint32(pos)] = true
						s.notice("a copy of a record chunk is missing or damaged; the next save writes both copies again")
					}
					break
				}
			}
		}
		if out[pos] == nil {
			if date.IsZero() {
				return nil, s.refuse(ErrRecord, "both copies of a record chunk (%s) are missing — restore it from your newest backup; nothing was changed", copies[0].Name.Path())
			}
			return nil, s.refuse(ErrRecord, "both copies of a record chunk (%s) are damaged — restore this one file from any backup made after %s; nothing was changed", copies[0].Name.Path(), date.UTC().Format("2006-01-02"))
		}
	}
	return out, nil
}

// checkF12 applies the F12 rule (§3) and the RecordedGen rule (A25).
func (s *Store) checkF12(v state.View) error {
	P := s.ptr.Gen
	switch {
	case s.inv == nil && s.ptr.Inv >= 1:
		return s.refuse(ErrInventoryDamaged, "its file list is missing or damaged — restore the whole archive folder from your newest backup; nothing was changed")
	case s.inv == nil:
		// No file list that ever landed: proceed, and sweep nothing (sweepOK is set
		// only beside a whole inventory).
	case s.inv.Gen == P+1:
		return s.refuse(ErrUnfinishedSave, "nothing was changed")
	case s.inv.Gen > P:
		return s.refuse(ErrRestoredOlder, "an older copy of the record may have been restored (record at save %d, file list at %d) — restore the newest copy of the whole archive folder; nothing was changed", P, s.inv.Gen)
	case s.inv.Gen < s.ptr.Inv:
		return s.refuse(ErrRestoredOlder, "an older copy of its file list was restored (file list at save %d, the record saw %d) — restore the newest copy of the whole archive folder; nothing was changed", s.inv.Gen, s.ptr.Inv)
	}
	if s.opts.RecordedGen == P+1 {
		// This machine's last commit landed, then its pointer rename was lost (a power
		// cut before the folder reached disk): the last save did not finish — not a
		// merge (CON-6).
		return s.refuse(ErrUnfinishedSave, "this computer's last save (%d) is not on disk; nothing was changed", s.opts.RecordedGen)
	}
	if s.opts.RecordedGen > P {
		merged, err := s.unnamedFiles(v)
		if err != nil {
			return err
		}
		if merged {
			return s.refuse(ErrRestoredOlder, "this archive is older than the last backup this computer made (save %d, archive at %d), and holds files its record does not name; nothing was changed", s.opts.RecordedGen, P)
		}
		s.notice(fmt.Sprintf("this archive was restored to an older copy (save %d; this computer had made %d)", P, s.opts.RecordedGen))
	}
	return nil
}

// unnamedFiles reports whether units/ or chunks/ holds a file that neither the
// pointer and its records nor the inventory names: a merge-restore's signature.
func (s *Store) unnamedFiles(v state.View) (bool, error) {
	named, err := s.liveFiles(v)
	if err != nil {
		return false, err
	}
	listed := s.listedFiles()
	found := false
	err = s.walkFiles(func(rel string, _ os.FileInfo) {
		if _, ok := named[rel]; !ok && !listed[rel] {
			found = true
		}
	})
	return found, err
}

// listedFiles is every path the inventory lists, in any state.
func (s *Store) listedFiles() map[string]bool {
	out := map[string]bool{}
	for prefix, b := range s.shards {
		if sh, err := ParseShard(prefix, b); err == nil {
			for _, f := range sh.Files {
				out[f.Path] = true
			}
		}
	}
	return out
}

// walkFiles calls fn for every unit- or chunk-shaped file under units/ and chunks/.
func (s *Store) walkFiles(fn func(rel string, fi os.FileInfo)) error {
	for _, top := range []string{"units", ChunksDir} {
		root := filepath.Join(s.out, top)
		if ok, err := realDir(root); err != nil || !ok {
			if err != nil {
				return s.refuse(ErrRecord, "%v", err)
			}
			continue
		}
		shards, err := os.ReadDir(root)
		if err != nil {
			return err
		}
		for _, sh := range shards {
			if sh.Type().IsRegular() {
				continue
			}
			dir := filepath.Join(root, sh.Name())
			if _, err := realDir(dir); err != nil {
				return s.refuse(ErrRecord, "%v", err)
			}
			files, err := os.ReadDir(dir)
			if err != nil {
				return err
			}
			for _, f := range files {
				rel := top + "/" + sh.Name() + "/" + f.Name()
				if !filePath.MatchString(rel) {
					continue
				}
				fi, err := f.Info()
				if err != nil {
					continue
				}
				fn(rel, fi)
			}
		}
	}
	return nil
}

// liveFiles is every file the records and the pointer name, with its digest.
func (s *Store) liveFiles(v state.View) (map[string]Digest, error) {
	files := map[string]Digest{}
	if s.opts.Units != nil {
		u, err := s.opts.Units(v.Entries)
		if err != nil {
			return nil, err
		}
		for p, d := range u {
			files[p] = d
		}
	}
	for _, sec := range [][]Entry{s.ptr.Manifest, s.ptr.History, s.ptr.Index} {
		for _, e := range sec {
			files[e.Name.Path()] = Digest{SHA256: e.SHA256, Size: int64(e.Size)}
		}
	}
	return files, nil
}

// Peeked is what PeekFile saw of the record pointer.
type Peeked struct {
	Gen    uint64   // the generation it claims (0: none, or not a pointer)
	SHA256 [32]byte // the digest of its bytes (zero: no pointer)
}

// PeekFile reads out's record pointer without the key, for a reader that loads
// without the archive lock (design-encrypted-archives-phase3 §7.3): what it returns
// before and after a load differs when a backup committed in between. The primary
// pointer is the commit point — renamed first — so its bytes tell every commit; the
// claimed generation is compared, never trusted. No pointer yet is the zero Peeked;
// only an error reading it is an error.
func PeekFile(out string) (Peeked, error) {
	raw, _, err := readFile(out, PointerName, MaxPointerSize)
	if errors.Is(err, fs.ErrNotExist) {
		return Peeked{}, nil
	}
	if err != nil {
		return Peeked{}, err
	}
	_, gen, _ := Peek(raw)
	return Peeked{Gen: gen, SHA256: sha256.Sum256(raw)}, nil
}

// Fresh reports whether the archive has no record yet: its first commit is owed.
func (s *Store) Fresh() bool { return s.fresh }

// Generation is the last committed generation.
func (s *Store) Generation() uint64 { return s.ptr.Gen }

// SweepAllowed reports whether the start sweep may run (§3): never with no
// inventory, nor with a damaged one.
func (s *Store) SweepAllowed() bool { return s.sweepOK }

// KeepUnits is the set of unit paths the inventory lists as live: the start
// sweep keeps them as well as what the records name.
func (s *Store) KeepUnits() map[string]bool { return s.invLive }

// SweepAfter is the start of the run that made the record's last commit. A file
// nothing names and the inventory does not list as retained is removable only when
// a run since then wrote it (its mtime is that run's start): an older one came from
// somewhere else — a merge, a half-synced copy, another device — and is kept
// (the adversarial pass, CRIT-3b, OUT-6).
func (s *Store) SweepAfter() time.Time {
	if s.ptr.Gen == 0 {
		return time.Time{}
	}
	return time.Unix(s.ptr.RunStart, 0)
}

// MayRemove reports whether the start sweep may remove the unnamed file rel whose
// mtime is mtime: never one the inventory lists as live; one it lists as retained
// (superseded during the last run); otherwise only one written since the record's
// last commit.
func (s *Store) MayRemove(rel string, mtime time.Time) bool {
	if s.invLive[rel] {
		return false
	}
	return s.invRetained[rel] || !mtime.Before(s.SweepAfter())
}

// SetRunStamp is this run's LastSeen stamp (graph runAt).
func (s *Store) SetRunStamp(t time.Time) { s.runStamp = t }

// WalkDone records that the walk of the mailbox token finished at the run stamp
// at: the next commit moves that mailbox's seen_at forward (P2b-C12).
func (s *Store) WalkDone(token string, at time.Time) { s.walkDone[token] = at }

// SweepContainers removes, under the lock and before the first write, chunk files
// the pointer does not name and the inventory does not list as live, and
// inventory files the top file does not name, when older than before. It returns
// how many it removed. With the sweep not allowed it removes nothing.
func (s *Store) SweepContainers(before time.Time) (int, error) {
	if s.opts.ReadOnly {
		return 0, s.refuse(ErrReadOnly, "the sweep did not run")
	}
	if !s.sweepOK {
		return 0, nil
	}
	named := map[string]bool{}
	for _, sec := range [][]Entry{s.ptr.Manifest, s.ptr.History, s.ptr.Index} {
		for _, e := range sec {
			named[e.Name.Path()] = true
		}
	}
	removed := 0
	err := s.walkFiles(func(rel string, fi os.FileInfo) {
		if !strings.HasPrefix(rel, ChunksDir+"/") || named[rel] || !s.MayRemove(rel, fi.ModTime()) || !fi.Mode().IsRegular() || !fi.ModTime().Before(before) {
			return
		}
		if os.Remove(filepath.Join(s.out, filepath.FromSlash(rel))) == nil {
			removed++
		}
	})
	if err != nil {
		return removed, err
	}
	keep := map[string]bool{}
	if s.inv != nil {
		for _, ref := range s.inv.Shards {
			keep[ref.Name] = true
		}
	}
	dir := filepath.Join(s.out, InventoryDir)
	if ok, err := realDir(dir); err != nil {
		return removed, s.refuse(ErrRecord, "%v", err)
	} else if ok {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return removed, err
		}
		for _, e := range entries {
			rel := InventoryDir + "/" + e.Name()
			fi, err := e.Info()
			if err != nil || keep[rel] || !fi.Mode().IsRegular() || !fi.ModTime().Before(before) {
				continue
			}
			if os.Remove(filepath.Join(dir, e.Name())) == nil {
				removed++
			}
		}
	}
	return removed, nil
}

// nextFixedTime is the mtime of the next commit's pointer pair and top file:
// run start + 2 s × the commit's number, never at or before the last one (P2b-C10).
func (s *Store) nextFixedTime() time.Time {
	t := s.opts.RunStart.Add(time.Duration(2*(s.commits+1)) * time.Second).Truncate(time.Second)
	if floor := s.fixedTime.Truncate(time.Second).Add(2 * time.Second); !s.fixedTime.IsZero() && t.Before(floor) {
		t = floor // at least 2 s after the last one, whole seconds (CON-5)
	}
	return t
}

// Init is a fresh archive's first commit (P2b-C3): the envelope first, then an
// empty generation-1 pointer, its mirror and inventory 1, before any unit is
// written.
func (s *Store) Init(m *state.Manifest) error {
	if s.opts.ReadOnly {
		return s.refuse(ErrReadOnly, "its first save did not run")
	}
	if !s.fresh {
		return nil
	}
	// An interrupted first commit's leftovers (checkFresh allowed only chunk-shaped
	// files and temps, which no pointer names) are removed under the lock first, so
	// none outlives the record that now starts (CON-1).
	if root := filepath.Join(s.out, ChunksDir); exists(s.out, ChunksDir) {
		filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err == nil && d.Type().IsRegular() {
				os.Remove(p)
			}
			return nil
		})
	}
	if err := s.writeEnvelope(); err != nil {
		return err
	}
	err := m.Save()
	if s.ptr.Gen > 0 {
		// The pointer landed: the record exists, whatever its inventory did (CON-2).
		s.fresh = false
	}
	return err
}

func (s *Store) writeEnvelope() error {
	b, err := vault.NewManifestEnvelope(s.opts.Key.ArchiveID(), PointerName).Marshal()
	if err != nil {
		return err
	}
	return writeFile(s.out, EnvelopeName, b, time.Time{})
}

// Commit is the sealed save (state.Persister): changed sections sealed twice
// under fresh names, the lock re-proved, the pointer renamed into place [COMMIT],
// the mirror, then the inventory. Nothing is deleted (P2b-C9).
func (s *Store) Commit(v state.View) error {
	if s.opts.ReadOnly {
		return s.refuse(ErrReadOnly, "the record was not saved")
	}
	for token, at := range s.walkDone {
		if s.hdr.SeenAt == nil {
			s.hdr.SeenAt = map[string]time.Time{}
		}
		s.hdr.SeenAt[token] = at
	}
	gen := s.attempted + 1
	// A commit that does not land leaves the header as it found it where its staging
	// changed it ahead of the pointer: the index's row count (the adversarial pass's
	// CON-06) and a history hold cleared below.
	gap, rows, landed := s.hdr.HistoryGap, s.hdr.IndexRows, false
	defer func() {
		if !landed {
			s.hdr.HistoryGap, s.hdr.IndexRows = gap, rows
		}
	}()
	// The timeline first: a history that cannot be staged keeps its events buffered
	// for the next commit and records the gap in THIS commit's header (§1, P2b-C14);
	// the commit that lands them seals the header without that hold (FID-02).
	hist, herr := s.stageHistory(gen)
	switch {
	case herr != nil:
		s.holdHistory(gen)
		hist = stagedHistory{table: s.ptr.History}
	case hist.n > 0 && s.histGapStaging:
		s.hdr.HistoryGap = s.histLoss
	}
	// Then the index, also before the manifest's sections: a commit that cannot seal
	// it records index_behind in its own header (§1, P2b-C16).
	idx := s.stageIndex(gen)
	sections, n, err := encodeSections(s.opts.Key, v, s.hdr, s.ptr.Buckets, s.runStamp)
	if err != nil {
		return err
	}
	old := map[uint32][]Entry{}
	for _, e := range s.ptr.Manifest {
		old[e.I] = append(old[e.I], e)
	}
	var table []Entry
	digests := map[uint32][32]byte{}
	for i, plain := range sections {
		pos := uint32(i)
		d := sha256.Sum256(plain)
		digests[pos] = d
		if prev, ok := s.sections[pos]; ok && prev == d && !s.reseal[pos] && n == s.ptr.Buckets && len(old[pos]) == 2 {
			table = append(table, old[pos]...)
			continue
		}
		for c := 0; c < 2; c++ {
			e, err := s.writeChunk(vault.ContainerManifest, gen, pos, plain)
			if err != nil {
				return err
			}
			table = append(table, e)
		}
	}
	if s.opts.StillHeld != nil {
		if err := s.opts.StillHeld(); err != nil {
			return fmt.Errorf("%w: %v", ErrLockLost, err)
		}
	}
	p := Pointer{Head: s.ptr.Head, Manifest: table, History: hist.table, Index: idx.table}
	p.Format, p.Gen, p.Buckets = vault.FormatVersion, gen, n
	p.IndexImage, p.IndexChunk, p.IndexMeta = idx.length, 0, idx.version
	if len(idx.table) > 0 {
		p.IndexChunk = indexChunkSize
	}
	p.RunStart = s.opts.RunStart.Unix()
	if s.inv != nil {
		p.Inv = s.inv.Gen
	}
	raw, err := Seal(s.opts.Key, p)
	if err != nil {
		return err
	}
	mt := s.nextFixedTime()
	s.attempted = gen
	if err := writeFile(s.out, PointerName, raw, mt); err != nil {
		return err
	}
	// [COMMIT]: the primary pointer names the new record.
	landed = true
	prevLive := s.prevLive
	s.ptr, s.sections, s.reseal, s.commits, s.fixedTime = p, digests, map[uint32]bool{}, s.commits+1, mt
	s.walkDone = map[string]time.Time{}
	s.apply(hist)
	if idx.captured {
		s.indexDigests = idx.digests
		if idx.sealed != nil {
			idx.sealed()
		}
	}
	mirror := &Digest{SHA256: sha256.Sum256(raw), Size: int64(len(raw))}
	if err := writeFile(s.out, MirrorName, raw, mt); err != nil {
		mirror = nil
		s.notice("the record pointer's mirror could not be written; the next save rewrites it")
	}
	if s.envHeal {
		if err := s.writeEnvelope(); err == nil {
			s.envHeal = false
		} else {
			s.notice("the record's envelope could not be rewritten; the next save tries again")
		}
	}
	live, err := s.liveFiles(v)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInventoryWrite, err)
	}
	for path, d := range prevLive {
		if _, still := live[path]; !still {
			s.retained[path] = d
		}
	}
	for path := range live {
		delete(s.retained, path)
	}
	s.prevLive = live
	if err := s.writeInventory(gen, Digest{SHA256: sha256.Sum256(raw), Size: int64(len(raw))}, mirror, live, mt); err != nil {
		return fmt.Errorf("%w: %v", ErrInventoryWrite, err)
	}
	return nil
}

// writeChunk seals plain as chunk pos of kind at gen under a fresh name and
// writes it durably, with the run's start as its mtime.
func (s *Store) writeChunk(kind vault.ContainerKind, gen uint64, pos uint32, plain []byte) (Entry, error) {
	sealed, err := s.opts.Key.SealChunk(kind, gen, pos, plain)
	if err != nil {
		return Entry{}, err
	}
	if len(sealed) > MaxChunkSize {
		return Entry{}, fmt.Errorf("%w: a record chunk is %d bytes, over the %d-byte bound", ErrPointer, len(sealed), MaxChunkSize)
	}
	name, err := NewName()
	if err != nil {
		return Entry{}, err
	}
	if err := writeFile(s.out, name.Path(), sealed, s.opts.RunStart); err != nil {
		return Entry{}, err
	}
	return Entry{I: pos, Name: name, Gen: gen, SHA256: sha256.Sum256(sealed), Size: uint32(len(sealed))}, nil
}

// writeInventory writes the shards whose content changed, under new generation
// names, then the top file — its commit point.
func (s *Store) writeInventory(gen uint64, ptr Digest, mirror *Digest, live map[string]Digest, mt time.Time) error {
	all := map[string]File{}
	for p, d := range live {
		all[p] = File{Path: p, Digest: d, State: Live}
	}
	for p, d := range s.retained {
		all[p] = File{Path: p, Digest: d, State: Retained}
	}
	fanout := MinFanout
	if s.inv != nil {
		fanout = s.inv.Fanout
	}
	for fanout < MaxFanout && len(all) > 512*(1<<(4*fanout)) {
		fanout++
	}
	byPrefix := map[string][]File{}
	for _, f := range all {
		prefix, err := PrefixOf(f.Path, fanout)
		if err != nil {
			return err
		}
		byPrefix[prefix] = append(byPrefix[prefix], f)
	}
	prefixes := make([]string, 0, len(byPrefix))
	for p := range byPrefix {
		prefixes = append(prefixes, p)
	}
	sort.Strings(prefixes)
	oldRef := map[string]ShardRef{}
	if s.inv != nil && s.inv.Fanout == fanout && s.shards != nil {
		for _, r := range s.inv.Shards {
			oldRef[r.Prefix] = r
		}
	}
	top := Top{Gen: gen, Pointer: ptr, Mirror: mirror, Fanout: fanout}
	shards := map[string][]byte{}
	for _, prefix := range prefixes {
		files := byPrefix[prefix]
		sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
		b, err := Shard{Prefix: prefix, Files: files}.Marshal()
		if err != nil {
			return err
		}
		shards[prefix] = b
		if r, ok := oldRef[prefix]; ok && bytes.Equal(s.shards[prefix], b) {
			top.Shards = append(top.Shards, r)
			continue
		}
		name := ShardName(prefix, gen)
		if err := writeFile(s.out, name, b, s.opts.RunStart); err != nil {
			return err
		}
		top.Shards = append(top.Shards, ShardRef{Prefix: prefix, Name: name, SHA256: sha256.Sum256(b), Entries: len(files)})
	}
	b, err := top.Marshal()
	if err != nil {
		return err
	}
	if err := writeFile(s.out, InventoryName, b, mt); err != nil {
		return err
	}
	live2, retained2 := map[string]bool{}, map[string]bool{}
	for p := range live {
		live2[p] = true
	}
	for p := range s.retained {
		retained2[p] = true
	}
	s.inv, s.shards, s.invLive, s.invRetained = &top, shards, live2, retained2
	return nil
}
