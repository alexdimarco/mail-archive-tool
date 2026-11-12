//go:build unix

package tbcomplete

// Slice C1a on Unix: a link or a pipe planted where the reader looks (TC-15). The cache file is
// read through util.ReadRegularFile, so a link is refused and never followed and a pipe is never
// waited on; a key's path is never opened, so a pipe at a folder's .msf cannot hang a scheduled
// backup; and the "still in the store" check (M2) never follows a link out of the store.

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// within fails the test unless fn returns within a few seconds: a reader that waited on a pipe
// would block for good.
func within(t *testing.T, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { defer close(done); fn() }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("%s: still waiting after 10 seconds (a pipe was opened)", what)
	}
}

// covers: MA-1171
// A link at folderCache.json is refused, not followed, and a pipe there is refused at once, never
// waited on; each is a CacheError of kind CacheNotRegular naming the file, and what was planted
// is left as it was. Positive twin first: the same cache as a regular file reads.
func TestALinkOrAPipeAtTheCacheIsRefused(t *testing.T) {
	p, store := newProfile(t, "imap.gmail.com")
	writeCache(t, p, pair{filepath.Join(store, "INBOX.msf"), folder(0, 1, 1)})
	if s := readStore(t, p, store); len(s.Folders) != 1 {
		t.Fatalf("the positive twin read %+v", s)
	}

	t.Run("a link", func(t *testing.T) {
		p, store := newProfile(t, "imap.gmail.com")
		target := filepath.Join(t.TempDir(), "elsewhere.json")
		write(t, target, cacheJSON(t, pair{filepath.Join(store, "INBOX.msf"), folder(0, 1, 1)}))
		if err := os.Symlink(target, filepath.Join(p, CacheFile)); err != nil {
			t.Fatal(err)
		}
		cacheRefused(t, p, CacheNotRegular)
	})
	t.Run("a pipe", func(t *testing.T) {
		p, _ := newProfile(t, "imap.gmail.com")
		if err := syscall.Mkfifo(filepath.Join(p, CacheFile), 0o600); err != nil {
			t.Fatal(err)
		}
		within(t, "ReadCache", func() { cacheRefused(t, p, CacheNotRegular) })
	})
}

// covers: MA-1170
// A key's path is never opened: a pipe at a folder's .msf (its exact key) and at an old-path
// folder's mailbox file in the store (the M2 check, an Lstat) never block. And the M2 check never
// follows a link: with the store's a.sbd a link to a folder outside the profile, an old-path key
// for a.sbd/b.msf is checked no further than the link, which reads as "can't tell" (the folder may
// be in the store), never as gone. Positive twin first: the same old key with a.sbd a real folder
// holding b.msf is "can't tell" too, checked inside the store.
func TestAKeysPathIsNeverOpenedOrFollowed(t *testing.T) {
	old := filepath.Join(string(filepath.Separator)+"old", "ImapMail", "imap.gmail.com")

	p, store := newProfile(t, "imap.gmail.com")
	write(t, filepath.Join(store, "a.sbd", "b.msf"), "// mork\n")
	writeCache(t, p, pair{filepath.Join(store, "INBOX.msf"), folder(0, 1, 1)}, pair{filepath.Join(old, "a.sbd", "b.msf"), folder(0, 1, 1)})
	log := recordLstat(t)
	if s := readStore(t, p, store); !equal(paths(s.OldPathOnly), []string{"a/b"}) || len(log.outside(store)) != 0 {
		t.Fatalf("the positive twin: old-path-only %q, lstat %q", paths(s.OldPathOnly), log.all())
	}

	t.Run("pipes at a key's paths", func(t *testing.T) {
		p, store := newProfile(t, "imap.gmail.com")
		for _, f := range []string{"INBOX.msf", "Sent"} {
			if err := syscall.Mkfifo(filepath.Join(store, f), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		writeCache(t, p, pair{filepath.Join(store, "INBOX.msf"), folder(0, 1, 1)}, pair{filepath.Join(old, "Sent.msf"), folder(0, 1, 1)})
		within(t, "Store", func() {
			s := readStore(t, p, store)
			if !equal(paths(s.Folders), []string{"INBOX"}) || !equal(paths(s.OldPathOnly), []string{"Sent"}) {
				t.Errorf("folders %q, old-path-only %q", paths(s.Folders), paths(s.OldPathOnly))
			}
		})
	})
	t.Run("a folder that cannot be looked in reads as can't tell", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root looks in a folder without permission; the Linux Docker run is as user 1000")
		}
		p, store := newProfile(t, "imap.gmail.com")
		locked := filepath.Join(store, "a.sbd")
		mkdir(t, locked)
		if err := os.Chmod(locked, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(locked, 0o755) })
		writeCache(t, p, pair{filepath.Join(store, "INBOX.msf"), folder(0, 1, 1)}, pair{filepath.Join(old, "a.sbd", "b.msf"), folder(0, 1, 1)})
		if s := readStore(t, p, store); !equal(paths(s.OldPathOnly), []string{"a/b"}) {
			t.Fatalf("old-path-only %q; want a/b can't tell (never read as gone)", paths(s.OldPathOnly))
		}
	})
	t.Run("a link in the store is never followed", func(t *testing.T) {
		p, store := newProfile(t, "imap.gmail.com")
		outside := t.TempDir()
		write(t, filepath.Join(outside, "b.msf"), "outside")
		if err := os.Symlink(outside, filepath.Join(store, "a.sbd")); err != nil {
			t.Fatal(err)
		}
		writeCache(t, p, pair{filepath.Join(store, "INBOX.msf"), folder(0, 1, 1)}, pair{filepath.Join(old, "a.sbd", "b.msf"), folder(0, 1, 1)})
		log := recordLstat(t)
		s := readStore(t, p, store)
		if !equal(paths(s.OldPathOnly), []string{"a/b"}) {
			t.Fatalf("old-path-only %q; want a/b can't tell", paths(s.OldPathOnly))
		}
		for _, got := range log.all() {
			if got != filepath.Join(store, "a.sbd") {
				t.Fatalf("checked %s, past the link (or outside the store)", got)
			}
		}
	})
}

// covers: MA-1170
// The store's current path has two spellings when a folder above it is a link (macOS's /var is
// /private/var): Thunderbird writes its keys through one, a backup may be given the other, and
// both are the store's current path, so its keys are exact (and an old key beside them is still
// ignored, not taken for a moved profile's). Positive twin first: the spelling the keys use.
func TestEitherSpellingOfTheStoreIsItsCurrentPath(t *testing.T) {
	p, store := newProfile(t, "imap.gmail.com")
	real, err := filepath.EvalSymlinks(store)
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "via-link")
	if err := os.Symlink(p, link); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(string(filepath.Separator)+"old", "ImapMail", "imap.gmail.com", "INBOX.msf")
	writeCache(t, p, pair{filepath.Join(real, "INBOX.msf"), folder(0, 1, 1)}, pair{old, folder(0, 9, 9)})
	for _, given := range []string{real, filepath.Join(link, "ImapMail", "imap.gmail.com")} {
		c, err := ReadCache(p)
		if err != nil {
			t.Fatal(err)
		}
		s, err := c.Store(given)
		if err != nil {
			t.Fatal(err)
		}
		if s.Mode != Exact || !equal(paths(s.Folders), []string{"INBOX"}) || s.IgnoredOldKeys != 1 {
			t.Fatalf("given %s: %+v; want exact, INBOX from its current key, the old key ignored", given, s)
		}
	}
}
