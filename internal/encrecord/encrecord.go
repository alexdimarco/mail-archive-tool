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
	// CasePermissions: a copy is readable by other users (or not owned right).
	CasePermissions Case = iota + 1
	// CaseNewer: a copy was written by a newer MailArchive.
	CaseNewer
	// CaseDamaged: neither copy reads whole.
	CaseDamaged
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
		return fmt.Sprintf("this computer's record of encrypted archives (%s) can be read by other users — run `%s`", e.Path, fix)
	case CaseNewer:
		return fmt.Sprintf("this computer's record of encrypted archives (%s) was written by a newer MailArchive — upgrade MailArchive", e.Path)
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

// readHeader reads the first line of path without any size limit applying, so a
// newer record is recognised as newer whatever its size (§4.2).
func readHeader(path string) (format int, seq uint64, ok bool) {
	f, err := util.OpenRegularFile(path)
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()
	line, err := bufio.NewReaderSize(io.LimitReader(f, maxHeader), maxHeader).ReadString('\n')
	if err != nil {
		return 0, 0, false
	}
	return parseHeader(strings.TrimSuffix(line, "\n"))
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
		c.state, c.err = copyDamaged, err
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
		if errors.Is(err, util.ErrReadableByOthers) {
			c.state = copyPermissions
		} else {
			c.state = copyDamaged
		}
		c.err = err
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
		return nil, err
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
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	lock, err := lockfile.AcquireWait(filepath.Join(dir, LockName), verb, lockWait)
	if err != nil {
		return "", fmt.Errorf("MailArchive is busy updating its record of encrypted archives: %w", err)
	}
	defer lock.Release()
	pc, mc := readOne(p), readOne(m)
	rec, err := judge(pc, mc)
	if err != nil {
		return "", err
	}
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
	if err := util.WriteFileAtomic0600(p, data); err != nil {
		return "", err
	}
	if err := util.WriteFileAtomic0600(m, data); err != nil {
		return notice, fmt.Errorf("the record of encrypted archives was saved, but its mirror was not: %w", err)
	}
	rec.seq = seq
	return notice, nil
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

// Spelling is how a path is stored and compared: absolute and cleaned, folded to
// one letter case only where state.PathSourceID folds.
func Spelling(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	abs = filepath.Clean(abs)
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		abs = strings.ToLower(abs)
	}
	return abs
}

// recordedForm is the spelling a folder is recorded under: its link target when the
// folder itself is a link (§4.2), so a recorded path is never legitimately a link.
func recordedForm(path string) string {
	if link, _ := util.IsLinkLike(path); link {
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			return Spelling(resolved)
		}
	}
	return Spelling(path)
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
// entry, and the folder on disk that matched (path itself or a folder above it).
type Match struct {
	ID     string
	Path   PathEntry
	Folder string
}

// Lookup finds the recorded folder that path is or lies inside, comparing both the
// spelling as given and the resolved source id, for path and every folder above
// it, against both stored forms (§4.5). The nearest match wins.
func (r *Record) Lookup(path string) (Match, bool) {
	if r == nil || len(r.Archives) == 0 {
		return Match{}, false
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = filepath.Clean(path)
	}
	for d := abs; ; d = filepath.Dir(d) {
		sp, src := Spelling(d), state.PathSourceID(d)
		ids := make([]string, 0, len(r.Archives))
		for id := range r.Archives {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			for _, pe := range r.Archives[id].Paths {
				if pe.Spelling == sp || pe.Source == src || "path:"+pe.Spelling == src || pe.Source == "path:"+sp {
					return Match{ID: id, Path: pe, Folder: d}, true
				}
			}
		}
		if filepath.Dir(d) == d {
			return Match{}, false
		}
	}
}
