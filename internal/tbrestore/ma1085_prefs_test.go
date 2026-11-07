package tbrestore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// covers: MA-1085, R40, S73
// prefs.js string values decoded as JavaScript string literals (design §3.2 as
// folded, RT-15): \\, \", \', \n, \r, \t, \uXXXX and a surrogate pair; an unknown
// escape and a lone surrogate refused rather than guessed (W-PREFS, naming the file);
// user.js read after prefs.js and winning; a prefs.js or user.js over 16 MiB refused
// with W-PREFS naming that file. The files are never written.
func TestRestorePrefsDecoding(t *testing.T) {
	dir := makeProfile(t, filepath.Join(t.TempDir(), "p"),
		`user_pref("mail.server.server1.directory", "C:\\Users\\Ann\\AppData\\Roaming\\Thunderbird\\Mail\\Local Folders");`,
		`user_pref("mail.a", "q\"uote 'single' \'esc\' tab\tnl\ncr\r");`,
		`user_pref("mail.b", "caf\u00e9 \ud83d\udce8");`,
		`user_pref("mail.c", true);`,
		`user_pref("mail.d", 42);`,
		`user_pref("other.x", "\q is not mail");`,
	)
	writeFile(t, filepath.Join(dir, "user.js"), `user_pref("mail.d", 7);`+"\n"+`user_pref('mail.e', 'single quoted');`+"\n")
	before := snapshot(t, dir)
	p, err := ReadPrefs(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"mail.server.server1.directory": `C:\Users\Ann\AppData\Roaming\Thunderbird\Mail\Local Folders`,
		"mail.a":                        "q\"uote 'single' 'esc' tab\tnl\ncr\r",
		"mail.b":                        "café 📨",
		"mail.c":                        "true",
		"mail.d":                        "7",
		"mail.e":                        "single quoted",
	}
	for k, w := range want {
		if got, ok, err := p.Get(k); err != nil || !ok || got != w {
			t.Errorf("%s: got %q %v %v, want %q", k, got, ok, err, w)
		}
	}
	if snapshot(t, dir) != before {
		t.Fatal("reading the settings changed the profile")
	}

	for _, bad := range []string{`"mail.server.server1.directory", "x\qy"`, `"mail.server.server1.directory", "\ud83d"`, `"mail.server.server1.directory", "\udce8"`} {
		dir := makeProfile(t, filepath.Join(t.TempDir(), "p"), localPrefs()...)
		writeFile(t, filepath.Join(dir, "user.js"), "user_pref("+bad+");\n")
		p, err := ReadPrefs(dir)
		if err != nil {
			t.Fatalf("%s: a bad value is kept as unreadable, not a read failure: %v", bad, err)
		}
		before := snapshot(t, dir)
		_, err = FindLocalFolders(dir, p)
		// The bad value is in user.js, so the words name user.js (foldcheck N11).
		refusedAs(t, err, "W-PREFS", func() bool { return snapshot(t, dir) == before }, "user.js", "could not read")
	}

	for _, name := range []string{"prefs.js", "user.js"} {
		dir := makeProfile(t, filepath.Join(t.TempDir(), "p"), localPrefs()...)
		big := strings.Repeat("// padding\n", PrefsMaxBytes/11+1)
		writeFile(t, filepath.Join(dir, name), big)
		_, err := ReadPrefs(dir)
		refusedAs(t, err, "W-PREFS", nil, name, "larger than MailArchive reads")
	}
}

// covers: MA-1085, R40, S73
// The Local Folders lookup (design §3.2): by mail.accountmanager.localfoldersserver,
// and by the one server of type "none"; directory-rel ([ProfD]…) over directory; a
// [ProfD] path with ".." resolved as Mozilla does and then checked (refused, W-OVERLAP,
// when it lands on another account's folder); directory (absolute) when there is no
// directory-rel; no Local Folders, two of type "none", or a named server with no
// settings refused with W-NOLOCAL, prefs.js and user.js byte-identical before and
// after; a folder under ImapMail and the archive folder refused (W-OVERLAP).
func TestRestoreLocalFoldersLookup(t *testing.T) {
	t.Run("named server, directory-rel wins", func(t *testing.T) {
		profile, local := localProfile(t)
		other := filepath.Join(t.TempDir(), "ignored")
		if err := os.MkdirAll(other, 0o700); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(profile, "user.js"), `user_pref("mail.server.server1.directory", "`+jsEscape(other)+`");`+"\n")
		p, err := ReadPrefs(profile)
		if err != nil {
			t.Fatal(err)
		}
		lf, err := FindLocalFolders(profile, p)
		if err != nil || lf.Dir != local || lf.ServerKey != "server1" || lf.Identity == (Identity{}) {
			t.Fatalf("got %+v %v, want %s", lf, err, local)
		}
	})
	t.Run("type none, absolute directory", func(t *testing.T) {
		local := filepath.Join(t.TempDir(), "LF")
		if err := os.MkdirAll(local, 0o700); err != nil {
			t.Fatal(err)
		}
		profile := makeProfile(t, filepath.Join(t.TempDir(), "p"),
			`user_pref("mail.server.server2.type", "imap");`,
			`user_pref("mail.server.server2.directory-rel", "[ProfD]ImapMail/imap.example.com");`,
			`user_pref("mail.server.server3.type", "none");`,
			`user_pref("mail.server.server3.directory", "`+jsEscape(local)+`");`)
		p, _ := ReadPrefs(profile)
		lf, err := FindLocalFolders(profile, p)
		if err != nil || lf.Dir != canonical(local) || lf.ServerKey != "server3" {
			t.Fatalf("got %+v %v", lf, err)
		}
	})
	t.Run("[ProfD] with .. resolved, then checked", func(t *testing.T) {
		root := t.TempDir()
		profile := makeProfile(t, filepath.Join(root, "p"),
			`user_pref("mail.accountmanager.localfoldersserver", "server1");`,
			`user_pref("mail.server.server1.type", "none");`,
			`user_pref("mail.server.server1.directory-rel", "[ProfD]../Beside");`)
		if err := os.MkdirAll(filepath.Join(root, "Beside"), 0o700); err != nil {
			t.Fatal(err)
		}
		p, _ := ReadPrefs(profile)
		lf, err := FindLocalFolders(profile, p)
		if err != nil || lf.Dir != canonical(filepath.Join(root, "Beside")) {
			t.Fatalf("a .. path is joined as Mozilla joins it: %+v %v", lf, err)
		}
		// The same .. landing on another account's folder.
		writeFile(t, filepath.Join(profile, "user.js"),
			`user_pref("mail.server.server2.type", "pop3");`+"\n"+
				`user_pref("mail.server.server2.hostname", "pop.example.com");`+"\n"+
				`user_pref("mail.server.server2.directory-rel", "[ProfD]../Beside/pop");`+"\n")
		before := snapshot(t, profile)
		p, _ = ReadPrefs(profile)
		_, err = FindLocalFolders(profile, p)
		refusedAs(t, err, "W-OVERLAP", func() bool { return snapshot(t, profile) == before }, "overlaps", "pop.example.com", "Choose another profile")
	})
	t.Run("no Local Folders", func(t *testing.T) {
		for name, prefs := range map[string][]string{
			"no server at all": {`user_pref("mail.account.lastLoadedVersion", 1);`},
			"two of type none": {`user_pref("mail.server.server1.type", "none");`, `user_pref("mail.server.server1.directory-rel", "[ProfD]");`,
				`user_pref("mail.server.server2.type", "none");`, `user_pref("mail.server.server2.directory-rel", "[ProfD]");`},
			"named server missing": {`user_pref("mail.accountmanager.localfoldersserver", "server9");`, `user_pref("mail.server.server1.type", "imap");`},
			"server with no dir":   {`user_pref("mail.accountmanager.localfoldersserver", "server1");`, `user_pref("mail.server.server1.type", "none");`},
		} {
			profile := makeProfile(t, filepath.Join(t.TempDir(), "p"), prefs...)
			writeFile(t, filepath.Join(profile, "user.js"), "// the person's own\n")
			before := snapshot(t, profile)
			p, err := ReadPrefs(profile)
			if err != nil {
				t.Fatal(err)
			}
			_, err = FindLocalFolders(profile, p)
			// The remedy is lab row MA-1133's menu path (Thunderbird 153.6.0 on Windows,
			// 2026-11-07): ≡ → Add Account → Newsgroup made Local Folders on a profile with
			// no account, and stayed after that account was deleted; Email (an IMAP
			// account) made it too; Feed did not.
			refusedAs(t, err, "W-NOLOCAL", func() bool { return snapshot(t, profile) == before },
				"has no Local Folders yet", "nothing was written", "menu button (≡, top right)",
				"Add Account", "Email", "Newsgroup", "Finish", "Local Folders stays",
				"click Check again", "does not change Thunderbird's settings")
			if strings.Contains(err.Error(), "MENU PATH") {
				t.Fatalf("%s: the placeholder is still in W-NOLOCAL: %s", name, err)
			}
		}
	})
	t.Run("under ImapMail, or the archive folder", func(t *testing.T) {
		profile := makeProfile(t, filepath.Join(t.TempDir(), "p"),
			`user_pref("mail.server.server1.type", "none");`,
			`user_pref("mail.server.server1.directory-rel", "[ProfD]ImapMail/x");`)
		if err := os.MkdirAll(filepath.Join(profile, "ImapMail", "x"), 0o700); err != nil {
			t.Fatal(err)
		}
		p, _ := ReadPrefs(profile)
		_, err := FindLocalFolders(profile, p)
		refusedAs(t, err, "W-OVERLAP", nil, "ImapMail")

		prof2, local := localProfile(t)
		p2, _ := ReadPrefs(prof2)
		lf, err := FindLocalFolders(prof2, p2)
		if err != nil {
			t.Fatal(err)
		}
		if err := CheckArchiveOverlap(lf, filepath.Join(t.TempDir(), "archive")); err != nil {
			t.Fatalf("an archive elsewhere does not overlap: %v", err)
		}
		refusedAs(t, CheckArchiveOverlap(lf, filepath.Join(local, "archive")), "W-OVERLAP", nil, "the archive folder")
	})
	t.Run("Local Folders missing", func(t *testing.T) {
		profile := makeProfile(t, filepath.Join(t.TempDir(), "p"), localPrefs()...)
		p, _ := ReadPrefs(profile)
		_, err := FindLocalFolders(profile, p)
		refusedAs(t, err, "W-LOCALGONE", nil, "Local Folders", "not there")
	})
}

// jsEscape writes a path as prefs.js does (backslashes doubled).
func jsEscape(s string) string { return strings.ReplaceAll(s, `\`, `\\`) }
