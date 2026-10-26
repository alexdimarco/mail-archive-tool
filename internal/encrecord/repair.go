package encrecord

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/util"
)

// ErrNotDamaged refuses a repair of a record that is not damaged: a usable one
// needs none, and the other cases have their own remedy (§4.3).
var ErrNotDamaged = errors.New("this computer's record of encrypted archives is not damaged")

// ErrRepairRaced refuses a repair whose record changed between its scan and its
// lock.
var ErrRepairRaced = errors.New("this computer's record of encrypted archives changed while it was being repaired; run the repair again")

// Seams for fault injection: afterRepairScan runs between the scan and the lock,
// beforeRepairWrite once the damaged copies are kept aside and before the rebuilt
// record is written.
var (
	afterRepairScan   = func() {}
	beforeRepairWrite = func() {}
)

// RepairReport says what a repair did.
type RepairReport struct {
	// MovedAside are the damaged copies kept under new names, never overwritten.
	MovedAside []string
	// Paths and Tombstones count what was rebuilt; every rebuilt path is unproven.
	Paths, Tombstones int
	// NotRebuilt says, per copy, what could not be read back.
	NotRebuilt []string
}

// Repair rebuilds a damaged record (§4.3). It scans first without the lock, then
// under it re-checks that the record is still damaged and unchanged since the
// scan, refusing if not; moves both copies aside; and writes a record of every path
// and tombstone that still parses from either, each path marked unproven — it only
// refuses keyless writes until a write or remember open proves it.
func Repair() (RepairReport, error) {
	var rep RepairReport
	dir, p, m, err := paths()
	if err != nil {
		return rep, &UnusableError{Case: CaseUnreadable, Path: "MailArchive's settings folder", Err: err}
	}
	if err := checkRepairable(p, m); err != nil {
		return rep, err
	}
	scanned := [2][]byte{readRaw(p), readRaw(m)}
	afterRepairScan()
	if err := RefuseSettingsWrite(p, "this computer's record of encrypted archives"); err != nil {
		return rep, err
	}
	lock, err := lockfile.AcquireWait(filepath.Join(dir, LockName), "repair-record", lockWait)
	if err != nil {
		if errors.Is(err, lockfile.ErrHeld) {
			return rep, fmt.Errorf("MailArchive is busy updating its record of encrypted archives: %w", err)
		}
		return rep, fmt.Errorf("MailArchive cannot lock its record of encrypted archives in %s: %w", dir, err)
	}
	defer lock.Release()
	if err := checkRepairable(p, m); err != nil {
		return rep, err
	}
	if !bytes.Equal(readRaw(p), scanned[0]) || !bytes.Equal(readRaw(m), scanned[1]) {
		return rep, ErrRepairRaced
	}

	// Copies are salvaged oldest first, so a later copy's tombstone decides an id's
	// (the P3-1a pass, CRIT-08).
	type found struct {
		path string
		raw  []byte
		seq  uint64
	}
	var copies []found
	var seq uint64
	for _, path := range []string{p, m} {
		raw := readRaw(path)
		if raw == nil {
			continue
		}
		_, s, _ := readHeader(path)
		copies = append(copies, found{path, raw, s})
		seq = max(seq, s)
	}
	sort.SliceStable(copies, func(i, j int) bool { return copies[i].seq < copies[j].seq })
	rebuilt := &Record{Archives: map[string]*Entry{}}
	for _, c := range copies {
		salvage(rebuilt, c.raw, filepath.Base(c.path), &rep)
	}
	for _, e := range rebuilt.Archives {
		// A folder recorded again is not forgotten.
		if len(e.Paths) > 0 {
			e.Forgotten = false
		}
		rep.Paths += len(e.Paths)
		if e.Forgotten {
			rep.Tombstones++
		}
	}
	data, err := encode(rebuilt, seq+1)
	if err != nil {
		return rep, err
	}
	if len(data) > MaxBytes {
		return rep, ErrTooLarge
	}
	if err := lock.StillHeld(); err != nil {
		return rep, err
	}
	// Each damaged copy is COPIED aside, then replaced in one rename: the record
	// never passes through having no copy, which would read as an empty, usable
	// record that no longer needs repair (the P3-1a pass, CON-02).
	for _, c := range copies {
		aside, err := copyAside(c.path, c.raw)
		if err != nil {
			return rep, fmt.Errorf("keep %s aside: %w", c.path, err)
		}
		rep.MovedAside = append(rep.MovedAside, aside)
	}
	beforeRepairWrite()
	if err := util.WriteFileAtomic0600(p, data); err != nil {
		return rep, err
	}
	if err := util.WriteFileAtomic0600(m, data); err != nil {
		return rep, fmt.Errorf("the record of encrypted archives was rebuilt, but its mirror was not written: %w", err)
	}
	return rep, nil
}

// copyAside writes raw to a new <path>.damaged-<date>-<n>, never over an existing
// file, owner-only and synced, and returns its name.
func copyAside(path string, raw []byte) (string, error) {
	date := time.Now().UTC().Format("20060102")
	for n := 1; n < 1000; n++ {
		dst := fmt.Sprintf("%s.damaged-%s-%d", path, date, n)
		f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, werr := f.Write(raw)
		if werr == nil {
			werr = f.Sync()
		}
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			os.Remove(dst)
			return "", werr
		}
		util.SyncDir(filepath.Dir(dst))
		return dst, nil
	}
	return "", errors.New("too many damaged copies already set aside")
}

// checkRepairable refuses a record that is not damaged.
func checkRepairable(p, m string) error {
	_, err := judge(readOne(p), readOne(m))
	if err == nil {
		return fmt.Errorf("%w: it can be used as it is", ErrNotDamaged)
	}
	var ue *UnusableError
	if errors.As(err, &ue) && ue.Case == CaseDamaged {
		return nil
	}
	return fmt.Errorf("%w: %w", ErrNotDamaged, err)
}

// readRaw is a copy's bytes, nil when it is absent or cannot be read whole.
func readRaw(path string) []byte {
	b, err := util.ReadRegularFile(path, MaxBytes)
	if err != nil {
		return nil
	}
	return b
}

// salvage adds to rec every archive entry the body of raw still decodes to, its
// paths unproven, whatever its trailer says.
func salvage(rec *Record, raw []byte, name string, rep *RepairReport) {
	_, body, ok := bytes.Cut(raw, []byte("\n"))
	if !ok {
		rep.NotRebuilt = append(rep.NotRebuilt, name+": nothing after its first line")
		return
	}
	var got struct {
		Archives map[string]json.RawMessage `json:"archives"`
	}
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&got); err != nil {
		rep.NotRebuilt = append(rep.NotRebuilt, name+": its list of archives does not read ("+err.Error()+")")
		return
	}
	ids := make([]string, 0, len(got.Archives))
	for id := range got.Archives {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		var e Entry
		if !validID(id) || json.Unmarshal(got.Archives[id], &e) != nil {
			rep.NotRebuilt = append(rep.NotRebuilt, fmt.Sprintf("%s: an entry %q that is not an archive's", name, id))
			continue
		}
		have := rec.Archives[id]
		if have == nil {
			have = &Entry{}
			rec.Archives[id] = have
		}
		have.Forgotten = e.Forgotten // the later copy decides
		for _, pe := range e.Paths {
			if pe.Spelling == "" || !filepath.IsAbs(pe.Spelling) {
				rep.NotRebuilt = append(rep.NotRebuilt, fmt.Sprintf("%s: a path of %s that is not a folder (%q)", name, id, pe.Spelling))
				continue
			}
			pe.Unproven = true
			dup := false
			for _, old := range have.Paths {
				dup = dup || old.Spelling == pe.Spelling
			}
			if !dup {
				have.Paths = append(have.Paths, pe)
			}
		}
		sort.Slice(have.Paths, func(i, j int) bool { return have.Paths[i].Spelling < have.Paths[j].Spelling })
	}
}
