package thunderbird

import (
	"os"
	"path/filepath"
	"testing"
)

// compatINI is compatibility.ini as Thunderbird writes it at every start: the
// [Compatibility] section first, then LastVersion and the rest.
const compatINI = "[Compatibility]\nLastVersion=153.6.0_20251212000000/20251212000000\nLastOSABI=Darwin_aarch64-gcc3\n"

// markProfile makes dir a profile by its own two files, as Thunderbird leaves them: prefs.js
// holding prefs, and compatibility.ini.
func markProfile(t *testing.T, dir, prefs string) {
	t.Helper()
	mkdirs(t, dir)
	writeAt(t, filepath.Join(dir, "prefs.js"), prefs)
	writeAt(t, filepath.Join(dir, "compatibility.ini"), compatINI)
}

// covers: MA-1206, R9
// A folder is a Thunderbird profile only by its own content (the C0c pass round 2's R2-03,
// R2-04 and R2-06): a regular prefs.js and a regular compatibility.ini that reads as
// Thunderbird's (its first line [Compatibility], with a LastVersion= line in that section),
// and the input is that folder or sits under its own ImapMail/ or Mail/. The nearest folder
// holding both files decides. Folder names above the profile no longer matter, so a profile
// kept in ~/Mail/tb-profile is found (R2-06); a mail store never holds a compatibility.ini that
// reads as Thunderbird's, since an mbox file starts with "From ", so a mail folder named
// prefs.js beside a folder named Mail is never the profile (R2-03); and a copied profile under
// this profile's Mail/ that carries its own two files is its own profile, so its store is
// never given this profile's account (R2-04). Positive twins first.
func TestAProfileIsKnownByItsOwnFiles(t *testing.T) {
	p := t.TempDir()
	markProfile(t, p, onePrefs)
	store := filepath.Join(p, "ImapMail", "imap.gmail.com")
	local := filepath.Join(p, "Mail", "Local Folders")
	mkdirs(t, store, local)
	writeAt(t, filepath.Join(local, "Inbox"), "")
	for _, in := range []string{store, filepath.Join(local, "Inbox"), p, filepath.Join(p, "ImapMail")} {
		if got, ok := FindProfileDir(in); !ok || got != p {
			t.Fatalf("the positive twin: FindProfileDir(%s) = %q, %v; want %s", in, got, ok, p)
		}
	}

	t.Run("the profile under a folder named Mail (R2-06)", func(t *testing.T) {
		prof := filepath.Join(t.TempDir(), "Mail", "tb-profile")
		markProfile(t, prof, onePrefs)
		st := filepath.Join(prof, "ImapMail", "imap.gmail.com")
		mkdirs(t, st)
		for _, in := range []string{prof, st} {
			if got, ok := FindProfileDir(in); !ok || got != prof {
				t.Fatalf("FindProfileDir(%s) = %q, %v; want %s", in, got, ok, prof)
			}
		}
	})

	t.Run("prefs.js alone is no profile", func(t *testing.T) {
		q := t.TempDir()
		st := filepath.Join(q, "ImapMail", "imap.gmail.com")
		mkdirs(t, st)
		writeAt(t, filepath.Join(q, "prefs.js"), onePrefs)
		if got, ok := FindProfileDir(st); ok {
			t.Fatalf("a folder with prefs.js and no compatibility.ini was taken for the profile: %s", got)
		}
	})

	t.Run("a mail folder named prefs.js beside a folder named Mail (R2-03)", func(t *testing.T) {
		q := t.TempDir()
		markProfile(t, q, onePrefs)
		st := filepath.Join(q, "ImapMail", "imap.gmail.com")
		mkdirs(t, filepath.Join(st, "Mail", "cur"))
		writeAt(t, filepath.Join(st, "prefs.js"), "From - Sat Nov 07 10:00:00 2026\n\nbody\n")
		// Even a mail folder named compatibility.ini whose message holds Thunderbird's lines.
		writeAt(t, filepath.Join(st, "compatibility.ini"), "From - Sat Nov 07 10:00:00 2026\nSubject: hi\n\n[Compatibility]\nLastVersion=153.6.0\n")
		if got, ok := FindProfileDir(filepath.Join(st, "Mail")); !ok || got != q {
			t.Fatalf("FindProfileDir(store/Mail) = %q, %v; want the profile %s, never the store", got, ok, q)
		}
	})

	for name, ini := range map[string]string{
		"no [Compatibility] line":           "LastVersion=153.6.0\n",
		"[Compatibility] not first":         "From - Sat Nov 07 10:00:00 2026\n[Compatibility]\nLastVersion=153.6.0\n",
		"no LastVersion line":               "[Compatibility]\nLastOSABI=Darwin_aarch64-gcc3\n",
		"LastVersion in another section":    "[Compatibility]\n[Other]\nLastVersion=153.6.0\n",
		"an empty file":                     "",
		"a [Compatibility] in another case": "[compatibility]\nLastVersion=153.6.0\n",
	} {
		t.Run("compatibility.ini: "+name, func(t *testing.T) {
			q := t.TempDir()
			st := filepath.Join(q, "ImapMail", "imap.gmail.com")
			mkdirs(t, st)
			writeAt(t, filepath.Join(q, "prefs.js"), onePrefs)
			writeAt(t, filepath.Join(q, "compatibility.ini"), ini)
			if got, ok := FindProfileDir(st); ok {
				t.Fatalf("a compatibility.ini that is not Thunderbird's made %s a profile", got)
			}
		})
	}

	t.Run("compatibility.ini as a folder or a link", func(t *testing.T) {
		q := t.TempDir()
		st := filepath.Join(q, "ImapMail", "imap.gmail.com")
		mkdirs(t, st, filepath.Join(q, "compatibility.ini"))
		writeAt(t, filepath.Join(q, "prefs.js"), onePrefs)
		if got, ok := FindProfileDir(st); ok {
			t.Fatalf("a folder named compatibility.ini made %s a profile", got)
		}
		r := t.TempDir()
		st = filepath.Join(r, "ImapMail", "imap.gmail.com")
		mkdirs(t, st)
		writeAt(t, filepath.Join(r, "prefs.js"), onePrefs)
		target := filepath.Join(t.TempDir(), "compatibility.ini")
		writeAt(t, target, compatINI)
		if err := os.Symlink(target, filepath.Join(r, "compatibility.ini")); err != nil {
			t.Skip("no links here:", err)
		}
		if got, ok := FindProfileDir(st); ok {
			t.Fatalf("a link at compatibility.ini made %s a profile", got)
		}
	})

	t.Run("a copied profile under this profile's Mail/ is its own profile (R2-04)", func(t *testing.T) {
		q := t.TempDir()
		markProfile(t, q, onePrefs)
		mkdirs(t, filepath.Join(q, "ImapMail", "imap.gmail.com"))
		old := filepath.Join(q, "Mail", "old-profile")
		oldPrefs := live("server7") + imapServer("server7", "imap.gmail.com", "[ProfD]ImapMail/imap.gmail.com", "")
		markProfile(t, old, oldPrefs)
		st := filepath.Join(old, "ImapMail", "imap.gmail.com")
		mkdirs(t, st)
		got, ok := FindProfileDir(st)
		if !ok || got != old {
			t.Fatalf("FindProfileDir(the copy's store) = %q, %v; want the copy %s", got, ok, old)
		}
		acct, err := AccountToEnable(got, st)
		if err != nil || acct == nil || acct.ServerKey != "server7" {
			t.Fatalf("the copy's store was given %+v (%v), want the copy's own server7", acct, err)
		}
	})

	t.Run("a path in the profile but outside its mail folders", func(t *testing.T) {
		q := t.TempDir()
		markProfile(t, q, onePrefs)
		in := filepath.Join(q, "extensions", "x")
		mkdirs(t, in)
		if got, ok := FindProfileDir(in); ok {
			t.Fatalf("a folder outside ImapMail/ and Mail/ was given the profile %s", got)
		}
		// The nearest profile decides: a folder outside the mail of a copy kept under this
		// profile's Mail/ is the copy's, never this profile's store.
		old := filepath.Join(q, "Mail", "old-profile")
		markProfile(t, old, onePrefs)
		in = filepath.Join(old, "extensions", "x")
		mkdirs(t, in)
		if got, ok := FindProfileDir(in); ok {
			t.Fatalf("a folder of the copy %s outside its mail was given the profile %s", old, got)
		}
	})
}
