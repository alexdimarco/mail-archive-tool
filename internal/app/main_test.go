package app

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/progcache"
	"mail-archive-tool/internal/vault"
)

// testHome is the throwaway home the whole package runs under.
var testHome string

// refusingReleases is the package's stand-in for the release server: it answers every
// request "not found", so a test that makes this program a release build without
// serving its own releases (asRelease(t, v, nil)) can never download a real one.
var refusingReleases *httptest.Server

// TestMain points MailArchive's scratch folder at a throwaway folder for the whole
// package, so a keyed -copy-first run never writes into the developer's real cache
// folder (job.ScratchDir). useScratchRoot still overrides it per test. It does the
// same for this computer's record of encrypted archives, which every keyless
// writer reads, and for the home, settings, cache and state folders (MA-894). Its
// tests make keys at the cheap test cost (MA-776).
func TestMain(m *testing.M) {
	vault.UseTestCost()
	root, err := os.MkdirTemp("", "mailarchive-app-scratch-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "TestMain: create the scratch folder:", err)
		os.Exit(1)
	}
	scratchRoot = func() (string, error) { return root, nil }
	// The copy's plan reads the archives this computer knows from the real config
	// folder; the package's tests know none unless one says so.
	knownArchives = func() []string { return nil }
	// The program copies (program design R22): a throwaway cache, checked untouched at
	// the end, and a small file standing in for this program, so a test's self-copy is
	// not the whole test binary. On Windows it is named mailarchive.exe, the console
	// program a self-copy is made from there (selfSource). Before the home moves, so
	// the real program cache is the one guarded.
	cacheDone := progcache.UseTestRoot(filepath.Join(root, "program-cache"))
	stand := filepath.Join(root, testProgramName())
	os.WriteFile(stand, []byte("#!/bin/sh\necho mailarchive test program\n"), 0o755)
	standFI, _ := os.Stat(stand)
	selfExecutable = func() (string, os.FileInfo) { return stand, standFI }
	restore := encrecord.SetDirForTest(filepath.Join(root, "record"))
	// The home, settings, cache and state folders are throwaway for the whole package
	// (MA-894); a test that needs its own sets them again with t.Setenv.
	testHome = filepath.Join(root, "home")
	if err := os.MkdirAll(testHome, 0o700); err != nil {
		fmt.Fprintln(os.Stderr, "TestMain:", err)
		os.Exit(1)
	}
	for _, v := range []string{"HOME", "USERPROFILE", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME", "APPDATA", "LOCALAPPDATA"} {
		os.Setenv(v, testHome)
	}
	// No test downloads a real release (MA-894).
	refusingReleases = httptest.NewTLSServer(http.NotFoundHandler())
	programFetcher = func(c *progcache.Cache, this string) *progcache.Fetcher {
		return &progcache.Fetcher{Cache: c, This: this, BaseURL: refusingReleases.URL, Client: refusingReleases.Client(),
			Trusted: func(h string) bool { return h == "127.0.0.1" }, Overall: programDownloadMax}
	}
	code := m.Run()
	refusingReleases.Close()
	restore()
	if err := cacheDone(); err != nil {
		fmt.Fprintln(os.Stderr, "TestMain:", err)
		code = 1
	}
	os.RemoveAll(root)
	os.Exit(code)
}

// testProgramName is the name of the file standing in for this program: on Windows
// mailarchive.exe, since a self-copy there is made only from the console program of
// that name (the P3-8 Windows lab), else a name no real program has.
func testProgramName() string {
	if runtime.GOOS == "windows" {
		return "mailarchive.exe"
	}
	return "mailarchive-test-program"
}
