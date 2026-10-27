package progcache

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/util"
)

// The download (program design §5, R17, R18): HTTPS to the built-in repository only,
// redirects only to github.com and hosts under .githubusercontent.com, at most five;
// a 200 MB ceiling per file; the proxy from the environment; resume with a range
// request; SHA256SUMS first, then each asset against it.

// DefaultBaseURL is where releases are downloaded from: <base>/<version>/<asset>.
const DefaultBaseURL = "https://github.com/alexdimarco/mail-archive-tool/releases/download"

// MaxFile is the largest file the download takes.
const MaxFile = 200 << 20

// maxSums bounds a release's checksum file.
const maxSums = 64 << 10

// The outcome classes of a download. ClassNotPublished and ClassRepeatedMismatch are
// terminal: retries stop until this program's version or the expects version changes.
const (
	ClassNotPublished     = "not published"
	ClassMismatch         = "checksum mismatch"
	ClassRepeatedMismatch = "repeated checksum mismatch"
	ClassTooLarge         = "too large"
	ClassRedirect         = "refused redirect"
	ClassServer           = "server error"
	ClassNetwork          = "no network"
	ClassStalled          = "stalled"
	ClassCancelled        = "cancelled"
	ClassBusy             = "cache busy"
	ClassWaitRanOut       = "the download did not finish within the wait"
)

// terminal says a class stops retries until a version changes.
func terminal(class string) bool { return class == ClassNotPublished || class == ClassRepeatedMismatch }

// counted says a class counts as that day's attempt: a busy cache and a cancel do not.
func counted(class string) bool { return class != ClassBusy && class != ClassCancelled }

// GitHubHost says host is one a release download may be served from.
func GitHubHost(host string) bool {
	h := strings.ToLower(host)
	return h == "github.com" || strings.HasSuffix(h, ".githubusercontent.com")
}

// Fetcher downloads release copies into a cache.
type Fetcher struct {
	Cache   *Cache
	BaseURL string            // DefaultBaseURL unless a test serves its own
	Trusted func(string) bool // the hosts a download and its redirects may use; GitHubHost unless a test's
	Client  *http.Client      // nil: one with the environment's proxy
	This    string            // this program's version: a terminal class holds while it and expects stay
	Idle    time.Duration     // no bytes for this long ends the download (60 s)
	Overall time.Duration     // the whole download's limit (0: none beyond the caller's)
	MaxFile int64             // the per-file ceiling (MaxFile)
	onBytes func(n int64)     // a test's progress hook
}

// Result is what a download did.
type Result struct {
	Verified     map[string]string // the assets now verified, with their SHA-256
	NotPublished []string          // the assets the release does not have
	Class        string            // "" when every published asset is verified; else the class that stopped one
}

var errRedirect = errors.New("a redirect to a host or scheme MailArchive does not download from")

func (f *Fetcher) trusted(host string) bool {
	if f.Trusted != nil {
		return f.Trusted(host)
	}
	return GitHubHost(host)
}

func (f *Fetcher) maxFile() int64 {
	if f.MaxFile > 0 {
		return f.MaxFile
	}
	return MaxFile
}

func (f *Fetcher) idle() time.Duration {
	if f.Idle > 0 {
		return f.Idle
	}
	return 60 * time.Second
}

func (f *Fetcher) client() *http.Client {
	base := f.Client
	if base == nil {
		base = &http.Client{Transport: &http.Transport{Proxy: http.ProxyFromEnvironment, TLSHandshakeTimeout: 30 * time.Second}}
	}
	c := *base
	c.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || req.URL.Scheme != "https" || !f.trusted(req.URL.Hostname()) {
			return errRedirect
		}
		return nil
	}
	return &c
}

// Fetch downloads version's SHA256SUMS and every asset it lists that the cache does
// not hold verified. It records the attempt (R17, R19).
func (f *Fetcher) Fetch(ctx context.Context, version string) Result {
	dir, err := f.Cache.versionDir(version, true)
	if err != nil {
		return Result{Class: ClassBusy}
	}
	if f.Overall > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, f.Overall)
		defer cancel()
	}
	var r Result
	sums, class := f.sums(ctx, dir, version)
	if class == "" {
		for _, a := range Assets {
			want, ok := sums[a]
			if !ok {
				r.NotPublished = append(r.NotPublished, a)
				continue
			}
			if f.Cache.Verified(version)[a] == want {
				continue
			}
			cl, notPub := f.asset(ctx, dir, version, a, want)
			if notPub {
				r.NotPublished = append(r.NotPublished, a)
				continue
			}
			if cl != "" {
				class = cl
				if cl == ClassMismatch || cl == ClassRepeatedMismatch || cl == ClassCancelled {
					break
				}
			}
		}
	}
	r.Class = class
	r.Verified = f.Cache.Verified(version)
	f.record(dir, class)
	return r
}

// CountAttempt records an attempt that ended outside Fetch — the wait running out
// counts as that day's attempt (R17).
func (f *Fetcher) CountAttempt(version, class string) {
	if dir, err := f.Cache.versionDir(version, true); err == nil {
		f.record(dir, class)
	}
}

// record writes the attempt record for a class that counts.
func (f *Fetcher) record(dir, class string) {
	if !counted(class) {
		return
	}
	a := f.Cache.readAttempt(dir)
	a.At, a.Class, a.StopKey = f.Cache.now().UTC(), class, ""
	if terminal(class) {
		a.StopKey = f.stopKey()
	}
	if class != ClassMismatch && class != ClassRepeatedMismatch {
		a.Mismatches = 0
	}
	writeJSON(filepath.Join(dir, attemptName), a)
}

func (f *Fetcher) stopKey() string { return f.This + "|" + f.Cache.Expects() }

// ShouldTry says whether a download of version may start now (R17, R19): once a day
// after a counted failure; never after a terminal class until this program's version
// or the expects version changes; an attempt record dated in the future has expired.
func (f *Fetcher) ShouldTry(version string) (bool, string) {
	dir, err := f.Cache.versionDir(version, false)
	if err != nil {
		return true, ""
	}
	a := f.Cache.readAttempt(dir)
	now := f.Cache.now()
	switch {
	case a.At.IsZero(), a.At.After(now):
		return true, ""
	case terminal(a.Class) && a.StopKey == f.stopKey():
		return false, a.Class
	case terminal(a.Class):
		return true, ""
	case a.Class != "" && now.Sub(a.At) < 24*time.Hour:
		return false, "tried today (" + a.Class + ")"
	}
	return true, ""
}

var sumsLine = regexp.MustCompile(`^([0-9a-f]{64}) [ *]([A-Za-z0-9][A-Za-z0-9._-]{0,63})$`)

// parseSums reads a SHA256SUMS file's lines.
func parseSums(b []byte) (map[string]string, error) {
	out := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if line == "" {
			continue
		}
		m := sumsLine.FindStringSubmatch(line)
		if m == nil {
			return nil, fmt.Errorf("progcache: a SHA256SUMS line does not read: %q", line)
		}
		out[m[2]] = m[1]
	}
	return out, sc.Err()
}

// sums is version's checksums: the cached SHA256SUMS when verified, else downloaded
// and written atomically, its digest recorded in verified (R18). A release without
// one is not published.
func (f *Fetcher) sums(ctx context.Context, dir, version string) (map[string]string, string) {
	if b, err := f.Cache.Sums(version); err == nil {
		if m, err := parseSums(b); err == nil {
			return m, ""
		}
	}
	resp, class := f.get(ctx, version, SumsName, 0)
	if class != "" {
		return nil, class
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, ClassNotPublished
	}
	if resp.StatusCode != http.StatusOK {
		return nil, ClassServer
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxSums+1))
	switch {
	case err != nil:
		return nil, ClassNetwork
	case len(b) > maxSums:
		return nil, ClassTooLarge
	}
	m, err := parseSums(b)
	if err != nil {
		return nil, ClassServer
	}
	l, err := f.Cache.lock()
	if err != nil {
		return nil, ClassBusy
	}
	defer l.Release()
	v := f.Cache.readVerified(dir)
	if v.Sums != hexSHA(b) {
		v = verified{Sums: hexSHA(b), Assets: map[string]string{}} // checked against other sums: check again
	}
	if err := util.WriteFileAtomic0600(filepath.Join(dir, SumsName), b); err != nil {
		return nil, ClassBusy
	}
	if err := writeJSON(filepath.Join(dir, verifiedName), v); err != nil {
		return nil, ClassBusy
	}
	return m, ""
}

// get requests one file of version from offset, every response checked by the
// redirect rule; it never returns a response for a refused redirect.
func (f *Fetcher) get(ctx context.Context, version, name string, offset int64) (*http.Response, string) {
	base := f.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	u, err := url.Parse(strings.TrimRight(base, "/") + "/" + url.PathEscape(version) + "/" + url.PathEscape(name))
	if err != nil || u.Scheme != "https" || !f.trusted(u.Hostname()) {
		return nil, ClassRedirect
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, ClassNetwork
	}
	if offset > 0 {
		req.Header.Set("Range", "bytes="+strconv.FormatInt(offset, 10)+"-")
	}
	resp, err := f.client().Do(req)
	switch {
	case err == nil:
		if resp.StatusCode >= 500 {
			resp.Body.Close()
			return nil, ClassServer
		}
		return resp, ""
	case errors.Is(err, errRedirect):
		return nil, ClassRedirect
	case ctx.Err() != nil:
		return nil, ClassCancelled
	}
	return nil, ClassNetwork
}

// asset downloads one asset into its partial file — held under its own OS lock, never
// the cache's (R17) — resuming what an earlier attempt left, checks it against want
// and moves it into place under the cache's lock.
func (f *Fetcher) asset(ctx context.Context, dir, version, asset, want string) (class string, notPublished bool) {
	part := filepath.Join(dir, asset+partialSuffix)
	if fi, err := os.Lstat(part); err == nil && !fi.Mode().IsRegular() {
		return ClassBusy, false
	} else if err != nil {
		pf, err := os.OpenFile(part, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return ClassBusy, false
		}
		pf.Close()
	}
	hold, err := lockfile.HoldFile(part)
	if err != nil {
		return ClassBusy, false
	}
	class, notPublished = f.download(ctx, hold, version, asset)
	if class != "" || notPublished {
		hold.Close()
		if class != ClassCancelled && class != ClassStalled && class != ClassNetwork && class != ClassServer {
			os.Remove(part) // a refused or too-large file is not resumed
		}
		return class, notPublished
	}
	hold.Sync()
	hold.Seek(0, io.SeekStart)
	h := sha256.New()
	_, herr := io.Copy(h, io.LimitReader(hold, f.maxFile()+1))
	hold.Close()
	if herr != nil {
		return ClassNetwork, false
	}
	if hex.EncodeToString(h.Sum(nil)) != want {
		os.Remove(part)
		return f.mismatch(dir), false
	}
	l, err := f.Cache.lock()
	if err != nil {
		return ClassBusy, false
	}
	defer l.Release()
	if err := os.Rename(part, filepath.Join(dir, asset)); err != nil {
		return ClassBusy, false
	}
	v := f.Cache.readVerified(dir)
	v.Assets[asset] = want
	writeJSON(filepath.Join(dir, verifiedName), v)
	return "", false
}

// mismatch deletes SHA256SUMS with the asset (R18) and counts it: two in a row are
// terminal.
func (f *Fetcher) mismatch(dir string) string {
	if l, err := f.Cache.lock(); err == nil {
		os.Remove(filepath.Join(dir, SumsName))
		writeJSON(filepath.Join(dir, verifiedName), verified{Assets: map[string]string{}})
		l.Release()
	}
	a := f.Cache.readAttempt(dir)
	a.Mismatches++
	writeJSON(filepath.Join(dir, attemptName), a)
	if a.Mismatches >= 2 {
		return ClassRepeatedMismatch
	}
	return ClassMismatch
}

// download fills the held partial file from where it ends, a read idle for f.idle()
// ending it as stalled, a total above the ceiling as too large.
func (f *Fetcher) download(ctx context.Context, part *os.File, version, asset string) (string, bool) {
	offset, _ := part.Seek(0, io.SeekEnd)
	rctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var stalled atomic.Bool
	timer := time.AfterFunc(f.idle(), func() { stalled.Store(true); cancel() })
	defer timer.Stop()
	resp, class := f.get(rctx, version, asset, offset)
	if class != "" {
		if stalled.Load() {
			return ClassStalled, false
		}
		return class, false
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusNotFound:
		return "", true
	case http.StatusPartialContent:
		if !strings.HasPrefix(resp.Header.Get("Content-Range"), fmt.Sprintf("bytes %d-", offset)) {
			return ClassServer, false
		}
	case http.StatusOK:
		if err := part.Truncate(0); err != nil {
			return ClassBusy, false
		}
		offset, _ = part.Seek(0, io.SeekStart)
	case http.StatusRequestedRangeNotSatisfiable:
		part.Truncate(0) // the next attempt starts over
		return ClassServer, false
	default:
		return ClassServer, false
	}
	if resp.ContentLength > 0 && offset+resp.ContentLength > f.maxFile() {
		return ClassTooLarge, false
	}
	buf := make([]byte, 64<<10)
	total := offset
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			timer.Reset(f.idle())
			total += int64(n)
			if total > f.maxFile() {
				return ClassTooLarge, false
			}
			if _, werr := part.Write(buf[:n]); werr != nil {
				return ClassBusy, false
			}
			if f.onBytes != nil {
				f.onBytes(total)
			}
		}
		if err == io.EOF {
			return "", false
		}
		if err != nil {
			switch {
			case stalled.Load():
				return ClassStalled, false
			case ctx.Err() != nil:
				return ClassCancelled, false
			}
			return ClassNetwork, false
		}
	}
}

// Manager runs at most one download per cache and version in this process; a second
// request joins the one running (R17).
type Manager struct {
	mu      sync.Mutex
	running map[string]*Task
}

// Downloads is this process's download manager.
var Downloads = &Manager{}

// Task is a running download.
type Task struct {
	done chan struct{}
	res  Result
}

// Start starts f's download of version, or joins the one running.
func (m *Manager) Start(ctx context.Context, f *Fetcher, version string) *Task {
	key := f.Cache.root + "\x00" + version
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.running == nil {
		m.running = map[string]*Task{}
	}
	if t := m.running[key]; t != nil {
		return t
	}
	t := &Task{done: make(chan struct{})}
	m.running[key] = t
	go func() {
		defer close(t.done)
		t.res = f.Fetch(ctx, version)
		m.mu.Lock()
		delete(m.running, key)
		m.mu.Unlock()
	}()
	return t
}

// Wait waits for the download up to max, or until ctx ends; ok is false when the wait
// ended first (the download goes on).
func (t *Task) Wait(ctx context.Context, max time.Duration) (Result, bool) {
	timer := time.NewTimer(max)
	defer timer.Stop()
	select {
	case <-t.done:
		return t.res, true
	case <-timer.C:
	case <-ctx.Done():
	}
	return Result{}, false
}
