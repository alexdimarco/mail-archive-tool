package app

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/progcache"
	"mail-archive-tool/internal/util"
)

// covers: MA-842, R37
// A Mac app bundle's self-copy is a zip that keeps each entry's Unix mode — the
// program executable — and stores a link as a link (R14).
func TestTheBundleZipKeepsModesAndLinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix modes and links")
	}
	app := filepath.Join(t.TempDir(), "MailArchive.app")
	os.MkdirAll(filepath.Join(app, "Contents", "MacOS"), 0o755)
	os.WriteFile(filepath.Join(app, "Contents", "MacOS", "mailarchive"), []byte("program"), 0o755)
	os.WriteFile(filepath.Join(app, "Contents", "Info.plist"), []byte("plist"), 0o644)
	os.Symlink("MacOS/mailarchive", filepath.Join(app, "Contents", "current"))
	var buf bytes.Buffer
	if err := zipBundle(&buf, app); err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	modes := map[string]fs.FileMode{}
	link := ""
	for _, f := range z.File {
		modes[f.Name] = f.Mode()
		if f.Mode()&fs.ModeSymlink != 0 {
			r, _ := f.Open()
			b, _ := io.ReadAll(r)
			r.Close()
			link = string(b)
		}
	}
	assure.Reached(t, modes["MailArchive.app/Contents/MacOS/mailarchive"].Perm() == 0o755 && modes["MailArchive.app/Contents/Info.plist"].Perm() == 0o644 &&
		link == "MacOS/mailarchive", "modes and the link kept")
}

// covers: MA-842, R37
// A self-copy is written only from the very file this program started from (R14): one
// replaced since is not copied — the run warns, and the archive keeps no self-copy.
func TestASelfCopyIsNeverOfAReplacedProgram(t *testing.T) {
	asDevBuild(t)
	stand := filepath.Join(t.TempDir(), "mailarchive")
	os.WriteFile(stand, []byte("the program that started"), 0o755)
	fi, _ := os.Stat(stand)
	fi = util.PinIdentity(fi) // as selfAtStart takes it: on Windows, else the swap matches itself (B1)
	orig := selfExecutable
	selfExecutable = func() (string, os.FileInfo) { return stand, fi }
	defer func() { selfExecutable = orig }()
	os.Remove(stand)
	os.WriteFile(stand, []byte("another program"), 0o755)
	out, key := encryptedOut(t)
	res, logged := backup(t, out, key, ProgramCopiesOn)
	assure.Reached(t, len(programFiles(t, out)) == 0 && strings.Contains(logged, "was replaced since it started") && res.Program.Class != ProgramUpdated,
		"no self-copy of a replaced program: "+res.Program.Class)
}

// covers: MA-842, R37
// A program running from the archive's own program folder does not refresh it (C15).
func TestAProgramRunFromTheArchiveDoesNotRefreshIt(t *testing.T) {
	asDevBuild(t)
	out, key := encryptedOut(t)
	inside := filepath.Join(out, container.ProgramDir, "mailarchive-linux-amd64")
	os.MkdirAll(filepath.Dir(inside), 0o755)
	os.WriteFile(inside, []byte("x"), 0o755)
	fi, _ := os.Stat(inside)
	orig := selfExecutable
	selfExecutable = func() (string, os.FileInfo) { return inside, fi }
	defer func() { selfExecutable = orig }()
	res, logged := backup(t, out, key, ProgramCopiesOn)
	assure.Reached(t, res.Program.Class == ProgramSkipped && strings.Contains(logged, "runs from the archive's own program folder"), "skipped: "+res.Program.Class)
}

// covers: MA-842, R37
// The download a release backup starts is waited for up to the limit (§5, R17, R20):
// one that does not finish in time counts as that day's attempt, and this computer's
// own program is kept instead, with a warning; the backup itself is not held past the
// wait.
func TestADownloadThatOutlastsTheWaitBringsTheOwnProgram(t *testing.T) {
	stall := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer stall.Close()
	isolatedCache(t)
	oi, of, ow := programIdentity, programFetcher, programWaitMax
	programIdentity = func() (string, bool) { return "v1.7.0", true }
	programFetcher = func(c *progcache.Cache, this string) *progcache.Fetcher {
		return &progcache.Fetcher{Cache: c, This: this, BaseURL: stall.URL, Client: stall.Client(), Trusted: func(h string) bool { return h == "127.0.0.1" }, Idle: 5 * time.Second}
	}
	programWaitMax = 300 * time.Millisecond
	defer func() { programIdentity, programFetcher, programWaitMax = oi, of, ow }()
	out, key := encryptedOut(t)
	start := time.Now()
	res, logged := backup(t, out, key, ProgramCopiesOn)
	_, name := selfPlatformAndName()
	files := strings.Join(programFiles(t, out), " ")
	c, _ := progcache.Open("")
	f := programFetcher(c, "v1.7.0")
	ok, _ := f.ShouldTry("v1.7.0")
	assure.Reached(t, strings.Contains(logged, "did not finish within the wait") && strings.Contains(files, name) && !ok &&
		time.Since(start) < time.Minute && res.Program.Class == ProgramUpdated, "the own program, the attempt counted: "+files)
}

// covers: MA-847, R37
// Skipping the wait (R20) ends only the wait: the front end is told when the wait starts
// and ends, the run goes on and keeps this computer's own program, with a warning; the
// skipped attempt is not counted, and the download, under a context that outlives the
// run, carries on after the run and its own context end (the PG4 pass, DUR-1).
func TestSkippingTheWaitKeepsTheOwnProgramAndCountsNoAttempt(t *testing.T) {
	cutOff := make(chan struct{}, 16) // a download request ended by its client
	stall := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		cutOff <- struct{}{}
	}))
	defer stall.Close()
	dl, endDownload := context.WithCancel(context.Background())
	defer endDownload() // before the server closes: the stalled request ends first
	isolatedCache(t)
	oi, of, ow := programIdentity, programFetcher, programWaitMax
	programIdentity = func() (string, bool) { return "v1.7.0", true }
	programFetcher = func(c *progcache.Cache, this string) *progcache.Fetcher {
		return &progcache.Fetcher{Cache: c, This: this, BaseURL: stall.URL, Client: stall.Client(), Trusted: func(h string) bool { return h == "127.0.0.1" }, Idle: time.Minute}
	}
	programWaitMax = 2 * time.Minute
	defer func() { programIdentity, programFetcher, programWaitMax = oi, of, ow }()
	out, key := encryptedOut(t)
	skip := make(chan struct{})
	var told []bool
	var logged bytes.Buffer
	start := time.Now()
	runCtx, endRun := context.WithCancel(context.Background())
	res, err := Run(runCtx, Options{Inputs: []string{encryptedSourcePST(t)}, Out: out, Key: key, ProgramCopies: ProgramCopiesOn,
		ProgramDownload: dl, ProgramSkip: skip, OnProgramWait: func(waiting bool) {
			told = append(told, waiting)
			if waiting {
				close(skip)
			}
		}}, log.New(&logged, "", 0), nil)
	endRun() // as the Desktop's job ends
	if err != nil {
		t.Fatalf("backup: %v\n%s", err, logged.String())
	}
	_, name := selfPlatformAndName()
	files := strings.Join(programFiles(t, out), " ")
	c, _ := progcache.Open("")
	ok, _ := programFetcher(c, "v1.7.0").ShouldTry("v1.7.0")
	// The download's request is not cut off when the run and its context end.
	finished := false
	select {
	case <-cutOff:
		finished = true
	case <-time.After(500 * time.Millisecond):
	}
	assure.Reached(t, strings.Contains(logged.String(), "was skipped") && strings.Contains(files, name) && ok && !finished &&
		len(told) == 2 && told[0] && !told[1] && time.Since(start) < time.Minute && res.Program.Class == ProgramUpdated,
		"skipped: the own program, no attempt counted, the download carrying on: "+files)
}
