package progcache

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/lockfile"
)

// release is a fake release server: files by version, the requests it saw.
type release struct {
	mu       sync.Mutex
	files    map[string][]byte // "<version>/<name>"
	hits     map[string]int
	ranges   []string
	stall    string // a name whose body stalls after its first bytes
	redirect string // a name answered with a redirect to this URL
	srv      *httptest.Server
}

func newRelease(t *testing.T, version string, assets map[string][]byte) *release {
	t.Helper()
	r := &release{files: map[string][]byte{}, hits: map[string]int{}}
	var sums strings.Builder
	for _, a := range Assets {
		b, ok := assets[a]
		if !ok {
			continue
		}
		r.files[version+"/"+a] = b
		s := sha256.Sum256(b)
		fmt.Fprintf(&sums, "%s  %s\n", hex.EncodeToString(s[:]), a)
	}
	r.files[version+"/"+SumsName] = []byte(sums.String())
	r.srv = httptest.NewTLSServer(http.HandlerFunc(r.serve))
	t.Cleanup(r.srv.Close)
	return r
}

func (r *release) serve(w http.ResponseWriter, req *http.Request) {
	name := strings.TrimPrefix(req.URL.Path, "/")
	r.mu.Lock()
	r.hits[name]++
	if rg := req.Header.Get("Range"); rg != "" {
		r.ranges = append(r.ranges, name+" "+rg)
	}
	b, ok := r.files[name]
	stall, redirect := r.stall, r.redirect
	r.mu.Unlock()
	if redirect != "" && strings.HasSuffix(name, "/"+Assets[2]) {
		http.Redirect(w, req, redirect, http.StatusFound)
		return
	}
	if !ok {
		http.NotFound(w, req)
		return
	}
	if stall != "" && strings.HasSuffix(name, "/"+stall) {
		w.Write(b[:10])
		w.(http.Flusher).Flush()
		<-req.Context().Done()
		return
	}
	http.ServeContent(w, req, name, time.Time{}, bytes.NewReader(b))
}

func assets(n int) map[string][]byte {
	m := map[string][]byte{}
	for i, a := range Assets {
		m[a] = bytes.Repeat([]byte{byte('a' + i)}, n+i)
	}
	return m
}

func (r *release) fetcher(t *testing.T) *Fetcher {
	t.Helper()
	c, err := Open(filepath.Join(t.TempDir(), "mailarchive", "program-cache"))
	if err != nil {
		t.Fatal(err)
	}
	return &Fetcher{Cache: c, BaseURL: r.srv.URL, Client: r.srv.Client(), This: "v1.5.0",
		Trusted: func(h string) bool { return h == "127.0.0.1" }, Idle: 2 * time.Second}
}

func sha(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

// covers: MA-836, R37
// A release is downloaded into the cache (program design §5, R18): SHA256SUMS first,
// then each asset checked against it, moved into place, recorded verified, and copied
// out byte for byte — checked again as it goes and the version marked used. The
// checksum file is kept byte for byte.
func TestAReleaseIsDownloadedVerifiedAndCopiedOut(t *testing.T) {
	want := assets(1000)
	r := newRelease(t, "v1.5.0", want)
	f := r.fetcher(t)
	res := f.Fetch(context.Background(), "v1.5.0")
	if res.Class != "" || len(res.Verified) != 4 || len(res.NotPublished) != 0 {
		t.Fatalf("the download: %+v", res)
	}
	var out bytes.Buffer
	got, err := f.Cache.CopyOut("v1.5.0", Assets[2], &out)
	sums, serr := f.Cache.Sums("v1.5.0")
	a := f.Cache.readAttempt(filepath.Join(f.Cache.Root(), "v1.5.0"))
	assure.Reached(t, err == nil && got == sha(want[Assets[2]]) && bytes.Equal(out.Bytes(), want[Assets[2]]) && serr == nil &&
		bytes.Equal(sums, r.files["v1.5.0/"+SumsName]) && !a.Used.IsZero(), "copied out whole and marked used")

	// A second download asks for nothing it holds verified.
	f.Fetch(context.Background(), "v1.5.0")
	assure.Reached(t, r.hits["v1.5.0/"+Assets[0]] == 1, "nothing fetched twice")
}

// covers: MA-836, R37
// A file in the cache that changed is never copied out (R18): the copy is refused,
// the file and its verified entry are removed; a changed SHA256SUMS makes nothing of
// that version verified.
func TestAChangedCacheFileIsNeverCopiedOut(t *testing.T) {
	r := newRelease(t, "v1.5.0", assets(500))
	f := r.fetcher(t)
	f.Fetch(context.Background(), "v1.5.0")
	dir := filepath.Join(f.Cache.Root(), "v1.5.0")
	os.WriteFile(filepath.Join(dir, Assets[1]), []byte("swapped"), 0o600)
	var out bytes.Buffer
	_, err := f.Cache.CopyOut("v1.5.0", Assets[1], &out)
	_, gone := os.Stat(filepath.Join(dir, Assets[1]))
	assure.Refused(t, 1, fmt.Sprint(err), assure.Code(1), assure.Names("changed in the cache"),
		assure.NoSideEffect(func() bool { return os.IsNotExist(gone) && f.Cache.Verified("v1.5.0")[Assets[1]] == "" }))
	os.WriteFile(filepath.Join(dir, SumsName), []byte("tampered"), 0o600)
	_, err = f.Cache.CopyOut("v1.5.0", Assets[0], &out)
	assure.Reached(t, err != nil && len(f.Cache.Verified("v1.5.0")) == 0, "a changed SHA256SUMS verifies nothing")
}

// covers: MA-836, R37
// What the release does not publish is said, not fetched (requirement 8): an asset the
// release lacks is "not published" and the rest are verified; a release with no
// SHA256SUMS is wholly "not published" — a terminal class.
func TestWhatTheReleaseDoesNotPublishIsSaid(t *testing.T) {
	a := assets(100)
	delete(a, Assets[3])
	r := newRelease(t, "v1.5.0", a)
	f := r.fetcher(t)
	res := f.Fetch(context.Background(), "v1.5.0")
	if res.Class != "" || len(res.Verified) != 3 || len(res.NotPublished) != 1 || res.NotPublished[0] != Assets[3] {
		t.Fatalf("one asset unpublished: %+v", res)
	}
	res = f.Fetch(context.Background(), "v9.9.9")
	ok, why := f.ShouldTry("v9.9.9")
	assure.Reached(t, res.Class == ClassNotPublished && !ok && why == ClassNotPublished, "an unpublished release stops retries: "+why)

	// Listed in SHA256SUMS but not served: not published, the rest verified.
	r2 := newRelease(t, "v1.5.0", assets(100))
	delete(r2.files, "v1.5.0/"+Assets[1])
	f2 := r2.fetcher(t)
	res = f2.Fetch(context.Background(), "v1.5.0")
	assure.Reached(t, res.Class == "" && len(res.NotPublished) == 1 && res.NotPublished[0] == Assets[1] && len(res.Verified) == 3, fmt.Sprintf("listed, not served: %+v", res))

	// A release whose SHA256SUMS changed — an asset withdrawn — verifies nothing of the
	// old list: the withdrawn asset is no longer verified (R18).
	r3 := newRelease(t, "v1.5.0", assets(100))
	f3 := r3.fetcher(t)
	f3.Fetch(context.Background(), "v1.5.0")
	r3.mu.Lock()
	delete(r3.files, "v1.5.0/"+Assets[3])
	sums := string(r3.files["v1.5.0/"+SumsName])
	lines := strings.Split(strings.TrimSpace(sums), "\n")
	r3.files["v1.5.0/"+SumsName] = []byte(strings.Join(lines[:3], "\n") + "\n")
	r3.mu.Unlock()
	os.Remove(filepath.Join(f3.Cache.Root(), "v1.5.0", SumsName))
	res = f3.Fetch(context.Background(), "v1.5.0")
	_, still := res.Verified[Assets[3]]
	assure.Reached(t, !still && len(res.Verified) == 3, fmt.Sprintf("withdrawn: %+v", res.Verified))
}

// covers: MA-836, R37
// A file that does not match its checksum is deleted with SHA256SUMS (R18); a second
// mismatch in a row is terminal.
func TestAChecksumMismatchDeletesAndTwiceIsTerminal(t *testing.T) {
	r := newRelease(t, "v1.5.0", assets(300))
	r.files["v1.5.0/"+Assets[0]] = []byte("not what the sums say")
	f := r.fetcher(t)
	dir := filepath.Join(f.Cache.Root(), "v1.5.0")
	res := f.Fetch(context.Background(), "v1.5.0")
	_, a := os.Stat(filepath.Join(dir, Assets[0]))
	_, p := os.Stat(filepath.Join(dir, Assets[0]+partialSuffix))
	_, s := os.Stat(filepath.Join(dir, SumsName))
	if res.Class != ClassMismatch || !os.IsNotExist(a) || !os.IsNotExist(p) || !os.IsNotExist(s) {
		t.Fatalf("a mismatch: %+v (asset %v, partial %v, sums %v)", res, a, p, s)
	}
	res = f.Fetch(context.Background(), "v1.5.0")
	ok, why := f.ShouldTry("v1.5.0")
	assure.Reached(t, res.Class == ClassRepeatedMismatch && !ok && why == ClassRepeatedMismatch, "two in a row are terminal")
	f.This = "v1.6.0" // this program's version changed: retries resume
	ok, _ = f.ShouldTry("v1.5.0")
	assure.Reached(t, ok, "a new program version tries again")
}

// covers: MA-836, R37
// The download takes nothing past its ceiling, from no host but the trusted ones, over
// HTTPS only, through at most five redirects; a refused or too-large file is not kept.
func TestTheDownloadRefusesTooLargeAndUntrustedRedirects(t *testing.T) {
	r := newRelease(t, "v1.5.0", assets(5000))
	f := r.fetcher(t)
	f.MaxFile = 4000
	res := f.Fetch(context.Background(), "v1.5.0")
	_, p := os.Stat(filepath.Join(f.Cache.Root(), "v1.5.0", Assets[0]+partialSuffix))
	if res.Class != ClassTooLarge || !os.IsNotExist(p) {
		t.Fatalf("too large: %+v, partial %v", res, p)
	}
	// A server that sends no length is held to the ceiling as the bytes come.
	r0 := newRelease(t, "v1.5.0", assets(5000))
	chunked := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		b, ok := r0.files[strings.TrimPrefix(req.URL.Path, "/")]
		if !ok {
			http.NotFound(w, req)
			return
		}
		for i := 0; i < len(b); i += 1000 {
			w.Write(b[i:min(i+1000, len(b))])
			w.(http.Flusher).Flush()
		}
	}))
	defer chunked.Close()
	f0 := r0.fetcher(t)
	f0.BaseURL, f0.Client, f0.MaxFile = chunked.URL, chunked.Client(), 4000
	res = f0.Fetch(context.Background(), "v1.5.0")
	assure.Reached(t, res.Class == ClassTooLarge, "no length, too large: "+res.Class)
	for name, to := range map[string]string{
		"another host": "https://evil.example/x",
		"plain http":   strings.Replace(r.srv.URL, "https://", "http://", 1) + "/v1.5.0/" + Assets[3],
	} {
		r2 := newRelease(t, "v1.5.0", assets(100))
		r2.redirect = to
		f := r2.fetcher(t)
		if name == "plain http" {
			r2.redirect = strings.Replace(r2.srv.URL, "https://", "http://", 1) + "/v1.5.0/" + Assets[3]
		}
		res := f.Fetch(context.Background(), "v1.5.0")
		assure.Reached(t, res.Class == ClassRedirect && f.Cache.Verified("v1.5.0")[Assets[2]] == "", name+": "+res.Class)
	}
	// Six redirects in a row.
	r3 := newRelease(t, "v1.5.0", assets(100))
	n := 0
	loop := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		n++
		http.Redirect(w, req, "/again"+strconv.Itoa(n), http.StatusFound)
	}))
	defer loop.Close()
	f3 := r3.fetcher(t)
	f3.BaseURL, f3.Client = loop.URL, loop.Client()
	res = f3.Fetch(context.Background(), "v1.5.0")
	assure.Reached(t, res.Class == ClassRedirect && n <= 6, fmt.Sprintf("a redirect loop: %s after %d", res.Class, n))
	f3.BaseURL = strings.Replace(r3.srv.URL, "https://", "http://", 1)
	assure.Reached(t, f3.Fetch(context.Background(), "v1.5.0").Class == ClassRedirect, "a plain-http base is refused")
}

// covers: MA-836, R37
// An interrupted download resumes with a range request where it stopped (§5); a server
// that stalls for the idle limit ends the attempt as stalled, its partial file kept to
// resume (R17).
func TestADownloadResumesAndAStallEndsIt(t *testing.T) {
	want := assets(2000)
	r := newRelease(t, "v1.5.0", want)
	r.stall = Assets[0]
	f := r.fetcher(t)
	f.Idle = 300 * time.Millisecond
	res := f.Fetch(context.Background(), "v1.5.0")
	part := filepath.Join(f.Cache.Root(), "v1.5.0", Assets[0]+partialSuffix)
	fi, err := os.Stat(part)
	if res.Class != ClassStalled || err != nil || fi.Size() != 10 {
		t.Fatalf("a stall: %+v, partial %v %v", res, fi, err)
	}
	r.mu.Lock()
	r.stall = ""
	r.mu.Unlock()
	res = f.Fetch(context.Background(), "v1.5.0")
	resumed := false
	for _, rg := range r.ranges {
		resumed = resumed || rg == "v1.5.0/"+Assets[0]+" bytes=10-"
	}
	assure.Reached(t, res.Class == "" && f.Cache.Verified("v1.5.0")[Assets[0]] == sha(want[Assets[0]]) && resumed, fmt.Sprintf("resumed: %v", r.ranges))
}

// covers: MA-837, R37
// Retries are bounded (R17, R19): a counted failure waits a day; a cancel and a busy
// partial file do not count; a record dated in the future has expired; the wait
// running out counts as that day's attempt.
func TestRetriesAreOnceADay(t *testing.T) {
	r := newRelease(t, "v1.5.0", assets(100))
	r.files = map[string][]byte{} // everything 404s but on a server error below
	f := r.fetcher(t)
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	f.Cache.now = func() time.Time { return now }
	srvErr := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(503) }))
	defer srvErr.Close()
	f.BaseURL, f.Client = srvErr.URL, srvErr.Client()
	if res := f.Fetch(context.Background(), "v1.5.0"); res.Class != ClassServer {
		t.Fatalf("a server error: %+v", res)
	}
	ok1, _ := f.ShouldTry("v1.5.0")
	now = now.Add(25 * time.Hour)
	ok2, _ := f.ShouldTry("v1.5.0")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f.Fetch(ctx, "v1.5.0") // a cancel is not counted
	now = now.Add(time.Minute)
	ok3, _ := f.ShouldTry("v1.5.0")
	f.CountAttempt("v1.5.0", ClassWaitRanOut)
	ok4, why := f.ShouldTry("v1.5.0")
	a := f.Cache.readAttempt(filepath.Join(f.Cache.Root(), "v1.5.0"))
	a.At = now.Add(48 * time.Hour)
	writeJSON(filepath.Join(f.Cache.Root(), "v1.5.0", attemptName), a)
	ok5, _ := f.ShouldTry("v1.5.0")
	assure.Reached(t, !ok1 && ok2 && ok3 && !ok4 && strings.Contains(why, ClassWaitRanOut) && ok5, fmt.Sprint(ok1, ok2, ok3, ok4, ok5, why))
}

// covers: MA-837, R37
// A partial file another download holds is not touched: the attempt ends "cache busy"
// and does not count (R17).
func TestAHeldPartialIsBusyAndNotCounted(t *testing.T) {
	r := newRelease(t, "v1.5.0", assets(100))
	f := r.fetcher(t)
	dir := filepath.Join(f.Cache.Root(), "v1.5.0")
	os.MkdirAll(dir, 0o700)
	part := filepath.Join(dir, Assets[0]+partialSuffix)
	os.WriteFile(part, []byte("someone's"), 0o600)
	h, err := lockfile.HoldFile(part)
	if err != nil {
		t.Skip("this system's file lock cannot be held twice in one process:", err)
	}
	defer h.Close()
	res := f.Fetch(context.Background(), "v1.5.0")
	b, _ := os.ReadFile(part)
	ok, _ := f.ShouldTry("v1.5.0")
	assure.Reached(t, res.Class == ClassBusy && string(b) == "someone's" && ok, "busy, untouched, not counted: "+res.Class)
}

// covers: MA-837, R37
// One download per cache and version in this process: a second request joins the
// running one, and every file is fetched once.
func TestASecondRequestJoinsTheRunningDownload(t *testing.T) {
	r := newRelease(t, "v1.5.0", assets(100000))
	f := r.fetcher(t)
	var m Manager
	t1 := m.Start(context.Background(), f, "v1.5.0")
	t2 := m.Start(context.Background(), f, "v1.5.0")
	r1, ok1 := t1.Wait(context.Background(), time.Minute)
	r2, ok2 := t2.Wait(context.Background(), time.Minute)
	assure.Reached(t, t1 == t2 && ok1 && ok2 && r1.Class == "" && r2.Class == "" && r.hits["v1.5.0/"+Assets[0]] == 1, "joined")
	slow := newRelease(t, "v1.5.0", assets(100))
	slow.stall = Assets[0]
	fs := slow.fetcher(t)
	fs.Idle = time.Second
	task := m.Start(context.Background(), fs, "v1.5.0")
	_, ok := task.Wait(context.Background(), 200*time.Millisecond)
	res, done := task.Wait(context.Background(), time.Minute) // the download goes on, then ends stalled
	assure.Reached(t, !ok && done && res.Class == ClassStalled, "the wait ends before the download: "+res.Class)
}

// covers: MA-835, R37
// The cache's folders are made 0700 and checked without following links (R19): a link
// at the cache's folder, a folder others can write, and a cache of a newer format are
// refused, the last left untouched; expects is validated on every read and only rises.
func TestTheCacheFolderIsSafe(t *testing.T) {
	base := t.TempDir()
	c, err := Open(filepath.Join(base, "mailarchive", "program-cache"))
	if err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(c.Root())
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o700 {
		t.Fatalf("the cache folder is %v", fi.Mode().Perm())
	}
	if runtime.GOOS != "windows" {
		other := t.TempDir()
		link := filepath.Join(t.TempDir(), "mailarchive")
		os.Mkdir(link, 0o700)
		os.Symlink(other, filepath.Join(link, "program-cache"))
		_, err := Open(filepath.Join(link, "program-cache"))
		wide := filepath.Join(t.TempDir(), "mailarchive")
		os.Mkdir(wide, 0o700)
		os.Chmod(wide, 0o777)
		_, err2 := Open(filepath.Join(wide, "program-cache"))
		assure.Reached(t, err != nil && err2 != nil, fmt.Sprintf("refused: %v / %v", err, err2))
	}
	os.WriteFile(filepath.Join(c.Root(), formatName), []byte("2\n"), 0o600)
	_, err = Open(c.Root())
	b, _ := os.ReadFile(filepath.Join(c.Root(), formatName))
	assure.Refused(t, 1, fmt.Sprint(err), assure.Code(1), assure.Names("newer MailArchive"),
		assure.NoSideEffect(func() bool { return string(b) == "2\n" }))
	os.WriteFile(filepath.Join(c.Root(), formatName), []byte("1\n"), 0o600)

	os.WriteFile(filepath.Join(c.Root(), expectsName), []byte("../../etc\n"), 0o600)
	if c.Expects() != "" {
		t.Fatal("an invalid expects was read")
	}
	c.SetExpects("v1.6.0")
	c.SetExpects("v1.5.9")
	assure.Reached(t, c.Expects() == "v1.6.0", "expects only rises: "+c.Expects())
}

// covers: MA-835, R37
// Pruning keeps this computer's version, the expects version and the two most recently
// used, removing the rest — only the names the cache writes, a folder only when empty —
// and a version being downloaded (R19).
func TestPruningKeepsWhatIsNeeded(t *testing.T) {
	c, _ := Open(filepath.Join(t.TempDir(), "mailarchive", "program-cache"))
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return now }
	mk := func(v string, used time.Duration, extra ...string) {
		d := filepath.Join(c.Root(), v)
		os.MkdirAll(d, 0o700)
		os.WriteFile(filepath.Join(d, Assets[0]), []byte("x"), 0o600)
		if used >= 0 {
			writeJSON(filepath.Join(d, attemptName), attempt{Used: now.Add(-used)})
		}
		for _, e := range extra {
			os.WriteFile(filepath.Join(d, e), []byte("x"), 0o600)
		}
	}
	mk("v1.0.0", 10*time.Hour)
	mk("v1.1.0", 1*time.Hour)
	mk("v1.2.0", 2*time.Hour)
	mk("v1.3.0", 3*time.Hour, "someone-elses.txt")
	mk("v1.4.0", -1)
	mk("v1.5.0", -1)
	mk("v1.6.0", -1)
	mk("v1.7.0", -1)
	c.SetExpects("v1.6.0")
	held := filepath.Join(c.Root(), "v1.7.0", Assets[1]+partialSuffix)
	os.WriteFile(held, nil, 0o600)
	h, herr := lockfile.HoldFile(held)
	if herr == nil {
		defer h.Close()
	}
	if err := c.Prune("v1.5.0"); err != nil {
		t.Fatal(err)
	}
	exists := func(v string) bool { _, err := os.Stat(filepath.Join(c.Root(), v)); return err == nil }
	_, extra := os.Stat(filepath.Join(c.Root(), "v1.3.0", "someone-elses.txt"))
	_, asset := os.Stat(filepath.Join(c.Root(), "v1.3.0", Assets[0]))
	assure.Reached(t, exists("v1.1.0") && exists("v1.2.0") && exists("v1.5.0") && exists("v1.6.0") &&
		!exists("v1.0.0") && !exists("v1.4.0") && extra == nil && os.IsNotExist(asset) && (herr != nil || exists("v1.7.0")), "kept and pruned")
}

// covers: MA-838, R37
// Uninstall lists the cache's own files and folders, deepest first, the cache's folder
// last — one folder to the person — never following a link; it refuses while another
// MailArchive holds the cache.
func TestUninstallListsTheCacheAndRefusesWhileHeld(t *testing.T) {
	r := newRelease(t, "v1.5.0", assets(100))
	f := r.fetcher(t)
	f.Fetch(context.Background(), "v1.5.0")
	targets, err := UninstallTargets(f.Cache.Root())
	if err != nil || len(targets) == 0 || targets[len(targets)-1] != f.Cache.Root() {
		t.Fatalf("targets: %v %v", targets, err)
	}
	for _, p := range targets {
		if fi, err := os.Lstat(p); err != nil || fi.Mode()&fs.ModeSymlink != 0 {
			t.Fatalf("a target is not the cache's own: %s", p)
		}
	}
	l, err := lockfile.Acquire(filepath.Join(f.Cache.Root(), lockName))
	if err != nil {
		t.Fatal(err)
	}
	defer l.Release()
	_, err = UninstallTargets(f.Cache.Root())
	assure.Refused(t, 1, fmt.Sprint(err), assure.Code(1), assure.Names("under way"), assure.NoSideEffect(func() bool { return true }))
}
