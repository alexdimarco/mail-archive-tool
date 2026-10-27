package progcache

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/lockfile"
)

// The adversarial pass on program copies (docs/review-encrypted-archives-phase3-pg-adversarial.md).

// covers: MA-843, R37
// A partial file an earlier attempt finished but could not move is moved as it is,
// never asked for again with a range (NET-2).
func TestAFinishedPartialIsMovedNotFetchedAgain(t *testing.T) {
	want := assets(500)
	r := newRelease(t, "v1.5.0", want)
	f := r.fetcher(t)
	dir := filepath.Join(f.Cache.Root(), "v1.5.0")
	os.MkdirAll(dir, 0o700)
	os.WriteFile(filepath.Join(dir, Assets[0]+partialSuffix), want[Assets[0]], 0o600)
	res := f.Fetch(context.Background(), "v1.5.0")
	assure.Reached(t, res.Class == "" && f.Cache.Verified("v1.5.0")[Assets[0]] == sha(want[Assets[0]]) && r.hits["v1.5.0/"+Assets[0]] == 0 && len(r.ranges) == 0,
		"moved as it was")
}

// covers: MA-843, R37
// A download under way holds the cache busy for uninstall (NET-3): a partial file
// another download holds makes Busy true and UninstallTargets refuse.
func TestADownloadUnderWayHoldsTheCacheBusy(t *testing.T) {
	c, _ := Open(filepath.Join(t.TempDir(), "mailarchive", "program-cache"))
	dir := filepath.Join(c.Root(), "v1.5.0")
	os.MkdirAll(dir, 0o700)
	part := filepath.Join(dir, Assets[2]+partialSuffix)
	os.WriteFile(part, []byte("part"), 0o600)
	if Busy(c.Root()) {
		t.Fatal("positive twin: an unheld partial is not busy")
	}
	h, err := lockfile.HoldFile(part)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	_, uerr := UninstallTargets(c.Root())
	assure.Refused(t, 1, errMsg(uerr), assure.Code(1), assure.Names("under way"), assure.NoSideEffect(func() bool { return Busy(c.Root()) }))
}

// covers: MA-843, R37
// A file is recorded verified only under the checksum file it was checked against
// (NET-4): a SHA256SUMS fetched again meanwhile leaves it unrecorded.
func TestAnAssetIsRecordedOnlyUnderItsOwnChecksumFile(t *testing.T) {
	r := newRelease(t, "v1.5.0", assets(300))
	f := r.fetcher(t)
	dir := filepath.Join(f.Cache.Root(), "v1.5.0")
	f.onBytes = func(int64) {
		writeJSON(filepath.Join(dir, verifiedName), verified{Sums: strings.Repeat("0", 64), Assets: map[string]string{}})
		f.onBytes = nil
	}
	f.Fetch(context.Background(), "v1.5.0")
	v := f.Cache.readVerified(dir)
	_, recorded := v.Assets[Assets[0]]
	assure.Reached(t, !recorded, "not recorded under another checksum file")
}

// covers: MA-843, R37
// Uninstall's plan and run agree (NET-6): an atomic write's leftover temporary file and
// the system's litter are listed and removed; anything else in the cache refuses the
// plan before anything is removed.
func TestUninstallListsLeftoversAndRefusesStrangers(t *testing.T) {
	r := newRelease(t, "v1.5.0", assets(100))
	f := r.fetcher(t)
	f.Fetch(context.Background(), "v1.5.0")
	dir := filepath.Join(f.Cache.Root(), "v1.5.0")
	os.WriteFile(filepath.Join(dir, ".tmp-12345"), []byte("x"), 0o600)
	os.WriteFile(filepath.Join(f.Cache.Root(), ".DS_Store"), []byte("x"), 0o600)
	targets, err := UninstallTargets(f.Cache.Root())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range targets {
		if err := os.Remove(p); err != nil {
			t.Fatalf("the plan's removal failed at %s: %v", p, err)
		}
	}
	r2 := newRelease(t, "v1.5.0", assets(100))
	f2 := r2.fetcher(t)
	f2.Fetch(context.Background(), "v1.5.0")
	os.WriteFile(filepath.Join(f2.Cache.Root(), "v1.5.0", "notes.txt"), []byte("mine"), 0o600)
	_, err = UninstallTargets(f2.Cache.Root())
	_, kept := os.Stat(filepath.Join(f2.Cache.Root(), "v1.5.0", "notes.txt"))
	assure.Refused(t, 1, errMsg(err), assure.Code(1), assure.Names("notes.txt", "did not put there"),
		assure.NoSideEffect(func() bool { return errors.Is(err, ErrUnsafe) && kept == nil }))
}

func errMsg(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
