package tbrestore

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// Fake Thunderbird profiles live in temp folders only: no test reads or writes
// ~/Library/Thunderbird, %APPDATA%\Thunderbird or ~/.thunderbird (each test that needs
// a profile list passes its own bases).

// writeFile writes data at path, making its folders.
func writeFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

// makeProfile makes a profile folder (0700) holding prefs.js with the given lines.
func makeProfile(t *testing.T, dir string, prefs ...string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "prefs.js"), "// Mozilla User Preferences\n"+strings.Join(prefs, "\n")+"\n")
	return dir
}

// localPrefs are the prefs.js lines of a profile whose Local Folders is
// [ProfD]Mail/Local Folders, as Thunderbird 153 wrote them in the Windows lab.
func localPrefs() []string {
	return []string{
		`user_pref("mail.accountmanager.localfoldersserver", "server1");`,
		`user_pref("mail.server.server1.directory-rel", "[ProfD]Mail/Local Folders");`,
		`user_pref("mail.server.server1.hostname", "Local Folders");`,
		`user_pref("mail.server.server1.type", "none");`,
		`user_pref("mail.server.server1.storeContractID", "@mozilla.org/msgstore/berkeleystore;1");`,
	}
}

// localProfile is a usable profile with its Local Folders folder made.
func localProfile(t *testing.T) (profile, local string) {
	t.Helper()
	profile = makeProfile(t, filepath.Join(t.TempDir(), "p.default"), localPrefs()...)
	local = filepath.Join(profile, "Mail", "Local Folders")
	if err := os.MkdirAll(local, 0o700); err != nil {
		t.Fatal(err)
	}
	return canonical(profile), canonical(local)
}

// snapshot is every entry under root (relative, with its kind and, for a file, the
// SHA-256 of its bytes), sorted: what a refusal must leave as it found it. Names in
// skip are left out (Thunderbird's own ThunderbirdRefusalFile, when a test allows it).
func snapshot(t *testing.T, root string, skip ...string) string {
	t.Helper()
	var lines []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		for _, s := range skip {
			if d.Name() == s && p != root {
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
		}
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			tgt, _ := os.Readlink(p)
			lines = append(lines, "L "+rel+" -> "+tgt)
		case d.IsDir():
			lines = append(lines, "D "+rel)
		default:
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			sum := sha256.Sum256(b)
			lines = append(lines, "F "+rel+" "+hex.EncodeToString(sum[:]))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// refusedAs asserts err is the restore's typed refusal code, whose words name each
// needle, with nothing changed (unchanged reports so).
func refusedAs(t *testing.T, err error, code string, unchanged func() bool, names ...string) {
	t.Helper()
	rc, msg := 0, ""
	if err != nil {
		msg = err.Error()
		if RefusalCode(err) == code {
			rc = 1
		}
	}
	opts := []assure.RefuseOption{assure.Code(1), assure.Names(names...)}
	if unchanged != nil {
		opts = append(opts, assure.NoSideEffect(unchanged))
	}
	assure.Refused(t, rc, msg, opts...)
}

// noProcesses makes the process probe report none for the test (the OS process list
// is an external binary or OS call: the seam). The owner's own Thunderbird may be
// running on the machine the suite runs on.
func noProcesses(t *testing.T) {
	t.Helper()
	was := processProbe
	processProbe = func() (int, bool) { return 0, true }
	t.Cleanup(func() { processProbe = was })
}
