// Package encrecord is this computer's record of the encrypted archives it made or
// wrote (design-encrypted-archives-phase3 §4, A15, A27): which archive ids it knows,
// and the folders it used for each. A folder this computer recorded stays
// encrypted to every keyless writer whatever the folder now holds, so deleting an
// archive's key files and sealed data cannot make it read as plaintext here.
//
// The record lives outside any archive — MailArchive's settings folder, or the
// non-roaming per-computer folder on Windows (instance.Dir) — as
// encrypted-archives.rec and its mirror encrypted-archives.rec.bak. Neither is a
// *.json name: uninstall deletes those, and a scheduled job's file is one.
package encrecord

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"

	"mail-archive-tool/internal/instance"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/util"
)

const (
	// FileName and MirrorName are the record and its mirror in Dir.
	FileName   = "encrypted-archives.rec"
	MirrorName = "encrypted-archives.rec.bak"
	// LockName guards every change (§4.3).
	LockName = "encrypted-archives.lock"

	// Format is the record format this build writes and reads. A field whose
	// meaning can refuse or protect raises it (§4.2), so an older build reads a
	// newer record as "newer — upgrade" and fails closed.
	Format = 1

	// MaxBytes caps the record (§4.3).
	MaxBytes = 1 << 20

	// lockWait is how long a change waits for another writer (§4.3).
	lockWait = 5 * time.Second

	magic        = "MAILARCHIVE-RECORD"
	trailerLabel = "sha256="
	maxHeader    = 128
)

// dirFn finds the folder the record lives in; SetDirForTest replaces it.
var dirFn = defaultDir

// defaultDir is the per-account folder the instance files already live in —
// MailArchive's settings folder, or the non-roaming per-computer folder on Windows
// (§4.1) — so the record adds no new per-user state location (R33, MA-621).
func defaultDir() (string, error) {
	d, err := instance.Dir()
	if err != nil {
		return "", fmt.Errorf("cannot find the folder for this computer's record of encrypted archives: %w", err)
	}
	return d, nil
}

// Dir is the folder the record lives in.
func Dir() (string, error) { return dirFn() }

// RefuseSettingsWrite refuses the record inside an encrypted archive (phase3 §6
// E5). encmark installs its check here (it imports this package); until then it
// refuses nothing.
var RefuseSettingsWrite = func(path, kind string) error { return nil }

// SetDirForTest points the record at dir until the returned func is called, so a
// test never reads or writes the developer's real record. Test-only.
func SetDirForTest(dir string) (restore func()) {
	old := dirFn
	dirFn = func() (string, error) { return dir, nil }
	return func() { dirFn = old }
}

// PathEntry is one folder this computer used for an archive.
type PathEntry struct {
	// Spelling is the cleaned absolute path, folded to one letter case where
	// state.PathSourceID folds (Windows, macOS).
	Spelling string `json:"spelling"`
	// Source is state.PathSourceID of the folder when it was recorded.
	Source   string    `json:"source"`
	Recorded time.Time `json:"recorded"`
	// Unproven marks a path rebuilt by repair: it refuses keyless writes but is
	// never trusted as the archive's id (§4.3).
	Unproven bool `json:"unproven,omitempty"`
}

// Entry is what this computer knows of one archive id.
type Entry struct {
	Paths []PathEntry `json:"paths"`
	// Forgotten is the tombstone of a whole-id forget (§4.2): it only stops
	// lost-record warnings and clears when the id is recorded again.
	Forgotten bool `json:"forgotten,omitempty"`
}

// Record is the decoded record. Archives is keyed by the archive id's hex.
type Record struct {
	Archives map[string]*Entry `json:"archives"`

	seq uint64
}

// Seq is the record's sequence number (0 for an empty, never-written record).
func (r *Record) Seq() uint64 { return r.seq }

// Case is why the record cannot be used (§4.3).
type Case int

const (
	// CasePermissions: a copy is readable by other users, or not owned by this
	// account.
	CasePermissions Case = iota + 1
	// CaseNewer: a copy was written by a newer MailArchive.
	CaseNewer
	// CaseDamaged: neither copy reads whole.
	CaseDamaged
	// CaseUnreadable: the record's folder cannot be found or searched (no home
	// folder, a folder MailArchive may not read), so whether a record exists is
	// unknown (the P3-0 adversarial pass, REG-03).
	CaseUnreadable
)

// UnusableError is the refusal when the record cannot be used. Its message names
// the file and the remedy for its case; the record is never rewritten.
type UnusableError struct {
	Case Case
	Path string
	Err  error
}

func (e *UnusableError) Error() string {
	switch e.Case {
	case CasePermissions:
		fix := "chmod 600 " + e.Path
		if runtime.GOOS == "windows" {
			fix = `icacls "` + e.Path + `" /inheritance:r /grant:r "%USERNAME%:F"`
		}
		return fmt.Sprintf("this computer's record of encrypted archives (%s) can be read by other users or is not yours — run `%s`", e.Path, fix)
	case CaseNewer:
		return fmt.Sprintf("this computer's record of encrypted archives (%s) was written by a newer MailArchive — upgrade MailArchive", e.Path)
	case CaseUnreadable:
		return fmt.Sprintf("this computer's record of encrypted archives cannot be looked for (%s: %v) — MailArchive needs its own settings folder, readable by you", e.Path, e.Err)
	default:
		return fmt.Sprintf("this computer's record of encrypted archives (%s) is damaged and needs repair", e.Path)
	}
}

func (e *UnusableError) Unwrap() error { return e.Err }

// ErrTooLarge refuses a change that would take the record past MaxBytes.
var ErrTooLarge = errors.New("this computer's record of encrypted archives would be larger than its limit")

// copyState is what one of the two files holds.
type copyState int

const (
	copyAbsent copyState = iota
	copyValid
	copyNewer
	copyPermissions
	copyDamaged
	copyUnreadable
)

type copyRead struct {
	path  string
	state copyState
	seq   uint64 // readable from the header even when the trailer fails
	rec   *Record
	err   error
}

// paths is the record and mirror in the record's folder.
func paths() (dir, primary, mirror string, err error) {
	dir, err = Dir()
	if err != nil {
		return "", "", "", err
	}
	return dir, filepath.Join(dir, FileName), filepath.Join(dir, MirrorName), nil
}

// readHeader reads the start of path's first line without any size limit
// applying. A newer format is recognised from the magic word and the format number
// alone, whatever follows them and however long the line is, so a future header
// with more fields still reads as newer, never as damaged (§4.2; the P3-0
// adversarial pass, CRIT-01). seq is read only from a header of this format's
// shape.
func readHeader(path string) (format int, seq uint64, ok bool) {
	f, err := util.OpenRegularFile(path)
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()
	head, _ := bufio.NewReaderSize(io.LimitReader(f, maxHeader), maxHeader).Peek(maxHeader)
	line, _, _ := strings.Cut(string(head), "\n")
	fields := strings.Fields(line)
	if len(fields) < 2 || fields[0] != magic {
		return 0, 0, false
	}
	format, err = strconv.Atoi(fields[1])
	if err != nil || format < 1 {
		return 0, 0, false
	}
	if format > Format {
		return format, 0, true
	}
	return parseHeader(line)
}

// parseHeader reads "MAILARCHIVE-RECORD <format> seq=<n>".
func parseHeader(line string) (format int, seq uint64, ok bool) {
	fields := strings.Fields(line)
	if len(fields) != 3 || fields[0] != magic || !strings.HasPrefix(fields[2], "seq=") {
		return 0, 0, false
	}
	format, err := strconv.Atoi(fields[1])
	if err != nil || format < 1 {
		return 0, 0, false
	}
	seq, err = strconv.ParseUint(strings.TrimPrefix(fields[2], "seq="), 10, 64)
	if err != nil {
		return 0, 0, false
	}
	return format, seq, true
}

// readOne reads one copy and judges it.
func readOne(path string) copyRead {
	c := copyRead{path: path}
	fi, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return c
	}
	if err != nil {
		// The folder cannot be searched (permission, a file where a folder belongs):
		// whether a record exists is unknown, which is not damage (REG-03).
		c.state, c.err = copyUnreadable, err
		return c
	}
	if !fi.Mode().IsRegular() {
		c.state, c.err = copyDamaged, fmt.Errorf("%s is %s, not a regular file", path, util.ModeKind(fi.Mode()))
		return c
	}
	format, seq, hok := readHeader(path)
	if hok {
		c.seq = seq
		if format > Format {
			c.state = copyNewer
			return c
		}
	}
	data, err := util.ReadSecureFile(path, "record of encrypted archives", MaxBytes)
	if err != nil {
		switch {
		case errors.Is(err, util.ErrReadableByOthers):
			c.state = copyPermissions
		case errors.Is(err, fs.ErrPermission):
			c.state = copyUnreadable
		default:
			c.state = copyDamaged
		}
		c.err = err
		return c
	}
	// The record is this account's: one written by another account is not trusted,
	// as the instance files beside it are not (the P3-0 adversarial pass, SEC-03).
	if err := instance.CheckOwner(path, instance.OwnerReader); err != nil {
		c.state, c.err = copyPermissions, err
		return c
	}
	rec, err := decode(data)
	if err != nil {
		c.state, c.err = copyDamaged, err
		return c
	}
	c.state, c.rec = copyValid, rec
	return c
}

// decode checks the header, the trailer and the body.
func decode(data []byte) (*Record, error) {
	i := bytes.LastIndex(bytes.TrimSuffix(data, []byte("\n")), []byte("\n"))
	if i < 0 {
		return nil, errors.New("no trailer")
	}
	signed, trailer := data[:i+1], strings.TrimSpace(string(data[i+1:]))
	if !strings.HasPrefix(trailer, trailerLabel) {
		return nil, errors.New("no trailer")
	}
	sum := sha256.Sum256(signed)
	if trailer[len(trailerLabel):] != hex.EncodeToString(sum[:]) {
		return nil, errors.New("its trailer does not match its content")
	}
	head, body, ok := bytes.Cut(signed, []byte("\n"))
	if !ok {
		return nil, errors.New("no header")
	}
	format, seq, hok := parseHeader(string(head))
	if !hok || format != Format {
		return nil, errors.New("its header is not one this MailArchive writes")
	}
	var rec Record
	dec := json.NewDecoder(bytes.NewReader(body))
	if err := dec.Decode(&rec); err != nil {
		return nil, fmt.Errorf("its body does not decode: %w", err)
	}
	if rec.Archives == nil {
		rec.Archives = map[string]*Entry{}
	}
	for id, e := range rec.Archives {
		if !validID(id) || e == nil {
			return nil, errors.New("it holds an entry that is not an archive's")
		}
	}
	rec.seq = seq
	return &rec, nil
}

// validID is an archive id's hex: 32 lowercase hex characters (vault.ID).
func validID(s string) bool {
	if len(s) != 32 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

// encode writes the header, the body and the trailer.
func encode(r *Record, seq uint64) ([]byte, error) {
	body, err := json.Marshal(struct {
		Archives map[string]*Entry `json:"archives"`
	}{r.Archives})
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "%s %d seq=%d\n", magic, Format, seq)
	b.Write(body)
	b.WriteByte('\n')
	sum := sha256.Sum256(b.Bytes())
	b.WriteString(trailerLabel + hex.EncodeToString(sum[:]) + "\n")
	return b.Bytes(), nil
}

// judge combines the two copies into the record, or the reason it cannot be used.
func judge(primary, mirror copyRead) (*Record, error) {
	for _, c := range []copyRead{primary, mirror} {
		if c.state == copyNewer {
			return nil, &UnusableError{Case: CaseNewer, Path: c.path}
		}
	}
	for _, c := range []copyRead{primary, mirror} {
		if c.state == copyPermissions {
			return nil, &UnusableError{Case: CasePermissions, Path: c.path, Err: c.err}
		}
	}
	// A copy that cannot be looked at may hold the newer change: never fall back
	// past it.
	for _, c := range []copyRead{primary, mirror} {
		if c.state == copyUnreadable {
			return nil, &UnusableError{Case: CaseUnreadable, Path: c.path, Err: c.err}
		}
	}
	var best *Record
	for _, c := range []copyRead{primary, mirror} {
		if c.state == copyValid && (best == nil || c.rec.seq > best.seq) {
			best = c.rec
		}
	}
	if best != nil {
		return best, nil
	}
	if primary.state == copyAbsent && mirror.state == copyAbsent {
		return &Record{Archives: map[string]*Entry{}}, nil
	}
	damaged := primary
	if damaged.state != copyDamaged {
		damaged = mirror
	}
	return nil, &UnusableError{Case: CaseDamaged, Path: damaged.path, Err: damaged.err}
}

// Load reads the record. A record never written is empty, with no error. One the
// computer cannot use returns *UnusableError and no record.
func Load() (*Record, error) {
	_, p, m, err := paths()
	if err != nil {
		return nil, &UnusableError{Case: CaseUnreadable, Path: "MailArchive's settings folder", Err: err}
	}
	return judge(readOne(p), readOne(m))
}

// Update changes the record under its lock: it re-reads both copies, refuses when
// the record cannot be used, applies fn, and writes the primary (the commit point)
// then the mirror. A copy that failed its trailer is moved aside before anything is
// written over it; notice says when that copy's readable sequence was the higher,
// so a recent change may have been lost. fn returning an error writes nothing.
func Update(verb string, fn func(*Record) error) (notice string, err error) {
	dir, p, m, err := paths()
	if err != nil {
		return "", &UnusableError{Case: CaseUnreadable, Path: "MailArchive's settings folder", Err: err}
	}
	// The record never lands inside an encrypted archive (phase3 §6 E5).
	if err := RefuseSettingsWrite(p, "this computer's record of encrypted archives"); err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	lock, err := lockfile.AcquireWait(filepath.Join(dir, LockName), verb, lockWait)
	if err != nil {
		if errors.Is(err, lockfile.ErrHeld) {
			return "", fmt.Errorf("MailArchive is busy updating its record of encrypted archives: %w", err)
		}
		// Not a holder: a folder MailArchive may not write, or a planted lock file
		// (the P3-0 adversarial pass, CON-05).
		return "", fmt.Errorf("MailArchive cannot lock its record of encrypted archives in %s: %w", dir, err)
	}
	defer lock.Release()
	pc, mc := readOne(p), readOne(m)
	rec, err := judge(pc, mc)
	if err != nil {
		return "", err
	}
	if err := fn(rec); err != nil {
		return "", err
	}
	seq := rec.seq + 1
	if pc.seq >= seq {
		seq = pc.seq + 1
	}
	if mc.seq >= seq {
		seq = mc.seq + 1
	}
	data, err := encode(rec, seq)
	if err != nil {
		return "", err
	}
	if len(data) > MaxBytes {
		return "", ErrTooLarge
	}
	if err := lock.StillHeld(); err != nil {
		return "", err
	}
	// Only now, with the write certain to be attempted, is a damaged copy moved
	// aside: a refused change leaves the folder as it was, notice and all (CON-03).
	for _, c := range []copyRead{pc, mc} {
		if c.state != copyDamaged {
			continue
		}
		if c.seq > rec.seq {
			notice = "a recent change to this computer's record of encrypted archives may have been lost"
		}
		if err := moveAside(c.path); err != nil {
			return "", fmt.Errorf("move the damaged copy %s aside: %w", c.path, err)
		}
	}
	if err := util.WriteFileAtomic0600(p, data); err != nil {
		return "", err
	}
	if err := util.WriteFileAtomic0600(m, data); err != nil {
		return notice, fmt.Errorf("the record of encrypted archives was saved, but its mirror was not: %w", err)
	}
	rec.seq = seq
	return notice, nil
}

// LostName lists the archive ids a keyed open found missing from the record while
// this computer remembers their keys (a lost record, §4.3): one id per line,
// owner-only. Status warns of each until the id is recorded again.
const LostName = "encrypted-archives.lost"

// maxLostBytes bounds the lost list (an id and a newline each).
const maxLostBytes = 64 << 10

// MarkLost adds id to the lost list.
func MarkLost(id string) error { return changeLost(id, true) }

// ClearLost removes id from the lost list, if it is there.
func ClearLost(id string) error { return changeLost(id, false) }

// Lost reads the lost list. None written is an empty list.
func Lost() ([]string, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	return readLost(filepath.Join(dir, LostName))
}

func readLost(path string) ([]string, error) {
	data, err := util.ReadSecureFile(path, "list of encrypted archives missing from the record", maxLostBytes)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, line := range strings.Split(string(data), "\n") {
		if line = strings.TrimSpace(line); line == "" {
			continue
		}
		if !validID(line) {
			return nil, fmt.Errorf("%s holds %q, which is not an archive id", path, line)
		}
		ids = append(ids, line)
	}
	return ids, nil
}

// changeLost adds or removes id under the record's lock.
func changeLost(id string, add bool) error {
	if !validID(id) {
		return fmt.Errorf("%q is not an archive id", id)
	}
	dir, err := Dir()
	if err != nil {
		return err
	}
	path := filepath.Join(dir, LostName)
	if _, err := os.Lstat(path); !add && errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err := RefuseSettingsWrite(path, "list of encrypted archives missing from the record"); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	lock, err := lockfile.AcquireWait(filepath.Join(dir, LockName), "lost", lockWait)
	if err != nil {
		return fmt.Errorf("MailArchive cannot lock its record of encrypted archives in %s: %w", dir, err)
	}
	defer lock.Release()
	ids, err := readLost(path)
	if err != nil {
		return err
	}
	// A mark decided on an older look at the record is not made once the record
	// lists the id (the P3-1a pass, CON-06).
	if add {
		_, p, m, perr := paths()
		if perr == nil {
			if rec, jerr := judge(readOne(p), readOne(m)); jerr == nil && rec.Archives[id] != nil {
				return nil
			}
		}
	}
	kept := ids[:0:0]
	found := false
	for _, have := range ids {
		if have == id {
			found = true
			if !add {
				continue
			}
		}
		kept = append(kept, have)
	}
	switch {
	case add && found, !add && !found:
		return nil
	case add:
		kept = append(kept, id)
	}
	if err := lock.StillHeld(); err != nil {
		return err
	}
	if len(kept) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	sort.Strings(kept)
	return util.WriteFileAtomic0600(path, []byte(strings.Join(kept, "\n")+"\n"))
}

// moveAside renames a damaged copy to <name>.damaged-<date>-<n>, never over an
// existing file.
func moveAside(path string) error {
	date := time.Now().UTC().Format("20060102")
	for n := 1; n < 1000; n++ {
		dst := fmt.Sprintf("%s.damaged-%s-%d", path, date, n)
		if _, err := os.Lstat(dst); errors.Is(err, fs.ErrNotExist) {
			return os.Rename(path, dst)
		}
	}
	return errors.New("too many damaged copies already set aside")
}

// Spelling is how a path is stored and compared: absolute and cleaned, and — where
// the file system treats more than one spelling as one folder (Windows, macOS) —
// Unicode-normalised (NFC) and case-folded in full, so a composed and a decomposed
// accent, or σ and ς, are one spelling (the P3-0 adversarial pass, OUT-02). Folding
// may join two distinct folders (ß and ss): that only refuses more, never less.
func Spelling(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	return fold(filepath.Clean(abs))
}

// fold is Spelling's comparison form of an absolute path.
func fold(s string) string {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return cases.Fold().String(norm.NFC.String(s))
	}
	return s
}

// sourceKey is a state.PathSourceID in Spelling's comparison form.
func sourceKey(src string) string { return fold(strings.TrimPrefix(src, "path:")) }

// recordedForm is the spelling a folder is recorded under: the folder with every
// link on the way to it resolved — its own, and any parent's (§4.2, amended in
// P3-1a: OUT-04, OUT-05, SEC-04) — so no recorded path is legitimately a link or
// reached through one, and a recorded path that later resolves elsewhere has had a
// link put on its way (case L, encmark).
func recordedForm(path string) string { return Spelling(util.ResolveExisting(path)) }

// ResolvesElsewhere reports whether a recorded spelling now resolves to another
// folder: a link was put where the folder, or a folder above it, was.
func ResolvesElsewhere(spelling string) bool { return recordedForm(spelling) != spelling }

// Names reports whether path, as a person typed it, names the recorded spelling:
// in its comparison form, or once resolved as it would be recorded.
func Names(path, spelling string) bool {
	return Spelling(path) == spelling || recordedForm(path) == spelling
}

// Add records path for id (an archive id's hex), proven or not, and clears the
// id's tombstone. Recording a path already there updates it.
func (r *Record) Add(id, path string, unproven bool, now time.Time) error {
	if !validID(id) {
		return fmt.Errorf("%q is not an archive id", id)
	}
	pe := PathEntry{Spelling: recordedForm(path), Source: state.PathSourceID(path), Recorded: now.UTC(), Unproven: unproven}
	e := r.Archives[id]
	if e == nil {
		e = &Entry{}
		r.Archives[id] = e
	}
	e.Forgotten = false
	for i, old := range e.Paths {
		if old.Spelling == pe.Spelling {
			e.Paths[i] = pe
			return nil
		}
	}
	e.Paths = append(e.Paths, pe)
	sort.Slice(e.Paths, func(i, j int) bool { return e.Paths[i].Spelling < e.Paths[j].Spelling })
	return nil
}

// ForgetPath removes exactly the stored spelling from id's paths. The id stays,
// with no paths if this was its last. It reports whether anything was removed.
func (r *Record) ForgetPath(id, spelling string) bool {
	e := r.Archives[id]
	if e == nil {
		return false
	}
	for i, pe := range e.Paths {
		if pe.Spelling == spelling {
			e.Paths = append(e.Paths[:i], e.Paths[i+1:]...)
			return true
		}
	}
	return false
}

// ForgetID drops every path of id and sets its tombstone (§4.6).
func (r *Record) ForgetID(id string) {
	if e := r.Archives[id]; e != nil {
		e.Paths = nil
		e.Forgotten = true
	}
}

// Match is the recorded path that path is, or lies inside: the archive id, the
// entry, and the folder that matched — path itself or a folder above it, as given
// or with its links resolved. BySpelling says the folder was reached by the
// recorded spelling itself; otherwise by another way to the same folder (a link,
// its resolved source, the same folder on disk), and the recorded spelling is the
// folder to judge (the P3-0 adversarial pass, FID-01).
type Match struct {
	ID         string
	Path       PathEntry
	Folder     string
	BySpelling bool
}

type recordedPath struct {
	id string
	pe PathEntry
}

// Lookup finds the recorded folder that path is or lies inside (§4.5). It walks
// path and every folder above it, as given and with its links resolved (so a link
// into a recorded archive is seen through, OUT-01), comparing each folder's
// spelling and resolved source against both stored forms. When no name matches it
// compares the folders themselves: a recorded folder that exists, reached by a
// spelling no name comparison can know (a firmlink, a junction or subst drive, a
// bind mount), is the same folder (os.SameFile; SEC-01). The nearest match wins.
func (r *Record) Lookup(path string) (Match, bool) {
	if r == nil || len(r.Archives) == 0 {
		return Match{}, false
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = filepath.Clean(path)
	}
	var all []recordedPath
	ids := make([]string, 0, len(r.Archives))
	for id := range r.Archives {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		for _, pe := range r.Archives[id].Paths {
			all = append(all, recordedPath{id, pe})
		}
	}
	if len(all) == 0 {
		return Match{}, false
	}
	chains := []string{abs}
	if res := util.ResolveExisting(abs); res != abs {
		chains = append(chains, res)
	}
	for ci, start := range chains {
		for d := start; ; d = filepath.Dir(d) {
			sp, src := Spelling(d), sourceKey(state.PathSourceID(d))
			// A folder's own recorded spelling — as given, or with only its parents
			// resolved — wins over any other entry the folder reaches through a link
			// at its own name: a recorded folder replaced by a link to another
			// recorded archive is itself, and reads case L (the P3-1a pass, OUT-01).
			if ci == 0 {
				leaf := Spelling(filepath.Join(util.ResolveExisting(filepath.Dir(d)), filepath.Base(d)))
				for _, rp := range all {
					if rp.pe.Spelling == sp || rp.pe.Spelling == leaf {
						return Match{ID: rp.id, Path: rp.pe, Folder: d, BySpelling: true}, true
					}
				}
			}
			for _, rp := range all {
				stored, storedSrc := rp.pe.Spelling, sourceKey(rp.pe.Source)
				if stored == sp || stored == src || storedSrc == sp || storedSrc == src {
					return Match{ID: rp.id, Path: rp.pe, Folder: d, BySpelling: ci == 0 && stored == sp}, true
				}
			}
			if filepath.Dir(d) == d {
				break
			}
		}
	}
	type statted struct {
		rp recordedPath
		fi os.FileInfo
	}
	var present []statted
	for _, rp := range all {
		if fi, err := os.Stat(rp.pe.Spelling); err == nil {
			present = append(present, statted{rp, fi})
		}
	}
	if len(present) == 0 {
		return Match{}, false
	}
	for d := abs; ; d = filepath.Dir(d) {
		if dfi, err := os.Stat(d); err == nil {
			for _, s := range present {
				if os.SameFile(s.fi, dfi) {
					return Match{ID: s.rp.id, Path: s.rp.pe, Folder: d}, true
				}
			}
		}
		if filepath.Dir(d) == d {
			return Match{}, false
		}
	}
}
