package presync

import (
	"fmt"
	"hash/fnv"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// covers: MA-564
// No writes to Thunderbird (invariant R31, design 4.4.2): running a full pre-sync
// session over a real fixture profile tree — watched through the actual digest
// walk — leaves that tree byte-identical (a whole-tree hash of names, sizes,
// modes and bytes is unchanged), so the tool never edits, deletes or moves a
// Thunderbird file. The tool's own record is written only under the separate
// state directory at 0600 and never leaks into the profile.
func TestPresyncNoWritesToThunderbird(t *testing.T) {
	base := time.Date(2026, 10, 22, 2, 0, 0, 0, time.UTC)
	const app = "/Applications/Thunderbird.app/Contents/MacOS/thunderbird"
	const profile = "/Users/x/profile.default"

	profDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(profDir, "ImapMail", "acct"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(profDir, "prefs.js"), "user_pref(\"x\", 1);")
	writeFile(t, filepath.Join(profDir, "ImapMail", "acct", "INBOX"), "From a@b\n\nbody\n")
	stateDir := t.TempDir()
	statePath := filepath.Join(stateDir, "presync-x.json")

	before := treeHash(t, profDir)

	f := newFakeEnv()
	f.sm, f.realDigest = true, true
	f.boot, f.idle, f.idleOK, f.lockOK = 1000, 300, true, true
	f.appearIn, f.exitIn = 1*time.Second, 1*time.Second
	f.nowT = base
	f.child = proc{pid: 7, exe: app, start: base, args: "thunderbird -profile " + profile}

	d := deps{
		env:       f,
		cfg:       quietConfig{settle: time.Second, quiet: time.Second, maxWait: 3 * time.Second},
		app:       app,
		profile:   profile,
		watch:     []string{profDir},
		statePath: statePath,
		lockPath:  "/l",
		base:      record{Version: recordVersion, Profile: profile, App: app},
		awaitPoll: time.Second,
		awaitUp:   30 * time.Second,
		pollEvery: time.Second,
		quitGrace: 10 * time.Second,
		idleAway:  120 * time.Second,
	}

	runSession(d) // the outcome is beside the point; this asserts the tree is untouched

	if after := treeHash(t, profDir); after != before {
		t.Errorf("the Thunderbird profile tree changed during a pre-sync session (R31: never edit a Thunderbird file)")
	}

	// The tool's own record is a 0600 file under the state directory...
	if err := storeRecord(statePath, record{Version: recordVersion, State: stateIdle}); err != nil {
		t.Fatalf("store: %v", err)
	}
	fi, err := os.Stat(statePath)
	if err != nil {
		t.Fatalf("state record not written: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("state record mode = %v, want 0600", fi.Mode().Perm())
	}
	// ...and nothing of the tool's leaked into the profile.
	if _, err := os.Stat(filepath.Join(profDir, filepath.Base(statePath))); !os.IsNotExist(err) {
		t.Errorf("a tool file leaked into the Thunderbird profile")
	}
}

// treeHash folds names, sizes, modes and bytes of every entry under root into one
// value, so any edit, addition or deletion moves it.
func treeHash(t *testing.T, root string) uint64 {
	t.Helper()
	h := fnv.New64a()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		info, ierr := d.Info()
		if ierr != nil {
			return ierr
		}
		fmt.Fprintf(h, "%s|%d|%o|", rel, info.Size(), info.Mode().Perm())
		if info.Mode().IsRegular() {
			b, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			h.Write(b)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("treeHash %s: %v", root, err)
	}
	return h.Sum64()
}
