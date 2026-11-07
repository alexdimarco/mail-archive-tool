package tbrestore

import (
	"os"
	"path/filepath"
	"testing"
)

// covers: MA-1086, R40
// The store kind (design §3.3 as folded, RT-20): an mbox Local Folders goes on
// (positive twin first); maildir by the account's setting, by the profile-wide
// setting, and on disk by a folder holding cur, new or tmp (each alone), or an Inbox
// or Trash that is a folder, each refused with W-MAILDIR before anything is written.
func TestRestoreStoreKind(t *testing.T) {
	check := func(t *testing.T, extraPrefs []string, mkdirs ...string) (error, func() bool) {
		t.Helper()
		profile, local := localProfile(t)
		if len(extraPrefs) > 0 {
			writeFile(t, filepath.Join(profile, "user.js"), joinLines(extraPrefs))
		}
		for _, d := range mkdirs {
			if err := os.MkdirAll(filepath.Join(local, filepath.FromSlash(d)), 0o700); err != nil {
				t.Fatal(err)
			}
		}
		writeFile(t, filepath.Join(local, "Trashlike"), "")
		before := snapshot(t, profile)
		p, err := ReadPrefs(profile)
		if err != nil {
			t.Fatal(err)
		}
		lf, err := FindLocalFolders(profile, p)
		if err != nil {
			t.Fatal(err)
		}
		return CheckStoreKind(p, lf), func() bool { return snapshot(t, profile) == before }
	}

	if err, _ := check(t, nil, "Inbox.sbd", "Restored — X.sbd"); err != nil {
		t.Fatalf("an mbox Local Folders goes on: %v", err)
	}
	if err, _ := check(t, []string{`user_pref("mail.server.server1.storeContractID", "");`}); err != nil {
		t.Fatalf("an empty account setting falls to the default (mbox): %v", err)
	}
	cases := map[string]struct {
		prefs []string
		dirs  []string
	}{
		"account setting": {prefs: []string{`user_pref("mail.server.server1.storeContractID", "@mozilla.org/msgstore/maildirstore;1");`}},
		"profile-wide":    {prefs: []string{`user_pref("mail.server.server1.storeContractID", "");`, `user_pref("mail.serverDefaultStoreContractID", "@mozilla.org/msgstore/maildirstore;1");`}},
		"unknown kind":    {prefs: []string{`user_pref("mail.server.server1.storeContractID", "@example.org/store;1");`}},
		"cur on disk":     {dirs: []string{"Sent/cur"}},
		"new on disk":     {dirs: []string{"Sent/new"}},
		"tmp alone":       {dirs: []string{"Sent/tmp"}},
		"Inbox a folder":  {dirs: []string{"Inbox"}},
		"Trash a folder":  {dirs: []string{"Trash"}},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			err, unchanged := check(t, c.prefs, c.dirs...)
			refusedAs(t, err, "W-MAILDIR", unchanged, "maildir", "nothing was written", "Choose another profile")
		})
	}
}

func joinLines(lines []string) string {
	out := ""
	for _, l := range lines {
		out += l + "\n"
	}
	return out
}
