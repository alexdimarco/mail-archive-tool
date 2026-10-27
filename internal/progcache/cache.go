// Package progcache is this computer's cache of MailArchive's release copies, the
// ones an encrypted archive keeps in its program folder (design-encrypted-archives-
// phase3-program §5, R17–R19; PG2): <user cache dir>/mailarchive/program-cache/,
// outside the scratch folder, local, with a closed shape — a format file, the
// expects file, one lock, and a folder per version holding the release's four
// assets, its SHA256SUMS, the verified record, partial downloads and the attempt
// record. Nothing in it is ever followed through a link, and every file is checked
// against the release's checksums before anything is copied out of it.
package progcache

import (
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

	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/util"
)

// Format is the cache's layout version; a cache of a newer format is left alone.
const Format = 1

// The names in the cache.
const (
	SumsName      = "SHA256SUMS"
	verifiedName  = "verified"
	attemptName   = "attempt"
	expectsName   = "expects"
	formatName    = "format"
	lockName      = "lock"
	partialSuffix = ".partial"
)

// Assets are the release's files an encrypted archive keeps (§1).
var Assets = []string{"MailArchive-macos.zip", "mailarchive-windows-amd64.zip", "mailarchive-linux-amd64", "mailarchive-linux-arm64"}

// AssetFor is the asset of a release platform (container.ReleasePlatform), or "".
func AssetFor(platform string) string {
	switch platform {
	case "macos":
		return Assets[0]
	case "windows-amd64":
		return Assets[1]
	case "linux-amd64":
		return Assets[2]
	case "linux-arm64":
		return Assets[3]
	}
	return ""
}

var (
	// ErrNewerFormat: the cache was laid out by a newer MailArchive; it is left alone.
	ErrNewerFormat = errors.New("the program cache was made by a newer MailArchive; it is left as it is")
	// ErrUnsafe refuses a cache folder that is a link, not a folder, another user's,
	// or one others can write.
	ErrUnsafe = errors.New("the program cache's folder is not safe to use")
	// ErrBusy: another MailArchive holds the cache for longer than the bounded wait.
	ErrBusy = errors.New("the program cache is busy")
	// ErrNotVerified: what was asked for is not a verified copy in the cache.
	ErrNotVerified = errors.New("the program cache holds no verified copy of that file")
)

// defaultRoot is <user cache dir>/mailarchive/program-cache; a var so a test can move
// it (a TestMain points it at a temporary folder).
var defaultRoot = func() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "mailarchive", "program-cache"), nil
}

// DefaultRoot is the cache's folder on this computer.
func DefaultRoot() (string, error) { return defaultRoot() }

// UseTestRoot points the cache's folder at dir for a test binary's whole run (each
// package's TestMain, R22). done restores it and reports an error when the real
// folder appeared or vanished meanwhile — a test that touched it.
func UseTestRoot(dir string) (done func() error) {
	orig := defaultRoot
	real, rerr := orig()
	_, before := os.Lstat(real)
	defaultRoot = func() (string, error) { return dir, nil }
	return func() error {
		defaultRoot = orig
		if rerr != nil {
			return nil
		}
		if _, after := os.Lstat(real); (before == nil) != (after == nil) {
			return fmt.Errorf("the real program cache was touched: %s", real)
		}
		return nil
	}
}

// Cache is the program cache at a folder.
type Cache struct {
	root     string
	now      func() time.Time
	lockWait time.Duration
}

// Open opens the cache at root ("" for this computer's), making its folders 0700. Each
// part MailArchive owns — mailarchive/ and program-cache/ — is checked without
// following links and, on Unix, refused when another user owns it or others can write
// it. A cache of a newer format is ErrNewerFormat and left alone.
func Open(root string) (*Cache, error) {
	if root == "" {
		r, err := defaultRoot()
		if err != nil {
			return nil, err
		}
		root = r
	}
	root = filepath.Clean(root)
	if err := ensureOwnDir(filepath.Dir(root)); err != nil {
		return nil, err
	}
	if err := ensureOwnDir(root); err != nil {
		return nil, err
	}
	c := &Cache{root: root, now: time.Now, lockWait: 30 * time.Second}
	switch b, err := util.ReadRegularFile(filepath.Join(root, formatName), 64); {
	case errors.Is(err, fs.ErrNotExist):
		if err := util.WriteFileAtomic0600(filepath.Join(root, formatName), []byte(strconv.Itoa(Format)+"\n")); err != nil {
			return nil, err
		}
	case err != nil:
		return nil, fmt.Errorf("%w: its format file: %v", ErrUnsafe, err)
	default:
		n, perr := strconv.Atoi(strings.TrimSpace(string(b)))
		if perr != nil || n > Format {
			return nil, ErrNewerFormat
		}
	}
	return c, nil
}

// Root is the cache's folder.
func (c *Cache) Root() string { return c.root }

// ensureOwnDir makes dir (0700) when absent and refuses it unless it is a real folder
// — never a link — this user's, that others cannot write.
func ensureOwnDir(dir string) error {
	fi, err := os.Lstat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
			return err
		}
		fi, err = os.Lstat(dir)
	}
	if err != nil {
		return err
	}
	if !fi.IsDir() || fi.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("%w: %s is %s, not a folder", ErrUnsafe, dir, util.ModeKind(fi.Mode()))
	}
	if runtime.GOOS != "windows" {
		if fi.Mode().Perm()&0o022 != 0 {
			return fmt.Errorf("%w: others can write %s", ErrUnsafe, dir)
		}
		if !ownedByMe(fi) {
			return fmt.Errorf("%w: %s belongs to another user", ErrUnsafe, dir)
		}
	}
	return nil
}

// versionDir is the folder of an exact version, made and checked as the root is.
func (c *Cache) versionDir(version string, make bool) (string, error) {
	if !container.ExactVersion(version) {
		return "", fmt.Errorf("progcache: %q is not an exact version", version)
	}
	d := filepath.Join(c.root, version)
	if !make {
		if fi, err := os.Lstat(d); err != nil {
			return "", err
		} else if !fi.IsDir() || fi.Mode()&fs.ModeSymlink != 0 {
			return "", fmt.Errorf("%w: %s is not a folder", ErrUnsafe, d)
		}
		return d, nil
	}
	return d, ensureOwnDir(d)
}

// lock takes the cache's lock for a short step, waiting at most lockWait (R17).
func (c *Cache) lock() (*lockfile.Lock, error) {
	l, err := lockfile.AcquireWait(filepath.Join(c.root, lockName), "program-cache", c.lockWait)
	if errors.Is(err, lockfile.ErrHeld) {
		return nil, fmt.Errorf("%w: %v", ErrBusy, err)
	}
	return l, err
}

// Busy says another MailArchive is using the cache now: its lock is held, or a
// download holds one of its partial files (NET-3).
func Busy(root string) bool {
	if lockfile.Held(filepath.Join(root, lockName)) {
		return true
	}
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		if !e.IsDir() || !container.ExactVersion(e.Name()) {
			continue
		}
		for _, a := range Assets {
			p := filepath.Join(root, e.Name(), a+partialSuffix)
			if fi, err := os.Lstat(p); err == nil && fi.Mode().IsRegular() && lockfile.Held(p) {
				return true
			}
		}
	}
	return false
}

// verified is the record of a version's checked files (R18): the SHA256SUMS digest
// and, per asset, its SHA-256 from SHA256SUMS.
type verified struct {
	Sums   string            `json:"sums"`
	Assets map[string]string `json:"assets"`
}

func (c *Cache) readVerified(dir string) verified {
	var v verified
	if b, err := util.ReadRegularFile(filepath.Join(dir, verifiedName), 1<<16); err == nil {
		json.Unmarshal(b, &v)
	}
	if v.Assets == nil {
		v.Assets = map[string]string{}
	}
	return v
}

func writeJSON(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return util.WriteFileAtomic0600(path, b)
}

// Verified is the assets of version the cache holds verified, each with its SHA-256.
func (c *Cache) Verified(version string) map[string]string {
	dir, err := c.versionDir(version, false)
	if err != nil {
		return nil
	}
	v := c.readVerified(dir)
	if sum, err := fileSHA(filepath.Join(dir, SumsName)); err != nil || sum != v.Sums || v.Sums == "" {
		return nil
	}
	out := map[string]string{}
	for a, d := range v.Assets {
		if fi, err := os.Lstat(filepath.Join(dir, a)); err == nil && fi.Mode().IsRegular() {
			out[a] = d
		}
	}
	return out
}

// Asset is a verified file of the cache: its SHA-256 and size.
type Asset struct {
	SHA256 string
	Size   int64
}

// Assets is Verified with each file's size, and the version's SHA256SUMS as one more
// entry under SumsName.
func (c *Cache) Assets(version string) map[string]Asset {
	v := c.Verified(version)
	if v == nil {
		return nil
	}
	dir := filepath.Join(c.root, version)
	out := map[string]Asset{}
	for a, d := range v {
		if fi, err := os.Lstat(filepath.Join(dir, a)); err == nil {
			out[a] = Asset{SHA256: d, Size: fi.Size()}
		}
	}
	if b, err := c.Sums(version); err == nil {
		out[SumsName] = Asset{SHA256: hexSHA(b), Size: int64(len(b))}
	}
	return out
}

// Sums is version's verified SHA256SUMS, byte for byte (R16, R18).
func (c *Cache) Sums(version string) ([]byte, error) {
	dir, err := c.versionDir(version, false)
	if err != nil {
		return nil, err
	}
	b, err := util.ReadRegularFile(filepath.Join(dir, SumsName), maxSums)
	if err != nil {
		return nil, err
	}
	if v := c.readVerified(dir); v.Sums == "" || hexSHA(b) != v.Sums {
		return nil, ErrNotVerified
	}
	return b, nil
}

// CopyOut streams version's asset to w under the cache's lock, checked again against
// the verified record as it goes, and records the version as used (R19). It returns
// the SHA-256 of the bytes written; a file that no longer matches is removed with
// its verified entry.
func (c *Cache) CopyOut(version, asset string, w io.Writer) (string, error) {
	dir, err := c.versionDir(version, false)
	if err != nil {
		return "", err
	}
	l, err := c.lock()
	if err != nil {
		return "", err
	}
	defer l.Release()
	want := c.Verified(version)[asset]
	if want == "" {
		return "", ErrNotVerified
	}
	f, err := util.OpenRegularFile(filepath.Join(dir, asset))
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(w, h), io.LimitReader(f, MaxFile+1)); err != nil {
		return "", err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != want {
		f.Close()
		c.forget(dir, asset)
		return "", fmt.Errorf("%w: %s %s changed in the cache", ErrNotVerified, version, asset)
	}
	a := c.readAttempt(dir)
	a.Used = c.now().UTC()
	writeJSON(filepath.Join(dir, attemptName), a)
	return got, nil
}

// forget removes an asset and its verified entry.
func (c *Cache) forget(dir, asset string) {
	os.Remove(filepath.Join(dir, asset))
	v := c.readVerified(dir)
	delete(v.Assets, asset)
	writeJSON(filepath.Join(dir, verifiedName), v)
}

// Expects is the newest version an archive opened here expects, or "" (validated on
// every read, before any path is built from it; R19).
func (c *Cache) Expects() string {
	b, err := util.ReadRegularFile(filepath.Join(c.root, expectsName), 64)
	if err != nil {
		return ""
	}
	if v := strings.TrimSpace(string(b)); container.ExactVersion(v) {
		return v
	}
	return ""
}

// SetExpects records version as expected when it is above the one recorded.
func (c *Cache) SetExpects(version string) error {
	if !container.ExactVersion(version) {
		return fmt.Errorf("progcache: %q is not an exact version", version)
	}
	if container.CompareVersions(version, c.Expects()) <= 0 {
		return nil
	}
	return util.WriteFileAtomic0600(filepath.Join(c.root, expectsName), []byte(version+"\n"))
}

// Prune keeps this computer's version, the expects version and the two most recently
// used versions, and removes the rest — file by file, only names the cache writes, a
// folder only when empty; a version being downloaded is kept (R19).
func (c *Cache) Prune(this string) error {
	l, err := c.lock()
	if err != nil {
		return err
	}
	defer l.Release()
	keep := map[string]bool{this: true, c.Expects(): true}
	type used struct {
		v  string
		at time.Time
	}
	var all []used
	entries, _ := os.ReadDir(c.root)
	for _, e := range entries {
		if !e.IsDir() || !container.ExactVersion(e.Name()) {
			continue
		}
		d := filepath.Join(c.root, e.Name())
		a := c.readAttempt(d)
		at := a.Used
		if at.After(c.now()) {
			at = time.Time{} // a record dated in the future counts as expired
		}
		all = append(all, used{e.Name(), at})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].at.After(all[j].at) })
	for i, u := range all {
		if i < 2 && !u.at.IsZero() {
			keep[u.v] = true
		}
	}
	for _, u := range all {
		if keep[u.v] {
			continue
		}
		c.removeVersion(filepath.Join(c.root, u.v))
	}
	return nil
}

// removeVersion removes a version's folder unless a download holds one of its
// partial files.
func (c *Cache) removeVersion(d string) {
	for _, a := range Assets {
		if lockfile.Held(filepath.Join(d, a+partialSuffix)) {
			return
		}
	}
	for _, n := range versionNames() {
		os.Remove(filepath.Join(d, n))
	}
	os.Remove(d)
}

// versionNames are the names a version's folder may hold.
func versionNames() []string {
	names := []string{SumsName, verifiedName, attemptName}
	for _, a := range Assets {
		names = append(names, a, a+partialSuffix)
	}
	return names
}

// UninstallTargets is what uninstall removes of the cache at root, deepest first: the
// names the cache writes and its folders, never a link followed. It refuses while
// another MailArchive holds the cache (R19, PG2).
func UninstallTargets(root string) ([]string, error) {
	fi, err := os.Lstat(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !fi.IsDir() || fi.Mode()&fs.ModeSymlink != 0 {
		return nil, fmt.Errorf("%w: %s is not a folder", ErrUnsafe, root)
	}
	if Busy(root) {
		return nil, fmt.Errorf("%w: a download of MailArchive's program copies is under way — let it finish, then uninstall again", ErrBusy)
	}
	// Every entry is either one the cache writes — its fixed names, an atomic write's
	// leftover temporary file, the system's litter — or the whole uninstall is refused
	// before anything is removed, so the plan and the run agree (NET-6).
	// own lists dir's files, each a name the cache writes, an atomic write's leftover
	// temporary file or the system's litter; a version folder (at the root) is listed by
	// its caller. Anything else refuses.
	own := func(dir string, names []string, versions bool) ([]string, []string, error) {
		known := map[string]bool{}
		for _, n := range names {
			known[n] = true
		}
		var files, dirs []string
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, nil, err
		}
		for _, e := range entries {
			n := e.Name()
			switch {
			case versions && e.IsDir() && container.ExactVersion(n):
				dirs = append(dirs, filepath.Join(dir, n))
			case e.Type().IsRegular() && (known[n] || strings.HasPrefix(n, ".tmp-") || util.IsLitter(n)):
				files = append(files, filepath.Join(dir, n))
			default:
				return nil, nil, fmt.Errorf("%w: %s holds %s, which MailArchive did not put there — remove it, then uninstall again; nothing was removed", ErrUnsafe, dir, strconv.QuoteToASCII(n))
			}
		}
		return files, dirs, nil
	}
	top, versionDirs, err := own(root, []string{formatName, expectsName, lockName}, true)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, d := range versionDirs {
		files, _, err := own(d, versionNames(), false)
		if err != nil {
			return nil, err
		}
		out = append(append(out, files...), d)
	}
	return append(append(out, top...), root), nil
}

// attempt is a version's attempt record (R17, R19).
type attempt struct {
	At         time.Time `json:"at,omitempty"`    // the last counted attempt
	Class      string    `json:"class,omitempty"` // its outcome class; "" a success
	Mismatches int       `json:"mismatches,omitempty"`
	StopKey    string    `json:"stop_key,omitempty"` // with a terminal class: this program's version and expects when it was reached
	Used       time.Time `json:"used,omitempty"`     // the last successful copy out
}

func (c *Cache) readAttempt(dir string) attempt {
	var a attempt
	if b, err := util.ReadRegularFile(filepath.Join(dir, attemptName), 1<<12); err == nil {
		json.Unmarshal(b, &a)
	}
	return a
}

func fileSHA(path string) (string, error) {
	f, err := util.OpenRegularFile(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, io.LimitReader(f, MaxFile+1)); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func hexSHA(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
