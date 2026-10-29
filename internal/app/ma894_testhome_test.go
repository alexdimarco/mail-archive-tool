package app

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/progcache"
)

// covers: MA-894, R37
// The package's tests reach only the throwaway home and never download a real release:
// the settings, cache and home folders are under the home TestMain made, and a release
// build that serves no releases of its own (asRelease with none) fetches from the
// package's local server that answers nothing, never from GitHub. Checked before any
// fetch, so a broken guard downloads nothing.
func TestThePackageReachesNoRealHomeAndNoRealRelease(t *testing.T) {
	under := func(p string) bool {
		rel, err := filepath.Rel(testHome, p)
		return err == nil && !strings.HasPrefix(rel, "..")
	}
	cfg, cerr := os.UserConfigDir()
	cache, kerr := os.UserCacheDir()
	home, herr := os.UserHomeDir()
	if cerr != nil || kerr != nil || herr != nil || !under(cfg) || !under(cache) || !under(home) {
		t.Fatalf("settings %s (%v), cache %s (%v), home %s (%v): not all under the throwaway home %s", cfg, cerr, cache, kerr, home, herr, testHome)
	}

	asRelease(t, "v1.5.0", nil)
	isolatedCache(t)
	c, err := progcache.Open("")
	if err != nil {
		t.Fatal(err)
	}
	f := programFetcher(c, "v1.5.0")
	u, err := url.Parse(f.BaseURL)
	if err != nil || f.BaseURL == "" || f.BaseURL != refusingReleases.URL || u.Hostname() != "127.0.0.1" {
		t.Fatalf("a release build with no releases of its own fetches from %q (%v), not the package's refusing local server", f.BaseURL, err)
	}
	res := f.Fetch(t.Context(), "v1.5.0")
	assure.Reached(t, len(res.Verified) == 0, "nothing downloaded from the refusing server")
}
