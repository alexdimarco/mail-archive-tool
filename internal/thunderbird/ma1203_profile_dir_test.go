package thunderbird

import (
	"os"
	"path/filepath"
	"testing"
)

// mkdirs makes each folder.
func mkdirs(t *testing.T, dirs ...string) {
	t.Helper()
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

// writeAt writes body to path.
func writeAt(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// covers: MA-1203
// FindProfileDir never takes a mail store for a profile (the C0c pass's INS-01). A
// Thunderbird folder named "prefs.js" is the mbox file ImapMail/<host>/prefs.js, so the old
// walk, which checked the store itself first, took the store for the profile. A folder is a
// profile only when it holds prefs.js as a regular file (what the restore's and the
// pre-sync's profile checks accept) and the input sits under its own ImapMail/ or Mail/, or
// is the profile itself (which holds an ImapMail/ or Mail/ folder and is not inside a store).
// Positive twins first: an IMAP store, a Local Folders mbox file, and the profile itself each
// find the profile.
//
// Amended by MA-1206 (the C0c pass round 2): the profile is known by its own two files, prefs.js
// and a compatibility.ini that reads as Thunderbird's, not by the names of the folders above it.
// The fixtures carry compatibility.ini as Thunderbird writes it; the case "a store with no profile
// above it" now holds the mail folder named prefs.js and no compatibility.ini (a folder holding
// both files is a profile wherever it sits, MA-1206).
func TestFindProfileDirNeverTakesAStore(t *testing.T) {
	p := t.TempDir()
	store := filepath.Join(p, "ImapMail", "imap.gmail.com")
	local := filepath.Join(p, "Mail", "Local Folders")
	mkdirs(t, store, local)
	markProfile(t, p, onePrefs)
	writeAt(t, filepath.Join(local, "Inbox"), "")
	for _, in := range []string{store, filepath.Join(local, "Inbox"), p} {
		if got, ok := FindProfileDir(in); !ok || got != p {
			t.Fatalf("the positive twin: FindProfileDir(%s) = %q, %v; want %s", in, got, ok, p)
		}
	}

	t.Run("a mail folder named prefs.js", func(t *testing.T) {
		writeAt(t, filepath.Join(store, "prefs.js"), "From - Sat Nov 07 10:00:00 2026\n\n")
		writeAt(t, filepath.Join(store, "prefs.js.msf"), "")
		got, ok := FindProfileDir(store)
		if !ok || got != p {
			t.Fatalf("FindProfileDir(store) = %q, %v; want the profile %s, never the store", got, ok, p)
		}
	})

	t.Run("a store with no profile above it", func(t *testing.T) {
		q := t.TempDir()
		st := filepath.Join(q, "ImapMail", "imap.gmail.com")
		mkdirs(t, st)
		writeAt(t, filepath.Join(st, "prefs.js"), "From - Sat Nov 07 10:00:00 2026\n\n"+onePrefs)
		writeAt(t, filepath.Join(q, "ImapMail", "prefs.js"), "From - Sat Nov 07 10:00:00 2026\n\n"+onePrefs)
		if got, ok := FindProfileDir(st); ok {
			t.Fatalf("a folder inside ImapMail/ was taken for the profile: %s", got)
		}
	})

	t.Run("a link or a folder at prefs.js is no profile", func(t *testing.T) {
		q := t.TempDir()
		st := filepath.Join(q, "ImapMail", "imap.gmail.com")
		mkdirs(t, st, filepath.Join(q, "prefs.js"))
		writeAt(t, filepath.Join(q, "compatibility.ini"), compatINI)
		if got, ok := FindProfileDir(st); ok {
			t.Fatalf("a folder named prefs.js made %s a profile", got)
		}
		r := t.TempDir()
		st = filepath.Join(r, "ImapMail", "imap.gmail.com")
		mkdirs(t, st)
		writeAt(t, filepath.Join(r, "compatibility.ini"), compatINI)
		target, _ := elsewhere(t)
		if err := os.Symlink(target, filepath.Join(r, "prefs.js")); err != nil {
			t.Skip("no links here:", err)
		}
		if got, ok := FindProfileDir(st); ok {
			t.Fatalf("a link at prefs.js made %s a profile", got)
		}
	})
}
